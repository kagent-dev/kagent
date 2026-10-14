package bridge

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWireRejectsMalformedAndOversizedFrames(t *testing.T) {
	for _, test := range []struct{ name, data string }{
		{"not JSON", "startup log\n"},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"result":{}}` + "\n"},
		{"oversized", strings.Repeat("x", maxFrameBytes+1) + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWire(io.Discard, strings.NewReader(test.data))
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
			w := newWire(io.Discard, strings.NewReader(test.data+"\n"))
			_, err := w.call(t.Context(), "1", "initialize", struct{}{})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
			close(w.done)
			<-w.readDone
		})
	}
}
