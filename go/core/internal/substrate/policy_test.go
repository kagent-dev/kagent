package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

const testControlPlaneURL = "http://kagent-controller.kagent.svc:8083"

func TestActorTokenHeaderUsesGatewayMinting(t *testing.T) {
	for _, origin := range []string{"http://mcp.example.com:80", "https://mcp.example.com:443"} {
		t.Run(origin, func(t *testing.T) {
			credential := egress.Credential{Origin: origin, Header: "Authorization", Prefix: "Bearer ", ActorToken: &egress.ActorToken{Audiences: []string{"mcp"}, ExpirationSeconds: 900}}
			policy, err := ActorEgressPolicy("team", []string{testControlPlaneURL, origin}, []egress.Credential{credential}, testControlPlaneURL)
			require.NoError(t, err)
			headers, err := policyHeaders(policy)
			require.NoError(t, err)
			outbound := headers[origin+"\x00authorization"]
			require.NotNil(t, outbound)
			require.Equal(t, []string{"mcp"}, outbound.ActorJwt.Audiences)
			require.EqualValues(t, 900, outbound.ActorJwt.ExpirationSeconds)
			require.Empty(t, outbound.CredentialUri)
			require.Equal(t, "Bearer ", outbound.Prefix)
			internal := headers[testControlPlaneURL+"\x00authorization"]
			require.Equal(t, []string{runtimeJWTAudience}, internal.ActorJwt.Audiences)
		})
	}
}

func TestActorPolicyRequiresControlPlaneOrigin(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{"missing", ""},
		{"invalid", "kagent-controller.kagent.svc:8083"},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := ActorEgressPolicy("team", nil, nil, test.url)
			require.ErrorContains(t, err, "invalid runtime control-plane origin")
			require.Nil(t, policy)
		})
	}
}

func TestActorPolicyKeepsCredentialsOnExactOrigin(t *testing.T) {
	policy, err := ActorEgressPolicy("team", []string{"https://*.example.com", "https://api.example.com:443", "https://api.example.com:8443", "http://api.example.com"}, []egress.Credential{
		{Origin: "https://api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"},
		{Origin: "https://api.example.com:8443", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team/other/key"},
	}, testControlPlaneURL)
	require.NoError(t, err)
	require.Len(t, policy.Rules, 4)
	require.Empty(t, policy.Rules[0].GetHttp().GetEffects().GetReplaceHeaders())
	require.Equal(t, []string{"*.example.com"}, policy.Rules[1].GetHttps().GetHostnames())
	require.Empty(t, policy.Rules[1].GetHttps().GetEffects().GetReplaceHeaders())
	for i, header := range []string{"authorization", "x-api-key"} {
		rule := policy.Rules[i+2].GetHttps()
		require.Equal(t, []string{"api.example.com"}, rule.GetHostnames())
		require.Len(t, rule.GetEffects().GetReplaceHeaders(), 1)
		require.Equal(t, header, rule.GetEffects().GetReplaceHeaders()[0].Header)
	}
	require.Equal(t, []int32{443}, policy.Rules[2].GetHttps().GetPorts().GetNumbers())
	require.Equal(t, []int32{8443}, policy.Rules[3].GetHttps().GetPorts().GetNumbers())
}

type compilerFunc func(context.Context, egresspolicy.Input, *ateapipb.EgressPolicy) error

func (f compilerFunc) Compile(ctx context.Context, input egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
	return f(ctx, input, policy)
}

func TestCompileActorEgressPolicyExtensions(t *testing.T) {
	secret := egress.Credential{Origin: "https://other.example.com:443", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team/auth/token"}
	target := egresspolicy.GatewayTarget{Origin: "https://api.example.com:443", Header: "authorization", Prefix: "Bearer "}
	access := egress.Access{Destinations: []string{"https://api.example.com:443", "https://other.example.com:443"}, Credentials: []egress.Credential{secret}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{target}}
	addHeader := func(host, header, prefix, uri string) compilerFunc {
		return func(_ context.Context, _ egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
			for _, rule := range policy.GetRules() {
				if https := rule.GetHttps(); https != nil && https.GetHostnames()[0] == host {
					if https.Effects == nil {
						https.Effects = &ateapipb.HttpRuleEffects{}
					}
					https.Effects.ReplaceHeaders = append(https.Effects.ReplaceHeaders, &ateapipb.CredentialHeader{Header: header, Prefix: prefix, CredentialUri: uri})
				}
			}
			return nil
		}
	}
	const callerURI = "ate-secret://caller-token.example/v1/https/api.example.com:443"
	bind := addHeader("api.example.com", "Authorization", "Bearer ", callerURI)
	for _, test := range []struct {
		name      string
		compilers []egresspolicy.Compiler
		wantErr   string
	}{
		{name: "binds the target", compilers: []egresspolicy.Compiler{bind}},
		{name: "extensions run after the built-in policy with their own input", compilers: []egresspolicy.Compiler{
			compilerFunc(func(_ context.Context, input egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
				input.GatewayTargets[0].Header = "changed"
				if policy.GetMetadata().GetAtespace() != "team" || len(policy.GetRules()) != 2 {
					return errors.New("built-in policy missing")
				}
				return nil
			}),
			compilerFunc(func(ctx context.Context, input egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
				if input.GatewayTargets[0] != target {
					return errors.New("input shared between compilers")
				}
				return bind(ctx, input, policy)
			}),
		}},
		{name: "unbound target", compilers: []egresspolicy.Compiler{compilerFunc(func(context.Context, egresspolicy.Input, *ateapipb.EgressPolicy) error { return nil })}, wantErr: "has no credential"},
		{name: "compiler error", compilers: []egresspolicy.Compiler{compilerFunc(func(context.Context, egresspolicy.Input, *ateapipb.EgressPolicy) error {
			return errors.New("denied")
		})}, wantErr: "denied"},
		{name: "invalid URI", compilers: []egresspolicy.Compiler{addHeader("api.example.com", "authorization", "Bearer ", "https://caller-token.example/token")}, wantErr: "credential"},
		{name: "rebinding the target", compilers: []egresspolicy.Compiler{bind, bind}, wantErr: "duplicate credential header"},
		{name: "extensions may change earlier output", compilers: []egresspolicy.Compiler{bind, compilerFunc(func(_ context.Context, _ egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
			policy.GetRules()[1].GetHttps().GetEffects().GetReplaceHeaders()[0] = &ateapipb.CredentialHeader{Header: "x-api-key", ActorJwt: &ateapipb.ActorJwtSource{}}
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Https: &ateapipb.HttpsRule{Hostnames: []string{"*"}}})
			return nil
		})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := compileActorEgressPolicy(t.Context(), "team", access, testControlPlaneURL, test.compilers)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.Nil(t, policy)
				return
			}
			require.NoError(t, err)
			require.Equal(t, callerURI, policy.GetRules()[0].GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
		})
	}
}

