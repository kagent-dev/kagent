package managedagents

import (
	"context"
	"maps"
	"slices"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	collectionInterval = 30 * time.Second
	collectionTimeout  = 10 * time.Second
)

func (l *Launcher) collectPeriodically(ctx context.Context) {
	ticker := time.NewTicker(collectionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.collect(ctx)
		}
	}
}

func (l *Launcher) trackedSessions() []string {
	l.processMu.Lock()
	defer l.processMu.Unlock()
	return slices.Collect(maps.Keys(l.processes))
}

func (l *Launcher) collect(ctx context.Context) {
	for _, sessionID := range l.trackedSessions() {
		if ctx.Err() != nil {
			return
		}
		operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*collectionTimeout)
		l.collectSandbox(operationCtx, sessionID)
		cancel()
	}
}

func (l *Launcher) collectSandbox(ctx context.Context, sessionID string) error {
	ctx, stop := context.WithTimeout(ctx, 3*collectionTimeout)
	defer stop()
	entry, err := l.lockSandbox(ctx, sessionID)
	if err != nil {
		return safeRecoveryError("managed agents collection gate failed", err)
	}
	defer l.unlockSandbox(sessionID, entry)
	return l.finishWorker(ctx, entry)
}

func processNotFound(err error) bool {
	if code := serviceerrors.CodeOf(err); code != "" {
		return code == serviceerrors.CodeNotFound
	}
	return status.Code(err) == codes.NotFound
}
