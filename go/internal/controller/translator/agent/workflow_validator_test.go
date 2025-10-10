package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// T011: Reconcile Validation Test - Circular Dependency Detection (Sequential)
func TestWorkflowValidator_CircularDependency_Sequential(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	tests := []struct {
		name      string
		agents    []client.Object
		targetRef v1alpha2.SubAgentReference
		wantErr   bool
		errMsg    string
	}{
		{
			name: "no circular dependency - simple chain",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{
										{Name: "agent-b"},
									},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-b", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
			},
			targetRef: v1alpha2.SubAgentReference{Name: "agent-a"},
			wantErr:   false,
		},
		{
			name: "circular dependency - A -> B -> A",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{
										{Name: "agent-b"},
									},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-b", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{
										{Name: "agent-a"}, // Creates cycle
									},
								},
							},
						},
					},
				},
			},
			targetRef: v1alpha2.SubAgentReference{Name: "agent-a"},
			wantErr:   true,
			errMsg:    "circular dependency detected",
		},
		{
			name: "circular dependency - A -> B -> C -> A",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "agent-b"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-b", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "agent-c"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-c", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "agent-a"}}, // Creates cycle
								},
							},
						},
					},
				},
			},
			targetRef: v1alpha2.SubAgentReference{Name: "agent-a"},
			wantErr:   true,
			errMsg:    "circular dependency detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.agents...).
				Build()

			validator := NewWorkflowValidator(fakeClient)
			err := validator.ValidateCircularDependency(context.Background(), tt.targetRef, "default")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// T012: Reconcile Validation Test - Circular Dependency Detection (Parallel)
func TestWorkflowValidator_CircularDependency_Parallel(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	agents := []client.Object{
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Workflow,
				Workflow: &v1alpha2.WorkflowAgentSpec{
					Parallel: &v1alpha2.ParallelAgentSpec{
						BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
							SubAgents: []v1alpha2.SubAgentReference{
								{Name: "agent-b"},
								{Name: "agent-c"},
							},
						},
					},
				},
			},
		},
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-b", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Workflow,
				Workflow: &v1alpha2.WorkflowAgentSpec{
					Sequential: &v1alpha2.SequentialAgentSpec{
						BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
							SubAgents: []v1alpha2.SubAgentReference{{Name: "agent-a"}}, // Cycle
						},
					},
				},
			},
		},
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-c", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agents...).
		Build()

	validator := NewWorkflowValidator(fakeClient)
	err := validator.ValidateCircularDependency(context.Background(), v1alpha2.SubAgentReference{Name: "agent-a"}, "default")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

// T013: Reconcile Validation Test - Loop Agent Allows Self-Reference
func TestWorkflowValidator_LoopAgent_AllowsSelfReference(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	agents := []client.Object{
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "self-improving", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Workflow,
				Workflow: &v1alpha2.WorkflowAgentSpec{
					Loop: &v1alpha2.LoopAgentSpec{
						BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
							SubAgents: []v1alpha2.SubAgentReference{
								{Name: "self-improving"}, // Self-reference OK for loop
								{Name: "helper"},         // Need at least 2 sub-agents
							},
						},
						MaxIterations: 3,
					},
				},
			},
		},
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "default"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agents...).
		Build()

	validator := NewWorkflowValidator(fakeClient)

	// Loop agents should be exempt from circular dependency check
	err := validator.ValidateCircularDependency(context.Background(), v1alpha2.SubAgentReference{Name: "self-improving"}, "default")

	// Should NOT error for loop agents (circular is intentional)
	assert.NoError(t, err, "LoopAgent should allow circular references")
}

