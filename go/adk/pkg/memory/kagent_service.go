package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/embedding"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/api/adk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/memory"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// KagentMemoryService implements memory.Service by storing memories
// via the Kagent backend API (backed by pgvector).
type KagentMemoryService struct {
	agentName       string
	apiURL          string
	client          *http.Client
	ttlDays         int
	embeddingClient *embedding.Client
	model           adkmodel.LLM // Optional: for session summarization
}

// Config for creating a new KagentMemoryService.
type Config struct {
	// AgentName is used as the namespace for memory storage
	AgentName string
	// APIURL is the base URL of the Kagent API (e.g., "http://kagent-controller:8083")
	APIURL string
	// HTTPClient for making requests (optional, uses http.DefaultClient if nil)
	HTTPClient *http.Client
	// TTLDays is the TTL for memory entries in days (0 uses server default of 15)
	TTLDays int
	// EmbeddingConfig for generating embeddings (optional but recommended)
	EmbeddingConfig *adk.EmbeddingConfig
	// Model for session summarization (optional)
	Model adkmodel.LLM
}

// New creates a new KagentMemoryService.
func New(cfg Config) (*KagentMemoryService, error) {
	if cfg.AgentName == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if cfg.APIURL == "" {
		return nil, fmt.Errorf("API URL is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	if cfg.EmbeddingConfig == nil {
		return nil, fmt.Errorf("embedding config is required")
	}
	embClient, err := embedding.New(embedding.Config{
		EmbeddingConfig: cfg.EmbeddingConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding client: %w", err)
	}

	return &KagentMemoryService{
		agentName:       cfg.AgentName,
		apiURL:          strings.TrimSuffix(cfg.APIURL, "/"),
		client:          client,
		ttlDays:         cfg.TTLDays,
		embeddingClient: embClient,
		model:           cfg.Model,
	}, nil
}

// AddSessionToMemory implements memory.Service.AddSessionToMemory.
// It extracts content from the session, optionally summarizes it, generates embeddings,
// and stores it via the Kagent API.
func (s *KagentMemoryService) AddSessionToMemory(ctx context.Context, session adksession.Session) error {
	ctx, span := telemetry.StartMemorySpan(ctx, telemetry.SpanMemoryWrite,
		telemetry.MemoryOperationSave, telemetry.MemoryScopeUser, s.agentName)
	defer span.End()

	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Adding session to memory", "sessionID", session.ID(), "userID", session.UserID())

	// Extract text content from session events
	rawContent := s.extractSessionContent(session)
	if rawContent == "" {
		log.V(1).Info("No content to add to memory", "sessionID", session.ID())
		span.SetAttributes(attribute.Int(telemetry.AttrMemoryItemCount, 0))
		return nil
	}

	// Summarize content if model is available. Raw session text is stored as-is
	// (source=user); LLM-summarized facts are stored as source=agent_inference.
	contents := []string{rawContent}
	source := telemetry.MemorySourceUser
	if s.model != nil {
		summarized, err := s.summarizeContent(ctx, rawContent)
		if err != nil {
			log.V(1).Info("Summarization failed, using raw content", "error", err)
		} else if len(summarized) > 0 {
			contents = summarized
			source = telemetry.MemorySourceAgentInference
			log.V(1).Info("Summarized content", "items", len(contents))
		}
	}
	span.SetAttributes(attribute.String(telemetry.AttrMemorySource, source))

	// Generate embeddings
	embeddings, err := s.embeddingClient.Generate(ctx, contents)
	if err != nil {
		return s.recordSpanError(span, fmt.Errorf("failed to generate embeddings: %w", err))
	}

	if len(embeddings) != len(contents) {
		return s.recordSpanError(span, fmt.Errorf("embedding count mismatch: got %d, expected %d", len(embeddings), len(contents)))
	}

	// Store each content item with its embedding
	for i, content := range contents {
		if err := s.storeMemory(ctx, session.UserID(), content, embeddings[i]); err != nil {
			return s.recordSpanError(span, fmt.Errorf("failed to store memory %d: %w", i, err))
		}
	}

	span.SetAttributes(attribute.Int(telemetry.AttrMemoryItemCount, len(contents)))
	log.Info("Successfully added session to memory", "sessionID", session.ID(), "items", len(contents))
	return nil
}

// SaveMemoryItem stores a single user-provided content item as a first-class
// memory write and emits a memory.write span. Unlike AddSessionToMemory it stores
// the content verbatim (source=user) without LLM summarization, so it intentionally
// emits no memory.consolidate span — consolidation is reserved for the summarizing
// session-ingestion path. This is the path the save_memory tool drives, i.e. the
// write an agent actually performs at runtime.
func (s *KagentMemoryService) SaveMemoryItem(ctx context.Context, userID, content string) error {
	ctx, span := telemetry.StartMemorySpan(ctx, telemetry.SpanMemoryWrite,
		telemetry.MemoryOperationSave, telemetry.MemoryScopeUser, s.agentName)
	defer span.End()
	span.SetAttributes(attribute.String(telemetry.AttrMemorySource, telemetry.MemorySourceUser))

	if content == "" {
		return s.recordSpanError(span, fmt.Errorf("missing required parameter: content"))
	}

	embeddings, err := s.embeddingClient.Generate(ctx, []string{content})
	if err != nil {
		return s.recordSpanError(span, fmt.Errorf("failed to generate embedding: %w", err))
	}
	var vector []float32
	if len(embeddings) > 0 {
		vector = embeddings[0]
	}
	if vector == nil {
		return s.recordSpanError(span, fmt.Errorf("embedding generation returned no vectors"))
	}

	if err := s.storeMemory(ctx, userID, content, vector); err != nil {
		return s.recordSpanError(span, fmt.Errorf("failed to save memory: %w", err))
	}

	span.SetAttributes(attribute.Int(telemetry.AttrMemoryItemCount, 1))
	return nil
}

// recordSpanError marks span as failed, records err on it, and returns err so
// callers can `return s.recordSpanError(span, err)` in a single line.
func (s *KagentMemoryService) recordSpanError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// storeMemory stores a single memory item via the Kagent API.
func (s *KagentMemoryService) storeMemory(ctx context.Context, userID, content string, vector []float32) error {
	req := addSessionRequest{
		AgentName: s.agentName,
		UserID:    userID,
		Content:   content,
		Vector:    vector,
		TTLDays:   s.ttlDays,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/api/memories/sessions", s.apiURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return nil
}

// SearchMemory implements memory.Service.SearchMemory.
// It searches for relevant memories using vector similarity.
func (s *KagentMemoryService) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	// Recall runs before LLM dispatch, so this span attaches as a child of the
	// active invoke_agent span when one is present in ctx.
	ctx, span := telemetry.StartMemorySpan(ctx, telemetry.SpanMemoryRead,
		telemetry.MemoryOperationPrefetch, telemetry.MemoryScopeUser, s.agentName)
	defer span.End()

	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Searching memory", "query", req.Query, "userID", req.UserID)

	if req.Query == "" {
		setMemoryReadResult(span, 0)
		return &memory.SearchResponse{Memories: []memory.Entry{}}, nil
	}

	// Generate embedding for the query. Without a valid embedding we cannot
	// perform similarity search, so return empty results on failure.
	embeddings, err := s.embeddingClient.Generate(ctx, []string{req.Query})
	if err != nil {
		log.Error(err, "Failed to generate query embedding, returning empty results")
		span.RecordError(err)
		setMemoryReadResult(span, 0)
		return &memory.SearchResponse{Memories: []memory.Entry{}}, nil
	}
	var vector []float32
	if len(embeddings) > 0 {
		vector = embeddings[0]
	}
	if vector == nil {
		setMemoryReadResult(span, 0)
		return &memory.SearchResponse{Memories: []memory.Entry{}}, nil
	}

	// Prepare API request
	searchReq := searchRequest{
		AgentName: s.agentName,
		UserID:    req.UserID,
		Vector:    vector,
		Limit:     5,
		MinScore:  0.3,
	}

	body, err := json.Marshal(searchReq)
	if err != nil {
		return nil, s.recordSpanError(span, fmt.Errorf("failed to marshal request: %w", err))
	}

	// Make HTTP request
	url := fmt.Sprintf("%s/api/memories/search", s.apiURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, s.recordSpanError(span, fmt.Errorf("failed to create request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, s.recordSpanError(span, fmt.Errorf("failed to make request: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, s.recordSpanError(span, fmt.Errorf("API returned status %d", resp.StatusCode))
	}

	// Parse response
	var results []searchResultItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, s.recordSpanError(span, fmt.Errorf("failed to decode response: %w", err))
	}

	// Convert to memory.Entry
	memories := make([]memory.Entry, 0, len(results))
	for _, item := range results {
		content := &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: item.Content},
			},
		}
		memories = append(memories, memory.Entry{
			Content: content,
		})
	}

	setMemoryReadResult(span, len(memories))
	log.Info("Found memories", "count", len(memories), "query", req.Query)
	return &memory.SearchResponse{Memories: memories}, nil
}

