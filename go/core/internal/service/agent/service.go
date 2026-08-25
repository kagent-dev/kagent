package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Kind string

const (
	KindSandboxAgent Kind = "SandboxAgent"
	KindAgentHarness Kind = "AgentHarness"
)

type HarnessDetails struct {
	Backend      v1alpha3.AgentHarnessBackendType
	ActorID      string
	BackendRefID string
	Endpoint     string
}

type View struct {
	Ref            types.NamespacedName
	Kind           Kind
	Resource       client.Object
	ID             string
	ModelProvider  v1alpha3.ModelProvider
	Model          string
	ModelConfigRef types.NamespacedName
	MemoryRefs     []string
	Tools          []*v1alpha3.Tool
	Ready          bool
	Accepted       bool
	Harness        *HarnessDetails
}

type ListRequest struct {
	Namespace string
}

type GetRequest struct {
	Ref types.NamespacedName
}

type CreateSandboxAgentRequest struct {
	Agent *v1alpha3.SandboxAgent
}

type UpdateSandboxAgentRequest struct {
	Ref   types.NamespacedName
	Agent *v1alpha3.SandboxAgent
}

type CreateAgentHarnessRequest struct {
	AgentHarness *v1alpha3.AgentHarness
}

type DeleteRequest struct {
	Ref types.NamespacedName
}

type Validator func(context.Context, *v1alpha3.SandboxAgent) error

type ServiceOption func(*Service)

func WithValidator(validator Validator) ServiceOption {
	return func(service *Service) {
		service.validator = validator
	}
}

type Service struct {
	kubeClient       client.Client
	authorizer       auth.Authorizer
	defaultNamespace string
	validator        Validator
}

func NewService(kubeClient client.Client, authorizer auth.Authorizer, defaultNamespace string, options ...ServiceOption) *Service {
	service := &Service{
		kubeClient:       kubeClient,
		authorizer:       authorizer,
		defaultNamespace: defaultNamespace,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) List(ctx context.Context, request ListRequest) ([]View, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Agent"}); err != nil {
		return nil, err
	}

	options := make([]client.ListOption, 0, 1)
	if request.Namespace != "" {
		if strings.TrimSpace(request.Namespace) != request.Namespace {
			return nil, serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid namespace %q: must not contain leading or trailing whitespace", request.Namespace),
				nil,
			)
		}
		if validationErrors := utilvalidation.IsDNS1123Label(request.Namespace); len(validationErrors) > 0 {
			return nil, serviceerrors.NewInvalidArgument(
				fmt.Sprintf("invalid namespace %q: %s", request.Namespace, strings.Join(validationErrors, "; ")),
				nil,
			)
		}
		options = append(options, client.InNamespace(request.Namespace))
	}

	sandboxAgents := &v1alpha3.SandboxAgentList{}
	if err := s.kubeClient.List(ctx, sandboxAgents, options...); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list SandboxAgents from Kubernetes", err)
	}
	harnesses := &v1alpha3.AgentHarnessList{}
	if err := s.kubeClient.List(ctx, harnesses, options...); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list AgentHarness resources from Kubernetes", err)
	}

	views := make([]View, 0, len(sandboxAgents.Items)+len(harnesses.Items))
	for index := range sandboxAgents.Items {
		view, _ := s.agentView(ctx, &sandboxAgents.Items[index], KindSandboxAgent)
		views = append(views, view)
	}
	for index := range harnesses.Items {
		harness := &harnesses.Items[index]
		if !v1alpha3.IsKnownAgentHarnessBackend(harness.Spec.Backend) {
			continue
		}
		views = append(views, s.harnessView(ctx, harness))
	}
	return views, nil
}

func (s *Service) GetSandboxAgent(ctx context.Context, request GetRequest) (View, error) {
	agent := &v1alpha3.SandboxAgent{}
	if err := s.get(ctx, request.Ref, agent, "SandboxAgent not found"); err != nil {
		return View{}, err
	}
	return s.agentView(ctx, agent, KindSandboxAgent)
}

