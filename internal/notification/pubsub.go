package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
)

type PubSubPublisher struct {
	client    *pubsub.Client
	publisher *pubsub.Publisher
}

func NewPubSubPublisher(ctx context.Context, projectID, topicName string) (*PubSubPublisher, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("create Pub/Sub client: %w", err)
	}
	return &PubSubPublisher{client: client, publisher: client.Publisher(topicName)}, nil
}

func (p *PubSubPublisher) publish(ctx context.Context, event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal Pub/Sub notification: %w", err)
	}
	if _, err := p.publisher.Publish(ctx, &pubsub.Message{Data: body}).Get(ctx); err != nil {
		return fmt.Errorf("publish Pub/Sub notification: %w", err)
	}
	return nil
}

func (p *PubSubPublisher) PublishListingApproved(ctx context.Context, event ListingApprovedEnvelope) error {
	return p.publish(ctx, event)
}

func (p *PubSubPublisher) PublishGigCategoryMatched(ctx context.Context, event GigCategoryMatchedEventEnvelope) error {
	return p.publish(ctx, event)
}

func (p *PubSubPublisher) PublishContentRejected(ctx context.Context, event ContentRejectedEnvelope) error {
	return p.publish(ctx, event)
}

func (p *PubSubPublisher) Close() error {
	p.publisher.Stop()
	return p.client.Close()
}
