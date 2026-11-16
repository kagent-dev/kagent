// Command validate checks AgentSystem documents the way kagent delivers and the
// generic workflow-agent image runs them, without a cluster or credentials:
//
//  1. controller: size limit, strict decode, structural validation, and prompt
//     sources (ConfigMap only; Secret and env are rejected);
//  2. runtime: names resolve against the generic image's registry (any model
//     name, harness tools, declared MCP servers), and the ADK engine builds the
//     graph, which rejects cycles, converging routes, task-mode nodes and
//     schema mismatches;
//  3. optionally, -input runs the root once on the offline stub model.
//
// Run it from the go/ directory of the repository:
//
//	go run ../.agents/skills/agent-system-workflows/scripts/validate.go FILE...
//	go run ../.agents/skills/agent-system-workflows/scripts/validate.go -input "hello" -reply yes FILE
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// maxDocumentBytes mirrors the controller's limit in
// go/core/internal/translator/workflow.go.
const maxDocumentBytes = 64 << 10

type replies []string

func (r *replies) String() string     { return strings.Join(*r, ",") }
func (r *replies) Set(s string) error { *r = append(*r, s); return nil }

func main() {
	input := flag.String("input", "", "run the root once with this user message on the stub model")
	var answers replies
	flag.Var(&answers, "reply", "answer to a hitl request during -input (repeatable)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: validate [-input TEXT [-reply TEXT]...] FILE...")
		os.Exit(2)
	}
	failed := false
	for _, path := range flag.Args() {
		if err := check(context.Background(), path, *input, answers); err != nil {
			fmt.Printf("FAIL %s\n%v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("ok   %s\n", path)
	}
	if failed {
		os.Exit(1)
	}
}

func check(ctx context.Context, path, input string, answers []string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	canonical, err := controllerCheck(raw)
	if err != nil {
		return fmt.Errorf("controller would report UnsupportedConfiguration: %w", err)
	}
	workspace, err := os.MkdirTemp("", "agent-system-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)

	reg := declarative.NewRegistry()
	reg.SetModelResolver(func(_ context.Context, name string) (adkmodel.LLM, error) {
		return declarative.NewStubModel(name), nil
	})
	if err := reg.RegisterHarnessTools(declarative.HarnessConfig{Workspace: workspace}); err != nil {
		return err
	}
	spec, err := reg.Parse(canonical)
	if err != nil {
		return fmt.Errorf("generic workflow-agent image would fail at startup: %w", err)
	}
	system, err := declarative.Build(ctx, spec, reg)
	if err != nil {
		return fmt.Errorf("ADK engine rejects the graph: %w", err)
	}
	defer system.Close()
	if input == "" {
		return nil
	}
	return run(ctx, system.Root(), input, answers)
}

// controllerCheck applies the checks the controller runs on reconcile and
// returns the canonical document it would deliver. ConfigMap prompt sources
// are replaced with placeholder text, as the controller inlines them.
func controllerCheck(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("document is empty")
	}
	if len(raw) > maxDocumentBytes {
		return nil, fmt.Errorf("document exceeds %d bytes", maxDocumentBytes)
	}
	spec, err := declarative.Parse(raw)
	if err != nil {
		return nil, err
	}
	err = declarative.ResolvePrompts(spec, func(_ string, src declarative.TextSource) (string, *declarative.TextSource, error) {
		switch {
		case src.ConfigMap != nil:
			return fmt.Sprintf("<prompt from ConfigMap %s key %s>", src.ConfigMap.Name, src.ConfigMap.Key), nil, nil
		case src.Secret != nil:
			return "", nil, fmt.Errorf("secret %q: prompts cannot be read from Secrets; use a ConfigMap", src.Secret.Name)
		default:
			return "", nil, fmt.Errorf("env %q: environment prompt sources are not supported in a delivered workflow; use a ConfigMap", src.Env)
		}
	})
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	if len(canonical) > maxDocumentBytes {
		return nil, fmt.Errorf("canonical document exceeds %d bytes", maxDocumentBytes)
	}
	return canonical, nil
}

// run executes root once and prints each event's author and output, answering
// hitl requests from answers in order.
func run(ctx context.Context, root agent.Agent, input string, answers []string) error {
	sessions := session.InMemoryService()
	created, err := sessions.Create(ctx, &session.CreateRequest{AppName: "validate", UserID: "user"})
	if err != nil {
		return err
	}
	r, err := runner.New(runner.Config{AppName: "validate", Agent: root, SessionService: sessions})
	if err != nil {
		return err
	}
	message := genai.NewContentFromText(input, genai.RoleUser)
	for {
		var pending *session.RequestInput
		for ev, err := range r.Run(ctx, "user", created.Session.ID(), message, agent.RunConfig{}) {
			if err != nil {
				return fmt.Errorf("run failed: %w", err)
			}
			if ev.RequestedInput != nil {
				pending = ev.RequestedInput
				fmt.Printf("  %s asks: %s\n", ev.Author, pending.Message)
				continue
			}
			if line := eventText(ev); line != "" {
				fmt.Printf("  %s: %s\n", ev.Author, line)
			}
		}
		if pending == nil {
			return nil
		}
		if len(answers) == 0 {
			return fmt.Errorf("run is waiting for input %q; pass -reply", pending.Message)
		}
		message = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: pending.InterruptID, Name: workflow.WorkflowInputFunctionCallName,
			Response: map[string]any{"response": answers[0]},
		}}}}
		answers = answers[1:]
	}
}

func eventText(ev *session.Event) string {
	if ev.Output != nil {
		return fmt.Sprint(ev.Output)
	}
	if ev.Content == nil {
		return ""
	}
	var text strings.Builder
	for _, part := range ev.Content.Parts {
		if part != nil {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}
