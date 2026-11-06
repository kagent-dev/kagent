package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

const weatherOutputSchema = `{"type":"object","additionalProperties":false,"required":["city","temp_c"],"properties":{"city":{"type":"string"},"temp_c":{"type":"number"}}}`

// TestStructuredOutputKeepsToolsUsableOnOpenAIFormatProviders covers backends
// that enforce response_format by constraining generation to the schema. Such a
// backend can only emit the final JSON, so an agent that sends the schema with
// its tools never gets a tool call. The schema must reach the model as a final
// answer tool instead, and the agent must finish when the model calls it.
func TestStructuredOutputKeepsToolsUsableOnOpenAIFormatProviders(t *testing.T) {
	providers := []struct {
		name  string
		model func(url string) *configv1.ADKModelConfig
	}{
		{name: "an OpenAI-format model", model: func(url string) *configv1.ADKModelConfig {
			return &configv1.ADKModelConfig{Model: "deepseek-v4-flash", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: url}}}
		}},
		{name: "an Azure OpenAI model", model: func(url string) *configv1.ADKModelConfig {
			return &configv1.ADKModelConfig{Model: "gpt-4o", Provider: &configv1.ADKModelConfig_AzureOpenai{AzureOpenai: &configv1.ADKAzureOpenAI{
				Endpoint: url, Deployment: "gpt-4o", ApiVersion: "2024-10-21"}}}
		}},
		{name: "a Foundry model", model: func(url string) *configv1.ADKModelConfig {
			return &configv1.ADKModelConfig{Model: "gpt-4o", Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{
				Endpoint: url, Deployment: "gpt-4o", ApiVersion: "2024-10-21"}}}
		}},
	}
	for _, provider := range providers {
		for _, stream := range []bool{false, true} {
			delivery := map[bool]string{false: "a complete response", true: "a streamed response"}[stream]
			t.Run(fmt.Sprintf("Given %s and an agent with a tool and an output schema, when the model calls the tool and then submits its final answer in %s, assert that no request constrains the schema and the answer ends the run", provider.name, delivery), func(t *testing.T) {
				newStructuredOutputFixture(t, provider.model).
					Streaming(stream).
					GivenAgentWithToolAndOutputSchema().
					WhenModelCallsToolThenSubmitsFinalAnswer().
					AssertThatNoRequestSentTheSchemaAsResponseFormat().
					AssertThatEveryRequestOfferedTheFinalAnswerToolWithTheSchema().
					AssertThatToolRanOnceAndRunEndedAfterFinalAnswer().
					AssertThatFinalResponseIsTheSubmittedAnswer()
			})
		}
	}
}

// TestStructuredOutputRejectsAnAnswerThatBreaksTheSchema pins down what a
// nonconforming submission does. The tool refuses it, ADK still ends the run on
// the call, and the executor then fails the Task because the published answer
// does not match the schema, as documented for every schema failure.
func TestStructuredOutputRejectsAnAnswerThatBreaksTheSchema(t *testing.T) {
	t.Run("Given an agent with an output schema, when the model submits an answer that lacks a required field, assert that the run ends without a conforming answer", func(t *testing.T) {
		newStructuredOutputFixture(t, func(url string) *configv1.ADKModelConfig {
			return &configv1.ADKModelConfig{Model: "deepseek-v4-flash", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: url}}}
		}).
			GivenAgentWithToolAndOutputSchema().
			WhenModelSubmitsAnAnswerMissingARequiredField().
			AssertThatRunEndedAfterOneRequestWithoutAConformingAnswer()
	})
}

// TestStructuredOutputOffersOneFinalAnswerToolWhenADKProvidesIt checks that an
// OpenAI-format model whose name makes ADK add its own final answer tool does
// not also receive kagent's, which would send two tools with the same name.
func TestStructuredOutputOffersOneFinalAnswerToolWhenADKProvidesIt(t *testing.T) {
	t.Run("Given an OpenAI-format provider with a Gemini model name, when the agent runs, assert that exactly one final answer tool is offered", func(t *testing.T) {
		newStructuredOutputFixture(t, func(url string) *configv1.ADKModelConfig {
			return &configv1.ADKModelConfig{Model: "gemini-2.5-flash", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: url}}}
		}).
			GivenAgentWithToolAndOutputSchema().
			WhenModelAnswersWithoutCallingATool().
			AssertThatFinalAnswerToolWasOfferedOnce()
	})
}

