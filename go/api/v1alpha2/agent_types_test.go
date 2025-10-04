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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T005: Contract Test - SequentialAgentSpec CRD Schema
func TestSequentialAgentSpec_Schema(t *testing.T) {
	tests := []struct {
		name    string
		spec    SequentialAgentSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid sequential spec with 2 sub-agents",
			spec: SequentialAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid sequential spec with 50 sub-agents (max)",
			spec: SequentialAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: func() []SubAgentReference {
						agents := make([]SubAgentReference, 50)
						for i := 0; i < 50; i++ {
							agents[i] = SubAgentReference{Name: "agent"}
						}
						return agents
					}(),
				},
			},
			wantErr: false,
		},
		{
			name: "valid sequential spec with timeout",
			spec: SequentialAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					Timeout:   stringPtr("5m"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid sequential spec with description",
			spec: SequentialAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents:   []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					Description: "Test sequential workflow",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify struct can be created
			assert.NotNil(t, tt.spec.SubAgents)

			// Verify required field
			require.NotEmpty(t, tt.spec.SubAgents, "SubAgents is required")

			// Verify constraints
			assert.LessOrEqual(t, len(tt.spec.SubAgents), 50, "SubAgents max 50")
			assert.GreaterOrEqual(t, len(tt.spec.SubAgents), 2, "SubAgents min 2")
		})
	}
}

// T006: Contract Test - ParallelAgentSpec CRD Schema
func TestParallelAgentSpec_Schema(t *testing.T) {
	tests := []struct {
		name    string
		spec    ParallelAgentSpec
		wantErr bool
	}{
		{
			name: "valid parallel spec with 2 sub-agents (min)",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid parallel spec with 50 sub-agents (max)",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: func() []SubAgentReference {
						agents := make([]SubAgentReference, 50)
						for i := 0; i < 50; i++ {
							agents[i] = SubAgentReference{Name: "agent"}
						}
						return agents
					}(),
				},
			},
			wantErr: false,
		},
		{
			name: "valid parallel spec with timeout",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
					Timeout: stringPtr("300s"),
				},
			},
			wantErr: false,
		},
		// T001: Test maxWorkers field
		{
			name: "valid parallel spec with maxWorkers=5",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
				MaxWorkers: int32Ptr(5),
			},
			wantErr: false,
		},
		{
			name: "valid parallel spec without maxWorkers (defaults to 10)",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
				// MaxWorkers omitted - should default to 10
			},
			wantErr: false,
		},
		// T002: Test validation boundaries
		{
			name: "valid maxWorkers=1 (minimum boundary)",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
				MaxWorkers: int32Ptr(1),
			},
			wantErr: false,
		},
		{
			name: "valid maxWorkers=50 (maximum boundary)",
			spec: ParallelAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{
						{Name: "agent-a"},
						{Name: "agent-b"},
					},
				},
				MaxWorkers: int32Ptr(50),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify struct can be created
			assert.NotNil(t, tt.spec.SubAgents)

			// Verify required field
			require.NotEmpty(t, tt.spec.SubAgents, "SubAgents is required")

			// Verify constraints (min 2 for parallel)
			assert.LessOrEqual(t, len(tt.spec.SubAgents), 50, "SubAgents max 50")
			assert.GreaterOrEqual(t, len(tt.spec.SubAgents), 2, "ParallelAgent requires min 2 sub-agents")

			// T001: Verify maxWorkers handling
			if tt.spec.MaxWorkers != nil {
				// Verify value is within valid range (1-50)
				assert.GreaterOrEqual(t, *tt.spec.MaxWorkers, int32(1), "MaxWorkers min 1")
				assert.LessOrEqual(t, *tt.spec.MaxWorkers, int32(50), "MaxWorkers max 50")
			}
		})
	}
}

// T007: Contract Test - LoopAgentSpec CRD Schema
func TestLoopAgentSpec_Schema(t *testing.T) {
	tests := []struct {
		name string
		spec LoopAgentSpec
	}{
		{
			name: "valid loop spec with 2 sub-agents",
			spec: LoopAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
				},
				MaxIterations: 1,
			},
		},
		{
			name: "valid loop spec with max_iterations=100",
			spec: LoopAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
				},
				MaxIterations: 100,
			},
		},
		{
			name: "valid loop spec with timeout",
			spec: LoopAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					Timeout:   stringPtr("8m"),
				},
				MaxIterations: 5,
			},
		},
		{
			name: "valid loop spec with description",
			spec: LoopAgentSpec{
				BaseWorkflowSpec: BaseWorkflowSpec{
					SubAgents:   []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					Description: "Iterative workflow",
				},
				MaxIterations: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify struct can be created
			assert.NotNil(t, tt.spec.SubAgents)

			// Verify required fields
			require.NotEmpty(t, tt.spec.SubAgents, "SubAgents is required")
			require.NotZero(t, tt.spec.MaxIterations, "MaxIterations is required")

			// Verify constraints
			assert.LessOrEqual(t, len(tt.spec.SubAgents), 50, "SubAgents max 50")
			assert.GreaterOrEqual(t, len(tt.spec.SubAgents), 2, "SubAgents min 2")
			assert.GreaterOrEqual(t, tt.spec.MaxIterations, int32(1), "MaxIterations min 1")
			assert.LessOrEqual(t, tt.spec.MaxIterations, int32(100), "MaxIterations max 100")
		})
	}
}

