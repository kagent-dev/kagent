package session

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

func TestHTTPPushSenderRejectsHTTPSRedirectToHTTP(t *testing.T) {
	var downgraded atomic.Int32
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downgraded.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer httpTarget.Close()
	httpsSource := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer signed-jwt", r.Header.Get("Authorization"))
		http.Redirect(w, r, httpTarget.URL, http.StatusTemporaryRedirect)
	}))
	defer httpsSource.Close()

	sender := NewHTTPPushSender(time.Second, false, true)
	sender.client.Transport = httpsSource.Client().Transport
	config := &a2a.PushConfig{URL: httpsSource.URL, Auth: &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: "signed-jwt"}}
	err := sender.SendPush(t.Context(), config, &a2a.TaskStatusUpdateEvent{TaskID: "task"})
	require.ErrorContains(t, err, "307 Temporary Redirect")
	require.Zero(t, downgraded.Load(), "a redirect must not receive the JWT or task update")
}

func TestHTTPPushSenderEnforcesHTTPSettingAtDelivery(t *testing.T) {
	var requests atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	config := &a2a.PushConfig{URL: receiver.URL}
	event := &a2a.TaskStatusUpdateEvent{TaskID: "task"}

	err := NewHTTPPushSender(time.Second, false, true).SendPush(t.Context(), config, event)
	require.ErrorContains(t, err, "HTTPS")
	require.Zero(t, requests.Load())
	require.NoError(t, NewHTTPPushSender(time.Second, true, true).SendPush(t.Context(), config, event))
	require.EqualValues(t, 1, requests.Load())
}

func TestHTTPPushSenderStillBlocksPrivateAddresses(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	config := &a2a.PushConfig{URL: receiver.URL}
	err := NewHTTPPushSender(time.Second, true, false).SendPush(t.Context(), config, &a2a.TaskStatusUpdateEvent{TaskID: "task"})
	require.ErrorContains(t, err, "blocked address range")
}
