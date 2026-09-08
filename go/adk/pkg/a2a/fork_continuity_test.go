// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package a2a

import (
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	localsession "github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestKAgentExecutorForkRetainsConversation(t *testing.T) {
	for _, user := range []string{"", "alice"} {
		t.Run("user="+user, func(t *testing.T) {
			var wantContext, wantUser string
			var wantEvents int
			agent, err := adkagent.New(adkagent.Config{
				Name: "memory-agent",
				Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
					return func(yield func(*adksession.Event, error) bool) {
						require.Equal(t, wantContext, ic.Session().ID())
						require.Equal(t, wantUser, ic.Session().UserID())
						// The runner appends this turn's input before invoking the agent.
						require.Equal(t, wantEvents, ic.Session().Events().Len())
						event := adksession.NewEvent(ic, ic.InvocationID())
						event.Author = ic.Agent().Name()
						event.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText("remembered", genai.RoleModel)}
						yield(event, nil)
					}
				},
			})
			require.NoError(t, err)
			open := func(path string) *KAgentExecutor {
				t.Helper()
				svc, err := localsession.NewLocalSessionService("sqlite:///" + path)
				require.NoError(t, err)
				return NewKAgentExecutor(KAgentExecutorConfig{
					AppName: "app", SessionService: svc, Logger: slog.New(slog.DiscardHandler),
					RunnerConfig: runner.Config{AppName: "app", Agent: agent},
				})
			}
			send := func(executor *KAgentExecutor, contextID string, eventCount int) {
				t.Helper()
				wantContext, wantUser, wantEvents = contextID, user, eventCount
				if wantUser == "" {
					wantUser = "A2A_USER_" + contextID
				}
				ctx, callCtx := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"x-user-id": {user}}))
				ctx, _, err := UserIDCallInterceptor().Before(ctx, callCtx, nil)
				require.NoError(t, err)
				message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("continue"))
				message.ContextID = contextID
				request := &a2asrv.ExecutorContext{TaskID: a2atype.NewTaskID(), ContextID: contextID, Message: message}
				completed := false
				for event, err := range executor.Execute(ctx, request) {
					require.NoError(t, err)
					require.Equal(t, contextID, event.TaskInfo().ContextID)
					if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateCompleted {
						completed = true
					}
				}
				require.True(t, completed)
			}
			sourcePath := filepath.Join(t.TempDir(), "source.db")
			source := open(sourcePath)
			send(source, "source", 1)
			// Capture the quiescent SQLite file, then let the source advance.
			snapshot, err := os.ReadFile(sourcePath)
			require.NoError(t, err)
			send(source, "source", 3)
			forkPath := filepath.Join(t.TempDir(), "fork.db")
			require.NoError(t, os.WriteFile(forkPath, snapshot, 0o600))
			send(open(forkPath), "source", 3)
			send(open(forkPath), "source", 5)
			snapshot, err = os.ReadFile(forkPath)
			require.NoError(t, err)
			fork2Path := filepath.Join(t.TempDir(), "fork2.db")
			require.NoError(t, os.WriteFile(fork2Path, snapshot, 0o600))
			send(open(fork2Path), "source", 7)
			send(source, "source", 5)
		})
	}
}
