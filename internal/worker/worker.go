package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/vsayfb/gig-platform-categorization-service/internal/awsclient"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/internal/processor"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("worker")

// messageProcessingTimeout bounds a single message's processing time. It is
// deliberately independent of the worker's shutdown context: in-flight work
// is given up to this long to finish cleanly on SIGTERM rather than being
// cut off mid-transaction, but a stuck AI/DB call still can't hang forever.
const (
	messageProcessingTimeout = 2 * time.Minute
	messageVisibilityTimeout = 3 * time.Minute
	minimumReceiveBackoff    = time.Second
	maximumReceiveBackoff    = 30 * time.Second
)

// sqsClient is the subset of *sqs.Client this package actually calls. Having
// it as an interface (rather than depending on the concrete AWS SDK type
// directly) lets tests inject a fake and exercise Run()'s dispatch/shutdown
// behavior without a real SQS queue. *sqs.Client already satisfies this, so
// production wiring in New() is unaffected.
type sqsClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

type Worker struct {
	client      sqsClient
	queueURL    string
	handlers    map[string]processor.Processor
	concurrency int
}

func New(
	ctx context.Context,
	cfg *config.Config,
	queueURL string,
	handlers map[string]processor.Processor,
) (*Worker, error) {
	awsCfg, err := awsclient.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	concurrency := cfg.Server.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = config.DefaultWorkerConcurrency
	}

	return &Worker{
		client:      awsclient.NewSQS(awsCfg, cfg),
		queueURL:    queueURL,
		handlers:    handlers,
		concurrency: concurrency,
	}, nil
}

// Run polls SQS and dispatches messages to a bounded pool of goroutines so
// that slow, I/O-bound work (AI extraction, DB writes) for one message
// doesn't block the rest of the batch. On shutdown (ctx canceled) it stops
// pulling new messages and waits for in-flight ones to finish before
// returning.
func (w *Worker) Run(ctx context.Context) error {
	slog.Info("worker started", "queue", w.queueURL, "concurrency", w.concurrency)

	sem := make(chan struct{}, w.concurrency)
	var wg sync.WaitGroup
	receiveFailures := 0

	defer func() {
		slog.Info("draining in-flight messages before shutdown")
		wg.Wait()
		slog.Info("all in-flight messages finished")
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		resp, err := w.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(w.queueURL),
			MaxNumberOfMessages:   10,
			WaitTimeSeconds:       20,
			VisibilityTimeout:     int32(messageVisibilityTimeout / time.Second),
			MessageAttributeNames: []string{"All"},
		})

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			receiveFailures++
			retryIn := receiveBackoff(receiveFailures)
			slog.ErrorContext(
				ctx,
				"receive failed",
				"err", err,
				"consecutive_failures", receiveFailures,
				"retry_in", retryIn,
			)
			if err := waitForReceiveRetry(ctx, retryIn); err != nil {
				return err
			}
			continue
		}
		receiveFailures = 0

		for _, m := range resp.Messages {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			wg.Add(1)

			go func(m types.Message) {
				defer wg.Done()
				defer func() { <-sem }()

				// Detached from the shutdown context on purpose: a SIGTERM
				// shouldn't abort a message that's already been pulled off
				// the queue and is mid-processing. It's still bounded by
				// messageProcessingTimeout so a stuck call can't hang the
				// worker's drain step forever.
				workCtx, cancel := context.WithTimeout(context.Background(), messageProcessingTimeout)
				defer cancel()

				w.handleMessageSafely(workCtx, m)
			}(m)
		}
	}
}

func receiveBackoff(consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 1 {
		return minimumReceiveBackoff
	}

	delay := minimumReceiveBackoff
	for attempt := 1; attempt < consecutiveFailures; attempt++ {
		if delay >= maximumReceiveBackoff/2 {
			return maximumReceiveBackoff
		}
		delay *= 2
	}
	if delay > maximumReceiveBackoff {
		return maximumReceiveBackoff
	}
	return delay
}

func waitForReceiveRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handleMessageSafely is the panic boundary for a single message: a bug in
// a processor (or anywhere in the call chain) must not take down the whole
// worker goroutine. The message is simply left on the queue for redelivery
// rather than being deleted.
func (w *Worker) handleMessageSafely(ctx context.Context, m types.Message) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic recovered while handling message",
				"message_id", aws.ToString(m.MessageId),
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}
	}()

	w.handleMessage(ctx, m)
}

func (w *Worker) handleMessage(ctx context.Context, m types.Message) {
	msgType := aws.ToString(m.MessageAttributes["Type"].StringValue)

	msgCtx := tracing.ExtractTraceContext(ctx, m.MessageAttributes)

	msgCtx, span := tracer.Start(
		msgCtx,
		"process-message",
		trace.WithAttributes(
			attribute.String("messaging.system", "aws.sqs"),
			attribute.String("messaging.message.id", aws.ToString(m.MessageId)),
			attribute.String("messaging.message.type", msgType),
		),
	)
	defer span.End()

	if err := w.processMessage(msgCtx, m); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		if processor.IsPermanent(err) {
			if deleteErr := w.deleteMessage(msgCtx, m); deleteErr != nil {
				slog.ErrorContext(msgCtx,
					"failed to discard permanently invalid message",
					"message_id", aws.ToString(m.MessageId),
					"message_type", msgType,
					"err", deleteErr,
				)
				return
			}

			slog.WarnContext(msgCtx,
				"discarded permanently invalid message",
				"message_id", aws.ToString(m.MessageId),
				"message_type", msgType,
				"err", err,
			)
			return
		}

		slog.ErrorContext(msgCtx,
			"message processing failed",
			"message_id", aws.ToString(m.MessageId),
			"message_type", msgType,
			"err", err,
		)

		return
	}

	span.SetStatus(codes.Ok, "processed successfully")
}

func (w *Worker) processMessage(ctx context.Context, m types.Message) (err error) {
	start := time.Now()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while processing message: %v", r)
			slog.ErrorContext(ctx, "panic recovered while processing message",
				"message_id", aws.ToString(m.MessageId),
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}

		metrics.ObserveWorkerProcessing(ctx, start, err)
	}()

	msg := processor.Message{
		ID:   aws.ToString(m.MessageId),
		Body: aws.ToString(m.Body),
		Type: aws.ToString(m.MessageAttributes["Type"].StringValue),
	}

	if msg.Type == "" {
		return processor.Permanent(errors.New("missing message type"))
	}

	handler, ok := w.handlers[msg.Type]

	if !ok {
		return processor.Permanent(fmt.Errorf("no handler registered for message type %q", msg.Type))
	}

	if err = handler.Process(ctx, msg); err != nil {
		slog.ErrorContext(ctx,
			"handler returned error",
			"message_id", msg.ID,
			"message_type", msg.Type,
			"err", err,
		)

		return err
	}

	slog.InfoContext(ctx,
		"message processed successfully",
		"message_id", msg.ID,
		"message_type", msg.Type,
	)

	if err = w.deleteMessage(ctx, m); err != nil {
		slog.WarnContext(ctx,
			"failed to delete message",
			"message_id", msg.ID,
			"err", err,
		)

		return err
	}

	return nil
}

func (w *Worker) deleteMessage(ctx context.Context, m types.Message) error {
	_, err := w.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(w.queueURL),
		ReceiptHandle: m.ReceiptHandle,
	})
	return err
}
