package a2a

import (
	"fmt"
	"regexp"
	"strings"

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
)

// ToolApprovalRequest represents a tool call requiring user approval.
type ToolApprovalRequest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
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
				}
			}
		}
	}

	for _, part := range message.Parts {
		if tp, ok := part.(a2atype.TextPart); ok {
			if decision := ExtractDecisionFromText(tp.Text); decision != "" {
				return decision
			}
		}
	}

	return ""
}

// escapeMarkdownBackticks escapes backticks to prevent markdown rendering issues.
func escapeMarkdownBackticks(text string) string {
	return strings.ReplaceAll(text, "`", "\\`")
}

// formatToolApprovalTextParts formats tool approval requests as a single
// human-readable TextPart in markdown.
func formatToolApprovalTextParts(actionRequests []ToolApprovalRequest) a2atype.TextPart {
	var b strings.Builder
	b.WriteString("**Approval Required**\n\n")
	b.WriteString("The following actions require your approval:\n\n")

	for _, action := range actionRequests {
		fmt.Fprintf(&b, "**Tool**: `%s`\n", escapeMarkdownBackticks(action.Name))
		b.WriteString("**Arguments**:\n")
		for key, value := range action.Args {
			fmt.Fprintf(&b, "  • %s: `%s`\n",
				escapeMarkdownBackticks(key),
				escapeMarkdownBackticks(fmt.Sprintf("%v", value)))
		}
		b.WriteByte('\n')
	}

	return a2atype.TextPart{Text: b.String()}
}

// BuildToolApprovalMessage creates an A2A message with human-readable text
// describing the tool calls and a structured DataPart for machine processing
// by the client.
func BuildToolApprovalMessage(actionRequests []ToolApprovalRequest) *a2atype.Message {
	textPart := formatToolApprovalTextParts(actionRequests)

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

	dataPart := &a2atype.DataPart{
		Data: map[string]any{
			"interrupt_type":  KAgentHitlInterruptTypeToolApproval,
			"action_requests": actionRequestsData,
		},
		Metadata: map[string]any{
			GetKAgentMetadataKey("type"): "interrupt_data",
		},
	}

	return a2atype.NewMessage(a2atype.MessageRoleAgent, textPart, dataPart)
}
