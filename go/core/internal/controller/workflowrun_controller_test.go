package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mockTemporalClient implements TemporalWorkflowClient for testing.
type mockTemporalClient struct {
	startCalled  bool
	cancelCalled bool
	startErr     error
	cancelErr    error
	lastPlan     *compiler.ExecutionPlan
}

func (m *mockTemporalClient) StartWorkflow(_ context.Context, _, _ string, plan *compiler.ExecutionPlan) error {
	m.startCalled = true
	m.lastPlan = plan
	return m.startErr
}

func (m *mockTemporalClient) CancelWorkflow(_ context.Context, _ string) error {
	m.cancelCalled = true
	return m.cancelErr
}

// validTemplate returns a validated WorkflowTemplate for testing.
func validTemplate() *v1alpha2.WorkflowTemplate {
	return &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-template",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
				{Name: "step-b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}},
			},
		},
		Status: v1alpha2.WorkflowTemplateStatus{
			Validated:          true,
			ObservedGeneration: 1,
		},
	}
}

// templateWithParams returns a validated template with required params.
func templateWithParams() *v1alpha2.WorkflowTemplate {
	t := validTemplate()
	t.Name = "param-template"
	t.Spec.Params = []v1alpha2.ParamSpec{
		{Name: "env", Type: v1alpha2.ParamTypeString},
	}
	return t
}

func TestWorkflowRunController_TemplateNotFound(t *testing.T) {
	s := newTestScheme()
	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "nonexistent-template",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: &mockTemporalClient{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get updated run: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionAccepted)
	if cond == nil {
		t.Fatal("Accepted condition not found")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Accepted status = %v, want False", cond.Status)
	}
	if cond.Reason != "TemplateNotFound" {
		t.Errorf("Accepted reason = %q, want TemplateNotFound", cond.Reason)
	}
	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseFailed {
		t.Errorf("Phase = %q, want Failed", updated.Status.Phase)
	}
}

func TestWorkflowRunController_TemplateNotValidated(t *testing.T) {
	s := newTestScheme()
	template := validTemplate()
	template.Status.Validated = false

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template, run).
		WithStatusSubresource(template, run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: &mockTemporalClient{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get updated run: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionAccepted)
	if cond == nil {
		t.Fatal("Accepted condition not found")
	}
	if cond.Reason != "TemplateNotValidated" {
		t.Errorf("Accepted reason = %q, want TemplateNotValidated", cond.Reason)
	}
}

func TestWorkflowRunController_MissingRequiredParam(t *testing.T) {
	s := newTestScheme()
	template := templateWithParams()

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "param-template",
			// Missing required "env" param.
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template, run).
		WithStatusSubresource(template, run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: &mockTemporalClient{},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get updated run: %v", err)
	}

	cond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionAccepted)
	if cond == nil {
		t.Fatal("Accepted condition not found")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("Accepted status = %v, want False", cond.Status)
	}
	if cond.Reason != "InvalidParams" {
		t.Errorf("Accepted reason = %q, want InvalidParams", cond.Reason)
	}
}

func TestWorkflowRunController_ValidRun(t *testing.T) {
	s := newTestScheme()
	template := validTemplate()
	tc := &mockTemporalClient{}

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template, run).
		WithStatusSubresource(template, run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: tc,
	}

	// First reconcile: acceptance phase — should snapshot and requeue.
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile(accept) error = %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after acceptance")
	}

	var accepted v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &accepted); err != nil {
		t.Fatalf("failed to get accepted run: %v", err)
	}

	if accepted.Status.ResolvedSpec == nil {
		t.Fatal("ResolvedSpec should be set after acceptance")
	}
	if accepted.Status.TemplateGeneration != 1 {
		t.Errorf("TemplateGeneration = %d, want 1", accepted.Status.TemplateGeneration)
	}
	if accepted.Status.Phase != v1alpha2.WorkflowRunPhasePending {
		t.Errorf("Phase = %q, want Pending", accepted.Status.Phase)
	}

	cond := findCondition(accepted.Status.Conditions, v1alpha2.WorkflowRunConditionAccepted)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Error("Accepted condition should be True")
	}

	// Second reconcile: submission phase — should start Temporal workflow.
	result, err = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile(submit) error = %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue after submission")
	}

	if !tc.startCalled {
		t.Error("Temporal StartWorkflow should have been called")
	}

	var submitted v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &submitted); err != nil {
		t.Fatalf("failed to get submitted run: %v", err)
	}

	expectedWFID := "wf-default-my-template-test-run"
	if submitted.Status.TemporalWorkflowID != expectedWFID {
		t.Errorf("TemporalWorkflowID = %q, want %q", submitted.Status.TemporalWorkflowID, expectedWFID)
	}
	if submitted.Status.Phase != v1alpha2.WorkflowRunPhaseRunning {
		t.Errorf("Phase = %q, want Running", submitted.Status.Phase)
	}
	if submitted.Status.StartTime == nil {
		t.Error("StartTime should be set")
	}

	runningCond := findCondition(submitted.Status.Conditions, v1alpha2.WorkflowRunConditionRunning)
	if runningCond == nil || runningCond.Status != metav1.ConditionTrue {
		t.Error("Running condition should be True")
	}
}

