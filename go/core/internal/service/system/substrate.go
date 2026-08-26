package system

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// The substrate inventory, in the shape a caller can actually read it.
//
// GetSubstrateStatus answers with every actor and every worker in one message,
// which stopped working: a cluster reporting 103,134 actors produces a response
// gRPC refuses to send at all. What follows splits that one read into the three
// a caller needs — counts, a page of actors, a page of workers — so no single
// response grows with the size of the cluster.
//
// # What this does and does not fix
//
// It bounds what crosses the wire, not what ate-api hands the controller: the
// actor and worker lists still arrive here whole, because ListActors takes no
// page and no filter. So the cost of a read is unchanged on this side and the
// response is bounded on the other, which is the half that was failing. Pushing
// the narrowing all the way down needs ate-api to offer it.

const (
	// What a caller gets when it does not ask, matching AgentInstanceService.
	defaultSubstratePageSize = 50
	// The most one response will carry, matching AgentInstanceService.
	maxSubstratePageSize = 100
)

/*
 * The three public reads, each memoised briefly.
 *
 * The cache key is the whole question — every field of the request — so a different
 * filter, sort, page or scope is a different answer rather than a stale one. See
 * substratecache.go for why the answers carry the instant they were computed.
 */

// GetSubstrateSummary counts the inventory server-side and returns the two small lists whole.
func (s *Service) GetSubstrateSummary(ctx context.Context, requestedNamespace string) (SubstrateSummary, error) {
	// Authorized before the cache is consulted, so a cached answer can never be
	// served to a caller who would have been refused the read.
	if _, err := s.substrateScope(ctx, requestedNamespace); err != nil {
		return SubstrateSummary{}, err
	}
	key := fmt.Sprintf("summary|%s", requestedNamespace)
	value, computedAt, err := s.cache.get(ctx, key, func() (any, error) {
		return s.computeSubstrateSummary(ctx, requestedNamespace)
	})
	if err != nil {
		return SubstrateSummary{}, err
	}
	result := value.(SubstrateSummary)
	result.ComputedAt = computedAt
	return result, nil
}

// ListSubstrateActors returns one page of the actors matching filter, in the order asked for.
func (s *Service) ListSubstrateActors(ctx context.Context, request ListActorsRequest) (ListActorsResult, error) {
	if _, err := s.substrateScope(ctx, request.Namespace); err != nil {
		return ListActorsResult{}, err
	}
	key := fmt.Sprintf("actors|%s|%s|%d|%s|%s|%s",
		request.Namespace, request.Filter, request.Limit, request.PageToken,
		request.SortField, request.SortOrder)
	value, computedAt, err := s.cache.get(ctx, key, func() (any, error) {
		return s.computeSubstrateActors(ctx, request)
	})
	if err != nil {
		return ListActorsResult{}, err
	}
	result := value.(ListActorsResult)
	result.ComputedAt = computedAt
	return result, nil
}

// ListSubstrateWorkers returns one page of the workers matching filter, in the order asked for.
func (s *Service) ListSubstrateWorkers(ctx context.Context, request ListWorkersRequest) (ListWorkersResult, error) {
	if _, err := s.substrateScope(ctx, request.Namespace); err != nil {
		return ListWorkersResult{}, err
	}
	key := fmt.Sprintf("workers|%s|%s|%d|%s|%s|%s",
		request.Namespace, request.Filter, request.Limit, request.PageToken,
		request.SortField, request.SortOrder)
	value, computedAt, err := s.cache.get(ctx, key, func() (any, error) {
		return s.computeSubstrateWorkers(ctx, request)
	})
	if err != nil {
		return ListWorkersResult{}, err
	}
	result := value.(ListWorkersResult)
	result.ComputedAt = computedAt
	return result, nil
}

// SubstrateStatusCount is how many rows carry one status.
type SubstrateStatusCount struct {
	Status string
	Count  int32
}

