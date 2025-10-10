package agent

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// WorkflowValidator validates workflow agent specifications
type WorkflowValidator struct {
	client client.Client
}

// NewWorkflowValidator creates a new workflow validator
func NewWorkflowValidator(client client.Client) *WorkflowValidator {
	return &WorkflowValidator{
		client: client,
	}
}

// ValidateCircularDependency checks for circular dependencies in workflow sub-agents
// Loop agents are exempt from this check (circular references are intentional)
func (v *WorkflowValidator) ValidateCircularDependency(ctx context.Context, ref v1alpha2.SubAgentReference, namespace string) error {
	// Get the agent
	agent, err := v.getAgent(ctx, ref, namespace)
	if err != nil {
		return err
	}

	// Skip validation for LoopAgent (circular references allowed)
	if agent.Spec.Type == v1alpha2.AgentType_Workflow && agent.Spec.Workflow != nil && agent.Spec.Workflow.Loop != nil {
		return nil
	}

	// Run DFS cycle detection
	visited := make(map[string]bool)
	stack := make(map[string]bool)

	if v.detectCycle(ctx, ref, namespace, visited, stack) {
		return fmt.Errorf("circular dependency detected in workflow starting from %s/%s", namespace, ref.Name)
	}

	return nil
}

// detectCycle performs depth-first search to detect cycles
func (v *WorkflowValidator) detectCycle(ctx context.Context, ref v1alpha2.SubAgentReference, namespace string, visited, stack map[string]bool) bool {
	// Resolve namespace
	ns := ref.Namespace
	if ns == "" {
		ns = namespace
	}

	key := fmt.Sprintf("%s/%s", ns, ref.Name)

	// Back edge detected (cycle found)
	if stack[key] {
		return true
	}

	// Already visited and processed
	if visited[key] {
		return false
	}

	// Mark as visited and in current path
	visited[key] = true
	stack[key] = true

	// Get agent and check its sub-agents
	agent, err := v.getAgent(ctx, ref, namespace)
	if err != nil {
		// If agent doesn't exist, reference validation will catch it
		return false
	}

	// Get sub-agents based on workflow type
	subAgents := v.getSubAgents(agent)

	// Recursively check each sub-agent
	for _, subRef := range subAgents {
		if v.detectCycle(ctx, subRef, ns, visited, stack) {
			return true
		}
	}

	// Remove from current path (backtrack)
	delete(stack, key)

	return false
}

// ResolveSubAgentReference validates that a sub-agent reference points to an existing agent
func (v *WorkflowValidator) ResolveSubAgentReference(ctx context.Context, ref v1alpha2.SubAgentReference, namespace string) error {
	agent, err := v.getAgent(ctx, ref, namespace)
	if err != nil {
		return fmt.Errorf("failed to resolve sub-agent reference %s: %w", ref.Name, err)
	}

	// Check if agent is Ready
	for _, condition := range agent.Status.Conditions {
		if condition.Type == "Ready" && condition.Status != "True" {
			return fmt.Errorf("sub-agent %s/%s is not ready", namespace, ref.Name)
		}
	}

	return nil
}

// ValidateNestingDepth ensures workflow nesting doesn't exceed the maximum depth
func (v *WorkflowValidator) ValidateNestingDepth(ctx context.Context, ref v1alpha2.SubAgentReference, namespace string, maxDepth int) error {
	depth := v.calculateDepth(ctx, ref, namespace, 0)

	if depth > maxDepth {
		return fmt.Errorf("nesting depth exceeds maximum of %d (actual: %d)", maxDepth, depth)
	}

	return nil
}

// calculateDepth recursively calculates the maximum nesting depth
func (v *WorkflowValidator) calculateDepth(ctx context.Context, ref v1alpha2.SubAgentReference, namespace string, currentDepth int) int {
	// Get the agent
	agent, err := v.getAgent(ctx, ref, namespace)
	if err != nil {
		return currentDepth
	}

	// If not a workflow agent, this is a leaf node
	if agent.Spec.Type != v1alpha2.AgentType_Workflow || agent.Spec.Workflow == nil {
		return currentDepth + 1
	}

	// Get sub-agents and calculate depth for each
	subAgents := v.getSubAgents(agent)
	maxSubDepth := currentDepth + 1

	ns := ref.Namespace
	if ns == "" {
		ns = namespace
	}

	for _, subRef := range subAgents {
		subDepth := v.calculateDepth(ctx, subRef, ns, currentDepth+1)
		if subDepth > maxSubDepth {
			maxSubDepth = subDepth
		}
	}

	return maxSubDepth
}

// ValidateTimeout validates the timeout format and range
func (v *WorkflowValidator) ValidateTimeout(timeout string) error {
	// Validate format with regex
	pattern := `^[0-9]+(ms|s|m|h)$`
	matched, err := regexp.MatchString(pattern, timeout)
	if err != nil {
		return fmt.Errorf("regex error: %w", err)
	}
	if !matched {
		return fmt.Errorf("invalid timeout format: %s (expected format: <number><unit>, e.g., '5m', '300s')", timeout)
	}

	// Parse duration
	duration, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout format: %w", err)
	}

	// Validate range (1s to 1h)
	if duration < 1*time.Second {
		return fmt.Errorf("timeout too short: %s (minimum: 1s)", timeout)
	}
	if duration > 1*time.Hour {
		return fmt.Errorf("timeout too long: %s (maximum: 1h)", timeout)
	}

	return nil
}

// Helper methods

// getAgent retrieves an agent by reference
func (v *WorkflowValidator) getAgent(ctx context.Context, ref v1alpha2.SubAgentReference, parentNamespace string) (*v1alpha2.Agent, error) {
	// Resolve namespace (default to parent if not specified)
	ns := ref.Namespace
	if ns == "" {
		ns = parentNamespace
	}

	// Get agent
	agent := &v1alpha2.Agent{}
	err := v.client.Get(ctx, types.NamespacedName{
		Namespace: ns,
		Name:      ref.Name,
	}, agent)

	if err != nil {
		return nil, fmt.Errorf("agent %s/%s not found: %w", ns, ref.Name, err)
	}

	return agent, nil
}

// getSubAgents extracts sub-agent references from a workflow agent
func (v *WorkflowValidator) getSubAgents(agent *v1alpha2.Agent) []v1alpha2.SubAgentReference {
	if agent.Spec.Type != v1alpha2.AgentType_Workflow || agent.Spec.Workflow == nil {
		return nil
	}

	workflow := agent.Spec.Workflow

	if workflow.Sequential != nil {
		return workflow.Sequential.SubAgents
	}
	if workflow.Parallel != nil {
		return workflow.Parallel.SubAgents
	}
	if workflow.Loop != nil {
		return workflow.Loop.SubAgents
	}

	return nil
}
