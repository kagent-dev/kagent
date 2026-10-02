package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

func TestCompactionStrategies(t *testing.T) {
	tests := []struct {
		name        string
		settings    adk.AgentCompressionConfig
		want        compaction.Config
		wantIgnored int
		wantErr     string
	}{
		{
			name:     "sliding window only",
			settings: adk.AgentCompressionConfig{CompactionInterval: new(5), OverlapSize: new(2)},
			want:     compaction.Config{CompactionInterval: 5, OverlapSize: 2},
		},
		{
			name:     "unset settings leave their strategy off",
			settings: adk.AgentCompressionConfig{CompactionInterval: new(3)},
			want:     compaction.Config{CompactionInterval: 3},
		},
		{
			name: "both strategies",
			settings: adk.AgentCompressionConfig{
				CompactionInterval: new(5), OverlapSize: new(2), TokenThreshold: new(50000), EventRetentionSize: new(10),
			},
			want: compaction.Config{CompactionInterval: 5, OverlapSize: 2, TokenThreshold: 50000, EventRetentionSize: 10},
		},
		{
			name:     "tail retention only",
			settings: adk.AgentCompressionConfig{TokenThreshold: new(50000), EventRetentionSize: new(10)},
			want:     compaction.Config{TokenThreshold: 50000, EventRetentionSize: 10},
		},
		{
			name:        "overlap without an interval is ignored",
			settings:    adk.AgentCompressionConfig{OverlapSize: new(2), TokenThreshold: new(100), EventRetentionSize: new(4)},
			want:        compaction.Config{TokenThreshold: 100, EventRetentionSize: 4},
			wantIgnored: 1,
		},
		{
			name:        "token threshold without a retention size is ignored",
			settings:    adk.AgentCompressionConfig{CompactionInterval: new(5), TokenThreshold: new(100)},
			want:        compaction.Config{CompactionInterval: 5},
			wantIgnored: 1,
		},
		{
			name:        "retention size without a token threshold is ignored",
			settings:    adk.AgentCompressionConfig{CompactionInterval: new(5), EventRetentionSize: new(4)},
			want:        compaction.Config{CompactionInterval: 5},
			wantIgnored: 1,
		},
		{
			name:     "no strategy enabled",
			settings: adk.AgentCompressionConfig{},
			wantErr:  "no compaction strategy is enabled",
		},
		{
			name:     "only an ignored setting leaves no strategy",
			settings: adk.AgentCompressionConfig{TokenThreshold: new(100)},
			wantErr:  "no compaction strategy is enabled",
		},
		{
			name:     "negative values are rejected",
			settings: adk.AgentCompressionConfig{CompactionInterval: new(-1)},
			wantErr:  "must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ignored, err := compactionStrategies(&tt.settings)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Len(t, ignored, tt.wantIgnored)
		})
	}
}

func TestCompactionConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	ctx := logr.NewContext(t.Context(), logr.Discard())
	agentModel := &adk.OpenAI{BaseModel: adk.BaseModel{Type: "openai", Model: "gpt-4.1-mini"}, BaseUrl: "http://127.0.0.1:1/v1"}
	withCompaction := func(c *adk.AgentCompressionConfig) *adk.AgentConfig {
		return &adk.AgentConfig{Model: agentModel, ContextConfig: &adk.AgentContextConfig{Compaction: c}}
	}

	t.Run("not configured", func(t *testing.T) {
		for name, agentConfig := range map[string]*adk.AgentConfig{
			"nil config":             nil,
			"without context config": {Model: agentModel},
			"without compaction":     {Model: agentModel, ContextConfig: &adk.AgentContextConfig{}},
		} {
			cfg, err := CompactionConfig(ctx, agentConfig)
			require.NoError(t, err, name)
			assert.Nil(t, cfg, name)
		}
	})

	t.Run("agent model summarizes by default", func(t *testing.T) {
		cfg, err := CompactionConfig(ctx, withCompaction(&adk.AgentCompressionConfig{CompactionInterval: new(5), OverlapSize: new(2)}))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 5, cfg.CompactionInterval)
		assert.Equal(t, 2, cfg.OverlapSize)
		assert.Nil(t, cfg.Summarizer, "the runner installs a summarizer over the agent's own model")
	})

	t.Run("dedicated summarizer model", func(t *testing.T) {
		cfg, err := CompactionConfig(ctx, withCompaction(&adk.AgentCompressionConfig{
			CompactionInterval: new(5),
			SummarizerModel:    &adk.OpenAI{BaseModel: adk.BaseModel{Type: "openai", Model: "gpt-4.1-nano"}, BaseUrl: "http://127.0.0.1:1/v1"},
		}))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.IsType(t, &compaction.LLMSummarizer{}, cfg.Summarizer)
	})

	t.Run("custom prompt runs on the agent model", func(t *testing.T) {
		cfg, err := CompactionConfig(ctx, withCompaction(&adk.AgentCompressionConfig{
			CompactionInterval: new(5),
			PromptTemplate:     "Summarize this.\n\n{conversation_history}",
		}))
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.IsType(t, &compaction.LLMSummarizer{}, cfg.Summarizer)
	})

	t.Run("custom prompt without the history placeholder is rejected", func(t *testing.T) {
		_, err := CompactionConfig(ctx, withCompaction(&adk.AgentCompressionConfig{
			CompactionInterval: new(5),
			PromptTemplate:     "Summarize this.",
		}))
		require.ErrorContains(t, err, compaction.ConversationHistoryPlaceholder)
	})

	t.Run("custom prompt without any model is rejected", func(t *testing.T) {
		agentConfig := withCompaction(&adk.AgentCompressionConfig{CompactionInterval: new(5), PromptTemplate: "{conversation_history}"})
		agentConfig.Model = nil
		_, err := CompactionConfig(ctx, agentConfig)
		require.ErrorContains(t, err, "needs a model")
	})

	t.Run("no strategy is rejected", func(t *testing.T) {
		_, err := CompactionConfig(ctx, withCompaction(&adk.AgentCompressionConfig{}))
		require.ErrorContains(t, err, "invalid context compaction configuration")
	})

	t.Run("controller config.json", func(t *testing.T) {
		// The context_config block as the controller compiles it from
		// spec.declarative.context.compaction.
		configJSON := `{
			"model": {"type": "openai", "model": "gpt-4o", "base_url": "http://127.0.0.1:1/v1"},
			"description": "Agent with context management",
			"instruction": "You are a helpful assistant.",
			"context_config": {
				"compaction": {
					"compaction_interval": 5,
					"overlap_size": 2,
					"summarizer_model": {"type": "openai", "model": "gpt-4o-mini", "base_url": "http://127.0.0.1:1/v1"},
					"prompt_template": "Summarize the following conversation events concisely.\n\n{conversation_history}",
					"token_threshold": 50000,
					"event_retention_size": 10
				}
			}
		}`
		var agentConfig adk.AgentConfig
		require.NoError(t, json.Unmarshal([]byte(configJSON), &agentConfig))

		cfg, err := CompactionConfig(ctx, &agentConfig)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 5, cfg.CompactionInterval)
		assert.Equal(t, 2, cfg.OverlapSize)
		assert.Equal(t, 50000, cfg.TokenThreshold)
		assert.Equal(t, 10, cfg.EventRetentionSize)
		assert.IsType(t, &compaction.LLMSummarizer{}, cfg.Summarizer)
	})
}

func TestSummarizerPromptTemplate(t *testing.T) {
	assert.Empty(t, summarizerPromptTemplate(""), "an empty template selects ADK's default prompt")
	custom := "Summarize.\n\n{conversation_history}"
	assert.Equal(t, custom+toolNameInstruction, summarizerPromptTemplate(custom))
}

// scriptedModel answers every request with "reply N", N being the number of
// requests it has seen, and keeps the requests so a test can check what
// history the agent was shown after a compaction. The same model serves the
// agent and, through the runner's default, the summarizer.
type scriptedModel struct {
	mu           sync.Mutex
	requests     []*adkmodel.LLMRequest
	promptTokens int32
}

func (m *scriptedModel) Name() string { return "scripted" }

func (m *scriptedModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	text := fmt.Sprintf("reply %d", len(m.requests))
	m.mu.Unlock()
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		resp := &adkmodel.LLMResponse{
			Content:      genai.NewContentFromText(text, genai.RoleModel),
			FinishReason: genai.FinishReasonStop,
		}
		if m.promptTokens > 0 {
			resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: m.promptTokens}
		}
		yield(resp, nil)
	}
}

