/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// A claude Harness a chart applied with spec.claude.limits takes the chart's
// later `claude: {}` once the CRD no longer declares limits. While the CRD
// still declared them, the same server-side apply turned spec.claude into null,
// which the schema refuses, so the CRD must lose the field before or with the
// chart that drops it.
func TestClaudeHarnessAppliedWithLimitsTakesAnEmptyClaudeAfterTheCRDUpgrade(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(crdBasesDir(t), "api.kagent.dev_harnesses.yaml"))
	require.NoError(t, err)
	var current apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(b, &current))
	withLimits := current.DeepCopy()
	for i := range withLimits.Spec.Versions {
		spec := withLimits.Spec.Versions[i].Schema.OpenAPIV3Schema.Properties["spec"]
		claude := spec.Properties["claude"]
		claude.Properties = map[string]apiextensionsv1.JSONSchemaProps{"limits": {
			Type:       "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{"maxTurns": {Type: "integer"}},
		}}
		spec.Properties["claude"] = claude
		withLimits.Spec.Versions[i].Schema.OpenAPIV3Schema.Properties["spec"] = spec
	}

	testEnv := &envtest.Environment{
		BinaryAssetsDirectory: envtestAssetsDir(t),
		CRDs:                  []*apiextensionsv1.CustomResourceDefinition{withLimits},
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = testEnv.Stop() })
	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	c, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	require.NoError(t, err)

	apply := func(claude map[string]any) error {
		harness := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "api.kagent.dev/v1alpha3", "kind": "Harness",
			"metadata": map[string]any{"name": "claude", "namespace": "default"},
			"spec": map[string]any{
				"claude":   claude,
				"workload": map[string]any{"image": "registry.example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				"substrate": map[string]any{
					"workerPoolRef":  map[string]any{"name": "default"},
					"snapshotPolicy": map[string]any{"location": "gs://snapshots/kagent"},
				},
			},
		}}
		return c.Apply(t.Context(), ctrlclient.ApplyConfigurationFromUnstructured(harness), ctrlclient.FieldOwner("helm-controller"), ctrlclient.ForceOwnership)
	}
	require.NoError(t, apply(map[string]any{"limits": map[string]any{"maxTurns": int64(40)}}))

	var live apiextensionsv1.CustomResourceDefinition
	require.NoError(t, c.Get(t.Context(), ctrlclient.ObjectKey{Name: current.Name}, &live))
	current.ResourceVersion = live.ResourceVersion
	require.NoError(t, c.Update(t.Context(), &current))
	require.NoError(t, envtest.WaitForCRDs(cfg, []*apiextensionsv1.CustomResourceDefinition{&current}, envtest.CRDInstallOptions{}))

	require.NoError(t, apply(map[string]any{}))
	got := &unstructured.Unstructured{}
	got.SetAPIVersion("api.kagent.dev/v1alpha3")
	got.SetKind("Harness")
	require.NoError(t, c.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "default", Name: "claude"}, got))
	claude, found, err := unstructured.NestedMap(got.Object, "spec", "claude")
	require.NoError(t, err)
	require.True(t, found, "spec.claude = %#v", got.Object["spec"])
	require.Empty(t, claude)
}
