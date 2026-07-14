package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/scheduledrun"
)

func newControllerTestScheduledRunScheduler(t *testing.T, kube client.Client) *scheduledrun.ScheduledRunScheduler {
	t.Helper()
	scheduler, err := scheduledrun.NewScheduledRunScheduler(kube, nil, a2a.NewAgentClientRegistry())
	require.NoError(t, err)
	return scheduler
}

func scheduledRunTargetRef(kind, name string) corev1.TypedObjectReference {
	apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
	if kind == "" {
		kind = v1alpha2.ScheduledRunTargetKindAgent
	}
	return corev1.TypedObjectReference{
		APIGroup: &apiGroup,
		Kind:     kind,
		Name:     name,
	}
}

func newControllerTestAgent(namespace, name string) *v1alpha2.Agent {
	return &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

func newControllerTestSandboxAgent(namespace, name string) *v1alpha2.SandboxAgent {
	return &v1alpha2.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

func newControllerTestScheduledRun(namespace, name, schedule, targetName string) *v1alpha2.ScheduledRun {
	return &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Generation: 1},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  schedule,
			TargetRef: scheduledRunTargetRef("", targetName),
			Prompt:    "test prompt",
		},
	}
}

func TestScheduledRunController_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	suspendedRun := newControllerTestScheduledRun("default", "suspended-sr", "0 */2 * * *", "my-agent")
	suspendedRun.Spec.Suspend = true

	sandboxRun := newControllerTestScheduledRun("default", "sandbox-sr", "0 */2 * * *", "my-sandbox")
	sandboxRun.Spec.TargetRef = scheduledRunTargetRef(v1alpha2.ScheduledRunTargetKindSandboxAgent, "my-sandbox")

	crossDeniedRun := newControllerTestScheduledRun("default", "cross-denied", "0 */2 * * *", "my-agent")
	crossDeniedRun.Spec.TargetRef.Namespace = new("other")

	crossAllowedRun := newControllerTestScheduledRun("default", "cross-allowed", "0 */2 * * *", "my-agent")
	crossAllowedRun.Spec.TargetRef.Namespace = new("other")
	crossAllowedAgent := newControllerTestAgent("other", "my-agent")
	crossAllowedAgent.Spec.AllowedNamespaces = &v1alpha2.AllowedNamespaces{From: v1alpha2.NamespacesFromAll}

	crossSandboxRun := newControllerTestScheduledRun("default", "cross-sandbox", "0 */2 * * *", "my-sandbox")
	crossSandboxRun.Spec.TargetRef = scheduledRunTargetRef(v1alpha2.ScheduledRunTargetKindSandboxAgent, "my-sandbox")
	crossSandboxRun.Spec.TargetRef.Namespace = new("other")
	crossSandboxAgent := newControllerTestSandboxAgent("other", "my-sandbox")
	crossSandboxAgent.Spec.AllowedNamespaces = &v1alpha2.AllowedNamespaces{From: v1alpha2.NamespacesFromAll}

	selectorRun := newControllerTestScheduledRun("source", "selector-allowed", "0 */2 * * *", "my-agent")
	selectorRun.Spec.TargetRef.Namespace = new("target")
	selectorAgent := newControllerTestAgent("target", "my-agent")
	selectorAgent.Spec.AllowedNamespaces = &v1alpha2.AllowedNamespaces{
		From:     v1alpha2.NamespacesFromSelector,
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}},
	}

	unwatchedRun := newControllerTestScheduledRun("default", "cross-unwatched", "0 */2 * * *", "my-agent")
	unwatchedRun.Spec.TargetRef.Namespace = new("other")

	invalidTimeZoneRun := newControllerTestScheduledRun("default", "tz-bad", "0 9 * * *", "my-agent")
	invalidTimeZoneRun.Spec.TimeZone = "Mars/Olympus_Mons"
	validTimeZoneRun := newControllerTestScheduledRun("default", "tz-ok", "0 9 * * *", "my-agent")
	validTimeZoneRun.Spec.TimeZone = "America/Los_Angeles"

	tests := []struct {
		name              string
		scheduledRun      *v1alpha2.ScheduledRun
		dependencies      []runtime.Object
		wantStatus        metav1.ConditionStatus
		wantReason        string
		wantCronEntry     bool
		watchedNamespaces []string
	}{
		{
			name:          "valid schedule - accepted",
			scheduledRun:  newControllerTestScheduledRun("default", "my-sr", "0 */2 * * *", "my-agent"),
			dependencies:  []runtime.Object{newControllerTestAgent("default", "my-agent")},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
		{
			name:         "invalid cron expression",
			scheduledRun: newControllerTestScheduledRun("default", "my-sr", "invalid-cron", "my-agent"),
			dependencies: []runtime.Object{newControllerTestAgent("default", "my-agent")},
			wantStatus:   metav1.ConditionFalse,
			wantReason:   scheduledRunReasonInvalidSchedule,
		},
		{
			name:         "suspended schedule accepted without next run",
			scheduledRun: suspendedRun,
			dependencies: []runtime.Object{newControllerTestAgent("default", "my-agent")},
			wantStatus:   metav1.ConditionTrue,
			wantReason:   scheduledRunReasonAccepted,
		},
		{
			name:         "agent not found",
			scheduledRun: newControllerTestScheduledRun("default", "my-sr", "0 */2 * * *", "nonexistent-agent"),
			wantStatus:   metav1.ConditionFalse,
			wantReason:   scheduledRunReasonTargetNotFound,
		},
		{
			name:          "sandbox agent target - accepted",
			scheduledRun:  sandboxRun,
			dependencies:  []runtime.Object{newControllerTestSandboxAgent("default", "my-sandbox")},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
		{
			name:         "omitted target namespace defaults to scheduledrun namespace",
			scheduledRun: newControllerTestScheduledRun("default", "cross-sr", "0 */2 * * *", "my-agent"),
			dependencies: []runtime.Object{newControllerTestAgent("other", "my-agent")},
			wantStatus:   metav1.ConditionFalse,
			wantReason:   scheduledRunReasonTargetNotFound,
		},
		{
			name:         "cross-namespace agent target denied without allowed namespaces",
			scheduledRun: crossDeniedRun,
			dependencies: []runtime.Object{newControllerTestAgent("other", "my-agent")},
			wantStatus:   metav1.ConditionFalse,
			wantReason:   scheduledRunReasonTargetReferenceNotAllowed,
		},
		{
			name:          "cross-namespace agent target allowed",
			scheduledRun:  crossAllowedRun,
			dependencies:  []runtime.Object{crossAllowedAgent},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
		{
			name:          "cross-namespace sandbox agent target allowed",
			scheduledRun:  crossSandboxRun,
			dependencies:  []runtime.Object{crossSandboxAgent},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
		{
			name:         "cross-namespace target allowed by source namespace selector",
			scheduledRun: selectorRun,
			dependencies: []runtime.Object{
				selectorAgent,
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name:   "source",
					Labels: map[string]string{"team": "platform"},
				}},
			},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
		{
			name:              "cross-namespace target must be watched",
			scheduledRun:      unwatchedRun,
			wantStatus:        metav1.ConditionFalse,
			wantReason:        scheduledRunReasonTargetNamespaceNotWatched,
			watchedNamespaces: []string{"default"},
		},
		{
			name:         "invalid time zone",
			scheduledRun: invalidTimeZoneRun,
			dependencies: []runtime.Object{newControllerTestAgent("default", "my-agent")},
			wantStatus:   metav1.ConditionFalse,
			wantReason:   scheduledRunReasonInvalidTimeZone,
		},
		{
			name:          "valid time zone accepted",
			scheduledRun:  validTimeZoneRun,
			dependencies:  []runtime.Object{newControllerTestAgent("default", "my-agent")},
			wantStatus:    metav1.ConditionTrue,
			wantReason:    scheduledRunReasonAccepted,
			wantCronEntry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, 0, 1+len(tt.dependencies))
			objects = append(objects, tt.scheduledRun)
			objects = append(objects, tt.dependencies...)

			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&v1alpha2.ScheduledRun{}).
				WithRuntimeObjects(objects...).
				Build()

			// Use a real scheduler (not started). UpdateSchedule/RemoveSchedule
			// work without the cron engine running.
			scheduler := newControllerTestScheduledRunScheduler(t, kubeClient)

			controller := &ScheduledRunController{
				Kube:              kubeClient,
				Scheduler:         scheduler,
				WatchedNamespaces: tt.watchedNamespaces,
			}

			key := client.ObjectKeyFromObject(tt.scheduledRun)
			req := ctrl.Request{NamespacedName: key}

			result, err := controller.Reconcile(context.Background(), req)

			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, result)

			var sr v1alpha2.ScheduledRun
			err = kubeClient.Get(context.Background(), key, &sr)
			require.NoError(t, err)

			cond := meta.FindStatusCondition(sr.Status.Conditions, v1alpha2.ScheduledRunConditionTypeAccepted)
			require.NotNil(t, cond)
			assert.Equal(t, tt.wantStatus, cond.Status)
			assert.Equal(t, tt.wantReason, cond.Reason)
			assert.Equal(t, sr.Generation, cond.ObservedGeneration)
			if tt.wantCronEntry {
				assert.NotNil(t, sr.Status.NextRunTime)
			} else {
				assert.Nil(t, sr.Status.NextRunTime)
			}

			assert.Equal(t, sr.Generation, sr.Status.ObservedGeneration)
			assert.Equal(t, tt.wantCronEntry, scheduler.HasSchedule(key))
		})
	}
}

