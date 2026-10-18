package credentialprovider_test

import (
	"encoding/base64"
	"testing"

	"github.com/kagent-dev/kagent/go/core/test/credentialprovider"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const actor = "spiffe://cluster.local/actor/kagent/session-1"

func TestAnswerNamesTheActor(t *testing.T) {
	provider := &credentialprovider.Provider{Authority: "credentials.e2e.kagent.dev", Refused: []string{"refused"}}

	git, err := provider.Answer("ate-secret://credentials.e2e.kagent.dev/forge/git", actor)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("x-access-token:e2e-person-session-1")), string(git))
	named, err := credentialprovider.ActorOf("Basic " + string(git))
	require.NoError(t, err)
	require.Equal(t, "session-1", named)

	api, err := provider.Answer("ate-secret://credentials.e2e.kagent.dev/forge/api", actor)
	require.NoError(t, err)
	require.Equal(t, "e2e-person-session-1", string(api))
	named, err = credentialprovider.ActorOf("Bearer " + string(api))
	require.NoError(t, err)
	require.Equal(t, "session-1", named)
}

func TestAnswerRefuses(t *testing.T) {
	provider := &credentialprovider.Provider{Authority: "credentials.e2e.kagent.dev", Refused: []string{"refused"}}
	for uri, code := range map[string]codes.Code{
		"ate-secret://credentials.e2e.kagent.dev/refused/api": codes.PermissionDenied,
		"ate-secret://elsewhere.example.com/forge/git":        codes.InvalidArgument,
		"ate-secret://credentials.e2e.kagent.dev/forge/token": codes.InvalidArgument,
		"ate-secret://credentials.e2e.kagent.dev/forge":       codes.InvalidArgument,
		"https://credentials.e2e.kagent.dev/forge/git":        codes.InvalidArgument,
	} {
		_, err := provider.Answer(uri, actor)
		require.Equal(t, code, status.Code(err), "%s: %v", uri, err)
	}
	_, err := provider.Answer("ate-secret://credentials.e2e.kagent.dev/forge/git", "spiffe://cluster.local/ns/kagent/sa/default")
	require.Equal(t, codes.InvalidArgument, status.Code(err), "a workload that is not an Actor")
	_, err = provider.Answer("ate-secret://credentials.e2e.kagent.dev/refused/git", actor)
	require.ErrorContains(t, err, "the person of session-1 has not connected refused")
}

func TestActorOfRefusesOtherCredentials(t *testing.T) {
	for _, authorization := range []string{
		"Bearer kagent-credential-injected",
		"Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:kagent-credential-injected")),
		"Basic " + base64.StdEncoding.EncodeToString([]byte("someone:e2e-person-session-1")),
		"token e2e-person-session-1",
		"Bearer e2e-person-",
		"",
	} {
		_, err := credentialprovider.ActorOf(authorization)
		require.Error(t, err, authorization)
	}
}
