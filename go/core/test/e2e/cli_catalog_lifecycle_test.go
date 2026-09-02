package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
)

func TestE2ECLIAgentTemplateCatalogAndInstanceLifecycle(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	templateName := createInteractionTemplate(t, startInteractionMock(t))
	binary := kagentCLI(t)
	baseArgs := []string{
		"--grpc-url", target,
		"--grpc-tls=false",
		"--namespace", "kagent",
		"--user-id", "e2e",
	}
	run := func(ctx context.Context, args ...string) string {
		return runKagentCLI(t, ctx, binary, append(append([]string{}, baseArgs...), args...)...)
	}

	listedTemplates := run(t.Context(), "get", "agent-template")
	if !strings.Contains(listedTemplates, templateName) || !strings.Contains(listedTemplates, "TRUE") {
		t.Fatalf("list AgentTemplates stdout = %q, want ready template %s", listedTemplates, templateName)
	}
	templateJSON := run(t.Context(), "--output-format", "json", "get", "agent-template", templateName)
	if !json.Valid([]byte(templateJSON)) || !strings.Contains(templateJSON, `"name":"`+templateName+`"`) ||
		!strings.Contains(templateJSON, `"status":"True"`) {
		t.Fatalf("get AgentTemplate stdout = %q, want ready template %s as JSON", templateJSON, templateName)
	}

	requestID := uuid.NewString()
	createArgs := []string{
		"--output-format", "json", "create", "agent-instance",
		"--harness", "kagent", "--agent-template", templateName, "--request-id", requestID,
	}
	createdJSON := run(t.Context(), createArgs...)
	var created apiv1alpha1.CreateAgentInstanceResponse
	if err := protojson.Unmarshal([]byte(createdJSON), &created); err != nil {
		t.Fatalf("decode create AgentInstance stdout %q: %v", createdJSON, err)
	}
	instance := created.GetAgentInstance()
	if instance.GetId() == "" || instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		t.Fatalf("created AgentInstance = %#v, want ID and READY state", instance)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			run(context.Background(), "delete", "agent-instance", instance.GetId())
		}
	})

	replayedJSON := run(t.Context(), createArgs...)
	var replayed apiv1alpha1.CreateAgentInstanceResponse
	if err := protojson.Unmarshal([]byte(replayedJSON), &replayed); err != nil {
		t.Fatalf("decode replayed create stdout %q: %v", replayedJSON, err)
	}
	if replayed.GetAgentInstance().GetId() != instance.GetId() {
		t.Fatalf("replayed create ID = %q, want %q", replayed.GetAgentInstance().GetId(), instance.GetId())
	}

	listedInstances := run(t.Context(), "get", "agent-instance")
	if !strings.Contains(listedInstances, instance.GetId()) {
		t.Fatalf("list AgentInstances stdout = %q, want instance %s", listedInstances, instance.GetId())
	}
	gotInstance := run(t.Context(), "--output-format", "json", "get", "agent-instance", instance.GetId())
	if !json.Valid([]byte(gotInstance)) || !strings.Contains(gotInstance, instance.GetId()) {
		t.Fatalf("get AgentInstance stdout = %q, want instance %s as JSON", gotInstance, instance.GetId())
	}

	deletedJSON := run(t.Context(), "--output-format", "json", "delete", "agent-instance", instance.GetId())
	deleted = true
	var deletedResponse apiv1alpha1.DeleteAgentInstanceResponse
	if err := protojson.Unmarshal([]byte(deletedJSON), &deletedResponse); err != nil {
		t.Fatalf("decode delete AgentInstance stdout %q: %v", deletedJSON, err)
	}
	if deletedResponse.GetAgentInstance().GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED {
		t.Fatalf("deleted AgentInstance state = %s, want DELETED", deletedResponse.GetAgentInstance().GetState())
	}
}

func TestE2ECLIAgentTemplateLifecycle(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	model := createInteractionModel(t, kube, startInteractionMock(t), nil)
	templateName := "cli-lifecycle-" + uuid.NewString()
	manifestPath := filepath.Join(t.TempDir(), "agent-template.yaml")
	binary := kagentCLI(t)
	baseArgs := []string{
		"--grpc-url", target,
		"--grpc-tls=false",
		"--namespace", "kagent",
		"--user-id", "e2e",
	}
	run := func(args ...string) string {
		return runKagentCLI(t, t.Context(), binary, append(append([]string{}, baseArgs...), args...)...)
	}
	writeManifest := func(systemPrompt, runtime string) {
		t.Helper()
		manifest := fmt.Sprintf(`apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: %s
  labels:
    kagent.dev/e2e-runtime: %s
spec:
  modelConfig:
    name: %s
  systemPrompt: %q
`, templateName, runtime, model.Name, systemPrompt)
		if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
			t.Fatalf("write AgentTemplate manifest: %v", err)
		}
	}
	assertTemplate := func(wantPrompt, wantRuntime string) {
		t.Helper()
		template := &v1alpha3.AgentTemplate{}
		if err := kube.Get(t.Context(), types.NamespacedName{Namespace: "kagent", Name: templateName}, template); err != nil {
			t.Fatalf("get AgentTemplate %q: %v", templateName, err)
		}
		if template.Spec.SystemPrompt != wantPrompt {
			t.Fatalf("AgentTemplate systemPrompt = %q, want %q", template.Spec.SystemPrompt, wantPrompt)
		}
		if got := template.Labels["kagent.dev/e2e-runtime"]; got != wantRuntime {
			t.Fatalf("AgentTemplate runtime label = %q, want %q", got, wantRuntime)
		}
	}
	t.Cleanup(func() {
		template := &v1alpha3.AgentTemplate{}
		if err := kube.Get(context.Background(), types.NamespacedName{Namespace: "kagent", Name: templateName}, template); err == nil {
			if err := kube.Delete(context.Background(), template); err != nil {
				t.Errorf("cleanup AgentTemplate %q: %v", templateName, err)
			}
		}
	})

	writeManifest("applied through the CLI", "kagent")
	applied := run("--output-format", "json", "apply", "-f", manifestPath)
	if !json.Valid([]byte(applied)) || !strings.Contains(applied, `"name":"`+templateName+`"`) {
		t.Fatalf("apply AgentTemplate stdout = %q, want JSON for %q", applied, templateName)
	}
	assertTemplate("applied through the CLI", "kagent")

	writeManifest("reapplied through the CLI", "codex")
	run("apply", "-f", manifestPath)
	assertTemplate("reapplied through the CLI", "codex")
}

func TestE2ECLIAgentInstanceDiscoveryAndInvoke(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	fixture := newInteractionFixture(t, target, startInteractionMock(t))
	binary := kagentCLI(t)
	baseArgs := []string{
		"--grpc-url", target,
		"--grpc-tls=false",
		"--namespace", "kagent",
		"--user-id", "e2e",
	}

	listOutput := runKagentCLI(t, fixture.ctx, binary, append(baseArgs, "get", "agent-instance")...)
	if !strings.Contains(listOutput, fixture.instanceID) {
		t.Fatalf("list AgentInstances stdout = %q, want instance %s", listOutput, fixture.instanceID)
	}

	getArgs := append(append([]string{}, baseArgs...), "--output-format", "json", "get", "agent-instance", fixture.instanceID)
	getOutput := runKagentCLI(t, fixture.ctx, binary, getArgs...)
	if !json.Valid([]byte(getOutput)) || !strings.Contains(getOutput, fixture.instanceID) {
		t.Fatalf("get AgentInstance stdout = %q, want JSON for instance %s", getOutput, fixture.instanceID)
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
				"--agent-instance", fixture.instanceID,
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
