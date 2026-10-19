package substrate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

func TestActorPolicyCompilesTLSPassthrough(t *testing.T) {
	credential := egress.Credential{Hostname: "api.example.com", Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}
	policy, err := ActorEgressPolicy("team", []string{"tls://db.example.com:5432", "tls://*.example.com:443", "https://api.example.com", "tls://api.example.com:6379"}, []egress.Credential{credential})
	require.NoError(t, err)
	require.Len(t, policy.Rules, 4)
	require.Equal(t, []string{"api.example.com"}, policy.Rules[0].GetHttps().GetHostnames())
	require.Len(t, policy.Rules[0].GetHttps().GetEffects().GetReplaceHeaders(), 1)
	for i, want := range []struct {
		host string
		port int32
	}{{"*.example.com", 443}, {"api.example.com", 6379}, {"db.example.com", 5432}} {
		rule := policy.Rules[i+1]
		require.Nil(t, rule.GetHttp())
		require.Nil(t, rule.GetHttps())
		require.Equal(t, []string{want.host}, rule.GetTlsPassthrough().GetHostnames())
		require.Equal(t, []int32{want.port}, rule.GetTlsPassthrough().GetPorts().GetNumbers())
	}

	changed, err := ActorEgressPolicy("team", []string{"tls://db.example.com:6432"}, nil)
	require.NoError(t, err)
	require.Equal(t, []int32{6432}, changed.Rules[0].GetTlsPassthrough().GetPorts().GetNumbers())

	_, err = ActorEgressPolicy("team", []string{"tls://db.example.com:443", "https://db.example.com"}, nil)
	require.ErrorContains(t, err, "both HTTPS and TLS")
	// Passthrough is not decrypted: a credential cannot bind to a host only it allows.
	_, err = ActorEgressPolicy("team", []string{"tls://db.example.com:443"}, []egress.Credential{{Hostname: "db.example.com", Header: "authorization", URI: credential.URI}})
	require.ErrorContains(t, err, "is not allowed")
}
