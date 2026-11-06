// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// TestEgressPolicyCatalog runs the transport against the real CRD schema. The
// normal Go test job supplies KUBEBUILDER_ASSETS.
func TestEgressPolicyCatalog(t *testing.T) {
	fixture := newEgressPolicyFixture(t)

	t.Run("Given a new policy, when a caller creates, reads, updates and deletes it, assert that each response carries the stored CR", func(t *testing.T) {
		ref := &apiv1.ResourceReference{Namespace: "team", Name: "round-trip"}
		policy := testEgressPolicy("https://api.example.com")

		created, err := fixture.client.CreateEgressPolicy(fixture.ctx, &apiv1.CreateEgressPolicyRequest{Ref: ref, Resource: structured(t, policy, v1alpha3.EgressPolicyKind)})
		require.NoError(t, err)
		require.True(t, proto.Equal(ref, created.EgressPolicy.Ref))
		require.Equal(t, policy.Spec, fixture.decode(t, created.EgressPolicy).Spec)
		require.NotEmpty(t, fixture.decode(t, created.EgressPolicy).UID)

		got, err := fixture.client.GetEgressPolicy(fixture.ctx, &apiv1.GetEgressPolicyRequest{Ref: ref})
		require.NoError(t, err)
		require.Equal(t, policy.Spec, fixture.decode(t, got.EgressPolicy).Spec)

		for _, namespace := range []string{"team", ""} {
			listed, err := fixture.client.ListEgressPolicies(fixture.ctx, &apiv1.ListEgressPoliciesRequest{Namespace: namespace})
			require.NoError(t, err)
			require.Contains(t, fixture.names(listed), "team/round-trip")
		}

		edited := testEgressPolicy("https://api.example.com", "https://*.example.org")
		edited.Labels = map[string]string{"team": "platform"}
		updated, err := fixture.client.UpdateEgressPolicy(fixture.ctx, &apiv1.UpdateEgressPolicyRequest{Ref: ref, Resource: structured(t, edited, v1alpha3.EgressPolicyKind)})
		require.NoError(t, err)
		stored := fixture.decode(t, updated.EgressPolicy)
		require.Equal(t, edited.Spec, stored.Spec)
		require.Equal(t, map[string]string{"team": "platform"}, stored.Labels)

		_, err = fixture.client.DeleteEgressPolicy(fixture.ctx, &apiv1.DeleteEgressPolicyRequest{Ref: ref})
		require.NoError(t, err)
		_, err = fixture.client.GetEgressPolicy(fixture.ctx, &apiv1.GetEgressPolicyRequest{Ref: ref})
		require.Equal(t, codes.NotFound, status.Code(err), "%v", err)
		_, err = fixture.client.DeleteEgressPolicy(fixture.ctx, &apiv1.DeleteEgressPolicyRequest{Ref: ref})
		require.Equal(t, codes.NotFound, status.Code(err), "%v", err)
	})

	t.Run("Given an existing policy, when a caller creates it again, assert that the request is rejected as already existing", func(t *testing.T) {
		request := &apiv1.CreateEgressPolicyRequest{
			Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "duplicate"},
			Resource: structured(t, testEgressPolicy("https://api.example.com"), v1alpha3.EgressPolicyKind),
		}
		_, err := fixture.client.CreateEgressPolicy(fixture.ctx, request)
		require.NoError(t, err)
		_, err = fixture.client.CreateEgressPolicy(fixture.ctx, request)
		require.Equal(t, codes.AlreadyExists, status.Code(err), "%v", err)
	})

	t.Run("Given no policy, when a caller updates it, assert that the request is rejected as not found", func(t *testing.T) {
		_, err := fixture.client.UpdateEgressPolicy(fixture.ctx, &apiv1.UpdateEgressPolicyRequest{
			Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "missing"},
			Resource: structured(t, testEgressPolicy("https://api.example.com"), v1alpha3.EgressPolicyKind),
		})
		require.Equal(t, codes.NotFound, status.Code(err), "%v", err)
	})

	t.Run("Given a policy changed since it was read, when a caller updates it with the stale version, assert that the write is aborted and the stored spec is unchanged", func(t *testing.T) {
		ref := &apiv1.ResourceReference{Namespace: "team", Name: "stale"}
		created, err := fixture.client.CreateEgressPolicy(fixture.ctx, &apiv1.CreateEgressPolicyRequest{Ref: ref, Resource: structured(t, testEgressPolicy("https://a.example.com"), v1alpha3.EgressPolicyKind)})
		require.NoError(t, err)
		staleVersion := fixture.decode(t, created.EgressPolicy).ResourceVersion
		_, err = fixture.client.UpdateEgressPolicy(fixture.ctx, &apiv1.UpdateEgressPolicyRequest{Ref: ref, Resource: structured(t, testEgressPolicy("https://b.example.com"), v1alpha3.EgressPolicyKind)})
		require.NoError(t, err)

		stale := testEgressPolicy("https://c.example.com")
		stale.ResourceVersion = staleVersion
		_, err = fixture.client.UpdateEgressPolicy(fixture.ctx, &apiv1.UpdateEgressPolicyRequest{Ref: ref, Resource: structured(t, stale, v1alpha3.EgressPolicyKind)})
		require.Equal(t, codes.Aborted, status.Code(err), "%v", err)

		got, err := fixture.client.GetEgressPolicy(fixture.ctx, &apiv1.GetEgressPolicyRequest{Ref: ref})
		require.NoError(t, err)
		require.Equal(t, "https://b.example.com", fixture.decode(t, got.EgressPolicy).Spec.Destinations[0].Origin)
	})

	for _, scenario := range []struct {
		name    string
		mutate  func(*testing.T, *apiv1.CreateEgressPolicyRequest)
		message string
	}{
		{name: "a missing ref", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) { r.Ref = nil }},
		{name: "a missing resource", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) { r.Resource = nil }},
		{name: "an invalid namespace", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) { r.Ref.Namespace = "INVALID" }},
		{name: "the wrong kind", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) { r.Resource.Kind = "Harness" }},
		{name: "a conflicting name", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) {
			r.Resource.Value.Fields["metadata"] = structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{"name": structpb.NewStringValue("other")}})
		}},
		{name: "authentication on a wildcard origin", message: "authentication requires an exact HTTP(S) origin", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) {
			policy := testEgressPolicy("https://*.example.com")
			policy.Spec.Destinations[0].Authentication = testSecretAuthentication("Authorization")
			r.Resource = structured(t, policy, v1alpha3.EgressPolicyKind)
		}},
		{name: "a blocked credential header", message: "this header cannot carry an egress credential", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) {
			policy := testEgressPolicy("https://api.example.com")
			policy.Spec.Destinations[0].Authentication = testSecretAuthentication("Cookie")
			r.Resource = structured(t, policy, v1alpha3.EgressPolicyKind)
		}},
		{name: "more than 64 destinations", message: "must have at most 64 items", mutate: func(t *testing.T, r *apiv1.CreateEgressPolicyRequest) {
			origins := make([]string, 65)
			for i := range origins {
				origins[i] = fmt.Sprintf("https://host%d.example.com", i)
			}
			r.Resource = structured(t, testEgressPolicy(origins...), v1alpha3.EgressPolicyKind)
		}},
	} {
		t.Run("Given "+scenario.name+", when a caller creates a policy, assert that the request is rejected as invalid", func(t *testing.T) {
			request := &apiv1.CreateEgressPolicyRequest{
				Ref:      &apiv1.ResourceReference{Namespace: "team", Name: "invalid"},
				Resource: structured(t, testEgressPolicy("https://api.example.com"), v1alpha3.EgressPolicyKind),
			}
			scenario.mutate(t, request)
			_, err := fixture.client.CreateEgressPolicy(fixture.ctx, request)
			require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", err)
			require.Contains(t, status.Convert(err).Message(), scenario.message)
		})
	}

	t.Run("Given an existing policy, when a caller updates it with a blocked credential header, assert that the API server message is returned", func(t *testing.T) {
		ref := &apiv1.ResourceReference{Namespace: "team", Name: "invalid-update"}
		_, err := fixture.client.CreateEgressPolicy(fixture.ctx, &apiv1.CreateEgressPolicyRequest{Ref: ref, Resource: structured(t, testEgressPolicy("https://api.example.com"), v1alpha3.EgressPolicyKind)})
		require.NoError(t, err)
		invalid := testEgressPolicy("https://api.example.com")
		invalid.Spec.Destinations[0].Authentication = testSecretAuthentication("Host")
		_, err = fixture.client.UpdateEgressPolicy(fixture.ctx, &apiv1.UpdateEgressPolicyRequest{Ref: ref, Resource: structured(t, invalid, v1alpha3.EgressPolicyKind)})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", err)
		require.Contains(t, status.Convert(err).Message(), "this header cannot carry an egress credential")
	})
}

