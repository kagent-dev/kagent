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

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
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
How many ate-api pages a counting walk will read before giving up.

A drain used to be bounded by a deadline: the client wrapped the whole loop in one
call timeout, so a token that never drained ran out of time. Each page now carries its
own timeout, which is right — a page should not be charged for the pages before it —
and it leaves the loop itself unbounded. A cyclic or non-advancing `next_page_token`
would then spin against ate-api until the inbound request was cancelled.

At ate-api's own ceiling of 1,000 rows a page this allows ten million actors, which is
far above any cluster this has to count and far below forever.
*/
const maxATEPagesPerWalk = 10_000

// SubstrateListInput is what both paged substrate reads take.
type SubstrateListInput struct {
	// Empty means every namespace the controller observes.
	Namespace string
	// Zero means defaultSubstratePageSize. An `int` as the other paged services take
	// it; ate-api's own request is what wants the int32.
	PageSize int
	// Empty for the first page; otherwise the previous answer's NextPageToken.
	PageToken string
	// Matched case-insensitively as a substring of what the row shows. Empty matches
	// everything.
	Filter string
	// Which column to order by, and which way. Zero values are the read's default
	// order, which is the one every column ends in.
	SortField int32
	SortOrder int32
}

// SubstrateActorPage is one page of actors, as read.
type SubstrateActorPage struct {
	Enabled       bool
	ATEAPIError   string
	Actors        []SubstrateActor
	NextPageToken string
	ComputedAt    time.Time
	// How many actors match the filter across every page.
	TotalSize        int64
	AppliedSortField int32
	AppliedSortOrder int32
}

