// Package executor builds the Codex Harness A2A executor for kagent-codex
// and for binaries that embed the harness elsewhere.
package executor

import (
	"context"
	"fmt"
	"io"
	"time"
	"unicode"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/harness/codex/internal/adapter"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
)

// Config is the input to New.
type Config struct {
	// ConfigJSON is the compiler-owned KAGENT_CONFIG_JSON document.
	ConfigJSON []byte
	// DataDir is the absolute durable directory; it is created if missing.
	DataDir string
	// Environment is the process environment passed to the Codex CLI.
	Environment []string
}

// New validates the configuration and Codex installation, then returns the
// executor and the resource to close on shutdown.
func New(ctx context.Context, cfg Config) (a2asrv.AgentExecutor, io.Closer, error) {
	runner, err := adapter.New(ctx, adapter.Input{
		ConfigJSON: cfg.ConfigJSON, Workspace: cfg.DataDir + "/workspace", DurableDir: cfg.DataDir, Environment: cfg.Environment,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure Codex Harness: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := runner.Validate(validateCtx); err != nil {
		return nil, nil, err
	}
	store, err := continuation.New(cfg.DataDir+"/adapter", "codex", validateThreadID)
	if err != nil {
		return nil, nil, err
	}
	executor, err := runtimea2a.New(runner, store)
	if err != nil {
		return nil, nil, err
	}
	return executor, io.NopCloser(nil), nil
}

func validateThreadID(id string) error {
	if id == "" || len(id) > 256 {
		return fmt.Errorf("invalid Codex thread ID length")
	}
	for _, character := range id {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return fmt.Errorf("invalid Codex thread ID")
		}
	}
	return nil
}
