package agentinstance

import (
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

const authoritySuffix = "agents"

// Authority is the logical public A2A endpoint for an AgentInstance. Clients
// dial the shared gateway and use this value as the gRPC authority; it never
// identifies the private Substrate Actor behind the instance.
func Authority(namespace, id string) string {
	return strings.ToLower(id) + "." + namespace + "." + authoritySuffix
}

// ParseAuthority extracts the only caller-selectable routing information the
// gateway accepts. The returned identity is still checked against authenticated
// storage before any private runtime address is resolved.
func ParseAuthority(authority string) (namespace, id string, err error) {
	authority = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(authority)), ".")
	if host, _, splitErr := net.SplitHostPort(authority); splitErr == nil {
		authority = host
	}
	parts := strings.Split(authority, ".")
	if len(parts) != 3 || parts[2] != authoritySuffix {
		return "", "", fmt.Errorf("invalid AgentInstance authority %q", authority)
	}
	if _, parseErr := uuid.Parse(parts[0]); parseErr != nil {
		return "", "", fmt.Errorf("invalid AgentInstance authority %q: %w", authority, parseErr)
	}
	if problems := utilvalidation.IsDNS1123Label(parts[1]); len(problems) > 0 {
		return "", "", fmt.Errorf("invalid AgentInstance authority %q: %s", authority, strings.Join(problems, "; "))
	}
	return parts[1], parts[0], nil
}
