package a2a

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

var (
	denyWordPatterns    []*regexp.Regexp
	approveWordPatterns []*regexp.Regexp
)

func init() {
	for _, keyword := range KAgentHitlResumeKeywordsDeny {
		denyWordPatterns = append(denyWordPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(keyword)+`\b`))
	}
	for _, keyword := range KAgentHitlResumeKeywordsApprove {
		approveWordPatterns = append(approveWordPatterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(keyword)+`\b`))
	}
}

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

// DecisionType represents a HITL decision.
type DecisionType string

const (
	DecisionApprove DecisionType = "approve"
	DecisionDeny    DecisionType = "deny"
	DecisionReject  DecisionType = "reject"
)

// ToolApprovalRequest represents a tool call requiring user approval.
type ToolApprovalRequest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
}

// EventWriter is an interface for writing A2A events to a queue.
type EventWriter interface {
	Write(ctx context.Context, event a2atype.Event) error
}

// GetKAgentMetadataKey returns the prefixed metadata key.
func GetKAgentMetadataKey(key string) string {
	return KAgentMetadataKeyPrefix + key
}

// ExtractDecisionFromText extracts a decision from text using whole-word
// keyword matching. Word boundaries prevent false positives from substrings
// (e.g. "no" inside "know", "yes" inside "yesterday").
func ExtractDecisionFromText(text string) DecisionType {
	for _, pattern := range denyWordPatterns {
		if pattern.MatchString(text) {
			return DecisionDeny
		}
	}

	for _, pattern := range approveWordPatterns {
		if pattern.MatchString(text) {
			return DecisionApprove
		}
	}

	return ""
}

// ExtractDecisionFromMessage extracts a decision from an A2A message.
// Priority 1: DataPart with decision_type field.
// Priority 2: TextPart keyword matching.
func ExtractDecisionFromMessage(message *a2atype.Message) DecisionType {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}

	for _, part := range message.Parts {
		if dataPart, ok := part.(*a2atype.DataPart); ok {
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

	for _, part := range message.Parts {
		switch p := part.(type) {
		case a2atype.TextPart:
			if decision := ExtractDecisionFromText(p.Text); decision != "" {
				return decision
			}
		}
	}

	return ""
}

// IsInputRequiredTask checks if a task state indicates waiting for user input.
func IsInputRequiredTask(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired
}

// escapeMarkdownBackticks escapes backticks to prevent markdown rendering issues.
func escapeMarkdownBackticks(text any) string {
	str := fmt.Sprintf("%v", text)
	return strings.ReplaceAll(str, "`", "\\`")
}

// formatToolApprovalTextParts formats tool approval requests as human-readable TextParts.
func formatToolApprovalTextParts(actionRequests []ToolApprovalRequest) []a2atype.Part {
	var parts []a2atype.Part

	parts = append(parts, a2atype.TextPart{Text: "**Approval Required**\n\n"})
	parts = append(parts, a2atype.TextPart{Text: "The following actions require your approval:\n\n"})

	for _, action := range actionRequests {
		escapedToolName := escapeMarkdownBackticks(action.Name)
		parts = append(parts, a2atype.TextPart{Text: fmt.Sprintf("**Tool**: `%s`\n", escapedToolName)})
		parts = append(parts, a2atype.TextPart{Text: "**Arguments**:\n"})

		for key, value := range action.Args {
			escapedKey := escapeMarkdownBackticks(key)
			escapedValue := escapeMarkdownBackticks(value)
			parts = append(parts, a2atype.TextPart{Text: fmt.Sprintf("  • %s: `%s`\n", escapedKey, escapedValue)})
		}

		parts = append(parts, a2atype.TextPart{Text: "\n"})
	}

	return parts
}

// BuildToolApprovalMessage creates an A2A message with human-readable text
// parts describing the tool calls and a structured DataPart for machine
// processing by the client.
func BuildToolApprovalMessage(actionRequests []ToolApprovalRequest) *a2atype.Message {
	textParts := formatToolApprovalTextParts(actionRequests)

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

	dataPart := &a2atype.DataPart{
		Data: interruptData,
		Metadata: map[string]any{
			GetKAgentMetadataKey("type"): "interrupt_data",
		},
	}

	allParts := append(textParts, dataPart)
	return a2atype.NewMessage(a2atype.MessageRoleAgent, allParts...)
}

// HandleToolApprovalInterrupt sends an input_required event for tool approval.
// This is a framework-agnostic handler that any executor can call when
// it needs user approval for tool calls. It returns the message that was
// written so callers can use it for final-event tracking.
func HandleToolApprovalInterrupt(
	ctx context.Context,
	actionRequests []ToolApprovalRequest,
	infoProvider a2atype.TaskInfoProvider,
	queue EventWriter,
	appName string,
) (*a2atype.Message, error) {
	msg := BuildToolApprovalMessage(actionRequests)

	eventMetadata := map[string]any{
		"interrupt_type": KAgentHitlInterruptTypeToolApproval,
	}
	if appName != "" {
		eventMetadata["app_name"] = appName
	}

	now := time.Now().UTC()
	event := &a2atype.TaskStatusUpdateEvent{
		TaskID:    infoProvider.TaskInfo().TaskID,
		ContextID: infoProvider.TaskInfo().ContextID,
		Status: a2atype.TaskStatus{
			State:     a2atype.TaskStateInputRequired,
			Timestamp: &now,
			Message:   msg,
		},
		Final:    false,
		Metadata: eventMetadata,
	}

	if err := queue.Write(ctx, event); err != nil {
		return nil, fmt.Errorf("failed to write hitl event: %w", err)
	}

	return msg, nil
}
