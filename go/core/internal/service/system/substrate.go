package system

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/inventoryauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// How many rows a list call asks ate-api for when the caller names no page size.
const defaultSubstratePageSize int32 = 50

const (
	workerPoolResource             = "WorkerPool"
	substrateActorResource         = "SubstrateActor"
	substrateActorTemplateResource = "SubstrateActorTemplate"
	substrateWorkerResource        = "SubstrateWorker"
	systemSummaryResource          = "SystemSummary"
)

// How many ate-api pages a walk will read before giving up. Each page carries its own
// timeout, so nothing else bounds the loop against a cyclic next_page_token. At
// ate-api's 1,000 rows a page this allows ten million.
const maxATEPagesPerWalk = 10_000

// List requests have a smaller, request-wide budget than operator summaries.
// Both lookahead and traversal across watched atespaces consume this budget.
const (
	maxInventoryListPages        = 100
	maxInventoryListRows   int32 = 10_000
	inventoryListBatchRows int32 = 1_000
	inventoryListTimeout         = 10 * time.Second
)

type inventoryScan struct {
	pagesRead int
	rowsRead  int32
}

func (i *inventoryScan) nextPageSize(ctx context.Context, initialSize int32) (int32, error) {
	if err := ctx.Err(); err != nil {
		return 0, status.FromContextError(err).Err()
	}
	if i.pagesRead >= maxInventoryListPages || i.rowsRead >= maxInventoryListRows {
		return 0, status.Error(codes.ResourceExhausted, "inventory list scan budget exhausted")
	}
	size := inventoryListBatchRows
	if i.pagesRead == 0 {
		size = initialSize
	}
	i.pagesRead++
	return min(size, maxInventoryListRows-i.rowsRead), nil
}

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
	WorkerPools       []atev1.WorkerPool
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
		summary.ATEAPIError = fmt.Sprintf("ate-api could not read inventory: %s", status.Code(err))
	}
}

// SubstrateActorStatusCount is one status and how many actors hold it.
type SubstrateActorStatusCount struct {
	State ateapipb.ActorState
	Count int64
}

