package processor

import (
	"context"
	"testing"
)

func TestProfessionProcessorMarksInvalidMessagePermanent(t *testing.T) {
	processor := &ProfessionProcessor{}
	err := processor.Process(context.Background(), Message{
		ID:   "message-1",
		Type: "ProfessionPublished",
		Body: `{not-json}`,
	})

	if !IsPermanent(err) {
		t.Fatalf("invalid profession message should be permanent, got %v", err)
	}
}
