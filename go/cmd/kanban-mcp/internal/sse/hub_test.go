package sse

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	h := NewHub()
	ch1 := h.Subscribe()
	ch2 := h.Subscribe()
	ch3 := h.Subscribe()

	// Unsubscribe ch3 before broadcast.
	h.Unsubscribe(ch3)

	h.Broadcast("test")

	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != "board_update" {
				t.Errorf("subscriber %d: expected board_update, got %q", i+1, ev.Type)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("subscriber %d: timed out waiting for event", i+1)
		}
	}

	// ch3 must not receive anything.
	select {
	case ev := <-ch3:
		t.Errorf("unsubscribed channel received unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// expected: no event
	}
}

func TestHub_Broadcast_NonBlocking(t *testing.T) {
	h := NewHub()

	// Create and fill the slow subscriber's buffer completely.
	slow := h.Subscribe()
	for i := 0; i < subBufferSize; i++ {
		slow <- Event{Type: "prefill"}
	}

	fast := h.Subscribe()

	done := make(chan struct{})
	go func() {
		h.Broadcast("new-data")
		close(done)
	}()

	select {
	case <-done:
		// Good: Broadcast returned without blocking.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Broadcast blocked on a slow subscriber")
	}

	// The fast subscriber should still receive the event.
	select {
	case ev := <-fast:
		if ev.Type != "board_update" {
			t.Errorf("fast: expected board_update, got %q", ev.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("fast subscriber timed out")
	}
}

func TestHub_ConcurrentSubscribers(t *testing.T) {
	h := NewHub()
	const N = 50

	channels := make([]chan Event, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channels[i] = h.Subscribe()
		}(i)
	}
	wg.Wait()

	h.Broadcast("concurrent")

	for i, ch := range channels {
		select {
		case ev := <-ch:
			if ev.Type != "board_update" {
				t.Errorf("subscriber %d: expected board_update, got %q", i, ev.Type)
			}
		case <-time.After(500 * time.Millisecond):
			t.Errorf("subscriber %d timed out", i)
		}
	}
}

func TestServeSSE_Integration(t *testing.T) {
	h := NewHub()

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}

	lines := make(chan string, 200)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	// Wait for the initial snapshot event.
	gotSnapshot := false
	deadline := time.After(2 * time.Second)
	for !gotSnapshot {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "event: snapshot") {
				gotSnapshot = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for initial snapshot event")
		}
	}

	// Trigger a mutation broadcast.
	h.Broadcast(map[string]string{"title": "integration-test"})

	// Wait for the board_update data line.
	gotUpdate := false
	deadline2 := time.After(2 * time.Second)
	for !gotUpdate {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "data:") && strings.Contains(line, "board_update") {
				gotUpdate = true
			}
		case <-deadline2:
			t.Fatal("timed out waiting for board_update event")
		}
	}
}
