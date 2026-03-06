package compaction

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const defaultSystemPrompt = `You are summarizing a conversation history to reduce token usage while preserving key information.

Your task: Create a concise summary that captures:
- Important decisions made
- Key facts or data mentioned
- Current context and state
- User preferences or requirements

Keep the summary factual and relevant. Omit pleasantries and redundant information.

Output ONLY the summary text, no preamble or meta-commentary.`

// Compactor handles session history compaction by summarizing old events using an LLM.
//
// NOTE: This is a temporary implementation that mirrors the upstream design from
// https://github.com/google/adk-go/pull/300. Once upstream releases compaction
// support, this package should be deprecated in favor of google.golang.org/adk/compaction.
type Compactor struct {
	config Config
	model  adkmodel.LLM

	mu                         sync.Mutex
	invocationsSinceCompaction int
	lastCompactionTime         float64
}

// New creates a new Compactor with the given configuration and LLM model.
func New(config Config, model adkmodel.LLM) (*Compactor, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid compaction config: %w", err)
	}

	if model == nil {
		return nil, fmt.Errorf("model is required for compaction")
	}

	return &Compactor{
		config: config,
		model:  model,
	}, nil
}

// MaybeCompact checks if compaction should be triggered based on the current invocation
// and performs compaction if needed. Returns a compaction event if compaction occurred,
// or nil if no compaction was needed.
func (c *Compactor) MaybeCompact(ctx context.Context, session adksession.Session, currentInvocationID string) (*adksession.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.config.Enabled {
		return nil, nil
	}

	log := logr.FromContextOrDiscard(ctx)

	c.invocationsSinceCompaction++

	log.V(1).Info("Checking compaction trigger",
		"invocationsSinceCompaction", c.invocationsSinceCompaction,
		"compactionInterval", c.config.CompactionInterval)

	// Check if we've reached the compaction interval
	if c.invocationsSinceCompaction < c.config.CompactionInterval {
		return nil, nil
	}

	log.Info("Triggering compaction",
		"sessionID", session.ID(),
		"invocationsSinceCompaction", c.invocationsSinceCompaction)

	// Extract events to compact
	events := session.Events()
	if events.Len() < 2 {
		log.V(1).Info("Not enough events to compact")
		return nil, nil // Need at least some history to compact
	}

	// Determine compaction range
	// We want to compact all but the most recent overlap_size invocations
	// For simplicity, we'll compact events up to the most recent N events
	keepRecentCount := c.config.OverlapSize * 2 // Rough estimate: 2 events per invocation
	if keepRecentCount >= events.Len() {
		log.V(1).Info("Session too short for compaction", "eventCount", events.Len())
		return nil, nil
	}

	compactUpTo := events.Len() - keepRecentCount
	if compactUpTo <= 0 {
		return nil, nil
	}

	// Extract content from events to compact
	var contentParts []string
	var startTime, endTime float64

	for i := 0; i < compactUpTo; i++ {
		event := events.At(i)

		// Track time range
		timestamp := float64(event.Timestamp.Unix())
		if i == 0 {
			startTime = timestamp
		}
		endTime = timestamp

		// Extract text content
		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					role := event.Author
					if role == "" {
						role = "system"
					}
					contentParts = append(contentParts, fmt.Sprintf("%s: %s", role, part.Text))
				}
			}
		}
	}

	if len(contentParts) == 0 {
		log.V(1).Info("No content to compact")
		return nil, nil
	}

	// Generate summary using LLM
	conversationText := strings.Join(contentParts, "\n\n")
	summary, err := c.summarize(ctx, conversationText)
	if err != nil {
		return nil, fmt.Errorf("failed to generate compaction summary: %w", err)
	}

	log.Info("Compaction successful",
		"compactedEvents", compactUpTo,
		"summaryLength", len(summary))

	// Create compaction event
	compactionEvent := adksession.NewEvent(currentInvocationID)
	compactionEvent.Author = "system"
	compactionEvent.Content = &genai.Content{
		Role: "user", // Inject as user message so it's part of conversation history
		Parts: []*genai.Part{
			{Text: fmt.Sprintf("[Compacted conversation history: %s]", summary)},
		},
	}

	// Store compaction metadata in event state delta for reference
	// Note: EventActions.EventCompaction field will be available once upstream
	// releases PR #300. For now, we use StateDelta as a workaround.
	compactionEvent.Actions.StateDelta["compaction_start_time"] = startTime
	compactionEvent.Actions.StateDelta["compaction_end_time"] = endTime
	compactionEvent.Actions.StateDelta["compacted_event_count"] = compactUpTo
	compactionEvent.Actions.SkipSummarization = true // Don't summarize the summary

	// Reset counter
	c.invocationsSinceCompaction = 0
	c.lastCompactionTime = endTime

	return compactionEvent, nil
}

// summarize uses the LLM to create a concise summary of the conversation text.
func (c *Compactor) summarize(ctx context.Context, conversationText string) (string, error) {
	systemPrompt := c.config.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: fmt.Sprintf("%s\n\nConversation to summarize:\n\n%s", systemPrompt, conversationText)},
				},
			},
		},
	}

	// Generate summary (non-streaming)
	iter := c.model.GenerateContent(ctx, req, false)

	var summaryBuilder strings.Builder
	for resp, err := range iter {
		if err != nil {
			return "", err
		}

		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					summaryBuilder.WriteString(part.Text)
				}
			}
		}
	}

	summary := strings.TrimSpace(summaryBuilder.String())
	if summary == "" {
		return "", fmt.Errorf("LLM returned empty summary")
	}

	return summary, nil
}
