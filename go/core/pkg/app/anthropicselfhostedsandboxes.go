package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v5"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/anthropicselfhostedsandboxes"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// AnthropicSelfHostedSandboxesConfig configures the optional environment-driven launcher.
type AnthropicSelfHostedSandboxesConfig = anthropicselfhostedsandboxes.Config

type anthropicSelfHostedSandboxesRunnable struct {
	launcher *anthropicselfhostedsandboxes.Launcher
	session  auth.Session
	log      *slog.Logger
}

var _ manager.Runnable = (*anthropicSelfHostedSandboxesRunnable)(nil)
var _ manager.LeaderElectionRunnable = (*anthropicSelfHostedSandboxesRunnable)(nil)

func newAnthropicSelfHostedSandboxesRunnable(cfg AnthropicSelfHostedSandboxesConfig, sandboxes anthropicselfhostedsandboxes.SandboxService, log *slog.Logger) (*anthropicSelfHostedSandboxesRunnable, error) {
	launcher, err := anthropicselfhostedsandboxes.New(cfg, sandboxes, log)
	if err != nil {
		return nil, err
	}
	// Stable internal ownership, independent of pods and human credentials.
	// The Sandbox service still applies the configured authorizer.
	session := auth.NewCredentialSession(auth.Principal{
		ID: auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", cfg.EnvironmentID),
	}, nil)
	return &anthropicSelfHostedSandboxesRunnable{launcher: launcher, session: session, log: log}, nil
}

func (*anthropicSelfHostedSandboxesRunnable) NeedLeaderElection() bool { return true }

func (r *anthropicSelfHostedSandboxesRunnable) Start(ctx context.Context) error {
	ctx = auth.AuthSessionTo(ctx, r.session)
	retry := backoff.NewExponentialBackOff()
	retry.InitialInterval = time.Second
	retry.MaxInterval = time.Minute
	retry.Reset()
	for ctx.Err() == nil {
		err := r.launcher.Start(ctx)
		if ctx.Err() != nil {
			break
		}
		delay := min(retry.NextBackOff(), time.Minute)
		r.log.ErrorContext(ctx, "anthropic self-hosted sandboxes launcher stopped; retrying", "error", err, "retry_delay", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return nil
}
