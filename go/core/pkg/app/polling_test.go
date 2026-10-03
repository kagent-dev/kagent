package app

import (
	"testing"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

func TestPollingConfigFromEnv(t *testing.T) {
	variables := []kagentenv.DurationVar{
		kagentenv.SessionQuiescencePollInterval, kagentenv.SessionExpirationPollInterval,
		kagentenv.SandboxExpirationPollInterval, kagentenv.ScheduledRunPollInterval,
		kagentenv.ScheduledRunExecutionPollInterval, kagentenv.RuntimeRevisionGCInterval,
		kagentenv.MCPToolRefreshInterval, kagentenv.MCPReadinessPollInterval,
	}
	for _, variable := range variables {
		t.Setenv(variable.Name(), "")
	}
	config, err := pollingConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, pollingConfig{
		sessionQuiescence: time.Second, sessionExpiration: time.Minute, sandboxExpiration: time.Second,
		scheduledRun: time.Second, scheduledRunExecution: time.Second, runtimeRevisionGC: time.Minute,
		mcpToolRefresh: 5 * time.Minute, mcpReadiness: 10 * time.Second,
	}, config)

	for _, variable := range variables {
		t.Run(variable.Name(), func(t *testing.T) {
			for _, input := range []string{"typo", "0s", "-1m"} {
				t.Run(input, func(t *testing.T) {
					t.Setenv(variable.Name(), input)
					_, err := pollingConfigFromEnv()
					require.ErrorContains(t, err, variable.Name())
				})
			}
		})
	}
	for i, variable := range variables {
		t.Setenv(variable.Name(), (time.Duration(i+1) * time.Minute).String())
	}
	config, err = pollingConfigFromEnv()
	require.NoError(t, err)
	require.Equal(t, pollingConfig{
		sessionQuiescence: time.Minute, sessionExpiration: 2 * time.Minute, sandboxExpiration: 3 * time.Minute,
		scheduledRun: 4 * time.Minute, scheduledRunExecution: 5 * time.Minute, runtimeRevisionGC: 6 * time.Minute,
		mcpToolRefresh: 7 * time.Minute, mcpReadiness: 8 * time.Minute,
	}, config)
}
