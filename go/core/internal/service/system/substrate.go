package system

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// How many rows a list call asks ate-api for when the caller names no page size.
const defaultSubstratePageSize int32 = 50

const (
	workerPoolResource    = "WorkerPool"
	substrateResource     = "Substrate"
	systemSummaryResource = "SystemSummary"
)

// How many ate-api pages a walk will read before giving up. Each page carries its own
// timeout, so nothing else bounds the loop against a cyclic next_page_token. At
// ate-api's 1,000 rows a page this allows ten million.
const maxATEPagesPerWalk = 10_000

// SubstrateActorPage is one page of actors, as read.
type SubstrateActorPage struct {
	ATEAPIError   string
	Actors        []*ateapipb.Actor
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateWorkerPage is one page of workers. The mirror of SubstrateActorPage.
type SubstrateWorkerPage struct {
	ATEAPIError   string
	Workers       []*ateapipb.Worker
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateSummary is the inventory as counts, plus the two lists whose length is set
// by configuration rather than by the cluster.
type SubstrateSummary struct {
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

// GetSubstrateSummary requires the SystemSummary capability and filters pool rows
// by their collection scope before counting the ATE inventory.
//
// ate-api reports no totals, so every count costs a walk of its pages. The walk holds
// one page at a time and keeps only tallies.
func (s *Service) GetSubstrateSummary(ctx context.Context, requestedNamespace, atespace string) (SubstrateSummary, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: systemSummaryResource}); err != nil {
		return SubstrateSummary{}, err
	}
	poolMatcher, err := kubeauth.CollectionMatcher(ctx, s.authorizer, workerPoolResource)
	if err != nil {
		return SubstrateSummary{}, err
	}
	workerScope, err := s.resolveSubstrateScope(requestedNamespace)
	if err != nil {
		return SubstrateSummary{}, err
	}
	actorScope, err := s.resolveSubstrateScope(atespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
		WorkerPools:       []atev1alpha1.WorkerPool{},
		ActorTemplates:    []*ateapipb.ActorTemplate{},
		ActorStatusCounts: []SubstrateActorStatusCount{},
		ComputedAt:        time.Now().UTC(),
	}

	poolNamespaces := workerScope.listNamespaces()
	if poolMatcher.Empty() {
		poolNamespaces = nil
	}
	for _, namespace := range poolNamespaces {
		workerPools, err := s.listWorkerPools(ctx, namespace)
		if err != nil {
			return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		for index := range workerPools {
			if poolMatcher.Matches(&workerPools[index]) {
				result.WorkerPools = append(result.WorkerPools, workerPools[index])
			}
		}
	}
	slices.SortStableFunc(result.WorkerPools, func(left, right atev1alpha1.WorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
	if templates, err := s.substrateActorTemplates(ctx, actorScope); err != nil {
		result.recordATEError(ctx, err)
	} else {
		result.ActorTemplates = templates
	}

	statusCounts := map[ateapipb.ActorState]int64{}
	if err := s.walkActors(ctx, actorScope, func(actor *ateapipb.Actor) {
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
		if worker == nil || !workerScope.contains(worker.GetWorkerNamespace()) {
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

// ListSubstrateActors returns one page of actors.
func (s *Service) ListSubstrateActors(ctx context.Context, input *apiv1.ListSubstrateActorsRequest) (SubstrateActorPage, error) {
	scope, err := s.authorizeSubstrateScope(ctx, input.GetAtespace())
	if err != nil {
		return SubstrateActorPage{}, err
	}
	limit, token := substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken()
	var actors []*ateapipb.Actor
	var next string
	if len(s.observedNamespaces) == 0 {
		actors, next, err = s.ateClient.ListActorsPage(ctx, input.GetAtespace(), limit, token)
	} else {
		start, inner, tokenErr := parseScopedPageToken(scope.namespaces, token)
		if tokenErr != nil {
			return SubstrateActorPage{}, tokenErr
		}
		actors, next, err = s.listScopedActors(ctx, scope.namespaces[start:], inner, limit)
	}
	result := SubstrateActorPage{ComputedAt: time.Now().UTC()}
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		return result, nil
	}
	for _, actor := range actors {
		if actor != nil && scope.contains(actor.GetMetadata().GetAtespace()) {
			result.Actors = append(result.Actors, actor)
		}
	}
	result.NextPageToken = next
	return result, nil
}

const maxPageTokenBytes = 1024

// scopedPageToken is the cursor a controller that watches some namespaces hands out:
// the watched namespace a page ended in, then where to resume.
func scopedPageToken(namespace, cursor string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(namespace + "/" + cursor))
}

func parseScopedPageToken(namespaces []string, token string) (int, string, error) {
	if token == "" {
		return 0, "", nil
	}
	if len(token) > maxPageTokenBytes {
		return 0, "", invalidPageToken()
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	namespace, cursor, ok := strings.Cut(string(decoded), "/")
	start := slices.Index(namespaces, namespace)
	if err != nil || !ok || start < 0 {
		return 0, "", invalidPageToken()
	}
	return start, cursor, nil
}

func invalidPageToken() error {
	return serviceerrors.NewInvalidArgument("Invalid page token", nil)
}

func (s *Service) listScopedActors(ctx context.Context, atespaces []string, inner string, limit int32) ([]*ateapipb.Actor, string, error) {
	var actors []*ateapipb.Actor
	for i, atespace := range atespaces {
		rows, next, err := s.ateClient.ListActorsPage(ctx, atespace, limit-int32(len(actors)), inner)
		if err != nil {
			return nil, "", err
		}
		actors = append(actors, rows...)
		switch {
		case next != "":
			return actors, scopedPageToken(atespace, next), nil
		case int32(len(actors)) >= limit && i+1 < len(atespaces):
			return actors, scopedPageToken(atespaces[i+1], ""), nil
		}
		inner = ""
	}
	return actors, "", nil
}

// ListSubstrateWorkers returns one page of workers.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input *apiv1.ListSubstrateWorkersRequest) (SubstrateWorkerPage, error) {
	scope, err := s.authorizeSubstrateScope(ctx, input.GetNamespace())
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	limit, token := substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken()
	if len(s.observedNamespaces) > 0 {
		_, after, err := parseScopedPageToken(scope.namespaces, token)
		if err == nil && token != "" && after == "" {
			err = invalidPageToken()
		}
		if err != nil {
			return SubstrateWorkerPage{}, err
		}
		return s.listWatchedWorkers(ctx, scope, limit, after), nil
	}
	result := SubstrateWorkerPage{ComputedAt: time.Now().UTC()}
	workers, next, err := s.ateClient.ListWorkersPage(ctx, limit, token)
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return result, nil
	}
	for _, worker := range workers {
		if worker != nil && scope.contains(worker.GetWorkerNamespace()) {
			result.Workers = append(result.Workers, worker)
		}
	}
	result.NextPageToken = next
	return result, nil
}

func (s *Service) listWatchedWorkers(ctx context.Context, scope substrateScope, limit int32, after string) SubstrateWorkerPage {
	result := SubstrateWorkerPage{ComputedAt: time.Now().UTC()}
	var workers []*ateapipb.Worker
	if err := s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
		if scope.contains(worker.GetWorkerNamespace()) && worker.GetMetadata().GetName() > after {
			workers = append(workers, worker)
		}
	}); err != nil {
		result.ATEAPIError = err.Error()
		return result
	}
	slices.SortFunc(workers, func(left, right *ateapipb.Worker) int {
		return strings.Compare(left.GetMetadata().GetName(), right.GetMetadata().GetName())
	})
	if int32(len(workers)) > limit {
		workers = workers[:limit]
		last := workers[limit-1]
		result.NextPageToken = scopedPageToken(last.GetWorkerNamespace(), last.GetMetadata().GetName())
	}
	result.Workers = workers
	return result
}

func (s *Service) walkActors(ctx context.Context, scope substrateScope, visit func(*ateapipb.Actor)) error {
	for _, atespace := range scope.listNamespaces() {
		read := func(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
			return s.ateClient.ListActorsPage(ctx, atespace, pageSize, pageToken)
		}
		if err := walkSubstrate(ctx, read, visit); err != nil {
			return err
		}
	}
	return nil
}

// walkWorkers calls visit for every worker ate-api holds, one page at a time.
// ate-api's ListWorkersRequest has no namespace filter, so a controller that watches
// some namespaces still reads every tenant's workers.
func (s *Service) walkWorkers(ctx context.Context, visit func(*ateapipb.Worker)) error {
	err := walkSubstrate(ctx, s.ateClient.ListWorkersPage, visit)
	if err != nil && len(s.observedNamespaces) > 0 {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return fmt.Errorf("ate-api could not list workers: %s", status.Code(err))
	}
	return err
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

type substrateScope struct {
	all        bool
	namespaces []string
}

func (s *Service) authorizeSubstrateScope(ctx context.Context, requested string) (substrateScope, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: substrateResource}); err != nil {
		return substrateScope{}, err
	}
	return s.resolveSubstrateScope(requested)
}

func (s *Service) resolveSubstrateScope(requested string) (substrateScope, error) {
	switch {
	case requested == "" && len(s.observedNamespaces) == 0:
		return substrateScope{all: true}, nil
	case requested == "":
		return substrateScope{namespaces: slices.Clone(s.observedNamespaces)}, nil
	case len(s.observedNamespaces) == 0 || slices.Contains(s.observedNamespaces, requested):
		return substrateScope{namespaces: []string{requested}}, nil
	default:
		return substrateScope{}, serviceerrors.NewPermissionDenied(fmt.Sprintf("Namespace %q is not watched by this controller", requested), nil)
	}
}

func (scope substrateScope) contains(namespace string) bool {
	return scope.all || slices.Contains(scope.namespaces, namespace)
}

func (scope substrateScope) listNamespaces() []string {
	if scope.all {
		return []string{""}
	}
	return scope.namespaces
}

func substratePageSize(requested int32) int32 {
	if requested == 0 {
		return defaultSubstratePageSize
	}
	return requested
}
