package auth

import (
	"context"
	"testing"
)

func TestShareContext(t *testing.T) {
	tests := []struct {
		name   string
		stored *ShareContext
		wantOK bool
	}{
		{
			name:   "empty context returns nil and false",
			stored: nil, // not stored at all — use background context directly
			wantOK: false,
		},
		{
			name: "store and retrieve read-only context",
			stored: &ShareContext{
				Token:     "tok-abc",
				SessionID: "sess-123",
				UserID:    "user-456",
				ReadOnly:  true,
			},
			wantOK: true,
		},
		{
			name: "store and retrieve read-write context",
			stored: &ShareContext{
				Token:     "tok-xyz",
				SessionID: "sess-789",
				UserID:    "user-001",
				ReadOnly:  false,
			},
			wantOK: true,
		},
		{
			name:   "nil value stored returns false",
			stored: (*ShareContext)(nil), // explicitly store nil via ShareContextTo
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.name == "empty context returns nil and false" {
				ctx = context.Background()
			} else {
				ctx = ShareContextTo(context.Background(), tt.stored)
			}

			got, ok := ShareContextFrom(ctx)

			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				if got != nil {
					t.Errorf("expected nil ShareContext, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil ShareContext, got nil")
			}
			if got.Token != tt.stored.Token {
				t.Errorf("Token = %q, want %q", got.Token, tt.stored.Token)
			}
			if got.SessionID != tt.stored.SessionID {
				t.Errorf("SessionID = %q, want %q", got.SessionID, tt.stored.SessionID)
			}
			if got.UserID != tt.stored.UserID {
				t.Errorf("UserID = %q, want %q", got.UserID, tt.stored.UserID)
			}
			if got.ReadOnly != tt.stored.ReadOnly {
				t.Errorf("ReadOnly = %v, want %v", got.ReadOnly, tt.stored.ReadOnly)
			}
		})
	}
}
