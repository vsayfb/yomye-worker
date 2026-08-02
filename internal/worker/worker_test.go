package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/vsayfb/gig-platform-categorization-service/internal/processor"
	"github.com/vsayfb/gig-platform-categorization-service/pkg/metrics"
)

// TestMain initializes the package-level OTel metric instruments once.
// processMessage's own deferred call to metrics.ObserveWorkerProcessing
// otherwise hits nil instruments (they default to their zero value until
// Register() runs, normally from app.go) and panics on every test. The
// default global MeterProvider is a no-op, so this doesn't need the rest of
// the telemetry stack to be running.
func TestMain(m *testing.M) {
	if err := metrics.Register(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// processorFunc adapts a plain function to processor.Processor, the same
// way http.HandlerFunc adapts a function to http.Handler.
type processorFunc func(ctx context.Context, msg processor.Message) error

func (f processorFunc) Process(ctx context.Context, msg processor.Message) error {
	return f(ctx, msg)
}

// fakeSQSClient is a minimal in-memory stand-in for sqsClient. The first
// ReceiveMessage call returns the configured messages; every call after
// that blocks until the context is canceled, mirroring how the real AWS SDK
// behaves on a long poll once ctx is done - this keeps Worker.Run's loop
// from spinning and lets tests control exactly when it exits.
type fakeSQSClient struct {
	mu       sync.Mutex
	messages []types.Message
	sent     bool

	deletedMu      sync.Mutex
	deletedHandles []string
}

func (f *fakeSQSClient) ReceiveMessage(
	ctx context.Context,
	_ *sqs.ReceiveMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.ReceiveMessageOutput, error) {
	f.mu.Lock()
	if !f.sent {
		f.sent = true
		msgs := f.messages
		f.mu.Unlock()
		return &sqs.ReceiveMessageOutput{Messages: msgs}, nil
	}
	f.mu.Unlock()

	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeSQSClient) DeleteMessage(
	_ context.Context,
	params *sqs.DeleteMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.DeleteMessageOutput, error) {
	f.deletedMu.Lock()
	f.deletedHandles = append(f.deletedHandles, aws.ToString(params.ReceiptHandle))
	f.deletedMu.Unlock()
	return &sqs.DeleteMessageOutput{}, nil
}

func (f *fakeSQSClient) deletedCount() int {
	f.deletedMu.Lock()
	defer f.deletedMu.Unlock()
	return len(f.deletedHandles)
}

func newTestMessage(id, msgType string) types.Message {
	return types.Message{
		MessageId:     aws.String(id),
		ReceiptHandle: aws.String("receipt-" + id),
		Body:          aws.String(`{}`),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"Type": {
				DataType:    aws.String("String"),
				StringValue: aws.String(msgType),
			},
		},
	}
}

func TestProcessMessage_PanicIsRecoveredAsError(t *testing.T) {
	w := &Worker{
		handlers: map[string]processor.Processor{
			"panics": processorFunc(func(context.Context, processor.Message) error {
				panic("boom")
			}),
		},
	}

	err := w.processMessage(context.Background(), newTestMessage("1", "panics"))
	if err == nil {
		t.Fatal("expected an error from a panicking handler, got nil")
	}
}

func TestHandleMessageSafely_PanicDoesNotEscape(t *testing.T) {
	// The outer safety net: even if something above processMessage's own
	// recover were to panic, handleMessageSafely must still not let it
	// escape and crash the caller. Simply completing this test proves that.
	w := &Worker{
		handlers: map[string]processor.Processor{
			"panics": processorFunc(func(context.Context, processor.Message) error {
				panic("boom")
			}),
		},
	}

	w.handleMessageSafely(context.Background(), newTestMessage("1", "panics"))
}

func TestProcessMessage_MissingType(t *testing.T) {
	w := &Worker{handlers: map[string]processor.Processor{}}

	m := newTestMessage("1", "")
	delete(m.MessageAttributes, "Type")

	err := w.processMessage(context.Background(), m)
	if err == nil {
		t.Fatal("expected an error for a message with no type")
	}
}

func TestProcessMessage_UnknownType(t *testing.T) {
	w := &Worker{handlers: map[string]processor.Processor{}}

	err := w.processMessage(context.Background(), newTestMessage("1", "SomethingUnregistered"))
	if err == nil {
		t.Fatal("expected an error for an unregistered message type")
	}
}

func TestProcessMessage_SuccessDeletesMessage(t *testing.T) {
	client := &fakeSQSClient{}
	w := &Worker{
		client:   client,
		queueURL: "test-queue",
		handlers: map[string]processor.Processor{
			"ok": processorFunc(func(context.Context, processor.Message) error {
				return nil
			}),
		},
	}

	if err := w.processMessage(context.Background(), newTestMessage("1", "ok")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.deletedCount(); got != 1 {
		t.Fatalf("expected the message to be deleted once, got %d deletes", got)
	}
}

func TestProcessMessage_HandlerErrorDoesNotDeleteMessage(t *testing.T) {
	client := &fakeSQSClient{}
	handlerErr := errors.New("processing failed")
	w := &Worker{
		client:   client,
		queueURL: "test-queue",
		handlers: map[string]processor.Processor{
			"fails": processorFunc(func(context.Context, processor.Message) error {
				return handlerErr
			}),
		},
	}

	err := w.processMessage(context.Background(), newTestMessage("1", "fails"))
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error to propagate, got %v", err)
	}
	if got := client.deletedCount(); got != 0 {
		t.Fatalf("expected the message to be left for redelivery, but it was deleted %d times", got)
	}
}

func TestReceiveBackoffIsExponentialAndCapped(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: time.Second},
		{failures: 1, want: time.Second},
		{failures: 2, want: 2 * time.Second},
		{failures: 3, want: 4 * time.Second},
		{failures: 5, want: 16 * time.Second},
		{failures: 6, want: 30 * time.Second},
		{failures: 100, want: 30 * time.Second},
	}

	for _, tt := range tests {
		if got := receiveBackoff(tt.failures); got != tt.want {
			t.Errorf("receiveBackoff(%d) = %s, want %s", tt.failures, got, tt.want)
		}
	}
}

func TestWaitForReceiveRetryStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	err := waitForReceiveRetry(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForReceiveRetry() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled retry wait took %s", elapsed)
	}
}

// TestRun_RespectsConcurrencyBound dispatches more messages than the
// configured concurrency and proves, via a handler that blocks until
// released, that no more than `concurrency` are ever in flight at once.
func TestRun_RespectsConcurrencyBound(t *testing.T) {
	const concurrency = 2
	const messageCount = 5

	messages := make([]types.Message, messageCount)
	for i := range messages {
		messages[i] = newTestMessage(fmt.Sprintf("m-%d", i), "slow")
	}

	client := &fakeSQSClient{messages: messages}

	var active int32
	var maxActive int32
	started := make(chan struct{}, messageCount)
	release := make(chan struct{})

	bumpMax := func(n int32) {
		for {
			cur := atomic.LoadInt32(&maxActive)
			if n <= cur || atomic.CompareAndSwapInt32(&maxActive, cur, n) {
				return
			}
		}
	}

	w := &Worker{
		client:      client,
		queueURL:    "test-queue",
		concurrency: concurrency,
		handlers: map[string]processor.Processor{
			"slow": processorFunc(func(context.Context, processor.Message) error {
				n := atomic.AddInt32(&active, 1)
				bumpMax(n)
				started <- struct{}{}
				<-release
				atomic.AddInt32(&active, -1)
				return nil
			}),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- w.Run(ctx) }()

	// Wait for exactly `concurrency` handlers to start.
	for i := 0; i < concurrency; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for handler %d to start", i)
		}
	}

	// Give any (incorrectly) over-dispatched goroutines a chance to start
	// before asserting the bound held.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&maxActive); got > concurrency {
		t.Fatalf("expected at most %d concurrent handlers, saw %d", concurrency, got)
	}

	close(release)

	for i := 0; i < messageCount-concurrency; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for remaining messages to process")
		}
	}

	cancel()

	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected Run to return context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after shutdown")
	}

	if got := atomic.LoadInt32(&maxActive); got > concurrency {
		t.Fatalf("expected at most %d concurrent handlers over the whole run, saw %d", concurrency, got)
	}
	if got := client.deletedCount(); got != messageCount {
		t.Fatalf("expected all %d messages to be deleted after successful processing, got %d", messageCount, got)
	}
}