// recordedRequest is the part of a model request the scenarios assert on.
type recordedRequest struct {
	hasResponseFormat bool
	toolNames         []string
	finalAnswerParams map[string]any
}

type structuredOutputFixture struct {
	t        *testing.T
	newModel func(url string) *configv1.ADKModelConfig

	stream   bool
	server   *httptest.Server
	replies  []scriptedReply
	mu       sync.Mutex
	requests []recordedRequest
	toolRuns atomic.Int32
	root     agent.Agent
	events   []*session.Event
}

func newStructuredOutputFixture(t *testing.T, newModel func(url string) *configv1.ADKModelConfig) *structuredOutputFixture {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("AZURE_OPENAI_API_KEY", "test-key")
	t.Setenv("FOUNDRY_API_KEY", "test-key")
	return &structuredOutputFixture{t: t, newModel: newModel}
}

func (f *structuredOutputFixture) Streaming(stream bool) *structuredOutputFixture {
	f.stream = stream
	return f
}

func (f *structuredOutputFixture) GivenAgentWithToolAndOutputSchema() *structuredOutputFixture {
	f.t.Helper()
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	f.t.Cleanup(f.server.Close)

	weather, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Get current weather for a city."},
		func(_ agent.Context, args struct {
			City string `json:"city"`
		}) (map[string]any, error) {
			f.toolRuns.Add(1)
			return map[string]any{"temp_c": 21}, nil
		})
	require.NoError(f.t, err)

	config := &configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{
		Model:           f.newModel(f.server.URL),
		Instruction:     "Call get_weather first, then answer.",
		SkillsDirectory: f.t.TempDir(),
		Output:          &configv1.ADKOutputConfig{JsonSchema: []byte(weatherOutputSchema), Sha256: "test"},
	}}
	f.root, err = CreateGoogleADKAgent(f.t.Context(), config, "root", nil, weather)
	require.NoError(f.t, err)
	return f
}

func (f *structuredOutputFixture) WhenModelCallsToolThenSubmitsFinalAnswer() *structuredOutputFixture {
	f.t.Helper()
	f.replies = []scriptedReply{
		{toolName: "get_weather", arguments: `{"city":"Paris"}`},
		{toolName: "set_model_response", arguments: `{"city":"Paris","temp_c":21}`},
	}
	return f.run()
}

func (f *structuredOutputFixture) WhenModelSubmitsAnAnswerMissingARequiredField() *structuredOutputFixture {
	f.t.Helper()
	f.replies = []scriptedReply{{toolName: "set_model_response", arguments: `{"city":"Paris"}`}}
	return f.run()
}

func (f *structuredOutputFixture) WhenModelAnswersWithoutCallingATool() *structuredOutputFixture {
	f.t.Helper()
	f.replies = []scriptedReply{{text: `{"city":"Paris","temp_c":21}`}}
	return f.run()
}

func (f *structuredOutputFixture) run() *structuredOutputFixture {
	f.t.Helper()
	run, err := runner.NewInMemory("app", f.root)
	require.NoError(f.t, err)
	var runConfig agent.RunConfig
	if f.stream {
		runConfig.StreamingMode = agent.StreamingModeSSE
	}
	for event, err := range run.Run(f.t.Context(), "user", "session", genai.NewContentFromText("What is the weather in Paris?", genai.RoleUser), runConfig) {
		require.NoError(f.t, err)
		f.events = append(f.events, event)
	}
	return f
}

func (f *structuredOutputFixture) AssertThatNoRequestSentTheSchemaAsResponseFormat() *structuredOutputFixture {
	f.t.Helper()
	require.NotEmpty(f.t, f.requests, "the model endpoint received no request")
	for i, request := range f.requests {
		require.False(f.t, request.hasResponseFormat, "request %d constrained the model with response_format", i+1)
	}
	return f
}

func (f *structuredOutputFixture) AssertThatEveryRequestOfferedTheFinalAnswerToolWithTheSchema() *structuredOutputFixture {
	f.t.Helper()
	var want map[string]any
	require.NoError(f.t, json.Unmarshal([]byte(weatherOutputSchema), &want))
	for i, request := range f.requests {
		require.Contains(f.t, request.toolNames, "get_weather", "request %d", i+1)
		require.Equal(f.t, want, request.finalAnswerParams, "request %d did not offer the final answer tool with the schema", i+1)
	}
	return f
}

