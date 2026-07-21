package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
)

func ctxWithShare(userID string, readOnly bool) context.Context {
	return pkgauth.ShareContextTo(ctxWithUser(userID), &pkgauth.ShareContext{
		Token:     "tok",
		SessionID: "sess-1",
		UserID:    "owner-id",
		ReadOnly:  readOnly,
	})
}

func TestRequireWritableShare(t *testing.T) {
	require.NoError(t, requireWritableShare(context.Background()), "no share token: allowed")
	require.NoError(t, requireWritableShare(ctxWithShare("caller", false)), "read-write share: allowed")
	require.Error(t, requireWritableShare(ctxWithShare("caller", true)), "read-only share: rejected")
}

// A read-only share must not reach the upstream client on any mutating method.
// The handler is built with a nil client on purpose: the guard has to return
// before any client call, so a nil dereference would mean the guard is missing.
func TestReadOnlyShareRejectsWrites(t *testing.T) {
	h := &PassthroughRequestHandler{}
	ctx := ctxWithShare("caller", true)

	_, err := h.CancelTask(ctx, &a2atype.CancelTaskRequest{})
	require.Error(t, err, "CancelTask")

	_, err = h.SendMessage(ctx, &a2atype.SendMessageRequest{})
	require.Error(t, err, "SendMessage")

	_, err = h.CreateTaskPushConfig(ctx, &a2atype.PushConfig{})
	require.Error(t, err, "CreateTaskPushConfig")

	err = h.DeleteTaskPushConfig(ctx, &a2atype.DeleteTaskPushConfigRequest{})
	require.Error(t, err, "DeleteTaskPushConfig")

	var streamErr error
	for _, e := range h.SendStreamingMessage(ctx, &a2atype.SendMessageRequest{}) {
		streamErr = e
		break
	}
	require.Error(t, streamErr, "SendStreamingMessage")
}
