package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

func TestBuildWorkflowSkill_Parallel(t *testing.T) {
	maxWorkers := 5
	timeout := "5m"
	config := &WorkflowAgentConfig{
		Name:         "test-parallel-workflow",
		Description:  "Test parallel workflow",
		Namespace:    "default",
		WorkflowType: "parallel",
		SubAgents: []WorkflowSubAgentRef{
			{Name: "agent1", Namespace: "default", Kind: "Agent", Description: "First agent"},
			{Name: "agent2", Namespace: "default", Kind: "Agent", Description: "Second agent"},
			{Name: "agent3", Namespace: "default", Kind: "Agent", Description: "Third agent"},
		},
		MaxWorkers: &maxWorkers,
		Timeout:    &timeout,
	}

	skill, err := BuildWorkflowSkill(config)
	require.NoError(t, err)

	// Verify basic skill fields
	assert.Equal(t, "workflow.parallel", skill.ID)
	assert.Equal(t, "Parallel Workflow", skill.Name)
	assert.Contains(t, *skill.Description, "parallel")
	assert.Contains(t, skill.Tags, "workflow")
	assert.Contains(t, skill.Tags, "parallel")
	assert.Contains(t, skill.Tags, "orchestration")
}

func TestBuildWorkflowSkill_Sequential(t *testing.T) {
	timeout := "10m"
	config := &WorkflowAgentConfig{
		Name:         "test-sequential-workflow",
		Description:  "Test sequential workflow",
		Namespace:    "default",
		WorkflowType: "sequential",
		SubAgents: []WorkflowSubAgentRef{
			{Name: "step1", Namespace: "default", Kind: "Agent", Description: "First step"},
			{Name: "step2", Namespace: "default", Kind: "Agent", Description: "Second step"},
		},
		Timeout: &timeout,
	}

	skill, err := BuildWorkflowSkill(config)
	require.NoError(t, err)

	// Verify basic skill fields
	assert.Equal(t, "workflow.sequential", skill.ID)
	assert.Equal(t, "Sequential Workflow", skill.Name)
	assert.Contains(t, *skill.Description, "sequential")
}

func TestBuildWorkflowSkill_Loop(t *testing.T) {
	maxIterations := 5
	timeout := "3m"
	config := &WorkflowAgentConfig{
		Name:         "test-loop-workflow",
		Description:  "Test loop workflow",
		Namespace:    "default",
		WorkflowType: "loop",
		SubAgents: []WorkflowSubAgentRef{
			{Name: "iterator", Namespace: "default", Kind: "Agent", Description: "Iterator agent"},
		},
		MaxIterations: &maxIterations,
		Timeout:       &timeout,
	}

	skill, err := BuildWorkflowSkill(config)
	require.NoError(t, err)

	// Verify basic skill fields
	assert.Equal(t, "workflow.loop", skill.ID)
	assert.Equal(t, "Loop Workflow", skill.Name)
	assert.Contains(t, *skill.Description, "loop")
}

