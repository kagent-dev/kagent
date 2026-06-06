package compaction

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"google.golang.org/adk/model"

	adkapiconfig "github.com/kagent-dev/kagent/go/api/adk"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	summaryEventAuthor        = "compaction_summarizer"
	defaultCompactionInterval = 5
	defaultOverlapSize        = 2
	// noInvocationSentinel is a sentinel for events with no InvocationID.
	// The NUL prefix makes collision with real IDs practically impossible.
	noInvocationSentinel = "\x00no_invocation"
	defaultSummaryPrompt = "You are a conversation compactor. Summarise the following agent conversation history concisely, preserving all key facts, decisions, tool calls, and outcomes. The summary will replace these events in the agent context window.\n\nConversation history:\n{{events}}\n\nProvide a concise summary:"
)

// Config holds compaction settings for an agent.
type Config struct {
	CompactionInterval int
	OverlapSize        int
	TokenThreshold     int
	EventRetentionSize int
	SummarizerLLM      model.LLM
	PromptTemplate     string
}

// Compactor performs post-invocation event history compaction on a session.
// A nil Compactor is valid; all methods are no-ops.
type Compactor struct {
	cfg    *Config
	logger logr.Logger
}

// New returns a Compactor for cfg, or nil when cfg is nil.
func New(cfg *Config, logger logr.Logger) *Compactor {
	if cfg == nil {
		return nil
	}
	return &Compactor{cfg: cfg, logger: logger.WithName("compaction")}
}

// MaybeCompact checks whether compaction should run after the latest invocation
// and performs it if so. Safe to call after every agent run.
func (c *Compactor) MaybeCompact(
	ctx context.Context,
	sess adksession.Session,
	sessionSvc adksession.Service,
	lastTokens int,
) error {
	if c == nil {
		return nil
	}

	log := c.logger.WithValues("sessionID", sess.ID())

	allEvents := collectEvents(sess)
	invocations := groupByInvocation(allEvents)

	trigger := false

	if len(invocations) >= c.cfg.CompactionInterval {
		trigger = true
		log.V(1).Info("Compaction triggered by interval",
			"invocations", len(invocations),
			"threshold", c.cfg.CompactionInterval)
	}

	if !trigger && c.cfg.TokenThreshold > 0 {
		tokens := lastTokens
		if tokens == 0 {
			tokens = estimateTokens(allEvents)
		}
		if tokens >= c.cfg.TokenThreshold {
			trigger = true
			log.V(1).Info("Compaction triggered by token threshold",
				"tokens", tokens,
				"threshold", c.cfg.TokenThreshold)
		}
	}

	if !trigger {
		return nil
	}

	return c.compact(ctx, sess, sessionSvc, invocations, log)
}

func (c *Compactor) compact(
	ctx context.Context,
	sess adksession.Session,
	sessionSvc adksession.Service,
	invocations []invocationGroup,
	log logr.Logger,
) error {
	keepCount := c.cfg.OverlapSize
	if keepCount >= len(invocations) {
		return nil
	}

	toCompact := invocations[:len(invocations)-keepCount]
	if len(toCompact) == 0 {
		return nil
	}

	var compactEvents []*adksession.Event
	for _, inv := range toCompact {
		compactEvents = append(compactEvents, inv.events...)
	}

	log.Info("Compacting events",
		"compactCount", len(compactEvents),
		"keepInvocations", keepCount)

	summaryText, err := c.summarise(ctx, compactEvents)
	if err != nil {
		log.Error(err, "Failed to summarise events; skipping compaction")
		return nil
	}

	summaryEvent := buildSummaryEvent(summaryText)

	if err := sessionSvc.AppendEvent(ctx, sess, summaryEvent); err != nil {
		return fmt.Errorf("compaction: failed to persist summary event: %w", err)
	}

	var keepEvents []*adksession.Event
	for _, inv := range invocations[len(invocations)-keepCount:] {
		keepEvents = append(keepEvents, inv.events...)
	}
	// Apply EventRetentionSize cap if configured.
	if c.cfg.EventRetentionSize > 0 && len(keepEvents) > c.cfg.EventRetentionSize {
		keepEvents = keepEvents[len(keepEvents)-c.cfg.EventRetentionSize:]
	}
	replaceSessionEvents(sess, summaryEvent, keepEvents)

	log.Info("Compaction complete",
		"summaryLen", len(summaryText),
		"keptInvocations", keepCount)

	return nil
}

