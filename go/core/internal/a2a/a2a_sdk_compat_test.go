package a2a_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
)

func TestJSONRPCClientDecodesSingleObjectErrorData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(request.ID) + `,"error":{"code":-32603,"message":"agent execution failed","data":{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"a2a-protocol.org","reason":"INTERNAL_ERROR"}}}`))
	}))
	t.Cleanup(server.Close)

	transport := a2aclient.NewJSONRPCTransport(server.URL, server.Client())
	_, err := transport.SendMessage(t.Context(), a2aclient.ServiceParams{}, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("test")),
	})
	if err == nil {
		t.Fatal("SendMessage() succeeded, want application error")
	}
	if !strings.Contains(err.Error(), "agent execution failed") {
		t.Fatalf("SendMessage() error = %q, want application error", err)
	}
	if strings.Contains(err.Error(), "failed to decode response") {
		t.Fatalf("SendMessage() error obscured application error: %v", err)
	}
}