// SubstrateSummary is the whole inventory as counts, plus the two lists that are
// small enough to travel inline.
//
// The counts are over everything in scope and take no filter: they are what a
// caller reports as a total, and a total narrowed by a search is not one. The
// paged reads carry their own filtered total for the other half of "20 of 4,312".
type SubstrateSummary struct {
	// ComputedAt is when this answer was produced, which is not necessarily now:
	// the reads are memoised briefly (see substratecache.go). Reported so a caller
	// can say how old the numbers are instead of implying they are live.
	ComputedAt        time.Time
	Enabled           bool
	ATEAPIError       string
	WorkerPools       []SubstrateWorkerPool
	ActorTemplates    []SubstrateActorTemplate
	ActorCount        int32
	WorkerCount       int32
	RunningActorCount int32
	BusyWorkerCount   int32
	ActorStatusCounts []SubstrateStatusCount
}

// SortOrder is the direction a paged read is sorted in.
type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

// ActorSortField names the column ListSubstrateActors orders by.
//
// Every order below ends in the actor id, which is unique. That is not tidiness:
// a page token is the sort key of the last row already sent, so a key that could
// tie would skip or repeat rows at a page boundary.
type ActorSortField string

const (
	// ActorSortDefault groups by status and orders by id within each group.
	ActorSortDefault  ActorSortField = "status_then_id"
	ActorSortStatus   ActorSortField = "status"
	ActorSortID       ActorSortField = "id"
	ActorSortTemplate ActorSortField = "template"
	ActorSortWorker   ActorSortField = "worker"
)

// WorkerSortField names the column ListSubstrateWorkers orders by.
type WorkerSortField string

const (
	// WorkerSortDefault groups by pool and orders by pod within each group.
	WorkerSortDefault WorkerSortField = "pool_then_pod"
	WorkerSortPool    WorkerSortField = "pool"
	WorkerSortPod     WorkerSortField = "pod"
	WorkerSortActor   WorkerSortField = "actor"
)

// ListActorsRequest is what one page of actors is asked for by.
type ListActorsRequest struct {
	Namespace string
	Filter    string
	Limit     int32
	PageToken string
	SortField ActorSortField
	SortOrder SortOrder
}

// ListWorkersRequest is the mirror of ListActorsRequest.
type ListWorkersRequest struct {
	Namespace string
	Filter    string
	Limit     int32
	PageToken string
	SortField WorkerSortField
	SortOrder SortOrder
}

// ListActorsResult is one page of actors and how many matched in total.
type ListActorsResult struct {
	// ComputedAt is when this page was produced. See SubstrateSummary.ComputedAt.
	ComputedAt    time.Time
	Actors        []SubstrateActor
	NextPageToken string
	TotalSize     int32
	// The order actually applied. Reported rather than assumed, so a caller can
	// say how the rows are sorted instead of trusting that its request was
	// honoured — an unspecified field resolves to a concrete one here.
	SortField ActorSortField
	SortOrder SortOrder
}

// ListWorkersResult is one page of workers and how many matched in total.
type ListWorkersResult struct {
	ComputedAt    time.Time
	Workers       []SubstrateWorker
	NextPageToken string
	TotalSize     int32
	SortField     WorkerSortField
	SortOrder     SortOrder
}

