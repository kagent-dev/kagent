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

ate-api pages and does nothing else — no order, no filter, no total — so every read
here walks all of its pages and does those three itself. The walks cost time, but
they answer with a page or a tally rather than the inventory, which is the whole
difference from GetSubstrateStatus and its message-size ceiling.
*/

// How many rows a list call asks ate-api for when the caller names no page size.
const defaultSubstratePageSize int32 = 50

// The largest page a caller may ask for. Refused rather than clamped; system.proto
// declares the same cap and rejects an oversized request before this runs.
const maxSubstratePageSize int32 = 100

// How many ate-api pages a walk will read before giving up. Each page carries its own
// timeout, so nothing else bounds the loop against a cyclic next_page_token. At
// ate-api's 1,000 rows a page this allows ten million.
const maxATEPagesPerWalk = 10_000

// SubstrateListInput is what both paged substrate reads take.
type SubstrateListInput struct {
	// Empty means every namespace the controller observes.
	Namespace string
	// Zero means defaultSubstratePageSize.
	PageSize int
	// Empty for the first page; otherwise the previous answer's NextPageToken.
	PageToken string
	// Matched case-insensitively as a substring of what the row shows. Empty matches
	// everything.
	Filter string
	// Zero values are the read's default order.
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
	Status string
	Count  int64
}

// GetSubstrateSummary counts the inventory without sending it.
//
// ate-api reports no totals, so every count costs a walk of its pages. The walk holds
// one page at a time and keeps only tallies.
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

	// Read here rather than inside the template listing below, so a PostgreSQL outage
	// is an internal error instead of being reported as an ate-api one.
	harnesses, err := s.actorTemplateHarnesses(ctx)
	if err != nil {
		return SubstrateSummary{}, serviceerrors.NewInternal("Failed to list ActorTemplate harnesses", err)
	}

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
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
		 * Counted here because ate-api's Worker carries no actor reference: the binding
		 * is on the actor. Scoped by the pod's namespace, not the actor's atespace, so
		 * that it matches WorkerCount below — the two are shown as one fraction, and an
		 * actor in atespace `team` on a pod in namespace `kagent` would render "1/0".
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

	// Clamped because the two counts come from different walks: a failed worker walk
	// beside a successful actor one would render the tile as "11/0".
	result.BusyWorkerCount = min(int64(len(busyWorkers)), result.WorkerCount)
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
// Shared by all four, so a new read cannot arrive without the check.
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

// The order, the filter and the slice, which ate-api offers none of.

// matchesFilter reports whether a row's own text contains the needle.
func matchesFilter(needle, text string) bool {
	return needle == "" || strings.Contains(strings.ToLower(text), needle)
}

// actorSearchText is everything an actor row shows, including what a column composes
// out of several fields, so a search matches what the reader can see.
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

// actorSortKey turns a column into the string a row is ordered by. Every key ends in
// the unique actor id: an order whose last key repeats gives a page boundary naming
// more than one row, and paging across it drops or repeats them.
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
		// Status and the default are one ordering, so the Status header changes nothing
		// ascending and reverses descending. Correct, and not obvious.
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

// substrateSortOrder reads an unset or unknown order as ascending.
func substrateSortOrder(order int32) apiv1alpha1.SubstrateSortOrder {
	if apiv1alpha1.SubstrateSortOrder(order) == apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC {
		return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_DESC
	}
	return apiv1alpha1.SubstrateSortOrder_SUBSTRATE_SORT_ORDER_ASC
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
