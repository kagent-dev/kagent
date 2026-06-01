package substrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

const (
	defaultActorHostSuffix = "actors.resources.substrate.ate.dev"
	actorIDPrefix          = "ahr"
	actorIDHashHexLen      = 16
)

// AgentHarnessAPIBase is the kagent REST prefix for an AgentHarness resource.
func AgentHarnessAPIBase(namespace, name string) string {
	return fmt.Sprintf("/api/agentharnesses/%s/%s", namespace, name)
}

// AgentHarnessGatewayUIPath is the same-origin HTTP/WebSocket path clients use for
// the OpenClaw Control UI (proxied by kagent to the actor pod).
func AgentHarnessGatewayUIPath(namespace, name string) string {
	return AgentHarnessAPIBase(namespace, name) + "/gateway/"
}

// AgentHarnessGatewayControlUIBasePath is gateway.controlUi.basePath in openclaw.json
// (no trailing slash; OpenClaw expects a path prefix, not a URL).
func AgentHarnessGatewayControlUIBasePath(namespace, name string) string {
	return strings.TrimSuffix(AgentHarnessGatewayUIPath(namespace, name), "/")
}

func connectionEndpoint(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return ""
	}
	return AgentHarnessGatewayUIPath(ah.Namespace, ah.Name)
}

// ClawBackend implements AsyncBackend for OpenClaw/NemoClaw on Agent Substrate.
type ClawBackend struct {
	client   *Client
	cfg      Config
	backend  v1alpha2.AgentHarnessBackendType
	recorder record.EventRecorder
}

var _ sandboxbackend.AsyncBackend = (*ClawBackend)(nil)

// NewOpenClawBackend returns a substrate backend for openclaw/nemoclaw harness types.
func NewOpenClawBackend(client *Client, cfg Config, backend v1alpha2.AgentHarnessBackendType, recorder record.EventRecorder) *ClawBackend {
	return &ClawBackend{
		client:   client,
		cfg:      cfg,
		backend:  backend,
		recorder: recorder,
	}
}

func (b *ClawBackend) Name() v1alpha2.AgentHarnessBackendType {
	return b.backend
}

func (b *ClawBackend) EnsureAgentHarness(ctx context.Context, ah *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}
	if err := validateSubstrateSpec(ah); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}

	actorID := ActorID(ah)
	tmplNS, tmplName := actorTemplateRef(ah, b.cfg)

	actor, err := b.client.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
		}
		actor, err = b.client.CreateActor(ctx, actorID, tmplNS, tmplName)
		if err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate CreateActor %q: %w", actorID, err)
		}
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		// already active or waking
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED:
		_, err = b.client.ResumeActor(ctx, actorID)
		if err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate ResumeActor %q: %w", actorID, err)
		}
	default:
		// suspending — wait for next reconcile
	}

	endpoint := connectionEndpoint(ah)

	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: actorID},
		Endpoint: endpoint,
	}, nil
}

func (b *ClawBackend) GetStatus(ctx context.Context, h sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	if h.ID == "" {
		return metav1.ConditionUnknown, "ActorHandleMissing", "no substrate actor id recorded yet"
	}
	actor, err := b.client.GetActor(ctx, h.ID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return metav1.ConditionUnknown, "ActorNotFound", fmt.Sprintf("substrate actor %q not found", h.ID)
		}
		return metav1.ConditionUnknown, "ActorGetFailed", err.Error()
	}
	return actorStatusToCondition(actor)
}

func (b *ClawBackend) DeleteAgentHarness(ctx context.Context, h sandboxbackend.Handle) error {
	if h.ID == "" {
		return nil
	}
	done, err := b.client.AdvanceActorDelete(ctx, h.ID)
	if err != nil {
		return fmt.Errorf("substrate delete actor %q: %w", h.ID, err)
	}
	if !done {
		return fmt.Errorf("substrate delete actor %q in progress", h.ID)
	}
	return nil
}

func (b *ClawBackend) OnAgentHarnessReady(_ context.Context, _ *v1alpha2.AgentHarness, _ sandboxbackend.Handle) error {
	// OpenClaw config is baked into the ActorTemplate golden snapshot at provision time
	// (see substrate/provision_openclaw.go — openclaw.BuildSubstrateBootstrapJSON with secretKeyRef env).
	return nil
}

// ActorID returns a stable DNS-1123 actor id derived from namespace/name (ahr-<hex>).
func ActorID(ah *v1alpha2.AgentHarness) string {
	if ah == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(ah.Namespace + "/" + ah.Name))
	return fmt.Sprintf("%s-%s", actorIDPrefix, hex.EncodeToString(sum[:])[:actorIDHashHexLen])
}

// ActorHost returns the atenet router Host header value for the actor.
func ActorHost(actorID string, suffix string) string {
	if suffix == "" {
		suffix = defaultActorHostSuffix
	}
	return actorID + "." + suffix
}

func actorTemplateRef(ah *v1alpha2.AgentHarness, cfg Config) (string, string) {
	if ah.Spec.Substrate != nil && ah.Spec.Substrate.ActorTemplateRef != nil {
		if ref := ah.Spec.Substrate.ActorTemplateRef; ref.Name != "" {
			return ah.Namespace, ref.Name
		}
	}
	// Auto-provisioned template in the harness namespace (also when status was not persisted yet).
	if ah.Annotations != nil && ah.Annotations[AnnotationManagedActorTemplate] == "true" {
		return ah.Namespace, actorTemplateName(ah)
	}
	if cfg.DefaultActorTemplateNamespace != "" && cfg.DefaultActorTemplateName != "" {
		return cfg.DefaultActorTemplateNamespace, cfg.DefaultActorTemplateName
	}
	return ah.Namespace, actorTemplateName(ah)
}

func validateSubstrateSpec(ah *v1alpha2.AgentHarness) error {
	runtime := ah.Spec.Runtime
	if runtime == "" {
		runtime = v1alpha2.AgentHarnessRuntimeOpenshell
	}
	if runtime != v1alpha2.AgentHarnessRuntimeSubstrate {
		return fmt.Errorf("substrate backend called for runtime %q", runtime)
	}
	return nil
}

func actorStatusToCondition(actor *ateapipb.Actor) (metav1.ConditionStatus, string, string) {
	if actor == nil {
		return metav1.ConditionUnknown, "ActorMissing", "empty actor response"
	}
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING:
		if ip := actor.GetAteomPodIp(); ip != "" {
			return metav1.ConditionTrue, "ActorRunning", fmt.Sprintf("actor running on %s", ip)
		}
		return metav1.ConditionTrue, "ActorRunning", "actor is running"
	case ateapipb.Actor_STATUS_RESUMING:
		return metav1.ConditionFalse, "ActorResuming", "actor is resuming"
	case ateapipb.Actor_STATUS_SUSPENDING:
		return metav1.ConditionFalse, "ActorSuspending", "actor is suspending"
	case ateapipb.Actor_STATUS_SUSPENDED:
		return metav1.ConditionFalse, "ActorSuspended", "actor is suspended"
	default:
		return metav1.ConditionUnknown, "ActorStatusUnknown", actor.GetStatus().String()
	}
}
