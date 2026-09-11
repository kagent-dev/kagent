package models

import (
	"context"
	"testing"
	"time"
)

type fakeExchangedTokens struct {
	bySession map[string]string
	calls     int
}

func (f *fakeExchangedTokens) GetTokenForSession(sessionID string) string {
	f.calls++
	return f.bySession[sessionID]
}

func TestPassthroughToken(t *testing.T) {
	const (
		inbound   = "INBOUND-CALLER-TOKEN"
		exchanged = "EXCHANGED-STS-TOKEN"
		sessionID = "session-1"
	)

	provider := &fakeExchangedTokens{bySession: map[string]string{sessionID: exchanged}}
	empty := &fakeExchangedTokens{bySession: map[string]string{}}

	tests := []struct {
		name              string
		apiKeyPassthrough bool
		buildCtx          func() context.Context
		wantToken         string
		wantOK            bool
	}{
		{
			name:              "exchanged token wins over the caller's own",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				ctx = context.WithValue(ctx, SessionIDKey, sessionID)
				return context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(provider))
			},
			wantToken: exchanged,
			wantOK:    true,
		},
		{
			name:              "exchanged token survives a deadline-wrapped context",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				ctx = context.WithValue(ctx, SessionIDKey, sessionID)
				ctx = context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(provider))
				// http.Client re-wraps the request context whenever Timeout > 0,
				// which is always for model transports. See kagent #2795.
				wrapped, cancel := context.WithTimeout(ctx, time.Minute)
				t.Cleanup(cancel)
				return wrapped
			},
			wantToken: exchanged,
			wantOK:    true,
		},
		{
			name:              "falls back to the caller's token when no token is cached for the session",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				ctx = context.WithValue(ctx, SessionIDKey, sessionID)
				return context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(empty))
			},
			wantToken: inbound,
			wantOK:    true,
		},
		{
			name:              "falls back to the caller's token when no session is stamped",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				return context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(provider))
			},
			wantToken: inbound,
			wantOK:    true,
		},
		{
			name:              "falls back to the caller's token when no provider is stamped",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				return context.WithValue(ctx, SessionIDKey, sessionID)
			},
			wantToken: inbound,
			wantOK:    true,
		},
		{
			name:              "nothing when passthrough is disabled",
			apiKeyPassthrough: false,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
				ctx = context.WithValue(ctx, SessionIDKey, sessionID)
				return context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(provider))
			},
			wantToken: "",
			wantOK:    false,
		},
		{
			name:              "nothing when neither source has a token",
			apiKeyPassthrough: true,
			buildCtx: func() context.Context {
				ctx := context.WithValue(context.Background(), SessionIDKey, sessionID)
				return context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(empty))
			},
			wantToken: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, ok := PassthroughToken(tt.buildCtx(), tt.apiKeyPassthrough)
			if token != tt.wantToken || ok != tt.wantOK {
				t.Errorf("PassthroughToken() = (%q, %v), want (%q, %v)", token, ok, tt.wantToken, tt.wantOK)
			}
		})
	}
}

func TestPassthroughTokenDisabledSkipsProvider(t *testing.T) {
	provider := &fakeExchangedTokens{bySession: map[string]string{"session-1": "EXCHANGED-STS-TOKEN"}}
	ctx := context.WithValue(context.Background(), SessionIDKey, "session-1")
	ctx = context.WithValue(ctx, ExchangedTokenProviderKey, ExchangedTokenProvider(provider))

	if _, ok := PassthroughToken(ctx, false); ok {
		t.Fatal("PassthroughToken() returned a token with passthrough disabled")
	}
	if provider.calls != 0 {
		t.Errorf("provider consulted %d times with passthrough disabled, want 0", provider.calls)
	}
}
