// Command kagent-claude runs the Claude Harness runtime adapter.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/adapter"
	"github.com/kagent-dev/kagent/go/harness/claude/internal/session"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
)

const (
	configEnv    = "KAGENT_CONFIG_JSON"
	agentCardEnv = "KAGENT_AGENT_CARD_JSON"
	dataDir      = "/data"
	privatePort  = "80"
)

func main() {
	check := flag.Bool("check", false, "validate configuration and Claude version, then exit")
	flag.Parse()
	if err := run(*check, os.Getenv, os.Environ()); err != nil {
		log.Fatal(err)
	}
}

func run(check bool, getenv func(string) string, environment []string) error {
	configJSON, err := requiredEnvironment(getenv, configEnv)
	if err != nil {
		return err
	}
	agentCardJSON, err := requiredEnvironment(getenv, agentCardEnv)
	if err != nil {
		return err
	}
	var card a2atype.AgentCard
	if err := json.Unmarshal(agentCardJSON, &card); err != nil {
		return fmt.Errorf("decode agent card: %w", err)
	}
	if strings.TrimSpace(card.Name) == "" {
		return fmt.Errorf("agent card name is required")
	}

	runner, err := adapter.New(adapter.Input{
		ConfigJSON: configJSON,
		Workspace:  dataDir + "/workspace", DurableDir: dataDir,
		EphemeralDir: "/tmp/kagent-claude",
		Environment:  environment,
	})
	if err != nil {
		return fmt.Errorf("configure Claude Harness: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.Validate(validateCtx); err != nil {
		return err
	}
	if check {
		return nil
	}
	store, err := session.New(dataDir + "/adapter")
	if err != nil {
		return err
	}
	executor, err := runtimea2a.New(runner, store)
	if err != nil {
		return err
	}
	application, err := app.New(app.AppConfig{AgentCard: card, Port: privatePort, AppName: card.Name}, executor)
	if err != nil {
		return fmt.Errorf("construct private A2A app: %w", err)
	}
	return application.Run()
}

func requiredEnvironment(getenv func(string) string, name string) ([]byte, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	return []byte(value), nil
}