func TestEgressPolicyMethodPolicies(t *testing.T) {
	policies := DefaultMethodPolicies()
	require.Equal(t, auth.AccessRead, policies[apiv1.EgressPolicyService_ListEgressPolicies_FullMethodName])
	require.Equal(t, auth.AccessRead, policies[apiv1.EgressPolicyService_GetEgressPolicy_FullMethodName])
	require.Equal(t, auth.AccessCreate, policies[apiv1.EgressPolicyService_CreateEgressPolicy_FullMethodName])
	require.Equal(t, auth.AccessUpdate, policies[apiv1.EgressPolicyService_UpdateEgressPolicy_FullMethodName])
	require.Equal(t, auth.AccessDelete, policies[apiv1.EgressPolicyService_DeleteEgressPolicy_FullMethodName])
}

type egressPolicyFixture struct {
	client apiv1.EgressPolicyServiceClient
	ctx    context.Context
}

func newEgressPolicyFixture(t *testing.T) *egressPolicyFixture {
	t.Helper()
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true,
	}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kubeClient, err := ctrlclient.New(config, ctrlclient.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, kubeClient.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team"}}))
	return &egressPolicyFixture{
		client: apiv1.NewEgressPolicyServiceClient(newConfigurationConnection(t, kubeClient)),
		ctx:    metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "policy-user")),
	}
}

