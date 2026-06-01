package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// CleanupGeneratedTemplate removes external Substrate actors that Kubernetes garbage collection cannot see.
// The generated ActorTemplate CR is deleted by owner-reference garbage collection after the
// AgentHarness finalizer is removed. WorkerPools are externally owned and are never deleted here.
func (p *Lifecycle) CleanupGeneratedTemplate(ctx context.Context, ah *v1alpha2.AgentHarness) (bool, error) {
	if ah == nil {
		return true, nil
	}
	if p.Client == nil {
		return true, nil
	}

	tmplKey := types.NamespacedName{Namespace: ah.Namespace, Name: actorTemplateName(ah)}
	goldenID, err := p.goldenActorID(ctx, tmplKey)
	if err != nil {
		return false, err
	}
	if goldenID == "" {
		return true, nil
	}
	done, err := deleteGoldenActor(ctx, p.AteClient, goldenID)
	if err != nil {
		return false, fmt.Errorf("delete golden actor %q for ActorTemplate %s: %w", goldenID, tmplKey, err)
	}
	if !done {
		return false, nil
	}

	return true, nil
}

func deleteGoldenActor(ctx context.Context, ateClient *Client, actorID string) (bool, error) {
	return deleteActor(ctx, ateClient, actorID)
}

func (p *Lifecycle) goldenActorID(ctx context.Context, tmplKey types.NamespacedName) (string, error) {
	var tmpl atev1alpha1.ActorTemplate
	if err := p.Client.Get(ctx, tmplKey, &tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("get ActorTemplate %s for golden actor cleanup: %w", tmplKey, err)
	}
	return strings.TrimSpace(tmpl.Status.GoldenActorID), nil
}

// HarnessLabelKey labels substrate lifecycle managed for an AgentHarness.
const HarnessLabelKey = "kagent.dev/agent-harness"

// HarnessNameFromLabels returns the AgentHarness name from generated lifecycle labels.
func HarnessNameFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels[HarnessLabelKey])
}