// GetSubstrateSummary counts the inventory server-side and returns the two small
// lists whole.
//
// The ate-api halves can be absent (no endpoint configured) or partial (an error
// on an otherwise successful read). Both are reported rather than raised: the
// Kubernetes-derived halves are complete either way, and failing the whole call
// would hide them.
func (s *Service) computeSubstrateSummary(ctx context.Context, requestedNamespace string) (SubstrateSummary, error) {
	namespaces, err := s.substrateScope(ctx, requestedNamespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
		Enabled:           s.ateClient != nil,
		WorkerPools:       []SubstrateWorkerPool{},
		ActorTemplates:    []SubstrateActorTemplate{},
		ActorStatusCounts: []SubstrateStatusCount{},
	}
	if s.ateClient == nil {
		return result, nil
	}
	if s.kubeClient == nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", fmt.Errorf("kubernetes client is not configured"))
	}

	for _, namespace := range namespaces {
		workerPools, actorTemplates, err := s.listSubstrateCRs(ctx, namespace)
		if err != nil {
			return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		result.WorkerPools = append(result.WorkerPools, workerPools...)
		result.ActorTemplates = append(result.ActorTemplates, actorTemplates...)
	}
	sortWorkerPools(result.WorkerPools)
	sortActorTemplates(result.ActorTemplates)

	// Counted straight off the protos ate-api returned, without converting them.
	//
	// A count needs no struct, and building 410,110 of them to take their length is
	// how the paged read next door OOM-killed the controller. The whole distribution
	// is kept rather than only the running tally: knowing 12 of 4,312 are running
	// says nothing about the other 4,300.
	statusCounts := map[string]int32{}
	inScope := namespaceFilter(namespaces)

	// Walked a page at a time and never accumulated: counting 410,110 actors costs
	// a few integers this way, where holding them cost the controller its memory
	// limit.
	if err := s.ateClient.EachActorPage(ctx, "", func(page []*ateapipb.Actor) error {
		for _, actor := range page {
			if actor == nil || !inScope(actor.GetActorTemplateNamespace()) {
				continue
			}
			result.ActorCount++
			status := substrate.ActorStatusLabel(actor.GetStatus().GetState())
			statusCounts[status]++
			if strings.EqualFold(status, "Running") {
				result.RunningActorCount++
			}
		}
		return nil
	}); err != nil {
		// Partial rather than failed: the Kubernetes halves above are complete, and
		// the caller is told the counts may be short instead of losing everything.
		result.ATEAPIError = err.Error()
		ctrllog.FromContext(ctx).Error(err, "list ate-api actors")
	}

	if workersFromAPI, err := s.ateClient.ListWorkers(ctx); err != nil {
		if result.ATEAPIError == "" {
			result.ATEAPIError = err.Error()
		}
		ctrllog.FromContext(ctx).Error(err, "list ate-api workers")
	} else {
		for _, worker := range workersFromAPI {
			if worker == nil || !inScope(worker.GetWorkerNamespace()) {
				continue
			}
			result.WorkerCount++
			// A worker holding an actor is busy; one holding none is available.
			if worker.GetStatus().GetAssignment().GetActor().GetName() != "" {
				result.BusyWorkerCount++
			}
		}
	}

	for status, count := range statusCounts {
		result.ActorStatusCounts = append(result.ActorStatusCounts, SubstrateStatusCount{Status: status, Count: count})
	}
	slices.SortStableFunc(result.ActorStatusCounts, func(left, right SubstrateStatusCount) int {
		return strings.Compare(left.Status, right.Status)
	})

	return result, nil
}

// ListSubstrateActors returns one page of the actors matching filter, in the
// order asked for.
//
// Sorting is server-side for the same reason the filter is: the rows are paged,
// so ordering a page that has already been fetched reorders a hundred rows out
// of hundreds of thousands. It looks like sorting and it is not — the first row
// of the sorted cluster is almost certainly not among the hundred on screen.
func (s *Service) computeSubstrateActors(ctx context.Context, request ListActorsRequest) (ListActorsResult, error) {
	namespaces, err := s.substrateScope(ctx, request.Namespace)
	if err != nil {
		return ListActorsResult{}, err
	}
	pageSize, err := substratePageSize(request.Limit)
	if err != nil {
		return ListActorsResult{}, err
	}
	after, err := decodeSubstratePageToken(request.PageToken)
	if err != nil {
		return ListActorsResult{}, err
	}
	field, order := actorSort(request.SortField, request.SortOrder)

	result := ListActorsResult{Actors: []SubstrateActor{}, SortField: field, SortOrder: order}
	if s.ateClient == nil {
		return result, nil
	}

	/*
	 * Selected while streaming, so the cost of this call does not grow with the
	 * cluster.
	 *
	 * The obvious implementation — collect every actor, sort, take a slice — is what
	 * OOM-killed the controller at 410,110 actors: ate-api pages its own list, and
	 * accumulating those pages is hundreds of megabytes of protos before any of this
	 * code runs. Instead the actors are walked a page at a time and a bounded buffer
	 * keeps only the `pageSize` rows that belong on the page being asked for, which
	 * is `pageSize` rows of memory whatever the cluster is running.
	 *
	 * The filtered total is counted in the same pass, because it is the other half of
	 * "20 of 4,312" and counting it afterwards would mean a second walk.
	 */
	inScope := namespaceFilter(namespaces)
	key := actorKey(field)
	selector := newPageSelector(pageSize, after, order, key)

	if err := s.ateClient.EachActorPage(ctx, "", func(page []*ateapipb.Actor) error {
		for _, actor := range page {
			if actor == nil || !inScope(actor.GetActorTemplateNamespace()) {
				continue
			}
			converted := actorFromProto(actor)
			if !matchesFilter(request.Filter, converted.ActorID, converted.Status, converted.ActorTemplateNamespace, converted.ActorTemplateName, converted.AteomPodNamespace, converted.AteomPodName, converted.AteomPodIP) {
				continue
			}
			selector.offer(converted)
		}
		return nil
	}); err != nil {
		// Unlike the summary, there is no complete half to salvage here: this call
		// answers with actors or it answers with nothing.
		return ListActorsResult{}, serviceerrors.NewInternal("Failed to list actors from ate-api", err)
	}

	rows, nextToken, total := selector.page()
	result.Actors = rows
	result.NextPageToken = nextToken
	result.TotalSize = total
	return result, nil
}

