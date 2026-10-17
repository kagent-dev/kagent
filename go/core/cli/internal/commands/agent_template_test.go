package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
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

// fakeAgentTemplateReadClient serves AgentTemplates as the kagent API does and records the requests.
type fakeAgentTemplateReadClient struct {
	t           *testing.T
	templates   []apiv1alpha3.AgentTemplate
	next        string
	err         error
	listRequest *apiv1.ListAgentTemplatesRequest
	getRequest  *apiv1.GetAgentTemplateRequest
}

func (f *fakeAgentTemplateReadClient) encode(template apiv1alpha3.AgentTemplate) *apiv1.AgentTemplate {
	resource, err := structuredobject.FromGo(&template, apiv1alpha3.GroupVersion.String(), agentTemplateKind, 0)
	require.NoError(f.t, err)
	return &apiv1.AgentTemplate{Ref: &apiv1.ResourceReference{Namespace: template.Namespace, Name: template.Name}, Resource: resource}
}

func (f *fakeAgentTemplateReadClient) ListAgentTemplates(_ context.Context, request *apiv1.ListAgentTemplatesRequest) (*apiv1.ListAgentTemplatesResponse, error) {
	f.listRequest = request
	if f.err != nil {
		return nil, f.err
	}
	response := &apiv1.ListAgentTemplatesResponse{Page: &apiv1.PageResponse{NextPageToken: f.next}}
	for _, template := range f.templates {
		response.AgentTemplates = append(response.AgentTemplates, f.encode(template))
	}
	return response, nil
}

func (f *fakeAgentTemplateReadClient) GetAgentTemplate(_ context.Context, request *apiv1.GetAgentTemplateRequest) (*apiv1.GetAgentTemplateResponse, error) {
	f.getRequest = request
	if f.err != nil {
		return nil, f.err
	}
	return &apiv1.GetAgentTemplateResponse{AgentTemplate: f.encode(f.templates[0])}, nil
}

func TestGetAgentTemplatesTableReportsNames(t *testing.T) {
	client := &fakeAgentTemplateReadClient{t: t, next: "next-page", templates: []apiv1alpha3.AgentTemplate{
		templateNamed("ready-template"),
		templateNamed("not-ready-template"),
		{ObjectMeta: metav1.ObjectMeta{Name: "unknown-template"}},
	}}
	var output bytes.Buffer

	err := getAgentTemplates(context.Background(), client, &AgentTemplateGetCfg{
		Namespace: "kagent", PageSize: 3, PageToken: "previous-page",
	}, clioutput.FormatTable, &output)
	require.NoError(t, err)
	assert.Equal(t, "kagent", client.listRequest.GetNamespace())
	assert.Equal(t, int32(3), client.listRequest.GetPage().GetLimit())
	assert.Equal(t, "previous-page", client.listRequest.GetPage().GetPageToken())
	assert.Contains(t, output.String(), "ready-template")
	assert.Contains(t, output.String(), "not-ready-template")
	assert.Contains(t, output.String(), "unknown-template")
	assert.NotContains(t, output.String(), "Items:")
	assert.Contains(t, output.String(), "Next page token: next-page")
}

