package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
)

const subBufferSize = 16

// Event represents an SSE event sent to clients.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub manages SSE subscriber connections and polls Temporal for workflow updates.
type Hub struct {
	tc       temporal.WorkflowClient
	interval time.Duration

	mu       sync.RWMutex
	subs     map[chan Event]struct{}
	lastJSON []byte
}

// NewHub creates a Hub that polls the given Temporal client at the specified interval.
func NewHub(tc temporal.WorkflowClient, interval time.Duration) *Hub {
	return &Hub{
		tc:       tc,
		interval: interval,
		subs:     make(map[chan Event]struct{}),
	}
}

// Start begins the background polling loop. It blocks until ctx is canceled.
func (h *Hub) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Initial poll
	h.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.poll(ctx)
		}
	}
}

func (h *Hub) poll(ctx context.Context) {
	workflows, err := h.tc.ListWorkflows(ctx, temporal.WorkflowFilter{PageSize: 100})
	if err != nil {
		log.Printf("SSE poll error: %v", err)
		return
	}

	h.Broadcast(Event{
		Type: "workflow_update",
		Data: map[string]interface{}{
			"workflows": workflows,
		},
	})
}

// Subscribe registers a new subscriber.
func (h *Hub) Subscribe() chan Event {
	ch := make(chan Event, subBufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes the given subscriber channel.
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// Broadcast sends an event to all connected subscribers and stores it as the latest snapshot.
func (h *Hub) Broadcast(event Event) {
	eventJSON, err := json.Marshal(event)

	h.mu.Lock()
	if err == nil {
		h.lastJSON = eventJSON
	}
	clients := make([]chan Event, 0, len(h.subs))
	for ch := range h.subs {
		clients = append(clients, ch)
	}
	h.mu.Unlock()

	for _, ch := range clients {
		select {
		case ch <- event:
		default: // drop for slow subscribers
		}
	}
}

// RunningCount returns the count of running workflows from the last poll.
func (h *Hub) RunningCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.lastJSON == nil {
		return 0
	}

	var event Event
	if err := json.Unmarshal(h.lastJSON, &event); err != nil {
		return 0
	}

	dataMap, ok := event.Data.(map[string]interface{})
	if !ok {
		return 0
	}

	workflows, ok := dataMap["workflows"].([]interface{})
	if !ok {
		return 0
	}

	count := 0
	for _, w := range workflows {
		wf, ok := w.(map[string]interface{})
		if ok && wf["Status"] == "Running" {
			count++
		}
	}
	return count
}

// ServeSSE handles the /events SSE endpoint.
func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	// Send initial snapshot
	h.mu.RLock()
	lastJSON := h.lastJSON
	h.mu.RUnlock()

	if lastJSON != nil {
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", lastJSON)
	} else {
		fmt.Fprintf(w, "event: snapshot\ndata: {}\n\n")
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			eventJSON, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", eventJSON)
			flusher.Flush()
		}
	}
}
