package session

import (
	"context"
	"crypto/sha256"
	"errors"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// ShareStore resolves the digest of a Session share token.
type ShareStore interface {
	GetSessionShareByTokenHash(context.Context, []byte) (*apiv1alpha1.SessionShare, string, error)
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
	share, owner, err := store.GetSessionShareByTokenHash(ctx, digest[:])
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
