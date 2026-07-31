package notification

import "testing"

func TestSQSPublisherClose(t *testing.T) {
	t.Parallel()

	publisher := &SQSPublisher{}
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
