package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func main() {
	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	card := &a2atype.AgentCard{
		Name:        "test_agent",
		Description: "Test agent",
		Capabilities: a2atype.AgentCapabilities{
			Streaming: true,
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []a2atype.AgentSkill{{
			ID:          "test",
			Name:        "test",
			Description: "test",
			Tags:        []string{"test"},
		}},
		SupportedInterfaces: []*a2atype.AgentInterface{{
			URL:             "http://localhost:8080",
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
			ProtocolVersion: a2atype.Version,
		}},
	}

	// do we have mcp everything port open?
	if c, err := net.DialTimeout("tcp", "127.0.0.1:3001", time.Second); err == nil {
		c.Close()
		cfg.HttpTools = []adk.HttpMcpServerConfig{
			{
				Params: adk.StreamableHTTPConnectionParams{
					Url:     "http://127.0.0.1:3001/mcp",
					Headers: map[string]string{},
					Timeout: new(30.0),
				},
				Tools: []string{},
			},
		}
	}

	bCfg, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		logger.Error("failed to marshal agent config", "error", err)
		return
	}
	bCard, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		logger.Error("failed to marshal agent card", "error", err)
		return
	}

	if err := os.WriteFile("config.json", bCfg, 0644); err != nil {
		logger.Error("failed to write agent config", "error", err)
	}
	if err := os.WriteFile("agent-card.json", bCard, 0644); err != nil {
		logger.Error("failed to write agent card", "error", err)
	}
}
