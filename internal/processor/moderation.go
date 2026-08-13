package processor

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vsayfb/gig-platform-categorization-service/internal/notification"
)

func publishContentRejected(
	ctx context.Context,
	publisher notification.Publisher,
	recipientID uuid.UUID,
	subjectType notification.RejectedSubjectType,
	subjectID uuid.UUID,
	reason string,
) error {
	event := notification.ContentRejectedEnvelope{
		Type:    notification.EventContentRejected,
		EventID: notification.StableEventID(notification.EventContentRejected, subjectID),
		Version: notification.CurrentEventVersion,
		Payload: notification.ContentRejectedEvent{
			RecipientID: recipientID,
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Reason:      reason,
			OccurredAt:  time.Now().UTC(),
		},
	}
	if err := publisher.PublishContentRejected(ctx, event); err != nil {
		return fmt.Errorf("publish content-rejected event: %w", err)
	}
	return nil
}
