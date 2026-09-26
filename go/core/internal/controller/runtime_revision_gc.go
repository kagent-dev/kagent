package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const runtimeRevisionGCInterval = time.Minute

type runtimeRevisionGCStore interface {
	ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error)
	BeginRuntimeRevisionDeletion(context.Context, string) (*database.RuntimeRevision, error)
	DeleteRuntimeRevision(context.Context, string, string) error
}

type runtimeRevisionGCClient interface {
	GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error)
	DeleteActorTemplate(context.Context, string, string) error
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

func NewRuntimeRevisionGC(store runtimeRevisionGCStore, templates runtimeRevisionGCClient, provider metric.MeterProvider) (*RuntimeRevisionGC, error) {
	metrics, err := newRuntimeRevisionGCMetrics(provider)
	if err != nil {
		return nil, err
	}
	return &RuntimeRevisionGC{store: store, templates: templates, metrics: metrics}, nil
}

func (r *RuntimeRevisionGC) NeedLeaderElection() bool { return true }

func (r *RuntimeRevisionGC) Start(ctx context.Context) error {
	defer r.metrics.pending.Store(nil)
	ticker := time.NewTicker(runtimeRevisionGCInterval)
	defer ticker.Stop()
	for {
		r.sweep(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *RuntimeRevisionGC) sweep(ctx context.Context) {
	revisions, err := r.discover(ctx)
	if err != nil {
		return
	}
	for _, candidate := range revisions {
		if ctx.Err() != nil {
			return
		}
		if err := r.collect(ctx, candidate.Revision); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to collect runtime revision",
				"revision", candidate.Revision, "actor_template_atespace", candidate.ActorTemplateAtespace,
				"actor_template_name", candidate.ActorTemplateName, "error", err)
		}
	}
	// Refresh persisted eligibility, not an optimistic decrement after deletion.
	_, _ = r.discover(ctx)
}

func (r *RuntimeRevisionGC) discover(ctx context.Context) ([]database.RuntimeRevision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	started := time.Now()
	listCtx, cancel := context.WithTimeout(ctx, time.Minute)
	revisions, err := r.store.ListUnreferencedRuntimeRevisions(listCtx)
	cancel()
	r.metrics.recordDuration(ctx, gcStageDiscovery, time.Since(started), err)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list unreferenced runtime revisions", "error", err)
		return nil, err
	}
	r.metrics.recordPending(int64(len(revisions)))
	return revisions, nil
}

// collect retains the database row until compute deletion succeeds. Each candidate
// has its own deadline so a stuck backend or database lock cannot stall the sweep.
func (r *RuntimeRevisionGC) collect(ctx context.Context, id string) (err error) {
	started := time.Now()
	defer func() {
		r.metrics.recordDuration(ctx, gcStageCollection, time.Since(started), err)
	}()
	collectionCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	revision, err := r.store.BeginRuntimeRevisionDeletion(collectionCtx, id)
	if err != nil {
		return fmt.Errorf("begin deletion of runtime revision %s: %w", id, err)
	}
	if revision == nil {
		return nil
	}
	template, err := r.templates.GetActorTemplate(collectionCtx, revision.ActorTemplateAtespace, revision.ActorTemplateName)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("get unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	if err == nil && (revision.ActorTemplateUID == "" || template.GetMetadata().GetUid() != revision.ActorTemplateUID) {
		return fmt.Errorf("unreferenced ActorTemplate %s/%s UID changed", revision.ActorTemplateAtespace, revision.ActorTemplateName)
	}
	// Both deletes tolerate already-missing objects. If runtime cleanup succeeds
	// but database finalization fails, the durable deletion marker keeps this
	// revision discoverable so the next sweep can safely retry the sequence.
	if err := r.templates.DeleteActorTemplate(collectionCtx, revision.ActorTemplateAtespace, revision.ActorTemplateName); err != nil {
		return fmt.Errorf("delete unreferenced ActorTemplate %s/%s: %w", revision.ActorTemplateAtespace, revision.ActorTemplateName, err)
	}
	if err := r.store.DeleteRuntimeRevision(collectionCtx, revision.Revision, revision.ActorTemplateUID); err != nil {
		return fmt.Errorf("delete unreferenced runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}