func (s *Service) GetAgentHarness(ctx context.Context, request GetRequest) (View, error) {
	harness := &v1alpha3.AgentHarness{}
	if err := s.get(ctx, request.Ref, harness, "AgentHarness not found"); err != nil {
		return View{}, err
	}
	if !v1alpha3.IsKnownAgentHarnessBackend(harness.Spec.Backend) {
		return View{}, serviceerrors.NewNotFound("AgentHarness not found", nil)
	}
	return s.harnessView(ctx, harness), nil
}

func (s *Service) CreateSandboxAgent(ctx context.Context, request CreateSandboxAgentRequest) (View, error) {
	if request.Agent == nil {
		return View{}, serviceerrors.NewInvalidArgument("SandboxAgent resource is required", nil)
	}
	agent := request.Agent.DeepCopy()
	normalizeSandboxAgent(agent)
	if _, err := s.prepareCreate(ctx, agent, KindSandboxAgent, auth.VerbCreate); err != nil {
		return View{}, err
	}
	if err := s.validate(ctx, agent); err != nil {
		return View{}, err
	}
	if err := s.kubeClient.Create(ctx, agent); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return View{}, serviceerrors.NewAlreadyExists("SandboxAgent already exists", err)
		}
		return View{}, serviceerrors.NewInternal("Failed to create Agent in Kubernetes", err)
	}
	return s.agentView(ctx, agent, KindSandboxAgent)
}

func (s *Service) UpdateSandboxAgent(ctx context.Context, request UpdateSandboxAgentRequest) (View, error) {
	if request.Agent == nil {
		return View{}, serviceerrors.NewInvalidArgument("SandboxAgent resource is required", nil)
	}
	incoming := request.Agent.DeepCopy()
	normalizeSandboxAgent(incoming)
	ref, err := s.updateRef(incoming, request.Ref, true)
	if err != nil {
		return View{}, err
	}
	if err := s.authorize(ctx, auth.VerbUpdate, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
		return View{}, err
	}
	existing := &v1alpha3.SandboxAgent{}
	if err := s.loadForMutation(ctx, ref, existing, "SandboxAgent not found", "Failed to get SandboxAgent"); err != nil {
		return View{}, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	if err := s.validate(ctx, existing); err != nil {
		return View{}, err
	}
	if err := s.kubeClient.Update(ctx, existing); err != nil {
		return View{}, serviceerrors.NewInternal("Failed to update SandboxAgent", err)
	}
	return s.agentView(ctx, existing, KindSandboxAgent)
}

func (s *Service) DeleteSandboxAgent(ctx context.Context, request DeleteRequest) error {
	return s.delete(ctx, request.Ref, &v1alpha3.SandboxAgent{}, "SandboxAgent not found", "Failed to delete SandboxAgent")
}

func (s *Service) CreateAgentHarness(ctx context.Context, request CreateAgentHarnessRequest) (View, error) {
	if request.AgentHarness == nil {
		return View{}, serviceerrors.NewInvalidArgument("AgentHarness resource is required", nil)
	}
	harness := request.AgentHarness.DeepCopy()
	if harness.APIVersion == "" {
		harness.APIVersion = v1alpha3.GroupVersion.String()
	}
	if harness.Kind == "" {
		harness.Kind = string(KindAgentHarness)
	}
	if _, err := s.prepareCreate(ctx, harness, KindAgentHarness, auth.VerbCreate); err != nil {
		return View{}, err
	}
	if strings.TrimSpace(string(harness.Spec.Backend)) == "" {
		return View{}, serviceerrors.NewInvalidArgument("spec.backend is required", nil)
	}
	if err := s.kubeClient.Create(ctx, harness); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return View{}, serviceerrors.NewAlreadyExists("AgentHarness already exists", err)
		}
		return View{}, serviceerrors.NewInternal("Failed to create AgentHarness in Kubernetes", err)
	}
	return s.harnessView(ctx, harness), nil
}

