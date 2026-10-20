package grpcserver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	promptservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type resourceAuthorizer struct{ denySummary bool }

func (a resourceAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if resource.Type == "SystemSummary" && !a.denySummary {
		return nil
	}
	if resource.Name != "allowed" || (resource.Type == "ModelProviderConfig" && verb == auth.VerbUpdate) {
		return errors.New("denied")
	}
	return nil
}

func (resourceAuthorizer) Scope(_ context.Context, _ auth.Principal, _ auth.Verb, _ string) (apiauthorization.AuthorizationScope, error) {
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"allowed"}}}}}}, nil
}

type deniedProviderRefresher struct{ calls atomic.Int32 }

func (r *deniedProviderRefresher) RefreshModelProviderConfigModels(context.Context, string, string) ([]string, error) {
	r.calls.Add(1)
	return []string{"unexpected"}, nil
}

func TestKubernetesResourceScopesThroughGeneratedClients(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed", Labels: map[string]string{"kagent.dev/prompt-library": "true"}}, Data: map[string]string{"visible": "prompt"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden", Labels: map[string]string{"kagent.dev/prompt-library": "true"}}, Data: map[string]string{"private": "prompt"}},
		&v1alpha3.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}, Status: v1alpha3.ModelProviderConfigStatus{Conditions: []metav1.Condition{{Type: v1alpha3.ModelProviderConfigConditionTypeReady, Status: metav1.ConditionTrue}}, DiscoveredModels: []string{"cached"}}},
		&v1alpha3.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden"}, Status: v1alpha3.ModelProviderConfigStatus{Conditions: []metav1.Condition{{Type: v1alpha3.ModelProviderConfigConditionTypeReady, Status: metav1.ConditionTrue}}}},
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}},
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden"}},
	).Build()
	authorizer := resourceAuthorizer{}
	refresher := &deniedProviderRefresher{}
	connection := newConfigurationConnection(t, kube, func(config *Config) {
		config.PromptTemplateService = promptservice.NewService(kube, authorizer)
		config.ModelService = modelservice.NewService(kube, authorizer, "team", modelservice.WithProviderModelRefresher(refresher))
		config.SystemService = systemservice.NewService(kube, []string{"team"}, authorizer, emptySystemATEClient{})
	})
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "reader"))
	prompts := apiv1.NewPromptTemplateServiceClient(connection)
	libraries, err := prompts.ListPromptTemplates(ctx, &apiv1.ListPromptTemplatesRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Len(t, libraries.GetPromptTemplates(), 1)
	require.Equal(t, "allowed", libraries.GetPromptTemplates()[0].GetRef().GetName())
	_, err = prompts.GetPromptTemplate(ctx, &apiv1.GetPromptTemplateRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "hidden"}})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = prompts.CreatePromptTemplate(ctx, &apiv1.CreatePromptTemplateRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "denied"}, Data: map[string]string{"key": "value"}})
	assertGRPCCode(t, err, codes.PermissionDenied)

	models := apiv1.NewModelServiceClient(connection)
	providers, err := models.ListConfiguredProviders(ctx, &apiv1.ListConfiguredProvidersRequest{})
	require.NoError(t, err)
	require.Len(t, providers.GetProviders(), 1)
	require.Equal(t, "allowed", providers.GetProviders()[0].GetName())
	cached, err := models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: "allowed"})
	require.NoError(t, err)
	require.Equal(t, []string{"cached"}, cached.GetModels())
	_, err = models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: "hidden"})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: "allowed", Refresh: true})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Zero(t, refresher.calls.Load())

	summary, err := apiv1.NewSystemServiceClient(connection).GetSubstrateSummary(ctx, &apiv1.GetSubstrateSummaryRequest{Namespace: "team"})
	require.NoError(t, err)
	require.Len(t, summary.GetWorkerPools(), 1)
	require.Equal(t, "allowed", summary.GetWorkerPools()[0].GetRef().GetName())
	require.Zero(t, summary.GetActorCount())
	require.Empty(t, summary.GetActorTemplates())
}

