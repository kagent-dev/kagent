package v1alpha3

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestAgentManifestsCarryNoVolumeSource checks against the shipped CRDs that an
// Agent or AgentTemplate naming a volume source is refused: a Session names the
// workspace volume it works in on create, and nothing else does.
func TestAgentManifestsCarryNoVolumeSource(t *testing.T) {
	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDir(t),
		CRDDirectoryPaths:     []string{crdBasesDir(t)},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = testEnv.Stop() })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, AddToScheme(scheme))
	cl, err := ctrl_client.New(cfg, ctrl_client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	const ns = "no-volume-source"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))

	volumeSource := map[string]any{
		"volume": map[string]any{"csiDriver": "efs.csi.aws.com", "volumeHandle": "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
		"mounts": []any{map[string]any{"subPath": "sessions/${SESSION_ID}", "mountPath": "/workspace"}},
	}
	for _, tc := range []struct {
		name string
		kind string
		// spec is the manifest without the field; place puts the field in.
		spec  func() map[string]any
		place func(spec map[string]any)
	}{
		{
			name: "AgentTemplate", kind: "AgentTemplate",
			spec:  func() map[string]any { return map[string]any{"modelConfig": map[string]any{"name": "model"}} },
			place: func(spec map[string]any) { spec["volumeSource"] = volumeSource },
		},
		{
			name: "Agent", kind: "Agent",
			spec: func() map[string]any {
				return map[string]any{"templateRef": map[string]any{"name": "assistant"}, "harnessRef": map[string]any{"name": "kagent"}}
			},
			place: func(spec map[string]any) { spec["volumeSource"] = volumeSource },
		},
		{
			name: "Agent inline template", kind: "Agent",
			spec: func() map[string]any {
				return map[string]any{"template": map[string]any{"modelConfig": map[string]any{"name": "model"}}, "harnessRef": map[string]any{"name": "kagent"}}
			},
			place: func(spec map[string]any) { spec["template"].(map[string]any)["volumeSource"] = volumeSource },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slug := strings.ReplaceAll(strings.ToLower(tc.name), " ", "-")
			manifest := func(name string, spec map[string]any) *unstructured.Unstructured {
				return &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": GroupVersion.String(), "kind": tc.kind,
					"metadata": map[string]any{"name": slug + "-" + name, "namespace": ns},
					"spec":     spec,
				}}
			}
			withVolume := tc.spec()
			tc.place(withVolume)
			err := cl.Create(ctx, manifest("with-volume", withVolume), &ctrl_client.CreateOptions{FieldValidation: metav1.FieldValidationStrict})
			require.ErrorContains(t, err, "unknown field")
			require.ErrorContains(t, err, "volumeSource")
			// The same manifest without the field is what the schema admits, so
			// the refusal above is the field's and not the fixture's.
			require.NoError(t, cl.Create(ctx, manifest("without-volume", tc.spec()), &ctrl_client.CreateOptions{FieldValidation: metav1.FieldValidationStrict}))
		})
	}
}
