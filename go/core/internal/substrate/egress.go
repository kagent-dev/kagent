package substrate

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/validation"
)

// EnsureActorEgressPolicy can be retried after a lost create response. A prepared
// revision is immutable, so an existing policy must match; never accept or
// overwrite a different allowlist. Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name string, policy *ateapipb.EgressPolicy) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	actor := actorRef(atespace, name)
	_, err := c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{
		Actor: actor, EgressPolicy: policy,
	})
	if status.Code(err) != codes.AlreadyExists {
		return err
	}
	existing, err := c.GetActorEgressPolicy(ctx, &ateapipb.GetActorEgressPolicyRequest{Actor: actor})
	if err != nil {
		return err
	}
	if !proto.Equal(&ateapipb.EgressPolicy{Rules: existing.GetRules()}, &ateapipb.EgressPolicy{Rules: policy.Rules}) {
		return fmt.Errorf("existing Actor egress policy does not match the prepared revision")
	}
	return nil
}

// ActorEgressPolicy compiles destinations into an actor's default allowlist.
// Credential bindings are already canonicalized by the store.
func ActorEgressPolicy(atespace string, destinations []string, credentials []egress.Credential) (*ateapipb.EgressPolicy, error) {
	var hostnames, cidrs []string
	for _, destination := range destinations {
		if ip, err := netip.ParseAddr(destination); err == nil && ip.Zone() == "" {
			ip = ip.Unmap()
			cidrs = append(cidrs, netip.PrefixFrom(ip, ip.BitLen()).String())
			continue
		}
		hostname := strings.TrimSuffix(strings.ToLower(destination), ".")
		if len(validation.IsDNS1123Subdomain(hostname)) != 0 {
			return nil, fmt.Errorf("invalid egress destination %q", destination)
		}
		hostnames = append(hostnames, hostname)
	}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	for _, binding := range credentials {
		if !slices.Contains(hostnames, binding.Hostname) {
			return nil, fmt.Errorf("credential destination %q is not allowed", binding.Hostname)
		}
		var rule *ateapipb.HostnameRule
		if len(policy.Rules) > 0 {
			rule = policy.Rules[len(policy.Rules)-1].GetHostnames()
		}
		if rule == nil || rule.Patterns[0] != binding.Hostname {
			rule = &ateapipb.HostnameRule{Patterns: []string{binding.Hostname}, Effects: &ateapipb.EgressRuleEffects{}}
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Hostnames: rule})
		}
		rule.Effects.InjectStaticHeaders = append(rule.Effects.InjectStaticHeaders, &ateapipb.CredentialHeaderInjection{Header: binding.Header, Prefix: binding.Prefix, CredentialUri: binding.URI})
	}
	if len(hostnames) > 0 {
		slices.Sort(hostnames)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Hostnames: &ateapipb.HostnameRule{Patterns: slices.Compact(hostnames)}})
	}
	if len(cidrs) > 0 {
		slices.Sort(cidrs)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Cidrs: &ateapipb.CIDRRule{Cidrs: slices.Compact(cidrs)}})
	}
	if len(policy.Rules) > 256 {
		return nil, fmt.Errorf("egress policy exceeds 256 rules")
	}
	return policy, nil
}
