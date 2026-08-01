package events

import (
	"testing"
	"time"

	"mobius/internal/config"
)

func newTestPipeline() *EventPipeline {
	return NewEventPipeline(nil, nil, config.EventConfig{})
}

// Subscribers must see published events live — this is what drives the
// WebSocket push that replaced frontend polling (plan 7.3).
func TestSubscribe_ReceivesPublishedEvents(t *testing.T) {
	ep := newTestPipeline()
	sub, cancel := ep.Subscribe()
	defer cancel()

	ep.Publish(&Event{EventType: "task_ready"})

	select {
	case evt := <-sub:
		if evt.EventType != "task_ready" {
			t.Fatalf("got event type %q, want task_ready", evt.EventType)
		}
		if evt.ID == "" || evt.Timestamp.IsZero() {
			t.Fatalf("Publish should stamp ID and timestamp, got %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive published event")
	}
}

// Cancel must detach the subscriber (channel closed, no further delivery) so
// a departed WebSocket client doesn't leak.
func TestSubscribe_CancelDetaches(t *testing.T) {
	ep := newTestPipeline()
	sub, cancel := ep.Subscribe()
	cancel()
	cancel() // idempotent — a double cancel must not panic

	ep.Publish(&Event{EventType: "task_ready"})

	if _, ok := <-sub; ok {
		t.Fatal("expected closed channel after cancel, got a delivered event")
	}
}

// A full queue must be accounted for, not silently dropped (plan 7.6): the
// counter feeds the /metrics event-drop gauge.
func TestPublish_CountsDroppedEvents(t *testing.T) {
	ep := NewEventPipeline(nil, nil, config.EventConfig{BufferSize: 1})

	// Nothing drains the queue (Start not called): the second publish drops.
	ep.Publish(&Event{EventType: "a"})
	ep.Publish(&Event{EventType: "b"})
	ep.Publish(&Event{EventType: "c"})

	if got := ep.Dropped(); got != 2 {
		t.Fatalf("Dropped() = %d, want 2", got)
	}
}

// A subscriber that never drains must not block Publish — events are dropped
// for that subscriber instead (the UI refresh it drives is idempotent).
func TestPublish_SlowSubscriberDoesNotBlock(t *testing.T) {
	ep := newTestPipeline()
	_, cancel := ep.Subscribe() // never read from
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < subscriberBuffer*2; i++ {
			ep.Publish(&Event{EventType: "spam"})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