func (c *Compactor) summarise(ctx context.Context, events []*adksession.Event) (string, error) {
	text := serializeEvents(events)

	if c.cfg.SummarizerLLM == nil {
		return "[Compacted history]\n" + text, nil
	}

	prompt := strings.ReplaceAll(c.cfg.PromptTemplate, "{{events}}", text)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
		},
	}

	var parts []string
	for resp, err := range c.cfg.SummarizerLLM.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("summarizer LLM error: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, p := range resp.Content.Parts {
				if p != nil && p.Text != "" {
					parts = append(parts, p.Text)
				}
			}
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("summarizer returned empty response")
	}
	return strings.Join(parts, ""), nil
}

type invocationGroup struct {
	invocationID string
	events       []*adksession.Event
}

func groupByInvocation(events []*adksession.Event) []invocationGroup {
	var groups []invocationGroup
	index := map[string]int{}

	for _, e := range events {
		id := e.InvocationID
		if id == "" {
			id = noInvocationSentinel
		}
		if i, ok := index[id]; ok {
			groups[i].events = append(groups[i].events, e)
		} else {
			index[id] = len(groups)
			groups = append(groups, invocationGroup{
				invocationID: id,
				events:       []*adksession.Event{e},
			})
		}
	}
	return groups
}

func collectEvents(sess adksession.Session) []*adksession.Event {
	var out []*adksession.Event
	for e := range sess.Events().All() {
		out = append(out, e)
	}
	return out
}

func serializeEvents(events []*adksession.Event) string {
	var sb strings.Builder
	for _, e := range events {
		if e.Content == nil {
			continue
		}
		role := e.Author
		if role == "" {
			role = e.Content.Role
		}
		for _, p := range e.Content.Parts {
			if p == nil {
				continue
			}
			switch {
			case p.Text != "":
				fmt.Fprintf(&sb, "[%s]: %s\n", role, p.Text)
			case p.FunctionCall != nil:
				fmt.Fprintf(&sb, "[%s] called tool %q\n", role, p.FunctionCall.Name)
			case p.FunctionResponse != nil:
				fmt.Fprintf(&sb, "[tool %s response]\n", p.FunctionResponse.Name)
			}
		}
	}
	return sb.String()
}

func buildSummaryEvent(summaryText string) *adksession.Event {
	e := &adksession.Event{
		ID:           uuid.NewString(),
		InvocationID: "compaction_" + uuid.NewString(),
		Timestamp:    time.Now(),
		Author:       summaryEventAuthor,
	}
	e.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: summaryText}},
		},
	}
	return e
}

func estimateTokens(events []*adksession.Event) int {
	return len(serializeEvents(events)) / 4
}

// replaceSessionEvents rewrites the in-memory event list when the session
// supports it. If not, the summary was still persisted via AppendEvent and
// will be visible on the next fresh Get from the backend.
func replaceSessionEvents(sess adksession.Session, summary *adksession.Event, keep []*adksession.Event) {
	type replacer interface {
		ReplaceEvents([]*adksession.Event)
	}
	if r, ok := sess.(replacer); ok {
		newEvents := make([]*adksession.Event, 0, 1+len(keep))
		newEvents = append(newEvents, summary)
		newEvents = append(newEvents, keep...)
		r.ReplaceEvents(newEvents)
	}
}

// FromAgentConfig builds a Config from the kagent AgentConfig.
// Returns nil when compaction is not configured.
func FromAgentConfig(agentCfg *adkapiconfig.AgentConfig) (*Config, error) {
	if agentCfg == nil || agentCfg.ContextConfig == nil || agentCfg.ContextConfig.Compaction == nil {
		return nil, nil
	}
	comp := agentCfg.ContextConfig.Compaction

	cfg := &Config{
		CompactionInterval: defaultCompactionInterval,
		OverlapSize:        defaultOverlapSize,
		PromptTemplate:     defaultSummaryPrompt,
	}

	if comp.CompactionInterval != nil && *comp.CompactionInterval > 0 {
		cfg.CompactionInterval = *comp.CompactionInterval
	}
	if comp.OverlapSize != nil && *comp.OverlapSize >= 0 {
		cfg.OverlapSize = *comp.OverlapSize
	}
	if comp.TokenThreshold != nil {
		cfg.TokenThreshold = *comp.TokenThreshold
	}
	if comp.EventRetentionSize != nil {
		cfg.EventRetentionSize = *comp.EventRetentionSize
	}
	if comp.PromptTemplate != "" {
		cfg.PromptTemplate = comp.PromptTemplate
	}

	return cfg, nil
}
