package grpcserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
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
	require.NoError(t, atev1.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed", Labels: map[string]string{"kagent.dev/prompt-library": "true"}}, Data: map[string]string{"visible": "prompt"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden", Labels: map[string]string{"kagent.dev/prompt-library": "true"}}, Data: map[string]string{"private": "prompt"}},
		&v1alpha3.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}, Status: v1alpha3.ModelProviderConfigStatus{Conditions: []metav1.Condition{{Type: v1alpha3.ModelProviderConfigConditionTypeReady, Status: metav1.ConditionTrue}}, DiscoveredModels: []string{"cached"}}},
		&v1alpha3.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden"}, Status: v1alpha3.ModelProviderConfigStatus{Conditions: []metav1.Condition{{Type: v1alpha3.ModelProviderConfigConditionTypeReady, Status: metav1.ConditionTrue}}}},
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}},
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden"}},
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
func TestInventoryScopesFilterBeforePaginationThroughGeneratedClient(t *testing.T) {
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.SystemService = systemservice.NewService(nil, nil, inventoryRPCPolicy{}, inventoryRPCATE{})
	})
	client := apiv1.NewSystemServiceClient(connection)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "reader"))
	actors, err := client.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.NoError(t, err)
	require.Empty(t, actors.GetAteApiError())
	require.Empty(t, actors.GetPage().GetNextPageToken())
	require.Len(t, actors.Actors, 1)
	require.Equal(t, "visible", actors.Actors[0].GetMetadata().GetAtespace())
	workers, err := client.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	require.Empty(t, workers.GetAteApiError())
	require.Empty(t, workers.GetPage().GetNextPageToken())
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

type pagedInventoryPolicy struct{ auth.NoopAuthorizer }

func (pagedInventoryPolicy) Scope(context.Context, auth.Principal, auth.Verb, string) (apiauthorization.AuthorizationScope, error) {
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"b-visible", "d-visible"}}}}}}, nil
}

type pagedInventoryATE struct {
	emptySystemATEClient
	t *testing.T
}

func (p pagedInventoryATE) page(kind, scope, token string) (string, string) {
	p.t.Helper()
	type cursor struct {
		Version int      `json:"v"`
		Kind    string   `json:"kind"`
		Scope   string   `json:"scope"`
		Last    []string `json:"last"`
	}
	after := ""
	if token != "" {
		raw, err := base64.StdEncoding.DecodeString(token)
		require.NoError(p.t, err)
		var c cursor
		require.NoError(p.t, json.Unmarshal(raw, &c))
		require.Equal(p.t, 1, c.Version)
		require.Equal(p.t, kind, c.Kind)
		require.Equal(p.t, scope, c.Scope)
		require.NotEmpty(p.t, c.Last)
		after = c.Last[len(c.Last)-1]
	}
	for _, name := range []string{"a-hidden", "b-visible", "c-hidden", "d-visible", "z-hidden"} {
		if name <= after {
			continue
		}
		next := ""
		if name != "z-hidden" {
			last := []string{name}
			if kind == "actor" && scope == "" {
				last = []string{"team", name}
			}
			raw, err := json.Marshal(cursor{Version: 1, Kind: kind, Scope: scope, Last: last})
			require.NoError(p.t, err)
			next = base64.StdEncoding.EncodeToString(raw)
		}
		return name, next
	}
	return "", ""
}

func (p pagedInventoryATE) ListActorsPage(_ context.Context, scope string, _ int32, token string) ([]*ateapipb.Actor, string, error) {
	name, next := p.page("actor", scope, token)
	if name == "" {
		return nil, "", nil
	}
	return []*ateapipb.Actor{{Metadata: &ateapipb.ResourceMetadata{Atespace: "team", Name: name}, Status: &ateapipb.ActorStatus{AssignedNode: "private-node", WorkerAssignment: &ateapipb.WorkerAssignment{Worker: &ateapipb.ObjectRef{Name: "private-worker"}, WorkerNamespace: "team", WorkerPod: "private-pod", WorkerPodIps: []string{"10.1.2.3"}}}}}, next, nil
}

func (p pagedInventoryATE) ListWorkersPage(_ context.Context, _ int32, token string) ([]*ateapipb.Worker, string, error) {
	name, next := p.page("worker", "", token)
	if name == "" {
		return nil, "", nil
	}
	return []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: name}, WorkerNamespace: "team"}}, next, nil
}

func TestInventoryCursorConfidentialityThroughGeneratedClient(t *testing.T) {
	for _, watched := range [][]string{nil, {"team"}} {
		t.Run(fmt.Sprint(watched), func(t *testing.T) {
			connection := newConfigurationConnection(t, nil, func(config *Config) {
				config.SystemService = systemservice.NewService(nil, watched, pagedInventoryPolicy{}, pagedInventoryATE{t: t})
			})
			client := apiv1.NewSystemServiceClient(connection)
			ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "reader"))
			actorToken, workerToken := "", ""
			for _, name := range []string{"b-visible", "d-visible"} {
				actors, err := client.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 1, PageToken: actorToken}})
				require.NoError(t, err)
				require.Empty(t, actors.GetAteApiError())
				require.Len(t, actors.Actors, 1)
				require.Equal(t, name, actors.Actors[0].GetMetadata().GetName())
				require.Nil(t, actors.Actors[0].GetStatus().GetWorkerAssignment())
				require.Empty(t, actors.Actors[0].GetStatus().GetAssignedNode())
				workers, err := client.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1, PageToken: workerToken}})
				require.NoError(t, err)
				require.Empty(t, workers.GetAteApiError())
				require.Len(t, workers.Workers, 1)
				require.Equal(t, name, workers.Workers[0].GetMetadata().GetName())
				actorToken, workerToken = actors.GetPage().GetNextPageToken(), workers.GetPage().GetNextPageToken()
				for _, token := range []string{actorToken, workerToken} {
					if name == "d-visible" {
						require.Empty(t, token)
						continue
					}
					require.NotEmpty(t, token)
					raw, err := base64.RawURLEncoding.DecodeString(token)
					require.NoError(t, err)
					require.Contains(t, string(raw), name)
					require.NotContains(t, string(raw), "hidden")
				}
			}
		})
	}
}
