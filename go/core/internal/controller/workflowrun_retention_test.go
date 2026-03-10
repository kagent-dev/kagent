package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(s)
	return s
}

func makeRun(name, namespace, templateRef, phase string, completionTime *metav1.Time, ttl *int32) *v1alpha2.WorkflowRun {
	return &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef:     templateRef,
			TTLSecondsAfterFinished: ttl,
		},
		Status: v1alpha2.WorkflowRunStatus{
			Phase:          phase,
			CompletionTime: completionTime,
		},
	}
}

func makeTemplate(name, namespace string, successLimit, failLimit *int32) *v1alpha2.WorkflowTemplate {
	var retention *v1alpha2.RetentionPolicy
	if successLimit != nil || failLimit != nil {
		retention = &v1alpha2.RetentionPolicy{
			SuccessfulRunsHistoryLimit: successLimit,
			FailedRunsHistoryLimit:     failLimit,
		}
	}
	return &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Steps: []v1alpha2.StepSpec{
				{Name: "step1", Type: v1alpha2.StepTypeAction, Action: "noop"},
			},
			Retention: retention,
		},
	}
}

func timeAt(minutesAgo int) *metav1.Time {
	t := metav1.NewTime(time.Now().Add(-time.Duration(minutesAgo) * time.Minute))
	return &t
}

func TestRetentionTTL(t *testing.T) {
	tests := []struct {
		name          string
		runs          []*v1alpha2.WorkflowRun
		wantDeleted   []string
		wantRetained  []string
	}{
		{
			name: "TTL expired - run deleted",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), ptr.To(int32(60))),
			},
			wantDeleted:  []string{"run1"},
			wantRetained: nil,
		},
		{
			name: "TTL not expired - run retained",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(1), ptr.To(int32(600))),
			},
			wantDeleted:  nil,
			wantRetained: []string{"run1"},
		},
		{
			name: "No TTL set - run retained",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
			},
			wantDeleted:  nil,
			wantRetained: []string{"run1"},
		},
		{
			name: "Running run with TTL - not deleted",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseRunning, nil, ptr.To(int32(60))),
			},
			wantDeleted:  nil,
			wantRetained: []string{"run1"},
		},
		{
			name: "Failed run with expired TTL - deleted",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(10), ptr.To(int32(60))),
			},
			wantDeleted:  []string{"run1"},
			wantRetained: nil,
		},
		{
			name: "Mixed - some expired some not",
			runs: []*v1alpha2.WorkflowRun{
				makeRun("expired", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), ptr.To(int32(60))),
				makeRun("active", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(1), ptr.To(int32(600))),
			},
			wantDeleted:  []string{"expired"},
			wantRetained: []string{"active"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newScheme()
			objs := make([]client.Object, len(tt.runs))
			for i, r := range tt.runs {
				objs[i] = r
			}
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&v1alpha2.WorkflowRun{}).Build()

			rc := &WorkflowRunRetentionController{K8sClient: k8sClient}
			err := rc.cleanupTTL(context.Background())
			if err != nil {
				t.Fatalf("cleanupTTL() error = %v", err)
			}

			for _, name := range tt.wantDeleted {
				run := &v1alpha2.WorkflowRun{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, run)
				if err == nil {
					t.Errorf("expected run %q to be deleted, but it still exists", name)
				}
			}

			for _, name := range tt.wantRetained {
				run := &v1alpha2.WorkflowRun{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, run)
				if err != nil {
					t.Errorf("expected run %q to be retained, but got error: %v", name, err)
				}
			}
		})
	}
}

