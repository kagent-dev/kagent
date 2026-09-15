package agent

import (
	"context"
	"iter"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// toolCallingLLM asks for one tool call, then answers with text.
type toolCallingLLM struct{ calls int }

func (m *toolCallingLLM) Name() string { return "tool-calling" }

func (m *toolCallingLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	m.calls++
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.calls == 1 {
			part := genai.NewPartFromFunctionCall("get_pods", map[string]any{"namespace": "default"})
			part.FunctionCall.ID = "call-1"
			yield(&model.LLMResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{part}}}, nil)
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("two pods", genai.RoleModel)}, nil)
	}
}

type getPodsInput struct {
	Namespace string `json:"namespace"`
}

// TestAfterToolCallback_RecordsToolCallOnExecuteToolSpan drives a real tool
// call through ADK's runner and asserts the gen_ai.tool.call.* attributes land
// on the execute_tool span, which is the span active in the after-tool
// callback context.
func TestAfterToolCallback_RecordsToolCallOnExecuteToolSpan(t *testing.T) {
	t.Setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "true")
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	getPods, err := functiontool.New(functiontool.Config{Name: "get_pods", Description: "List pods."},
		func(adkagent.Context, getPodsInput) (map[string]any, error) {
			return map[string]any{"pods": []string{"a", "b"}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	llmAgent, err := llmagent.New(llmagent.Config{
		Name:               "agent",
		Model:              &toolCallingLLM{},
		Tools:              []tool.Tool{getPods},
		AfterToolCallbacks: []llmagent.AfterToolCallback{makeAfterToolCallback(logger)},
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "app", Agent: llmAgent, SessionService: sessions})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "app", UserID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range r.Run(t.Context(), "user", sess.Session.ID(), genai.NewContentFromText("pods?", genai.RoleUser), adkagent.RunConfig{}) {
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	var found bool
	for _, span := range exporter.GetSpans() {
		if span.Name != "execute_tool get_pods" {
			continue
		}
		found = true
		attrs := map[string]attribute.Value{}
		for _, a := range span.Attributes {
			attrs[string(a.Key)] = a.Value
		}
		if got, want := attrs["gen_ai.tool.call.arguments"].AsString(), `{"namespace":"default"}`; got != want {
			t.Errorf("gen_ai.tool.call.arguments = %q, want %q", got, want)
		}
		if got, want := attrs["gen_ai.tool.call.result"].AsString(), `{"pods":["a","b"]}`; got != want {
			t.Errorf("gen_ai.tool.call.result = %q, want %q", got, want)
		}
		if got := attrs["gen_ai.tool.name"].AsString(); got != "get_pods" {
			t.Errorf("span missing ADK's own gen_ai.tool.name, got %q", got)
		}
	}
	if !found {
		names := []string{}
		for _, s := range exporter.GetSpans() {
			names = append(names, s.Name)
		}
		t.Fatalf("no execute_tool span exported; spans = %v", names)
	}
}
