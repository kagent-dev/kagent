package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/protobuf/types/known/durationpb"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func main() {
	logger, err := logging.New(os.Stderr, "info")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg := &configv1.ADKAgentConfig{
		Model: &configv1.ADKModelConfig{
			Model: "gpt-4.1-mini", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{
				BaseUrl: "http://127.0.0.1:8090/v1"}}},
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
		cfg.HttpTools = []*configv1.ADKHTTPMCPServerConfig{
			{
				Params: &configv1.ADKHTTPConnectionParams{
					Url:     "http://127.0.0.1:3001/mcp",
					Headers: map[string]string{},
					Timeout: durationpb.New(30 * time.Second),
				},
				Tools: []string{},
			},
		}
	}

	bCfg, err := runtimeconfig.Marshal(&configv1.ADKConfig{Agent: cfg})
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
