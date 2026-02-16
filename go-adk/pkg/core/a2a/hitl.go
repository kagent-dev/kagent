package a2a

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
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
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
	ID   string                 `json:"id,omitempty"`
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
func ExtractDecisionFromMessage(message *protocol.Message) DecisionType {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}

	// Priority 1: Scan for DataPart with decision_type
	for _, part := range message.Parts {
		if dataPart, ok := part.(*protocol.DataPart); ok {
			if dataMap, ok := dataPart.Data.(map[string]interface{}); ok {
				if decision, ok := dataMap[KAgentHitlDecisionTypeKey].(string); ok {
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
	}

	// Priority 2: Fallback to TextPart keyword matching
	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			if decision := ExtractDecisionFromText(textPart.Text); decision != "" {
				return decision
			}
		}
	}

	return ""
}

// IsInputRequiredTask checks if task state indicates waiting for user input.
// This matches Python's is_input_required_task function.
func IsInputRequiredTask(state protocol.TaskState) bool {
	return state == protocol.TaskStateInputRequired
}

// EventQueue is an interface for publishing A2A events.
type EventQueue interface {
	EnqueueEvent(ctx context.Context, event protocol.Event) error
}

// TaskStore is an interface for task persistence and synchronization.
// This is a simplified interface for HITL operations.
// The full implementation is KAgentTaskStore.
type TaskStore interface {
	WaitForSave(ctx context.Context, taskID string, timeout time.Duration) error
}

// escapeMarkdownBackticks escapes backticks in text to prevent markdown rendering issues
func escapeMarkdownBackticks(text interface{}) string {
	str := fmt.Sprintf("%v", text)
	return strings.ReplaceAll(str, "`", "\\`")
}

// formatToolApprovalTextParts formats tool approval requests as human-readable TextParts
// with proper markdown escaping to prevent rendering issues (matching Python implementation)
func formatToolApprovalTextParts(actionRequests []ToolApprovalRequest) []protocol.Part {
	var parts []protocol.Part

	// Add header
	parts = append(parts, protocol.NewTextPart("**Approval Required**\n\n"))
	parts = append(parts, protocol.NewTextPart("The following actions require your approval:\n\n"))

	// List each action
	for _, action := range actionRequests {
		// Escape backticks to prevent markdown breaking
		escapedToolName := escapeMarkdownBackticks(action.Name)
		parts = append(parts, protocol.NewTextPart(fmt.Sprintf("**Tool**: `%s`\n", escapedToolName)))
		parts = append(parts, protocol.NewTextPart("**Arguments**:\n"))

		for key, value := range action.Args {
			escapedKey := escapeMarkdownBackticks(key)
			escapedValue := escapeMarkdownBackticks(value)
			parts = append(parts, protocol.NewTextPart(fmt.Sprintf("  • %s: `%s`\n", escapedKey, escapedValue)))
		}

		parts = append(parts, protocol.NewTextPart("\n"))
	}

	return parts
}

// HandleToolApprovalInterrupt sends input_required event for tool approval.
// This is a framework-agnostic handler that any executor can call when
// it needs user approval for tool calls. It formats an approval message,
// sends an input_required event, and waits for the task to be saved.
//
// Args:
//   - actionRequests: List of tool calls requiring approval
//   - taskID: A2A task ID
//   - contextID: A2A context ID
//   - eventQueue: Event queue for publishing events
//   - taskStore: Task store for synchronization (can be nil)
//   - appName: Optional application name for metadata (empty string if not provided)
//
// Returns error if event enqueue fails. Timeout errors from WaitForSave are logged but not returned.
func HandleToolApprovalInterrupt(
	ctx context.Context,
	actionRequests []ToolApprovalRequest,
	taskID string,
	contextID string,
	eventQueue EventQueue,
	taskStore TaskStore,
	appName string,
) error {
	// Build human-readable message with markdown escaping (matching Python format_tool_approval_text_parts)
	textParts := formatToolApprovalTextParts(actionRequests)

	// Build structured DataPart for machine processing (client can parse this)
	// Convert action requests to map format (matching Python: [{"name": req.name, "args": req.args, "id": req.id} for req in action_requests])
	actionRequestsData := make([]map[string]interface{}, len(actionRequests))
	for i, req := range actionRequests {
		actionRequestsData[i] = map[string]interface{}{
			"name": req.Name,
			"args": req.Args,
		}
		if req.ID != "" {
			actionRequestsData[i]["id"] = req.ID
		}
	}

	interruptData := map[string]interface{}{
		"interrupt_type":  KAgentHitlInterruptTypeToolApproval,
		"action_requests": actionRequestsData,
	}

	dataPart := &protocol.DataPart{
		Kind: "data",
		Data: interruptData,
		Metadata: map[string]interface{}{
			GetKAgentMetadataKey("type"): "interrupt_data",
		},
	}

	// Combine message parts
	allParts := append(textParts, dataPart)

	// Build event metadata (only add app_name if provided, matching Python behavior)
	eventMetadata := map[string]interface{}{
		"interrupt_type": KAgentHitlInterruptTypeToolApproval,
	}
	if appName != "" {
		eventMetadata["app_name"] = appName
	}

	// Send input_required event (matching Python: final=False - not final, waiting for user input)
	event := &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateInputRequired,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message: &protocol.Message{
				MessageID: uuid.New().String(),
				Role:      protocol.MessageRoleAgent,
				Parts:     allParts,
			},
		},
		Final:    false, // Not final - waiting for user input (matching Python)
		Metadata: eventMetadata,
	}

	if err := eventQueue.EnqueueEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to enqueue hitl event: %w", err)
	}

	// Wait for the event consumer to persist the task (event-based sync)
	// This prevents race condition where approval arrives before task is saved
	// Timeout errors are handled gracefully (matching Python: logged as warning, not raised)
	if taskStore != nil {
		if err := taskStore.WaitForSave(ctx, taskID, 5*time.Second); err != nil {
			// Log warning but don't fail - timeout is expected in some cases
			// In production, use proper logging framework
			_ = err // TODO: Use proper logging when available
		}
	}

	return nil
}