func TestBuildWorkflowSkill_NilConfig(t *testing.T) {
	skill, err := BuildWorkflowSkill(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
	assert.Equal(t, "", skill.ID)
}

func TestBuildWorkflowSkillMetadata_Parallel(t *testing.T) {
	maxWorkers := 5
	timeout := "5m"
	config := &WorkflowAgentConfig{
		Name:         "test-parallel-workflow",
		Description:  "Test parallel workflow",
		Namespace:    "default",
		WorkflowType: "parallel",
		SubAgents: []WorkflowSubAgentRef{
			{Name: "agent1", Namespace: "default", Kind: "Agent", Description: "First agent"},
			{Name: "agent2", Namespace: "default", Kind: "Agent", Description: "Second agent"},
		},
		MaxWorkers: &maxWorkers,
		Timeout:    &timeout,
	}

	metadata, err := BuildWorkflowSkillMetadata(config)
	require.NoError(t, err)

	// Verify metadata fields
	assert.Equal(t, "workflow.parallel", metadata.ID)
	assert.Equal(t, "Parallel Workflow", metadata.Name)
	assert.Equal(t, "parallel", metadata.WorkflowType)
	assert.Len(t, metadata.SubAgents, 2)
	assert.Equal(t, 5, metadata.Config["maxWorkers"])
	assert.Equal(t, "5m", metadata.Config["timeout"])
}

func TestMergeSubAgentSkills(t *testing.T) {
	// Create a workflow card with one workflow skill
	workflowSkill := server.AgentSkill{
		ID:   "workflow.parallel",
		Name: "Parallel Workflow",
	}

	workflowCard := &server.AgentCard{
		Name:        "test_workflow",
		Description: "Test workflow",
		URL:         "http://test-workflow.default:8080",
		Skills:      []server.AgentSkill{workflowSkill},
	}

	// Create sub-agent cards with their own skills
	desc1 := "Diagnose Istio"
	desc2 := "Install Helm"
	subAgentCards := []*server.AgentCard{
		{
			Name: "istio_agent",
			Skills: []server.AgentSkill{
				{ID: "istio.diagnose", Name: "Diagnose Istio", Description: &desc1},
				{ID: "istio.config", Name: "Configure Istio"},
			},
		},
		{
			Name: "helm_agent",
			Skills: []server.AgentSkill{
				{ID: "helm.install", Name: "Install Chart", Description: &desc2},
			},
		},
	}

	// Merge skills
	MergeSubAgentSkills(workflowCard, subAgentCards)

	// Verify the workflow card now has all skills
	assert.Len(t, workflowCard.Skills, 4) // 1 workflow + 2 istio + 1 helm

	// Verify order: workflow skill first, then sub-agent skills
	assert.Equal(t, "workflow.parallel", workflowCard.Skills[0].ID)
	assert.Equal(t, "istio.diagnose", workflowCard.Skills[1].ID)
	assert.Equal(t, "istio.config", workflowCard.Skills[2].ID)
	assert.Equal(t, "helm.install", workflowCard.Skills[3].ID)
}

func TestMergeSubAgentSkills_EmptySubAgents(t *testing.T) {
	workflowCard := &server.AgentCard{
		Skills: []server.AgentSkill{{ID: "workflow.sequential"}},
	}

	MergeSubAgentSkills(workflowCard, []*server.AgentCard{})

	// Should still have the workflow skill
	assert.Len(t, workflowCard.Skills, 1)
	assert.Equal(t, "workflow.sequential", workflowCard.Skills[0].ID)
}

func TestMergeSubAgentSkills_NilCards(t *testing.T) {
	workflowCard := &server.AgentCard{
		Skills: []server.AgentSkill{{ID: "workflow.parallel"}},
	}

	// Mix of nil and valid cards
	subAgentCards := []*server.AgentCard{
		nil,
		{Skills: []server.AgentSkill{{ID: "agent.skill1"}}},
		nil,
	}

	MergeSubAgentSkills(workflowCard, subAgentCards)

	// Should have workflow skill + 1 sub-agent skill (nil cards skipped)
	assert.Len(t, workflowCard.Skills, 2)
	assert.Equal(t, "workflow.parallel", workflowCard.Skills[0].ID)
	assert.Equal(t, "agent.skill1", workflowCard.Skills[1].ID)
}

func TestInjectWorkflowMetadataIntoCard(t *testing.T) {
	// Create a card with workflow skill and sub-agent skills
	card := &server.AgentCard{
		Name:        "test_workflow",
		Description: "Test workflow",
		URL:         "http://test:8080",
		Skills: []server.AgentSkill{
			{ID: "workflow.parallel", Name: "Parallel Workflow"},
			{ID: "sub.skill1", Name: "Sub Skill 1"},
			{ID: "sub.skill2", Name: "Sub Skill 2"},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	// Marshal card to JSON
	cardJSON, err := json.Marshal(card)
	require.NoError(t, err)

	// Create workflow metadata
	maxWorkers := 5
	timeout := "5m"
	metadata := &WorkflowSkillMetadata{
		ID:           "workflow.parallel",
		Name:         "Parallel Workflow",
		WorkflowType: "parallel",
		SubAgents: []WorkflowSubAgentRef{
			{Name: "agent1", Namespace: "default", Kind: "Agent"},
		},
		Config: map[string]interface{}{
			"maxWorkers": maxWorkers,
			"timeout":    timeout,
		},
	}

	// Inject workflow metadata
	modifiedJSON, err := InjectWorkflowMetadataIntoCard(cardJSON, metadata)
	require.NoError(t, err)

	// Parse the modified JSON
	var cardMap map[string]interface{}
	err = json.Unmarshal(modifiedJSON, &cardMap)
	require.NoError(t, err)

	// Verify the card still has all skills
	skills := cardMap["skills"].([]interface{})
	assert.Len(t, skills, 3)

	// Verify the first skill has workflow metadata
	firstSkill := skills[0].(map[string]interface{})
	assert.Equal(t, "workflow.parallel", firstSkill["id"])
	assert.Equal(t, "parallel", firstSkill["workflowType"])

	// Verify sub-agents
	subAgents := firstSkill["subAgents"].([]interface{})
	assert.Len(t, subAgents, 1)
	subAgent := subAgents[0].(map[string]interface{})
	assert.Equal(t, "agent1", subAgent["name"])

	// Verify config
	config := firstSkill["config"].(map[string]interface{})
	assert.Equal(t, float64(5), config["maxWorkers"])
	assert.Equal(t, "5m", config["timeout"])

	// Verify other skills are unchanged
	secondSkill := skills[1].(map[string]interface{})
	assert.Equal(t, "sub.skill1", secondSkill["id"])
	assert.Nil(t, secondSkill["workflowType"]) // Should not have workflow metadata
}

func TestInjectWorkflowMetadataIntoCard_ErrorCases(t *testing.T) {
	metadata := &WorkflowSkillMetadata{
		WorkflowType: "parallel",
		SubAgents:    []WorkflowSubAgentRef{},
		Config:       map[string]interface{}{},
	}

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := InjectWorkflowMetadataIntoCard([]byte("invalid json"), metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal card JSON")
	})

	t.Run("missing skills field", func(t *testing.T) {
		cardJSON := []byte(`{"name":"test"}`)
		_, err := InjectWorkflowMetadataIntoCard(cardJSON, metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing 'skills' field")
	})

	t.Run("empty skills array", func(t *testing.T) {
		cardJSON := []byte(`{"skills":[]}`)
		_, err := InjectWorkflowMetadataIntoCard(cardJSON, metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "skills' array is empty")
	})
}
