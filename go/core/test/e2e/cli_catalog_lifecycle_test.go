package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/client-go/tools/clientcmd"
)

func TestCLIAgentTemplateCatalogAndInstanceLifecycle(t *testing.T) {
	if os.Getenv("KUBECONFIG") == "" {
		t.Setenv("KUBECONFIG", clientcmd.RecommendedHomeFile)
	}
	target := interactionTarget(t)
	templateName := createInteractionTemplate(t, startInteractionMock(t))
	binary := buildKagentCLI(t)
	baseArgs := []string{
		"--kagent-grpc-url", target,
		"--kagent-grpc-tls=false",
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
