package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go/pkg/adk"
)

func main() {
	cfg := &adk.AgentConfig{
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{
				Type:  "openai",
				Model: "gpt-4.1-mini",
			},
			BaseUrl: "http://127.0.0.1:8090/v1",
		},
		Instruction: "You are a test agent. The system prompt doesn't matter because we're using a mock server.",
	}
	card := &a2a.AgentCard{
		Name:        "test_agent",
		Description: "Test agent",
		URL:         "http://localhost:8080",
		Capabilities: a2a.AgentCapabilities{
			Streaming: true, StateTransitionHistory: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []a2a.AgentSkill{{ID: "test", Name: "test", Description: "test", Tags: []string{"test"}}},
	}

	// do we have mcp everything port open?
	if c, err := net.DialTimeout("tcp", "127.0.0.1:3001", time.Second); err == nil {
		c.Close()
		cfg.HttpTools = []adk.HttpMcpServerConfig{
			{
				Params: adk.StreamableHTTPConnectionParams{
					Url:     "http://127.0.0.1:3001/mcp",
					Headers: map[string]string{},
					Timeout: ptrTo(30.0),
				},
				Tools: []string{},
			},
		}
	}

	bCfg, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	bCard, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	os.WriteFile("config.json", bCfg, 0644)
	os.WriteFile("agent-card.json", bCard, 0644)
}

func ptrTo[T any](v T) *T {
	return &v
}