func TestProviderModelRefreshDeniedThroughShareRPC(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.ModelProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "provider"},
			Status:     v1alpha3.ModelProviderConfigStatus{DiscoveredModels: []string{"cached"}},
		},
	).Build()
	refresher := &deniedProviderRefresher{}
	connection := newConfigurationConnection(t, kube, func(config *Config) {
		config.ShareStore = &testShareStore{
			sessionShare: &apiv1.SessionShare{SessionId: testSessionID.String()}, ownerUserID: "owner",
		}
		config.ModelService = modelservice.NewService(kube, &auth.NoopAuthorizer{}, "team", modelservice.WithProviderModelRefresher(refresher))
	})
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "visitor", "x-share-token", "share"))
	models := apiv1.NewModelServiceClient(connection)
	cached, err := models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: "provider"})
	require.NoError(t, err)
	require.Equal(t, []string{"cached"}, cached.GetModels())
	_, err = models.ListProviderModels(ctx, &apiv1.ListProviderModelsRequest{ProviderName: "provider", Refresh: true})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Zero(t, refresher.calls.Load())
}

func TestSummaryCapabilityDeniedThroughGeneratedClient(t *testing.T) {
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.SystemService = systemservice.NewService(nil, nil, resourceAuthorizer{denySummary: true}, nil)
	})
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "reader"))
	_, err := apiv1.NewSystemServiceClient(connection).GetSubstrateSummary(ctx, &apiv1.GetSubstrateSummaryRequest{})
	assertGRPCCode(t, err, codes.PermissionDenied)
}

type inventoryRPCPolicy struct{ auth.NoopAuthorizer }

func (inventoryRPCPolicy) Scope(_ context.Context, _ auth.Principal, verb auth.Verb, resource string) (apiauthorization.AuthorizationScope, error) {
	if verb != auth.VerbList {
		return apiauthorization.AuthorizationScope{}, errors.New("unexpected verb")
	}
	var location, value, name string
	switch resource {
	case "SubstrateActor", "SubstrateActorTemplate":
		location, value, name = apiauthorization.AttributeAtespace, "visible", "allowed"
	case "SubstrateWorker":
		location, value, name = apiauthorization.AttributeNamespace, "workers", "worker"
	default:
		return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}, nil
	}
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: location, Operator: apiauthorization.ScopeIn, Values: []string{value}}, {Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{name}}}}}}, nil
}

type inventoryRPCATE struct{}

func (inventoryRPCATE) ListActorsPage(_ context.Context, _ string, _ int32, token string) ([]*ateapipb.Actor, string, error) {
	space, next := "hidden", "second"
	if token != "" {
		space, next = "visible", ""
	}
	return []*ateapipb.Actor{{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: "allowed"}, Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_RUNNING}}}, next, nil
}
func (inventoryRPCATE) ListWorkersPage(_ context.Context, _ int32, token string) ([]*ateapipb.Worker, string, error) {
	namespace, next := "hidden", "second"
	if token != "" {
		namespace, next = "workers", ""
	}
	return []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "worker"}, WorkerNamespace: namespace, WorkerPod: "different-pod", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 1}}}}, next, nil
}
func (inventoryRPCATE) ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error) {
	return []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Atespace: "visible", Name: "allowed"}, Containers: []*ateapipb.Container{{Env: []*ateapipb.EnvVar{{Name: "SECRET", Value: "hidden"}}}}}, {Metadata: &ateapipb.ResourceMetadata{Atespace: "hidden", Name: "allowed"}}}, nil
}
func TestInventoryScopesAndEmptyPagesThroughGeneratedClient(t *testing.T) {
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.SystemService = systemservice.NewService(nil, nil, inventoryRPCPolicy{}, inventoryRPCATE{})
	})
	client := apiv1.NewSystemServiceClient(connection)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "reader"))
	actors, err := client.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.NoError(t, err)
	require.Empty(t, actors.Actors)
	require.Equal(t, "second", actors.GetPage().GetNextPageToken())
	actors, err = client.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{PageToken: "second"}})
	require.NoError(t, err)
	require.Len(t, actors.Actors, 1)
	require.Equal(t, "visible", actors.Actors[0].GetMetadata().GetAtespace())
	workers, err := client.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	require.Empty(t, workers.Workers)
	require.Equal(t, "second", workers.GetPage().GetNextPageToken())
	workers, err = client.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{PageToken: "second"}})
	require.NoError(t, err)
	require.Len(t, workers.Workers, 1)
	require.Equal(t, "worker", workers.Workers[0].GetMetadata().GetName())
	summary, err := client.GetSubstrateSummary(ctx, &apiv1.GetSubstrateSummaryRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.ActorCount)
	require.EqualValues(t, 1, summary.WorkerCount)
	require.EqualValues(t, 1, summary.RunningActorCount)
	require.EqualValues(t, 1, summary.BusyWorkerCount)
	require.Len(t, summary.ActorTemplates, 1)
	require.Empty(t, summary.ActorTemplates[0].Containers)
	require.Empty(t, summary.WorkerPools)
}