// SubstrateWorkerPage is one page of workers. The mirror of SubstrateActorPage.
type SubstrateWorkerPage struct {
	Enabled          bool
	ATEAPIError      string
	Workers          []SubstrateWorker
	NextPageToken    string
	ComputedAt       time.Time
	TotalSize        int64
	AppliedSortField int32
	AppliedSortOrder int32
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

/*
recordATEError keeps the first ate-api failure of the summary's three reads.

The first rather than the last, because the reads run in a fixed order and the earliest
failure is the one most likely to explain the others — a controller that has lost
ate-api fails all three, and reporting the last would name the worker walk for an
outage the template listing already found.
*/
func (summary *SubstrateSummary) recordATEError(ctx context.Context, err error) {
	logging.FromContext(ctx).ErrorContext(ctx, "failed to summarise ate-api state", "error", err)
	if summary.ATEAPIError == "" {
		summary.ATEAPIError = err.Error()
	}
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
	 * The harnesses come from PostgreSQL, and a failure there is an internal error
	 * rather than a warning about ate-api.
	 *
	 * These used to be read inside the template listing, so a database outage arrived
	 * here indistinguishable from an ate-api one: the page told the reader that ate-api
	 * had answered with an error while ate-api was healthy, and — because the counts
	 * were gated on that same field — reported a cluster of 410,110 actors as running
	 * none.
	 */
	harnesses, err := s.actorTemplateHarnesses(ctx)
	if err != nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list ActorTemplate harnesses", err)
	}

	/*
	 * Three independent ate-api reads, each contributing whatever it can.
	 *
	 * None of them gates the others. They are separate calls against separate
	 * collections, so a template listing that fails says nothing about whether the
	 * actors can be counted, and skipping the walks because of it would turn one
	 * failed read into a page reporting zero of everything. What each one reached is
	 * kept; the first failure is reported beside it, and the reader is told the figures
	 * may be short rather than shown a blank page.
	 */
	if templates, err := s.substrateActorTemplates(ctx, harnesses, allowAll, allowed); err != nil {
		result.recordATEError(ctx, err)
	} else {
		result.ActorTemplates = templates
	}

	statusCounts := map[string]int64{}
	busyWorkers := map[string]struct{}{}
	if err := s.walkActors(ctx, func(actor *ateapipb.Actor) {
		if actor == nil || !allowedAtespace(actor.GetActorTemplate().GetAtespace(), allowAll, allowed) {
			return
		}
		entry := actorFromProto(actor)
		result.ActorCount++
		statusCounts[entry.Status]++
		if strings.EqualFold(entry.Status, "Running") {
			result.RunningActorCount++
		}
		/*
		 * A worker is busy when an actor is placed on it. The binding is on the actor,
		 * not the worker: ate-api's Worker carries capacity and allocation but no actor
		 * reference, so this walk is the only place it can be counted.
		 *
		 * Scoped by the pod's namespace rather than by the actor's atespace, because
		 * that is what WorkerCount below is scoped by and the two are shown as one
		 * fraction. An actor in atespace `team` can sit on a pod in namespace `kagent`;
		 * counting it here and not there renders the tile as "1/0".
		 */
		if entry.AteomPodName != "" &&
			allowedWorkerNamespace(entry.AteomPodNamespace, allowAll, allowed) {
			busyWorkers[entry.AteomPodNamespace+"/"+entry.AteomPodName] = struct{}{}
		}
	}); err != nil {
		result.recordATEError(ctx, err)
	}

	if err := s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
		if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			return
		}
		result.WorkerCount++
	}); err != nil {
		result.recordATEError(ctx, err)
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

// ListSubstrateActors answers with one page of actors, ordered and narrowed across the
// whole inventory.
func (s *Service) ListSubstrateActors(ctx context.Context, input SubstrateListInput) (SubstrateActorPage, error) {
	namespaces, err := s.substrateScope(ctx, input.Namespace)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	pageSize, err := substratePageSize(input.PageSize)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	offset, err := decodeSubstrateOffset(input.PageToken)
	if err != nil {
		return SubstrateActorPage{}, err
	}

	sortField := apiv1alpha1.SubstrateActorSortField(input.SortField)
	if _, known := apiv1alpha1.SubstrateActorSortField_name[input.SortField]; !known {
		sortField = apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_UNSPECIFIED
	}
	result := SubstrateActorPage{
		Enabled:          s.ateClient != nil,
		Actors:           []SubstrateActor{},
		ComputedAt:       time.Now().UTC(),
		AppliedSortField: int32(sortField),
		AppliedSortOrder: int32(substrateSortOrder(input.SortOrder)),
	}
	if s.ateClient == nil {
		return result, nil
	}

	allowAll, allowed := substrateScopeFilter(namespaces)
	matching := []SubstrateActor{}
	needle := strings.ToLower(strings.TrimSpace(input.Filter))
	if err := s.walkActors(ctx, func(actor *ateapipb.Actor) {
		if actor == nil || !allowedAtespace(actor.GetActorTemplate().GetAtespace(), allowAll, allowed) {
			return
		}
		entry := actorFromProto(actor)
		if !matchesFilter(needle, actorSearchText(entry)) {
			return
		}
		matching = append(matching, entry)
	}); err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		return result, nil
	}

	slices.SortStableFunc(matching, substrateOrder(actorSortKey(sortField), input.SortOrder))
	page, next := sliceSubstratePage(matching, offset, pageSize)
	result.Actors = page
	result.NextPageToken = next
	result.TotalSize = int64(len(matching))
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
	offset, err := decodeSubstrateOffset(input.PageToken)
	if err != nil {
		return SubstrateWorkerPage{}, err
	}

	sortField := apiv1alpha1.SubstrateWorkerSortField(input.SortField)
	if _, known := apiv1alpha1.SubstrateWorkerSortField_name[input.SortField]; !known {
		sortField = apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_UNSPECIFIED
	}
	result := SubstrateWorkerPage{
		Enabled:          s.ateClient != nil,
		Workers:          []SubstrateWorker{},
		ComputedAt:       time.Now().UTC(),
		AppliedSortField: int32(sortField),
		AppliedSortOrder: int32(substrateSortOrder(input.SortOrder)),
	}
	if s.ateClient == nil {
		return result, nil
	}

	allowAll, allowed := substrateScopeFilter(namespaces)
	matching := []SubstrateWorker{}
	needle := strings.ToLower(strings.TrimSpace(input.Filter))
	if err := s.walkWorkers(ctx, func(worker *ateapipb.Worker) {
		if worker == nil || !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			return
		}
		entry := workerFromProto(worker)
		if !matchesFilter(needle, workerSearchText(entry)) {
			return
		}
		matching = append(matching, entry)
	}); err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return result, nil
	}

	slices.SortStableFunc(matching, substrateOrder(workerSortKey(sortField), input.SortOrder))
	page, next := sliceSubstratePage(matching, offset, pageSize)
	result.Workers = page
	result.NextPageToken = next
	result.TotalSize = int64(len(matching))
	return result, nil
}

// walkActors calls visit for every actor ate-api holds, one page at a time.
func (s *Service) walkActors(ctx context.Context, visit func(*ateapipb.Actor)) error {
	// Every atespace, narrowed by the caller's own filter: an actor whose template has
	// no atespace is in scope everywhere, and asking ate-api for one atespace would
	// drop it.
	read := func(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
		return s.ateClient.ListActorsPage(ctx, "", pageSize, pageToken)
	}
	return walkSubstrate(ctx, read, visit)
}

// walkWorkers calls visit for every worker ate-api holds, one page at a time.
func (s *Service) walkWorkers(ctx context.Context, visit func(*ateapipb.Worker)) error {
	return walkSubstrate(ctx, s.ateClient.ListWorkersPage, visit)
}

/*
walkSubstrate calls visit for every row ate-api holds, one page at a time.

One page in memory at a time, whatever the cluster's size: the callers reduce these
rows to counts, so what they cost is the time to read them and not the space to hold
them. Bounded by maxATEPagesPerWalk — see there for what that is defending against.
*/
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

