package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mockErrorResponseWriter struct {
	*httptest.ResponseRecorder
	errorResponse error
}

func newMockErrorResponseWriter() *mockErrorResponseWriter {
	return &mockErrorResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockErrorResponseWriter) RespondWithError(err error) {
	m.errorResponse = err
	if httpErr, ok := err.(interface{ StatusCode() int }); ok {
		m.WriteHeader(httpErr.StatusCode())
	} else {
		m.WriteHeader(http.StatusInternalServerError)
	}
	json.NewEncoder(m).Encode(map[string]string{"error": err.Error()})
}

func TestGetErrorFromConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       *string
	}{
		{
			name: "no error conditions",
			conditions: []metav1.Condition{
				{
					Type:    "Accepted",
					Status:  metav1.ConditionTrue,
					Message: "All good",
				},
			},
			want: nil,
		},
		{
			name: "has error condition",
			conditions: []metav1.Condition{
				{
					Type:    "Accepted",
					Status:  metav1.ConditionFalse,
					Message: "Failed to reconcile",
				},
			},
			want: stringPtr("Failed to reconcile"),
		},
		{
			name: "multiple conditions with error",
			conditions: []metav1.Condition{
				{
					Type:    "Accepted",
					Status:  metav1.ConditionTrue,
					Message: "All good",
				},
				{
					Type:    "Error",
					Status:  metav1.ConditionFalse,
					Message: "Something went wrong",
				},
			},
			want: stringPtr("Something went wrong"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getErrorFromConditions(tt.conditions)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("getErrorFromConditions() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && tt.want != nil && *got != *tt.want {
				t.Errorf("getErrorFromConditions() = %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestHandleListToolServers(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	tests := []struct {
		name           string
		toolServers    []v1alpha1.ToolServer
		expectedStatus int
		expectedBody   map[string]interface{}
	}{
		{
			name: "successful tool servers",
			toolServers: []v1alpha1.ToolServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-server-1",
					},
					Spec: v1alpha1.ToolServerSpec{
						Config: v1alpha1.ToolServerConfig{},
					},
					Status: v1alpha1.ToolServerStatus{
						Conditions: []metav1.Condition{
							{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Message: "All good",
							},
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody: map[string]interface{}{
				"name":            "test-server-1",
				"config":          map[string]interface{}{},
				"discoveredTools": []interface{}{},
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":    "Accepted",
							"status":  "True",
							"message": "All good",
						},
					},
					"error": nil,
				},
			},
		},
		{
			name: "tool server with error",
			toolServers: []v1alpha1.ToolServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-server-2",
					},
					Spec: v1alpha1.ToolServerSpec{
						Config: v1alpha1.ToolServerConfig{},
					},
					Status: v1alpha1.ToolServerStatus{
						Conditions: []metav1.Condition{
							{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Message: "Failed to reconcile",
							},
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody: map[string]interface{}{
				"name":            "test-server-2",
				"config":          map[string]interface{}{},
				"discoveredTools": []interface{}{},
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":    "Accepted",
							"status":  "False",
							"message": "Failed to reconcile",
						},
					},
					"error": "Failed to reconcile",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with test data
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			for _, ts := range tt.toolServers {
				if err := client.Create(nil, &ts); err != nil {
					t.Fatalf("Failed to create test tool server: %v", err)
				}
			}

			// Create handler
			handler := NewToolServersHandler(&Base{
				KubeClient: client,
			})

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/api/toolservers", nil)
			w := newMockErrorResponseWriter()

			// Call handler
			handler.HandleListToolServers(w, req)

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("HandleListToolServers() status = %v, want %v", w.Code, tt.expectedStatus)
			}

			// Parse response body
			var response []map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Check response
			if len(response) != len(tt.toolServers) {
				t.Errorf("HandleListToolServers() returned %d tool servers, want %d", len(response), len(tt.toolServers))
			}

			// Compare first tool server response with expected
			if len(response) > 0 {
				compareToolServerResponse(t, response[0], tt.expectedBody)
			}
		})
	}
}

func compareToolServerResponse(t *testing.T, got, want map[string]interface{}) {
	// Compare basic fields
	for key, wantVal := range want {
		gotVal, exists := got[key]
		if !exists {
			t.Errorf("Response missing key: %s", key)
			continue
		}

		if key == "status" {
			gotStatus := gotVal.(map[string]interface{})
			wantStatus := wantVal.(map[string]interface{})
			compareStatus(t, gotStatus, wantStatus)
		} else {
			if !compareValues(gotVal, wantVal) {
				t.Errorf("Field %s = %v, want %v", key, gotVal, wantVal)
			}
		}
	}
}

func compareStatus(t *testing.T, got, want map[string]interface{}) {
	gotConditions := got["conditions"].([]interface{})
	wantConditions := want["conditions"].([]interface{})
	if len(gotConditions) != len(wantConditions) {
		t.Errorf("Status conditions length = %d, want %d", len(gotConditions), len(wantConditions))
		return
	}
	
	gotError := got["error"]
	wantError := want["error"]
	if !compareValues(gotError, wantError) {
		t.Errorf("Status error = %v, want %v", gotError, wantError)
	}
}

func compareValues(got, want interface{}) bool {
	switch v := want.(type) {
	case nil:
		return got == nil
	case string:
		return got == v
	case []interface{}:
		gotSlice, ok := got.([]interface{})
		if !ok || len(gotSlice) != len(v) {
			return false
		}
		for i := range v {
			if !compareValues(gotSlice[i], v[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		gotMap, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		for k, wantVal := range v {
			gotVal, exists := gotMap[k]
			if !exists || !compareValues(gotVal, wantVal) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}

func stringPtr(s string) *string {
	return &s
} 
