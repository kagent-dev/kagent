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

func (f *fakeExchangedTokens) ExchangedToken(ctx context.Context) (string, bool) {
	f.calls++
	sessionID, _ := ctx.Value(SessionIDKey).(string)
	token, ok := f.bySession[sessionID]
	return token, ok
}

func TestPassthroughToken(t *testing.T) {
	const (
		inbound   = "INBOUND-CALLER-TOKEN"
		exchanged = "EXCHANGED-STS-TOKEN"
		sessionID = "session-1"
	)

	provider := &fakeExchangedTokens{bySession: map[string]string{sessionID: exchanged}}
	empty := &fakeExchangedTokens{bySession: map[string]string{}}

	callerCtx := func() context.Context {
		ctx := context.WithValue(context.Background(), BearerTokenKey, inbound)
		return context.WithValue(ctx, SessionIDKey, sessionID)
	}

	tests := []struct {
		name              string
		apiKeyPassthrough bool
		provider          ExchangedTokenProvider
		buildCtx          func(t *testing.T) context.Context
		wantToken         string
		wantOK            bool
	}{
		{
			name:              "exchanged token wins over the caller's own",
			apiKeyPassthrough: true,
			provider:          provider,
			buildCtx:          func(*testing.T) context.Context { return callerCtx() },
			wantToken:         exchanged,
			wantOK:            true,
		},
		{
			name:              "exchanged token survives a deadline-wrapped context",
			apiKeyPassthrough: true,
			provider:          provider,
			buildCtx: func(t *testing.T) context.Context {
				wrapped, cancel := context.WithTimeout(callerCtx(), time.Minute)
				t.Cleanup(cancel)
				return wrapped
			},
			wantToken: exchanged,
			wantOK:    true,
		},
		{
			name:              "falls back to the caller's token when the provider has none",
			apiKeyPassthrough: true,
			provider:          empty,
			buildCtx:          func(*testing.T) context.Context { return callerCtx() },
			wantToken:         inbound,
			wantOK:            true,
		},
		{
			// The dependency is explicit, so an absent provider is a nil argument
			// rather than a context value nobody stamped.
			name:              "falls back to the caller's token when no provider is injected",
			apiKeyPassthrough: true,
			buildCtx:          func(*testing.T) context.Context { return callerCtx() },
			wantToken:         inbound,
			wantOK:            true,
		},
		{
			name:              "falls back to the caller's token when no session is stamped",
			apiKeyPassthrough: true,
			provider:          provider,
			buildCtx: func(*testing.T) context.Context {
				return context.WithValue(context.Background(), BearerTokenKey, inbound)
			},
			wantToken: inbound,
			wantOK:    true,
		},
		{
			name:              "nothing when passthrough is disabled",
			apiKeyPassthrough: false,
			provider:          provider,
			buildCtx:          func(*testing.T) context.Context { return callerCtx() },
			wantToken:         "",
			wantOK:            false,
		},
		{
			name:              "nothing when neither source has a token",
			apiKeyPassthrough: true,
			provider:          empty,
			buildCtx: func(*testing.T) context.Context {
				return context.WithValue(context.Background(), SessionIDKey, sessionID)
			},
			wantToken: "",
			wantOK:    false,
		},
		{
			// A later turn on the same session, presenting no caller token.
			name:              "nothing when the request presented no caller token",
			apiKeyPassthrough: true,
			provider:          provider,
			buildCtx: func(*testing.T) context.Context {
				return context.WithValue(context.Background(), SessionIDKey, sessionID)
			},
			wantToken: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, ok := PassthroughToken(tt.buildCtx(t), tt.apiKeyPassthrough, tt.provider)
			if token != tt.wantToken || ok != tt.wantOK {
				t.Errorf("PassthroughToken() = (%q, %v), want (%q, %v)", token, ok, tt.wantToken, tt.wantOK)
			}
		})
	}
}

func TestPassthroughTokenDisabledSkipsProvider(t *testing.T) {
	provider := &fakeExchangedTokens{bySession: map[string]string{"session-1": "EXCHANGED-STS-TOKEN"}}
	ctx := context.WithValue(context.Background(), SessionIDKey, "session-1")

	if _, ok := PassthroughToken(ctx, false, provider); ok {
		t.Fatal("PassthroughToken() returned a token with passthrough disabled")
	}
	if provider.calls != 0 {
		t.Errorf("provider consulted %d times with passthrough disabled, want 0", provider.calls)
	}
}

// A request with no caller token must not reach the provider at all: the
// exchanged token replaces the caller's, it never stands in for its absence.
func TestPassthroughTokenWithoutACallerTokenSkipsProvider(t *testing.T) {
	provider := &fakeExchangedTokens{bySession: map[string]string{"session-1": "EXCHANGED-STS-TOKEN"}}
	ctx := context.WithValue(context.Background(), SessionIDKey, "session-1")

	if _, ok := PassthroughToken(ctx, true, provider); ok {
		t.Fatal("PassthroughToken() returned a token for a request with no caller token")
	}
	if provider.calls != 0 {
		t.Errorf("provider consulted %d times with no caller token, want 0", provider.calls)
	}
}

// No STS configured means no provider argument, and the caller's own token goes
// out unchanged. PassthroughToken screens on the interface being nil, so callers
// holding a concrete plugin must convert through sts.ExchangedTokens rather than
// assigning a possibly-nil pointer straight into the interface.
func TestPassthroughTokenWithoutAProvider(t *testing.T) {
	ctx := context.WithValue(context.Background(), BearerTokenKey, "INBOUND")

	token, ok := PassthroughToken(ctx, true, nil)
	if !ok || token != "INBOUND" {
		t.Fatalf("PassthroughToken() = (%q, %v), want (%q, true)", token, ok, "INBOUND")
	}
}
