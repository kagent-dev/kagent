package substrate

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// ActorEgressPolicy compiles HTTP(S) origins into an actor's default allowlist.
// Credential bindings are already canonicalized by the store.
func ActorEgressPolicy(atespace string, destinations []string, credentials []egress.Credential, controlPlaneURL string) (*ateapipb.EgressPolicy, error) {
	access, err := egress.CanonicalAccess(destinations, credentials, nil)
	if err != nil {
		return nil, err
	}
	u, err := egress.ParseOrigin(controlPlaneURL)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime control-plane origin: %w", err)
	}
	runtimeOrigin := u.String()
	effects := map[string]*ateapipb.HttpRuleEffects{}
	for _, binding := range access.Credentials {
		if binding.Origin == runtimeOrigin && binding.Header == "authorization" {
			return nil, fmt.Errorf("runtime authorization header is reserved for Actor JWTs")
		}
		if effects[binding.Origin] == nil {
			effects[binding.Origin] = &ateapipb.HttpRuleEffects{}
		}
		effects[binding.Origin].ReplaceHeaders = append(effects[binding.Origin].ReplaceHeaders, &ateapipb.CredentialHeader{Header: binding.Header, Prefix: binding.Prefix, CredentialUri: binding.URI})
	}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	for _, origin := range access.Destinations {
		if origin == runtimeOrigin {
			if effects[origin] == nil {
				effects[origin] = &ateapipb.HttpRuleEffects{}
			}
			effects[origin].ReplaceHeaders = append(effects[origin].ReplaceHeaders, &ateapipb.CredentialHeader{
				Header: "authorization", Prefix: "Bearer ",
				ActorJwt: &ateapipb.ActorJwtSource{Audiences: []string{runtimeJWTAudience}, ExpirationSeconds: 300},
			})
		}
		target, _ := egress.ParseOrigin(origin) // validated by CanonicalAccess
		port, _ := strconv.ParseInt(target.Port(), 10, 32)
		ports := &ateapipb.Ports{Numbers: []int32{int32(port)}}
		// One rule per origin: exact matches win over wildcard rules, keeping
		// credentials on their exact origin even when a wildcard also grants access.
		if target.Scheme == "https" {
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Https: &ateapipb.HttpsRule{Hostnames: []string{target.Hostname()}, Ports: ports, Effects: effects[origin]}})
		} else {
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Http: &ateapipb.HttpRule{Hostnames: []string{target.Hostname()}, Ports: ports, Effects: effects[origin]}})
		}
	}
	return policy, nil
}

// defaultEgressPolicyCompiler adds the allowlist, Secret credentials, and Actor JWTs. It runs first.
type defaultEgressPolicyCompiler struct {
	access          egress.Access
	controlPlaneURL string
}

func (c defaultEgressPolicyCompiler) Compile(_ context.Context, input egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
	base, err := ActorEgressPolicy(input.Atespace, c.access.Destinations, c.access.Credentials, c.controlPlaneURL)
	if err != nil {
		return err
	}
	policy.Rules = append(policy.Rules, base.Rules...)
	return nil
}

// compileActorEgressPolicy runs the built-in compiler, then each extension.
// Extensions may change anything; the result must still validate, and with
// any extension every tokenPassthrough target must end bound.
func compileActorEgressPolicy(ctx context.Context, atespace string, access egress.Access, controlPlaneURL string, compilers []egresspolicy.Compiler) (*ateapipb.EgressPolicy, error) {
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	chain := append([]egresspolicy.Compiler{defaultEgressPolicyCompiler{access: access, controlPlaneURL: controlPlaneURL}}, compilers...)
	for _, compiler := range chain {
		input := egresspolicy.Input{Atespace: atespace, GatewayTargets: slices.Clone(access.TokenPassthroughTargets)}
		if err := compiler.Compile(ctx, input, policy); err != nil {
			return nil, fmt.Errorf("compile egress policy with %T: %w", compiler, err)
		}
	}
	headers, err := policyHeaders(policy)
	if err != nil {
		return nil, err
	}
	var bindings []egress.Credential
	for key, header := range headers {
		if header.GetActorJwt() != nil {
			continue
		}
		origin, _, _ := strings.Cut(key, "\x00")
		bindings = append(bindings, egress.Credential{Origin: origin, Header: header.GetHeader(), Prefix: header.GetPrefix(), URI: header.GetCredentialUri()})
	}
	if _, err := egress.CanonicalCredentials(bindings); err != nil {
		return nil, err
	}
	for _, target := range access.TokenPassthroughTargets {
		if len(compilers) != 0 && headers[target.Origin+"\x00"+target.Header] == nil {
			return nil, fmt.Errorf("gateway target %s header %s has no credential", target.Origin, target.Header)
		}
	}
	return policy, nil
}

// policyHeaders returns credential headers keyed by origin and lowercase header.
func policyHeaders(policy *ateapipb.EgressPolicy) (map[string]*ateapipb.CredentialHeader, error) {
	headers := map[string]*ateapipb.CredentialHeader{}
	for _, rule := range policy.GetRules() {
		scheme, hostnames, ports, effects := "https", rule.GetHttps().GetHostnames(), rule.GetHttps().GetPorts(), rule.GetHttps().GetEffects()
		if rule.GetHttp() != nil {
			scheme, hostnames, ports, effects = "http", rule.GetHttp().GetHostnames(), rule.GetHttp().GetPorts(), rule.GetHttp().GetEffects()
		}
		if len(effects.GetReplaceHeaders()) == 0 {
			continue
		}
		if len(hostnames) != 1 || len(ports.GetNumbers()) != 1 {
			return nil, fmt.Errorf("credential headers require a rule with one hostname and port")
		}
		u := url.URL{Scheme: scheme, Host: net.JoinHostPort(hostnames[0], strconv.Itoa(int(ports.GetNumbers()[0])))}
		origin := egress.Origin(&u)
		for _, header := range effects.GetReplaceHeaders() {
			key := origin + "\x00" + strings.ToLower(header.GetHeader())
			if headers[key] != nil {
				return nil, fmt.Errorf("duplicate credential header %s on %s", header.GetHeader(), origin)
			}
			headers[key] = header
		}
	}
	return headers, nil
}
