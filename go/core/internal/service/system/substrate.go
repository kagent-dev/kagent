package system

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// How many rows a list call asks ate-api for when the caller names no page size.
const defaultSubstratePageSize int32 = 50

// How many ate-api pages a walk will read before giving up. Each page carries its own
// timeout, so nothing else bounds the loop against a cyclic next_page_token. At
// ate-api's 1,000 rows a page this allows ten million.
const maxATEPagesPerWalk = 10_000

// SubstrateActorPage is one page of actors, as read.
type SubstrateActorPage struct {
	Enabled       bool
	ATEAPIError   string
	Actors        []*ateapipb.Actor
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateWorkerPage is one page of workers. The mirror of SubstrateActorPage.
type SubstrateWorkerPage struct {
	Enabled       bool
	ATEAPIError   string
	Workers       []*ateapipb.Worker
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateSummary is the inventory as counts, plus the two lists whose length is set
// by configuration rather than by the cluster.
type SubstrateSummary struct {
	Enabled           bool
	ATEAPIError       string
	WorkerPools       []atev1alpha1.WorkerPool
	ActorTemplates    []*ateapipb.ActorTemplate
	ActorCount        int64
	WorkerCount       int64
	RunningActorCount int64
	BusyWorkerCount   int64
	ActorStatusCounts []SubstrateActorStatusCount
	ComputedAt        time.Time
}

// recordATEError keeps the first of the summary's three ate-api failures: when all
// three fail together, the earliest is the one that explains the others.
func (summary *SubstrateSummary) recordATEError(ctx context.Context, err error) {
	logging.FromContext(ctx).ErrorContext(ctx, "failed to summarise ate-api state", "error", err)
	if summary.ATEAPIError == "" {
		summary.ATEAPIError = err.Error()
	}
}

// SubstrateActorStatusCount is one status and how many actors hold it.
type SubstrateActorStatusCount struct {
	State ateapipb.ActorState
	Count int64
}

// GetSubstrateSummary counts the inventory without sending it.
//
// ate-api reports no totals, so every count costs a walk of its pages. The walk holds
// one page at a time and keeps only tallies.
func (s *Service) GetSubstrateSummary(ctx context.Context, requestedNamespace, atespace string) (SubstrateSummary, error) {
	namespaces, err := s.substrateScope(ctx, requestedNamespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
		Enabled:           true,
		WorkerPools:       []atev1alpha1.WorkerPool{},
		ActorTemplates:    []*ateapipb.ActorTemplate{},
		ActorStatusCounts: []SubstrateActorStatusCount{},
		ComputedAt:        time.Now().UTC(),
	}

	for _, namespace := range namespaces {
		workerPools, err := s.listWorkerPools(ctx, namespace)
		if err != nil {
			return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		result.WorkerPools = append(result.WorkerPools, workerPools...)
	}
	slices.SortStableFunc(result.WorkerPools, func(left, right atev1alpha1.WorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})

	allowAll, allowed := substrateScopeFilter(namespaces)

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
	if templates, err := s.substrateActorTemplates(ctx, atespace); err != nil {
		result.recordATEError(ctx, err)
	} else {
		result.ActorTemplates = templates
	}

	statusCounts := map[ateapipb.ActorState]int64{}
	if err := s.walkActors(ctx, atespace, func(actor *ateapipb.Actor) {
		if actor == nil {
			return
		}
		result.ActorCount++
		statusCounts[actor.GetStatus().GetState()]++
		if actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_RUNNING {
			result.RunningActorCount++
		}
	}); err != nil {
		result.recordATEError(ctx, err)
	}

	if err := s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
		if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			return
		}
		result.WorkerCount++
		if worker.GetStatus().GetAllocated().GetActors() > 0 {
			result.BusyWorkerCount++
		}
	}); err != nil {
		result.recordATEError(ctx, err)
	}

	result.ActorStatusCounts = make([]SubstrateActorStatusCount, 0, len(statusCounts))
	for _, status := range slices.Sorted(maps.Keys(statusCounts)) {
		result.ActorStatusCounts = append(result.ActorStatusCounts, SubstrateActorStatusCount{
			State: status,
			Count: statusCounts[status],
		})
	}
	return result, nil
}