func (s *Service) DeleteAgentHarness(ctx context.Context, request DeleteRequest) error {
	if request.Ref.Namespace == "" || request.Ref.Name == "" {
		return serviceerrors.NewInvalidArgument("AgentHarness namespace and name are required", nil)
	}
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "Agent", Name: request.Ref.String()}); err != nil {
		return err
	}
	harness := &v1alpha3.AgentHarness{}
	if err := s.loadForMutation(ctx, request.Ref, harness, "AgentHarness not found", "Failed to get AgentHarness"); err != nil {
		return err
	}
	if !v1alpha3.IsKnownAgentHarnessBackend(harness.Spec.Backend) {
		return serviceerrors.NewNotFound("AgentHarness not found", nil)
	}
	if err := s.kubeClient.Delete(ctx, harness); err != nil {
		return serviceerrors.NewInternal("Failed to delete AgentHarness", err)
	}
	return nil
}

func (s *Service) prepareCreate(ctx context.Context, object client.Object, kind Kind, verb auth.Verb) (types.NamespacedName, error) {
	if object.GetNamespace() == "" {
		object.SetNamespace(s.defaultNamespace)
	}
	ref, err := utils.ParseRefString(object.GetName(), object.GetNamespace())
	if err != nil {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument(fmt.Sprintf("Invalid %s metadata", kind), err)
	}
	if ref.Name != object.GetName() || ref.Namespace != object.GetNamespace() {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument(fmt.Sprintf("Invalid %s metadata", kind), nil)
	}
	if err := s.authorize(ctx, verb, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
		return types.NamespacedName{}, err
	}
	return ref, nil
}

func (s *Service) updateRef(object client.Object, requestRef types.NamespacedName, requireMatch bool) (types.NamespacedName, error) {
	if object.GetNamespace() == "" {
		object.SetNamespace(s.defaultNamespace)
	}
	bodyRef, err := utils.ParseRefString(object.GetName(), object.GetNamespace())
	if err != nil || bodyRef.Name != object.GetName() || bodyRef.Namespace != object.GetNamespace() {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Invalid Agent metadata", err)
	}
	if requestRef.Namespace == "" && requestRef.Name == "" {
		return bodyRef, nil
	}
	if requestRef.Namespace == "" || requestRef.Name == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Agent namespace and name are required", nil)
	}
	if requireMatch && requestRef != bodyRef {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Path does not match request body metadata", nil)
	}
	if !requireMatch && requestRef != bodyRef {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("Agent reference does not match resource metadata", nil)
	}
	return requestRef, nil
}

func (s *Service) validate(ctx context.Context, object *v1alpha3.SandboxAgent) error {
	if err := v1alpha3.ValidateSubstrateSandboxAgentSpec(object); err != nil {
		return serviceerrors.NewInvalidArgument(err.Error(), err)
	}
	if s.validator == nil {
		return nil
	}
	if err := s.validator(ctx, object); err != nil {
		if serviceerrors.CodeOf(err) != "" {
			return err
		}
		return serviceerrors.NewInvalidArgument("Invalid agent configuration", err)
	}
	return nil
}

func (s *Service) delete(ctx context.Context, ref types.NamespacedName, object client.Object, notFoundMessage, failureMessage string) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument("Agent namespace and name are required", nil)
	}
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
		return err
	}
	if err := s.loadForMutation(ctx, ref, object, notFoundMessage, "Failed to get Agent"); err != nil {
		return err
	}
	if err := s.kubeClient.Delete(ctx, object); err != nil {
		return serviceerrors.NewInternal(failureMessage, err)
	}
	return nil
}

func (s *Service) loadForMutation(ctx context.Context, ref types.NamespacedName, object client.Object, notFoundMessage, failureMessage string) error {
	if err := s.kubeClient.Get(ctx, ref, object); err != nil {
		if apierrors.IsNotFound(err) {
			return serviceerrors.NewNotFound(notFoundMessage, err)
		}
		return serviceerrors.NewInternal(failureMessage, err)
	}
	return nil
}

