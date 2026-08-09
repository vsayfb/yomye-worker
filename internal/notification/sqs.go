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
)

type SQSPublisher struct {
	queueURL string
	client   *sqs.Client
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
) error {
	messageID, err := p.publish(ctx, event)
	if err != nil {
		return err
	}

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
	messageID, err := p.publish(ctx, event)
	if err != nil {
		return err
	}

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
