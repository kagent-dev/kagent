package app

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/anthropicselfhostedsandboxes"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// ManagedAgentsConfig configures the optional environment-driven launcher.
type ManagedAgentsConfig = anthropicselfhostedsandboxes.Config

type managedAgentsRunnable struct {
	launcher *anthropicselfhostedsandboxes.Launcher
	session  auth.Session
}

var _ manager.Runnable = (*managedAgentsRunnable)(nil)
var _ manager.LeaderElectionRunnable = (*managedAgentsRunnable)(nil)

func newManagedAgentsRunnable(cfg ManagedAgentsConfig, sandboxes anthropicselfhostedsandboxes.SandboxService, log *slog.Logger) (*managedAgentsRunnable, error) {
	launcher, err := anthropicselfhostedsandboxes.New(cfg, sandboxes, log)
	if err != nil {
		return nil, err
	}
	// Stable internal ownership, independent of pods and human credentials.
	// The Sandbox service still applies the configured authorizer.
	session := auth.NewCredentialSession(auth.Principal{
		ID: auth.PrincipalID("kagent:managed-agents", cfg.EnvironmentID),
	}, nil)
	return &managedAgentsRunnable{launcher: launcher, session: session}, nil
}

func (*managedAgentsRunnable) NeedLeaderElection() bool { return true }

func (r *managedAgentsRunnable) Start(ctx context.Context) error {
	return r.launcher.Start(auth.AuthSessionTo(ctx, r.session))
}
