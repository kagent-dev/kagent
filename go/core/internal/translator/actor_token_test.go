package translator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestActorTokenHeaderCompilation(t *testing.T) {
	headers := []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{ActorToken: &v1alpha3.ActorToken{Audiences: []string{"mcp"}}}}}
	access, err := CompileCredentialHeaders("https://mcp.example.com/tools", "team", headers)
	require.NoError(t, err)
	require.Empty(t, access.TokenPassthroughTargets)
	require.Len(t, access.Credentials, 1)
	require.Empty(t, access.Credentials[0].URI)
	require.Equal(t, []string{"mcp"}, access.Credentials[0].ActorToken.Audiences)
	require.EqualValues(t, 900, access.Credentials[0].ActorToken.ExpirationSeconds)
	runtime, passthrough, err := RuntimeCredentialHeaders(headers, nil)
	require.NoError(t, err)
	require.Empty(t, passthrough, "Actor tokens need no invocation caller credential")
	require.Equal(t, "Bearer "+CredentialPlaceholder, runtime["Authorization"])
	headers[0].ValueFrom.ActorToken.Audiences = []string{"kagent-runtime"}
	_, err = CompileCredentialHeaders("https://mcp.example.com/tools", "team", headers)
	require.Error(t, err)
	headers[0].ValueFrom.TokenPassthrough = &v1alpha3.TokenPassthrough{}
	_, err = CompileCredentialHeaders("https://mcp.example.com/tools", "team", headers)
	require.ErrorContains(t, err, "exactly one source")
}
