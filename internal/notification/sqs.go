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

func (p *SQSPublisher) PublishApproved(ctx context.Context, event ApprovedEnvelope) error {

	body, err := json.Marshal(event)

	if err != nil {
		return fmt.Errorf("sqs: marshal event: %w", err)
	}

	inf, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
	})

	if err != nil {
		return fmt.Errorf("sqs: send message: %w", err)
	}

	slog.InfoContext(ctx, "sqs message sent for (gig):", "message", aws.ToString(inf.MessageId))

	return nil
}

func (p *SQSPublisher) PublishRejected(ctx context.Context, event RejectedEnvelope) error {

	body, err := json.Marshal(event)

	if err != nil {
		return fmt.Errorf("sqs: marshal event: %w", err)
	}

	inf, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
	})

	if err != nil {
		return fmt.Errorf("sqs: send message: %w", err)
	}

	slog.InfoContext(ctx, "sqs message sent for (gig):", "message", aws.ToString(inf.MessageId))

	return nil
}

func (p *SQSPublisher) PublishGigCategoryMatched(ctx context.Context, event GigCategoryMatchedEventEnvelope) error {

	body, err := json.Marshal(event)

	if err != nil {
		return fmt.Errorf("sqs: marshal event: %w", err)
	}

	inf, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
	})

	if err != nil {
		return fmt.Errorf("sqs: send message: %w", err)
	}

	slog.InfoContext(ctx, "sqs message sent for (gig_matched_subsribers):", "message", aws.ToString(inf.MessageId))

	return nil
}

func (p *SQSPublisher) Close() error {
	return p.Close()
}
