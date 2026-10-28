package e2e_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelRecorderBodyOutlivesResponseStart(t *testing.T) {
	t.Parallel()
	reread := make(chan string, 1)
	recorder := startModelRecorder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		http.NewResponseController(w).Flush()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			reread <- err.Error()
			return
		}
		reread <- string(body)
		_, _ = io.WriteString(w, "data: done\n\n")
	}), nil)

	response, err := http.Post(recorder.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"stream":true}`))
	require.NoError(t, err)
	defer response.Body.Close()
	streamed, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	require.Equal(t, `{"stream":true}`, <-reread)
	require.Equal(t, "data: done\n\n", string(streamed))
}
