// Command oneshot runs a single prompt against an agent config and prints the response.
//
// Usage:
//
//	# From a config.json file (same format as the controller produces in K8s secrets):
//	go run ./examples/oneshot -config config.json -task "What pods are running?"
//
//	# Pipe from kubectl:
//	kubectl get secret -n kagent k8s-agent -ojson | jq -r '.data."config.json"' | base64 -d > /tmp/config.json
//	go run ./examples/oneshot -config /tmp/config.json -task "List namespaces"
//
//	# Stream mode:
//	go run ./examples/oneshot -config config.json -task "Tell me a joke" -stream
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kagent-dev/kagent/go/adk/pkg/agent"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to agent config.json")
	task := flag.String("task", "", "Prompt to send to the agent (required)")
	stream := flag.Bool("stream", false, "Enable streaming output")
	logLevel := flag.String("log-level", "warn", "Log level (debug, info, warn, error)")
	flag.Parse()

	if *task == "" {
		fmt.Fprintln(os.Stderr, "error: -task is required")
		flag.Usage()
		os.Exit(1)
	}

	logger := setupLogger(*logLevel)
	ctx := logr.NewContext(context.Background(), logger)

	agentConfig, err := config.LoadAgentConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	logger.Info("Loaded config",
		"model_type", agentConfig.Model.GetType(),
		"http_tools", len(agentConfig.HttpTools),
		"sse_tools", len(agentConfig.SseTools),
		"stream", agentConfig.GetStream())

	// Override stream if flag is set
	if *stream {
		t := true
		agentConfig.Stream = &t
	}

	adkAgent, err := agent.CreateGoogleADKAgent(ctx, agentConfig, "oneshot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating agent: %v\n", err)
		os.Exit(1)
	}

	sessionService := adksession.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:        "oneshot",
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating runner: %v\n", err)
		os.Exit(1)
	}

	sess, err := sessionService.Create(ctx, &adksession.CreateRequest{
		AppName: "oneshot",
		UserID:  "user",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
		os.Exit(1)
	}

	content := &genai.Content{
		Role:  string(genai.RoleUser),
		Parts: []*genai.Part{{Text: *task}},
	}

	useStream := agentConfig.GetStream()
	streamMode := adkagent.StreamingModeNone
	if useStream {
		streamMode = adkagent.StreamingModeSSE
	}

	for ev, err := range r.Run(ctx, "user", sess.Session.ID(), content, adkagent.RunConfig{
		StreamingMode: streamMode,
	}) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
			os.Exit(1)
		}
		printEvent(ev)
	}
	fmt.Println()
}

func printEvent(ev *adksession.Event) {
	if ev == nil || ev.Content == nil {
		return
	}
	for _, part := range ev.Content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			fmt.Print(part.Text)
		}
		if part.FunctionCall != nil {
			fmt.Fprintf(os.Stderr, "[tool call: %s]\n", part.FunctionCall.Name)
		}
		if part.FunctionResponse != nil {
			fmt.Fprintf(os.Stderr, "[tool response: %s]\n", part.FunctionResponse.Name)
		}
	}
}

func setupLogger(level string) logr.Logger {
	var zapLevel zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.WarnLevel
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zapLogger, _ := cfg.Build()
	return zapr.NewLogger(zapLogger)
}
