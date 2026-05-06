package openshell

import (
	"fmt"
	"strings"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sandboxName is the deterministic name used on the gateway. Format:
// "<namespace>-<name>". Collisions across clusters sharing one gateway are
// a known limitation.
func sandboxName(sbx *v1alpha2.AgentHarness) string {
	return fmt.Sprintf("%s-%s", sbx.Namespace, sbx.Name)
}

// sandboxBackendHandleID is ObjectMeta.name — the canonical lookup key for
// GetSandbox / DeleteSandbox (same string as CreateSandboxRequest.Name).
func sandboxBackendHandleID(sb *openshellv1.Sandbox) string {
	if sb == nil || sb.GetMetadata() == nil {
		return ""
	}
	return strings.TrimSpace(sb.GetMetadata().GetName())
}

// buildOpenshellCreateRequest maps a kagent AgentHarness into an OpenShell
// CreateSandboxRequest. unsupported collects fields the gateway
// cannot currently express so callers can surface them as events.
func buildOpenshellCreateRequest(sbx *v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string) {
	unsupported := []string{}
	tpl := &openshellv1.SandboxTemplate{}
	env := map[string]string{}

	if sbx.Spec.Image != "" {
		tpl.Image = sbx.Spec.Image
	}
	for _, e := range sbx.Spec.Env {
		if e.ValueFrom != nil {
			unsupported = append(unsupported, "env."+e.Name+".valueFrom")
			continue
		}
		env[e.Name] = e.Value
	}
	spec := &openshellv1.SandboxSpec{
		Environment: env,
		Template:    tpl,
	}
	if pol := sandboxPolicyForCreateRequest(sbx); pol != nil {
		spec.Policy = pol
	}

	return &openshellv1.CreateSandboxRequest{
		Name: sandboxName(sbx),
		Spec: spec,
	}, unsupported
}

// phaseToCondition maps OpenShell SandboxPhase + status message into a
// (Ready status, reason, message) triple for AgentHarness.Status.
func phaseToCondition(sb *openshellv1.Sandbox) (metav1.ConditionStatus, string, string) {
	if sb == nil {
		return metav1.ConditionUnknown, "SandboxNotFound", "no sandbox returned by gateway"
	}
	msg := summarizeConditions(sb.GetStatus())
	switch sb.GetPhase() {
	case openshellv1.SandboxPhase_SANDBOX_PHASE_READY:
		return metav1.ConditionTrue, "SandboxReady", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_PROVISIONING:
		return metav1.ConditionFalse, "SandboxProvisioning", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_ERROR:
		return metav1.ConditionFalse, "SandboxError", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_DELETING:
		return metav1.ConditionFalse, "SandboxDeleting", msg
	case openshellv1.SandboxPhase_SANDBOX_PHASE_UNKNOWN, openshellv1.SandboxPhase_SANDBOX_PHASE_UNSPECIFIED:
		return metav1.ConditionUnknown, "SandboxPhaseUnknown", msg
	default:
		return metav1.ConditionUnknown, "SandboxPhaseUnrecognized", fmt.Sprintf("unrecognized phase %s", sb.GetPhase())
	}
}

func summarizeConditions(s *openshellv1.SandboxStatus) string {
	if s == nil {
		return ""
	}
	parts := make([]string, 0, len(s.GetConditions()))
	for _, c := range s.GetConditions() {
		if c.GetMessage() != "" {
			parts = append(parts, fmt.Sprintf("%s=%s: %s", c.GetType(), c.GetStatus(), c.GetMessage()))
		}
	}
	return strings.Join(parts, "; ")
}

// endpointFor returns a connection hint surfaced on AgentHarness.Status.Connection.
// For OpenShell the gateway URL itself is the addressable endpoint — clients
// use it together with the sandbox name to Exec/SSH in.
func endpointFor(gatewayURL, sandboxID string) string {
	if gatewayURL == "" {
		return ""
	}
	return fmt.Sprintf("%s#%s", gatewayURL, sandboxID)
}
