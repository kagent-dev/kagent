package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// ShareStore resolves the digest of a Session share token as of now.
type ShareStore interface {
	GetSessionShareByTokenHash(context.Context, []byte, time.Time) (*apiv1alpha1.SessionShare, string, error)
}

// ResolveShare validates an optional share token after the caller authenticates.
// The returned authority supplements the caller's identity; it never replaces it.
func ResolveShare(ctx context.Context, store ShareStore, token string) (*auth.ShareContext, error) {
	if token == "" {
		return nil, nil
	}
	if store == nil {
		return nil, serviceerrors.NewInternal("share-token validation is unavailable", nil)
	}
	digest := sha256.Sum256([]byte(token))
	share, owner, err := store.GetSessionShareByTokenHash(ctx, digest[:], time.Now())
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewPermissionDenied("invalid or expired share token", nil)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to validate share token", err)
	}
	return &auth.ShareContext{
		Token: token, UserID: owner, SessionID: share.GetSessionId(),
		ReadOnly: share.GetPermission() != apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE,
	}, nil
}

// errShareRunsSenderCode is the refusal of a share of a Session whose Harness
// may run code a turn brings with it. Every client shows it to the person who
// asked, so it names the remedy.
const errShareRunsSenderCode = "This Session cannot be shared: its agent runs code from each turn, " +
	"which would then run with the next person's credentials. Ask the others to start a Session of their own."

// refuseShareOfSenderCode refuses a share of the session unless its Agent's
// Harness, inline or referenced, is the kagent runtime: the claude, codex and
// BYO runtimes run code a sender's turn leaves behind (a repository hook, a
// build target), which would then run under the next sender's credentials. It
// is checked once the caller may share the session, so it tells nobody else
// what the Agent runs. An Agent or Harness that cannot be read refuses the
// share rather than mint it unchecked.
func (s *Service) refuseShareOfSenderCode(ctx context.Context, sessionID, userID string) error {
	if s.agents == nil {
		return nil
	}
	session, err := s.store.GetSession(ctx, sessionID, userID)
	if errors.Is(err, database.ErrNotFound) {
		return serviceerrors.NewNotFound("Session not found", err)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to read Session", err)
	}
	ref := session.GetAgent()
	agent := &kagentv1alpha3.Agent{}
	err = s.agents.Get(ctx, types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, agent)
	if apierrors.IsNotFound(err) {
		return serviceerrors.NewFailedPrecondition("This Session cannot be shared: its agent no longer exists.", err)
	}
	if err != nil {
		return serviceerrors.NewUnavailable("Failed to check whether the Session may be shared", err)
	}
	harness := agent.Spec.Harness
	if harness == nil && agent.Spec.HarnessRef != nil {
		referenced := &kagentv1alpha3.Harness{}
		key := types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.HarnessRef.Name}
		if err := s.agents.Get(ctx, key, referenced); err != nil {
			return serviceerrors.NewUnavailable("Failed to check whether the Session may be shared", err)
		}
		harness = &referenced.Spec
	}
	if harness == nil || harness.Kagent == nil {
		return serviceerrors.NewFailedPrecondition(errShareRunsSenderCode, nil)
	}
	return nil
}
