package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"
	workflow "github.com/kagent-dev/kagent/go/core/internal/temporal/workflow"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// syncerMockTemporalClient extends mockTemporalClient with step result support.
type syncerMockTemporalClient struct {
	describeResults map[string]*WorkflowDescription
	describeErr     error
	queryResults    map[string][]workflow.StepResult
	queryErr        error
}

func (m *syncerMockTemporalClient) StartWorkflow(_ context.Context, _, _ string, _ *compiler.ExecutionPlan) error {
	return nil
}

func (m *syncerMockTemporalClient) CancelWorkflow(_ context.Context, _ string) error {
	return nil
}

func (m *syncerMockTemporalClient) DescribeWorkflow(_ context.Context, workflowID string) (*WorkflowDescription, error) {
	if m.describeErr != nil {
		return nil, m.describeErr
	}
	if desc, ok := m.describeResults[workflowID]; ok {
		return desc, nil
	}
	return &WorkflowDescription{Status: WorkflowExecutionRunning}, nil
}

func (m *syncerMockTemporalClient) QueryWorkflow(_ context.Context, workflowID, _ string, valuePtr any) error {
	if m.queryErr != nil {
		return m.queryErr
	}
	if results, ok := m.queryResults[workflowID]; ok {
		if ptr, ok := valuePtr.(*[]workflow.StepResult); ok {
			*ptr = results
		}
	}
	return nil
}

func runningWorkflowRun(name, workflowID string) *v1alpha2.WorkflowRun {
	return &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
		Status: v1alpha2.WorkflowRunStatus{
			Phase:              v1alpha2.WorkflowRunPhaseRunning,
			TemporalWorkflowID: workflowID,
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
}

func TestStatusSyncer_RunningWorkflowStepSync(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeResults: map[string]*WorkflowDescription{
			"wf-default-my-template-test-run": {Status: WorkflowExecutionRunning},
		},
		queryResults: map[string][]workflow.StepResult{
			"wf-default-my-template-test-run": {
				{Name: "step-a", Phase: "Succeeded"},
				{Name: "step-b", Phase: "Running"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	// Should still be Running.
	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseRunning {
		t.Errorf("Phase = %q, want Running", updated.Status.Phase)
	}

	// Steps should be synced.
	if len(updated.Status.Steps) != 2 {
		t.Fatalf("Steps count = %d, want 2", len(updated.Status.Steps))
	}
	if updated.Status.Steps[0].Name != "step-a" || updated.Status.Steps[0].Phase != v1alpha2.StepPhaseSucceeded {
		t.Errorf("Step 0 = %+v, want step-a Succeeded", updated.Status.Steps[0])
	}
	if updated.Status.Steps[1].Name != "step-b" || updated.Status.Steps[1].Phase != v1alpha2.StepPhaseRunning {
		t.Errorf("Step 1 = %+v, want step-b Running", updated.Status.Steps[1])
	}
}

func TestStatusSyncer_CompletedWorkflow(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeResults: map[string]*WorkflowDescription{
			"wf-default-my-template-test-run": {Status: WorkflowExecutionCompleted},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseSucceeded {
		t.Errorf("Phase = %q, want Succeeded", updated.Status.Phase)
	}
	if updated.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set")
	}

	runningCond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionRunning)
	if runningCond == nil || runningCond.Status != metav1.ConditionFalse {
		t.Error("Running condition should be False")
	}

	succeededCond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionSucceeded)
	if succeededCond == nil || succeededCond.Status != metav1.ConditionTrue {
		t.Error("Succeeded condition should be True")
	}
}

func TestStatusSyncer_FailedWorkflow(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeResults: map[string]*WorkflowDescription{
			"wf-default-my-template-test-run": {Status: WorkflowExecutionFailed, Error: "step-b failed"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseFailed {
		t.Errorf("Phase = %q, want Failed", updated.Status.Phase)
	}
	if updated.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set")
	}

	runningCond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionRunning)
	if runningCond == nil || runningCond.Status != metav1.ConditionFalse {
		t.Error("Running condition should be False")
	}
	if runningCond != nil && runningCond.Reason != "WorkflowFailed" {
		t.Errorf("Running reason = %q, want WorkflowFailed", runningCond.Reason)
	}

	succeededCond := findCondition(updated.Status.Conditions, v1alpha2.WorkflowRunConditionSucceeded)
	if succeededCond == nil || succeededCond.Status != metav1.ConditionFalse {
		t.Error("Succeeded condition should be False")
	}
}

func TestStatusSyncer_CancelledWorkflow(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeResults: map[string]*WorkflowDescription{
			"wf-default-my-template-test-run": {Status: WorkflowExecutionCancelled},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}

	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseCancelled {
		t.Errorf("Phase = %q, want Cancelled", updated.Status.Phase)
	}
	if updated.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set")
	}
}

func TestStatusSyncer_TemporalDescribeError(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeErr: fmt.Errorf("temporal unavailable"),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	// Should not return error (logged and continued).
	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	// Run should not be modified.
	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseRunning {
		t.Errorf("Phase = %q, want Running (unchanged)", updated.Status.Phase)
	}
}

func TestStatusSyncer_SkipsPendingRuns(t *testing.T) {
	s := newTestScheme()
	run := &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pending-run",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: "my-template",
		},
		Status: v1alpha2.WorkflowRunStatus{
			Phase: v1alpha2.WorkflowRunPhasePending,
		},
	}

	tc := &syncerMockTemporalClient{
		describeErr: fmt.Errorf("should not be called"),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	// Should succeed — pending runs are skipped, so describe is never called.
	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}
}

