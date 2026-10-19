package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

func TestActorPolicyKeepsCredentialsOnExactOrigin(t *testing.T) {
	policy, err := ActorEgressPolicy("team", []string{"https://*.example.com", "https://api.example.com:443", "https://api.example.com:8443", "http://api.example.com"}, []egress.Credential{
		{Origin: "https://api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"},
		{Origin: "https://api.example.com:8443", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team/other/key"},
	})
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
			policy, err := compileActorEgressPolicy(t.Context(), "team", access, test.compilers)
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
	policy, err := compileActorEgressPolicy(t.Context(), "team", egress.Access{Destinations: []string{target.Origin}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{target}}, nil)
	require.NoError(t, err, "runtimes forward the caller token without an extension")
	require.Empty(t, policy.GetRules()[0].GetHttps().GetEffects().GetReplaceHeaders())
}
