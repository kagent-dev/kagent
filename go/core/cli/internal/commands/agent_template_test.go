package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	clientfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestValidateAgentTemplateGetCfg(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AgentTemplateGetCfg
		wantErr string
	}{
		{name: "list"},
		{name: "list page", cfg: AgentTemplateGetCfg{PageSize: 10, PageToken: "next"}},
		{name: "get", cfg: AgentTemplateGetCfg{Name: "template"}},
		{name: "negative page size", cfg: AgentTemplateGetCfg{PageSize: -1}, wantErr: "page size"},
		{name: "large page size", cfg: AgentTemplateGetCfg{PageSize: 101}, wantErr: "page size"},
		{name: "get with pagination", cfg: AgentTemplateGetCfg{Name: "template", PageSize: 10}, wantErr: "pagination"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentTemplateGetCfg(&tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGetAgentTemplatesTableReportsHarnessReadiness(t *testing.T) {
	clientSet := clientfake.NewSimpleClientset()
	clientSet.PrependReactor("list", "agenttemplates", func(action k8stesting.Action) (bool, runtime.Object, error) {
		options := action.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		assert.Equal(t, int64(3), options.Limit)
		assert.Equal(t, "previous-page", options.Continue)
		return true, &apiv1alpha3.AgentTemplateList{
			ListMeta: metav1.ListMeta{Continue: "next-page"},
			Items: []apiv1alpha3.AgentTemplate{
				templateWithReadyCondition("ready-template", "kagent", metav1.ConditionTrue),
				templateWithReadyCondition("not-ready-template", "codex", metav1.ConditionFalse),
				{ObjectMeta: metav1.ObjectMeta{Name: "unknown-template"}},
			},
		}, nil
	})
	var output bytes.Buffer

	err := getAgentTemplates(context.Background(), clientSet.ApiV1alpha3().AgentTemplates("kagent"), &AgentTemplateGetCfg{
		Namespace: "kagent", PageSize: 3, PageToken: "previous-page",
	}, clioutput.FormatTable, &output)
	require.NoError(t, err)
	assert.Contains(t, output.String(), "ready-template")
	assert.Contains(t, output.String(), "kagent")
	assert.Contains(t, output.String(), "TRUE")
	assert.Contains(t, output.String(), "not-ready-template")
	assert.Contains(t, output.String(), "FALSE")
	assert.Contains(t, output.String(), "unknown-template")
	assert.Contains(t, output.String(), "UNKNOWN")
	assert.NotContains(t, output.String(), "Items:")
	assert.Contains(t, output.String(), "Next page token: next-page")
}

func TestGetAgentTemplatesJSONPreservesListMetadata(t *testing.T) {
	clientSet := clientfake.NewSimpleClientset()
	clientSet.PrependReactor("list", "agenttemplates", func(action k8stesting.Action) (bool, runtime.Object, error) {
		options := action.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		assert.Equal(t, int64(agentTemplateMaxPageSize), options.Limit)
		return true, &apiv1alpha3.AgentTemplateList{
			ListMeta: metav1.ListMeta{Continue: "next-page"},
			Items: []apiv1alpha3.AgentTemplate{
				templateWithReadyCondition("ready-template", "kagent", metav1.ConditionTrue),
				templateWithReadyCondition("not-ready-template", "codex", metav1.ConditionFalse),
				{ObjectMeta: metav1.ObjectMeta{Name: "unknown-template"}},
			},
		}, nil
	})
	var output bytes.Buffer

	err := getAgentTemplates(context.Background(), clientSet.ApiV1alpha3().AgentTemplates("kagent"), &AgentTemplateGetCfg{
		Namespace: "kagent",
	}, clioutput.FormatJSON, &output)
	require.NoError(t, err)
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"continue":"next-page"`)
	assert.Contains(t, output.String(), `"name":"ready-template"`)
	assert.Contains(t, output.String(), `"status":"True"`)
	assert.Contains(t, output.String(), `"name":"not-ready-template"`)
	assert.Contains(t, output.String(), `"status":"False"`)
	assert.Contains(t, output.String(), `"name":"unknown-template"`)
}

func templateWithReadyCondition(name, harness string, status metav1.ConditionStatus) apiv1alpha3.AgentTemplate {
	return apiv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: apiv1alpha3.AgentTemplateStatus{Harnesses: []apiv1alpha3.AgentTemplateHarnessStatus{{
			Harness: harness,
			Conditions: []metav1.Condition{{
				Type: apiv1alpha3.AgentTemplateConditionReady, Status: status, Reason: "Test", Message: "test",
			}},
		}}},
	}
}

func TestReadAgentTemplateManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "template.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: researcher
spec:
  modelConfig:
    name: default
`), 0o600))

	ref, resource, err := readAgentTemplateManifest(manifestPath, "team-a")
	require.NoError(t, err)
	assert.Equal(t, &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "researcher"}, ref)
	decoded := &apiv1alpha3.AgentTemplate{}
	require.NoError(t, structuredobject.ToGo(resource, agentTemplateKind, decoded, 0))
	assert.Equal(t, "default", decoded.Spec.ModelConfig.Name)

	require.NoError(t, os.WriteFile(manifestPath, []byte(`
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: researcher
  namespace: other
spec:
  modelConfig:
    name: default
`), 0o600))
	_, _, err = readAgentTemplateManifest(manifestPath, "team-a")
	require.ErrorContains(t, err, `does not match --namespace`)
}

