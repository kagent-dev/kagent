package system

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
)

/*
Paged substrate reads.

ate-api pages and does nothing else: ListActors and ListWorkers take a page size and
a token, and answer with a page and a token. No ordering, no filter, no total. So the
calls here page and do nothing else either — ordering a page is the caller's, over the
rows it was handed, and a caller that presents that as ordering the cluster is lying
to its reader.

The counts live on GetSubstrateSummary instead, which walks every ate-api page and
keeps only the tallies. That walk is the expensive read on this page and the one to
poll least often, but it crosses the wire as a handful of integers, so it has no
message-size ceiling — which is the whole difference from GetSubstrateStatus.
*/

// How many rows a list call asks ate-api for when the caller names no page size.
const defaultSubstratePageSize int32 = 50

// The largest page a caller may ask for. Refused rather than clamped, so a caller
// learns its page size was not honoured; also declared on the request in system.proto,
// which is what actually rejects an oversized one before this code runs.
const maxSubstratePageSize int32 = 100

/*
How many ate-api pages one list call will read to fill one page of its own.

Rows outside the requested scope are dropped after ate-api has counted them into its
page, so a narrow scope on a wide cluster can turn a page of 100 into a page of 3.
Reading a few more ate-api pages keeps that from rendering as "no actors" on a cluster
that has plenty; a bound keeps it from becoming the whole-inventory walk this replaced.
A short page with a next token is the honest answer once the bound is reached.
*/
const maxATEPagesPerRequest = 10

// SubstrateListInput is what both paged substrate reads take.
type SubstrateListInput struct {
	// Empty means every namespace the controller observes.
	Namespace string
	// Zero means defaultSubstratePageSize.
	PageSize int32
	// Empty for the first page; otherwise the previous answer's NextPageToken.
	PageToken string
}