func TestGetAgentTemplatesJSONPreservesListMetadata(t *testing.T) {
	client := &fakeAgentTemplateReadClient{t: t, next: "next-page", templates: []apiv1alpha3.AgentTemplate{
		templateNamed("ready-template"),
		templateNamed("not-ready-template"),
		{ObjectMeta: metav1.ObjectMeta{Name: "unknown-template"}},
	}}
	var output bytes.Buffer

	err := getAgentTemplates(context.Background(), client, &AgentTemplateGetCfg{
		Namespace: "kagent",
	}, clioutput.FormatJSON, &output)
	require.NoError(t, err)
	assert.Equal(t, int32(agentTemplateMaxPageSize), client.listRequest.GetPage().GetLimit())
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"continue":"next-page"`)
	assert.Contains(t, output.String(), `"name":"ready-template"`)
	assert.Contains(t, output.String(), `"name":"not-ready-template"`)
	assert.Contains(t, output.String(), `"name":"unknown-template"`)
}

func TestGetAgentTemplateByName(t *testing.T) {
	template := templateNamed("ready-template")
	template.Namespace = "team-a"
	client := &fakeAgentTemplateReadClient{t: t, templates: []apiv1alpha3.AgentTemplate{template}}
	var output bytes.Buffer

	err := getAgentTemplates(context.Background(), client, &AgentTemplateGetCfg{Namespace: "team-a", Name: "ready-template"}, clioutput.FormatJSON, &output)
	require.NoError(t, err)
	assert.True(t, proto.Equal(&apiv1.ResourceReference{Namespace: "team-a", Name: "ready-template"}, client.getRequest.GetRef()))
	assert.Contains(t, output.String(), `"name":"ready-template"`)
}

// An API error is returned as-is, with no fallback to Kubernetes.
func TestGetAgentTemplatesReturnsAPIErrors(t *testing.T) {
	client := &fakeAgentTemplateReadClient{t: t, err: status.Error(codes.PermissionDenied, "not allowed")}

	err := getAgentTemplates(context.Background(), client, &AgentTemplateGetCfg{Namespace: "kagent"}, clioutput.FormatTable, &bytes.Buffer{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func templateNamed(name string) apiv1alpha3.AgentTemplate {
	return apiv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestReadAgentTemplateManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "template.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
apiVersion: api.kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: researcher
spec:
  modelConfig:
    name: default
`), 0o600))

	ref, resource, err := readAgentTemplateManifest(manifestPath, "team-a")
	require.NoError(t, err)
	assert.Equal(t, &apiv1.ResourceReference{Namespace: "team-a", Name: "researcher"}, ref)
	decoded := &apiv1alpha3.AgentTemplate{}
	require.NoError(t, structuredobject.ToGo(resource, agentTemplateKind, decoded, 0))
	assert.Equal(t, "default", decoded.Spec.ModelConfig.Name)

	require.NoError(t, os.WriteFile(manifestPath, []byte(`
apiVersion: api.kagent.dev/v1alpha3
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
		assert.True(t, proto.Equal(&apiv1.CreateAgentTemplateRequest{Ref: ref, Resource: resource}, client.createRequest))
		assert.Contains(t, output.String(), "researcher")
	})

	t.Run("apply updates existing", func(t *testing.T) {
		client := &recordingAgentTemplateClient{template: template, createErr: status.Error(codes.AlreadyExists, "exists")}
		require.NoError(t, applyAgentTemplate(t.Context(), client, ref, resource, clioutput.FormatTable, &bytes.Buffer{}))
		assert.NotNil(t, client.createRequest)
		assert.True(t, proto.Equal(&apiv1.UpdateAgentTemplateRequest{Ref: ref, Resource: resource}, client.updateRequest))
	})
}

func testAgentTemplateMessage(t *testing.T) *apiv1.AgentTemplate {
	t.Helper()
	template := &apiv1alpha3.AgentTemplate{
		TypeMeta:   metav1.TypeMeta{APIVersion: apiv1alpha3.GroupVersion.String(), Kind: agentTemplateKind},
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "researcher"},
		Spec:       apiv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default"}},
	}
	resource, err := structuredobject.FromGo(template, apiv1alpha3.GroupVersion.String(), agentTemplateKind, 0)
	require.NoError(t, err)
	return &apiv1.AgentTemplate{
		Ref:      &apiv1.ResourceReference{Namespace: template.Namespace, Name: template.Name},
		Resource: resource,
	}
}

type recordingAgentTemplateClient struct {
	template      *apiv1.AgentTemplate
	createErr     error
	createRequest *apiv1.CreateAgentTemplateRequest
	updateRequest *apiv1.UpdateAgentTemplateRequest
}

func (c *recordingAgentTemplateClient) CreateAgentTemplate(_ context.Context, request *apiv1.CreateAgentTemplateRequest) (*apiv1.CreateAgentTemplateResponse, error) {
	c.createRequest = request
	if c.createErr != nil {
		return nil, c.createErr
	}
	return &apiv1.CreateAgentTemplateResponse{AgentTemplate: c.template}, nil
}

func (c *recordingAgentTemplateClient) UpdateAgentTemplate(_ context.Context, request *apiv1.UpdateAgentTemplateRequest) (*apiv1.UpdateAgentTemplateResponse, error) {
	c.updateRequest = request
	return &apiv1.UpdateAgentTemplateResponse{AgentTemplate: c.template}, nil
}