func TestRetentionHistoryLimits(t *testing.T) {
	tests := []struct {
		name         string
		template     *v1alpha2.WorkflowTemplate
		runs         []*v1alpha2.WorkflowRun
		wantDeleted  []string
		wantRetained []string
	}{
		{
			name:     "Successful limit of 3 with 5 runs - 2 oldest deleted",
			template: makeTemplate("tmpl", "default", ptr.To(int32(3)), nil),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(50), nil),
				makeRun("run2", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(40), nil),
				makeRun("run3", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(30), nil),
				makeRun("run4", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
				makeRun("run5", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
			},
			wantDeleted:  []string{"run1", "run2"},
			wantRetained: []string{"run3", "run4", "run5"},
		},
		{
			name:     "Failed limit of 2 with 4 runs - 2 oldest deleted",
			template: makeTemplate("tmpl", "default", nil, ptr.To(int32(2))),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(40), nil),
				makeRun("run2", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(30), nil),
				makeRun("run3", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(20), nil),
				makeRun("run4", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(10), nil),
			},
			wantDeleted:  []string{"run1", "run2"},
			wantRetained: []string{"run3", "run4"},
		},
		{
			name:     "Under limit - no deletions",
			template: makeTemplate("tmpl", "default", ptr.To(int32(5)), nil),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
				makeRun("run2", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
			},
			wantDeleted:  nil,
			wantRetained: []string{"run1", "run2"},
		},
		{
			name:     "No retention policy - no deletions",
			template: makeTemplate("tmpl", "default", nil, nil),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("run1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
				makeRun("run2", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
			},
			wantDeleted:  nil,
			wantRetained: []string{"run1", "run2"},
		},
		{
			name:     "Both limits enforced independently",
			template: makeTemplate("tmpl", "default", ptr.To(int32(1)), ptr.To(int32(1))),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("s1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(30), nil),
				makeRun("s2", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
				makeRun("f1", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(30), nil),
				makeRun("f2", "default", "tmpl", v1alpha2.WorkflowRunPhaseFailed, timeAt(10), nil),
			},
			wantDeleted:  []string{"s1", "f1"},
			wantRetained: []string{"s2", "f2"},
		},
		{
			name:     "Running runs not affected by limits",
			template: makeTemplate("tmpl", "default", ptr.To(int32(1)), nil),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("s1", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
				makeRun("s2", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
				makeRun("r1", "default", "tmpl", v1alpha2.WorkflowRunPhaseRunning, nil, nil),
			},
			wantDeleted:  []string{"s1"},
			wantRetained: []string{"s2", "r1"},
		},
		{
			name:     "Runs from different template not affected",
			template: makeTemplate("tmpl-a", "default", ptr.To(int32(1)), nil),
			runs: []*v1alpha2.WorkflowRun{
				makeRun("a1", "default", "tmpl-a", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
				makeRun("a2", "default", "tmpl-a", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(10), nil),
				makeRun("b1", "default", "tmpl-b", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(20), nil),
			},
			wantDeleted:  []string{"a1"},
			wantRetained: []string{"a2", "b1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newScheme()
			objs := []client.Object{tt.template}
			for _, r := range tt.runs {
				objs = append(objs, r)
			}
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&v1alpha2.WorkflowRun{}).Build()

			rc := &WorkflowRunRetentionController{K8sClient: k8sClient}
			err := rc.cleanupHistoryLimits(context.Background())
			if err != nil {
				t.Fatalf("cleanupHistoryLimits() error = %v", err)
			}

			for _, name := range tt.wantDeleted {
				run := &v1alpha2.WorkflowRun{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, run)
				if err == nil {
					t.Errorf("expected run %q to be deleted, but it still exists", name)
				}
			}

			for _, name := range tt.wantRetained {
				run := &v1alpha2.WorkflowRun{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, run)
				if err != nil {
					t.Errorf("expected run %q to be retained, but got error: %v", name, err)
				}
			}
		})
	}
}

func TestRetentionSortByCompletionTime(t *testing.T) {
	runs := []*v1alpha2.WorkflowRun{
		makeRun("newest", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(1), nil),
		makeRun("oldest", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(30), nil),
		makeRun("middle", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, timeAt(15), nil),
		makeRun("no-time", "default", "tmpl", v1alpha2.WorkflowRunPhaseSucceeded, nil, nil),
	}

	sortByCompletionTime(runs)

	expected := []string{"no-time", "oldest", "middle", "newest"}
	for i, name := range expected {
		if runs[i].Name != name {
			t.Errorf("position %d: got %q, want %q", i, runs[i].Name, name)
		}
	}
}

func TestRetentionIsTerminalPhase(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{v1alpha2.WorkflowRunPhaseSucceeded, true},
		{v1alpha2.WorkflowRunPhaseFailed, true},
		{v1alpha2.WorkflowRunPhaseCancelled, true},
		{v1alpha2.WorkflowRunPhaseRunning, false},
		{v1alpha2.WorkflowRunPhasePending, false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("phase=%s", tt.phase), func(t *testing.T) {
			if got := isTerminalPhase(tt.phase); got != tt.want {
				t.Errorf("isTerminalPhase(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

func TestRetentionNeedLeaderElection(t *testing.T) {
	rc := &WorkflowRunRetentionController{}
	if !rc.NeedLeaderElection() {
		t.Error("NeedLeaderElection() should return true")
	}
}
