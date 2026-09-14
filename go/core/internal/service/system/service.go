package system

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Version struct {
	KAgentVersion string
	GitCommit     string
	BuildDate     string
}

type ATEClient interface {
	ListActors(context.Context, string) ([]*ateapipb.Actor, error)
	ListWorkers(context.Context) ([]*ateapipb.Worker, error)
	ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error)
	// The paged reads. Kept alongside the draining ones above, not replacing them: an
	// answer is built from a page, a count from a drain.
	ListActorsPage(ctx context.Context, atespace string, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error)
	ListWorkersPage(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error)
}

type runtimeRevisionStore interface {
	ListActorTemplateHarnesses(context.Context) ([]database.ActorTemplateHarness, error)
}

type Service struct {
	kubeClient         client.Client
	observedNamespaces []string
	authorizer         auth.Authorizer
	ateClient          ATEClient
	revisions          runtimeRevisionStore
}

type Namespace struct {
	Name   string
	Status string
}

type SubstrateStatus struct {
	Enabled        bool
	ATEAPIError    string
	WorkerPools    []atev1alpha1.WorkerPool
	ActorTemplates []SubstrateActorTemplate
	Actors         []*ateapipb.Actor
	Workers        []*ateapipb.Worker
}

type SubstrateActorTemplate struct {
	ActorTemplate   *ateapipb.ActorTemplate
	HarnessName     string
	ManagedByKagent bool
}

func NewService(
	kubeClient client.Client,
	observedNamespaces []string,
	authorizer auth.Authorizer,
	ateClient ATEClient,
	revisions runtimeRevisionStore,
) *Service {
	return &Service{
		kubeClient:         kubeClient,
		observedNamespaces: slices.Clone(observedNamespaces),
		authorizer:         authorizer,
		ateClient:          ateClient,
		revisions:          revisions,
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
			logging.FromContext(ctx).ErrorContext(ctx, "failed to get namespace", "error", err, "namespace", observedNamespace)
			continue
		}
		namespaces = append(namespaces, Namespace{Name: namespace.Name, Status: string(namespace.Status.Phase)})
	}
	sortNamespaces(namespaces)
	return namespaces, nil
}

