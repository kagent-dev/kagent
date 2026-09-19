// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package a2a

import (
	"iter"
	"log/slog"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	localsession "github.com/kagent-dev/kagent/go/adk/pkg/session"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// The person of a turn — the x-kagent-user metadata the gateway forwards once
// it has resolved the caller — reaches the agent's invocation context, where
// the model transport reads it (models.UserFromContext). Neither the session
// owner in x-user-id nor the caller's bearer in authorization ever does: in a
// flow that puts a raw token there, no model call names it.
func TestKAgentExecutorNamesTheResolvedUserToModelCalls(t *testing.T) {
	for _, user := range []string{"", "alice"} {
		t.Run("user="+user, func(t *testing.T) {
			var gotUser string
			agent, err := adkagent.New(adkagent.Config{
				Name: "identity-agent",
				Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
					return func(yield func(*adksession.Event, error) bool) {
						gotUser = models.UserFromContext(ic)
						event := adksession.NewEvent(ic, ic.InvocationID())
						event.Author = ic.Agent().Name()
						event.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText("done", genai.RoleModel)}
						yield(event, nil)
					}
				},
			})
			require.NoError(t, err)
			svc, err := localsession.NewLocalSessionService("sqlite:///" + filepath.Join(t.TempDir(), "sessions.db"))
			require.NoError(t, err)
			executor, err := NewKAgentExecutor(KAgentExecutorConfig{
				AppName: "app", SessionService: svc, Logger: slog.New(slog.DiscardHandler),
				RunnerConfig: runner.Config{AppName: "app", Agent: agent},
			})
			require.NoError(t, err)
			// What the gateway forwards to an actor: the caller's bearer (stored for
			// API-key passthrough), the session owner, and — once resolved — the person.
			params := map[string][]string{
				"authorization": {"Bearer eyJraWQiOiJyYXctdG9rZW4ifQ.eyJlbWFpbCI6ImFsaWNlIn0.sig"},
				"x-user-id":     {"eyJraWQiOiJyYXctdG9rZW4ifQ.not-a-person"},
			}
			if user != "" {
				params[adk.UserHeader] = []string{user}
			}
			ctx, callCtx := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
			ctx, _, err = UserIDCallInterceptor().Before(ctx, callCtx, nil)
			require.NoError(t, err)
			message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))
			message.ContextID = "conversation"
			request := &a2asrv.ExecutorContext{TaskID: a2atype.NewTaskID(), ContextID: "conversation", Message: message}
			for _, err := range executor.Execute(ctx, request) {
				require.NoError(t, err)
			}
			require.Equal(t, user, gotUser)
		})
	}
}
