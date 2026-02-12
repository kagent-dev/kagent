package a2a

import (
	"context"
	"fmt"
	"strings"
	"time"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
)

const (
	KAgentMetadataKeyPrefix = "kagent_"

	KAgentHitlInterruptTypeToolApproval = "tool_approval"
	KAgentHitlDecisionTypeKey           = "decision_type"
	KAgentHitlDecisionTypeApprove       = "approve"
	KAgentHitlDecisionTypeDeny          = "deny"
	KAgentHitlDecisionTypeReject        = "reject"
)

var (
	KAgentHitlResumeKeywordsApprove = []string{"approved", "approve", "proceed", "yes", "continue"}
	KAgentHitlResumeKeywordsDeny    = []string{"denied", "deny", "reject", "no", "cancel", "stop"}
)

type DecisionType string

const (
	DecisionApprove DecisionType = "approve"
	DecisionDeny    DecisionType = "deny"
	DecisionReject  DecisionType = "reject"
)

// ToolApprovalRequest structure for a tool call requiring approval.
type ToolApprovalRequest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
}

// EventWriter is an interface for writing A2A events to a queue.
type EventWriter interface {
	Write(ctx context.Context, event a2aschema.Event) error
}

// GetKAgentMetadataKey returns the prefixed metadata key.
func GetKAgentMetadataKey(key string) string {
	return KAgentMetadataKeyPrefix + key
}

// ExtractDecisionFromText extracts decision from text using keyword matching.
func ExtractDecisionFromText(text string) DecisionType {
	lower := strings.ToLower(text)

	// Check deny keywords first
	for _, keyword := range KAgentHitlResumeKeywordsDeny {
		if strings.Contains(lower, keyword) {
			return DecisionDeny
		}
	}

	// Check approve keywords
	for _, keyword := range KAgentHitlResumeKeywordsApprove {
		if strings.Contains(lower, keyword) {
			return DecisionApprove
		}
	}

	return ""
}

// ExtractDecisionFromMessage extracts decision from A2A message.
func ExtractDecisionFromMessage(message *a2aschema.Message) DecisionType {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}

	// Priority 1: Scan for DataPart with decision_type
	for _, part := range message.Parts {
		if dataPart, ok := part.(*a2aschema.DataPart); ok {
			if decision, ok := dataPart.Data[KAgentHitlDecisionTypeKey].(string); ok {
				switch decision {
				case KAgentHitlDecisionTypeApprove:
					return DecisionApprove
				case KAgentHitlDecisionTypeDeny:
					return DecisionDeny
				case KAgentHitlDecisionTypeReject:
					return DecisionReject
				}
			}
		}
	}

	// Priority 2: Fallback to TextPart keyword matching
	for _, part := range message.Parts {
		if textPart, ok := part.(*a2aschema.TextPart); ok {
			if decision := ExtractDecisionFromText(textPart.Text); decision != "" {
				return decision
			}
		}
	}

	return ""
}

// IsInputRequiredTask checks if task state indicates waiting for user input.
func IsInputRequiredTask(state a2aschema.TaskState) bool {
	return state == a2aschema.TaskStateInputRequired
}

// escapeMarkdownBackticks escapes backticks in text to prevent markdown rendering issues
func escapeMarkdownBackticks(text any) string {
	str := fmt.Sprintf("%v", text)
	return strings.ReplaceAll(str, "`", "\\`")
}

// formatToolApprovalTextParts formats tool approval requests as human-readable TextParts
// with proper markdown escaping to prevent rendering issues (matching Python implementation)
func formatToolApprovalTextParts(actionRequests []ToolApprovalRequest) []a2aschema.Part {
	var parts []a2aschema.Part

	// Add header
	parts = append(parts, &a2aschema.TextPart{Text: "**Approval Required**\n\n"})
	parts = append(parts, &a2aschema.TextPart{Text: "The following actions require your approval:\n\n"})

	// List each action
	for _, action := range actionRequests {
		escapedToolName := escapeMarkdownBackticks(action.Name)
		parts = append(parts, &a2aschema.TextPart{Text: fmt.Sprintf("**Tool**: `%s`\n", escapedToolName)})
		parts = append(parts, &a2aschema.TextPart{Text: "**Arguments**:\n"})

		for key, value := range action.Args {
			escapedKey := escapeMarkdownBackticks(key)
			escapedValue := escapeMarkdownBackticks(value)
			parts = append(parts, &a2aschema.TextPart{Text: fmt.Sprintf("  • %s: `%s`\n", escapedKey, escapedValue)})
		}

		parts = append(parts, &a2aschema.TextPart{Text: "\n"})
	}

	return parts
}

// HandleToolApprovalInterrupt sends input_required event for tool approval.
// This is a framework-agnostic handler that any executor can call when
// it needs user approval for tool calls.
func HandleToolApprovalInterrupt(
	ctx context.Context,
	actionRequests []ToolApprovalRequest,
	infoProvider a2aschema.TaskInfoProvider,
	queue EventWriter,
	appName string,
) error {
	// Build human-readable message with markdown escaping
	textParts := formatToolApprovalTextParts(actionRequests)

	// Build structured DataPart for machine processing
	actionRequestsData := make([]map[string]any, len(actionRequests))
	for i, req := range actionRequests {
		actionRequestsData[i] = map[string]any{
			"name": req.Name,
			"args": req.Args,
		}
		if req.ID != "" {
			actionRequestsData[i]["id"] = req.ID
		}
	}

	interruptData := map[string]any{
		"interrupt_type":  KAgentHitlInterruptTypeToolApproval,
		"action_requests": actionRequestsData,
	}

	dataPart := &a2aschema.DataPart{
		Data: interruptData,
		Metadata: map[string]any{
			GetKAgentMetadataKey("type"): "interrupt_data",
		},
	}

	// Combine message parts
	allParts := append(textParts, dataPart)

	// Build event metadata
	eventMetadata := map[string]any{
		"interrupt_type": KAgentHitlInterruptTypeToolApproval,
	}
	if appName != "" {
		eventMetadata["app_name"] = appName
	}

	msg := a2aschema.NewMessage(a2aschema.MessageRoleAgent, allParts...)

	now := time.Now().UTC()
	event := &a2aschema.TaskStatusUpdateEvent{
		TaskID:    infoProvider.TaskInfo().TaskID,
		ContextID: infoProvider.TaskInfo().ContextID,
		Status: a2aschema.TaskStatus{
			State:     a2aschema.TaskStateInputRequired,
			Timestamp: &now,
			Message:   msg,
		},
		Final:    false, // Not final - waiting for user input (matching Python)
		Metadata: eventMetadata,
	}

	if err := queue.Write(ctx, event); err != nil {
		return fmt.Errorf("failed to write hitl event: %w", err)
	}

	return nil
}
