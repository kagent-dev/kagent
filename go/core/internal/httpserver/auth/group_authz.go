package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AllowedGroupsAnnotation is the annotation key on Agent CRs that specifies
	// which groups can access the agent. Comma-separated list of group names.
	// Rules:
	//   - No annotation or empty: agent is NOT visible to anyone
	//   - "public": agent is visible to all authenticated users
	//   - "doctors,nurses": agent is visible only to users in those groups
	// Users in the "admin" group can see all agents regardless of annotation.
	AllowedGroupsAnnotation = "kagent.dev/allowed-groups"

	// PublicGroup is the special group value that makes an agent visible to all.
	PublicGroup = "public"

	// AdminGroup is the group that bypasses all access checks.
	AdminGroup = "admin"
)

// GroupAuthorizer implements auth.Authorizer by checking the user's JWT groups
// against the agent's allowed-groups annotation.
type GroupAuthorizer struct {
	kubeClient client.Client
}

var _ auth.Authorizer = (*GroupAuthorizer)(nil)

// NewGroupAuthorizer creates a new GroupAuthorizer.
// kubeClient can be nil at creation time — call SetKubeClient before use.
func NewGroupAuthorizer(kubeClient client.Client) *GroupAuthorizer {
	return &GroupAuthorizer{
		kubeClient: kubeClient,
	}
}

// SetKubeClient sets the kube client for the authorizer (used for late initialization).
func (a *GroupAuthorizer) SetKubeClient(c client.Client) {
	a.kubeClient = c
}

// Check verifies that the principal has access to the requested resource.
// For agent resources, it checks the allowed-groups annotation.
// For non-agent resources, access is always granted (backward compatible).
func (a *GroupAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	// Only enforce group checks on agent resources
	if resource.Type != "Agent" {
		return nil
	}

	// If no resource name, this is a list operation — filtering happens in the handler
	if resource.Name == "" {
		return nil
	}

	// Fail closed if kube client is not initialized
	if a.kubeClient == nil {
		return fmt.Errorf("access denied: authorizer not initialized")
	}

	// Parse namespace/name from resource name
	namespace, name, err := parseResourceRef(resource.Name)
	if err != nil {
		return fmt.Errorf("access denied: invalid resource reference")
	}

	// Fetch the agent CR
	agent := &v1alpha2.Agent{}
	if err := a.kubeClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, agent); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // Agent not found — let the handler return 404
		}
		// Fail closed on transient errors
		return fmt.Errorf("access denied: unable to verify agent access")
	}

	return checkAgentGroupAccess(principal, agent.GetAnnotations())
}

// FilterAgentsByGroup filters a list of agents to only those the principal can access.
// Used by list handlers to scope results by group.
func FilterAgentsByGroup(principal auth.Principal, agents []v1alpha2.AgentObject) []v1alpha2.AgentObject {
	filtered := make([]v1alpha2.AgentObject, 0, len(agents))
	for _, agent := range agents {
		if err := checkAgentGroupAccess(principal, agent.GetAnnotations()); err == nil {
			filtered = append(filtered, agent)
		}
	}
	return filtered
}

// checkAgentGroupAccess checks if the principal's groups intersect with the agent's allowed groups.
// Rules:
//   - Admin group bypasses all checks
//   - No annotation or empty → denied (agent is private by default)
//   - "public" in allowed groups → allowed for all authenticated users
//   - Otherwise, user must have at least one matching group
func checkAgentGroupAccess(principal auth.Principal, annotations map[string]string) error {
	userGroups := principal.Groups

	// Admin bypasses everything
	if containsString(userGroups, AdminGroup) {
		return nil
	}

	allowedGroupsStr, ok := annotations[AllowedGroupsAnnotation]
	if !ok || allowedGroupsStr == "" {
		return fmt.Errorf("access denied")
	}

	allowedGroups := parseCSV(allowedGroupsStr)
	if len(allowedGroups) == 0 {
		return fmt.Errorf("access denied")
	}

	// "public" means visible to all authenticated users
	if containsString(allowedGroups, PublicGroup) {
		return nil
	}

	if len(userGroups) == 0 {
		return fmt.Errorf("access denied")
	}

	if hasIntersection(userGroups, allowedGroups) {
		return nil
	}

	return fmt.Errorf("access denied")
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func hasIntersection(a, b []string) bool {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	for _, s := range a {
		if _, ok := set[s]; ok {
			return true
		}
	}
	return false
}

func parseResourceRef(name string) (namespace, resourceName string, err error) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid resource name: %s", name)
	}
	return parts[0], parts[1], nil
}
