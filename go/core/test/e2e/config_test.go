package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func TestInteractionTargetConfiguration(t *testing.T) {
	t.Setenv(kagentenv.E2EAPIURL.Name(), "http://e2e.example:8083")
	require.Equal(t, "e2e.example:8083", interactionTarget(t))
}

func TestReachableServerURLConfiguredHost(t *testing.T) {
	t.Setenv(kagentenv.KagentLocalHost.Name(), "mock.example")
	require.Equal(t, "http://mock.example:8090/v1", reachableServerURL(t, "http://127.0.0.1:8090/", "/v1"))
}
