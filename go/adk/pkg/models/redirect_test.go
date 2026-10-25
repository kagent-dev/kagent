package models_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
)

func TestModelRedirectsFailWithoutForwardingCredentials(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var destinationCalls atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				destinationCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(destination.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL, code)
			}))
			t.Cleanup(origin.Close)
			client, err := models.BuildHTTPClient(models.TransportConfig{Headers: map[string]string{"Authorization": "Bearer credential"}})
			require.NoError(t, err)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin.URL, strings.NewReader(`{}`))
			require.NoError(t, err)
			response, err := client.Do(request)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.ErrorContains(t, err, "model request redirects are not supported")
			require.Zero(t, destinationCalls.Load(), "redirect must not reach another destination")
		})
	}
}
