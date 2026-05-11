package openshell

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/openshell/gen/datamodelv1"
	inferencev1 "github.com/kagent-dev/kagent/go/api/openshell/gen/inferencev1"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// agentHarnessGatewayName is the deterministic OpenShell sandbox name for an AgentHarness. Format:
// "<namespace>-<name>". Collisions across clusters sharing one gateway are a known limitation.
func agentHarnessGatewayName(ah *v1alpha2.AgentHarness) string {
	return fmt.Sprintf("%s-%s", ah.Namespace, ah.Name)
}

// sandboxBackendHandleID is ObjectMeta.name — the canonical lookup key for
// GetSandbox / DeleteSandbox (same string as CreateSandboxRequest.Name).
func sandboxBackendHandleID(sb *openshellv1.Sandbox) string {
	if sb == nil || sb.GetMetadata() == nil {
		return ""
	}
	return strings.TrimSpace(sb.GetMetadata().GetName())
}

// buildAgentHarnessOpenshellCreateRequest maps an AgentHarness into an OpenShell CreateSandboxRequest.
// unsupported collects fields the gateway cannot currently express so callers can surface them as events.
func buildAgentHarnessOpenshellCreateRequest(ah *v1alpha2.AgentHarness) (*openshellv1.CreateSandboxRequest, []string) {
	unsupported := []string{}
	tpl := &openshellv1.SandboxTemplate{}
	env := map[string]string{}

	if ah.Spec.Image != "" {
		tpl.Image = ah.Spec.Image
	}
	for _, e := range ah.Spec.Env {
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
	if pol := openShellSandboxPolicyForAgentHarness(ah); pol != nil {
		spec.Policy = pol
	}

	return &openshellv1.CreateSandboxRequest{
		Name: agentHarnessGatewayName(ah),
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

// translateModelConfig syncs a ModelConfig CR onto the OpenShell control plane: it registers the
// gateway provider (CreateProvider/UpdateProvider with credentials) and pins cluster inference to
// the provider/model pair. Backends that need this work call it before CreateSandbox; it is a no-op
// when spec.modelConfigRef is empty.
func translateModelConfig(
	ctx context.Context,
	ah *v1alpha2.AgentHarness,
	kube client.Client,
	oc *OpenShellClients,
) error {
	if ah == nil {
		return fmt.Errorf("AgentHarness is required")
	}
	ref := strings.TrimSpace(ah.Spec.ModelConfigRef)
	if ref == "" {
		return nil
	}
	if oc == nil {
		return fmt.Errorf("openshell: OpenShell clients required")
	}
	inference, osCli := oc.Inference, oc.OpenShell
	if kube == nil {
		return fmt.Errorf("openshell: Kubernetes client is required when spec.modelConfigRef is set")
	}
	if inference == nil {
		return fmt.Errorf("openshell: inference client is required when spec.modelConfigRef is set")
	}
	if osCli == nil {
		return fmt.Errorf("openshell: OpenShell client is required when spec.modelConfigRef is set")
	}

	modelConfigRef, err := utils.ParseRefString(ref, ah.Namespace)
	if err != nil {
		return fmt.Errorf("failed to parse ModelConfigRef %s: %w", ref, err)
	}

	modelConfig := &v1alpha2.ModelConfig{}
	if err := kube.Get(ctx, modelConfigRef, modelConfig); err != nil {
		return fmt.Errorf("failed to get ModelConfig %s: %w", modelConfigRef.String(), err)
	}
	apiKey, err := openclaw.ResolveModelConfigAPIKey(ctx, kube, modelConfig)
	if err != nil {
		return fmt.Errorf("openshell gateway provider: %w", err)
	}

	providerRecordName := openclaw.GatewayProviderRecordName(modelConfig.Spec.Provider)
	model := modelConfig.Spec.Model

	getProviderResp, err := osCli.GetProvider(ctx, &openshellv1.GetProviderRequest{Name: providerRecordName})
	exists := false
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("GetProvider %s: %w", providerRecordName, err)
		}
	} else if getProviderResp.GetProvider() != nil {
		exists = true
	}

	providerProto := &datamodelv1.Provider{
		Metadata: &datamodelv1.ObjectMeta{Name: providerRecordName},
		Type:     providerRecordName,
		Credentials: map[string]string{
			"apiKey": apiKey,
		},
	}

	if exists {
		if _, err := osCli.UpdateProvider(ctx, &openshellv1.UpdateProviderRequest{Provider: providerProto}); err != nil {
			return fmt.Errorf("UpdateProvider %s: %w", providerRecordName, err)
		}
		ctrllog.FromContext(ctx).Info("updated gateway provider", "name", providerRecordName)
	} else {
		if _, err := osCli.CreateProvider(ctx, &openshellv1.CreateProviderRequest{Provider: providerProto}); err != nil {
			return fmt.Errorf("CreateProvider %s: %w", providerRecordName, err)
		}
		ctrllog.FromContext(ctx).Info("created gateway provider", "name", providerRecordName)
	}

	if _, err := inference.SetClusterInference(ctx, &inferencev1.SetClusterInferenceRequest{
		ProviderName: providerRecordName,
		ModelId:      model,
		NoVerify:     true,
	}); err != nil {
		return fmt.Errorf("cluster inference for model %s: %w", model, err)
	}
	ctrllog.FromContext(ctx).Info("set cluster inference", "provider", providerRecordName, "model", model)
	return nil
}
