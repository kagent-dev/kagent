package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/adk"
	"trpc.group/trpc-go/trpc-a2a-go/server"
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
	card := &server.AgentCard{
		Name:        "test_agent",
		Description: "Test agent",
		URL:         "http://localhost:8080",
		Capabilities: server.AgentCapabilities{
			Streaming: ptrTo(true), StateTransitionHistory: ptrTo(true),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             []server.AgentSkill{{ID: "test", Name: "test", Description: ptrTo("test"), Tags: []string{"test"}}},
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
