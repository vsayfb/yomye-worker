package worker

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"cloud.google.com/go/pubsub/v2"
	"github.com/vsayfb/gig-platform-categorization-service/internal/processor"
	"github.com/vsayfb/gig-platform-categorization-service/internal/runtimepolicy"
)

type PubSubWorker struct {
	client       *pubsub.Client
	subscription *pubsub.Subscriber
	handlers     map[string]processor.Processor
}

func NewPubSub(ctx context.Context, projectID, subscriptionName string, handlers map[string]processor.Processor, concurrency int) (*PubSubWorker, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("create Pub/Sub client: %w", err)
	}
	subscriber := client.Subscriber(subscriptionName)
	if concurrency <= 0 {
		concurrency = 1
	}
	subscriber.ReceiveSettings.MaxOutstandingMessages = concurrency
	subscriber.ReceiveSettings.NumGoroutines = 1
	return &PubSubWorker{client: client, subscription: subscriber, handlers: handlers}, nil
}

func (w *PubSubWorker) Run(ctx context.Context) error {
	return w.subscription.Receive(ctx, func(_ context.Context, message *pubsub.Message) {
		workCtx, cancel := context.WithTimeout(context.Background(), runtimepolicy.MessageProcessingTimeout)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(workCtx, "panic recovered while handling Pub/Sub message", "message_id", message.ID, "panic", recovered, "stack", string(debug.Stack()))
				message.Nack()
			}
		}()

		messageType := message.Attributes["Type"]
		if messageType == "" {
			messageType = message.Attributes["type"]
		}
		handler, ok := w.handlers[messageType]
		if !ok {
			slog.WarnContext(workCtx, "discarded permanently invalid Pub/Sub message", "message_id", message.ID, "message_type", messageType)
			message.Ack()
			return
		}
		err := handler.Process(workCtx, processor.Message{ID: message.ID, Body: string(message.Data), Type: messageType})
		if err == nil || processor.IsPermanent(err) {
			message.Ack()
			return
		}
		slog.ErrorContext(workCtx, "Pub/Sub message processing failed", "message_id", message.ID, "message_type", messageType, "err", err)
		message.Nack()
	})
}

func (w *PubSubWorker) Close() error { return w.client.Close() }
