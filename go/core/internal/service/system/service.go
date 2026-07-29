package system

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Version struct {
	KAgentVersion string
	GitCommit     string
	BuildDate     string
}

type ATEClient interface {
	ListActors(context.Context, string) ([]*ateapipb.Actor, error)
	ListWorkers(context.Context) ([]*ateapipb.Worker, error)
}

type Service struct {
	kubeClient         client.Client
	observedNamespaces []string
	authorizer         auth.Authorizer
	ateClient          ATEClient
}

type Option func(*Service)

type Namespace struct {
	Name   string
	Status string
}

type SubstrateStatus struct {
	Enabled        bool
	ATEAPIError    string
	WorkerPools    []SubstrateWorkerPool
	ActorTemplates []SubstrateActorTemplate
	Actors         []SubstrateActor
	Workers        []SubstrateWorker
}

type SubstrateWorkerPool struct {
	Namespace  string
	Name       string
	Replicas   int32
	AteomImage string
}

type SubstrateActorTemplate struct {
	Namespace       string
	Name            string
	Phase           string
	GoldenActorID   string
	GoldenSnapshot  string
	SandboxClass    string
	WorkerSelector  string
	HarnessName     string
	ManagedByKagent bool
}

type SubstrateActor struct {
	ActorID                string
	Atespace               string
	Status                 string
	ActorTemplateNamespace string
	ActorTemplateName      string
	AteomPodNamespace      string
	AteomPodName           string
	AteomPodIP             string
	LatestSnapshot         string
	WorkerPoolName         string
	InProgressSnapshot     string
	Version                int64
}

type SubstrateWorker struct {
	WorkerNamespace string
	WorkerPool      string
	WorkerPod       string
	ActorNamespace  string
	ActorTemplate   string
	ActorID         string
	IP              string
	Version         int64
}

func NewService(options ...Option) *Service {
	service := &Service{}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithInventory(
	kubeClient client.Client,
	observedNamespaces []string,
	authorizer auth.Authorizer,
	ateClient ATEClient,
) Option {
	return func(service *Service) {
		service.kubeClient = kubeClient
		service.observedNamespaces = slices.Clone(observedNamespaces)
		service.authorizer = authorizer
		service.ateClient = ateClient
	}
}

func (s *Service) GetVersion() Version {
	info := version.Get()
	return Version{
		KAgentVersion: info.Version,
		GitCommit:     info.GitCommit,
		BuildDate:     info.BuildDate,
	}
}

func (s *Service) GetCurrentUser(ctx context.Context) (map[string]any, error) {
	principal, err := authenticatedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if principal.Claims != nil {
		return maps.Clone(principal.Claims), nil
	}
	return map[string]any{"sub": principal.User.ID}, nil
}

func (s *Service) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	if s.kubeClient == nil {
		return nil, serviceerrors.NewInternal("Failed to list namespaces", fmt.Errorf("Kubernetes client is not configured"))
	}
	if len(s.observedNamespaces) == 0 {
		namespaceList := &corev1.NamespaceList{}
		if err := s.kubeClient.List(ctx, namespaceList); err != nil {
			return nil, serviceerrors.NewInternal("Failed to list namespaces", err)
		}

		namespaces := make([]Namespace, 0, len(namespaceList.Items))
		for _, namespace := range namespaceList.Items {
			namespaces = append(namespaces, Namespace{Name: namespace.Name, Status: string(namespace.Status.Phase)})
		}
		sortNamespaces(namespaces)
		return namespaces, nil
	}

	namespaces := make([]Namespace, 0, len(s.observedNamespaces))
	for _, observedNamespace := range s.observedNamespaces {
		namespace := &corev1.Namespace{}
		if err := s.kubeClient.Get(ctx, client.ObjectKey{Name: observedNamespace}, namespace); err != nil {
			if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
				namespaces = namespacesFromNames(s.observedNamespaces)
				break
			}
			if apierrors.IsNotFound(err) {
				continue
			}
			ctrllog.FromContext(ctx).Error(err, "Failed to get namespace", "namespace", observedNamespace)
			continue
		}
		namespaces = append(namespaces, Namespace{Name: namespace.Name, Status: string(namespace.Status.Phase)})
	}
	sortNamespaces(namespaces)
	return namespaces, nil
}