func (m *scriptedModel) request(i int) *adkmodel.LLMRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[i]
}

func (m *scriptedModel) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

type compactingRunner struct {
	runner    *runner.Runner
	sessions  adksession.Service
	sessionID string
}

func newCompactingRunner(t *testing.T, ctx context.Context, m adkmodel.LLM, cfg *compaction.Config) *compactingRunner {
	t.Helper()
	a, err := llmagent.New(llmagent.Config{Name: "compacting", Model: m, Instruction: "Answer briefly."})
	require.NoError(t, err)
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions, Compaction: cfg})
	require.NoError(t, err)
	sess, err := sessions.Create(ctx, &adksession.CreateRequest{AppName: "test", UserID: "user"})
	require.NoError(t, err)
	return &compactingRunner{runner: r, sessions: sessions, sessionID: sess.Session.ID()}
}

func (c *compactingRunner) turn(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	for _, err := range c.runner.Run(ctx, "user", c.sessionID, genai.NewContentFromText(text, genai.RoleUser), adkagent.RunConfig{}) {
		require.NoError(t, err)
	}
}

// summaries returns the compaction records stored in the session.
func (c *compactingRunner) summaries(t *testing.T, ctx context.Context) []*adksession.EventCompaction {
	t.Helper()
	resp, err := c.sessions.Get(ctx, &adksession.GetRequest{AppName: "test", UserID: "user", SessionID: c.sessionID})
	require.NoError(t, err)
	var out []*adksession.EventCompaction
	for ev := range resp.Session.Events().All() {
		if ev.Actions.Compaction != nil {
			out = append(out, ev.Actions.Compaction)
		}
	}
	return out
}

func contentTexts(contents []*genai.Content) []string {
	var texts []string
	for _, c := range contents {
		for _, p := range c.Parts {
			if p != nil && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
	}
	return texts
}

func TestCompactionSlidingWindowFiresAfterInterval(t *testing.T) {
	ctx := logr.NewContext(t.Context(), logr.Discard())
	cfg, _, err := compactionStrategies(&adk.AgentCompressionConfig{CompactionInterval: new(2)})
	require.NoError(t, err)
	m := &scriptedModel{}
	c := newCompactingRunner(t, ctx, m, &cfg)

	c.turn(t, ctx, "turn 1")
	assert.Empty(t, c.summaries(t, ctx), "no compaction before the interval is reached")

	c.turn(t, ctx, "turn 2")
	summaries := c.summaries(t, ctx)
	require.Len(t, summaries, 1, "the second completed invocation triggers the sliding window")
	// Calls 1 and 2 answered the turns; call 3 is the runner's default
	// summarizer running on the agent's own model.
	require.Equal(t, 3, m.requestCount())
	assert.Equal(t, []string{"reply 3"}, contentTexts([]*genai.Content{summaries[0].CompactedContent}))

	c.turn(t, ctx, "turn 3")
	shown := contentTexts(m.request(3).Contents)
	assert.Contains(t, shown, "reply 3", "the summary stands in for the compacted turns")
	assert.Contains(t, shown, "turn 3")
	for _, compacted := range []string{"turn 1", "reply 1", "turn 2", "reply 2"} {
		assert.NotContains(t, shown, compacted)
	}
}

func TestCompactionTailRetentionFiresOverTokenThreshold(t *testing.T) {
	ctx := logr.NewContext(t.Context(), logr.Discard())
	cfg, _, err := compactionStrategies(&adk.AgentCompressionConfig{TokenThreshold: new(100), EventRetentionSize: new(1)})
	require.NoError(t, err)
	m := &scriptedModel{promptTokens: 5000}
	c := newCompactingRunner(t, ctx, m, &cfg)

	c.turn(t, ctx, "turn 1")
	assert.Empty(t, c.summaries(t, ctx), "the first call has no prompt token count to compare against")

	// The prompt token count reported for turn 1 exceeds the threshold, so
	// tail retention compacts everything but the retained tail before the
	// model call of turn 2.
	c.turn(t, ctx, "turn 2")
	summaries := c.summaries(t, ctx)
	require.Len(t, summaries, 1)
	shown := contentTexts(m.request(m.requestCount() - 1).Contents)
	assert.Contains(t, shown, "turn 2")
	assert.NotContains(t, shown, "turn 1", "the compacted turn is replaced by its summary")
}
