package sse_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// compile-time assertion that *Hub satisfies the service.Broadcaster contract it
// is wired into in main.go.
var _ service.Broadcaster = (*sse.Hub)(nil)

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	h := sse.NewHub(nil)

	a := h.Subscribe("default")
	b := h.Subscribe("default")
	c := h.Subscribe("default")

	// Unsubscribe one client; it must not receive the broadcast.
	h.Unsubscribe(b)

	h.Broadcast("default", "payload")

	for name, ch := range map[string]chan sse.Event{"a": a, "b": b, "c": c} {
		if name == "b" {
			// b is closed; a receive must observe the closed channel (no event).
			select {
			case ev, ok := <-ch:
				if ok {
					t.Errorf("unsubscribed client %s received event %+v", name, ev)
				}
			default:
				t.Errorf("unsubscribed client %s channel should be closed", name)
			}
			continue
		}
		select {
		case ev := <-ch:
			if ev.Type != "board_update" {
				t.Errorf("client %s: got type %q, want board_update", name, ev.Type)
			}
			if ev.Data != "payload" {
				t.Errorf("client %s: got data %v, want payload", name, ev.Data)
			}
		default:
			t.Errorf("client %s did not receive event", name)
		}
	}
}

func TestHub_Unsubscribe_Idempotent(t *testing.T) {
	h := sse.NewHub(nil)
	ch := h.Subscribe("default")
	h.Unsubscribe(ch)
	// Second unsubscribe must not panic (would double-close otherwise).
	h.Unsubscribe(ch)
}

func TestHub_Broadcast_NonBlocking(t *testing.T) {
	h := sse.NewHub(nil)

	// A slow subscriber that never drains its channel.
	slow := h.Subscribe("default")
	fast := h.Subscribe("default")

	// Overfill the slow subscriber's buffer well beyond capacity. If Broadcast
	// blocked on a full buffer this would deadlock the test.
	done := make(chan struct{})
	go func() {
		for i := range 1000 {
			h.Broadcast("default", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Broadcast blocked on a slow subscriber")
	}

	// The fast subscriber should still have received at least one event (buffered).
	select {
	case <-fast:
	default:
		t.Error("fast subscriber received no events")
	}

	_ = slow
}

func TestHub_ConcurrentSubscribers(t *testing.T) {
	h := sse.NewHub(nil)

	const n = 50
	chans := make([]chan sse.Event, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			chans[i] = h.Subscribe("default")
		}(i)
	}
	wg.Wait()

	h.Broadcast("default", "hello")

	for i, ch := range chans {
		select {
		case ev := <-ch:
			if ev.Data != "hello" {
				t.Errorf("subscriber %d: got %v, want hello", i, ev.Data)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d did not receive event", i)
		}
	}
}

func TestServeSSE_Snapshot(t *testing.T) {
	snapshot := map[string]string{"hello": "world"}
	h := sse.NewHub(func(string) any { return snapshot })

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	ctx := t.Context()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	r := bufio.NewReader(resp.Body)
	gotSnapshot := readEvent(t, r)
	if !strings.Contains(gotSnapshot, "event: snapshot") {
		t.Errorf("first event missing snapshot header: %q", gotSnapshot)
	}
	if !strings.Contains(gotSnapshot, `"hello":"world"`) {
		t.Errorf("snapshot missing data: %q", gotSnapshot)
	}
}

func TestServeSSE_Broadcast(t *testing.T) {
	h := sse.NewHub(nil)

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	ctx := t.Context()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// Wait until the server has registered the subscriber before broadcasting.
	deadline := time.Now().Add(2 * time.Second)
	for h.SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.Broadcast("default", map[string]string{"k": "v"})

	r := bufio.NewReader(resp.Body)
	got := readEvent(t, r)
	if !strings.Contains(got, "event: board_update") {
		t.Errorf("event missing board_update type: %q", got)
	}
	if !strings.Contains(got, `"k":"v"`) {
		t.Errorf("event missing data: %q", got)
	}
}

func TestServeSSE_ClientDisconnect(t *testing.T) {
	h := sse.NewHub(nil)

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for h.SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Disconnect the client; the handler must Unsubscribe via r.Context().Done().
	cancel()
	resp.Body.Close()

	deadline = time.Now().Add(2 * time.Second)
	for h.SubscriberCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber not removed after client disconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readEvent reads a single SSE event (terminated by a blank line) from r.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	type result struct {
		s   string
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			b.WriteString(line)
			if err != nil {
				resCh <- result{b.String(), err}
				return
			}
			if line == "\n" {
				resCh <- result{b.String(), nil}
				return
			}
		}
	}()
	select {
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("reading event: %v (got %q)", res.err, res.s)
		}
		return res.s
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading SSE event")
		return ""
	}
}