func substratePageSize(requested int) (int32, error) {
	switch {
	case requested < 0:
		return 0, serviceerrors.NewInvalidArgument(fmt.Sprintf("invalid page size %d: must not be negative", requested), nil)
	case requested == 0:
		return defaultSubstratePageSize, nil
	case requested > int(maxSubstratePageSize):
		return 0, serviceerrors.NewInvalidArgument(
			fmt.Sprintf("invalid page size %d: the maximum is %d", requested, maxSubstratePageSize),
			nil,
		)
	default:
		return int32(requested), nil
	}
}

/*
The order, the filter and the slice — the three things the controller now does because
ate-api does none of them.

Each of these costs a walk of the whole inventory, which is the price of an order that
means the cluster rather than the hundred rows in front of the reader. The walk holds
only the rows that match, so a narrow filter on a wide cluster costs time rather than
memory.
*/

// matchesFilter reports whether a row's own text contains the needle. An empty needle
// matches everything, so an unfiltered read pays nothing for the check.
func matchesFilter(needle, text string) bool {
	return needle == "" || strings.Contains(strings.ToLower(text), needle)
}

// actorSearchText is everything an actor row shows, which is what a reader searching it
// expects to match — including the parts a column composes, like a pod and its IP.
func actorSearchText(actor SubstrateActor) string {
	return strings.Join([]string{
		actor.ActorID,
		actor.Status,
		actor.ActorTemplateNamespace,
		actor.ActorTemplateName,
		actor.AteomPodNamespace,
		actor.AteomPodName,
		actor.AteomPodIP,
	}, " ")
}

func workerSearchText(worker SubstrateWorker) string {
	return strings.Join([]string{
		worker.WorkerNamespace,
		worker.WorkerPool,
		worker.WorkerPod,
		worker.IP,
	}, " ")
}

/*
actorSortKey turns a column into the string a row is ordered by.

Every key ends in the actor id, which is unique. An order whose last key repeats gives a
page boundary that names more than one row, and paging across it drops or repeats
whatever shares the key — the defect worth designing out rather than testing for.
*/
func actorSortKey(field apiv1alpha1.SubstrateActorSortField) func(SubstrateActor) string {
	switch field {
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_ACTOR_ID:
		return func(a SubstrateActor) string { return a.ActorID }
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_TEMPLATE:
		return func(a SubstrateActor) string {
			return a.ActorTemplateNamespace + "/" + a.ActorTemplateName + "\x00" + a.ActorID
		}
	case apiv1alpha1.SubstrateActorSortField_SUBSTRATE_ACTOR_SORT_FIELD_WORKER_POD:
		return func(a SubstrateActor) string {
			return a.AteomPodNamespace + "/" + a.AteomPodName + "\x00" + a.ActorID
		}
	default:
		// Status and the default are one ordering, because the default *is* status then
		// id. So the Status header changes nothing ascending and reverses the grouping
		// descending, which is correct and not obvious.
		return func(a SubstrateActor) string { return a.Status + "\x00" + a.ActorID }
	}
}

func workerSortKey(field apiv1alpha1.SubstrateWorkerSortField) func(SubstrateWorker) string {
	pod := func(w SubstrateWorker) string { return w.WorkerNamespace + "/" + w.WorkerPod }
	switch field {
	case apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_POD:
		return pod
	case apiv1alpha1.SubstrateWorkerSortField_SUBSTRATE_WORKER_SORT_FIELD_IP:
		return func(w SubstrateWorker) string { return w.IP + "\x00" + pod(w) }
	default:
		// Pool and the default are one ordering, as status and the default are above.
		return func(w SubstrateWorker) string { return w.WorkerPool + "\x00" + pod(w) }
	}
}

// substrateOrder compares two rows by their sort key, reversed for a descending read.
func substrateOrder[Row any](key func(Row) string, order int32) func(Row, Row) int {
	descending := substrateSortOrder(order) == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC
	return func(left, right Row) int {
		compared := strings.Compare(key(left), key(right))
		if descending {
			return -compared
		}
		return compared
	}
}

// substrateSortOrder reads an unset or unknown order as ascending, which is what a
// caller that did not ask means.
func substrateSortOrder(order int32) apiv1alpha1.SubstrateSortOrder {
	if apiv1alpha1.SubstrateSortOrder(order) == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC {
		return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC
	}
	return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_ASC
}

/*
sliceSubstratePage cuts the page a caller asked for out of the ordered result.

An offset rather than a cursor into the rows, because the order is the controller's and
is rebuilt on every request: a key-based token would name a row's position in an ordering
that the next request may not produce. An offset past the end is an empty last page
rather than an error — a reader whose cluster shrank under them should see the end of the
list, not a failure.
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

// The page token is an offset, encoded so it reads as opaque and a caller is not tempted
// to do arithmetic on it — the same shape the other paged reads on this API use.
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
