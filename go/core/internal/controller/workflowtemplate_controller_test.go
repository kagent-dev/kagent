package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/compiler"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(s)
	return s
}

func TestWorkflowTemplateController_Reconcile(t *testing.T) {
	tests := []struct {
		name           string
		template       *v1alpha2.WorkflowTemplate
		wantValidated  bool
		wantAccepted   metav1.ConditionStatus
		wantReason     string
		wantStepCount  int32
	}{
		{
			name: "valid linear DAG",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "valid-template",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
						{Name: "step-b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}},
						{Name: "step-c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-b"}},
					},
				},
			},
			wantValidated: true,
			wantAccepted:  metav1.ConditionTrue,
			wantReason:    "Valid",
			wantStepCount: 3,
		},
		{
			name: "cycle detected",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cycle-template",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "a", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"c"}},
						{Name: "b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"a"}},
						{Name: "c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"b"}},
					},
				},
			},
			wantValidated: false,
			wantAccepted:  metav1.ConditionFalse,
			wantReason:    "CycleDetected",
			wantStepCount: 3,
		},
		{
			name: "duplicate step name",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "dup-template",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
						{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
					},
				},
			},
			wantValidated: false,
			wantAccepted:  metav1.ConditionFalse,
			wantReason:    "DuplicateStepName",
			wantStepCount: 2,
		},
		{
			name: "invalid reference",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "invalid-ref-template",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"nonexistent"}},
					},
				},
			},
			wantValidated: false,
			wantAccepted:  metav1.ConditionFalse,
			wantReason:    "InvalidReference",
			wantStepCount: 1,
		},
		{
			name: "action step missing action field",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "missing-action",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "step-a", Type: v1alpha2.StepTypeAction},
					},
				},
			},
			wantValidated: false,
			wantAccepted:  metav1.ConditionFalse,
			wantReason:    "InvalidStepSpec",
			wantStepCount: 1,
		},
		{
			name: "agent step missing agentRef",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "missing-agentref",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "step-a", Type: v1alpha2.StepTypeAgent, Prompt: "analyze this"},
					},
				},
			},
			wantValidated: false,
			wantAccepted:  metav1.ConditionFalse,
			wantReason:    "InvalidStepSpec",
			wantStepCount: 1,
		},
		{
			name: "parallel DAG with fan-in",
			template: &v1alpha2.WorkflowTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "parallel-template",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: v1alpha2.WorkflowTemplateSpec{
					Steps: []v1alpha2.StepSpec{
						{Name: "start", Type: v1alpha2.StepTypeAction, Action: "noop"},
						{Name: "branch-a", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"start"}},
						{Name: "branch-b", Type: v1alpha2.StepTypeAgent, AgentRef: "my-agent", Prompt: "go", DependsOn: []string{"start"}},
						{Name: "join", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"branch-a", "branch-b"}},
					},
				},
			},
			wantValidated: true,
			wantAccepted:  metav1.ConditionTrue,
			wantReason:    "Valid",
			wantStepCount: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheme()
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.template).
				WithStatusSubresource(tt.template).
				Build()

			r := &WorkflowTemplateController{
				Client:   fakeClient,
				Scheme:   s,
				Compiler: compiler.NewDAGCompiler(),
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.template.Name,
					Namespace: tt.template.Namespace,
				},
			}

			result, err := r.Reconcile(context.Background(), req)
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if result.Requeue {
				t.Errorf("Reconcile() unexpected requeue")
			}

			// Fetch the updated template.
			var updated v1alpha2.WorkflowTemplate
			if err := fakeClient.Get(context.Background(), req.NamespacedName, &updated); err != nil {
				t.Fatalf("failed to get updated template: %v", err)
			}

			if updated.Status.Validated != tt.wantValidated {
				t.Errorf("Validated = %v, want %v", updated.Status.Validated, tt.wantValidated)
			}
			if updated.Status.StepCount != tt.wantStepCount {
				t.Errorf("StepCount = %d, want %d", updated.Status.StepCount, tt.wantStepCount)
			}
			if updated.Status.ObservedGeneration != tt.template.Generation {
				t.Errorf("ObservedGeneration = %d, want %d", updated.Status.ObservedGeneration, tt.template.Generation)
			}

			// Check Accepted condition.
			cond := findCondition(updated.Status.Conditions, WorkflowTemplateConditionAccepted)
			if cond == nil {
				t.Fatal("Accepted condition not found")
			}
			if cond.Status != tt.wantAccepted {
				t.Errorf("Accepted status = %v, want %v", cond.Status, tt.wantAccepted)
			}
			if cond.Reason != tt.wantReason {
				t.Errorf("Accepted reason = %q, want %q", cond.Reason, tt.wantReason)
			}
		})
	}
}

func TestWorkflowTemplateController_SkipsReconciledGeneration(t *testing.T) {
	s := newTestScheme()
	template := &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "already-reconciled",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop"},
			},
		},
		Status: v1alpha2.WorkflowTemplateStatus{
			ObservedGeneration: 1,
			Validated:          true,
			StepCount:          1,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(template).
		WithStatusSubresource(template).
		Build()

	r := &WorkflowTemplateController{
		Client:   fakeClient,
		Scheme:   s,
		Compiler: compiler.NewDAGCompiler(),
	}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "already-reconciled", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Requeue {
		t.Error("should not requeue for already-reconciled generation")
	}
}

func TestWorkflowTemplateController_NotFoundIgnored(t *testing.T) {
	s := newTestScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	r := &WorkflowTemplateController{
		Client:   fakeClient,
		Scheme:   s,
		Compiler: compiler.NewDAGCompiler(),
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

func TestClassifyValidationError(t *testing.T) {
	tests := []struct {
		errMsg string
		want   string
	}{
		{"cycle detected among steps: a, b, c", "CycleDetected"},
		{"duplicate step name: foo", "DuplicateStepName"},
		{"depends on nonexistent step: Y", "InvalidReference"},
		{"step X depends on itself", "InvalidReference"},
		{"step count exceeds maximum", "TooManySteps"},
		{"action step must have 'action' field", "InvalidStepSpec"},
		{"agent step must have 'agentRef' field", "InvalidStepSpec"},
		{"some other error", "ValidationFailed"},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			got := classifyValidationError(fmt.Errorf("%s", tt.errMsg))
			if got != tt.want {
				t.Errorf("classifyValidationError(%q) = %q, want %q", tt.errMsg, got, tt.want)
			}
		})
	}
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
