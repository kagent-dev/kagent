package agenttemplate

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	clientfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestValidateGetCfg(t *testing.T) {
	tests := []struct {
		name    string
		cfg     GetCfg
		wantErr string
	}{
		{name: "list"},
		{name: "list page", cfg: GetCfg{PageSize: 10, PageToken: "next"}},
		{name: "get", cfg: GetCfg{Name: "template"}},
		{name: "negative page size", cfg: GetCfg{PageSize: -1}, wantErr: "page size"},
		{name: "large page size", cfg: GetCfg{PageSize: 101}, wantErr: "page size"},
		{name: "get with pagination", cfg: GetCfg{Name: "template", PageSize: 10}, wantErr: "pagination"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGetCfg(&tt.cfg)
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

	err := get(context.Background(), clientSet.ApiV1alpha3().AgentTemplates("kagent"), &GetCfg{
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
		assert.Equal(t, int64(maxPageSize), options.Limit)
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

	err := get(context.Background(), clientSet.ApiV1alpha3().AgentTemplates("kagent"), &GetCfg{
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
