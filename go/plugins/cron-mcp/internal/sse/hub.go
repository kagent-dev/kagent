package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// subBufferSize is the channel buffer per subscriber.
const subBufferSize = 16

// Event represents an SSE event sent to clients.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub manages SSE subscriber connections and broadcasts events to all of them.
// It implements service.Broadcaster.
type Hub struct {
	mu       sync.RWMutex
	subs     map[chan Event]struct{}
	lastJSON []byte
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[chan Event]struct{}),
	}
}

// Subscribe registers a new subscriber and returns a buffered channel for events.
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

// Broadcast wraps data in a job_update Event, stores it as the latest snapshot,
// and non-blockingly delivers it to all current subscribers.
func (h *Hub) Broadcast(data interface{}) {
	event := Event{Type: "job_update", Data: data}

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
		default:
		}
	}
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
