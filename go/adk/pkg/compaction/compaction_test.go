package compaction

import (
	"context"
	"testing"
	"time"

	adkapiconfig "github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

var (
	t0 = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Second)
	t2 = t0.Add(2 * time.Second)
	t3 = t0.Add(3 * time.Second)
	t4 = t0.Add(4 * time.Second)
	t5 = t0.Add(5 * time.Second)
	t6 = t0.Add(6 * time.Second)
)

func makeCompactionMarkerEvent(startTS, endTS time.Time, summaryText string) *adksession.Event {
	return buildCompactionEvent(context.Background(), &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: summaryText}},
	}, startTS, endTS)
}

func makeEvent(ts time.Time, invocationID string) *adksession.Event {
	return &adksession.Event{
		ID:           "ev-" + ts.Format("150405.000"),
		Timestamp:    ts,
		InvocationID: invocationID,
	}
}

func makeTextEvent(ts time.Time, invocationID, author, text string) *adksession.Event {
	ev := makeEvent(ts, invocationID)
	ev.Author = author
	ev.LLMResponse.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: text}},
	}
	return ev
}

func makeFuncCallEvent(ts time.Time, invocationID, callID, funcName string) *adksession.Event {
	ev := makeEvent(ts, invocationID)
	ev.LLMResponse.Content = &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: callID, Name: funcName}}},
	}
	return ev
}

func makeFuncRespEvent(ts time.Time, invocationID, callID, funcName string) *adksession.Event {
	ev := makeEvent(ts, invocationID)
	ev.LLMResponse.Content = &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: callID, Name: funcName}}},
	}
	return ev
}

// ---------------------------------------------------------------------------
// groupByInvocation
// ---------------------------------------------------------------------------

func TestGroupByInvocation(t *testing.T) {
	events := []*adksession.Event{
		makeEvent(t0, "inv1"),
		makeEvent(t1, "inv1"),
		makeEvent(t2, "inv2"),
		makeEvent(t3, ""),
	}
	groups := groupByInvocation(events)
	require.Len(t, groups, 3)
	require.Equal(t, "inv1", groups[0].invocationID)
	require.Len(t, groups[0].events, 2)
	require.Equal(t, "inv2", groups[1].invocationID)
	require.Equal(t, noInvocationSentinel, groups[2].invocationID)
}

// ---------------------------------------------------------------------------
// findWatermark
// ---------------------------------------------------------------------------

func TestFindWatermark_None(t *testing.T) {
	require.True(t, findWatermark([]*adksession.Event{makeEvent(t0, "inv1")}).IsZero())
}

func TestFindWatermark_Single(t *testing.T) {
	marker := makeCompactionMarkerEvent(t0, t3, "summary")
	require.Equal(t, t3, findWatermark([]*adksession.Event{makeEvent(t0, "inv1"), marker}))
}

func TestFindWatermark_MultiplePicksLatest(t *testing.T) {
	c1 := makeCompactionMarkerEvent(t0, t2, "summary1")
	c2 := makeCompactionMarkerEvent(t0, t3, "summary2")
	require.Equal(t, t3, findWatermark([]*adksession.Event{makeEvent(t0, "inv1"), c1, c2}))
}

// ---------------------------------------------------------------------------
// decideSlidingWindow
// ---------------------------------------------------------------------------

func TestDecideSlidingWindow_EmptyNoFire(t *testing.T) {
	cfg := &Config{CompactionInterval: 5, OverlapSize: 2}
	_, ok := decideSlidingWindow(nil, time.Time{}, cfg)
	require.False(t, ok)
}

func TestDecideSlidingWindow_InsufficientNewInvocations(t *testing.T) {
	inv := []invocationGroup{
		{invocationID: "inv1", events: []*adksession.Event{makeEvent(t0, "inv1")}},
		{invocationID: "inv2", events: []*adksession.Event{makeEvent(t1, "inv2")}},
		{invocationID: "inv3", events: []*adksession.Event{makeEvent(t2, "inv3")}},
	}
	_, ok := decideSlidingWindow(inv, time.Time{}, &Config{CompactionInterval: 5, OverlapSize: 2})
	require.False(t, ok)
}

func TestDecideSlidingWindow_ExactThreshold(t *testing.T) {
	inv := []invocationGroup{
		{invocationID: "inv1", events: []*adksession.Event{makeEvent(t0, "inv1")}},
		{invocationID: "inv2", events: []*adksession.Event{makeEvent(t1, "inv2")}},
		{invocationID: "inv3", events: []*adksession.Event{makeEvent(t2, "inv3")}},
		{invocationID: "inv4", events: []*adksession.Event{makeEvent(t3, "inv4")}},
		{invocationID: "inv5", events: []*adksession.Event{makeEvent(t4, "inv5")}},
	}
	// interval=5, overlap=2 → window = inv[0:3]
	window, ok := decideSlidingWindow(inv, time.Time{}, &Config{CompactionInterval: 5, OverlapSize: 2})
	require.True(t, ok)
	require.Len(t, window, 3)
	require.Equal(t, "inv1", window[0].invocationID)
	require.Equal(t, "inv3", window[2].invocationID)
}

func TestDecideSlidingWindow_WatermarkPreventsEarlyFire(t *testing.T) {
	inv := []invocationGroup{
		{invocationID: "inv1", events: []*adksession.Event{makeEvent(t0, "inv1")}},
		{invocationID: "inv2", events: []*adksession.Event{makeEvent(t1, "inv2")}},
		{invocationID: "inv3", events: []*adksession.Event{makeEvent(t2, "inv3")}},
		{invocationID: "inv4", events: []*adksession.Event{makeEvent(t3, "inv4")}},
		{invocationID: "inv5", events: []*adksession.Event{makeEvent(t4, "inv5")}},
	}
	// watermark = t1 → inv1 and inv2 are "old" (timestamp <= watermark).
	// inv3, inv4, inv5 are new (3 < 5) → no fire.
	_, ok := decideSlidingWindow(inv, t1, &Config{CompactionInterval: 5, OverlapSize: 2})
	require.False(t, ok)
}

