package compaction

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	adkapiconfig "github.com/kagent-dev/kagent/go/api/adk"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	compactionAuthor         = "compaction"
	compactionInvocationBase = "compaction_"
	noInvocationSentinel     = "\x00no_invocation"

	defaultCompactionInterval = 5
	defaultOverlapSize        = 2

	defaultSummaryPrompt = `You are a conversation compactor. Summarise the following agent conversation history concisely, preserving all key facts, decisions, tool calls, and outcomes. The summary will replace these events in the agent context window.

Conversation history:
{{events}}

Provide a concise summary:`
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

// MaybeCompact checks whether compaction should run after the latest
// invocation and performs it if triggered. Safe to call after every agent run.
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

	// Token threshold takes precedence.
	if c.cfg.TokenThreshold > 0 {
		tokens := lastTokens
		if tokens == 0 {
			tokens = estimateTokens(allEvents)
		}
		if tokens >= c.cfg.TokenThreshold {
			log.V(1).Info("Compaction triggered by token threshold",
				"tokens", tokens, "threshold", c.cfg.TokenThreshold)
			return c.compactTokenThreshold(ctx, sess, sessionSvc, allEvents, log)
		}
	}

	// Sliding window: count new invocations since the last compaction watermark.
	nonMarkers := filterCompactionMarkers(allEvents)
	invocations := groupByInvocation(nonMarkers)
	watermark := findWatermark(allEvents)

	window, ok := decideSlidingWindow(invocations, watermark, c.cfg)
	if !ok {
		return nil
	}
	log.V(1).Info("Compaction triggered by interval",
		"watermark", watermark, "windowGroups", len(window))
	return c.compact(ctx, sess, sessionSvc, window, log)
}

func (c *Compactor) compact(
	ctx context.Context,
	sess adksession.Session,
	sessionSvc adksession.Service,
	window []invocationGroup,
	log logr.Logger,
) error {
	var windowEvents []*adksession.Event
	for _, inv := range window {
		windowEvents = append(windowEvents, inv.events...)
	}

	// Never compact through an open function call or unresolved HITL confirmation.
	windowEvents = longestSelfContainedPrefix(windowEvents)
	if len(windowEvents) == 0 {
		return nil
	}

	startTS := windowEvents[0].Timestamp
	endTS := windowEvents[len(windowEvents)-1].Timestamp

	summaryContent, err := c.summarize(ctx, windowEvents)
	if err != nil {
		log.Error(err, "Failed to summarize; skipping compaction")
		return nil
	}
	if summaryContent == nil {
		return nil
	}

	compactionEvent := buildCompactionEvent(ctx, summaryContent, startTS, endTS)
	if err := sessionSvc.AppendEvent(ctx, sess, compactionEvent); err != nil {
		return fmt.Errorf("compaction: append event: %w", err)
	}
	log.Info("Compaction complete",
		"start", startTS, "end", endTS, "windowLen", len(windowEvents))
	return nil
}

func (c *Compactor) compactTokenThreshold(
	ctx context.Context,
	sess adksession.Session,
	sessionSvc adksession.Service,
	allEvents []*adksession.Event,
	log logr.Logger,
) error {
	windowEvents := filterCompactionMarkers(allEvents)
	if c.cfg.EventRetentionSize > 0 && len(windowEvents) > c.cfg.EventRetentionSize {
		windowEvents = windowEvents[:len(windowEvents)-c.cfg.EventRetentionSize]
	}

	windowEvents = longestSelfContainedPrefix(windowEvents)
	if len(windowEvents) == 0 {
		return nil
	}

	startTS := windowEvents[0].Timestamp
	endTS := windowEvents[len(windowEvents)-1].Timestamp

	summaryContent, err := c.summarize(ctx, windowEvents)
	if err != nil {
		log.Error(err, "Failed to summarize; skipping token compaction")
		return nil
	}
	if summaryContent == nil {
		return nil
	}

	compactionEvent := buildCompactionEvent(ctx, summaryContent, startTS, endTS)
	if err := sessionSvc.AppendEvent(ctx, sess, compactionEvent); err != nil {
		return fmt.Errorf("compaction: append event: %w", err)
	}
	log.Info("Token compaction complete",
		"start", startTS, "end", endTS, "windowLen", len(windowEvents))
	return nil
}

