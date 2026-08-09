package notification

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type fakeSQSClient struct{}

func (fakeSQSClient) SendMessage(
	context.Context,
	*sqs.SendMessageInput,
	...func(*sqs.Options),
) (*sqs.SendMessageOutput, error) {
	return &sqs.SendMessageOutput{MessageId: aws.String("sqs-message-id")}, nil
}

func TestSQSPublisherClose(t *testing.T) {
	t.Parallel()

	publisher := &SQSPublisher{}
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPublishGigCategoryMatchedEmitsProducerSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	listingID := uuid.New()
	categoryID := uuid.New()
	event := GigCategoryMatchedEventEnvelope{
		Type:    EventGigCategoryMatched,
		EventID: StableEventID(EventGigCategoryMatched, listingID, categoryID),
		Version: CurrentEventVersion,
		Payload: GigCategoryMatchedEvent{
			GigID:      listingID,
			CategoryID: categoryID,
		},
	}
	publisher := &SQSPublisher{queueURL: "queue-url", client: fakeSQSClient{}}

	if err := publisher.PublishGigCategoryMatched(context.Background(), event); err != nil {
		t.Fatalf("PublishGigCategoryMatched() error = %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Name() != "sqs.publish gig_category_matched" {
		t.Fatalf("span name = %q", spans[0].Name())
	}
	if spans[0].SpanKind() != trace.SpanKindProducer {
		t.Fatalf("span kind = %v, want producer", spans[0].SpanKind())
	}
}
