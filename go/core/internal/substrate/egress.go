package substrate

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/validation"
)

// EnsureActorEgressPolicy can be retried after a lost create response. A prepared
// revision is immutable, so an existing policy must match; never accept or
// overwrite a different allowlist. Substrate deletes the policy with its Actor.
func (c *Client) EnsureActorEgressPolicy(ctx context.Context, atespace, name string, destinations []string) error {
	policy, err := actorEgressPolicy(atespace, destinations)
	if err != nil {
		return err
	}
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	actor := actorRef(atespace, name)
	_, err = c.CreateActorEgressPolicy(ctx, &ateapipb.CreateActorEgressPolicyRequest{
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

func actorEgressPolicy(atespace string, destinations []string) (*ateapipb.EgressPolicy, error) {
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
	if len(hostnames) > 0 {
		slices.Sort(hostnames)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Hostnames: &ateapipb.HostnameRule{Patterns: slices.Compact(hostnames)}})
	}
	if len(cidrs) > 0 {
		slices.Sort(cidrs)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Cidrs: &ateapipb.CIDRRule{Cidrs: slices.Compact(cidrs)}})
	}
	return policy, nil
}