func (c *Compactor) summarize(ctx context.Context, events []*adksession.Event) (*genai.Content, error) {
	text := serializeEvents(events)
	if text == "" {
		return nil, nil
	}

	if c.cfg.SummarizerLLM == nil {
		return &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: "[Compacted history]\n" + text}},
		}, nil
	}

	prompt := strings.ReplaceAll(c.cfg.PromptTemplate, "{{events}}", text)
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
		},
		// No SystemInstruction: avoids Anthropic 400 on null system prompt.
	}

	var parts []string
	for resp, err := range c.cfg.SummarizerLLM.GenerateContent(ctx, req, false) {
		if err != nil {
			return nil, fmt.Errorf("summarizer: %w", err)
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
		return nil, fmt.Errorf("summarizer: empty response")
	}
	return &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: strings.Join(parts, "")}},
	}, nil
}

// BuildCompactedEventList returns a new event list with compacted ranges
// replaced by synthetic summary events. Compaction marker events are always
// removed from the output. Subsumed compactions are silently dropped.
//
// Called by compactingService.Get before the runner sees the session, so
// buildContentsDefault never processes marker events.
func BuildCompactedEventList(agentName string, events []*adksession.Event) []*adksession.Event {
	type compactionRange struct {
		startNano, endNano int64
		content            *genai.Content
		markerEv           *adksession.Event
	}

	var ranges []compactionRange
	for _, ev := range events {
		if !isCompactionMarker(ev) {
			continue
		}
		parsed, ok := parseCompactionInvocationID(ev.InvocationID)
		if !ok {
			continue
		}
		ranges = append(ranges, compactionRange{
			startNano: parsed[0],
			endNano:   parsed[1],
			content:   ev.Content,
			markerEv:  ev,
		})
	}
	if len(ranges) == 0 {
		return events
	}

	// Remove subsumed ranges.
	active := make([]compactionRange, 0, len(ranges))
	for _, r := range ranges {
		subsumed := false
		for _, r2 := range ranges {
			if r2.markerEv == r.markerEv {
				continue
			}
			if r2.startNano <= r.startNano && r2.endNano >= r.endNano {
				subsumed = true
				break
			}
		}
		if !subsumed {
			active = append(active, r)
		}
	}

	// Sort by start time (insertion sort; range count is typically small).
	for i := 1; i < len(active); i++ {
		for j := i; j > 0 && active[j].startNano < active[j-1].startNano; j-- {
			active[j], active[j-1] = active[j-1], active[j]
		}
	}

	injected := make([]bool, len(active))
	result := make([]*adksession.Event, 0, len(events))
	for _, ev := range events {
		if isCompactionMarker(ev) {
			continue
		}
		nano := ev.Timestamp.UnixNano()
		inRange := -1
		for i, r := range active {
			if nano >= r.startNano && nano <= r.endNano {
				inRange = i
				break
			}
		}
		if inRange >= 0 {
			if !injected[inRange] && active[inRange].content != nil {
				summaryEv := adksession.NewEvent(fmt.Sprintf("compacted_%d", active[inRange].startNano))
				summaryEv.Author = agentName
				summaryEv.Timestamp = time.Unix(0, active[inRange].startNano).UTC()
				summaryEv.Content = active[inRange].content
				result = append(result, summaryEv)
				injected[inRange] = true
			}
			continue
		}
		result = append(result, ev)
	}
	return result
}

// decideSlidingWindow returns the invocation groups to compact based on a
// watermark-based count of new invocations. Returns (nil, false) when the
// trigger condition is not met.
func decideSlidingWindow(invocations []invocationGroup, watermark time.Time, cfg *Config) ([]invocationGroup, bool) {
	firstNewIdx := len(invocations)
	for i, inv := range invocations {
		if watermark.IsZero() || maxTimestamp(inv.events).After(watermark) {
			firstNewIdx = i
			break
		}
	}
	newCount := len(invocations) - firstNewIdx
	if newCount < cfg.CompactionInterval {
		return nil, false
	}

	windowStart := firstNewIdx - cfg.OverlapSize
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := len(invocations) - cfg.OverlapSize
	if windowEnd <= windowStart {
		return nil, false
	}
	return invocations[windowStart:windowEnd], true
}

