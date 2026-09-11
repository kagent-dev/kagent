package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
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

func TestRPCClientPropagatesTraceContext(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	state, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, TraceState: state,
	}))
	written := &bytes.Buffer{}
	client := newRPCClient(
		nopWriteCloser{Buffer: written},
		strings.NewReader(`{"id":1,"result":{}}`+"\n"),
		1024,
	)
	if _, err := client.call(ctx, 1, "turn/start", map[string]string{"threadId": "thread"}); err != nil {
		t.Fatal(err)
	}
	var message rpcMessage
	if err := json.Unmarshal(bytes.TrimSpace(written.Bytes()), &message); err != nil {
		t.Fatal(err)
	}
	if message.Trace == nil || message.Trace.Traceparent != "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01" || message.Trace.Tracestate != "vendor=value" {
		t.Fatalf("trace carrier = %#v", message.Trace)
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

func (n nopWriteCloser) Close() error { return nil }
