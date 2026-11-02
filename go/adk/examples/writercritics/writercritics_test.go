// Package writercritics runs examples/langfuse/writer-critics/agent-system.yaml
// with a scripted model, so the loop, the join inside the cycle and the turn
// cap are checked without a real LLM.
package writercritics

import (
	"context"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

const documentPath = "../../../../examples/langfuse/writer-critics/agent-system.yaml"

var draftVersion = regexp.MustCompile(`draft v(\d+)`)

// scripted answers by role, which it reads from the node's system instruction.
// The writer numbers its drafts; each critic approves from a configured draft
// version on, or never when approveFrom is zero.
type scripted struct {
	writerCalls atomic.Int32
	approveFrom map[string]int // "Clarity", "Accuracy", "Style"
}

func (*scripted) Name() string { return "reviewer" }

func (m *scripted) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		var reply string
		instruction := systemText(req)
		switch {
		case strings.Contains(instruction, "You are the Writer"):
			reply = fmt.Sprintf("draft v%d", m.writerCalls.Add(1))
		default:
			reply = m.critique(instruction, lastUserText(req))
		}
		yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText(reply, genai.RoleModel), TurnComplete: true}, nil)
	}
}

func (m *scripted) critique(instruction, draft string) string {
	for critic, from := range m.approveFrom {
		if !strings.Contains(instruction, "You are the "+critic+" critic") {
			continue
		}
		match := draftVersion.FindStringSubmatch(draft)
		if from > 0 && match != nil {
			if v, _ := strconv.Atoi(match[1]); v >= from {
				return "APPROVE"
			}
		}
		return "REVISE: tighten the " + strings.ToLower(critic) + " of " + draft
	}
	return "REVISE: unknown critic"
}

func systemText(req *adkmodel.LLMRequest) string {
	if req == nil || req.Config == nil || req.Config.SystemInstruction == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range req.Config.SystemInstruction.Parts {
		if p != nil {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// lastUserText returns the text the node was given. The reviser's prompt embeds
// the previous draft, so the latest "draft vN" in it is the one a critic sees
// only when the writer produced it; critics receive the writer's raw reply.
func lastUserText(req *adkmodel.LLMRequest) string {
	for i := len(req.Contents) - 1; i >= 0; i-- {
		c := req.Contents[i]
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		var sb strings.Builder
		for _, p := range c.Parts {
			if p != nil {
				sb.WriteString(p.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
}

// run builds the document with model registered as `reviewer` and returns the
// final text the workflow emitted.
func run(t *testing.T, model *scripted, topic string) string {
	t.Helper()
	doc, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	reg := declarative.NewRegistry()
	if err := reg.RegisterModel("reviewer", model); err != nil {
		t.Fatal(err)
	}
	spec, err := reg.Parse(doc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	system, err := declarative.Build(t.Context(), spec, reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(func() { _ = system.Close() })

	sessions := session.InMemoryService()
	created, err := sessions.Create(t.Context(), &session.CreateRequest{AppName: "writer-critics", UserID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(runner.Config{AppName: "writer-critics", Agent: system.Root(), SessionService: sessions})
	if err != nil {
		t.Fatal(err)
	}
	var final string
	for ev, err := range r.Run(t.Context(), "user", created.Session.ID(), genai.NewContentFromText(topic, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if ev.Output != nil {
			final = fmt.Sprint(ev.Output)
		}
	}
	return final
}

func TestStopsWhenAllCriticsApprove(t *testing.T) {
	// Style approves only from draft v3, so the loop needs three turns.
	model := &scripted{approveFrom: map[string]int{"Clarity": 1, "Accuracy": 1, "Style": 3}}
	final := run(t, model, "why the sky is blue")

	if !strings.Contains(final, "Final draft after 3 turn(s), approved by 3 of 3 critics") || !strings.Contains(final, "draft v3") {
		t.Fatalf("final = %q", final)
	}
	if got := model.writerCalls.Load(); got != 3 {
		t.Fatalf("writer called %d times, want 3", got)
	}
}

func TestStopsAtFiveTurnsWhenACriticNeverApproves(t *testing.T) {
	model := &scripted{approveFrom: map[string]int{"Clarity": 1, "Accuracy": 0, "Style": 1}}
	final := run(t, model, "why the sky is blue")

	if !strings.Contains(final, "Final draft after 5 turn(s), approved by 2 of 3 critics") || !strings.Contains(final, "draft v5") {
		t.Fatalf("final = %q", final)
	}
	if got := model.writerCalls.Load(); got != 5 {
		t.Fatalf("writer called %d times, want exactly 5", got)
	}
}

func TestFinishesInOneTurnWhenEveryoneApproves(t *testing.T) {
	model := &scripted{approveFrom: map[string]int{"Clarity": 1, "Accuracy": 1, "Style": 1}}
	final := run(t, model, "why the sky is blue")

	if !strings.Contains(final, "Final draft after 1 turn(s), approved by 3 of 3 critics") {
		t.Fatalf("final = %q", final)
	}
	if got := model.writerCalls.Load(); got != 1 {
		t.Fatalf("writer called %d times, want 1", got)
	}
}