func (f *structuredOutputFixture) AssertThatToolRanOnceAndRunEndedAfterFinalAnswer() *structuredOutputFixture {
	f.t.Helper()
	require.EqualValues(f.t, 1, f.toolRuns.Load(), "the tool should have run exactly once")
	require.Len(f.t, f.requests, 2, "the run should end on the final answer call, not ask the model again")
	return f
}

func (f *structuredOutputFixture) AssertThatFinalResponseIsTheSubmittedAnswer() *structuredOutputFixture {
	f.t.Helper()
	var final *session.Event
	for _, event := range f.events {
		if event.Author == "root" && event.IsFinalResponse() {
			final = event
		}
	}
	require.NotNil(f.t, final, "the run produced no final response")
	var text strings.Builder
	for _, part := range final.Content.Parts {
		text.WriteString(part.Text)
	}
	require.JSONEq(f.t, `{"city":"Paris","temp_c":21}`, text.String())
	return f
}

func (f *structuredOutputFixture) AssertThatRunEndedAfterOneRequestWithoutAConformingAnswer() *structuredOutputFixture {
	f.t.Helper()
	require.Len(f.t, f.requests, 1, "the run should end on the rejected call, not ask the model again")
	var final *session.Event
	for _, event := range f.events {
		if event.Author == "root" && event.IsFinalResponse() {
			final = event
		}
	}
	require.NotNil(f.t, final, "the run produced no final response")
	var text strings.Builder
	for _, part := range final.Content.Parts {
		text.WriteString(part.Text)
	}
	var answer map[string]any
	require.NoError(f.t, json.Unmarshal([]byte(text.String()), &answer))
	require.Contains(f.t, answer, "error", "the final response should report the rejection")
	require.NotContains(f.t, answer, "temp_c", "the schema-breaking answer must not be published as if it were valid")
	return f
}

func (f *structuredOutputFixture) AssertThatFinalAnswerToolWasOfferedOnce() *structuredOutputFixture {
	f.t.Helper()
	require.NotEmpty(f.t, f.requests, "the model endpoint received no request")
	count := 0
	for _, name := range f.requests[0].toolNames {
		if name == "set_model_response" {
			count++
		}
	}
	require.Equal(f.t, 1, count)
	return f
}

// serve records each request and answers with the next scripted reply, so a
// test can script a multi-turn conversation.
func (f *structuredOutputFixture) serve(w http.ResponseWriter, r *http.Request) {
	encoded, _ := io.ReadAll(r.Body)
	var body struct {
		Stream         bool            `json:"stream"`
		ResponseFormat json.RawMessage `json:"response_format"`
		Tools          []struct {
			Function struct {
				Name       string         `json:"name"`
				Parameters map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(encoded, &body)
	request := recordedRequest{hasResponseFormat: len(body.ResponseFormat) > 0}
	for _, tool := range body.Tools {
		request.toolNames = append(request.toolNames, tool.Function.Name)
		if tool.Function.Name == "set_model_response" {
			request.finalAnswerParams = tool.Function.Parameters
		}
	}

	f.mu.Lock()
	f.requests = append(f.requests, request)
	n := len(f.requests)
	f.mu.Unlock()

	reply := scriptedReply{text: `{"unexpected":"extra request"}`}
	if n <= len(f.replies) {
		reply = f.replies[n-1]
	}
	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, reply.streamed())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, reply.complete())
}

// scriptedReply is one model turn: a tool call, or plain text when toolName is
// empty.
type scriptedReply struct {
	toolName  string
	arguments string
	text      string
}

func (r scriptedReply) complete() string {
	if r.toolName == "" {
		text, _ := json.Marshal(r.text)
		return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, text)
	}
	arguments, _ := json.Marshal(r.arguments)
	return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":%q,"arguments":%s}}]},"finish_reason":"tool_calls"}]}`, r.toolName, arguments)
}

// streamed renders the reply as Chat Completions server-sent events.
func (r scriptedReply) streamed() string {
	chunk := func(delta, finish string) string {
		return fmt.Sprintf("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, finish)
	}
	if r.toolName == "" {
		text, _ := json.Marshal(r.text)
		return chunk(fmt.Sprintf(`{"role":"assistant","content":%s}`, text), "null") + chunk(`{}`, `"stop"`) + "data: [DONE]\n\n"
	}
	arguments, _ := json.Marshal(r.arguments)
	call := fmt.Sprintf(`{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":%q,"arguments":%s}}]}`, r.toolName, arguments)
	return chunk(call, "null") + chunk(`{}`, `"tool_calls"`) + "data: [DONE]\n\n"
}
