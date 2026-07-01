// Package sse implements a minimal Server-Sent Events hub used to push the full
// kanban board state to connected browsers after every mutation. It depends only
// on the standard library: the board is server-to-client push only, so SSE is
// sufficient and proxy-friendly without any additional dependency.
//
// Subscriptions are board-scoped: each client connects to /events?board={key} and
// only receives events for that board. Broadcast(boardKey, data) delivers to the
// subscribers of that board.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// subBuffer is the per-subscriber channel buffer. A small buffer absorbs short
// bursts; once full, Broadcast drops the event for that subscriber rather than
// blocking the publisher (see Broadcast).
const subBuffer = 8

// defaultBoardKey is used when a client connects without a ?board= query param.
const defaultBoardKey = "default"

// Event is a single SSE message. Type is always "board_update" in v1; Data is the
// JSON-serializable payload (a single board's state).
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// SnapshotFunc returns the current state of the named board, used to send an
// initial snapshot to a client when it first connects. It may be nil, in which
// case no snapshot is sent.
type SnapshotFunc func(board string) any

// Hub fans out events to all subscribed SSE clients, scoped per board. It is safe
// for concurrent use by multiple goroutines. Subscribers are keyed by their event
// channel; the map value is the board key the subscriber is interested in.
type Hub struct {
	mu       sync.RWMutex
	subs     map[chan Event]string
	snapshot SnapshotFunc
}

// NewHub constructs an empty Hub. The optional snapshot function provides the
// board state sent to each client on connect; pass nil to disable snapshots.
func NewHub(snapshot SnapshotFunc) *Hub {
	return &Hub{
		subs:     make(map[chan Event]string),
		snapshot: snapshot,
	}
}

// Subscribe registers a new subscriber for the given board (empty = "default")
// and returns its event channel. The caller must call Unsubscribe with the same
// channel when done.
func (h *Hub) Subscribe(board string) chan Event {
	if board == "" {
		board = defaultBoardKey
	}
	ch := make(chan Event, subBuffer)
	h.mu.Lock()
	h.subs[ch] = board
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the hub and closes it. It is safe to call multiple
// times: a channel that is not registered is ignored.
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// SubscriberCount reports the number of currently connected subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Broadcast sends a "board_update" event carrying data to every subscriber of
// boardKey. It is non-blocking: if a subscriber's buffer is full (a slow client),
// the event is dropped for that subscriber so one slow client cannot stall the
// others.
//
// Broadcast implements service.Broadcaster.
func (h *Hub) Broadcast(boardKey string, data any) {
	if boardKey == "" {
		boardKey = defaultBoardKey
	}
	event := Event{Type: "board_update", Data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch, board := range h.subs {
		if board != boardKey {
			continue
		}
		select {
		case ch <- event:
		default:
			// Subscriber buffer full: drop to stay non-blocking.
		}
	}
}

// ServeSSE handles an SSE connection: it sets the streaming headers, sends an
// initial snapshot (if a snapshot function is configured) for the requested
// board, then streams that board's events until the client disconnects or the hub
// closes the channel. The board is taken from the ?board= query param (default
// "default").
func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	board := r.URL.Query().Get("board")
	if board == "" {
		board = defaultBoardKey
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := h.Subscribe(board)
	defer h.Unsubscribe(ch)

	// Flush the response headers immediately so the client's request returns and
	// the stream is established before any event is sent. Without this an HTTP
	// client blocks in Do() until the first byte is written.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Initial snapshot so a freshly connected client renders immediately and so a
	// reconnecting client recovers state without a separate fetch.
	if h.snapshot != nil {
		if data, err := json.Marshal(h.snapshot(board)); err == nil {
			fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				// Hub unsubscribed/closed this channel.
				return
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}
