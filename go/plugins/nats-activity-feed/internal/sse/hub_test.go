package sse

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/feed"
)

func TestHub_Broadcast_Received(t *testing.T) {
	h := NewHub(10)
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	event := feed.FeedEvent{Agent: "a1", Type: "token", Data: "hello"}
	h.Broadcast(event)

	select {
	case got := <-ch:
		if got.Agent != "a1" {
			t.Errorf("Agent = %q, want %q", got.Agent, "a1")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestHub_Broadcast_NonBlocking(t *testing.T) {
	h := NewHub(10)
	slow := h.Subscribe()

	// Fill slow subscriber's buffer
	for i := 0; i < subBufferSize; i++ {
		slow <- feed.FeedEvent{Data: "fill"}
	}

	done := make(chan struct{})
	go func() {
		h.Broadcast(feed.FeedEvent{Data: "new"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Broadcast blocked on full subscriber")
	}
}

func TestHub_RingBuffer(t *testing.T) {
	h := NewHub(3)

	h.Broadcast(feed.FeedEvent{Data: "1"})
	h.Broadcast(feed.FeedEvent{Data: "2"})
	h.Broadcast(feed.FeedEvent{Data: "3"})
	h.Broadcast(feed.FeedEvent{Data: "4"}) // overwrites "1"

	snap := h.snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap))
	}
	if snap[0].Data != "2" {
		t.Errorf("snap[0].Data = %q, want %q", snap[0].Data, "2")
	}
	if snap[2].Data != "4" {
		t.Errorf("snap[2].Data = %q, want %q", snap[2].Data, "4")
	}
}

func TestHub_ServeSSE_InitialBurst(t *testing.T) {
	h := NewHub(10)
	h.Broadcast(feed.FeedEvent{Agent: "a1", Type: "token", Data: "first"})
	h.Broadcast(feed.FeedEvent{Agent: "a2", Type: "error", Data: "second"})

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var events []feed.FeedEvent

	// Read initial burst events (within timeout)
	done := time.After(2 * time.Second)
	for {
		select {
		case <-done:
			goto check
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var fe feed.FeedEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &fe); err == nil {
				events = append(events, fe)
				if len(events) >= 2 {
					goto check
				}
			}
		}
	}

check:
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least 2", len(events))
	}
	if events[0].Data != "first" {
		t.Errorf("events[0].Data = %q, want %q", events[0].Data, "first")
	}
	if events[1].Data != "second" {
		t.Errorf("events[1].Data = %q, want %q", events[1].Data, "second")
	}
}