func TestDecideSlidingWindow_WatermarkTriggersWithSix(t *testing.T) {
	// 6 invocations, watermark covers inv1 (t0).
	// inv2-inv6 are new (5 >= interval 5) → fire.
	// windowStart = max(0, firstNewIdx-overlap) = max(0,1-2) = 0
	// windowEnd = 6-2 = 4 → inv[0:4]
	inv := make([]invocationGroup, 6)
	for i := range inv {
		ts := t0.Add(time.Duration(i) * time.Second)
		id := "inv" + string(rune('1'+i))
		inv[i] = invocationGroup{invocationID: id, events: []*adksession.Event{makeEvent(ts, id)}}
	}
	window, ok := decideSlidingWindow(inv, t0, &Config{CompactionInterval: 5, OverlapSize: 2})
	require.True(t, ok)
	require.Len(t, window, 4) // inv[0:4]
}

// ---------------------------------------------------------------------------
// longestSelfContainedPrefix
// ---------------------------------------------------------------------------

func TestLongestSelfContainedPrefix_Empty(t *testing.T) {
	require.Empty(t, longestSelfContainedPrefix(nil))
}

func TestLongestSelfContainedPrefix_NoOpenCalls(t *testing.T) {
	events := []*adksession.Event{
		makeTextEvent(t0, "inv1", "agent", "hello"),
		makeTextEvent(t1, "inv1", "user", "world"),
	}
	require.Len(t, longestSelfContainedPrefix(events), 2)
}

func TestLongestSelfContainedPrefix_OpenFunctionCallBlocks(t *testing.T) {
	events := []*adksession.Event{
		makeTextEvent(t0, "inv1", "agent", "before"),
		makeFuncCallEvent(t1, "inv1", "call-1", "my_tool"),
	}
	// Safe boundary is after the text event only.
	require.Len(t, longestSelfContainedPrefix(events), 1)
}

func TestLongestSelfContainedPrefix_ResolvedCallCompactable(t *testing.T) {
	events := []*adksession.Event{
		makeFuncCallEvent(t0, "inv1", "call-1", "my_tool"),
		makeFuncRespEvent(t1, "inv1", "call-1", "my_tool"),
		makeTextEvent(t2, "inv1", "agent", "done"),
	}
	require.Len(t, longestSelfContainedPrefix(events), 3)
}

func TestLongestSelfContainedPrefix_HITLConfirmationBlocks(t *testing.T) {
	hitlEvent := makeTextEvent(t1, "inv1", "agent", "confirm?")
	hitlEvent.Actions.RequestedToolConfirmations = map[string]toolconfirmation.ToolConfirmation{
		"call-1": {},
	}
	events := []*adksession.Event{
		makeTextEvent(t0, "inv1", "agent", "before"),
		hitlEvent,
	}
	require.Len(t, longestSelfContainedPrefix(events), 1)
}

func TestLongestSelfContainedPrefix_OpenCallAtStartBlocksAll(t *testing.T) {
	events := []*adksession.Event{
		makeFuncCallEvent(t0, "inv1", "call-1", "my_tool"),
		makeTextEvent(t1, "inv1", "agent", "waiting"),
	}
	require.Empty(t, longestSelfContainedPrefix(events))
}

// ---------------------------------------------------------------------------
// FromAgentConfig
// ---------------------------------------------------------------------------

func TestFromAgentConfig_Nil(t *testing.T) {
	cfg, err := FromAgentConfig(nil)
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestFromAgentConfig_NoCompaction(t *testing.T) {
	cfg, err := FromAgentConfig(&adkapiconfig.AgentConfig{})
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestFromAgentConfig_Defaults(t *testing.T) {
	interval := 0
	agentCfg := &adkapiconfig.AgentConfig{
		ContextConfig: &adkapiconfig.AgentContextConfig{
			Compaction: &adkapiconfig.AgentCompressionConfig{
				CompactionInterval: &interval, // zero → use default
			},
		},
	}
	cfg, err := FromAgentConfig(agentCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, defaultCompactionInterval, cfg.CompactionInterval)
	require.Equal(t, defaultOverlapSize, cfg.OverlapSize)
	require.Equal(t, defaultSummaryPrompt, cfg.PromptTemplate)
}

func TestFromAgentConfig_CustomValues(t *testing.T) {
	interval := 10
	overlap := 3
	threshold := 50000
	retention := 20
	prompt := "custom: {{events}}"
	agentCfg := &adkapiconfig.AgentConfig{
		ContextConfig: &adkapiconfig.AgentContextConfig{
			Compaction: &adkapiconfig.AgentCompressionConfig{
				CompactionInterval: &interval,
				OverlapSize:        &overlap,
				TokenThreshold:     &threshold,
				EventRetentionSize: &retention,
				PromptTemplate:     prompt,
			},
		},
	}
	cfg, err := FromAgentConfig(agentCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, 10, cfg.CompactionInterval)
	require.Equal(t, 3, cfg.OverlapSize)
	require.Equal(t, 50000, cfg.TokenThreshold)
	require.Equal(t, 20, cfg.EventRetentionSize)
	require.Equal(t, prompt, cfg.PromptTemplate)
}