// GetSubstrateSummary requires the SystemSummary capability, then applies each
// inventory family's independent scope before returning rows or counting status.
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
	actorMatcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateActorResource, authorization.AttributeAtespace)
	if err != nil {
		return SubstrateSummary{}, err
	}
	templateMatcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateActorTemplateResource, authorization.AttributeAtespace)
	if err != nil {
		return SubstrateSummary{}, err
	}
	workerMatcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateWorkerResource, authorization.AttributeNamespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
		WorkerPools:       []atev1.WorkerPool{},
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
	slices.SortStableFunc(result.WorkerPools, func(left, right atev1.WorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
	if !templateMatcher.Empty() {
		if templates, err := s.substrateActorTemplates(ctx, actorScope); err != nil {
			result.recordATEError(ctx, err)
		} else {
			for _, template := range templates {
				metadata := template.GetMetadata()
				if templateMatcher.Matches(metadata.GetAtespace(), metadata.GetName()) && (atespace == "" || metadata.GetAtespace() == atespace) {
					result.ActorTemplates = append(result.ActorTemplates, template)
				}
			}
		}
	}

	statusCounts := map[ateapipb.ActorState]int64{}
	if !actorMatcher.Empty() {
		if err := s.walkActors(ctx, actorScope, func(actor *ateapipb.Actor) {
			metadata := actor.GetMetadata()
			if !actorMatcher.Matches(metadata.GetAtespace(), metadata.GetName()) || (atespace != "" && metadata.GetAtespace() != atespace) {
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
	}
	if !workerMatcher.Empty() {
		if err := walkSubstrate(ctx, s.ateClient.ListWorkersPage, func(worker *ateapipb.Worker) {
			if !workerMatcher.Matches(worker.GetWorkerNamespace(), worker.GetMetadata().GetName()) || !workerScope.contains(worker.GetWorkerNamespace()) {
				return
			}
			result.WorkerCount++
			if worker.GetStatus().GetAllocated().GetActors() > 0 {
				result.BusyWorkerCount++
			}
		}); err != nil {
			result.recordATEError(ctx, err)
		}
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

// ListSubstrateActors pages authorized actors in atespace/name order.
func (s *Service) ListSubstrateActors(ctx context.Context, input *apiv1.ListSubstrateActorsRequest) (SubstrateActorPage, error) {
	ctx, cancel := context.WithTimeout(ctx, inventoryListTimeout)
	defer cancel()
	matcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateActorResource, authorization.AttributeAtespace)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	if matcher.Empty() {
		return SubstrateActorPage{}, serviceerrors.NewPermissionDenied("Actor inventory access denied", nil)
	}
	scope, err := s.resolveSubstrateScope(input.GetAtespace())
	if err != nil {
		return SubstrateActorPage{}, err
	}
	after, err := parseInventoryCursor(input.GetPage().GetPageToken(), "actor", input.GetAtespace(), scope)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	// Worker inventory is an independent permission, including when embedded in Actors.
	workerMatcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateWorkerResource, authorization.AttributeNamespace)
	if err != nil {
		return SubstrateActorPage{}, err
	}
	workerScope, err := s.resolveSubstrateScope("")
	if err != nil {
		return SubstrateActorPage{}, err
	}
	limit := substratePageSize(input.GetPage().GetLimit())
	result := SubstrateActorPage{ComputedAt: time.Now().UTC()}
	actors, err := s.collectAuthorizedActors(ctx, scope, matcher, after, limit+1)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		result.ATEAPIError = fmt.Sprintf("ate-api could not list actors: %s", status.Code(err))
		return result, nil
	}
	if int32(len(actors)) > limit {
		actors = actors[:limit]
		last := actors[limit-1].GetMetadata()
		result.NextPageToken = inventoryCursor("actor", input.GetAtespace(), last.GetAtespace(), last.GetName())
	}
	for index, actor := range actors {
		actors[index] = actorInventory(actor, workerMatcher, workerScope)
	}
	result.Actors = actors
	return result, nil
}

// actorInventory hides placement unless its exact worker identity is visible.
// Snapshot locality has no worker identity to authorize and is always omitted.
// Clone only redacted rows so shared backend objects are never mutated.
func actorInventory(actor *ateapipb.Actor, workers inventoryauth.Matcher, scope substrateScope) *ateapipb.Actor {
	status := actor.GetStatus()
	assignment := status.GetWorkerAssignment()
	visibleWorker := scope.contains(assignment.GetWorkerNamespace()) && workers.Matches(assignment.GetWorkerNamespace(), assignment.GetWorker().GetName())
	hasLocality := slices.ContainsFunc(status.GetSnapshots(), func(snapshot *ateapipb.Snapshot) bool {
		return snapshot.GetLocality() != ""
	})
	if !hasLocality && (visibleWorker || (assignment == nil && status.GetAssignedNode() == "")) {
		return actor
	}
	result := proto.CloneOf(actor)
	if !visibleWorker {
		result.Status.WorkerAssignment = nil
		result.Status.AssignedNode = ""
	}
	for _, snapshot := range result.Status.Snapshots {
		if snapshot != nil {
			snapshot.Locality = ""
		}
	}
	return result
}

// collectAuthorizedActors reads in atespace/name order, starting after the cursor.
// An explicit atespace uses one scoped query. An empty request uses either one
// global query or the controller's sorted watched atespaces. Only that last case
// needs traversal across separate queries.
func (s *Service) collectAuthorizedActors(ctx context.Context, scope substrateScope, matcher inventoryauth.Matcher, after inventoryPosition, count int32) ([]*ateapipb.Actor, error) {
	atespaces := scope.listNamespaces()
	if after.name != "" && !scope.all {
		// Cursor parsing already checked membership in the resolved scope.
		atespaces = atespaces[slices.Index(atespaces, after.location):]
	}
	var actors []*ateapipb.Actor
	scan := &inventoryScan{}
	for _, atespace := range atespaces {
		token := ""
		if after.name != "" && (scope.all || atespace == after.location) {
			token = substrate.ActorPageToken(atespace, after.location, after.name)
		}
		read := func(ctx context.Context, size int32, token string) ([]*ateapipb.Actor, string, error) {
			return s.ateClient.ListActorsPage(ctx, atespace, size, token)
		}
		accept := func(actor *ateapipb.Actor) bool {
			metadata := actor.GetMetadata()
			return (atespace == "" || metadata.GetAtespace() == atespace) && scope.contains(metadata.GetAtespace()) && matcher.Matches(metadata.GetAtespace(), metadata.GetName())
		}
		rows, err := collectInventory(ctx, scan, read, accept, token, count-int32(len(actors)))
		if err != nil {
			return nil, err
		}
		actors = append(actors, rows...)
		if int32(len(actors)) == count {
			break
		}
	}
	return actors, nil
}

const maxPageTokenBytes = 1024

// Public cursors contain only a returned identity, never an upstream scan position.
// The request scope and resource kind prevent accidental reuse across listings.
func inventoryCursor(kind, requested, location, name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{"1", kind, requested, location, name}, "/")))
}

