package events

import (
	"testing"
	"time"
)

func TestHubDelivers(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	defer cancel()
	h.Publish(Message{Type: "pools.changed"})
	select {
	case m := <-ch:
		if m.Type != "pools.changed" {
			t.Fatal(m)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe() // liest nie
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish(Message{Type: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked by slow subscriber")
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed")
	}
	h.Publish(Message{Type: "x"}) // darf nicht paniken
}
