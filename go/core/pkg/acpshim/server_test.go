package acpshim

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConfigValidate(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		cfg       Config
		wantErr   bool
		wantToken string
	}{
		{
			name:    "missing listen addr",
			cfg:     Config{ChildArgv: []string{"cat"}},
			wantErr: true,
		},
		{
			name:    "missing child command",
			cfg:     Config{ListenAddr: ":0"},
			wantErr: true,
		},
		{
			name:    "invalid policy",
			cfg:     Config{ListenAddr: ":0", ChildArgv: []string{"cat"}, Policy: "sometimes"},
			wantErr: true,
		},
		{
			name: "defaults applied",
			cfg:  Config{ListenAddr: ":0", ChildArgv: []string{"cat"}},
		},
		{
			name:      "token loaded and trimmed from file",
			cfg:       Config{ListenAddr: ":0", ChildArgv: []string{"cat"}, TokenFile: tokenFile},
			wantToken: "s3cret",
		},
		{
			name:    "missing token file",
			cfg:     Config{ListenAddr: ":0", ChildArgv: []string{"cat"}, TokenFile: "/nonexistent/token"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if tt.cfg.Policy != ChildPolicyLongLived && tt.cfg.Policy != ChildPolicyPerConnection {
				t.Errorf("Validate() did not default policy, got %q", tt.cfg.Policy)
			}
			if tt.wantToken != "" && tt.cfg.Token != tt.wantToken {
				t.Errorf("Validate() token = %q, want %q", tt.cfg.Token, tt.wantToken)
			}
		})
	}
}

// startTestServer runs a shim Server on an ephemeral port and returns the
// ws:// URL of the ACP endpoint.
func startTestServer(t *testing.T, cfg *Config) string {
	t.Helper()
	cfg.ListenAddr = "127.0.0.1:0"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	l, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := NewServer(cfg)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "ws://" + l.Addr().String() + "/acp"
}

func dial(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, h)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return conn
}

func TestEchoRoundTrip(t *testing.T) {
	url := startTestServer(t, &Config{ChildArgv: []string{"cat"}})
	conn := dial(t, url, "")
	defer conn.Close()

	msg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != msg {
		t.Errorf("round trip = %q, want %q", got, msg)
	}
}

func TestAuth(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "correct token", token: "hunter2", wantErr: false},
		{name: "wrong token", token: "wrong", wantErr: true},
		{name: "missing token", token: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := startTestServer(t, &Config{ChildArgv: []string{"cat"}, Token: "hunter2"})
			h := http.Header{}
			if tt.token != "" {
				h.Set("Authorization", "Bearer "+tt.token)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(url, h)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Dial() error = %v, wantErr %v", err, tt.wantErr)
			}
			if conn != nil {
				conn.Close()
			}
			if tt.wantErr && resp != nil && resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}

func TestNewClientPreemptsStale(t *testing.T) {
	url := startTestServer(t, &Config{ChildArgv: []string{"cat"}})
	conn1 := dial(t, url, "")
	defer conn1.Close()

	// A browser refresh opens a new connection while the stale one lingers.
	// The newcomer must preempt the incumbent rather than being rejected with
	// 409, otherwise the user is locked out behind a half-open connection.
	conn2, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("second Dial() failed, want takeover success: %v (resp=%v)", err, resp)
	}
	defer conn2.Close()

	// The preempted first connection is closed by the shim.
	_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn1.ReadMessage(); err == nil {
		t.Fatal("first connection still readable, want it closed after preemption")
	}

	// The preempting connection is fully functional (echo via the cat child).
	msg := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if err := conn2.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("write on preempting conn: %v", err)
	}
	_ = conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("read on preempting conn: %v", err)
	}
	if string(got) != msg {
		t.Errorf("round trip = %q, want %q", got, msg)
	}
}

func TestChildExitCloseCodes(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		wantCode int
	}{
		{
			name:     "clean exit",
			argv:     []string{"sh", "-c", "echo done; exit 0"},
			wantCode: CloseChildExited,
		},
		{
			name:     "failed exit",
			argv:     []string{"sh", "-c", "echo oops; exit 3"},
			wantCode: CloseChildFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := startTestServer(t, &Config{ChildArgv: tt.argv, Policy: ChildPolicyPerConnection})
			conn := dial(t, url, "")
			defer conn.Close()

			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			// First read gets the child's output line.
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatalf("read output line: %v", err)
			}
			// Next read should observe the close frame with the right code.
			_, _, err := conn.ReadMessage()
			closeErr, ok := err.(*websocket.CloseError)
			if !ok {
				t.Fatalf("read after child exit = %v, want *websocket.CloseError", err)
			}
			if closeErr.Code != tt.wantCode {
				t.Errorf("close code = %d, want %d", closeErr.Code, tt.wantCode)
			}
		})
	}
}

func TestLongLivedChildSurvivesReconnect(t *testing.T) {
	// The child prints "ready" exactly once at startup. If the second
	// connection echoes our ping without a second "ready", the same child
	// survived the reconnect.
	url := startTestServer(t, &Config{
		ChildArgv: []string{"sh", "-c", "echo ready; cat"},
		Policy:    ChildPolicyLongLived,
	})

	conn1 := dial(t, url, "")
	_ = conn1.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := conn1.ReadMessage()
	if err != nil || string(got) != "ready" {
		t.Fatalf("first read = %q, %v; want \"ready\"", got, err)
	}
	conn1.Close()

	// Give the server a moment to release the connection slot.
	deadline := time.Now().Add(5 * time.Second)
	var conn2 *websocket.Conn
	for {
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			conn2 = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer conn2.Close()

	if err := conn2.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err = conn2.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "ping" {
		t.Errorf("after reconnect read %q, want \"ping\" (a second \"ready\" means the child was restarted)", got)
	}
}

func TestPerConnectionChildRestarted(t *testing.T) {
	url := startTestServer(t, &Config{
		ChildArgv: []string{"sh", "-c", "echo ready; cat"},
		Policy:    ChildPolicyPerConnection,
	})

	for i := range 2 {
		var conn *websocket.Conn
		deadline := time.Now().Add(5 * time.Second)
		for {
			c, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err == nil {
				conn = c
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("connection %d: %v", i, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, got, err := conn.ReadMessage()
		if err != nil || string(got) != "ready" {
			t.Fatalf("connection %d read = %q, %v; want \"ready\" (fresh child per connection)", i, got, err)
		}
		conn.Close()
	}
}
