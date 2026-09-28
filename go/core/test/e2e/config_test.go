package e2e_test

import (
	"testing"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

func TestInteractionTargetConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		e2eURL string
		apiURL string
		want   string
	}{
		{name: "explicit E2E URL", e2eURL: "http://e2e.example:8083", want: "e2e.example:8083"},
		{name: "E2E URL takes precedence", e2eURL: "http://e2e.example:8083", apiURL: "http://runtime.example:8084", want: "e2e.example:8083"},
		{name: "empty E2E URL falls back", apiURL: "http://runtime.example:8084", want: "runtime.example:8084"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(kagentenv.E2EAPIURL.Name(), tc.e2eURL)
			t.Setenv(kagentenv.KagentAPIURL.Name(), tc.apiURL)
			require.Equal(t, tc.want, interactionTarget(t))
		})
	}
}

func TestReachableServerURLConfiguredHost(t *testing.T) {
	t.Setenv(kagentenv.KagentLocalHost.Name(), "mock.example")
	require.Equal(t, "http://mock.example:8090/v1", reachableServerURL(t, "http://127.0.0.1:8090/", "/v1"))
}
