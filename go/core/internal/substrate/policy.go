package substrate

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// ActorEgressPolicy compiles HTTP(S) origins into an actor's default allowlist.
// Credential bindings are already canonicalized by the store.
func ActorEgressPolicy(atespace string, destinations []string, credentials []egress.Credential) (*ateapipb.EgressPolicy, error) {
	access, err := egress.CanonicalAccess(destinations, credentials, nil)
	if err != nil {
		return nil, err
	}
	effects := map[string]*ateapipb.HttpRuleEffects{}
	for _, binding := range access.Credentials {
		if effects[binding.Origin] == nil {
			effects[binding.Origin] = &ateapipb.HttpRuleEffects{}
		}
		effects[binding.Origin].ReplaceHeaders = append(effects[binding.Origin].ReplaceHeaders, &ateapipb.CredentialHeader{Header: binding.Header, Prefix: binding.Prefix, CredentialUri: binding.URI})
	}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	for _, origin := range access.Destinations {
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

// defaultEgressPolicyCompiler adds the allowlist and Secret credentials. It runs first.
type defaultEgressPolicyCompiler struct{ access egress.Access }

func (c defaultEgressPolicyCompiler) Compile(_ context.Context, input egresspolicy.Input, policy *ateapipb.EgressPolicy) error {
	base, err := ActorEgressPolicy(input.Atespace, c.access.Destinations, c.access.Credentials)
	if err != nil {
		return err
	}
	policy.Rules = append(policy.Rules, base.Rules...)
	return nil
}

// compileActorEgressPolicy runs the built-in compiler, then each extension.
// With any extension, every tokenPassthrough target must end bound.
func compileActorEgressPolicy(ctx context.Context, atespace string, access egress.Access, gatewayTargets []egresspolicy.GatewayTarget, compilers []egresspolicy.Compiler) (*ateapipb.EgressPolicy, error) {
	targets := map[string]string{}
	for _, target := range gatewayTargets {
		targets[target.Origin+"\x00"+target.Header] = target.Prefix
	}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	chain := append([]egresspolicy.Compiler{defaultEgressPolicyCompiler{access: access}}, compilers...)
	for i, compiler := range chain {
		before := proto.CloneOf(policy)
		input := egresspolicy.Input{Atespace: atespace, GatewayTargets: slices.Clone(gatewayTargets)}
		if err := compiler.Compile(ctx, input, policy); err != nil {
			return nil, fmt.Errorf("compile egress policy with %T: %w", compiler, err)
		}
		if i == 0 {
			continue
		}
		if err := checkAddedEffects(before, policy, targets); err != nil {
			return nil, fmt.Errorf("egress policy compiler %T: %w", compiler, err)
		}
	}
	_, headers, err := policyHeaders(policy)
	if err != nil {
		return nil, err
	}
	var bindings []egress.Credential
	for key, header := range headers {
		origin, _, _ := strings.Cut(key, "\x00")
		bindings = append(bindings, egress.Credential{Origin: origin, Header: header.GetHeader(), Prefix: header.GetPrefix(), URI: header.GetCredentialUri()})
	}
	if _, err := egress.CanonicalCredentials(bindings); err != nil {
		return nil, err
	}
	for _, target := range gatewayTargets {
		if len(compilers) != 0 && headers[target.Origin+"\x00"+target.Header] == nil {
			return nil, fmt.Errorf("gateway target %s header %s has no credential", target.Origin, target.Header)
		}
	}
	return policy, nil
}

// checkAddedEffects allows an extension only to bind unbound targets.
func checkAddedEffects(before, after *ateapipb.EgressPolicy, targets map[string]string) error {
	beforeRules, beforeHeaders, err := policyHeaders(before)
	if err != nil {
		return err
	}
	afterRules, afterHeaders, err := policyHeaders(after)
	if err != nil {
		return err
	}
	if !proto.Equal(beforeRules, afterRules) {
		return fmt.Errorf("changed policy metadata or rules")
	}
	for key, header := range beforeHeaders {
		if !proto.Equal(header, afterHeaders[key]) {
			return fmt.Errorf("changed or removed an existing credential header")
		}
	}
	for key, header := range afterHeaders {
		if beforeHeaders[key] != nil {
			continue
		}
		prefix, ok := targets[key]
		if !ok || header.GetPrefix() != prefix || header.GetCredentialUri() == "" || header.GetActorJwt() != nil {
			origin, _, _ := strings.Cut(key, "\x00")
			return fmt.Errorf("added credential header %s on %s that does not match a gateway target", header.GetHeader(), origin)
		}
	}
	return nil
}

// policyHeaders splits a policy into rules and credential headers by origin and header.
func policyHeaders(policy *ateapipb.EgressPolicy) (*ateapipb.EgressPolicy, map[string]*ateapipb.CredentialHeader, error) {
	rules := proto.CloneOf(policy)
	headers := map[string]*ateapipb.CredentialHeader{}
	for _, rule := range rules.GetRules() {
		scheme, hostnames, ports, effects := "https", rule.GetHttps().GetHostnames(), rule.GetHttps().GetPorts(), rule.GetHttps().GetEffects()
		if rule.GetHttp() != nil {
			scheme, hostnames, ports, effects = "http", rule.GetHttp().GetHostnames(), rule.GetHttp().GetPorts(), rule.GetHttp().GetEffects()
		}
		if len(effects.GetReplaceHeaders()) == 0 {
			continue
		}
		if len(hostnames) != 1 || len(ports.GetNumbers()) != 1 {
			return nil, nil, fmt.Errorf("credential headers require a rule with one hostname and port")
		}
		u := url.URL{Scheme: scheme, Host: net.JoinHostPort(hostnames[0], strconv.Itoa(int(ports.GetNumbers()[0])))}
		origin := egress.Origin(&u)
		for _, header := range effects.GetReplaceHeaders() {
			key := origin + "\x00" + strings.ToLower(header.GetHeader())
			if headers[key] != nil {
				return nil, nil, fmt.Errorf("duplicate credential header %s on %s", header.GetHeader(), origin)
			}
			headers[key] = header
		}
		if rule.GetHttps() != nil {
			rule.Https.Effects = nil
		} else {
			rule.Http.Effects = nil
		}
	}
	return rules, headers, nil
}