// ListSubstrateWorkers returns one page of the workers matching filter, in the
// order asked for. The mirror of ListSubstrateActors.
func (s *Service) computeSubstrateWorkers(ctx context.Context, request ListWorkersRequest) (ListWorkersResult, error) {
	namespaces, err := s.substrateScope(ctx, request.Namespace)
	if err != nil {
		return ListWorkersResult{}, err
	}
	pageSize, err := substratePageSize(request.Limit)
	if err != nil {
		return ListWorkersResult{}, err
	}
	after, err := decodeSubstratePageToken(request.PageToken)
	if err != nil {
		return ListWorkersResult{}, err
	}
	field, order := workerSort(request.SortField, request.SortOrder)

	result := ListWorkersResult{Workers: []SubstrateWorker{}, SortField: field, SortOrder: order}
	if s.ateClient == nil {
		return result, nil
	}

	// The actors are not read at all — see ListSubstrateActors for why that matters.
	matching, err := s.matchingWorkers(ctx, namespaces, request.Filter)
	if err != nil {
		return ListWorkersResult{}, serviceerrors.NewInternal("Failed to list workers from ate-api", err)
	}

	// Not streamed, unlike the actors: ListWorkers answers in one response and a
	// worker count is bounded by the size of the pools, so the same selector is used
	// only to keep the paging and ordering rules identical between the two.
	selector := newPageSelector(pageSize, after, order, workerKey(field))
	for _, worker := range matching {
		selector.offer(worker)
	}

	rows, nextToken, total := selector.page()
	result.Workers = rows
	result.NextPageToken = nextToken
	result.TotalSize = total
	return result, nil
}

/*
 * The sort keys.
 *
 * Each is the chosen column followed by a unique tiebreaker, so that ordering is
 * total: a page token is the key of the last row sent, and a key two rows could
 * share would make the boundary between pages ambiguous — skipping one row or
 * repeating it.
 */
func actorSort(field ActorSortField, order SortOrder) (ActorSortField, SortOrder) {
	switch field {
	case ActorSortStatus, ActorSortID, ActorSortTemplate, ActorSortWorker:
	default:
		field = ActorSortDefault
	}
	if order != SortDescending {
		order = SortAscending
	}
	return field, order
}

func actorKey(field ActorSortField) func(SubstrateActor) string {
	switch field {
	case ActorSortID:
		return func(a SubstrateActor) string { return a.ActorID }
	case ActorSortStatus:
		return func(a SubstrateActor) string { return a.Status + "\x00" + a.ActorID }
	case ActorSortTemplate:
		return func(a SubstrateActor) string {
			return a.ActorTemplateNamespace + "/" + a.ActorTemplateName + "\x00" + a.ActorID
		}
	case ActorSortWorker:
		return func(a SubstrateActor) string {
			return a.AteomPodNamespace + "/" + a.AteomPodName + "\x00" + a.ActorID
		}
	default:
		return func(a SubstrateActor) string { return a.Status + "\x00" + a.ActorID }
	}
}