func TestStatusSyncer_QueryErrorNonFatal(t *testing.T) {
	s := newTestScheme()
	run := runningWorkflowRun("test-run", "wf-default-my-template-test-run")

	tc := &syncerMockTemporalClient{
		describeResults: map[string]*WorkflowDescription{
			"wf-default-my-template-test-run": {Status: WorkflowExecutionRunning},
		},
		queryErr: fmt.Errorf("query failed"),
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(run).
		WithStatusSubresource(run).
		Build()

	syncer := &WorkflowRunStatusSyncer{
		K8sClient:      fakeClient,
		TemporalClient: tc,
	}

	// Should not return error — query failure is non-fatal.
	if err := syncer.syncAll(context.Background()); err != nil {
		t.Fatalf("syncAll() error = %v", err)
	}

	// Phase should remain Running.
	var updated v1alpha2.WorkflowRun
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-run", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get run: %v", err)
	}
	if updated.Status.Phase != v1alpha2.WorkflowRunPhaseRunning {
		t.Errorf("Phase = %q, want Running", updated.Status.Phase)
	}
}

func TestStepStatusesEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []v1alpha2.StepStatus
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "different lengths",
			a:    []v1alpha2.StepStatus{{Name: "a"}},
			b:    nil,
			want: false,
		},
		{
			name: "equal",
			a:    []v1alpha2.StepStatus{{Name: "a", Phase: v1alpha2.StepPhaseRunning}},
			b:    []v1alpha2.StepStatus{{Name: "a", Phase: v1alpha2.StepPhaseRunning}},
			want: true,
		},
		{
			name: "different phase",
			a:    []v1alpha2.StepStatus{{Name: "a", Phase: v1alpha2.StepPhaseRunning}},
			b:    []v1alpha2.StepStatus{{Name: "a", Phase: v1alpha2.StepPhaseSucceeded}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stepStatusesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("stepStatusesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusSyncer_NeedLeaderElection(t *testing.T) {
	syncer := &WorkflowRunStatusSyncer{}
	if !syncer.NeedLeaderElection() {
		t.Error("NeedLeaderElection() should return true")
	}
}
