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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestDeploymentStrategyCELValidation pins the deploymentStrategy CEL rules
// against a real kube-apiserver loaded with the shipped CRDs:
//   - the field-level rule rejecting type Recreate combined with a
//     rollingUpdate block (the apps/v1 API server rejects the same
//     combination at Deployment-apply time; this surfaces it at admission)
//   - the SandboxAgentSpec rule rejecting deploymentStrategy entirely,
//     since a SandboxAgent's workload is an ActorTemplate, not a Deployment
func TestDeploymentStrategyCELValidation(t *testing.T) {
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
	const ns = "depstrategy-cel"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))

	declarativeSpec := func(strategy *appsv1.DeploymentStrategy) AgentSpec {
		return AgentSpec{
			Type: AgentType_Declarative,
			Declarative: &DeclarativeAgentSpec{
				Deployment: &DeclarativeDeploymentSpec{
					SharedDeploymentSpec: SharedDeploymentSpec{
						DeploymentStrategy: strategy,
					},
				},
			},
		}
	}

	maxUnavailable := intstr.FromInt32(0)
	rollingUpdate := &appsv1.RollingUpdateDeployment{MaxUnavailable: &maxUnavailable}

	cases := []struct {
		name       string
		build      func() ctrl_client.Object
		wantReject string // substring in admission error; empty means accept
	}{
		{
			name: "Agent: Recreate with rollingUpdate rejected",
			build: func() ctrl_client.Object {
				return &Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "ag-recreate-ru", Namespace: ns},
					Spec: declarativeSpec(&appsv1.DeploymentStrategy{
						Type:          appsv1.RecreateDeploymentStrategyType,
						RollingUpdate: rollingUpdate,
					}),
				}
			},
			wantReject: "rollingUpdate may not be specified when strategy type is Recreate",
		},
		{
			name: "Agent: Recreate without rollingUpdate accepted",
			build: func() ctrl_client.Object {
				return &Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "ag-recreate", Namespace: ns},
					Spec: declarativeSpec(&appsv1.DeploymentStrategy{
						Type: appsv1.RecreateDeploymentStrategyType,
					}),
				}
			},
		},
		{
			name: "Agent: RollingUpdate with rollingUpdate accepted",
			build: func() ctrl_client.Object {
				return &Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "ag-rollingupdate", Namespace: ns},
					Spec: declarativeSpec(&appsv1.DeploymentStrategy{
						Type:          appsv1.RollingUpdateDeploymentStrategyType,
						RollingUpdate: rollingUpdate,
					}),
				}
			},
		},
		{
			name: "Agent: type omitted with rollingUpdate accepted",
			build: func() ctrl_client.Object {
				return &Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "ag-no-type-ru", Namespace: ns},
					Spec: declarativeSpec(&appsv1.DeploymentStrategy{
						RollingUpdate: rollingUpdate,
					}),
				}
			},
		},
		{
			name: "Agent: invalid strategy type rejected",
			build: func() ctrl_client.Object {
				return &Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "ag-invalid-type", Namespace: ns},
					Spec: declarativeSpec(&appsv1.DeploymentStrategy{
						Type: appsv1.DeploymentStrategyType("Foo"),
					}),
				}
			},
			wantReject: "strategy type must be RollingUpdate or Recreate",
		},
		{
			name: "SandboxAgent: declarative deploymentStrategy rejected",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-decl-strategy", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: declarativeSpec(&appsv1.DeploymentStrategy{
							Type: appsv1.RecreateDeploymentStrategyType,
						}),
					},
				}
			},
			wantReject: "deploymentStrategy is not supported for sandbox agents",
		},
		{
			name: "SandboxAgent: byo deploymentStrategy rejected",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-byo-strategy", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: AgentSpec{
							Type: AgentType_BYO,
							BYO: &BYOAgentSpec{
								Deployment: &ByoDeploymentSpec{
									Image: "example.com/agent:latest",
									SharedDeploymentSpec: SharedDeploymentSpec{
										DeploymentStrategy: &appsv1.DeploymentStrategy{
											Type: appsv1.RecreateDeploymentStrategyType,
										},
									},
								},
							},
						},
					},
				}
			},
			wantReject: "deploymentStrategy is not supported for sandbox agents",
		},
		{
			name: "SandboxAgent: no deploymentStrategy accepted",
			build: func() ctrl_client.Object {
				return &SandboxAgent{
					ObjectMeta: metav1.ObjectMeta{Name: "sa-no-strategy", Namespace: ns},
					Spec: SandboxAgentSpec{
						AgentSpec: declarativeSpec(nil),
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
