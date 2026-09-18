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
	ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error)
	ListActorsPage(ctx context.Context, atespace string, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error)
	ListWorkersPage(ctx context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error)
}

type Service struct {
	kubeClient         client.Client
	observedNamespaces []string
	authorizer         auth.Authorizer
	ateClient          ATEClient
}

type Namespace struct {
	Name   string
	Status string
}

func NewService(
	kubeClient client.Client,
	observedNamespaces []string,
	authorizer auth.Authorizer,
	ateClient ATEClient,
) *Service {
	return &Service{
		kubeClient:         kubeClient,
		observedNamespaces: slices.Clone(observedNamespaces),
		authorizer:         authorizer,
		ateClient:          ateClient,
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

// substrateActorTemplates drains upstream pagination for the configuration-sized list.
func (s *Service) substrateActorTemplates(ctx context.Context, atespace string) ([]*ateapipb.ActorTemplate, error) {
	templatesFromAPI, err := s.ateClient.ListActorTemplates(ctx, atespace)
	if err != nil {
		return nil, err
	}
	templates := make([]*ateapipb.ActorTemplate, 0, len(templatesFromAPI))
	for _, template := range templatesFromAPI {
		if template == nil {
			continue
		}
		// Only expose inventory fields: containers can contain resolved credentials.
		templates = append(templates, &ateapipb.ActorTemplate{
			Metadata:       template.GetMetadata(),
			Status:         template.GetStatus(),
			SandboxConfig:  template.GetSandboxConfig(),
			WorkerSelector: template.GetWorkerSelector(),
		})
	}

	slices.SortStableFunc(templates, func(left, right *ateapipb.ActorTemplate) int {
		leftMetadata, rightMetadata := left.GetMetadata(), right.GetMetadata()
		return strings.Compare(leftMetadata.GetAtespace()+"/"+leftMetadata.GetName(), rightMetadata.GetAtespace()+"/"+rightMetadata.GetName())
	})
	return templates, nil
}
