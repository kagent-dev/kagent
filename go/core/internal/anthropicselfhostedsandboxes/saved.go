package anthropicselfhostedsandboxes

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func (l *Launcher) validateSavedSandbox(instance *apiv1.Sandbox, id string) error {
	ref := instance.GetSandboxTemplate()
	if uuid.Validate(id) != nil || instance.GetId() != id ||
		instance.GetCreator() != auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", l.config.EnvironmentID) ||
		ref.GetNamespace() != l.config.TemplateNamespace || ref.GetName() != l.config.TemplateName {
		return errors.New("anthropic self-hosted sandboxes adoption sandbox identity mismatch")
	}
	if expires := instance.GetExpiresAt(); expires != nil && !expires.IsValid() {
		return errors.New("anthropic self-hosted sandboxes adoption sandbox expiry is invalid")
	}
	if created := instance.GetCreatedAt(); created != nil && !created.IsValid() {
		return errors.New("anthropic self-hosted sandboxes sandbox creation time is invalid")
	}
	if created, expires := instance.GetCreatedAt(), instance.GetExpiresAt(); created != nil && expires != nil &&
		!created.AsTime().Before(expires.AsTime()) {
		return errors.New("anthropic self-hosted sandboxes sandbox lifetime is invalid")
	}
	return nil
}

func (l *Launcher) savedSandbox(ctx context.Context, entry *sandboxWorker) (*apiv1.Sandbox, error) {
	if entry.sandboxID == "" {
		return nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
	instance, err := l.sandboxes.Get(callCtx, entry.sandboxID)
	cancel()
	if processNotFound(err) {
		return nil, errors.New("anthropic self-hosted sandboxes saved sandbox is missing without expiry evidence")
	}
	if err != nil {
		return nil, safeRecoveryError("anthropic self-hosted sandboxes saved sandbox lookup failed", err)
	}
	if err := l.validateSavedSandbox(instance, entry.sandboxID); err != nil {
		return nil, err
	}
	return instance, nil
}

func newerSandbox(current, candidate *apiv1.Sandbox) (*apiv1.Sandbox, error) {
	if current == nil || current.GetId() == candidate.GetId() {
		return candidate, nil
	}
	if !sandboxExpired(current) && !sandboxExpired(candidate) {
		return nil, errors.New("conflicting anthropic self-hosted sandboxes active sandbox generations")
	}
	currentTime, err := sandboxGeneration(current)
	if err != nil {
		return nil, err
	}
	candidateTime, err := sandboxGeneration(candidate)
	if err != nil {
		return nil, err
	}
	if currentTime.Equal(candidateTime) {
		return nil, errors.New("conflicting anthropic self-hosted sandboxes sandbox generation times")
	}
	if candidateTime.After(currentTime) {
		current, candidate = candidate, current
	}
	if !sandboxExpired(candidate) {
		return nil, errors.New("conflicting anthropic self-hosted sandboxes sandbox generation lifetimes")
	}
	return current, nil
}

func sandboxGeneration(instance *apiv1.Sandbox) (time.Time, error) {
	if created := instance.GetCreatedAt(); created != nil && created.IsValid() {
		if expires := instance.GetExpiresAt(); expires != nil && !created.AsTime().Before(expires.AsTime()) {
			return time.Time{}, errors.New("anthropic self-hosted sandboxes sandbox lifetime is invalid")
		}
		return created.AsTime(), nil
	}
	return time.Time{}, errors.New("anthropic self-hosted sandboxes sandbox generation time is missing")
}