// T014: Reconcile Validation Test - Sub-Agent Reference Resolution
func TestWorkflowValidator_SubAgentReferenceResolution(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	tests := []struct {
		name      string
		agents    []client.Object
		ref       v1alpha2.SubAgentReference
		namespace string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid - agent exists and ready",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
					Status: v1alpha2.AgentStatus{
						Conditions: []metav1.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			ref:       v1alpha2.SubAgentReference{Name: "agent-a"},
			namespace: "default",
			wantErr:   false,
		},
		{
			name:      "invalid - agent does not exist",
			agents:    []client.Object{},
			ref:       v1alpha2.SubAgentReference{Name: "missing-agent"},
			namespace: "default",
			wantErr:   true,
			errMsg:    "not found",
		},
		{
			name: "valid - cross-namespace reference",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "other-ns"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
					Status: v1alpha2.AgentStatus{
						Conditions: []metav1.Condition{
							{Type: "Ready", Status: metav1.ConditionTrue},
						},
					},
				},
			},
			ref:       v1alpha2.SubAgentReference{Name: "agent-a", Namespace: "other-ns"},
			namespace: "default",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.agents...).
				Build()

			validator := NewWorkflowValidator(fakeClient)
			err := validator.ResolveSubAgentReference(context.Background(), tt.ref, tt.namespace)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// T015: Reconcile Validation Test - Nesting Depth Limit
func TestWorkflowValidator_NestingDepthLimit(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)

	tests := []struct {
		name    string
		agents  []client.Object
		ref     v1alpha2.SubAgentReference
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid - depth 1",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "leaf"}, {Name: "leaf2"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "leaf2", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
			},
			ref:     v1alpha2.SubAgentReference{Name: "agent-a"},
			wantErr: false,
		},
		{
			name: "valid - depth 3 (max allowed)",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-1", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "level-2"}, {Name: "leaf"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-2", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "level-3"}, {Name: "leaf"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-3", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
			},
			ref:     v1alpha2.SubAgentReference{Name: "level-1"},
			wantErr: false,
		},
		{
			name: "invalid - depth 4 (exceeds limit)",
			agents: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-1", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "level-2"}, {Name: "leaf"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-2", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "level-3"}, {Name: "leaf"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-3", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Workflow,
						Workflow: &v1alpha2.WorkflowAgentSpec{
							Sequential: &v1alpha2.SequentialAgentSpec{
								BaseWorkflowSpec: v1alpha2.BaseWorkflowSpec{
									SubAgents: []v1alpha2.SubAgentReference{{Name: "level-4"}, {Name: "leaf"}},
								},
							},
						},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "level-4", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "default"},
					Spec: v1alpha2.AgentSpec{
						Type:        v1alpha2.AgentType_Declarative,
						Declarative: &v1alpha2.DeclarativeAgentSpec{},
					},
				},
			},
			ref:     v1alpha2.SubAgentReference{Name: "level-1"},
			wantErr: true,
			errMsg:  "nesting depth exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.agents...).
				Build()

			validator := NewWorkflowValidator(fakeClient)
			err := validator.ValidateNestingDepth(context.Background(), tt.ref, "default", 3)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// T016: Reconcile Validation Test - Timeout Format Validation
func TestWorkflowValidator_TimeoutFormat(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid - 5m",
			timeout: "5m",
			wantErr: false,
		},
		{
			name:    "valid - 300s",
			timeout: "300s",
			wantErr: false,
		},
		{
			name:    "valid - 30m",
			timeout: "30m",
			wantErr: false,
		},
		{
			name:    "valid - 1h",
			timeout: "1h",
			wantErr: false,
		},
		{
			name:    "valid - 1s (minimum)",
			timeout: "1s",
			wantErr: false,
		},
		{
			name:    "invalid - 500ms (too short)",
			timeout: "500ms",
			wantErr: true,
			errMsg:  "timeout too short",
		},
		{
			name:    "invalid - no unit",
			timeout: "5",
			wantErr: true,
			errMsg:  "invalid timeout format",
		},
		{
			name:    "invalid - wrong unit",
			timeout: "5minutes",
			wantErr: true,
			errMsg:  "invalid timeout format",
		},
		{
			name:    "invalid - too short",
			timeout: "100ms",
			wantErr: true,
			errMsg:  "timeout too short",
		},
		{
			name:    "invalid - too long",
			timeout: "2h",
			wantErr: true,
			errMsg:  "timeout too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewWorkflowValidator(nil)
			err := validator.ValidateTimeout(tt.timeout)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
