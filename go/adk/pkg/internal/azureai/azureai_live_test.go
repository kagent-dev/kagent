package azureai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type recordingTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	urls []string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.urls = append(t.urls, req.URL.String())
	t.mu.Unlock()
	return t.base.RoundTrip(req)
}

func (t *recordingTransport) urlsCopy() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.urls))
	copy(out, t.urls)
	return out
}

func skipUnlessLiveAzure(t *testing.T) (endpoint, deployment, apiKey string) {
	t.Helper()
	if os.Getenv("AZURE_LIVE") != "1" {
		t.Skip("set AZURE_LIVE=1 to run Foundry live tests")
	}
	apiKey = os.Getenv("AZURE_OPENAI_API_KEY")
	endpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
	deployment = os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if apiKey == "" || endpoint == "" {
		t.Skip("AZURE_OPENAI_API_KEY and AZURE_OPENAI_ENDPOINT are required")
	}
	if deployment == "" {
		deployment = "gpt-4.1"
	}
	return endpoint, deployment, apiKey
}

func TestLiveAzureFoundryChatCompletionsPath(t *testing.T) {
	endpoint, deployment, apiKey := skipUnlessLiveAzure(t)
	rec := &recordingTransport{base: http.DefaultTransport}
	client, err := NewOpenAIClient(ClientConfig{
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: "2024-06-01",
		APIKey:     apiKey,
		HTTPClient: &http.Client{Transport: rec, Timeout: 60 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(deployment),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Reply with exactly: pong")},
	})
	if err != nil {
		t.Fatalf("Chat.Completions.New: %v", err)
	}
	if resp.Choices[0].Message.Content == "" {
		t.Fatal("empty chat completion content")
	}
	if !strings.Contains(strings.ToLower(resp.Choices[0].Message.Content), "pong") {
		t.Fatalf("content = %q, want pong", resp.Choices[0].Message.Content)
	}
	joined := strings.Join(rec.urlsCopy(), "\n")
	t.Logf("chatCompletions URLs:\n%s", joined)
	if !strings.Contains(joined, "/chat/completions") {
		t.Fatalf("did not observe /chat/completions, urls=%q", joined)
	}
	if !strings.Contains(joined, "api-version=") {
		t.Fatalf("chat completions URL missing api-version, urls=%q", joined)
	}
}

func TestLiveAzureFoundryResponsesPath(t *testing.T) {
	endpoint, deployment, apiKey := skipUnlessLiveAzure(t)
	rec := &recordingTransport{base: http.DefaultTransport}
	client, err := NewOpenAIClient(ClientConfig{
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: "2024-06-01",
		APIKey:     apiKey,
		Responses:  true,
		HTTPClient: &http.Client{Transport: rec, Timeout: 60 * time.Second},
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Responses.New(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(deployment),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("Reply with exactly: pong")},
	})
	if err != nil {
		t.Fatalf("Responses.New: %v", err)
	}
	text := strings.TrimSpace(resp.OutputText())
	t.Logf("responses text=%q", text)
	if text == "" {
		t.Fatal("empty responses output")
	}
	if !strings.Contains(strings.ToLower(text), "pong") {
		t.Fatalf("content = %q, want pong", text)
	}
	joined := strings.Join(rec.urlsCopy(), "\n")
	t.Logf("responses URLs:\n%s", joined)
	if !strings.Contains(joined, "/openai/v1/responses") {
		t.Fatalf("did not observe /openai/v1/responses, urls=%q", joined)
	}
	if strings.Contains(joined, "api-version=") {
		t.Fatalf("responses URL must not include api-version, urls=%q", joined)
	}
}
