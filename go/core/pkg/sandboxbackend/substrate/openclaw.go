package substrate

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

const (
	defaultActorHostSuffix = "actors.resources.substrate.ate.dev"
	defaultSubstrateGWPort = int32(80)
	actorIDPrefix          = "ahr"
)

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ClawBackend implements AsyncBackend for OpenClaw/NemoClaw on Agent Substrate.
type ClawBackend struct {
	client   *Client
	backend  v1alpha2.AgentHarnessBackendType
	recorder record.EventRecorder
}

var _ sandboxbackend.AsyncBackend = (*ClawBackend)(nil)

// NewOpenClawBackend returns a substrate backend for openclaw/nemoclaw harness types.
func NewOpenClawBackend(client *Client, backend v1alpha2.AgentHarnessBackendType, recorder record.EventRecorder) *ClawBackend {
	return &ClawBackend{
		client:   client,
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
	tmplNS, tmplName := generatedActorTemplateKey(ah)

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
		actor, err = b.client.ResumeActor(ctx, actorID)
		if err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate ResumeActor %q: %w", actorID, err)
		}
	default:
		// suspending — wait for next reconcile
	}

	endpoint := substrateConnectionEndpoint(ah.Namespace, ah.Name, actor)

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

func (b *ClawBackend) DeleteAgentHarness(ctx context.Context, h sandboxbackend.Handle) (bool, error) {
	if h.ID == "" {
		return true, nil
	}
	done, err := deleteActor(ctx, b.client, h.ID)
	if err != nil {
		return false, fmt.Errorf("substrate delete actor %q: %w", h.ID, err)
	}
	return done, nil
}

func (b *ClawBackend) OnAgentHarnessReady(_ context.Context, _ *v1alpha2.AgentHarness, _ sandboxbackend.Handle) error {
	// OpenClaw config is baked into the ActorTemplate golden snapshot when the
	// generated ActorTemplate is reconciled.
	return nil
}

// ActorID returns a stable DNS-1123 actor id for this harness.
func ActorID(ah *v1alpha2.AgentHarness) string {
	raw := fmt.Sprintf("%s-%s-%s", actorIDPrefix, ah.Namespace, ah.Name)
	raw = strings.ToLower(raw)
	raw = strings.ReplaceAll(raw, "_", "-")
	if len(raw) > 63 {
		raw = raw[:63]
		raw = strings.TrimRight(raw, "-")
	}
	if !dns1123Label.MatchString(raw) {
		// fallback: hash-like trim
		raw = fmt.Sprintf("%s-%s", actorIDPrefix, ah.UID)
		if len(raw) > 63 {
			raw = raw[:63]
		}
	}
	return raw
}

// ActorHost returns the atenet router Host header value for the actor.
func ActorHost(actorID string, suffix string) string {
	if suffix == "" {
		suffix = defaultActorHostSuffix
	}
	return actorID + "." + suffix
}

func generatedActorTemplateKey(ah *v1alpha2.AgentHarness) (string, string) {
	return ah.Namespace, actorTemplateName(ah)
}

func substrateConnectionEndpoint(namespace, name string, actor *ateapipb.Actor) string {
	gw := fmt.Sprintf("/api/agentharnesses/%s/%s/gateway/", namespace, name)
	if actor == nil {
		return "kagent gateway: " + gw
	}
	if actorID := strings.TrimSpace(actor.GetActorId()); actorID != "" {
		return fmt.Sprintf("atenet-router Host %s (UI via kagent %s)", ActorHost(actorID, ""), gw)
	}
	return fmt.Sprintf("kagent gateway: %s (actor status %s)", gw, actor.GetStatus())
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
