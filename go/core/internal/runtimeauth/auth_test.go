package runtimeauth

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestRuntimeJWTAuthentication(t *testing.T) {
	i := jwttest.New(t)
	v, err := substrate.NewActorJWTVerifier(t.Context(), i.URL, i.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	a := NewAuthenticator(v)
	id := uuid.NewString()
	valid := "Bearer " + i.Token(t, i.Claims("team-a", "session-"+id, "actor-uid"))
	for _, test := range []struct {
		name   string
		values []string
		valid  bool
	}{
		{"valid", []string{valid}, true},
		{"missing", nil, false},
		{"duplicate", []string{valid, valid}, false},
		{"placeholder", []string{"Bearer placeholder"}, false},
		{"wrong scheme", []string{"Basic abc"}, false},
		{"empty token", []string{"Bearer "}, false},
		{"invalid session", []string{"Bearer " + i.Token(t, i.Claims("team-a", "session-invalid", "actor-uid"))}, false},
		{"sandbox actor", []string{"Bearer " + i.Token(t, i.Claims("team-a", "sandbox-"+id, "actor-uid"))}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			// The removed header never supplies or overrides authenticated identity.
			headers.Set("x-kagent-insecure-runtime-identity", "other/session-"+uuid.NewString()+"/forged")
			for _, value := range test.values {
				headers.Add("authorization", value)
			}
			session, err := a.Authenticate(t.Context(), headers, nil)
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, runtimeSession{sessionID: id, atespace: "team-a", actorUID: "actor-uid"}, session)
		})
	}
}