func (s *Service) GetSubstrateStatus(ctx context.Context, requestedNamespace, atespace string) (SubstrateStatus, error) {
	namespaces, err := s.substrateScope(ctx, requestedNamespace)
	if err != nil {
		return SubstrateStatus{}, err
	}

	result := SubstrateStatus{
		Enabled:        true,
		WorkerPools:    []atev1alpha1.WorkerPool{},
		ActorTemplates: []SubstrateActorTemplate{},
		Actors:         []*ateapipb.Actor{},
		Workers:        []*ateapipb.Worker{},
	}

	for _, namespace := range namespaces {
		workerPools, err := s.listWorkerPools(ctx, namespace)
		if err != nil {
			return SubstrateStatus{}, serviceerrors.NewInternal("Failed to list substrate resources from Kubernetes", err)
		}
		result.WorkerPools = append(result.WorkerPools, workerPools...)
	}

	actorTemplates, actors, workers, err := s.listATEState(ctx, namespaces, atespace)
	result.ActorTemplates = actorTemplates
	result.Actors = actors
	result.Workers = workers
	if err != nil {
		result.ATEAPIError = err.Error()
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list ate-api state", "error", err)
	}

	slices.SortStableFunc(result.WorkerPools, func(left, right atev1alpha1.WorkerPool) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
	slices.SortStableFunc(result.Actors, func(left, right *ateapipb.Actor) int {
		return strings.Compare(actorIdentity(left), actorIdentity(right))
	})
	slices.SortStableFunc(result.Workers, func(left, right *ateapipb.Worker) int {
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

func (s *Service) listWorkerPools(ctx context.Context, namespace string) ([]atev1alpha1.WorkerPool, error) {
	var options []client.ListOption
	if namespace != "" {
		options = append(options, client.InNamespace(namespace))
	}

	workerPoolList := &atev1alpha1.WorkerPoolList{}
	if err := s.kubeClient.List(ctx, workerPoolList, options...); err != nil {
		return nil, err
	}

	return workerPoolList.Items, nil
}

func (s *Service) listATEState(ctx context.Context, namespaces []string, atespace string) ([]SubstrateActorTemplate, []*ateapipb.Actor, []*ateapipb.Worker, error) {
	allowAll, allowed := substrateScopeFilter(namespaces)

	harnesses, err := s.actorTemplateHarnesses(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	templates, err := s.substrateActorTemplates(ctx, harnesses, atespace)
	if err != nil {
		return nil, nil, nil, err
	}
	actorsFromAPI, err := s.ateClient.ListActors(ctx, atespace)
	if err != nil {
		return nil, nil, nil, err
	}
	workersFromAPI, err := s.ateClient.ListWorkers(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	actors := make([]*ateapipb.Actor, 0, len(actorsFromAPI))
	for _, actor := range actorsFromAPI {
		if actor == nil {
			continue
		}
		actors = append(actors, actor)
	}

	workers := make([]*ateapipb.Worker, 0, len(workersFromAPI))
	for _, worker := range workersFromAPI {
		if worker == nil {
			continue
		}
		if !allowedWorkerNamespace(worker.GetWorkerNamespace(), allowAll, allowed) {
			continue
		}
		workers = append(workers, worker)
	}
	return templates, actors, workers, nil
}

/*
substrateActorTemplates lists the ActorTemplates in scope, with the harness each one
was compiled from.

Drains ate-api's pagination where the actor and worker reads page it: templates are
configuration, so the list is small enough to answer with whole. The harnesses are
passed in rather than read here, so a caller can tell an ate-api failure from a
database one.
*/
func (s *Service) substrateActorTemplates(
	ctx context.Context,
	harnesses map[actorTemplateKey]string,
	atespace string,
) ([]SubstrateActorTemplate, error) {
	templatesFromAPI, err := s.ateClient.ListActorTemplates(ctx, atespace)
	if err != nil {
		return nil, err
	}
	templates := make([]SubstrateActorTemplate, 0, len(templatesFromAPI))
	for _, template := range templatesFromAPI {
		if template == nil {
			continue
		}
		metadata := template.GetMetadata()
		templates = append(templates, SubstrateActorTemplate{
			// Only expose inventory fields: containers can contain resolved credentials.
			ActorTemplate: &ateapipb.ActorTemplate{
				Metadata:       metadata,
				Status:         template.GetStatus(),
				SandboxConfig:  template.GetSandboxConfig(),
				WorkerSelector: template.GetWorkerSelector(),
			},
			HarnessName:     harnesses[actorTemplateKey{metadata.GetAtespace(), metadata.GetName(), metadata.GetUid()}],
			ManagedByKagent: true,
		})
	}

	slices.SortStableFunc(templates, func(left, right SubstrateActorTemplate) int {
		leftMetadata, rightMetadata := left.ActorTemplate.GetMetadata(), right.ActorTemplate.GetMetadata()
		return strings.Compare(leftMetadata.GetAtespace()+"/"+leftMetadata.GetName(), rightMetadata.GetAtespace()+"/"+rightMetadata.GetName())
	})
	return templates, nil
}

// actorTemplateKey identifies one ActorTemplate revision, which is what a compiled
// harness is keyed by.
type actorTemplateKey struct{ atespace, name, uid string }

// actorTemplateHarnesses reads from the control-plane database which harness each
// ActorTemplate revision was compiled from. Nothing here touches ate-api.
func (s *Service) actorTemplateHarnesses(ctx context.Context) (map[actorTemplateKey]string, error) {
	harnessesFromDB, err := s.revisions.ListActorTemplateHarnesses(ctx)
	if err != nil {
		return nil, err
	}
	harnesses := make(map[actorTemplateKey]string, len(harnessesFromDB))
	for _, template := range harnessesFromDB {
		harnesses[actorTemplateKey{template.Atespace, template.Name, template.UID}] = template.HarnessName
	}
	return harnesses, nil
}