func TestScheduledRunController_RejectedOrDeleted_RemovesExistingCronEntry(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	tests := []struct {
		name                string
		includeScheduledRun bool
		includeTarget       bool
		storedSchedule      string
	}{
		{name: "ScheduledRun deleted"},
		{name: "target missing", includeScheduledRun: true},
		{name: "schedule invalid", includeScheduledRun: true, includeTarget: true, storedSchedule: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registered := newControllerTestScheduledRun("default", "sr", "0 */2 * * *", "agent")
			objects := make([]runtime.Object, 0, 2)
			if tt.includeScheduledRun {
				stored := registered.DeepCopy()
				if tt.storedSchedule != "" {
					stored.Spec.Schedule = tt.storedSchedule
				}
				objects = append(objects, stored)
			}
			if tt.includeTarget {
				objects = append(objects, newControllerTestAgent("default", "agent"))
			}

			kube := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&v1alpha2.ScheduledRun{}).
				WithRuntimeObjects(objects...).
				Build()
			scheduler := newControllerTestScheduledRunScheduler(t, kube)
			require.NoError(t, scheduler.UpdateSchedule(registered))
			key := client.ObjectKeyFromObject(registered)
			require.True(t, scheduler.HasSchedule(key), "precondition: entry registered")

			controller := &ScheduledRunController{Kube: kube, Scheduler: scheduler}
			_, err := controller.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
			require.NoError(t, err)
			assert.False(t, scheduler.HasSchedule(key), "entry must be removed")
		})
	}
}

