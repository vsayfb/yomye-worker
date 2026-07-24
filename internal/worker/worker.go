package worker

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/vsayfb/gig-platform-categorization-service/internal/awsclient"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

type Worker struct {
	client    *sqs.Client
	processor Processor
}

func New(ctx context.Context, cfg *config.Config, p Processor) (*Worker, error) {

	awsCfg, err := awsclient.New(ctx, cfg)

	if err != nil {
		slog.Error("failed to initialize aws client", "err", err)
		os.Exit(1)
	}

	sqs := awsclient.NewSQS(awsCfg, cfg)

	return &Worker{
		client:    sqs,
		processor: p,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {

	queueUrl := aws.String(w.processor.CategorizationSQSQueue())

	slog.Info("worker is entering the loop for", "queue", *queueUrl)

	for {

		resp, err := w.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            queueUrl,
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   60,
			MessageAttributeNames: []string{
				"traceparent", "tracestate",
			},
		})

		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			slog.ErrorContext(ctx, "receive failed", "err", err)

			continue
		}

		for _, m := range resp.Messages {
			mCtx := tracing.ExtractTraceContext(ctx, m.MessageAttributes)

			mCtx, span := otel.Tracer("worker").Start(mCtx, "categorize-message")

			if err := w.processMessage(mCtx, m); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				slog.ErrorContext(mCtx, "processing failed", "err", err)
			} else {
				span.SetStatus(codes.Ok, "processing succeed")
			}

			span.End()
		}
	}
}

func (w *Worker) processMessage(ctx context.Context, m types.Message) (err error) {
	start := time.Now()

	defer func() {
		metrics.ObserveWorkerProcessing(ctx, start, err)
	}()

	msg := Message{
		ID:   aws.ToString(m.MessageId),
		Body: aws.ToString(m.Body),
	}

	if err = w.processor.Process(ctx, msg); err != nil {
		slog.ErrorContext(ctx,
			"processor returned error",
			"message_id", msg.ID,
			"err", err,
		)
		return err
	}

	slog.InfoContext(ctx, "processor succeeded", "message_id", msg.ID)

	_, err = w.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(w.processor.CategorizationSQSQueue()),
		ReceiptHandle: m.ReceiptHandle,
	})

	if err != nil {
		slog.WarnContext(ctx, "failed to delete message", "err", err)
	}

	return err
}