func TestCompileActorEgressPolicyWithoutExtensionsLeavesTargetsUnbound(t *testing.T) {
	target := egresspolicy.GatewayTarget{Origin: "https://api.example.com:443", Header: "authorization", Prefix: "Bearer "}
	policy, err := compileActorEgressPolicy(t.Context(), "team", egress.Access{Destinations: []string{target.Origin}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{target}}, testControlPlaneURL, nil)
	require.NoError(t, err, "runtimes forward the caller token without an extension")
	require.Empty(t, policy.GetRules()[0].GetHttps().GetEffects().GetReplaceHeaders())
}

func TestActorPolicyInjectsRuntimeJWTOnlyAtControlPlane(t *testing.T) {
	const controlPlane = testControlPlaneURL
	policy, err := ActorEgressPolicy("team", []string{controlPlane, "https://models.example"}, nil, "http://KAGENT-CONTROLLER.kagent.svc.:8083")
	require.NoError(t, err)
	headers := policy.Rules[0].GetHttp().GetEffects().GetReplaceHeaders()
	require.Len(t, headers, 1)
	require.Equal(t, "authorization", headers[0].Header)
	require.Equal(t, "Bearer ", headers[0].Prefix)
	require.Empty(t, headers[0].CredentialUri)
	require.Equal(t, []string{runtimeJWTAudience}, headers[0].ActorJwt.Audiences)
	require.EqualValues(t, 300, headers[0].ActorJwt.ExpirationSeconds)
	require.Empty(t, policy.Rules[1].GetHttps().GetEffects().GetReplaceHeaders())
	policy, err = ActorEgressPolicy("team", nil, nil, controlPlane)
	require.NoError(t, err)
	require.Empty(t, policy.Rules, "runtime authentication must not grant sandbox network access")
	_, err = ActorEgressPolicy("team", []string{controlPlane}, []egress.Credential{
		{Origin: controlPlane, Header: "Authorization", URI: "ate-secret://k8s.io/default/team/auth/token"},
	}, controlPlane)
	require.Error(t, err, "a credential binding must not override runtime identity")
}
