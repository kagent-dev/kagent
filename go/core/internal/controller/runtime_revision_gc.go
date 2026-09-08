package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const runtimeRevisionGCInterval = time.Minute

type runtimeRevisionGCStore interface {
	ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error)
	BeginRuntimeRevisionDeletion(context.Context, string) (*database.RuntimeRevision, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string, string) error
}

type runtimeRevisionGCClient interface {
	GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error)
	DeleteActorTemplate(context.Context, string, string, string) error
}

// RuntimeRevisionGC retries durable runtime deletions independently of preparation.
type RuntimeRevisionGC struct {
	store     runtimeRevisionGCStore
	templates runtimeRevisionGCClient
}

var (
	_ manager.Runnable               = (*RuntimeRevisionGC)(nil)
	_ manager.LeaderElectionRunnable = (*RuntimeRevisionGC)(nil)
)

func NewRuntimeRevisionGC(store runtimeRevisionGCStore, templates runtimeRevisionGCClient) *RuntimeRevisionGC {
	return &RuntimeRevisionGC{store: store, templates: templates}
}

func (r *RuntimeRevisionGC) NeedLeaderElection() bool { return true }

func (r *RuntimeRevisionGC) Start(ctx context.Context) error {
	ticker := time.NewTicker(runtimeRevisionGCInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		r.sweep(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	return nil
}

func (r *RuntimeRevisionGC) sweep(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, time.Minute)
	revisions, err := r.store.ListUnreferencedRuntimeRevisions(listCtx)
	cancel()
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list unreferenced runtime revisions", "error", err)
		return
	}
	// ponytail: sweeps are serial; add bounded workers if slow deletions delay reclamation.
	for _, candidate := range revisions {
		if ctx.Err() != nil {
			return
		}
		if err := r.collect(ctx, candidate.Revision); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to collect runtime revision", "revision", candidate.Revision, "error", err)
		}
	}
}

// collect retains the database row until compute deletion succeeds. Each candidate
// has its own deadline so a stuck backend or database lock cannot stall the sweep.
func (r *RuntimeRevisionGC) collect(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	revision, err := r.store.BeginRuntimeRevisionDeletion(ctx, id)
	if err != nil {
		return fmt.Errorf("begin deletion of runtime revision %s: %w", id, err)
	}
	if revision == nil {
		return nil
	}
	template, err := r.templates.GetActorTemplate(ctx, revision.ActorTemplateAtespace, revision.ActorTemplateName)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("get unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	if err == nil && (revision.ActorTemplateUID == "" || template.GetMetadata().GetUid() != revision.ActorTemplateUID) {
		return fmt.Errorf("unreferenced ActorTemplate %s/%s UID changed", revision.ActorTemplateAtespace, revision.ActorTemplateName)
	}
	if err := r.templates.DeleteActorTemplate(ctx, revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID); err != nil {
		return fmt.Errorf("delete unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	if err := r.store.DeleteUnreferencedRuntimeRevision(ctx, revision.Revision, revision.ActorTemplateUID); err != nil {
		return fmt.Errorf("delete unreferenced runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}
