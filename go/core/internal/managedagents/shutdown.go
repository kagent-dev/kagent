package managedagents

import (
	"context"
	"time"
)

func (l *Launcher) shutdown(ctx context.Context, gracefulTimeout, forcedTimeout time.Duration) {
	gracefulCtx, cancel := context.WithTimeout(ctx, gracefulTimeout)
	l.stopWorkers(gracefulCtx, false)
	l.drainSandboxes(gracefulCtx)
	cancel()
	if len(l.trackedSessions()) == 0 {
		return
	}

	forcedCtx, cancel := context.WithTimeout(ctx, forcedTimeout)
	defer cancel()
	stopped := l.stopWorkers(forcedCtx, true)
	for _, sessionID := range l.trackedSessions() {
		entry, err := l.lockSandbox(forcedCtx, sessionID)
		if err != nil {
			break
		}
		allStopped := true
		for _, workID := range entry.processIDs {
			if !stopped[workID] {
				allStopped = false
			}
		}
		if allStopped {
			// Forced work release permits suspension even with commands running.
			entry.suspending = true
		}
		l.unlockSandbox(sessionID, entry)
	}
	l.drainSandboxes(forcedCtx)
	if remaining := len(l.trackedSessions()); remaining != 0 {
		l.log.ErrorContext(ctx, "managed agent shutdown left unsuspended sandboxes", "sandbox_count", remaining)
	}
}

func (l *Launcher) drainSandboxes(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		l.collect(ctx)
		if len(l.trackedSessions()) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