// setMemoryReadResult stamps the recall outcome onto a memory.read span: the number
// of items returned and whether any memory passed the pgvector min-score gate
// (injected) or all were filtered out (filtered).
func setMemoryReadResult(span trace.Span, count int) {
	injection := telemetry.MemoryInjectionFiltered
	if count > 0 {
		injection = telemetry.MemoryInjectionInjected
	}
	span.SetAttributes(
		attribute.Int(telemetry.AttrMemoryItemCount, count),
		attribute.String(telemetry.AttrMemoryInjectionResult, injection),
	)
}

// summarizeContent uses the LLM to extract key facts from conversation content.
// Returns a list of summarized facts, or the original content wrapped in a slice if summarization fails.
func (s *KagentMemoryService) summarizeContent(ctx context.Context, content string) ([]string, error) {
	ctx, span := telemetry.StartMemorySpan(ctx, telemetry.SpanMemoryConsolidate,
		telemetry.MemoryOperationExtract, telemetry.MemoryScopeUser, s.agentName)
	defer span.End()

	log := logr.FromContextOrDiscard(ctx)

	if content == "" {
		span.SetAttributes(attribute.Int(telemetry.AttrMemoryItemCount, 0))
		return nil, nil
	}

	prompt := fmt.Sprintf(`Extract and summarize the key information from this conversation that would be useful for the agent to remember in future interactions.

Focus on:
- User preferences, decisions, and explicit requests
- Important facts mentioned (names, dates, project names, etc.)
- Contextual information that provides background
- Lessons learned from the conversation

You MUST output a JSON list of strings, where each string is a distinct fact or memory.
Example: ["User prefers dark mode", "Meeting scheduled for Friday", "Always use the save_memory tool to store memory"]

Do not include any preamble or markdown formatting like `+"```json"+`.
Output ONLY the JSON list.

Conversation:
%s

Summary (JSON List):`, content)

	// Create LLM request
	req := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: prompt},
				},
			},
		},
	}

	// Generate content using the model (streaming)
	iter := s.model.GenerateContent(ctx, req, false)

	// Collect response
	var summaryText strings.Builder
	for resp, err := range iter {
		if err != nil {
			return nil, fmt.Errorf("failed to generate summary: %w", err)
		}

		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					summaryText.WriteString(part.Text)
				}
			}
		}
	}

	summary := strings.TrimSpace(summaryText.String())
	if summary == "" {
		log.V(1).Info("Empty summary returned, using original content")
		return []string{content}, nil
	}

	// Clean up markdown formatting
	summary = strings.TrimPrefix(summary, "```json")
	summary = strings.TrimPrefix(summary, "```")
	summary = strings.TrimSuffix(summary, "```")
	summary = strings.TrimSpace(summary)

	// Parse JSON
	var facts []string
	if err := json.Unmarshal([]byte(summary), &facts); err != nil {
		log.V(1).Info("Failed to parse summary as JSON, using original content", "error", err, "output", summary)
		return []string{content}, nil
	}

	// Validate all items are strings
	if slices.Contains(facts, "") {
		log.V(1).Info("Summary contains empty strings, using original content")
		return []string{content}, nil
	}

	log.V(1).Info("Successfully summarized content", "facts", len(facts))
	return facts, nil
}

