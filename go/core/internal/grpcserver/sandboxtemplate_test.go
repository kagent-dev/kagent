// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
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
)

// TestSandboxTemplateCatalog exercises the transport and persistence against the
// actual CRD schema. The normal Go test job supplies KUBEBUILDER_ASSETS.
func TestSandboxTemplateCatalog(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../../api/config/crd/bases"}, ErrorIfCRDPathMissing: true,
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
	client := apiv1alpha1.NewSandboxTemplateServiceClient(newConfigurationConnection(t, kubeClient))
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "template-user"))
	resource := &v1alpha3.SandboxTemplate{Spec: v1alpha3.SandboxTemplateSpec{
		Workload: v1alpha3.SandboxTemplateWorkload{Image: testHarnessImage},
		Env:      []v1alpha3.HarnessEnvVar{{Name: "LANG", Value: new("C.UTF-8")}},
		Substrate: v1alpha3.HarnessSubstratePolicy{
			WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
			SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "s3://snapshots"},
		},
	}}
	request := &apiv1alpha1.CreateSandboxTemplateRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "environment"},
		Resource: structured(t, resource, v1alpha3.SandboxTemplateKind),
	}
	created, err := client.CreateSandboxTemplate(ctx, request)
	require.NoError(t, err)
	require.Equal(t, testHarnessImage, created.SandboxTemplate.WorkloadImage)
	require.True(t, proto.Equal(request.Ref, created.SandboxTemplate.Ref))
	require.NotEmpty(t, created.SandboxTemplate.Resource.Value.AsMap()["metadata"].(map[string]any)["uid"])
	_, err = client.CreateSandboxTemplate(ctx, request)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	for _, namespace := range []string{"team", ""} {
		listed, err := client.ListSandboxTemplates(ctx, &apiv1alpha1.ListSandboxTemplatesRequest{Namespace: namespace})
		require.NoError(t, err)
		require.Len(t, listed.SandboxTemplates, 1)
		listedTemplate := listed.SandboxTemplates[0]
		require.True(t, proto.Equal(request.Ref, listedTemplate.Ref))
		require.Equal(t, testHarnessImage, listedTemplate.WorkloadImage)
		stored := &v1alpha3.SandboxTemplate{}
		require.NoError(t, structuredobject.ToGo(listedTemplate.Resource, v1alpha3.SandboxTemplateKind, stored, DefaultMaxMessageSize))
		require.Equal(t, resource.Spec, stored.Spec)
		require.NotEmpty(t, stored.UID)
	}

	for _, tt := range []struct {
		name   string
		mutate func(*apiv1alpha1.CreateSandboxTemplateRequest)
	}{
		{"missing ref", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Ref = nil }},
		{"missing resource", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Resource = nil }},
		{"missing value", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Resource.Value = nil }},
		{"invalid namespace", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Ref.Namespace = "INVALID" }},
		{"wrong kind", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Resource.Kind = "Harness" }},
		{"wrong version", func(r *apiv1alpha1.CreateSandboxTemplateRequest) { r.Resource.ApiVersion = "kagent.dev/v1alpha2" }},
		{"conflicting kind", func(r *apiv1alpha1.CreateSandboxTemplateRequest) {
			r.Resource.Value.Fields["kind"] = structpb.NewStringValue("Harness")
		}},
		{"conflicting namespace", func(r *apiv1alpha1.CreateSandboxTemplateRequest) {
			r.Resource.Value.Fields["metadata"] = structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{"namespace": structpb.NewStringValue("other")}})
		}},
		{"conflicting name", func(r *apiv1alpha1.CreateSandboxTemplateRequest) {
			r.Resource.Value.Fields["metadata"] = structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{"name": structpb.NewStringValue("other")}})
		}},
		{"mutable image rejected by Kubernetes", func(r *apiv1alpha1.CreateSandboxTemplateRequest) {
			r.Resource.Value.Fields["spec"].GetStructValue().Fields["workload"].GetStructValue().Fields["image"] = structpb.NewStringValue("example.com/guest:latest")
		}},
		{"startup is not template configuration", func(r *apiv1alpha1.CreateSandboxTemplateRequest) {
			r.Resource.Value.Fields["spec"].GetStructValue().Fields["workload"].GetStructValue().Fields["command"] = structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{structpb.NewStringValue("/agent")}})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invalid := proto.Clone(request).(*apiv1alpha1.CreateSandboxTemplateRequest)
			invalid.Ref.Name = "invalid"
			tt.mutate(invalid)
			_, err := client.CreateSandboxTemplate(ctx, invalid)
			require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", err)
		})
	}
	_, err = client.DeleteSandboxTemplate(ctx, &apiv1alpha1.DeleteSandboxTemplateRequest{})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = client.ListSandboxTemplates(ctx, &apiv1alpha1.ListSandboxTemplatesRequest{Namespace: "INVALID"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = client.DeleteSandboxTemplate(ctx, &apiv1alpha1.DeleteSandboxTemplateRequest{Ref: request.Ref})
	require.NoError(t, err)
	_, err = client.DeleteSandboxTemplate(ctx, &apiv1alpha1.DeleteSandboxTemplateRequest{Ref: request.Ref})
	require.Equal(t, codes.NotFound, status.Code(err))
	listed, err := client.ListSandboxTemplates(ctx, &apiv1alpha1.ListSandboxTemplatesRequest{})
	require.NoError(t, err)
	require.Empty(t, listed.SandboxTemplates)
}

func TestSandboxTemplateMethodPolicies(t *testing.T) {
	policies := DefaultMethodPolicies()
	require.Equal(t, auth.AccessRead, policies[apiv1alpha1.SandboxTemplateService_ListSandboxTemplates_FullMethodName])
	require.Equal(t, auth.AccessCreate, policies[apiv1alpha1.SandboxTemplateService_CreateSandboxTemplate_FullMethodName])
	require.Equal(t, auth.AccessDelete, policies[apiv1alpha1.SandboxTemplateService_DeleteSandboxTemplate_FullMethodName])
}