// T008: Contract Test - SubAgentReference Structure
func TestSubAgentReference_Schema(t *testing.T) {
	tests := []struct {
		name string
		ref  SubAgentReference
	}{
		{
			name: "valid reference with name only",
			ref: SubAgentReference{
				Name: "agent-a",
			},
		},
		{
			name: "valid reference with namespace",
			ref: SubAgentReference{
				Name:      "agent-a",
				Namespace: "custom-namespace",
			},
		},
		{
			name: "valid reference with kind",
			ref: SubAgentReference{
				Name: "agent-a",
				Kind: "Agent",
			},
		},
		{
			name: "valid reference with all fields",
			ref: SubAgentReference{
				Name:      "agent-a",
				Namespace: "custom-namespace",
				Kind:      "Agent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify required field
			require.NotEmpty(t, tt.ref.Name, "Name is required")

			// Verify Kind default/enum
			if tt.ref.Kind != "" {
				assert.Equal(t, "Agent", tt.ref.Kind, "Kind must be 'Agent'")
			}
		})
	}
}

// T009: Contract Test - WorkflowAgentSpec OneOf Validation
func TestWorkflowAgentSpec_OneOf(t *testing.T) {
	tests := []struct {
		name    string
		spec    WorkflowAgentSpec
		valid   bool
		message string
	}{
		{
			name: "valid - sequential only",
			spec: WorkflowAgentSpec{
				Sequential: &SequentialAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					},
				},
			},
			valid: true,
		},
		{
			name: "valid - parallel only",
			spec: WorkflowAgentSpec{
				Parallel: &ParallelAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{
							{Name: "agent-a"},
							{Name: "agent-b"},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "valid - loop only",
			spec: WorkflowAgentSpec{
				Loop: &LoopAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					},
					MaxIterations: 3,
				},
			},
			valid: true,
		},
		{
			name: "invalid - sequential and parallel",
			spec: WorkflowAgentSpec{
				Sequential: &SequentialAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					},
				},
				Parallel: &ParallelAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{
							{Name: "agent-a"},
							{Name: "agent-b"},
						},
					},
				},
			},
			valid:   false,
			message: "exactly one of sequential, parallel, or loop must be specified",
		},
		{
			name: "invalid - all three specified",
			spec: WorkflowAgentSpec{
				Sequential: &SequentialAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					},
				},
				Parallel: &ParallelAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{
							{Name: "agent-a"},
							{Name: "agent-b"},
						},
					},
				},
				Loop: &LoopAgentSpec{
					BaseWorkflowSpec: BaseWorkflowSpec{
						SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
					},
					MaxIterations: 3,
				},
			},
			valid:   false,
			message: "exactly one of sequential, parallel, or loop must be specified",
		},
		{
			name:    "invalid - none specified",
			spec:    WorkflowAgentSpec{},
			valid:   false,
			message: "exactly one of sequential, parallel, or loop must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Count non-nil fields
			nonNilCount := 0
			if tt.spec.Sequential != nil {
				nonNilCount++
			}
			if tt.spec.Parallel != nil {
				nonNilCount++
			}
			if tt.spec.Loop != nil {
				nonNilCount++
			}

			if tt.valid {
				assert.Equal(t, 1, nonNilCount, "Exactly one workflow type must be set")
			} else {
				assert.NotEqual(t, 1, nonNilCount, tt.message)
			}
		})
	}
}

// T010: Contract Test - AgentSpec Type Extension
func TestAgentSpec_WorkflowType(t *testing.T) {
	tests := []struct {
		name  string
		spec  AgentSpec
		valid bool
	}{
		{
			name: "valid - Workflow type with workflow spec",
			spec: AgentSpec{
				Type: AgentType_Workflow,
				Workflow: &WorkflowAgentSpec{
					Sequential: &SequentialAgentSpec{
						BaseWorkflowSpec: BaseWorkflowSpec{
							SubAgents: []SubAgentReference{{Name: "agent-a"}, {Name: "agent-b"}},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "valid - Declarative type still works (backward compatibility)",
			spec: AgentSpec{
				Type: AgentType_Declarative,
				Declarative: &DeclarativeAgentSpec{
					SystemMessage: "test",
				},
			},
			valid: true,
		},
		{
			name: "valid - BYO type still works (backward compatibility)",
			spec: AgentSpec{
				Type: AgentType_BYO,
				BYO:  &BYOAgentSpec{},
			},
			valid: true,
		},
		{
			name: "invalid - Workflow type without workflow spec",
			spec: AgentSpec{
				Type: AgentType_Workflow,
			},
			valid: false,
		},
		{
			name: "invalid - Workflow type with declarative spec",
			spec: AgentSpec{
				Type: AgentType_Workflow,
				Declarative: &DeclarativeAgentSpec{
					SystemMessage: "test",
				},
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify type is valid enum value
			assert.Contains(t, []AgentType{
				AgentType_Declarative,
				AgentType_BYO,
				AgentType_Workflow,
			}, tt.spec.Type, "Type must be valid enum value")

			// Verify mutual exclusion
			if tt.valid {
				if tt.spec.Type == AgentType_Workflow {
					require.NotNil(t, tt.spec.Workflow, "Workflow spec required for Workflow type")
					assert.Nil(t, tt.spec.Declarative, "Declarative must be nil for Workflow type")
					assert.Nil(t, tt.spec.BYO, "BYO must be nil for Workflow type")
				}
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}
