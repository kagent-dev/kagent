package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"iter"
	"os"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/kagent-dev/kagent/go/adk/pkg/app"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type executor struct{}

// blockedMessage holds only the selected E2E request before its first task save.
// Other messages, including the follow-up after suspension, execute normally.
type blockedMessage struct {
	a2asrv.PassthroughCallInterceptor
	id string
}

func (b blockedMessage) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send.Message != nil && send.Message.ID == b.id {
		<-ctx.Done()
		return ctx, nil, ctx.Err()
	}
	return ctx, nil, nil
}

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
	blockMessageID := flag.String("block-message-id", "", "hold this E2E message before task admission until cancellation")
	flag.Parse()
	if err := config.MaterializeFromBootstrap(context.Background(), "/config"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	compiled, err := config.LoadAgentConfig("/config/config.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runtime := config.ProcessConfig(compiled)
	logger, err := logging.New(os.Stderr, cmp.Or(runtime.LogLevel, "info"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	appConfig := app.AppConfig{ControlPlaneURL: runtime.ControlPlaneUrl, MaxContentLength: runtime.MaxContentLength,
		AgentCard: a2atype.AgentCard{Name: "opaque-byo", Version: "v1", Capabilities: a2atype.AgentCapabilities{Streaming: true}},
		Port:      fmt.Sprint(cmp.Or(runtime.GetPort(), 80)), AppName: cmp.Or(runtime.Name, "opaque-byo"), Logger: logger,
	}
	if *blockMessageID != "" {
		appConfig.HandlerOpts = []a2asrv.RequestHandlerOption{a2asrv.WithCallInterceptors(blockedMessage{id: *blockMessageID})}
	}
	application, err := app.New(appConfig, executor{})
	if err != nil {
		logger.Error("failed to create A2A application", "error", err)
		return
	}
	if err := application.Run(); err != nil {
		logger.Error("a2A application stopped", "error", err)
	}
}
