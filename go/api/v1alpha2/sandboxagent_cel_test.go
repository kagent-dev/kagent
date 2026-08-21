/*
Copyright 2025.

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

package v1alpha2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestSandboxAgentCELValidation pins the SandboxAgentSpec CEL rules against a
// real kube-apiserver loaded with the shipped CRDs, so admission rejects
// unsupported configuration instead of the controller discovering it at
// reconcile time. ValidateSubstrateSandboxAgentSpec mirrors these rules in Go
// for objects that predate them.
func TestSandboxAgentCELValidation(t *testing.T) {
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
	const ns = "sandbox-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))

	cmd := "/app"
	cases := []struct {
		name       string
		build      func() ctrl_client.Object
		wantReject string // substring in admission error; empty means accept
	}{
		{
			name: "declarative nodeSelector rejected",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-decl-nodeselector", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: AgentSpec{
							Type: AgentType_Declarative,
							Declarative: &DeclarativeAgentSpec{
								Runtime: DeclarativeRuntime_Go,
								Deployment: &DeclarativeDeploymentSpec{
									SharedDeploymentSpec: SharedDeploymentSpec{
										NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"},
									},
								},
							},
						},
					},
				}
			},
			wantReject: "deployment.nodeSelector is not supported for sandbox agents",
		},
		{
			name: "byo nodeSelector rejected",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-byo-nodeselector", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: AgentSpec{
							Type: AgentType_BYO,
							BYO: &BYOAgentSpec{Deployment: &ByoDeploymentSpec{
								Image: "example/agent:latest",
								Cmd:   &cmd,
								SharedDeploymentSpec: SharedDeploymentSpec{
									NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"},
								},
							}},
						},
					},
				}
			},
			wantReject: "deployment.nodeSelector is not supported for sandbox agents",
		},
		{
			name: "declarative empty nodeSelector accepted",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-decl-empty-nodeselector", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: AgentSpec{
							Type: AgentType_Declarative,
							Declarative: &DeclarativeAgentSpec{
								Runtime: DeclarativeRuntime_Go,
								Deployment: &DeclarativeDeploymentSpec{
									SharedDeploymentSpec: SharedDeploymentSpec{
										NodeSelector: map[string]string{},
									},
								},
							},
						},
					},
				}
			},
		},
		{
			name: "declarative without nodeSelector accepted",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-decl-no-nodeselector", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: AgentSpec{
							Type: AgentType_Declarative,
							Declarative: &DeclarativeAgentSpec{
								Runtime: DeclarativeRuntime_Go,
							},
						},
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := cl.Create(ctx, c.build())
			if c.wantReject == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.wantReject)
		})
	}
}
