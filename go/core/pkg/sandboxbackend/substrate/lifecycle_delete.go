package substrate

import (
	"context"
	"fmt"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GoldenActorAtespace is the reserved substrate atespace that per-template
// golden actors live in. Mirrors substrate's internal/resources.GoldenActorAtespace,
// duplicated here because that package is internal to the substrate module.
const GoldenActorAtespace = "ate-golden"

func deleteGoldenActor(ctx context.Context, ateClient *Client, actorID string) (bool, error) {
	return deleteActor(ctx, ateClient, GoldenActorAtespace, actorID)
}

// CleanupSandboxAgentTemplate removes external Substrate actors tied to a generated SandboxAgent ActorTemplate.
func (p *Lifecycle) CleanupSandboxAgentTemplate(ctx context.Context, sa *v1alpha3.SandboxAgent) (bool, error) {
	if sa == nil || p == nil || p.Client == nil {
		return true, nil
	}
	list := &atev1alpha1.ActorTemplateList{}
	if err := p.Client.List(ctx, list,
		client.InNamespace(sa.Namespace),
		client.MatchingLabels{SandboxAgentLabelKey: sa.Name},
	); err != nil {
		return false, fmt.Errorf("list ActorTemplates for %s/%s: %w", sa.Namespace, sa.Name, err)
	}
	allDone := true
	for i := range list.Items {
		goldenID := strings.TrimSpace(list.Items[i].Status.GoldenActorID)
		if goldenID == "" {
			continue
		}
		done, err := deleteGoldenActor(ctx, p.AteClient, goldenID)
		if err != nil {
			return false, fmt.Errorf("delete golden actor %q: %w", goldenID, err)
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
}