// ListSubstrateActors returns one upstream page without changing its order or token.
func (s *Service) ListSubstrateActors(ctx context.Context, input *apiv1alpha1.ListSubstrateActorsRequest) (SubstrateActorPage, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Substrate"}); err != nil {
		return SubstrateActorPage{}, err
	}
	result := SubstrateActorPage{Enabled: true, ComputedAt: time.Now().UTC()}
	actors, next, err := s.ateClient.ListActorsPage(ctx, input.GetAtespace(), substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken())
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		return result, nil
	}
	result.Actors = actors
	result.NextPageToken = next
	return result, nil
}

// ListSubstrateWorkers filters one upstream page by namespace and preserves its token.
// A page with no workers in scope may still have a next page.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input *apiv1alpha1.ListSubstrateWorkersRequest) (SubstrateWorkerPage, error) {
	namespaces, err := s.substrateScope(ctx, input.GetNamespace())
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	result := SubstrateWorkerPage{Enabled: true, ComputedAt: time.Now().UTC()}
	workers, next, err := s.ateClient.ListWorkersPage(ctx, substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken())
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return result, nil
	}
	allowAll, allowed := substrateScopeFilter(namespaces)
	for _, worker := range workers {
		if worker != nil && allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			result.Workers = append(result.Workers, worker)
		}
	}
	result.NextPageToken = next
	return result, nil
}

// walkActors visits every actor in the requested atespace, one page at a time.
func (s *Service) walkActors(ctx context.Context, atespace string, visit func(*ateapipb.Actor)) error {
	read := func(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
		return s.ateClient.ListActorsPage(ctx, atespace, pageSize, pageToken)
	}
	return walkSubstrate(ctx, read, visit)
}

// walkWorkers calls visit for every worker ate-api holds, one page at a time.
func (s *Service) walkWorkers(ctx context.Context, visit func(*ateapipb.Worker)) error {
	return walkSubstrate(ctx, s.ateClient.ListWorkersPage, visit)
}

// walkSubstrate calls visit for every row ate-api holds, holding one page at a time
// whatever the cluster's size.
func walkSubstrate[Row any](
	ctx context.Context,
	read func(ctx context.Context, pageSize int32, pageToken string) ([]Row, string, error),
	visit func(Row),
) error {
	token := ""
	for range maxATEPagesPerWalk {
		rows, next, err := read(ctx, 0, token)
		if err != nil {
			return err
		}
		for _, row := range rows {
			visit(row)
		}
		if next == "" {
			return nil
		}
		if token, err = substrate.AdvancePageToken(token, next); err != nil {
			return err
		}
	}
	return fmt.Errorf("ate-api did not finish paging after %d pages", maxATEPagesPerWalk)
}

// substrateScope authorizes the caller and resolves the namespaces a read covers.
// ATE-only actor reads authorize independently of Kubernetes scope.
func (s *Service) substrateScope(ctx context.Context, requestedNamespace string) ([]string, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Substrate"}); err != nil {
		return nil, err
	}
	return s.substrateNamespaces(requestedNamespace), nil
}

// substrateScopeFilter turns resolved namespaces into the pair every row filter here
// takes: whether every namespace is in scope, and the set that is when it is not.
func substrateScopeFilter(namespaces []string) (bool, map[string]struct{}) {
	allowAll := len(namespaces) == 1 && namespaces[0] == ""
	allowed := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}
	return allowAll, allowed
}

// allowedWorkerNamespace filters workers by their Kubernetes namespace.
func allowedWorkerNamespace(namespace string, allowAll bool, allowed map[string]struct{}) bool {
	namespace = strings.TrimSpace(namespace)
	if allowAll || namespace == "" {
		return true
	}
	_, ok := allowed[namespace]
	return ok
}

func substratePageSize(requested int32) int32 {
	if requested == 0 {
		return defaultSubstratePageSize
	}
	return requested
}
