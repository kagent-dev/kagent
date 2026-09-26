package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/client-go/tools/clientcmd"
)

func TestE2ECLIAgentCatalogAndSessionLifecycle(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		templateName := createInteractionTemplate(t, harness, startInteractionMock(t))
		binary := kagentCLI(t)
		baseArgs := []string{
			"--api-url", "http://" + target,
			"--gateway-url", "http://" + target,
			"--namespace", "kagent",
			"--user-id", "e2e",
		}
		run := func(ctx context.Context, args ...string) string {
			return runKagentCLI(t, ctx, binary, append(append([]string{}, baseArgs...), args...)...)
		}

		listedTemplates := run(t.Context(), "get", "agent")
		if !strings.Contains(listedTemplates, templateName) || !strings.Contains(listedTemplates, "True") {
			t.Fatalf("list Agents stdout = %q, want ready template %s", listedTemplates, templateName)
		}
		templateJSON := run(t.Context(), "--output-format", "json", "get", "agent", templateName)
		if !json.Valid([]byte(templateJSON)) || !strings.Contains(templateJSON, `"name":"`+templateName+`"`) ||
			!strings.Contains(templateJSON, `"status":"True"`) {
			t.Fatalf("get Agent stdout = %q, want ready template %s as JSON", templateJSON, templateName)
		}

		requestID := uuid.NewString()
		createArgs := []string{
			"--output-format", "json", "create", "session",
			"--agent", templateName, "--request-id", requestID,
		}
		createdJSON := run(t.Context(), createArgs...)
		var created apiv1alpha1.CreateSessionResponse
		if err := protojson.Unmarshal([]byte(createdJSON), &created); err != nil {
			t.Fatalf("decode create Session stdout %q: %v", createdJSON, err)
		}
		session := created.GetSession()
		if session.GetId() == "" || session.GetState() != apiv1alpha1.SessionState_SESSION_STATE_READY {
			t.Fatalf("created Session = %#v, want ID and READY state", session)
		}
		deleted := false
		t.Cleanup(func() {
			if !deleted {
				run(context.Background(), "delete", "session", session.GetId())
			}
		})

		replayedJSON := run(t.Context(), createArgs...)
		var replayed apiv1alpha1.CreateSessionResponse
		if err := protojson.Unmarshal([]byte(replayedJSON), &replayed); err != nil {
			t.Fatalf("decode replayed create stdout %q: %v", replayedJSON, err)
		}
		if replayed.GetSession().GetId() != session.GetId() {
			t.Fatalf("replayed create ID = %q, want %q", replayed.GetSession().GetId(), session.GetId())
		}

		listedSessions := run(t.Context(), "get", "session")
		if !strings.Contains(listedSessions, session.GetId()) {
			t.Fatalf("list Sessions stdout = %q, want session %s", listedSessions, session.GetId())
		}
		gotSession := run(t.Context(), "--output-format", "json", "get", "session", session.GetId())
		if !json.Valid([]byte(gotSession)) || !strings.Contains(gotSession, session.GetId()) {
			t.Fatalf("get Session stdout = %q, want session %s as JSON", gotSession, session.GetId())
		}

		deletedJSON := run(t.Context(), "--output-format", "json", "delete", "session", session.GetId())
		deleted = true
		var deletedResponse apiv1alpha1.DeleteSessionResponse
		if err := protojson.Unmarshal([]byte(deletedJSON), &deletedResponse); err != nil {
			t.Fatalf("decode delete Session stdout %q: %v", deletedJSON, err)
		}
		if deletedResponse.GetSession().GetState() != apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			t.Fatalf("deleted Session state = %s, want DELETED", deletedResponse.GetSession().GetState())
		}
	})
}

func TestE2ECLISessionDiscoveryAndInvoke(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		fixture := newInteractionFixture(t, harness, target, startInteractionMock(t))
		binary := kagentCLI(t)
		baseArgs := []string{
			"--api-url", "http://" + target,
			"--gateway-url", "http://" + target,
			"--namespace", "kagent",
			"--user-id", "e2e",
		}

		listOutput := runKagentCLI(t, fixture.ctx, binary, append(baseArgs, "get", "session")...)
		if !strings.Contains(listOutput, fixture.sessionID) {
			t.Fatalf("list Sessions stdout = %q, want session %s", listOutput, fixture.sessionID)
		}

		getArgs := append(append([]string{}, baseArgs...), "--output-format", "json", "get", "session", fixture.sessionID)
		getOutput := runKagentCLI(t, fixture.ctx, binary, getArgs...)
		if !json.Valid([]byte(getOutput)) || !strings.Contains(getOutput, fixture.sessionID) {
			t.Fatalf("get Session stdout = %q, want JSON for session %s", getOutput, fixture.sessionID)
		}

		tests := []struct {
			name   string
			format string
			stream bool
		}{
			{name: "table", format: "table"},
			{name: "table stream", format: "table", stream: true},
			{name: "json", format: "json"},
			{name: "json stream", format: "json", stream: true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				args := append(append([]string{}, baseArgs...),
					"--output-format", tt.format,
					"invoke",
					"--session", fixture.sessionID,
					"--task", "What is 2+2?",
				)
				if tt.stream {
					args = append(args, "--stream")
				}
				stdout := runKagentCLI(t, fixture.ctx, binary, args...)
				if tt.format == "table" {
					if got := strings.TrimSpace(stdout); got != "The answer is 4." {
						t.Fatalf("CLI stdout = %q, want final response once", got)
					}
					return
				}
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				if !tt.stream && len(lines) != 1 {
					t.Fatalf("non-streaming JSON stdout has %d lines, want 1", len(lines))
				}
				for _, line := range lines {
					if !json.Valid([]byte(line)) {
						t.Fatalf("CLI stdout line is not JSON: %q", line)
					}
				}
			})
		}
	})
}

func runKagentCLI(t *testing.T, ctx context.Context, binary string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, binary, args...)
	kubeconfig := os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}
	command.Env = append(os.Environ(), "HOME="+t.TempDir(), clientcmd.RecommendedConfigPathEnvVar+"="+kubeconfig)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run CLI: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("CLI stderr = %q, want empty", stderr.String())
	}
	return stdout.String()
}

func kagentCLI(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("KAGENT_E2E_CLI")
	if binary == "" {
		t.Fatal("KAGENT_E2E_CLI is not set; run E2E tests through `make -C go e2e`")
	}
	return binary
}
