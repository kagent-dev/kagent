package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestScheduledRunController_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	newAgent := func(namespace, name string) *v1alpha2.Agent {
		return &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
	}

	newScheduledRun := func(namespace, name, schedule, agentName, agentNamespace string) *v1alpha2.ScheduledRun {
		return &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Generation: 1,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: schedule,
				AgentRef: v1alpha2.AgentReference{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Prompt:        "test prompt",
				MaxRunHistory: 10,
			},
		}
	}

	tests := []struct {
		name          string
		objects       []runtime.Object
		reqName       string
		reqNamespace  string
		wantErr       bool
		wantCondition metav1.ConditionStatus
		wantReason    string
		wantNotFound  bool // when the ScheduledRun doesn't exist
	}{
		{
			name: "valid schedule - accepted",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "0 */2 * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
		{
			name: "invalid cron expression",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "invalid-cron", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "InvalidSchedule",
		},
		{
			name: "sub-hourly schedule allowed - every 5 minutes",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "*/5 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
		{
			name: "sub-hourly schedule allowed - every 30 minutes",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "*/30 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
		{
			name: "agent not found",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "0 */2 * * *", "nonexistent-agent", "default"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "AgentNotFound",
		},
		{
			name:         "scheduledrun not found - deleted",
			objects:      []runtime.Object{},
			reqName:      "deleted-sr",
			reqNamespace: "default",
			wantErr:      false,
			wantNotFound: true,
		},
		{
			name: "agent ref namespace defaults to scheduledrun namespace",
			objects: []runtime.Object{
				newScheduledRun("mynamespace", "my-sr", "0 */2 * * *", "my-agent", ""),
				newAgent("mynamespace", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "mynamespace",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
		{
			name: "valid schedule - exactly 1 hour interval",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "0 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
		{
			name: "invalid time zone",
			objects: []runtime.Object{
				func() runtime.Object {
					sr := newScheduledRun("default", "tz-bad", "0 9 * * *", "my-agent", "default")
					sr.Spec.TimeZone = "Mars/Olympus_Mons"
					return sr
				}(),
				newAgent("default", "my-agent"),
			},
			reqName:       "tz-bad",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "InvalidTimeZone",
		},
		{
			name: "valid time zone accepted",
			objects: []runtime.Object{
				func() runtime.Object {
					sr := newScheduledRun("default", "tz-ok", "0 9 * * *", "my-agent", "default")
					sr.Spec.TimeZone = "America/Los_Angeles"
					return sr
				}(),
				newAgent("default", "my-agent"),
			},
			reqName:       "tz-ok",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "ScheduleAccepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&v1alpha2.ScheduledRun{})

			for _, obj := range tt.objects {
				clientBuilder = clientBuilder.WithRuntimeObjects(obj)
			}
			kubeClient := clientBuilder.Build()

			// Use a real scheduler (not started). UpdateSchedule/RemoveSchedule
			// work without the cron engine running.
			scheduler := NewScheduledRunScheduler(kubeClient, nil)

			controller := &ScheduledRunController{
				Scheme:    scheme,
				Kube:      kubeClient,
				Scheduler: scheduler,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.reqName,
					Namespace: tt.reqNamespace,
				},
			}

			result, err := controller.Reconcile(context.Background(), req)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, ctrl.Result{}, result)

			if tt.wantNotFound {
				// For deleted resources, just verify no error and the scheduler cleaned up
				return
			}

			// Verify status was updated
			var sr v1alpha2.ScheduledRun
			err = kubeClient.Get(context.Background(), types.NamespacedName{
				Name:      tt.reqName,
				Namespace: tt.reqNamespace,
			}, &sr)
			require.NoError(t, err)

			// Check condition
			require.NotEmpty(t, sr.Status.Conditions)
			cond := sr.Status.Conditions[0]
			assert.Equal(t, ScheduledRunConditionTypeAccepted, cond.Type)
			assert.Equal(t, tt.wantCondition, cond.Status)
			assert.Equal(t, tt.wantReason, cond.Reason)

			// NextRunTime is now owned by the scheduler (post-run), not the
			// reconciler — so it should be nil after a reconcile.
			assert.Nil(t, sr.Status.NextRunTime)

			// Check observed generation
			assert.Equal(t, int64(1), sr.Status.ObservedGeneration)
		})
	}
}

// TestScheduledRunController_AgentNotFound_RemovesCronEntry verifies that when
// a previously-accepted SR has its agentRef change to a non-existent agent,
// the controller removes the cron entry. Otherwise every tick would
// uselessly create a Failed history entry.
func TestScheduledRunController_AgentNotFound_RemovesCronEntry(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "sr",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 */2 * * *",
			AgentRef: v1alpha2.AgentReference{Name: "ghost", Namespace: "default"},
			Prompt:   "test",
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()

	scheduler := NewScheduledRunScheduler(kube, nil)
	// Simulate a prior reconcile that registered the cron entry while the
	// agent existed.
	require.NoError(t, scheduler.UpdateSchedule(sr))
	key := types.NamespacedName{Name: "sr", Namespace: "default"}
	_, ok := scheduler.entries[key]
	require.True(t, ok, "precondition: entry registered")

	c := &ScheduledRunController{Scheme: scheme, Kube: kube, Scheduler: scheduler}
	_, err := c.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)

	_, ok = scheduler.entries[key]
	assert.False(t, ok, "entry must be removed when referenced agent disappears")
}

// TestScheduledRunController_FindScheduledRunsForAgent verifies the Agent
// watch's mapper enqueues only SRs whose AgentRef points at the given Agent.
// The empty-namespace case must resolve to the SR's own namespace, matching
// the controller's resolution rule.
func TestScheduledRunController_FindScheduledRunsForAgent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	newSR := func(ns, name, agentName, agentNS string) *v1alpha2.ScheduledRun {
		return &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Generation: 1},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				AgentRef: v1alpha2.AgentReference{Name: agentName, Namespace: agentNS},
				Prompt:   "test",
			},
		}
	}

	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithIndex(&v1alpha2.ScheduledRun{}, scheduledRunAgentRefIndex, func(obj client.Object) []string {
			sr, ok := obj.(*v1alpha2.ScheduledRun)
			if !ok {
				return nil
			}
			ns := sr.Spec.AgentRef.Namespace
			if ns == "" {
				ns = sr.Namespace
			}
			return []string{ns + "/" + sr.Spec.AgentRef.Name}
		}).
		WithRuntimeObjects(
			newSR("ns-a", "sr-explicit-match", "agent-x", "ns-a"),
			newSR("ns-a", "sr-default-ns-match", "agent-x", ""),
			newSR("ns-a", "sr-other-agent", "agent-y", "ns-a"),
			newSR("ns-b", "sr-cross-ns", "agent-x", "ns-b"),
		).
		Build()

	c := &ScheduledRunController{Scheme: scheme, Kube: kube, Scheduler: NewScheduledRunScheduler(kube, nil)}

	requests := c.findScheduledRunsForAgent(context.Background(), &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-x", Namespace: "ns-a"},
	})

	got := map[string]bool{}
	for _, req := range requests {
		got[req.Namespace+"/"+req.Name] = true
	}
	assert.True(t, got["ns-a/sr-explicit-match"], "explicit ns match must enqueue")
	assert.True(t, got["ns-a/sr-default-ns-match"], "empty agentRef.namespace must resolve to SR namespace")
	assert.False(t, got["ns-a/sr-other-agent"], "non-matching agent name must NOT enqueue")
	assert.False(t, got["ns-b/sr-cross-ns"], "cross-namespace match must NOT enqueue")
	assert.Len(t, requests, 2)
}