func (s *Service) GetSubstrateStatus(ctx context.Context, requestedNamespace string) (SubstrateStatus, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Agent"}); err != nil {
		return SubstrateStatus{}, err
	}

	requestedNamespace = strings.TrimSpace(requestedNamespace)
	if requestedNamespace != "" {
		if validationErrors := utilvalidation.IsDNS1123Label(requestedNamespace); len(validationErrors) > 0 {
			return SubstrateStatus{}, serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid namespace %q: %s", requestedNamespace, strings.Join(validationErrors, ", ")),
				nil,
			)
		}
	}

	result := SubstrateStatus{
		Enabled:        s.ateClient != nil,
		WorkerPools:    []SubstrateWorkerPool{},
		ActorTemplates: []SubstrateActorTemplate{},
		Actors:         []SubstrateActor{},
		Workers:        []SubstrateWorker{},
	}
	if s.ateClient == nil {
		return result, nil
	}
	if s.kubeClient == nil {
		return SubstrateStatus{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", fmt.Errorf("Kubernetes client is not configured"))
	}

	namespaces := s.substrateNamespaces(requestedNamespace)
	for _, namespace := range namespaces {
		workerPools, actorTemplates, err := s.listSubstrateCRs(ctx, namespace)
		if err != nil {
			return SubstrateStatus{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		result.WorkerPools = append(result.WorkerPools, workerPools...)
		result.ActorTemplates = append(result.ActorTemplates, actorTemplates...)
	}

	actors, workers, err := s.listATEState(ctx, namespaces)
	result.Actors = actors
	result.Workers = workers
	if err != nil {
		result.ATEAPIError = err.Error()
		ctrllog.FromContext(ctx).Error(err, "list ate-api state")
	}

	slices.SortStableFunc(result.WorkerPools, func(left, right SubstrateWorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
	slices.SortStableFunc(result.ActorTemplates, func(left, right SubstrateActorTemplate) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
	slices.SortStableFunc(result.Actors, func(left, right SubstrateActor) int {
		return strings.Compare(left.ActorID, right.ActorID)
	})
	slices.SortStableFunc(result.Workers, func(left, right SubstrateWorker) int {
		return strings.Compare(
			left.WorkerNamespace+"/"+left.WorkerPool+"/"+left.WorkerPod,
			right.WorkerNamespace+"/"+right.WorkerPool+"/"+right.WorkerPod,
		)
	})
	return result, nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) error {
	principal, err := authenticatedPrincipal(ctx)
	if err != nil {
		return err
	}
	if s.authorizer == nil {
		return serviceerrors.NewInternal("Authorization is not configured", nil)
	}
	if err := s.authorizer.Check(ctx, principal, verb, resource); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func authenticatedPrincipal(ctx context.Context) (auth.Principal, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return auth.Principal{}, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	return session.Principal(), nil
}

func sortNamespaces(namespaces []Namespace) {
	slices.SortStableFunc(namespaces, func(left, right Namespace) int {
		return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
	})
}

func namespacesFromNames(names []string) []Namespace {
	result := make([]Namespace, 0, len(names))
	for _, name := range names {
		result = append(result, Namespace{Name: name})
	}
	return result
}

func (s *Service) substrateNamespaces(requested string) []string {
	if requested != "" {
		return []string{requested}
	}
	if len(s.observedNamespaces) > 0 {
		return slices.Clone(s.observedNamespaces)
	}
	return []string{""}
}

func (s *Service) listSubstrateCRs(ctx context.Context, namespace string) ([]SubstrateWorkerPool, []SubstrateActorTemplate, error) {
	var options []client.ListOption
	if namespace != "" {
		options = append(options, client.InNamespace(namespace))
	}

	workerPoolList := &atev1alpha1.WorkerPoolList{}
	if err := s.kubeClient.List(ctx, workerPoolList, options...); err != nil {
		return nil, nil, err
	}
	actorTemplateList := &atev1alpha1.ActorTemplateList{}
	if err := s.kubeClient.List(ctx, actorTemplateList, options...); err != nil {
		return nil, nil, err
	}

	workerPools := make([]SubstrateWorkerPool, 0, len(workerPoolList.Items))
	for index := range workerPoolList.Items {
		workerPool := &workerPoolList.Items[index]
		workerPools = append(workerPools, SubstrateWorkerPool{
			Namespace:  workerPool.Namespace,
			Name:       workerPool.Name,
			Replicas:   workerPool.Spec.Replicas,
			AteomImage: workerPool.Spec.AteomImage,
		})
	}

	actorTemplates := make([]SubstrateActorTemplate, 0, len(actorTemplateList.Items))
	for index := range actorTemplateList.Items {
		actorTemplate := &actorTemplateList.Items[index]
		entry := SubstrateActorTemplate{
			Namespace:       actorTemplate.Namespace,
			Name:            actorTemplate.Name,
			Phase:           string(actorTemplate.Status.Phase),
			GoldenActorID:   actorTemplate.Status.GoldenActorID,
			GoldenSnapshot:  actorTemplate.Status.GoldenSnapshot,
			SandboxClass:    string(actorTemplate.Spec.SandboxClass),
			WorkerSelector:  labelSelectorString(ctx, actorTemplate.Spec.WorkerSelector),
			ManagedByKagent: actorTemplate.Labels["app.kubernetes.io/managed-by"] == "kagent",
		}
		if harness := strings.TrimSpace(actorTemplate.Labels[substrate.HarnessLabelKey]); harness != "" {
			entry.HarnessName = harness
		} else if agentName := substrate.SandboxAgentNameFromLabels(actorTemplate.Labels); agentName != "" {
			entry.HarnessName = agentName
		}
		actorTemplates = append(actorTemplates, entry)
	}
	return workerPools, actorTemplates, nil
}

func (s *Service) listATEState(ctx context.Context, namespaces []string) ([]SubstrateActor, []SubstrateWorker, error) {
	allowAll := len(namespaces) == 1 && namespaces[0] == ""
	allowed := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		if namespace != "" {
			allowed[namespace] = struct{}{}
		}
	}

	actorsFromAPI, err := s.ateClient.ListActors(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	workersFromAPI, err := s.ateClient.ListWorkers(ctx)
	if err != nil {
		return nil, nil, err
	}

	actors := make([]SubstrateActor, 0, len(actorsFromAPI))
	for _, actor := range actorsFromAPI {
		if actor == nil {
			continue
		}
		namespace := strings.TrimSpace(actor.GetActorTemplateNamespace())
		if !allowAll && namespace != "" {
			if _, ok := allowed[namespace]; !ok {
				continue
			}
		}
		actors = append(actors, actorFromProto(actor))
	}

	workers := make([]SubstrateWorker, 0, len(workersFromAPI))
	for _, worker := range workersFromAPI {
		if worker == nil {
			continue
		}
		namespace := strings.TrimSpace(worker.GetWorkerNamespace())
		if !allowAll && namespace != "" {
			if _, ok := allowed[namespace]; !ok {
				continue
			}
		}
		workers = append(workers, workerFromProto(worker))
	}
	return actors, workers, nil
}

func actorFromProto(actor *ateapipb.Actor) SubstrateActor {
	return SubstrateActor{
		ActorID:                actor.GetMetadata().GetName(),
		Atespace:               actor.GetMetadata().GetAtespace(),
		Status:                 substrate.ActorStatusLabel(actor.GetStatus()),
		ActorTemplateNamespace: actor.GetActorTemplateNamespace(),
		ActorTemplateName:      actor.GetActorTemplateName(),
		AteomPodNamespace:      actor.GetAteomPodNamespace(),
		AteomPodName:           actor.GetAteomPodName(),
		AteomPodIP:             actor.GetAteomPodIp(),
		LatestSnapshot:         snapshotInfoString(actor.GetLatestSnapshotInfo()),
		WorkerPoolName:         actor.GetWorkerPoolName(),
		InProgressSnapshot:     actor.GetInProgressSnapshot(),
		Version:                actor.GetMetadata().GetVersion(),
	}
}

func workerFromProto(worker *ateapipb.Worker) SubstrateWorker {
	return SubstrateWorker{
		WorkerNamespace: worker.GetWorkerNamespace(),
		WorkerPool:      worker.GetWorkerPool(),
		WorkerPod:       worker.GetWorkerPod(),
		ActorNamespace:  worker.GetAssignment().GetActorTemplate().GetNamespace(),
		ActorTemplate:   worker.GetAssignment().GetActorTemplate().GetName(),
		ActorID:         worker.GetAssignment().GetActor().GetName(),
		IP:              worker.GetIp(),
		Version:         worker.GetVersion(),
	}
}

func snapshotInfoString(snapshot *ateapipb.SnapshotInfo) string {
	if snapshot == nil {
		return ""
	}
	if external := snapshot.GetExternal(); external != nil {
		return external.GetSnapshotUriPrefix()
	}
	if local := snapshot.GetLocal(); local != nil {
		return local.GetSnapshotPrefix()
	}
	return ""
}

func labelSelectorString(ctx context.Context, selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	result, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		ctrllog.FromContext(ctx).Info("invalid ActorTemplate workerSelector", "error", err)
		return "<invalid selector>"
	}
	return result.String()
}
