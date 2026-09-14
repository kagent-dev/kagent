package system

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strconv"
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

/*
Paged substrate reads.

ate-api pages and does nothing else — no order, no filter, no total — so every read
here walks all of its pages and does those three itself. The walks cost time, but
they answer with a page or a tally rather than the inventory, which is the whole
difference from GetSubstrateStatus and its message-size ceiling.
*/

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
	// How many actors match the filter across every page.
	TotalSize        int64
	AppliedSortField apiv1alpha1.SubstrateActorSortField
	AppliedSortOrder apiv1alpha1.SubstrateSortOrder
}

// SubstrateWorkerPage is one page of workers. The mirror of SubstrateActorPage.
type SubstrateWorkerPage struct {
	Enabled          bool
	ATEAPIError      string
	Workers          []*ateapipb.Worker
	NextPageToken    string
	ComputedAt       time.Time
	TotalSize        int64
	AppliedSortField apiv1alpha1.SubstrateWorkerSortField
	AppliedSortOrder apiv1alpha1.SubstrateSortOrder
}

// SubstrateSummary is the inventory as counts, plus the two lists whose length is set
// by configuration rather than by the cluster.
type SubstrateSummary struct {
	Enabled           bool
	ATEAPIError       string
	WorkerPools       []atev1alpha1.WorkerPool
	ActorTemplates    []SubstrateActorTemplate
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
		ActorTemplates:    []SubstrateActorTemplate{},
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

	// Read here rather than inside the template listing below, so a PostgreSQL outage
	// is an internal error instead of being reported as an ate-api one.
	harnesses, err := s.actorTemplateHarnesses(ctx)
	if err != nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list ActorTemplate harnesses", err)
	}

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
	if templates, err := s.substrateActorTemplates(ctx, harnesses, atespace); err != nil {
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

// ListSubstrateActors answers with one page of actors, ordered and narrowed across the
// whole inventory.
func (s *Service) ListSubstrateActors(ctx context.Context, input *apiv1alpha1.ListSubstrateActorsRequest) (SubstrateActorPage, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Substrate"}); err != nil {
		return SubstrateActorPage{}, err
	}
	pageSize := substratePageSize(input.GetPage().GetLimit())
	offset, err := decodeSubstrateOffset(input.GetPage().GetPageToken())
	if err != nil {
		return SubstrateActorPage{}, err
	}

	sortField := input.GetSortField()
	result := SubstrateActorPage{
		Enabled:          true,
		Actors:           []*ateapipb.Actor{},
		ComputedAt:       time.Now().UTC(),
		AppliedSortField: sortField,
		AppliedSortOrder: substrateSortOrder(input.GetSortOrder()),
	}

	matching := []*ateapipb.Actor{}
	needle := strings.ToLower(strings.TrimSpace(input.GetFilter()))
	if err := s.walkActors(ctx, input.GetAtespace(), func(actor *ateapipb.Actor) {
		if actor == nil {
			return
		}
		if !matchesFilter(needle, actorSearchText(actor)) {
			return
		}
		matching = append(matching, actor)
	}); err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		return result, nil
	}

	slices.SortStableFunc(matching, substrateOrder(actorSortKey(sortField), input.GetSortOrder()))
	page, next := sliceSubstratePage(matching, offset, pageSize)
	result.Actors = page
	result.NextPageToken = next
	result.TotalSize = int64(len(matching))
	return result, nil
}

// ListSubstrateWorkers answers with one page of workers. The mirror of ListSubstrateActors.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input *apiv1alpha1.ListSubstrateWorkersRequest) (SubstrateWorkerPage, error) {
	namespaces, err := s.substrateScope(ctx, input.GetNamespace())
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	pageSize := substratePageSize(input.GetPage().GetLimit())
	offset, err := decodeSubstrateOffset(input.GetPage().GetPageToken())
	if err != nil {
		return SubstrateWorkerPage{}, err
	}

	sortField := input.GetSortField()
	result := SubstrateWorkerPage{
		Enabled:          true,
		Workers:          []*ateapipb.Worker{},
		ComputedAt:       time.Now().UTC(),
		AppliedSortField: sortField,
		AppliedSortOrder: substrateSortOrder(input.GetSortOrder()),
	}

	allowAll, allowed := substrateScopeFilter(namespaces)
	matching := []*ateapipb.Worker{}
	needle := strings.ToLower(strings.TrimSpace(input.GetFilter()))
	if err := s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
		if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			return
		}
		if !matchesFilter(needle, workerSearchText(worker)) {
			return
		}
		matching = append(matching, worker)
	}); err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return result, nil
	}

	slices.SortStableFunc(matching, substrateOrder(workerSortKey(sortField), input.GetSortOrder()))
	page, next := sliceSubstratePage(matching, offset, pageSize)
	result.Workers = page
	result.NextPageToken = next
	result.TotalSize = int64(len(matching))
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

// The order, the filter and the slice, which ate-api offers none of.

