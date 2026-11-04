package anthropicselfhostedsandboxes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const sandboxNamePrefix = "anthropic-"

type recoveryError struct {
	message   string
	transient bool
}

func (e *recoveryError) Error() string { return e.message }

func recoveryRetryable(err error) bool {
	var failure *recoveryError
	return errors.As(err, &failure) && failure.transient
}

// Do not retain upstream payloads, which may include scoped work credentials.
func safeRecoveryError(message string, err error) error {
	code := upstreamStatus(err)
	var networkError net.Error
	return &recoveryError{message: message, transient: recoveryRetryable(err) || retryable(err) ||
		(serviceerrors.CodeOf(err) == "" && (errors.As(err, &networkError) || code == 408 || code == 409 || code == 429 || code >= 500))}
}

func (l *Launcher) adopt(ctx context.Context) error {
	selected := make(map[string]*apiv1.Sandbox)
	request := &apiv1.ListSandboxesRequest{
		SandboxTemplate: &apiv1.ResourceReference{Namespace: l.config.TemplateNamespace, Name: l.config.TemplateName},
		Page:            &apiv1.PageRequest{Limit: 200},
	}
	for {
		callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
		page, err := l.sandboxes.List(callCtx, request)
		cancel()
		if err != nil {
			return safeRecoveryError("anthropic self-hosted sandboxes sandbox discovery failed", err)
		}
		if page == nil {
			return errors.New("anthropic self-hosted sandboxes sandbox discovery returned no page")
		}
		for _, instance := range page.GetSandboxes() {
			sessionID, ok := strings.CutPrefix(instance.GetName(), sandboxNamePrefix)
			if !ok || sessionID == "" {
				continue
			}
			if err := l.validateSavedSandbox(instance, instance.GetId()); err != nil {
				return err
			}
			latest, err := newerSandbox(selected[sessionID], instance)
			if err != nil {
				return fmt.Errorf("failed to select anthropic self-hosted sandboxes sandbox: %w", err)
			}
			selected[sessionID] = latest
		}
		next := page.GetPage().GetNextPageToken()
		if next == "" {
			break
		}
		if next == request.Page.PageToken {
			return errors.New("anthropic self-hosted sandboxes sandbox discovery repeated a page token")
		}
		request.Page.PageToken = next
	}
	restored := make(map[string]*sandboxWorker, len(selected))
	for sessionID, instance := range selected {
		restored[sessionID] = &sandboxWorker{sandboxID: instance.GetId()}
	}
	l.processMu.Lock()
	l.processes = restored
	l.processMu.Unlock()
	for _, sessionID := range l.trackedSessions() {
		if err := l.collectSandbox(ctx, sessionID); err != nil {
			l.log.WarnContext(ctx, "failed to inspect discovered anthropic self-hosted sandbox sandbox", "error", err)
		}
	}
	return nil
}
