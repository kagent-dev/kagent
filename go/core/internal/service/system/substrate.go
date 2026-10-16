package system

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
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

// GetSubstrateSummary counts the inventory without sending it.
//
// ate-api reports no totals, so every count costs a walk of its pages. The walk holds
// one page at a time and keeps only tallies.
func (s *Service) GetSubstrateSummary(ctx context.Context, requestedNamespace, atespace string) (SubstrateSummary, error) {
	namespaces, err := s.substrateScope(ctx, requestedNamespace, atespace)
	if err != nil {
		return SubstrateSummary{}, err
	}

	result := SubstrateSummary{
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

	// Three independent reads: none gates the others, so one failure leaves the rest
	// counted rather than zeroing the whole summary.
	if templates, err := s.substrateActorTemplates(ctx, atespace); err != nil {
		result.recordATEError(ctx, err)
	} else {
		result.ActorTemplates = templates
	}

	statusCounts := map[ateapipb.ActorState]int64{}
	if err := s.walkActors(ctx, atespace, func(actor *ateapipb.Actor) {
		result.ActorCount++
		statusCounts[actor.GetStatus().GetState()]++
		if actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_RUNNING {
			result.RunningActorCount++
		}
	}); err != nil {
		result.recordATEError(ctx, err)
	}

	if err := s.walkWorkers(ctx, namespaces, func(worker *ateapipb.Worker) {
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

// ListSubstrateActors returns one page of actors. A controller that watches every
// namespace passes ate-api's page and token through. A controller that watches some
// reads each of its atespaces with its own scoped list, so ate-api reads, and no page
// token names, an actor of a namespace it does not watch.
func (s *Service) ListSubstrateActors(ctx context.Context, input *apiv1.ListSubstrateActorsRequest) (SubstrateActorPage, error) {
	if _, err := s.substrateScope(ctx, "", input.GetAtespace()); err != nil {
		return SubstrateActorPage{}, err
	}
	result := SubstrateActorPage{ComputedAt: time.Now().UTC()}
	limit, token := substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken()
	var actors []*ateapipb.Actor
	var next string
	var err error
	if len(s.observedNamespaces) == 0 {
		actors, next, err = s.ateClient.ListActorsPage(ctx, input.GetAtespace(), limit, token)
	} else {
		atespaces := s.substrateNamespaces(input.GetAtespace())
		start, inner := 0, ""
		if token != "" {
			// A token without a watched atespace is refused rather than read as page one.
			decoded, decodeErr := base64.RawURLEncoding.DecodeString(token)
			atespace, rest, ok := strings.Cut(string(decoded), "/")
			start = slices.Index(atespaces, atespace)
			if len(token) > maxPageTokenBytes || decodeErr != nil || !ok || atespace == "" || start < 0 {
				return SubstrateActorPage{}, errInvalidPageToken
			}
			inner = rest
		}
		actors, next, err = s.listScopedActors(ctx, atespaces[start:], inner, limit)
	}
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api actors", "error", err)
		return result, nil
	}
	result.Actors = actors
	result.NextPageToken = next
	return result, nil
}

// actorPageToken is the page token of a controller that watches some namespaces: the
// atespace to read next, and ate-api's token inside it. An atespace holds no "/".
func actorPageToken(atespace, inner string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(atespace + "/" + inner))
}

// The longest page token this controller accepts. Its own tokens are far shorter.
const maxPageTokenBytes = 1024

// errInvalidPageToken carries no cause: the token is the caller's, and its text can
// name anything.
var errInvalidPageToken = serviceerrors.NewInvalidArgument("Invalid page token", nil)

// listScopedActors fills one page from the atespaces in order, each with its own scoped
// list, starting at ate-api's inner token in the first.
func (s *Service) listScopedActors(ctx context.Context, atespaces []string, inner string, limit int32) ([]*ateapipb.Actor, string, error) {
	var actors []*ateapipb.Actor
	for i, atespace := range atespaces {
		rows, next, err := s.ateClient.ListActorsPage(ctx, atespace, limit-int32(len(actors)), inner)
		if err != nil {
			return nil, "", err
		}
		for _, actor := range rows {
			// ate-api scoped the list already. This is a second guard.
			if actor.GetMetadata().GetAtespace() == atespace {
				actors = append(actors, actor)
			}
		}
		switch {
		case next != "":
			return actors, actorPageToken(atespace, next), nil
		case int32(len(actors)) >= limit && i+1 < len(atespaces):
			return actors, actorPageToken(atespaces[i+1], ""), nil
		}
		inner = ""
	}
	return actors, "", nil
}

// ListSubstrateWorkers returns one page of workers. A controller that watches every
// namespace filters ate-api's page and passes its token through. A controller that
// watches some reads the Worker of each of its own worker pods, so ate-api reads, and
// no page token names, a worker of a namespace it does not watch.
func (s *Service) ListSubstrateWorkers(ctx context.Context, input *apiv1.ListSubstrateWorkersRequest) (SubstrateWorkerPage, error) {
	namespaces, err := s.substrateScope(ctx, input.GetNamespace(), "")
	if err != nil {
		return SubstrateWorkerPage{}, err
	}
	limit, token := substratePageSize(input.GetPage().GetLimit()), input.GetPage().GetPageToken()
	if len(s.observedNamespaces) > 0 {
		return s.listPodWorkers(ctx, namespaces, limit, token)
	}
	result := SubstrateWorkerPage{ComputedAt: time.Now().UTC()}
	workers, next, err := s.ateClient.ListWorkersPage(ctx, limit, token)
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api workers", "error", err)
		return result, nil
	}
	for _, worker := range workers {
		if inNamespace(worker, namespaces[0]) {
			result.Workers = append(result.Workers, worker)
		}
	}
	result.NextPageToken = next
	return result, nil
}

// listPodWorkers fills one page with the Workers of the worker pods in the namespaces,
// in pod UID order, which is ate-api's worker order. The token is the UID of the last
// worker pod on the previous page: a cursor only, never sent to ate-api.
func (s *Service) listPodWorkers(ctx context.Context, namespaces []string, limit int32, token string) (SubstrateWorkerPage, error) {
	if len(token) > maxPageTokenBytes {
		return SubstrateWorkerPage{}, errInvalidPageToken
	}
	result := SubstrateWorkerPage{ComputedAt: time.Now().UTC()}
	pods, err := s.workerPods(ctx, namespaces)
	if err != nil {
		result.ATEAPIError = err.Error()
		return result, nil
	}
	for i := range pods {
		if string(pods[i].UID) <= token {
			continue
		}
		worker, err := s.podWorker(ctx, &pods[i])
		if err != nil {
			return SubstrateWorkerPage{ATEAPIError: err.Error(), ComputedAt: result.ComputedAt}, nil
		}
		if worker == nil {
			continue
		}
		result.Workers = append(result.Workers, worker)
		if int32(len(result.Workers)) >= limit {
			if i+1 < len(pods) {
				result.NextPageToken = string(pods[i].UID)
			}
			break
		}
	}
	return result, nil
}

// WorkerPoolLabel names the WorkerPool of a Substrate worker pod. ate-api names each
// Worker after its pod's UID.
const WorkerPoolLabel = "ate.dev/worker-pool"

// workerPods lists the worker pods of the namespaces in UID order.
func (s *Service) workerPods(ctx context.Context, namespaces []string) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	for _, namespace := range namespaces {
		podList := &corev1.PodList{}
		// ponytail: one uncached apiserver list per watched namespace per read. Add a
		// label-limited cache if UI polling load matters.
		if err := s.podReader.List(ctx, podList, client.InNamespace(namespace), client.HasLabels{WorkerPoolLabel}); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list worker pods", "error", err, "namespace", namespace)
			return nil, errors.New("failed to list worker pods")
		}
		pods = append(pods, podList.Items...)
	}
	slices.SortFunc(pods, func(left, right corev1.Pod) int { return strings.Compare(string(left.UID), string(right.UID)) })
	return pods, nil
}

