package acpshim

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestShutdownReturnsWithStalledClient exercises the deadlock end to end
// through the real Server. A connected client that never reads causes pump()'s
// conn.WriteMessage to block, so pump stops draining c.out; c.out fills and
// the child's stdout reader goroutine blocks on send. Server.Shutdown ->
// child.terminate() must still kill the child and return promptly instead of
// hanging on <-c.done.
func TestShutdownReturnsWithStalledClient(t *testing.T) {
	// Emit ~6.4MB (400 x 16KB lines) to overflow the socket buffers of a
	// non-reading client, then stay alive so terminate() must kill the child.
	cfg := &Config{
		ListenAddr: "127.0.0.1:0",
		ChildArgv: []string{"sh", "-c",
			"head -c 6400000 /dev/zero | tr '\\0' 'A' | fold -w 16000; sleep 3600"},
		GracePeriod: 500 * time.Millisecond,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	l, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := NewServer(cfg)
	go func() { _ = srv.Serve(l) }()
	url := "ws://" + l.Addr().String() + "/acp"

	// A client that connects but never reads. Its receive window fills, so the
	// server's pump blocks in conn.WriteMessage and stops draining c.out.
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Let the pipeline stall: child spews -> out fills -> pump blocks on write.
	time.Sleep(2 * time.Second)

	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatalf("Server.Shutdown did not return within 6s: stalled client -> pump stops " +
			"draining c.out -> child reader blocks on send -> terminate() hangs on <-c.done")
	}
}
