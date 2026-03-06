package sse_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
)

type mockTC struct {
	workflows []*temporal.WorkflowSummary
	listErr   error
}

func (m *mockTC) ListWorkflows(_ context.Context, _ temporal.WorkflowFilter) ([]*temporal.WorkflowSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.workflows == nil {
		return []*temporal.WorkflowSummary{}, nil
	}
	return m.workflows, nil
}

func (m *mockTC) GetWorkflow(_ context.Context, id string) (*temporal.WorkflowDetail, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockTC) CancelWorkflow(_ context.Context, _ string) error { return nil }

func (m *mockTC) SignalWorkflow(_ context.Context, _, _ string, _ interface{}) error { return nil }

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	tc := &mockTC{}
	h := sse.NewHub(tc, time.Minute)
	ch1 := h.Subscribe()
	ch2 := h.Subscribe()
	ch3 := h.Subscribe()

	h.Unsubscribe(ch3)
	h.Broadcast(sse.Event{Type: "test", Data: "hello"})

	for i, ch := range []chan sse.Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != "test" {
				t.Errorf("subscriber %d: expected test, got %q", i+1, ev.Type)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("subscriber %d: timed out", i+1)
		}
	}

	select {
	case ev := <-ch3:
		t.Errorf("unsubscribed channel received: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHub_ConcurrentSubscribers(t *testing.T) {
	tc := &mockTC{}
	h := sse.NewHub(tc, time.Minute)
	const N = 50

	channels := make([]chan sse.Event, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channels[i] = h.Subscribe()
		}(i)
	}
	wg.Wait()

	h.Broadcast(sse.Event{Type: "concurrent", Data: "test"})

	for i, ch := range channels {
		select {
		case ev := <-ch:
			if ev.Type != "concurrent" {
				t.Errorf("subscriber %d: expected concurrent, got %q", i, ev.Type)
			}
		case <-time.After(500 * time.Millisecond):
			t.Errorf("subscriber %d timed out", i)
		}
	}
}

func TestServeSSE_Integration(t *testing.T) {
	now := time.Now()
	tc := &mockTC{
		workflows: []*temporal.WorkflowSummary{
			{WorkflowID: "wf-1", Status: "Running", StartTime: now},
		},
	}
	h := sse.NewHub(tc, time.Minute)

	// Pre-broadcast so there's a snapshot
	h.Broadcast(sse.Event{Type: "workflow_update", Data: map[string]interface{}{"workflows": tc.workflows}})

	srv := httptest.NewServer(http.HandlerFunc(h.ServeSSE))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
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

	gotSnapshot := false
	deadline := time.After(2 * time.Second)
	for !gotSnapshot {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "event: snapshot") {
				gotSnapshot = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for snapshot event")
		}
	}

	// Trigger another broadcast
	h.Broadcast(sse.Event{Type: "workflow_update", Data: map[string]interface{}{"test": true}})

	gotUpdate := false
	deadline2 := time.After(2 * time.Second)
	for !gotUpdate {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "data:") && strings.Contains(line, "workflow_update") {
				gotUpdate = true
			}
		case <-deadline2:
			t.Fatal("timed out waiting for workflow_update event")
		}
	}
}

func TestHub_Start_Polls(t *testing.T) {
	tc := &mockTC{
		workflows: []*temporal.WorkflowSummary{
			{WorkflowID: "wf-1", Status: "Running", StartTime: time.Now()},
		},
	}
	h := sse.NewHub(tc, 50*time.Millisecond)

	ch := h.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	go h.Start(ctx)

	// Wait for at least one broadcast from polling
	select {
	case ev := <-ch:
		if ev.Type != "workflow_update" {
			t.Errorf("expected workflow_update, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for poll broadcast")
	}

	cancel()
}
