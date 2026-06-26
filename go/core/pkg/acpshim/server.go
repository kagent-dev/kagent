package acpshim

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket close codes (4000-4999 is the application-defined range) used so
// the bridge can distinguish why the stream ended.
const (
	// CloseChildExited signals the child agent exited cleanly (status 0).
	CloseChildExited = 4000
	// CloseChildFailed signals the child agent exited with an error.
	CloseChildFailed = 4001
)

// preemptTimeout bounds how long a newly arriving client waits for the
// connection it is preempting to release the single-client slot before the
// shim gives up and drops the newcomer.
const preemptTimeout = 10 * time.Second

// Server is the shim's WebSocket server. It accepts at most one client at a
// time (the kagent bridge owns the stream) and pumps frames between the
// client and the child agent's stdio.
type Server struct {
	cfg      *Config
	upgrader websocket.Upgrader
	httpSrv  *http.Server

	mu         sync.Mutex
	child      *child
	activeConn *websocket.Conn
	released   chan struct{}
	graceTimer *time.Timer
}

// NewServer creates a Server from a validated Config.
func NewServer(cfg *Config) *Server {
	s := &Server{
		cfg: cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  64 * 1024,
			WriteBufferSize: 64 * 1024,
			// Browser clients (e.g. the kagent UI) connect cross-origin. The
			// shim is reached only through the controller's same-origin proxy
			// over the actor's private atenet ingress, so Origin checks add no
			// protection.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
	mux := http.NewServeMux()
	// Alias used by infrastructure probes (e.g. kagent's substrate actor
	// reachability check hits /health through atenet-router).
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/acp", s.handleACP)
	s.httpSrv = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe runs the server until Shutdown is called.
func (s *Server) ListenAndServe() error {
	l, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.cfg.ListenAddr, err)
	}
	return s.Serve(l)
}

// Serve runs the server on the given listener (used by tests to bind an
// ephemeral port).
func (s *Server) Serve(l net.Listener) error {
	log.Printf("acp-shim: listening on %s", l.Addr())
	err := s.httpSrv.Serve(l)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown stops the HTTP server and terminates any running child.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpSrv.Shutdown(ctx)
	s.mu.Lock()
	c := s.child
	s.child = nil
	s.stopGraceTimerLocked()
	s.mu.Unlock()
	if c != nil {
		c.terminate(s.cfg.GracePeriod)
	}
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleACP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("acp-shim: websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Single-client slot: make this connection the active client, preempting
	// any stale incumbent. The bridge is a single browser that reconnects on
	// every refresh, so the newcomer must win rather than be rejected behind a
	// half-open connection (see takeSlot).
	if !s.takeSlot(conn) {
		return
	}

	c, err := s.acquireChild()
	if err != nil {
		log.Printf("acp-shim: failed to start child: %v", err)
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseChildFailed, "failed to start agent"),
			time.Now().Add(5*time.Second))
		s.releaseSlot()
		return
	}

	log.Printf("acp-shim: client connected from %s", r.RemoteAddr)
	s.pump(conn, c)
	s.releaseSlot()
	s.releaseChild(c)
	log.Printf("acp-shim: client %s disconnected", r.RemoteAddr)
}

// takeSlot makes conn the single active client, preempting any incumbent.
// Because the bridge is a single browser that reconnects on every refresh,
// the newcomer must win rather than be rejected behind a stale (possibly
// half-open) connection: it closes the incumbent's connection and waits for
// it to release the slot. The connection is published as the active client
// before this returns, so a later newcomer can always find and preempt it
// without polling. Returns false if the incumbent does not release within
// preemptTimeout, in which case the caller drops the new connection.
func (s *Server) takeSlot(conn *websocket.Conn) bool {
	s.mu.Lock()
	for s.activeConn != nil {
		old := s.activeConn
		released := s.released
		s.mu.Unlock()

		log.Printf("acp-shim: preempting stale client to admit a new connection")
		_ = old.Close()
		select {
		case <-released:
		case <-time.After(preemptTimeout):
			log.Printf("acp-shim: previous client did not release in time, dropping new connection")
			return false
		}
		s.mu.Lock()
	}
	s.activeConn = conn
	s.released = make(chan struct{})
	s.stopGraceTimerLocked()
	s.mu.Unlock()
	return true
}

// releaseSlot frees the single-client slot and wakes any client waiting to
// preempt this connection.
func (s *Server) releaseSlot() {
	s.mu.Lock()
	s.activeConn = nil
	if s.released != nil {
		close(s.released)
		s.released = nil
	}
	s.mu.Unlock()
}

// acquireChild returns the long-lived child process for a new connection,
// reusing the running one across reconnects and starting a fresh one only
// when none is alive.
func (s *Server) acquireChild() (*child, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.child != nil && !s.child.exited() {
		return s.child, nil
	}
	c, err := startChild(s.cfg)
	if err != nil {
		return nil, err
	}
	s.child = c
	return c, nil
}

// releaseChild keeps the child alive after a connection ends so the next
// client can resume its in-memory sessions. When a reconnect grace window is
// configured, the child is terminated if no new client arrives within it.
func (s *Server) releaseChild(c *child) {
	if s.cfg.ReconnectGrace <= 0 || c.exited() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopGraceTimerLocked()
	s.graceTimer = time.AfterFunc(s.cfg.ReconnectGrace, func() {
		s.mu.Lock()
		busy := s.activeConn != nil
		if !busy && s.child == c {
			s.child = nil
		}
		s.mu.Unlock()
		if !busy {
			log.Printf("acp-shim: reconnect grace expired, terminating child")
			c.terminate(s.cfg.GracePeriod)
		}
	})
}

// stopGraceTimerLocked cancels a pending reconnect-grace termination. The
// caller must hold s.mu.
func (s *Server) stopGraceTimerLocked() {
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
}

// pump moves frames between the WebSocket and the child's stdio until either
// side ends. One WebSocket text frame corresponds to one newline-delimited
// JSON-RPC line; the shim never parses the payload.
func (s *Server) pump(conn *websocket.Conn, c *child) {
	readerDone := make(chan struct{})

	// WebSocket → child stdin.
	go func() {
		defer close(readerDone)
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
				continue
			}
			if err := c.writeLine(data); err != nil {
				log.Printf("acp-shim: %v", err)
				return
			}
		}
	}()

	// Child stdout → WebSocket.
	for {
		select {
		case line, ok := <-c.out:
			if !ok {
				// Child exited: tell the client why with a distinguishable
				// close code so the bridge can decide whether to restart.
				code := CloseChildExited
				reason := "agent exited"
				if err := c.exitError(); err != nil {
					code = CloseChildFailed
					reason = fmt.Sprintf("agent exited: %v", err)
				}
				msg := websocket.FormatCloseMessage(code, reason)
				_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
				log.Printf("acp-shim: websocket write failed: %v", err)
				return
			}
		case <-readerDone:
			return
		}
	}
}
