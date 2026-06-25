// Package acpshim implements the agent-agnostic WebSocket↔stdio shim that
// exposes a stdio ACP agent (Hermes, Codex, Gemini CLI, openclaw acp, ...)
// over a WebSocket endpoint reachable through Substrate's atenet ingress.
//
// The shim deliberately knows nothing about ACP semantics: it pumps frames
// (one WebSocket text frame ⇄ one newline-delimited JSON-RPC line on the
// child's stdin/stdout) and couples the child process lifecycle to the
// connection. All protocol translation lives on the kagent controller side.
package acpshim

import (
	"fmt"
	"os"
	"time"
)

// Config holds the shim's runtime configuration. The child command is the
// only per-backend part; everything else is uniform across agents.
type Config struct {
	// ListenAddr is the address the WebSocket server binds to, e.g. ":9000".
	ListenAddr string
	// ChildArgv is the argv vector of the stdio ACP agent to run. Never
	// interpreted by a shell.
	ChildArgv []string
	// ChildDir is the working directory for the child process.
	ChildDir string
	// ChildEnv is extra environment for the child, appended to os.Environ().
	ChildEnv []string
	// GracePeriod is how long to wait after SIGTERM before SIGKILL.
	GracePeriod time.Duration
	// ReconnectGrace is how long the child is kept alive after the last client
	// disconnects before the shim terminates it. Zero means keep the child
	// alive indefinitely.
	ReconnectGrace time.Duration
}

// LoadConfig applies the ACP_SHIM_* environment-variable fallbacks to c. The
// env fallbacks let the agent command be baked into an image without
// overriding the entrypoint. Call before Validate.
func LoadConfig(c *Config) {
	if len(c.ChildArgv) == 0 {
		if v := os.Getenv("ACP_SHIM_CHILD"); v != "" {
			c.ChildArgv = []string{"/bin/sh", "-c", v}
		}
	}
}

// Validate checks the config and applies defaults.
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if len(c.ChildArgv) == 0 {
		return fmt.Errorf("child command is required")
	}
	if c.GracePeriod <= 0 {
		c.GracePeriod = 5 * time.Second
	}
	return nil
}