func TestAgentTemplateLifecycleCommands(t *testing.T) {
	template := testAgentTemplateMessage(t)
	ref := template.GetRef()
	resource := template.GetResource()

	t.Run("apply creates", func(t *testing.T) {
		client := &recordingAgentTemplateClient{template: template}
		var output bytes.Buffer
		require.NoError(t, applyAgentTemplate(t.Context(), client, ref, resource, clioutput.FormatTable, &output))
		assert.True(t, proto.Equal(&apiv1alpha1.CreateAgentTemplateRequest{Ref: ref, Resource: resource}, client.createRequest))
		assert.Contains(t, output.String(), "researcher")
	})

	t.Run("apply updates existing", func(t *testing.T) {
		client := &recordingAgentTemplateClient{template: template, createErr: status.Error(codes.AlreadyExists, "exists")}
		require.NoError(t, applyAgentTemplate(t.Context(), client, ref, resource, clioutput.FormatTable, &bytes.Buffer{}))
		assert.NotNil(t, client.createRequest)
		assert.True(t, proto.Equal(&apiv1alpha1.UpdateAgentTemplateRequest{Ref: ref, Resource: resource}, client.updateRequest))
	})
}

func testAgentTemplateMessage(t *testing.T) *apiv1alpha1.AgentTemplate {
	t.Helper()
	template := &apiv1alpha3.AgentTemplate{
		TypeMeta:   metav1.TypeMeta{APIVersion: apiv1alpha3.GroupVersion.String(), Kind: agentTemplateKind},
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "researcher"},
		Spec:       apiv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default"}},
	}
	resource, err := structuredobject.FromGo(template, apiv1alpha3.GroupVersion.String(), agentTemplateKind, 0)
	require.NoError(t, err)
	return &apiv1alpha1.AgentTemplate{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: template.Namespace, Name: template.Name},
		Resource: resource,
	}
}

type recordingAgentTemplateClient struct {
	template      *apiv1alpha1.AgentTemplate
	createErr     error
	createRequest *apiv1alpha1.CreateAgentTemplateRequest
	updateRequest *apiv1alpha1.UpdateAgentTemplateRequest
}

func (c *recordingAgentTemplateClient) CreateAgentTemplate(_ context.Context, request *apiv1alpha1.CreateAgentTemplateRequest) (*apiv1alpha1.CreateAgentTemplateResponse, error) {
	c.createRequest = request
	if c.createErr != nil {
		return nil, c.createErr
	}
	return &apiv1alpha1.CreateAgentTemplateResponse{AgentTemplate: c.template}, nil
}

func (c *recordingAgentTemplateClient) UpdateAgentTemplate(_ context.Context, request *apiv1alpha1.UpdateAgentTemplateRequest) (*apiv1alpha1.UpdateAgentTemplateResponse, error) {
	c.updateRequest = request
	return &apiv1alpha1.UpdateAgentTemplateResponse{AgentTemplate: c.template}, nil
}
