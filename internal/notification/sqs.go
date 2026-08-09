package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/vsayfb/gig-platform-categorization-service/internal/awsclient"
	"github.com/vsayfb/gig-platform-categorization-service/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var notificationTracer = otel.Tracer("categorization/notification")

type sqsClient interface {
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type SQSPublisher struct {
	queueURL string
	client   sqsClient
}

func NewNotificationPublisher(ctx context.Context, cfg *config.Config) (*SQSPublisher, error) {

	awsCfg, err := awsclient.New(ctx, cfg)

	if err != nil {
		return nil, fmt.Errorf("initialize aws client: %w", err)
	}

	sqs := awsclient.NewSQS(awsCfg, cfg)

	return &SQSPublisher{
		queueURL: cfg.AWS.NotificationEventsQueue,
		client:   sqs,
	}, nil
}

func (p *SQSPublisher) PublishListingApproved(
	ctx context.Context,
	event ListingApprovedEnvelope,
) (resultErr error) {
	ctx, span := notificationTracer.Start(
		ctx,
		"sqs.publish listing_approved",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "aws_sqs"),
			attribute.String("messaging.operation.name", "publish"),
			attribute.String("messaging.message.type", string(event.Type)),
			attribute.String("messaging.message.id", event.EventID.String()),
			attribute.Int("messaging.message.version", event.Version),
			attribute.String("listing.id", event.Payload.ListingID.String()),
		),
	)
	defer func() { finishNotificationSpan(span, resultErr) }()

	messageID, err := p.publish(ctx, event)
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.String("messaging.sqs.message_id", messageID))

	slog.InfoContext(ctx,
		"listing-approved event published",
		"message_id", messageID,
		"event_id", event.EventID,
		"event_version", event.Version,
		"listing_id", event.Payload.ListingID,
	)

	return nil
}

func (p *SQSPublisher) PublishGigCategoryMatched(ctx context.Context, event GigCategoryMatchedEventEnvelope) error {
	ctx, span := notificationTracer.Start(
		ctx,
		"sqs.publish gig_category_matched",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "aws_sqs"),
			attribute.String("messaging.operation.name", "publish"),
			attribute.String("messaging.message.type", string(event.Type)),
			attribute.String("messaging.message.id", event.EventID.String()),
			attribute.Int("messaging.message.version", event.Version),
			attribute.String("listing.id", event.Payload.GigID.String()),
			attribute.String("category.id", event.Payload.CategoryID.String()),
		),
	)
	var resultErr error
	defer func() { finishNotificationSpan(span, resultErr) }()

	messageID, err := p.publish(ctx, event)
	if err != nil {
		resultErr = err
		return resultErr
	}
	span.SetAttributes(attribute.String("messaging.sqs.message_id", messageID))

	slog.InfoContext(
		ctx,
		"gig-category-matched event published",
		"message_id", messageID,
		"event_id", event.EventID,
		"event_version", event.Version,
		"gig_id", event.Payload.GigID,
		"category_id", event.Payload.CategoryID,
	)

	return nil
}

func finishNotificationSpan(span trace.Span, err error) {
	defer span.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetStatus(codes.Ok, "event published")
}

func (p *SQSPublisher) publish(ctx context.Context, event any) (string, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("sqs: marshal event: %w", err)
	}

	info, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return "", fmt.Errorf("sqs: send message: %w", err)
	}

	return aws.ToString(info.MessageId), nil
}

func (p *SQSPublisher) Close() error {
	// AWS SDK service clients do not own a closeable connection pool. Their
	// underlying HTTP transport is shared and cleaned up by the process.
	return nil
}
