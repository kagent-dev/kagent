package main

import (
	"context"
	"iter"
	"log"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/app"
)

type executor struct{}

func (executor) Execute(_ context.Context, request *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if !yield(a2atype.NewSubmittedTask(request, request.Message), nil) {
			return
		}
		message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("BYO agent response"))
		message.ContextID, message.TaskID = request.ContextID, request.TaskID
		yield(a2atype.NewStatusUpdateEvent(request, a2atype.TaskStateCompleted, message), nil)
	}
}

func (executor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(func(a2atype.Event, error) bool) {}
}

func main() {
	application, err := app.New(app.AppConfig{
		AgentCard: a2atype.AgentCard{Name: "opaque-byo", Version: "v1", Capabilities: a2atype.AgentCapabilities{Streaming: true}},
		Port:      "80", AppName: "opaque-byo",
	}, executor{})
	if err != nil {
		log.Fatal(err)
	}
	if err := application.Run(); err != nil {
		log.Fatal(err)
	}
}
