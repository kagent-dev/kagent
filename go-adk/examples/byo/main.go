// Package main demonstrates how to build a BYO (Bring Your Own) agent using
// the Go ADK's pkg/app builder. Instead of relying on the declarative config
// image, you implement a2asrv.AgentExecutor and pass it to app.New().
//
// The example uses a2a.NewEventQueue to wrap the raw event queue. EventQueue
// mirrors artifact events as status events for the UI (streaming text and
// history population) and injects the last artifact text into the final
// status when no explicit message is provided.
//
// Run:
//
//	go run ./examples/byo/
//
// Test with curl:
//
//	curl -s http://localhost:8080/.well-known/agent.json | jq .
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/kagent-dev/kagent/go-adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/app"
)

// EchoExecutor is a minimal AgentExecutor that echoes the user's message back.
// Replace this with your own agent logic.
type EchoExecutor struct{}

func (e *EchoExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	q := a2a.NewEventQueue(queue, reqCtx)

	text := "no message"
	if reqCtx.Message != nil {
		for _, part := range reqCtx.Message.Parts {
			if tp, ok := part.(a2atype.TextPart); ok {
				text = tp.Text
				break
			}
		}
	}

	// Write the echo as an artifact. EventQueue automatically mirrors it
	// as a "working" status event for the UI.
	reply := fmt.Sprintf("Echo: %s", text)
	artifact := a2atype.NewArtifactEvent(reqCtx, a2atype.TextPart{Text: reply})
	if err := q.Write(ctx, artifact); err != nil {
		return err
	}

	// Write the final status. EventQueue injects the artifact text as the
	// message since we don't provide one explicitly.
	done := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil)
	done.Final = true
	return q.Write(ctx, done)
}

func (e *EchoExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

var _ a2asrv.AgentExecutor = (*EchoExecutor)(nil)

func main() {
	kagentApp, err := app.New(app.AppConfig{
		AgentCard: a2atype.AgentCard{
			Name:        "echo-agent",
			Description: "A minimal BYO echo agent built with Go ADK",
			Version:     "1.0.0",
			URL:         "http://localhost:8080",
			Capabilities: a2atype.AgentCapabilities{
				Streaming: true,
			},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
			Skills: []a2atype.AgentSkill{
				{
					ID:          "echo",
					Name:        "Echo",
					Description: "Echoes back whatever you send",
				},
			},
		},
		Port:            "8080",
		ShutdownTimeout: 5 * time.Second,
	}, &EchoExecutor{})
	if err != nil {
		log.Fatalf("Failed to create app: %v", err)
	}

	if err := kagentApp.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