// SubstrateActorPage is one page of actors, as read.
type SubstrateActorPage struct {
	Enabled       bool
	ATEAPIError   string
	Actors        []SubstrateActor
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateWorkerPage is one page of workers. The mirror of SubstrateActorPage.
type SubstrateWorkerPage struct {
	Enabled       bool
	ATEAPIError   string
	Workers       []SubstrateWorker
	NextPageToken string
	ComputedAt    time.Time
}

// SubstrateSummary is the inventory as counts, plus the two lists whose length is set
// by configuration rather than by the cluster.
type SubstrateSummary struct {
	Enabled           bool
	ATEAPIError       string
	WorkerPools       []SubstrateWorkerPool
	ActorTemplates    []SubstrateActorTemplate
	ActorCount        int64
	WorkerCount       int64
	RunningActorCount int64
	BusyWorkerCount   int64
	ActorStatusCounts []SubstrateActorStatusCount
	ComputedAt        time.Time
}

// SubstrateActorStatusCount is one status and how many actors hold it.
type SubstrateActorStatusCount struct {
	Status string
	Count  int64
}

// GetSubstrateSummary counts the inventory without sending it.
//
// Every count here costs a walk of every ate-api page, because ate-api reports no
// totals of its own. The walk holds one page at a time and keeps only tallies, so its
// cost is time rather than memory, and its answer is small enough to send whatever the
// cluster's size.
func (s *Service) GetSubstrateSummary(ctx context.Context, requestedNamespace string) (SubstrateSummary, error) {
	namespaces, err := s.substrateScope(ctx, requestedNamespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
		Enabled:           s.ateClient != nil,
		WorkerPools:       []SubstrateWorkerPool{},
		ActorTemplates:    []SubstrateActorTemplate{},
		ActorStatusCounts: []SubstrateActorStatusCount{},
		ComputedAt:        time.Now().UTC(),
	}
	if s.ateClient == nil {
		return result, nil
	}
	if s.kubeClient == nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", fmt.Errorf("kubernetes client is not configured"))
	}
	if s.revisions == nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list ActorTemplate harnesses", fmt.Errorf("runtime revision store is not configured"))
	}

	for _, namespace := range namespaces {
		workerPools, err := s.listWorkerPools(ctx, namespace)
		if err != nil {
			return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		result.WorkerPools = append(result.WorkerPools, workerPools...)
	}
	slices.SortStableFunc(result.WorkerPools, func(left, right SubstrateWorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})

	allowAll, allowed := substrateScopeFilter(namespaces)

	/*
	 * An ate-api failure leaves the counts short and the Kubernetes halves complete,
	 * which is a warning to show beside the data rather than a reason to fail the call
	 * — the same contract GetSubstrateStatus has. Whatever was tallied before the
	 * failure is kept: a partial count next to the words that say it is partial beats
	 * an empty page.
	 */
	templates, err := s.substrateActorTemplates(ctx, allowAll, allowed)
	if err == nil {
		result.ActorTemplates = templates
	} else {
		result.ATEAPIError = err.Error()
	}

	statusCounts := map[string]int64{}
	busyWorkers := map[string]struct{}{}
	if result.ATEAPIError == "" {
		err = s.walkActors(ctx, func(actor *ateapipb.Actor) {
			if actor == nil || !allowedAtespace(actor.GetActorTemplate().GetAtespace(), allowAll, allowed) {
				return
			}
			entry := actorFromProto(actor)
			result.ActorCount++
			statusCounts[entry.Status]++
			if strings.EqualFold(entry.Status, "Running") {
				result.RunningActorCount++
			}
			// A worker is busy when an actor is placed on it. The binding is on the
			// actor, not the worker: ate-api's Worker carries capacity and allocation
			// but no actor reference, so this walk is the only place it can be counted.
			if entry.AteomPodName != "" {
				busyWorkers[entry.AteomPodNamespace+"/"+entry.AteomPodName] = struct{}{}
			}
		})
		if err != nil {
			result.ATEAPIError = err.Error()
		}
	}

	if result.ATEAPIError == "" {
		err = s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
			if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
				return
			}
			result.WorkerCount++
		})
		if err != nil {
			result.ATEAPIError = err.Error()
		}
	}

	if result.ATEAPIError != "" {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to summarise ate-api state", "error", result.ATEAPIError)
	}

	result.BusyWorkerCount = int64(len(busyWorkers))
	result.ActorStatusCounts = make([]SubstrateActorStatusCount, 0, len(statusCounts))
	for _, status := range slices.Sorted(maps.Keys(statusCounts)) {
		result.ActorStatusCounts = append(result.ActorStatusCounts, SubstrateActorStatusCount{
			Status: status,
			Count:  statusCounts[status],
		})
	}
	return result, nil
}

// ListSubstrateActors answers with one page of actors, ate-api's token passed through.
func (s *Service) ListSubstrateActors(ctx context.Context, input SubstrateListInput) (SubstrateActorPage, error) {
	namespaces, err := s.substrateScope(ctx, input.Namespace)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	pageSize, err := substratePageSize(input.PageSize)
	if err != nil {
		return SubstrateActorPage{}, err
	}

	result := SubstrateActorPage{
		Enabled:    s.ateClient != nil,
		Actors:     []SubstrateActor{},
		ComputedAt: time.Now().UTC(),
	}
	if s.ateClient == nil {
		return result, nil
	}

	allowAll, allowed := substrateScopeFilter(namespaces)
	token := input.PageToken
	for range maxATEPagesPerRequest {
		actors, next, err := s.ateClient.ListActorsPage(ctx, "", pageSize, token)
		if err != nil {
			/*
			 * A page that could not be read is an empty page with the error beside it,
			 * not a failed call — the same contract the summary has, and for the same
			 * reason: the request succeeded and only the runtime half is missing.
			 *
			 * NextPageToken stays empty, so the caller does not advance past a page it
			 * never saw. Its own token is unchanged, so retrying asks for this page again.
			 */
			result.ATEAPIError = err.Error()
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
			return result, nil
		}
		for _, actor := range actors {
			if actor == nil || !allowedAtespace(actor.GetActorTemplate().GetAtespace(), allowAll, allowed) {
				continue
			}
			result.Actors = append(result.Actors, actorFromProto(actor))
		}
		token = next
		if token == "" || int32(len(result.Actors)) >= pageSize {
			break
		}
	}
	result.NextPageToken = token
	return result, nil
}