func workerSort(field WorkerSortField, order SortOrder) (WorkerSortField, SortOrder) {
	switch field {
	case WorkerSortPool, WorkerSortPod, WorkerSortActor:
	default:
		field = WorkerSortDefault
	}
	if order != SortDescending {
		order = SortAscending
	}
	return field, order
}

func workerKey(field WorkerSortField) func(SubstrateWorker) string {
	pod := func(w SubstrateWorker) string { return w.WorkerNamespace + "/" + w.WorkerPod }
	switch field {
	case WorkerSortPod:
		return pod
	case WorkerSortActor:
		// Idle workers sort together, and after the busy ones ascending: an empty
		// string would put every available worker first, which buries the placements
		// this column exists to show.
		return func(w SubstrateWorker) string {
			actor := w.ActorID
			if actor == "" {
				actor = "\uffff"
			}
			return actor + "\x00" + pod(w)
		}
	default:
		return func(w SubstrateWorker) string { return w.WorkerPool + "\x00" + pod(w) }
	}
}

// matchingWorkers reads the workers and keeps only those in scope and matching
// the filter, converting as it goes.
//
// Not streamed, unlike the actors: ate-api answers ListWorkers in one response and
// a worker count is bounded by the size of the pools — eight on the cluster this
// was measured against — so there is nothing here to page around.
func (s *Service) matchingWorkers(ctx context.Context, namespaces []string, filter string) ([]SubstrateWorker, error) {
	workersFromAPI, err := s.ateClient.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}
	inScope := namespaceFilter(namespaces)

	var matching []SubstrateWorker
	for _, worker := range workersFromAPI {
		if worker == nil || !inScope(worker.GetWorkerNamespace()) {
			continue
		}
		converted := workerFromProto(worker)
		if !matchesFilter(filter, converted.WorkerNamespace, converted.WorkerPool, converted.WorkerPod, converted.ActorNamespace, converted.ActorTemplate, converted.ActorID, converted.IP) {
			continue
		}
		matching = append(matching, converted)
	}
	return matching, nil
}

// namespaceFilter reports whether a row's namespace is in scope.
//
// A row with no namespace is always in scope: ate-api leaves it empty on records
// it cannot attribute, and dropping those would quietly shorten the inventory.
// This is the same rule listATEState applies, extracted so the paged reads cannot
// drift from the unpaged one.
func namespaceFilter(namespaces []string) func(string) bool {
	if len(namespaces) == 1 && namespaces[0] == "" {
		return func(string) bool { return true }
	}
	allowed := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}
	return func(namespace string) bool {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			return true
		}
		_, ok := allowed[namespace]
		return ok
	}
}

// substrateScope authorizes the caller and resolves which namespaces to read.
//
// Shared by all three substrate reads so that one of them cannot quietly become
// more permissive than the others — the authorization and the namespace
// validation are the same check on every path.
func (s *Service) substrateScope(ctx context.Context, requestedNamespace string) ([]string, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Agent"}); err != nil {
		return nil, err
	}
	requestedNamespace = strings.TrimSpace(requestedNamespace)
	if requestedNamespace != "" {
		if problems := utilvalidation.IsDNS1123Label(requestedNamespace); len(problems) > 0 {
			return nil, serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid namespace %q: %s", requestedNamespace, strings.Join(problems, ", ")),
				nil,
			)
		}
	}
	return s.substrateNamespaces(requestedNamespace), nil
}

// substratePageSize applies the same bounds AgentInstanceService uses.
//
// Zero means "no preference" and takes the default; anything outside the range
// is refused rather than clamped, so a caller asking for 5,000 is told its
// request was not honoured instead of quietly receiving 100.
func substratePageSize(limit int32) (int, error) {
	if limit == 0 {
		return defaultSubstratePageSize, nil
	}
	if limit < 0 || limit > maxSubstratePageSize {
		return 0, serviceerrors.NewInvalidArgument(fmt.Sprintf("page limit must be between 1 and %d", maxSubstratePageSize), nil)
	}
	return int(limit), nil
}

