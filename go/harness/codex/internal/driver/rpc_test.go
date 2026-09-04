package driver

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRPCClientRejectsOversizedAndUnexpectedRequests(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		max               int
	}{
		{"oversized", strings.Repeat("x", 20) + "\n", "exceeds", 10},
		{"invalid JSON-RPC version", `{"jsonrpc":"1.0","id":1,"result":{}}` + "\n", "unsupported Codex JSON-RPC version", 1024},
		{"server request", `{"jsonrpc":"2.0","id":9,"method":"item/tool/requestUserInput","params":{}}` + "\n", "unsupported Codex server request", 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newRPCClient(nopWriteCloser{Buffer: &bytes.Buffer{}}, strings.NewReader(test.input), test.max)
			_, err := client.call(context.Background(), 1, "initialize", map[string]any{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("call() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRPCClientAcceptsOmittedJSONRPCVersion(t *testing.T) {
	client := newRPCClient(
		nopWriteCloser{Buffer: &bytes.Buffer{}},
		strings.NewReader(`{"id":1,"result":{"serverInfo":{"name":"codex-app-server"}}}`+"\n"),
		1024,
	)
	result, err := client.call(context.Background(), 1, "initialize", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte(`"name":"codex-app-server"`)) {
		t.Fatalf("initialize result = %s", result)
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

func (n nopWriteCloser) Close() error { return nil }