// inventoryPosition is a validated continuation boundary. Its zero value starts
// at the beginning; a parsed boundary has both location and name populated.
type inventoryPosition struct {
	location string
	name     string
}

func parseInventoryCursor(token, kind, requested string, scope substrateScope) (inventoryPosition, error) {
	if token == "" {
		return inventoryPosition{}, nil
	}
	if len(token) > maxPageTokenBytes {
		return inventoryPosition{}, invalidPageToken()
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	parts := strings.Split(string(raw), "/")
	if err != nil || len(parts) != 5 || parts[0] != "1" || parts[1] != kind || parts[2] != requested || strings.TrimSpace(parts[3]) == "" || strings.TrimSpace(parts[4]) == "" || !scope.contains(parts[3]) {
		return inventoryPosition{}, invalidPageToken()
	}
	return inventoryPosition{location: parts[3], name: parts[4]}, nil
}

func invalidPageToken() error { return serviceerrors.NewInvalidArgument("Invalid page token", nil) }

// collectInventory keeps upstream cursors internal and stops at the authorized
// lookahead row. A continuation starts at the last returned identity, so only
// lookahead is reread, not the prefix traversed by previous public pages.
func collectInventory[Row any](ctx context.Context, scan *inventoryScan, read func(context.Context, int32, string) ([]Row, string, error), accept func(Row) bool, token string, count int32) ([]Row, error) {
	var result []Row
	for {
		size, err := scan.nextPageSize(ctx, count)
		if err != nil {
			return nil, err
		}
		rows, next, err := read(ctx, size, token)
		if err := ctx.Err(); err != nil {
			return nil, status.FromContextError(err).Err()
		}
		if err != nil {
			return nil, err
		}
		scan.rowsRead += int32(len(rows))
		for _, row := range rows {
			if accept(row) {
				result = append(result, row)
				if int32(len(result)) == count {
					return result, nil
				}
			}
		}
		if next == "" {
			return result, nil
		}
		token, err = substrate.AdvancePageToken(token, next)
		if err != nil {
			return nil, err
		}
	}
}

// ListSubstrateWorkers pages authorized workers in upstream worker-name order.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input *apiv1.ListSubstrateWorkersRequest) (SubstrateWorkerPage, error) {
	ctx, cancel := context.WithTimeout(ctx, inventoryListTimeout)
	defer cancel()
	matcher, err := inventoryauth.CollectionMatcher(ctx, s.authorizer, substrateWorkerResource, authorization.AttributeNamespace)
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	if matcher.Empty() {
		return SubstrateWorkerPage{}, serviceerrors.NewPermissionDenied("Worker inventory access denied", nil)
	}
	scope, err := s.resolveSubstrateScope(input.GetNamespace())
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	after, err := parseInventoryCursor(input.GetPage().GetPageToken(), "worker", input.GetNamespace(), scope)
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	token := ""
	if after.name != "" {
		token = substrate.WorkerPageToken(after.name)
	}
	limit := substratePageSize(input.GetPage().GetLimit())
	accept := func(worker *ateapipb.Worker) bool {
		return scope.contains(worker.GetWorkerNamespace()) && matcher.Matches(worker.GetWorkerNamespace(), worker.GetMetadata().GetName())
	}
	result := SubstrateWorkerPage{ComputedAt: time.Now().UTC()}
	rows, err := collectInventory(ctx, &inventoryScan{}, s.ateClient.ListWorkersPage, accept, token, limit+1)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		result.ATEAPIError = fmt.Sprintf("ate-api could not list workers: %s", status.Code(err))
		return result, nil
	}
	if int32(len(rows)) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		result.NextPageToken = inventoryCursor("worker", input.GetNamespace(), last.GetWorkerNamespace(), last.GetMetadata().GetName())
	}
	result.Workers = rows
	return result, nil
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
