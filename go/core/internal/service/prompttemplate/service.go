package prompttemplate

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	promptTemplateResource = "PromptTemplate"
	promptLibraryLabelKey  = "kagent.dev/prompt-library"
	promptLibraryLabelVal  = "true"
)

type Service struct {
	kubeClient client.Client
	authorizer auth.CollectionAuthorizer
}

type Summary struct {
	Namespace string
	Name      string
	KeyCount  int
	Keys      []string
}

type Detail struct {
	Namespace string
	Name      string
	Data      map[string]string
}

type CreateRequest struct {
	Namespace string
	Name      string
	Data      map[string]string
}

func NewService(kubeClient client.Client, authorizer auth.CollectionAuthorizer) *Service {
	return &Service{kubeClient: kubeClient, authorizer: authorizer}
}

func (s *Service) List(ctx context.Context, namespace string) ([]Summary, error) {
	if namespace == "" {
		return nil, serviceerrors.NewInvalidArgument("namespace query parameter is required", nil)
	}

	matcher, err := kubeauth.CollectionMatcher(ctx, s.authorizer, promptTemplateResource)
	if err != nil {
		return nil, err
	}

	if matcher.Empty() {
		return []Summary{}, nil
	}
	list := &corev1.ConfigMapList{}
	if err := s.kubeClient.List(
		ctx,
		list,
		client.InNamespace(namespace),
		client.MatchingLabels(promptLibraryLabelSelector())); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list prompt template ConfigMaps", err)
	}

	result := make([]Summary, 0, len(list.Items))
	for index := range list.Items {
		if matcher.Matches(&list.Items[index]) {
			result = append(result, summarize(&list.Items[index]))
		}
	}
	slices.SortFunc(result, func(left, right Summary) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return result, nil
}

func (s *Service) Get(ctx context.Context, ref types.NamespacedName) (Detail, error) {
	if err := validateRef(ref); err != nil {
		return Detail{}, err
	}
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: promptTemplateResource, Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return Detail{}, err
	}

	configMap, err := s.library(ctx, ref)
	if err != nil {
		return Detail{}, err
	}
	return detail(configMap), nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (Detail, error) {
	if err := validateCreateRequest(request); err != nil {
		return Detail{}, err
	}
	if err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: promptTemplateResource, Namespace: request.Namespace, Name: request.Name}); err != nil {
		return Detail{}, err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: request.Namespace,
			Name:      request.Name,
			Labels:    promptLibraryLabelSelector(),
		},
		Data: cloneStringMap(request.Data),
	}
	if err := s.kubeClient.Create(ctx, configMap); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return Detail{}, serviceerrors.NewAlreadyExists("A ConfigMap with this name already exists in the namespace", err)
		}
		return Detail{}, serviceerrors.NewInternal("Failed to create ConfigMap", err)
	}
	return detail(configMap), nil
}

func (s *Service) Update(ctx context.Context, ref types.NamespacedName, data map[string]string) (Detail, error) {
	if err := validateRef(ref); err != nil {
		return Detail{}, err
	}
	if err := s.authorize(ctx, auth.VerbUpdate, auth.Resource{Type: promptTemplateResource, Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return Detail{}, err
	}
	if len(data) == 0 {
		return Detail{}, serviceerrors.NewInvalidArgument("at least one template key is required", nil)
	}

	configMap, err := s.library(ctx, ref)
	if err != nil {
		return Detail{}, err
	}
	configMap.Data = cloneStringMap(data)
	if err := s.kubeClient.Update(ctx, configMap); err != nil {
		return Detail{}, serviceerrors.NewInternal("Failed to update ConfigMap", err)
	}
	return detail(configMap), nil
}

func (s *Service) Delete(ctx context.Context, ref types.NamespacedName) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: promptTemplateResource, Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return err
	}

	configMap, err := s.library(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.kubeClient.Delete(ctx, configMap); err != nil {
		return serviceerrors.NewInternal("Failed to delete ConfigMap", err)
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	if err := s.authorizer.Check(ctx, session.Principal(), verb, resource); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func promptLibraryLabelSelector() map[string]string {
	return map[string]string{promptLibraryLabelKey: promptLibraryLabelVal}
}

func summarize(configMap *corev1.ConfigMap) Summary {
	keys := make([]string, 0, len(configMap.Data))
	for key := range configMap.Data {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return Summary{
		Namespace: configMap.Namespace,
		Name:      configMap.Name,
		KeyCount:  len(configMap.Data) + len(configMap.BinaryData),
		Keys:      keys,
	}
}

func detail(configMap *corev1.ConfigMap) Detail {
	return Detail{
		Namespace: configMap.Namespace,
		Name:      configMap.Name,
		Data:      cloneStringMap(configMap.Data),
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return maps.Clone(values)
}

func validateRef(ref types.NamespacedName) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument("PromptTemplate namespace and name are required", nil)
	}
	return nil
}

func validateCreateRequest(request CreateRequest) error {
	if request.Namespace == "" {
		return serviceerrors.NewInvalidArgument("namespace is required", nil)
	}
	if validationErrors := utilvalidation.IsDNS1123Subdomain(request.Namespace); len(validationErrors) > 0 {
		return serviceerrors.NewInvalidArgument("namespace must be a valid DNS subdomain", nil)
	}
	if request.Name == "" {
		return serviceerrors.NewInvalidArgument("name is required", nil)
	}
	if validationErrors := utilvalidation.IsDNS1123Subdomain(request.Name); len(validationErrors) > 0 {
		return serviceerrors.NewInvalidArgument("name must be a valid DNS subdomain", nil)
	}
	if len(request.Data) == 0 {
		return serviceerrors.NewInvalidArgument("at least one template key is required", nil)
	}
	if _, found := request.Data[""]; found {
		return serviceerrors.NewInvalidArgument("template keys cannot be empty", nil)
	}
	return nil
}

// A prompt reference addresses a library, not an arbitrary ConfigMap. Direct
// reads and mutations retain the same boundary as label-filtered discovery.
func (s *Service) library(ctx context.Context, ref types.NamespacedName) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	if err := s.kubeClient.Get(ctx, ref, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("Prompt library not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get prompt library", err)
	}
	if configMap.Labels[promptLibraryLabelKey] != promptLibraryLabelVal {
		return nil, serviceerrors.NewNotFound("Prompt library not found", nil)
	}
	return configMap, nil
}