func (f *egressPolicyFixture) decode(t *testing.T, policy *apiv1.EgressPolicy) *v1alpha3.EgressPolicy {
	t.Helper()
	stored := &v1alpha3.EgressPolicy{}
	require.NoError(t, structuredobject.ToGo(policy.Resource, v1alpha3.EgressPolicyKind, stored, DefaultMaxMessageSize))
	return stored
}

func (f *egressPolicyFixture) names(response *apiv1.ListEgressPoliciesResponse) []string {
	names := make([]string, 0, len(response.EgressPolicies))
	for _, policy := range response.EgressPolicies {
		names = append(names, strings.Join([]string{policy.Ref.Namespace, policy.Ref.Name}, "/"))
	}
	return names
}

func testEgressPolicy(origins ...string) *v1alpha3.EgressPolicy {
	destinations := make([]v1alpha3.EgressDestination, 0, len(origins))
	for _, origin := range origins {
		destinations = append(destinations, v1alpha3.EgressDestination{Origin: origin})
	}
	return &v1alpha3.EgressPolicy{Spec: v1alpha3.EgressPolicySpec{Destinations: destinations}}
}

func testSecretAuthentication(header string) *v1alpha3.EgressAuthentication {
	return &v1alpha3.EgressAuthentication{
		Header:       header,
		SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "token"},
	}
}