// matchesFilter reports whether a row's own text contains the needle.
func matchesFilter(needle, text string) bool {
	return needle == "" || strings.Contains(strings.ToLower(text), needle)
}

// actorSearchText is everything an actor row shows, including what a column composes
// out of several fields, so a search matches what the reader can see.
func actorSearchText(actor *ateapipb.Actor) string {
	return strings.Join([]string{
		actor.GetMetadata().GetName(),
		actor.GetMetadata().GetAtespace(),
		actorIdentity(actor),
		substrate.ActorStatusLabel(actor.GetStatus().GetState()),
		actor.GetActorTemplate().GetAtespace(),
		actor.GetActorTemplate().GetName(),
		actor.GetActorTemplate().GetAtespace() + "/" + actor.GetActorTemplate().GetName(),
		actor.GetStatus().GetWorkerAssignment().GetWorkerNamespace(),
		actor.GetStatus().GetWorkerAssignment().GetWorkerPod(),
		actor.GetStatus().GetWorkerAssignment().GetWorkerNamespace() + "/" + actor.GetStatus().GetWorkerAssignment().GetWorkerPod(),
		actor.GetStatus().GetWorkerAssignment().GetWorkerPodIp(),
	}, " ")
}

func workerSearchText(worker *ateapipb.Worker) string {
	return strings.Join([]string{
		worker.WorkerNamespace,
		worker.WorkerPool,
		worker.WorkerPod,
		worker.WorkerNamespace + "/" + worker.WorkerPod,
		worker.GetIp(),
	}, " ")
}

func actorIdentity(actor *ateapipb.Actor) string {
	return actor.GetMetadata().GetAtespace() + "/" + actor.GetMetadata().GetName()
}

// actorSortKey turns a column into the string a row is ordered by. Every key ends in
// the actor's atespace and name: an order whose last key repeats gives a page boundary naming
// more than one row, and paging across it drops or repeats them.
func actorSortKey(field apiv1alpha1.SubstrateActorSortField) func(*ateapipb.Actor) string {
	switch field {
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_ID:
		return actorIdentity
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_TEMPLATE:
		return func(a *ateapipb.Actor) string {
			return a.GetActorTemplate().GetAtespace() + "/" + a.GetActorTemplate().GetName() + "\x00" + actorIdentity(a)
		}
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_WORKER_POD:
		return func(a *ateapipb.Actor) string {
			assignment := a.GetStatus().GetWorkerAssignment()
			return assignment.GetWorkerNamespace() + "/" + assignment.GetWorkerPod() + "\x00" + actorIdentity(a)
		}
	default:
		// Status and the default are one ordering, so the Status header changes nothing
		// ascending and reverses descending. Correct, and not obvious.
		return func(a *ateapipb.Actor) string {
			return substrate.ActorStatusLabel(a.GetStatus().GetState()) + "\x00" + actorIdentity(a)
		}
	}
}

func workerSortKey(field apiv1alpha1.SubstrateWorkerSortField) func(*ateapipb.Worker) string {
	pod := func(w *ateapipb.Worker) string { return w.WorkerNamespace + "/" + w.WorkerPod }
	switch field {
	case apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POD:
		return pod
	case apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_IP:
		return func(w *ateapipb.Worker) string { return w.GetIp() + "\x00" + pod(w) }
	default:
		// Pool and the default are one ordering, as status and the default are above.
		return func(w *ateapipb.Worker) string { return w.WorkerPool + "\x00" + pod(w) }
	}
}

// substrateOrder compares two rows by their sort key, reversed for a descending read.
func substrateOrder[Row any](key func(Row) string, order apiv1alpha1.SubstrateSortOrder) func(Row, Row) int {
	descending := substrateSortOrder(order) == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC
	return func(left, right Row) int {
		compared := strings.Compare(key(left), key(right))
		if descending {
			return -compared
		}
		return compared
	}
}

// substrateSortOrder defaults an unset order to ascending.
func substrateSortOrder(order apiv1alpha1.SubstrateSortOrder) apiv1alpha1.SubstrateSortOrder {
	if order == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_UNSPECIFIED {
		return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_ASC
	}
	return order
}

/*
sliceSubstratePage cuts the requested page out of the ordered result.

An offset rather than a key-based cursor: the order is the controller's and is rebuilt
per request, so a key would name a position the next request may not produce. An offset
past the end is an empty last page, not an error — the cluster may have shrunk.
*/
func sliceSubstratePage[Row any](rows []Row, offset int, pageSize int32) ([]Row, string) {
	if offset >= len(rows) {
		return []Row{}, ""
	}
	end := min(offset+int(pageSize), len(rows))
	page := rows[offset:end]
	if end >= len(rows) {
		return page, ""
	}
	return page, encodeSubstrateOffset(end)
}

// Encoded so the token reads as opaque, the same shape the other paged reads use.
func encodeSubstrateOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeSubstrateOffset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, serviceerrors.NewInvalidArgument("invalid page token", err)
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, serviceerrors.NewInvalidArgument("invalid page token", err)
	}
	return offset, nil
}
