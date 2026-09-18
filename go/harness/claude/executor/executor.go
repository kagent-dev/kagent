// Package executor builds the Claude Harness A2A executor for kagent-claude
// and for binaries that embed the harness elsewhere.
package executor

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/adapter"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
)

// Config is the input to New.
type Config struct {
	// ConfigJSON is the compiler-owned KAGENT_CONFIG_JSON document.
	ConfigJSON []byte
	// DataDir is the absolute durable directory; it is created if missing.
	DataDir string
	// Environment is the process environment passed to the Claude CLI.
	Environment []string
}

// New validates the configuration and Claude installation, then returns the
// executor and the resource to close on shutdown.
func New(ctx context.Context, cfg Config) (a2asrv.AgentExecutor, io.Closer, error) {
	runner, err := adapter.New(ctx, adapter.Input{
		ConfigJSON: cfg.ConfigJSON,
		Workspace:  cfg.DataDir + "/workspace", DurableDir: cfg.DataDir,
		EphemeralDir: "/tmp/kagent-claude",
		Environment:  cfg.Environment,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure Claude Harness: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := runner.Validate(validateCtx); err != nil {
		_ = runner.Close()
		return nil, nil, err
	}
	store, err := continuation.New(cfg.DataDir+"/adapter", "claude", validateSessionID)
	if err != nil {
		_ = runner.Close()
		return nil, nil, err
	}
	executor, err := runtimea2a.New(runner, store)
	if err != nil {
		_ = runner.Close()
		return nil, nil, err
	}
	return executor, runner, nil
}

func validateSessionID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("invalid Claude session ID: %w", err)
	}
	return nil
}
