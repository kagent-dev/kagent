package handlers

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"::1", true},
		{"[::1]", true},
		{"openshell.openshell.svc.cluster.local", false},
		{"10.0.0.1", false},
	}
	for _, tt := range tests {
		if got := isLoopbackHost(tt.host); got != tt.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestResolveSandboxSSHRemoteCommand(t *testing.T) {
	plain, cmd := resolveSandboxSSHRemoteCommand(false, "")
	if plain || cmd != defaultSandboxSSHLaunchCmd {
		t.Fatalf("default launch: plain=%v cmd=%q", plain, cmd)
	}
	plain, cmd = resolveSandboxSSHRemoteCommand(true, "")
	if !plain || cmd != "" {
		t.Fatalf("plain shell: plain=%v cmd=%q", plain, cmd)
	}
	plain, cmd = resolveSandboxSSHRemoteCommand(false, "  custom  ")
	if plain || cmd != "custom" {
		t.Fatalf("override: plain=%v cmd=%q", plain, cmd)
	}
}

func TestResolveGatewayDialHost(t *testing.T) {
	got, err := resolveGatewayDialHost("ingress.example.com", "ignored:8080")
	if err != nil || got != "ingress.example.com" {
		t.Fatalf("non-loopback: got %q err %v", got, err)
	}

	got, err = resolveGatewayDialHost("127.0.0.1", "openshell.openshell.svc.cluster.local:8080")
	if err != nil || got != "openshell.openshell.svc.cluster.local" {
		t.Fatalf("loopback + grpc: got %q err %v", got, err)
	}

	got, err = resolveGatewayDialHost("127.0.0.1", "grpc.other.namespace.svc.cluster.local:9090")
	if err != nil || got != "grpc.other.namespace.svc.cluster.local" {
		t.Fatalf("loopback + other grpc host: got %q err %v", got, err)
	}
}

func TestResolveGatewayDialHost_InvalidGRPCTarget(t *testing.T) {
	_, err := resolveGatewayDialHost("127.0.0.1", "not-a-valid-hostport")
	if err == nil {
		t.Fatal("expected error for invalid grpc host:port")
	}

	_, err = resolveGatewayDialHost("127.0.0.1", ":8080")
	if err == nil {
		t.Fatal("expected error for empty grpc host with loopback gateway")
	}
	if !strings.Contains(err.Error(), "empty host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveOpenshellGRPCAddr(t *testing.T) {
	t.Run("start frame wins over env", func(t *testing.T) {
		t.Setenv(openshellGRPCEnv, "env-host:9090")
		got := resolveOpenshellGRPCAddr(sshStartMsg{GRPCAddress: "  frame:1111  "})
		if got != "frame:1111" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("env when frame empty", func(t *testing.T) {
		t.Setenv(openshellGRPCEnv, "env-host:9090")
		got := resolveOpenshellGRPCAddr(sshStartMsg{})
		if got != "env-host:9090" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("default when frame and env empty", func(t *testing.T) {
		t.Setenv(openshellGRPCEnv, "")
		got := resolveOpenshellGRPCAddr(sshStartMsg{})
		if got != defaultOpenshellGRPCAddr {
			t.Fatalf("got %q", got)
		}
	})
}

func TestParseSSHResizePayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantOK  bool
		wantR   int
		wantC   int
	}{
		{"empty", "", false, 0, 0},
		{"not json", "hello", false, 0, 0},
		{"json not object prefix", "[1,2]", false, 0, 0},
		{"wrong type", `{"type":"ping","cols":80,"rows":24}`, false, 0, 0},
		{"zero cols", `{"type":"resize","cols":0,"rows":24}`, false, 0, 0},
		{"zero rows", `{"type":"resize","cols":80,"rows":0}`, false, 0, 0},
		{"ok", `{"type":"resize","cols":100,"rows":40}`, true, 40, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, c, ok := parseSSHResizePayload([]byte(tt.payload))
			if ok != tt.wantOK || r != tt.wantR || c != tt.wantC {
				t.Fatalf("parseSSHResizePayload(%q) = (%d,%d,%v), want (%d,%d,%v)",
					tt.payload, r, c, ok, tt.wantR, tt.wantC, tt.wantOK)
			}
		})
	}
}

func TestHandleSandboxSSHWSInbound_WritesStdin(t *testing.T) {
	var buf bytes.Buffer
	handleSandboxSSHWSInbound(websocket.TextMessage, []byte("hello"), nil, &buf)
	if buf.String() != "hello" {
		t.Fatalf("text: got %q", buf.String())
	}
	buf.Reset()
	handleSandboxSSHWSInbound(websocket.BinaryMessage, []byte{0xaa, 0xbb}, nil, &buf)
	want := []byte{0xaa, 0xbb}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("binary: got %x want %x", buf.Bytes(), want)
	}
}

func TestReadSandboxSSHStart(t *testing.T) {
	out := make(chan any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			out <- err
			return
		}
		defer c.Close()
		start, err := readSandboxSSHStart(c)
		if err != nil {
			out <- err
			return
		}
		out <- start
	}))
	defer srv.Close()

	dialURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	t.Run("valid", func(t *testing.T) {
		client, _, err := websocket.DefaultDialer.Dial(dialURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"sandbox_name":"my-sb","cols":80,"rows":24}`)); err != nil {
			t.Fatal(err)
		}
		switch v := (<-out).(type) {
		case error:
			t.Fatal(v)
		case sshStartMsg:
			if v.SandboxName != "my-sb" || v.Cols != 80 || v.Rows != 24 {
				t.Fatalf("%+v", v)
			}
		default:
			t.Fatalf("unexpected %T", v)
		}
	})

	t.Run("defaults term size", func(t *testing.T) {
		client, _, err := websocket.DefaultDialer.Dial(dialURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"sandbox_name":"x"}`)); err != nil {
			t.Fatal(err)
		}
		switch v := (<-out).(type) {
		case error:
			t.Fatal(v)
		case sshStartMsg:
			if v.Cols != sandboxSSHDefaultCols || v.Rows != sandboxSSHDefaultRows {
				t.Fatalf("cols=%d rows=%d", v.Cols, v.Rows)
			}
		default:
			t.Fatalf("unexpected %T", v)
		}
	})

	t.Run("trims sandbox name", func(t *testing.T) {
		client, _, err := websocket.DefaultDialer.Dial(dialURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"sandbox_name":"  sb  "}`)); err != nil {
			t.Fatal(err)
		}
		switch v := (<-out).(type) {
		case error:
			t.Fatal(v)
		case sshStartMsg:
			if v.SandboxName != "sb" {
				t.Fatalf("got %q", v.SandboxName)
			}
		default:
			t.Fatalf("unexpected %T", v)
		}
	})

	t.Run("missing sandbox_name", func(t *testing.T) {
		client, _, err := websocket.DefaultDialer.Dial(dialURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.WriteMessage(websocket.TextMessage, []byte(`{"sandbox_name":"  "}`)); err != nil {
			t.Fatal(err)
		}
		gotErr, ok := (<-out).(error)
		if !ok || gotErr == nil || !strings.Contains(gotErr.Error(), "sandbox_name") {
			t.Fatalf("got %v ok=%v", gotErr, ok)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		client, _, err := websocket.DefaultDialer.Dial(dialURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.WriteMessage(websocket.TextMessage, []byte(`not-json`)); err != nil {
			t.Fatal(err)
		}
		gotErr, ok := (<-out).(error)
		if !ok || gotErr == nil {
			t.Fatalf("got %v ok=%v", gotErr, ok)
		}
	})
}

func TestCompleteHTTPConnect(t *testing.T) {
	t.Run("ok and reads tunneled bytes", func(t *testing.T) {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer server.Close()
			br := bufio.NewReader(server)
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if line == "\r\n" || line == "\n" {
					break
				}
			}
			if _, err := server.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\npayload")); err != nil {
				return
			}
		}()

		ctx := context.Background()
		tunnel, err := completeHTTPConnect(ctx, client, "gw.example", "/connect-path", "sid", "tok")
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 32)
		n, err := tunnel.Read(buf)
		if err != nil || string(buf[:n]) != "payload" {
			t.Fatalf("read %q err %v", buf[:n], err)
		}
		_ = client.Close()
		<-done
	})

	t.Run("non-200", func(t *testing.T) {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer server.Close()
			br := bufio.NewReader(server)
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if line == "\r\n" || line == "\n" {
					break
				}
			}
			_, _ = server.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n"))
		}()

		_, err := completeHTTPConnect(context.Background(), client, "gw.example", "/p", "sid", "tok")
		_ = client.Close()
		<-done
		if err == nil || !strings.Contains(err.Error(), "CONNECT failed") {
			t.Fatalf("got %v", err)
		}
	})
}
