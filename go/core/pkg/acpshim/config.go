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
	"strings"
	"time"
)

// ChildPolicy controls how the child agent process lifecycle relates to
// WebSocket connections.
type ChildPolicy string

const (
	// ChildPolicyLongLived starts the child once and keeps it alive across
	// reconnects (required for agents with in-memory sessions, e.g. Hermes).
	ChildPolicyLongLived ChildPolicy = "long-lived"
	// ChildPolicyPerConnection starts a fresh child per WebSocket connection
	// and terminates it when the connection closes.
	ChildPolicyPerConnection ChildPolicy = "per-connection"
)

// Config holds the shim's runtime configuration. The child command is the
// only per-backend part; everything else is uniform across agents.
type Config struct {
	// ListenAddr is the address the WebSocket server binds to, e.g. ":9000".
	ListenAddr string
	// TokenFile is the path to the bearer token used to authenticate the
	// WebSocket handshake. If empty, Token must be set (or auth is disabled
	// when both are empty — prototype/testing only).
	TokenFile string
	// Token is the literal bearer token. Takes precedence over TokenFile.
	Token string
	// ChildArgv is the argv vector of the stdio ACP agent to run. Never
	// interpreted by a shell.
	ChildArgv []string
	// ChildDir is the working directory for the child process.
	ChildDir string
	// ChildEnv is extra environment for the child, appended to os.Environ().
	ChildEnv []string
	// Policy selects long-lived vs per-connection child lifecycle.
	Policy ChildPolicy
	// GracePeriod is how long to wait after SIGTERM before SIGKILL.
	GracePeriod time.Duration
	// ReconnectGrace is how long a long-lived child is kept alive after the
	// last client disconnects before the shim terminates it. Zero means
	// keep the child alive indefinitely.
	ReconnectGrace time.Duration
}

// LoadConfig applies the child policy and the ACP_SHIM_* environment-variable
// fallbacks to c. The env fallbacks let the agent command and credentials be
// baked into an image without overriding the entrypoint: Substrate
// ActorTemplate containers support env (incl. secretKeyRef) but not volume
// mounts, so the token may be passed directly. A literal token wins over the
// token file (the base image bakes in a default ACP_SHIM_TOKEN_FILE that only
// exists when a Secret is mounted). Call before Validate.
func LoadConfig(c *Config, policy string) {
	c.Policy = ChildPolicy(policy)
	if len(c.ChildArgv) == 0 {
		if v := os.Getenv("ACP_SHIM_CHILD"); v != "" {
			c.ChildArgv = []string{"/bin/sh", "-c", v}
		}
	}
	if c.TokenFile == "" {
		c.TokenFile = os.Getenv("ACP_SHIM_TOKEN_FILE")
	}
	if c.Token == "" {
		c.Token = os.Getenv("ACP_SHIM_TOKEN")
	}
}

// Validate checks the config and resolves the token from TokenFile if needed.
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if len(c.ChildArgv) == 0 {
		return fmt.Errorf("child command is required")
	}
	switch c.Policy {
	case ChildPolicyLongLived, ChildPolicyPerConnection:
	case "":
		c.Policy = ChildPolicyLongLived
	default:
		return fmt.Errorf("invalid child policy %q (want %q or %q)", c.Policy, ChildPolicyLongLived, ChildPolicyPerConnection)
	}
	if c.GracePeriod <= 0 {
		c.GracePeriod = 5 * time.Second
	}
	if c.Token == "" && c.TokenFile != "" {
		b, err := os.ReadFile(c.TokenFile)
		if err != nil {
			return fmt.Errorf("failed to read token file %s: %w", c.TokenFile, err)
		}
		c.Token = strings.TrimSpace(string(b))
		if c.Token == "" {
			return fmt.Errorf("token file %s is empty", c.TokenFile)
		}
	}
	return nil
}
