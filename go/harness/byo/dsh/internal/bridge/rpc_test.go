package bridge

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWireRejectsMalformedAndOversizedFrames(t *testing.T) {
	for _, test := range []struct{ name, data string }{
		{"not JSON", "startup log\n"},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"result":{}}` + "\n"},
		{"oversized", strings.Repeat("x", maxFrameBytes+1) + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWire(discardACPInput{}, strings.NewReader(test.data))
			_, err := w.next(context.Background())
			require.Error(t, err)
			close(w.done)
			<-w.readDone
		})
	}
}

func TestWireChecksResponseIdentityAndHidesErrorContent(t *testing.T) {
	for _, test := range []struct{ name, data string }{
		{"wrong response", `{"jsonrpc":"2.0","id":2,"result":{}}`},
		{"remote error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"secret"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWire(discardACPInput{}, strings.NewReader(test.data+"\n"))
			_, err := w.call(t.Context(), "1", "initialize", struct{}{})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			close(w.done)
			<-w.readDone
		})
	}
}

type discardACPInput struct{}

func (discardACPInput) Write(data []byte) (int, error) { return len(data), nil }
func (discardACPInput) Close() error                   { return nil }

func TestWireCancelsBlockedWrites(t *testing.T) {
	for _, method := range []string{"prompt", "setup", "permission", "cancel"} {
		t.Run(method, func(t *testing.T) {
			reader, writer := io.Pipe()
			w := newWire(writer, strings.NewReader(""))
			t.Cleanup(func() {
				close(w.done)
				<-w.readDone
				require.NoError(t, reader.Close())
				require.NoError(t, writer.Close())
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				payload := strings.Repeat("x", 1<<20)
				var err error
				switch method {
				case "prompt":
					err = w.send(ctx, message{JSONRPC: "2.0", ID: json.RawMessage(promptID), Method: "session/prompt"}, payload)
				case "setup":
					_, err = w.call(ctx, "2", "session/new", payload)
				case "permission":
					err = w.respond(ctx, json.RawMessage("99"), payload)
				case "cancel":
					err = w.send(ctx, message{JSONRPC: "2.0", Method: "session/cancel"}, payload)
				}
				done <- err
			}()
			// Reading only one byte leaves Write blocked on the rest of the frame.
			_, err := io.ReadFull(reader, make([]byte, 1))
			require.NoError(t, err)
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("ACP write did not respond to cancellation")
			}
		})
	}
}
