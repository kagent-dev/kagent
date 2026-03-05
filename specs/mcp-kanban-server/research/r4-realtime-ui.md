# R4: Real-time UI Embedding in Go

## Recommendation: SSE (Server-Sent Events)

**Use SSE. No new dependencies. Kagent middleware already supports it.**

### SSE vs WebSocket for a Kanban Board

| | SSE | WebSocket |
|--|-----|-----------|
| Dependencies | None (stdlib only) | gorilla/websocket or nhooyr.io |
| Direction | Server → Client | Bidirectional |
| Browser API | `EventSource` (auto-reconnect) | `WebSocket` (manual reconnect) |
| Proxy/K8s friendliness | Excellent | Requires config in some setups |
| Fit for Kanban | ✅ Perfect | Overkill |

Mutations (create/move/delete task) happen via REST calls. Browser only needs push notifications.

## Embed Pattern

```go
//go:embed all:ui
var uiFiles embed.FS

uiFS, _ := fs.Sub(uiFiles, "ui")
mux.Handle("/", http.FileServer(http.FS(uiFS)))
```

## SSE Hub Pattern

```go
type Hub struct {
    mu   sync.RWMutex
    subs map[chan []byte]struct{}
}

func (h *Hub) Broadcast(msg []byte) {
    h.mu.RLock(); defer h.mu.RUnlock()
    for ch := range h.subs {
        select { case ch <- msg: default: } // non-blocking drop
    }
}

func (h *Hub) SSEHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("X-Accel-Buffering", "no")

    flusher := w.(http.Flusher)
    ch := make(chan []byte, 8)
    h.subscribe(ch); defer h.unsubscribe(ch)

    // send initial snapshot
    snapshot, _ := json.Marshal(currentBoard)
    fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", snapshot)
    flusher.Flush()

    for {
        select {
        case <-r.Context().Done(): return
        case msg := <-ch:
            fmt.Fprintf(w, "data: %s\n\n", msg)
            flusher.Flush()
        }
    }
}
```

## Browser JS (no framework)
```javascript
const es = new EventSource('/events');
es.addEventListener('snapshot', e => renderBoard(JSON.parse(e.data)));
es.onmessage = e => applyUpdate(JSON.parse(e.data));
```

## Single Port Layout
```
:8080
  /mcp         → MCP Streamable HTTP handler
  /events      → SSE push endpoint
  /api/tasks   → REST CRUD
  /api/boards  → REST CRUD
  /            → Embedded HTML+JS SPA
```
