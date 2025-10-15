package agent

import (
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// WorkflowSkillMetadata represents the complete workflow skill structure including workflow-specific fields.
// This struct is used to build the JSON that will be injected into the AgentCard.
type WorkflowSkillMetadata struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Tags         []string               `json:"tags"`
	WorkflowType string                 `json:"workflowType"`
	SubAgents    []WorkflowSubAgentRef  `json:"subAgents"`
	Config       map[string]interface{} `json:"config"`
}

// BuildWorkflowSkill creates an AgentSkill for the workflow.
// Note: The returned AgentSkill only has standard fields. The workflow-specific metadata
// must be injected later using InjectWorkflowMetadataIntoCard.
func BuildWorkflowSkill(config *WorkflowAgentConfig) (server.AgentSkill, error) {
	if config == nil {
		return server.AgentSkill{}, fmt.Errorf("workflow agent config cannot be nil")
	}

	description := fmt.Sprintf("Executes multiple sub-agents in %s mode", config.WorkflowType)

	skill := server.AgentSkill{
		ID:          fmt.Sprintf("workflow.%s", config.WorkflowType),
		Name:        fmt.Sprintf("%s Workflow", capitalize(config.WorkflowType)),
		Description: &description,
		Tags:        []string{"workflow", config.WorkflowType, "orchestration"},
	}

	return skill, nil
}

// BuildWorkflowSkillMetadata creates the complete workflow skill metadata including custom fields.
// This is used to inject workflow-specific data into the AgentCard JSON.
func BuildWorkflowSkillMetadata(config *WorkflowAgentConfig) (*WorkflowSkillMetadata, error) {
	if config == nil {
		return nil, fmt.Errorf("workflow agent config cannot be nil")
	}

	// Build sub-agents metadata
	subAgents := make([]WorkflowSubAgentRef, len(config.SubAgents))
	copy(subAgents, config.SubAgents)

	// Build workflow config based on type
	workflowConfig := make(map[string]interface{})

	switch config.WorkflowType {
	case "parallel":
		if config.MaxWorkers != nil {
			workflowConfig["maxWorkers"] = *config.MaxWorkers
		}
		if config.Timeout != nil {
			workflowConfig["timeout"] = *config.Timeout
		}
	case "sequential":
		if config.Timeout != nil {
			workflowConfig["timeout"] = *config.Timeout
		}
	case "loop":
		if config.MaxIterations != nil {
			workflowConfig["maxIterations"] = *config.MaxIterations
		}
		if config.Timeout != nil {
			workflowConfig["timeout"] = *config.Timeout
		}
	default:
		return nil, fmt.Errorf("unknown workflow type: %s", config.WorkflowType)
	}

	description := fmt.Sprintf("Executes multiple sub-agents in %s mode", config.WorkflowType)

	metadata := &WorkflowSkillMetadata{
		ID:           fmt.Sprintf("workflow.%s", config.WorkflowType),
		Name:         fmt.Sprintf("%s Workflow", capitalize(config.WorkflowType)),
		Description:  description,
		Tags:         []string{"workflow", config.WorkflowType, "orchestration"},
		WorkflowType: config.WorkflowType,
		SubAgents:    subAgents,
		Config:       workflowConfig,
	}

	return metadata, nil
}

// MergeSubAgentSkills adds sub-agent skills to the workflow card's skills array.
// The workflow skill (with metadata) will be first, followed by all sub-agent skills.
func MergeSubAgentSkills(card *server.AgentCard, subAgentCards []*server.AgentCard) {
	// Collect all sub-agent skills
	var allSkills []server.AgentSkill

	// Keep the workflow skill as the first skill (already in card.Skills)
	allSkills = append(allSkills, card.Skills...)

	// Add all sub-agent skills
	for _, subCard := range subAgentCards {
		if subCard != nil && len(subCard.Skills) > 0 {
			allSkills = append(allSkills, subCard.Skills...)
		}
	}

	// Update the card with merged skills
	card.Skills = allSkills
}

// InjectWorkflowMetadataIntoCard modifies an AgentCard JSON to include workflow-specific metadata in the first skill.
// This is necessary because server.AgentSkill doesn't support custom fields, so we inject them at the JSON level.
// It preserves all existing skills in the array (workflow skill + sub-agent skills).
func InjectWorkflowMetadataIntoCard(cardJSON []byte, workflowMetadata *WorkflowSkillMetadata) ([]byte, error) {
	// Parse the card as a generic map
	var cardMap map[string]interface{}
	if err := json.Unmarshal(cardJSON, &cardMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal card JSON: %w", err)
	}

	// Get the skills array
	skillsRaw, ok := cardMap["skills"]
	if !ok {
		return nil, fmt.Errorf("card JSON missing 'skills' field")
	}

	skills, ok := skillsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("card 'skills' field is not an array")
	}

	if len(skills) == 0 {
		return nil, fmt.Errorf("card 'skills' array is empty")
	}

	// Get the first skill (the workflow skill)
	firstSkill, ok := skills[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("first skill is not an object")
	}

	// Inject workflow-specific fields into the first skill only
	firstSkill["workflowType"] = workflowMetadata.WorkflowType
	firstSkill["subAgents"] = workflowMetadata.SubAgents
	firstSkill["config"] = workflowMetadata.Config

	// Re-marshal the modified card (preserves all skills)
	modifiedJSON, err := json.Marshal(cardMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified card: %w", err)
	}

	return modifiedJSON, nil
}

// capitalize capitalizes the first letter of a string
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	// Convert lowercase to uppercase by subtracting 32 from ASCII value
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
