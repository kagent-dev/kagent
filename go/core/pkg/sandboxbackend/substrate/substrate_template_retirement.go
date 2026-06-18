package substrate

import (
	"context"
	"fmt"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// RetireSupersededTemplates performs the cleanup half of a config-change rollout: it deletes a
// SandboxAgent's older ActorTemplates (and their golden actors) once a newer template is serving,
// so the previous golden keeps answering traffic until the new one is Ready (the blue-green
// resolution itself lives in ResolveCurrentActorTemplate).
//
// It keeps two templates: the newest (the desired/just-applied one, possibly still building) and
// the active one (newest with a Ready golden, which chat resolves to). Every other template is
// retired — but ONLY if its own golden is already Suspended (Phase==Ready). A template whose
// golden is still building is left alone: deleting it would orphan a RUNNING golden that can
// never be suspended afterwards (it would permanently pin a worker). The golden actor is deleted
// before the template object so we never leave a golden without its template.
//
// Performs at most one mutating ate-api step per superseded golden per call; returns done==false
// to be requeued until all superseded templates are gone.
func (p *Lifecycle) RetireSupersededTemplates(ctx context.Context, sa *v1alpha2.SandboxAgent) (bool, error) {
	if sa == nil || p == nil || p.Client == nil {
		return true, nil
	}
	templates, err := listSandboxAgentActorTemplates(ctx, p.Client, sa.Namespace, sa.Name)
	if err != nil {
		return false, err
	}
	if len(templates) <= 1 {
		return true, nil
	}

	var newest, active *atev1alpha1.ActorTemplate
	for _, t := range templates {
		if newest == nil || t.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = t
		}
		if t.Status.Phase == atev1alpha1.PhaseReady {
			if active == nil || t.CreationTimestamp.After(active.CreationTimestamp.Time) {
				active = t
			}
		}
	}

	done := true
	for _, t := range templates {
		if t.Name == newest.Name || (active != nil && t.Name == active.Name) {
			continue // keep the desired template and the one currently serving
		}
		if t.Status.Phase != atev1alpha1.PhaseReady {
			// Golden still building — retiring it now would orphan a RUNNING golden that can't
			// be suspended once its template is gone. Let it reach Ready, then retire next pass.
			done = false
			continue
		}
		// Golden is Suspended (Ready) — delete it first, then the template object.
		if goldenID := t.Status.GoldenActorID; goldenID != "" {
			gone, err := deleteGoldenActor(ctx, p.AteClient, goldenID)
			if err != nil {
				return false, fmt.Errorf("delete superseded golden %q for template %s: %w", goldenID, t.Name, err)
			}
			if !gone {
				done = false
				continue
			}
		}
		if err := p.Client.Delete(ctx, t); err != nil && !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("delete superseded ActorTemplate %s: %w", t.Name, err)
		}
	}
	return done, nil
}

// deleteActorIfSuspended deletes an actor only when it is Suspended (idle). RUNNING/RESUMING
// actors are left untouched — for substrate, RUNNING means an actor is resumed on a worker, and
// kagent's transport suspends a session actor after each request completes. Force-suspending a
// RUNNING actor could cut a live response mid-stream, so we wait for it to quiesce instead.
// Returns done==true when the actor no longer exists (or never did).
func deleteActorIfSuspended(ctx context.Context, c *Client, actorID string) (bool, error) {
	if actorID == "" || c == nil {
		return true, nil
	}
	actor, err := c.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, fmt.Errorf("get actor %q: %w", actorID, err)
	}
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED:
		if err := c.DeleteActor(ctx, actorID); err != nil && status.Code(err) != codes.NotFound {
			return false, fmt.Errorf("delete actor %q: %w", actorID, err)
		}
		return true, nil
	default:
		// RUNNING / RESUMING / SUSPENDING — actively (or transitionally) in use; skip.
		return false, nil
	}
}