func TestScheduledRunController_EnqueueScheduledRunsForCrossNamespaceTarget(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
	crossNamespace := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "cross", Namespace: "source-ns"},
		Spec: v1alpha2.ScheduledRunSpec{TargetRef: corev1.TypedObjectReference{
			APIGroup:  &apiGroup,
			Kind:      v1alpha2.ScheduledRunTargetKindAgent,
			Namespace: new("target-ns"),
			Name:      "target",
		}},
	}
	sameNamespace := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "target-ns"},
		Spec:       v1alpha2.ScheduledRunSpec{TargetRef: corev1.TypedObjectReference{Name: "target"}},
	}
	unrelated := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "source-ns"},
		Spec:       v1alpha2.ScheduledRunSpec{TargetRef: scheduledRunTargetRef("", "other")},
	}
	otherKind := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: "target-ns"},
		Spec:       v1alpha2.ScheduledRunSpec{TargetRef: scheduledRunTargetRef(v1alpha2.ScheduledRunTargetKindSandboxAgent, "target")},
	}

	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&v1alpha2.ScheduledRun{}, scheduledrun.TargetRefIndexField, scheduledrun.IndexTargetRef).
		WithRuntimeObjects(crossNamespace, sameNamespace, unrelated, otherKind).
		Build()
	controller := &ScheduledRunController{Kube: kube}
	target := &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "target-ns"}}

	requests := controller.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindAgent)(context.Background(), target)
	assert.ElementsMatch(t, []ctrl.Request{
		{NamespacedName: types.NamespacedName{Namespace: "source-ns", Name: "cross"}},
		{NamespacedName: types.NamespacedName{Namespace: "target-ns", Name: "same"}},
	}, requests)

	sandboxTarget := &v1alpha2.SandboxAgent{ObjectMeta: target.ObjectMeta}
	sandboxRequests := controller.enqueueScheduledRunsForTarget(v1alpha2.ScheduledRunTargetKindSandboxAgent)(context.Background(), sandboxTarget)
	assert.Equal(t, []ctrl.Request{
		{NamespacedName: types.NamespacedName{Namespace: "target-ns", Name: "sandbox"}},
	}, sandboxRequests)
}
