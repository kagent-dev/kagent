package a2a

import (
	"context"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func ctxWithUser(userID string) context.Context {
	return pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{
		P: pkgauth.Principal{User: pkgauth.User{ID: userID}},
	})
}

func ctxWithUserAndShare(userID string) context.Context {
	ctx := ctxWithUser(userID)
	return pkgauth.ShareContextTo(ctx, &pkgauth.ShareContext{
		Token:     "tok",
		SessionID: "sess-1",
		UserID:    "owner-id",
		ReadOnly:  false,
	})
}

func TestInjectInitiatedBy(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		wantMetaKey   bool
		wantInitiator string
	}{
		{
			name:        "no share context — metadata not set",
			ctx:         ctxWithUser("caller-id"),
			wantMetaKey: false,
		},
		{
			name:          "share context present — sets initiated_by to caller user ID",
			ctx:           ctxWithUserAndShare("caller-id"),
			wantMetaKey:   true,
			wantInitiator: "caller-id",
		},
		{
			name:        "no auth session — metadata not set",
			ctx:         pkgauth.ShareContextTo(context.Background(), &pkgauth.ShareContext{Token: "tok"}),
			wantMetaKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &a2atype.Message{}
			injectInitiatedBy(tt.ctx, msg)

			if !tt.wantMetaKey {
				if msg.Metadata != nil {
					if _, ok := msg.Metadata["initiated_by"]; ok {
						t.Errorf("expected initiated_by not set, but it was: %v", msg.Metadata["initiated_by"])
					}
				}
				return
			}

			if msg.Metadata == nil {
				t.Fatal("expected Metadata to be set, got nil")
			}
			got, ok := msg.Metadata["initiated_by"]
			if !ok {
				t.Fatal("expected initiated_by key in Metadata")
			}
			if got != tt.wantInitiator {
				t.Errorf("initiated_by = %q, want %q", got, tt.wantInitiator)
			}
		})
	}
}
