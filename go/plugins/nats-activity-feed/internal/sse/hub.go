package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/feed"
)

const subBufferSize = 16

// Hub manages SSE subscriber connections and broadcasts FeedEvents.
// It maintains a ring buffer of recent events for new subscribers.
type Hub struct {
	mu         sync.RWMutex
	subs       map[chan feed.FeedEvent]struct{}
	ring       []feed.FeedEvent
	ringSize   int
	ringOffset int
	ringCount  int
}

// NewHub creates a Hub with the given ring buffer capacity.
func NewHub(bufferSize int) *Hub {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &Hub{
		subs:     make(map[chan feed.FeedEvent]struct{}),
		ring:     make([]feed.FeedEvent, bufferSize),
		ringSize: bufferSize,
	}
}

// Subscribe registers a new subscriber and returns a buffered channel.
func (h *Hub) Subscribe() chan feed.FeedEvent {
	ch := make(chan feed.FeedEvent, subBufferSize)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes the given subscriber channel.
func (h *Hub) Unsubscribe(ch chan feed.FeedEvent) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// Broadcast adds event to the ring buffer and fans out to all subscribers.
func (h *Hub) Broadcast(event feed.FeedEvent) {
	h.mu.Lock()
	// Add to ring buffer
	h.ring[h.ringOffset] = event
	h.ringOffset = (h.ringOffset + 1) % h.ringSize
	if h.ringCount < h.ringSize {
		h.ringCount++
	}

	clients := make([]chan feed.FeedEvent, 0, len(h.subs))
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

// snapshot returns the ring buffer contents in chronological order.
func (h *Hub) snapshot() []feed.FeedEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.ringCount == 0 {
		return nil
	}

	result := make([]feed.FeedEvent, 0, h.ringCount)
	start := 0
	if h.ringCount == h.ringSize {
		start = h.ringOffset // oldest element
	}
	for i := 0; i < h.ringCount; i++ {
		idx := (start + i) % h.ringSize
		result = append(result, h.ring[idx])
	}
	return result
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

	// Send ring buffer contents as initial burst
	events := h.snapshot()
	for _, event := range events {
		eventJSON, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: activity\ndata: %s\n\n", eventJSON)
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
			fmt.Fprintf(w, "event: activity\ndata: %s\n\n", eventJSON)
			flusher.Flush()
		}
	}
}