func normalizeSandboxAgent(agent *v1alpha3.SandboxAgent) {
	if agent.Spec.Type == "" {
		agent.Spec.Type = v1alpha3.AgentType_Declarative
	}
}

func (s *Service) get(ctx context.Context, ref types.NamespacedName, object client.Object, notFoundMessage string) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument("Agent namespace and name are required", nil)
	}
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "Agent", Name: ref.String()}); err != nil {
		return err
	}
	if err := s.kubeClient.Get(ctx, ref, object); err != nil {
		if apierrors.IsNotFound(err) {
			return serviceerrors.NewNotFound(notFoundMessage, err)
		}
		return serviceerrors.NewInternal("Failed to get Agent", err)
	}
	return nil
}

func (s *Service) agentView(ctx context.Context, object *v1alpha3.SandboxAgent, kind Kind) (View, error) {
	ref := types.NamespacedName{Namespace: object.GetNamespace(), Name: object.GetName()}
	view := View{
		Ref:      ref,
		Kind:     kind,
		Resource: object,
		ID:       utils.ConvertToPythonIdentifier(utils.GetObjectRef(object)),
	}
	for _, condition := range object.GetAgentStatus().Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue && condition.Reason == "WorkloadReady" {
			view.Ready = true
		}
		if condition.Type == "Accepted" && condition.Status == metav1.ConditionTrue {
			view.Accepted = true
		}
	}

	spec := object.GetAgentSpec()
	if spec.Type != v1alpha3.AgentType_Declarative || spec.Declarative == nil {
		return view, nil
	}
	view.Tools = spec.Declarative.Tools
	modelConfigRef := types.NamespacedName{Namespace: object.GetNamespace(), Name: spec.Declarative.ModelConfig}
	modelConfig := &v1alpha3.ModelConfig{}
	if err := s.kubeClient.Get(ctx, modelConfigRef, modelConfig); err != nil {
		return view, serviceerrors.NewInternal("Failed to get ModelConfig", err)
	}
	view.ModelProvider = modelConfig.Spec.Provider
	view.Model = modelConfig.Spec.Model
	view.ModelConfigRef = types.NamespacedName{Namespace: modelConfig.Namespace, Name: modelConfig.Name}
	return view, nil
}

func (s *Service) harnessView(ctx context.Context, harness *v1alpha3.AgentHarness) View {
	ref := types.NamespacedName{Namespace: harness.Namespace, Name: harness.Name}
	view := View{
		Ref:      ref,
		Kind:     KindAgentHarness,
		Resource: harness,
		ID:       utils.ConvertToPythonIdentifier(utils.GetObjectRef(harness)),
		Harness: &HarnessDetails{
			Backend: harness.Spec.Backend,
		},
	}
	for _, condition := range harness.Status.Conditions {
		if condition.Type == v1alpha3.AgentHarnessConditionTypeReady && condition.Status == metav1.ConditionTrue {
			view.Ready = true
		}
		if condition.Type == v1alpha3.AgentHarnessConditionTypeAccepted && condition.Status == metav1.ConditionTrue {
			view.Accepted = true
		}
	}
	if harness.Status.BackendRef != nil {
		view.Harness.BackendRefID = harness.Status.BackendRef.ID
		view.Harness.ActorID = harness.Status.BackendRef.ID
	}
	if harness.Status.Connection != nil {
		view.Harness.Endpoint = harness.Status.Connection.Endpoint
	}

	modelConfigName := strings.TrimSpace(harness.Spec.ModelConfigRef)
	if modelConfigName == "" {
		return view
	}
	modelConfigRef, err := utils.ParseRefString(modelConfigName, harness.Namespace)
	if err != nil {
		return view
	}
	modelConfig := &v1alpha3.ModelConfig{}
	if err := s.kubeClient.Get(ctx, modelConfigRef, modelConfig); err != nil {
		return view
	}
	view.ModelProvider = modelConfig.Spec.Provider
	view.Model = modelConfig.Spec.Model
	view.ModelConfigRef = types.NamespacedName{Namespace: modelConfig.Namespace, Name: modelConfig.Name}
	return view
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
