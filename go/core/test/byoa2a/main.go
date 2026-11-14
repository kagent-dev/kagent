package main

import (
	"cmp"
	"context"
	"errors"
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

const (
	blockFirstRestoredSendFlag = "block-first-restored-send"
	restoredSendMarker         = "/data/e2e-restored-send"
	restoredSendArmed          = "armed"
	restoredSendReleased       = "released"
)

type canceledSendInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

var _ a2asrv.CallInterceptor = canceledSendInterceptor{}

// Before blocks message delivery until cancellation, before any task is saved.
func (canceledSendInterceptor) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if _, sending := request.Payload.(*a2atype.SendMessageRequest); sending {
		<-ctx.Done()
		return ctx, nil, ctx.Err()
	}
	return ctx, nil, nil
}

// restoredSendGate arms the golden snapshot, blocks sends on its first restore,
// and lets subsequent restores execute normally. The marker advances before
// readiness, so RUNNING is a deterministic barrier for canceling an unaccepted
// send. Its persisted value also proves the canceled actor was checkpointed.
func restoredSendGate() ([]a2asrv.RequestHandlerOption, error) {
	marker, err := os.ReadFile(restoredSendMarker)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.WriteFile(restoredSendMarker, []byte(restoredSendArmed), 0o600)
	}
	if err != nil {
		return nil, err
	}
	switch string(marker) {
	case restoredSendArmed:
		if err := os.WriteFile(restoredSendMarker, []byte(restoredSendReleased), 0o600); err != nil {
			return nil, err
		}
		return []a2asrv.RequestHandlerOption{a2asrv.WithCallInterceptors(canceledSendInterceptor{})}, nil
	case restoredSendReleased:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected restored-send marker %q", marker)
	}
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
	blockFirstRestoredSend := flag.Bool(blockFirstRestoredSendFlag, false, "block sends before persistence on the first restored activation")
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
	var handlerOpts []a2asrv.RequestHandlerOption
	if *blockFirstRestoredSend {
		handlerOpts, err = restoredSendGate()
		if err != nil {
			logger.Error("failed to prepare restored-send gate", "error", err)
			return
		}
	}
	application, err := app.New(app.AppConfig{ControlPlaneURL: runtime.ControlPlaneUrl, MaxContentLength: runtime.MaxContentLength,
		AgentCard: a2atype.AgentCard{Name: "opaque-byo", Version: "v1", Capabilities: a2atype.AgentCapabilities{Streaming: true}},
		Port:      fmt.Sprint(cmp.Or(runtime.GetPort(), 80)), AppName: cmp.Or(runtime.Name, "opaque-byo"), Logger: logger,
		HandlerOpts: handlerOpts,
	}, executor{})
	if err != nil {
		logger.Error("failed to create A2A application", "error", err)
		return
	}
	if err := application.Run(); err != nil {
		logger.Error("a2A application stopped", "error", err)
	}
}
