package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const runtimeRevisionGCInterval = time.Minute

type runtimeRevisionGCStore interface {
	ObserveRuntimeRevisionCleanupBacklog(context.Context) (database.RuntimeRevisionCleanupBacklog, error)
	ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error)
	BeginRuntimeRevisionDeletion(context.Context, string) (*database.RuntimeRevision, error)
	DeleteRuntimeRevision(context.Context, string, string) error
}

type runtimeRevisionGCClient interface {
	GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error)
	DeleteActorTemplate(context.Context, string, string, string) error
}

// RuntimeRevisionGC retries durable runtime deletions independently of preparation.
type RuntimeRevisionGC struct {
	store     runtimeRevisionGCStore
	templates runtimeRevisionGCClient
	metrics   *runtimeRevisionGCMetrics
}

var (
	_ manager.Runnable               = (*RuntimeRevisionGC)(nil)
	_ manager.LeaderElectionRunnable = (*RuntimeRevisionGC)(nil)
)

func NewRuntimeRevisionGC(store runtimeRevisionGCStore, templates runtimeRevisionGCClient, registerer prometheus.Registerer) (*RuntimeRevisionGC, error) {
	metrics, err := newRuntimeRevisionGCMetrics(registerer)
	if err != nil {
		return nil, err
	}
	return &RuntimeRevisionGC{store: store, templates: templates, metrics: metrics}, nil
}

func (r *RuntimeRevisionGC) NeedLeaderElection() bool { return true }

func (r *RuntimeRevisionGC) Start(ctx context.Context) error {
	r.metrics.setActive(true)
	defer r.metrics.setActive(false)
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
	if ctx.Err() != nil {
		return
	}
	r.observeBacklog(ctx)
	listCtx, cancel := context.WithTimeout(ctx, time.Minute)
	revisions, err := r.store.ListUnreferencedRuntimeRevisions(listCtx)
	cancel()
	if err != nil {
		r.metrics.recordFailure(ctx, gcStageDiscovery)
		if ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list unreferenced runtime revisions", "error", err)
		}
		return
	}
	// ponytail: sweeps are serial; add bounded workers if slow deletions delay reclamation.
	for _, candidate := range revisions {
		if ctx.Err() != nil {
			return
		}
		if err := r.collect(ctx, candidate.Revision); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to collect runtime revision",
				"revision", candidate.Revision, "actor_template_atespace", candidate.ActorTemplateAtespace,
				"actor_template_name", candidate.ActorTemplateName, "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		r.observeBacklog(ctx)
	}
}

func (r *RuntimeRevisionGC) observeBacklog(ctx context.Context) {
	observationCtx, cancel := context.WithTimeout(ctx, time.Minute)
	backlog, err := r.store.ObserveRuntimeRevisionCleanupBacklog(observationCtx)
	cancel()
	if err == nil {
		err = r.metrics.observe(backlog)
	}
	if err != nil {
		r.metrics.recordFailure(ctx, gcStageBacklogObservation)
		if ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to observe runtime revision cleanup backlog", "error", err)
		}
	}
}

// collect retains the database row until compute deletion succeeds. Each candidate
// has its own deadline so a stuck backend or database lock cannot stall the sweep.
func (r *RuntimeRevisionGC) collect(ctx context.Context, id string) (err error) {
	parent := ctx
	stage := gcStageBeginDeletion
	defer func() {
		if err != nil {
			r.metrics.recordFailure(parent, stage)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	revision, err := r.store.BeginRuntimeRevisionDeletion(ctx, id)
	if err != nil {
		return fmt.Errorf("begin deletion of runtime revision %s: %w", id, err)
	}
	if revision == nil {
		return nil
	}
	stage = gcStageGetActorTemplate
	template, err := r.templates.GetActorTemplate(ctx, revision.ActorTemplateAtespace, revision.ActorTemplateName)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("get unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	if err == nil && (revision.ActorTemplateUID == "" || template.GetMetadata().GetUid() != revision.ActorTemplateUID) {
		stage = gcStageUIDCheck
		return fmt.Errorf("unreferenced ActorTemplate %s/%s UID changed", revision.ActorTemplateAtespace, revision.ActorTemplateName)
	}
	// Both deletes tolerate already-missing objects. If runtime cleanup succeeds
	// but database finalization fails, the durable deletion marker keeps this
	// revision discoverable so the next sweep can safely retry the sequence.
	stage = gcStageDeleteActorTemplate
	if err := r.templates.DeleteActorTemplate(ctx, revision.ActorTemplateAtespace, revision.ActorTemplateName, revision.ActorTemplateUID); err != nil {
		return fmt.Errorf("delete unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	stage = gcStageFinalize
	if err := r.store.DeleteRuntimeRevision(ctx, revision.Revision, revision.ActorTemplateUID); err != nil {
		return fmt.Errorf("delete unreferenced runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}