// ListSubstrateWorkers answers with one page of workers. The mirror of ListSubstrateActors.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input SubstrateListInput) (SubstrateWorkerPage, error) {
	namespaces, err := s.substrateScope(ctx, input.Namespace)
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	pageSize, err := substratePageSize(input.PageSize)
	if err != nil {
		return SubstrateWorkerPage{}, err
	}

	result := SubstrateWorkerPage{
		Enabled:    s.ateClient != nil,
		Workers:    []SubstrateWorker{},
		ComputedAt: time.Now().UTC(),
	}
	if s.ateClient == nil {
		return result, nil
	}

	allowAll, allowed := substrateScopeFilter(namespaces)
	token := input.PageToken
	for range maxATEPagesPerRequest {
		workers, next, err := s.ateClient.ListWorkersPage(ctx, pageSize, token)
		if err != nil {
			result.ATEAPIError = err.Error()
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
			return result, nil
		}
		for _, worker := range workers {
			if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
				continue
			}
			result.Workers = append(result.Workers, workerFromProto(worker))
		}
		token = next
		if token == "" || int32(len(result.Workers)) >= pageSize {
			break
		}
	}
	result.NextPageToken = token
	return result, nil
}

// walkActors calls visit for every actor ate-api holds, one page at a time.
func (s *Service) walkActors(ctx context.Context, visit func(*ateapipb.Actor)) error {
	token := ""
	for {
		actors, next, err := s.ateClient.ListActorsPage(ctx, "", 0, token)
		if err != nil {
			return err
		}
		for _, actor := range actors {
			visit(actor)
		}
		if next == "" {
			return nil
		}
		token = next
	}
}

// walkWorkers calls visit for every worker ate-api holds, one page at a time.
func (s *Service) walkWorkers(ctx context.Context, visit func(*ateapipb.Worker)) error {
	token := ""
	for {
		workers, next, err := s.ateClient.ListWorkersPage(ctx, 0, token)
		if err != nil {
			return err
		}
		for _, worker := range workers {
			visit(worker)
		}
		if next == "" {
			return nil
		}
		token = next
	}
}

/*
substrateScope authorizes the caller and resolves the namespaces a substrate read
covers.

Shared by all four substrate reads so that a new one cannot arrive without the check:
the authorization and the namespace validation are the same for every one of them.
*/
func (s *Service) substrateScope(ctx context.Context, requestedNamespace string) ([]string, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Substrate"}); err != nil {
		return nil, err
	}
	requestedNamespace = strings.TrimSpace(requestedNamespace)
	if requestedNamespace != "" {
		if validationErrors := utilvalidation.IsDNS1123Label(requestedNamespace); len(validationErrors) > 0 {
			return nil, serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid namespace %q: %s", requestedNamespace, strings.Join(validationErrors, ", ")),
				nil,
			)
		}
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

// allowedWorkerNamespace mirrors allowedAtespace for workers, whose namespace is a
// Kubernetes namespace rather than an atespace. An unnamespaced worker is in scope
// everywhere, as an unnamespaced actor is.
func allowedWorkerNamespace(namespace string, allowAll bool, allowed map[string]struct{}) bool {
	namespace = strings.TrimSpace(namespace)
	if allowAll || namespace == "" {
		return true
	}
	_, ok := allowed[namespace]
	return ok
}

func substratePageSize(requested int32) (int32, error) {
	switch {
	case requested < 0:
		return 0, serviceerrors.NewInvalidArgument(fmt.Sprintf("invalid page size %d: must not be negative", requested), nil)
	case requested == 0:
		return defaultSubstratePageSize, nil
	case requested > maxSubstratePageSize:
		return 0, serviceerrors.NewInvalidArgument(
			fmt.Sprintf("invalid page size %d: the maximum is %d", requested, maxSubstratePageSize),
			nil,
		)
	default:
		return requested, nil
	}
}