// findWatermark returns the latest end timestamp encoded in any compaction
// marker event's InvocationID, or zero time when no marker exists.
func findWatermark(events []*adksession.Event) time.Time {
	var watermark time.Time
	for _, ev := range events {
		if !isCompactionMarker(ev) {
			continue
		}
		parsed, ok := parseCompactionInvocationID(ev.InvocationID)
		if !ok {
			continue
		}
		if end := time.Unix(0, parsed[1]).UTC(); end.After(watermark) {
			watermark = end
		}
	}
	return watermark
}

// buildCompactionEvent creates a compaction marker event.
// Summary is stored as Content. Start and end timestamps are encoded in
// InvocationID as "compaction_<startNano>_<endNano>".
// buildContentsDefault never sees this event: compactingService.Get applies
// BuildCompactedEventList before the runner loads the session.
func buildCompactionEvent(_ context.Context, content *genai.Content, startTS, endTS time.Time) *adksession.Event {
	invID := fmt.Sprintf("%s%d_%d", compactionInvocationBase, startTS.UnixNano(), endTS.UnixNano())
	ev := adksession.NewEvent(invID)
	ev.Author = compactionAuthor
	ev.Timestamp = endTS
	ev.Content = content
	return ev
}

// isCompactionMarker reports whether ev is a compaction marker event.
func isCompactionMarker(ev *adksession.Event) bool {
	return ev.Author == compactionAuthor &&
		strings.HasPrefix(ev.InvocationID, compactionInvocationBase)
}

// parseCompactionInvocationID parses "compaction_<startNano>_<endNano>"
// and returns [startNano, endNano].
func parseCompactionInvocationID(invID string) ([2]int64, bool) {
	rest := strings.TrimPrefix(invID, compactionInvocationBase)
	idx := strings.LastIndex(rest, "_")
	if idx < 0 {
		return [2]int64{}, false
	}
	startNano, err1 := strconv.ParseInt(rest[:idx], 10, 64)
	endNano, err2 := strconv.ParseInt(rest[idx+1:], 10, 64)
	if err1 != nil || err2 != nil {
		return [2]int64{}, false
	}
	return [2]int64{startNano, endNano}, true
}

// longestSelfContainedPrefix returns the longest prefix of events that ends
// at a point where no function call is open and no tool confirmation is pending.
func longestSelfContainedPrefix(events []*adksession.Event) []*adksession.Event {
	openCalls := map[string]bool{}
	safeIdx := 0

	for i, ev := range events {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p.FunctionCall != nil {
					openCalls[p.FunctionCall.ID] = true
				}
				if p.FunctionResponse != nil {
					delete(openCalls, p.FunctionResponse.ID)
				}
			}
		}
		if len(openCalls) == 0 && len(ev.Actions.RequestedToolConfirmations) == 0 {
			safeIdx = i + 1
		}
	}
	return events[:safeIdx]
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

// filterCompactionMarkers returns events with compaction markers removed.
func filterCompactionMarkers(events []*adksession.Event) []*adksession.Event {
	out := make([]*adksession.Event, 0, len(events))
	for _, ev := range events {
		if !isCompactionMarker(ev) {
			out = append(out, ev)
		}
	}
	return out
}

func maxTimestamp(events []*adksession.Event) time.Time {
	var t time.Time
	for _, e := range events {
		if e.Timestamp.After(t) {
			t = e.Timestamp
		}
	}
	return t
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

func estimateTokens(events []*adksession.Event) int {
	return len(serializeEvents(events)) / 4
}

// FromAgentConfig builds a Config from the kagent AgentConfig.
// Returns nil when compaction is not configured.
// SummarizerLLM must be wired separately (adapter.go buildCompactionConfig).
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

// SummarizerModelName returns the model name configured for summarization,
// or "" when none is set.
func SummarizerModelName(agentCfg *adkapiconfig.AgentConfig) string {
	if agentCfg == nil || agentCfg.ContextConfig == nil || agentCfg.ContextConfig.Compaction == nil {
		return ""
	}
	comp := agentCfg.ContextConfig.Compaction
	if comp.SummarizerModel == nil {
		return ""
	}
	type namer interface{ GetModelName() string }
	if n, ok := comp.SummarizerModel.(namer); ok {
		return n.GetModelName()
	}
	return "configured"
}
