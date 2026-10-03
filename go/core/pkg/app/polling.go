package app

import (
	"fmt"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

type pollingConfig struct {
	sessionQuiescence, sessionExpiration, sandboxExpiration time.Duration
	scheduledRun, scheduledRunExecution, runtimeRevisionGC  time.Duration
	agentPreparation, sandboxPreparation                    time.Duration
	mcpToolRefresh, mcpReadiness                            time.Duration
}

// Validate before opening the database so a typo cannot silently restore a fast
// polling default or panic a worker after the controller has started.
func pollingConfigFromEnv() (pollingConfig, error) {
	var config pollingConfig
	for _, setting := range []struct {
		variable kagentenv.DurationVar
		target   *time.Duration
	}{
		{kagentenv.SessionQuiescencePollInterval, &config.sessionQuiescence},
		{kagentenv.SessionExpirationPollInterval, &config.sessionExpiration},
		{kagentenv.SandboxExpirationPollInterval, &config.sandboxExpiration},
		{kagentenv.ScheduledRunPollInterval, &config.scheduledRun},
		{kagentenv.ScheduledRunExecutionPollInterval, &config.scheduledRunExecution},
		{kagentenv.RuntimeRevisionGCInterval, &config.runtimeRevisionGC},
		{kagentenv.AgentPreparationPollInterval, &config.agentPreparation},
		{kagentenv.SandboxPreparationPollInterval, &config.sandboxPreparation},
		{kagentenv.MCPToolRefreshInterval, &config.mcpToolRefresh},
		{kagentenv.MCPReadinessPollInterval, &config.mcpReadiness},
	} {
		interval, _, err := setting.variable.LookupWithError()
		if err != nil {
			return pollingConfig{}, err
		}
		if interval <= 0 {
			return pollingConfig{}, fmt.Errorf("%s must be positive", setting.variable.Name())
		}
		*setting.target = interval
	}
	return config, nil
}