// The page token is the sort key of the last row already sent.
//
// A key rather than an offset, so that rows appearing or disappearing between
// two reads shift the page's contents instead of causing a row to be skipped or
// repeated — which on an inventory that changes every second it is polled is the
// difference between a stable list and one that flickers.
func encodeSubstratePageToken(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

func decodeSubstratePageToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	value, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", serviceerrors.NewInvalidArgument("page token is invalid", err)
	}
	return string(value), nil
}

/*
 * The rows belonging on one page, chosen without holding the rest.
 *
 * Paging is by sort key — the key of the last row already sent — so the page being
 * asked for is "the `limit` rows that come after `after` in the chosen order". That
 * can be decided from a stream: keep the `limit` rows nearest the front and discard
 * anything that cannot make the page.
 *
 * Compaction rather than a heap. The buffer is allowed to grow to twice the limit
 * and is then sorted and truncated, which is the same amortised work as a heap for
 * these sizes and considerably easier to be sure is right — and being sure matters,
 * because the failure mode of a wrong selector is a page that silently skips rows.
 */
type pageSelector[T any] struct {
	limit int
	after string
	order SortOrder
	key   func(T) string
	rows  []T
	// Every row that matched, page or not. The caller reports it as the total, and
	// it is the reason this is counted here rather than by a second walk.
	total int32
}

func newPageSelector[T any](limit int, after string, order SortOrder, key func(T) string) *pageSelector[T] {
	return &pageSelector[T]{limit: limit, after: after, order: order, key: key}
}

// before reports whether left comes before right in the selected order.
func (s *pageSelector[T]) before(left, right string) bool {
	if s.order == SortDescending {
		return left > right
	}
	return left < right
}

func (s *pageSelector[T]) offer(row T) {
	// Every matching row counts towards the total, page or not — it is a total, not
	// a remainder.
	s.total++
	// Rows at or before the token have already been sent.
	if s.after != "" && !s.before(s.after, s.key(row)) {
		return
	}
	s.rows = append(s.rows, row)
	if len(s.rows) >= 2*s.limit {
		s.compact()
	}
}

func (s *pageSelector[T]) compact() {
	slices.SortStableFunc(s.rows, func(left, right T) int {
		leftKey, rightKey := s.key(left), s.key(right)
		if leftKey == rightKey {
			return 0
		}
		if s.before(leftKey, rightKey) {
			return -1
		}
		return 1
	})
	if len(s.rows) > s.limit {
		s.rows = s.rows[:s.limit]
	}
}

// page returns the page, the token to ask for the next one, and the filtered total.
//
// The token is empty on the last page rather than on a page that merely happens to
// be full, so a caller never fetches an empty page to discover it has finished.
func (s *pageSelector[T]) page() ([]T, string, int32) {
	s.compact()
	if len(s.rows) == 0 {
		return []T{}, "", s.total
	}
	// A full page means there may be more: the selector discarded everything past
	// the limit, so it cannot know whether anything was there. Asking again is the
	// only way to find out, and an empty answer is what ends the walk.
	token := ""
	if len(s.rows) == s.limit {
		token = encodeSubstratePageToken(s.key(s.rows[len(s.rows)-1]))
	}
	return s.rows, token, s.total
}

// matchesFilter reports whether any of the row's displayed fields contains the
// term, case-insensitively. An empty term matches every row.
//
// Matched against the fields the caller displays rather than against every field
// on the record: a search that hits on something not on screen reads as a list
// filtering itself at random.
func matchesFilter(filter string, fields ...string) bool {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if needle == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

func sortWorkerPools(pools []SubstrateWorkerPool) {
	slices.SortStableFunc(pools, func(left, right SubstrateWorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
}

func sortActorTemplates(templates []SubstrateActorTemplate) {
	slices.SortStableFunc(templates, func(left, right SubstrateActorTemplate) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
}