// extractSessionContent extracts text content from session events.
func (s *KagentMemoryService) extractSessionContent(session adksession.Session) string {
	var parts []string

	events := session.Events()
	for i := 0; i < events.Len(); i++ {
		event := events.At(i)
		if event.Content == nil || len(event.Content.Parts) == 0 {
			continue
		}

		role := event.Author
		if role == "" {
			role = "unknown"
		}

		for _, part := range event.Content.Parts {
			// Skip function calls
			if part.FunctionCall != nil {
				continue
			}

			// Get text content
			text := part.Text
			if text == "" && part.FunctionResponse != nil {
				// TODO: Extract content from function response if needed
				continue
			}

			if text != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", role, text))
			}
		}
	}

	return strings.Join(parts, "\n")
}

// Request/response types for Kagent API

type addSessionRequest struct {
	AgentName string    `json:"agent_name"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Vector    []float32 `json:"vector"`
	TTLDays   int       `json:"ttl_days,omitempty"`
}

type searchRequest struct {
	AgentName string    `json:"agent_name"`
	UserID    string    `json:"user_id"`
	Vector    []float32 `json:"vector"`
	Limit     int       `json:"limit"`
	MinScore  float64   `json:"min_score"`
}

type searchResultItem struct {
	ID      string  `json:"id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}