func TestWorkflowRunController_IdempotentReconciliation(t *testing.T) {
	s := newTestScheme()
	template := validTemplate()
	tc := &mockTemporalClient{}

	// Run that is already submitted.
	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
		Status: v1alpha2.WorkflowRunStatus{
			Phase:              v1alpha2.WorkflowRunPhaseRunning,
			TemporalWorkflowID: "wf-default-my-template-test-run",
			Conditions: []metav1.Condition{
				{
					Type:   v1alpha2.WorkflowRunConditionAccepted,
					Status: metav1.ConditionTrue,
					Reason: "Accepted",
				},
				{
					Type:   v1alpha2.WorkflowRunConditionRunning,
					Status: metav1.ConditionTrue,
					Reason: "WorkflowStarted",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template, run).
		WithStatusSubresource(template, run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: tc,
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue for already-submitted run")
	}
	if tc.startCalled {
		t.Error("StartWorkflow should NOT be called for already-submitted run")
	}
}

func TestWorkflowRunController_Deletion(t *testing.T) {
	s := newTestScheme()
	tc := &mockTemporalClient{}
	now := metav1.Now()

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-run",
			Namespace:         "default",
			Generation:        1,
			DeletionTimestamp: &now,
			Finalizers:        []string{v1alpha2.WorkflowRunFinalizer},
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
		Status: v1alpha2.WorkflowRunStatus{
			TemporalWorkflowID: "wf-default-my-template-test-run",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: tc,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if !tc.cancelCalled {
		t.Error("CancelWorkflow should have been called")
	}

	// After finalizer removal with DeletionTimestamp set, the fake client
	// deletes the object. Verify the object is gone (confirming finalizer was removed).
	var updated v1alpha2.WorkflowRun
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated)
	if err == nil {
		// Object still exists — check finalizer was removed.
		for _, f := range updated.Finalizers {
			if f == v1alpha2.WorkflowRunFinalizer {
				t.Error("finalizer should have been removed")
			}
		}
	}
	// If err is NotFound, that's expected — the fake client deleted the object
	// after the finalizer was removed.
}

func TestWorkflowRunController_DeletionWithoutWorkflowID(t *testing.T) {
	s := newTestScheme()
	tc := &mockTemporalClient{}
	now := metav1.Now()

	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-run",
			Namespace:         "default",
			Generation:        1,
			DeletionTimestamp: &now,
			Finalizers:        []string{v1alpha2.WorkflowRunFinalizer},
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: tc,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if tc.cancelCalled {
		t.Error("CancelWorkflow should NOT be called when no workflow ID exists")
	}
}

func TestWorkflowRunController_TemporalStartFailure(t *testing.T) {
	s := newTestScheme()
	template := validTemplate()
	tc := &mockTemporalClient{startErr: fmt.Errorf("temporal unavailable")}

	// Pre-accepted run ready for submission.
	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
		Status: v1alpha2.WorkflowRunStatus{
			Phase: v1alpha2.WorkflowRunPhasePending,
			ResolvedSpec: &v1alpha2.WorkflowTemplateSpec{
				Steps: []v1alpha2.StepSpec{
					{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1alpha2.WorkflowRunConditionAccepted,
					Status: metav1.ConditionTrue,
					Reason: "Accepted",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template, run).
		WithStatusSubresource(template, run).
		Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: tc,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-run", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("Reconcile() should return error when Temporal start fails")
	}
}

func TestWorkflowRunController_NotFoundIgnored(t *testing.T) {
	s := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	r := &WorkflowRunController{
		Client:         fakeClient,
		Scheme:         s,
		Compiler:       compiler.NewDAGCompiler(),
		TemporalClient: &mockTemporalClient{},
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v, want nil for not found", err)
	}
	if result.Requeue {
		t.Error("should not requeue for not found")
	}
}

func TestParamsToMap(t *testing.T) {
	params := []v1alpha2.Param{
		{Name: "env", Value: "prod"},
		{Name: "region", Value: "us-east-1"},
	}
	m := paramsToMap(params)
	if m["env"] != "prod" {
		t.Errorf("env = %q, want prod", m["env"])
	}
	if m["region"] != "us-east-1" {
		t.Errorf("region = %q, want us-east-1", m["region"])
	}
}

func TestIsConditionTrue(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		condType   string
		want       bool
	}{
		{
			name:       "empty conditions",
			conditions: nil,
			condType:   "Accepted",
			want:       false,
		},
		{
			name: "condition true",
			conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue},
			},
			condType: "Accepted",
			want:     true,
		},
		{
			name: "condition false",
			conditions: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionFalse},
			},
			condType: "Accepted",
			want:     false,
		},
		{
			name: "different condition type",
			conditions: []metav1.Condition{
				{Type: "Running", Status: metav1.ConditionTrue},
			},
			condType: "Accepted",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConditionTrue(tt.conditions, tt.condType)
			if got != tt.want {
				t.Errorf("isConditionTrue() = %v, want %v", got, tt.want)
			}
		})
	}
}