// podWorker reads the Worker of a worker pod. It is nil when ate-api has none yet,
// or when the Worker is not that pod's.
func (s *Service) podWorker(ctx context.Context, pod *corev1.Pod) (*ateapipb.Worker, error) {
	worker, err := s.ateClient.GetWorker(ctx, string(pod.UID))
	if status.Code(err) == codes.NotFound {
		// ate-api registers a worker pod only when it is Ready.
		return nil, nil
	}
	if err != nil {
		// ate-api error text can name a worker of any namespace. The user gets the code.
		logging.FromContext(ctx).ErrorContext(ctx, "failed to get ate-api worker", "error", err, "pod", pod.Name)
		return nil, fmt.Errorf("ate-api could not read a worker: %s", status.Code(err))
	}
	if worker.GetWorkerNamespace() != pod.Namespace || worker.GetWorkerPodUid() != string(pod.UID) {
		return nil, nil
	}
	return worker, nil
}

// walkActors visits every actor in the requested atespace, or in each watched one.
func (s *Service) walkActors(ctx context.Context, atespace string, visit func(*ateapipb.Actor)) error {
	for _, atespace := range s.substrateNamespaces(atespace) {
		read := func(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
			return s.ateClient.ListActorsPage(ctx, atespace, pageSize, pageToken)
		}
		if err := walkSubstrate(ctx, read, func(actor *ateapipb.Actor) {
			if actor != nil && s.watches(actor.GetMetadata().GetAtespace()) {
				visit(actor)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

// walkWorkers visits every worker in the namespaces: from ate-api's pages for a
// controller that watches every namespace, else the Worker of each worker pod.
func (s *Service) walkWorkers(ctx context.Context, namespaces []string, visit func(*ateapipb.Worker)) error {
	if len(s.observedNamespaces) == 0 {
		return walkSubstrate(ctx, s.ateClient.ListWorkersPage, func(worker *ateapipb.Worker) {
			if inNamespace(worker, namespaces[0]) {
				visit(worker)
			}
		})
	}
	pods, err := s.workerPods(ctx, namespaces)
	if err != nil {
		return err
	}
	// ponytail: one sequential GetWorker per own worker pod (about 1 ms each). Add
	// bounded concurrency when a controller runs more than about 100 own workers.
	for i := range pods {
		worker, err := s.podWorker(ctx, &pods[i])
		if err != nil {
			return err
		}
		if worker != nil {
			visit(worker)
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

// substrateScope authorizes the caller and resolves the namespaces a read covers.
// A controller that watches namespaces refuses a namespace or atespace it does not watch:
// kagent names each atespace after its namespace, and ate-api serves every tenant.
func (s *Service) substrateScope(ctx context.Context, requestedNamespace, requestedAtespace string) ([]string, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Substrate"}); err != nil {
		return nil, err
	}
	for _, requested := range []string{requestedNamespace, requestedAtespace} {
		if requested != "" && !s.watches(requested) {
			return nil, serviceerrors.NewPermissionDenied("Not authorized", fmt.Errorf("namespace %q is not watched", requested))
		}
	}
	return s.substrateNamespaces(requestedNamespace), nil
}

// watches reports whether this controller manages the namespace, or the atespace of
// the same name.
func (s *Service) watches(namespace string) bool {
	return len(s.observedNamespaces) == 0 || slices.Contains(s.observedNamespaces, namespace)
}

// inNamespace reports whether a worker is in the namespace, or in any when it is empty.
// A controller that watches every namespace reads exactly one, maybe empty.
func inNamespace(worker *ateapipb.Worker, namespace string) bool {
	return worker != nil && (namespace == "" || worker.GetWorkerNamespace() == namespace)
}

func substratePageSize(requested int32) int32 {
	if requested == 0 {
		return defaultSubstratePageSize
	}
	return requested
}
