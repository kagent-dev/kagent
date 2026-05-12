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
		name            string
		objects         []runtime.Object
		reqName         string
		reqNamespace    string
		wantErr         bool
		wantCondition   metav1.ConditionStatus
		wantReason      string
		wantNextRunTime bool
		wantNotFound    bool // when the ScheduledRun doesn't exist
	}{
		{
			name: "valid schedule - accepted",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "0 */2 * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:         "my-sr",
			reqNamespace:    "default",
			wantErr:         false,
			wantCondition:   metav1.ConditionTrue,
			wantReason:      "ScheduleAccepted",
			wantNextRunTime: true,
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
			name: "frequency too high - every 5 minutes",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "*/5 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "FrequencyTooHigh",
		},
		{
			name: "frequency too high - every 30 minutes",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "*/30 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:       "my-sr",
			reqNamespace:  "default",
			wantErr:       false,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "FrequencyTooHigh",
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
			reqName:         "my-sr",
			reqNamespace:    "mynamespace",
			wantErr:         false,
			wantCondition:   metav1.ConditionTrue,
			wantReason:      "ScheduleAccepted",
			wantNextRunTime: true,
		},
		{
			name: "valid schedule - exactly 1 hour interval",
			objects: []runtime.Object{
				newScheduledRun("default", "my-sr", "0 * * * *", "my-agent", "default"),
				newAgent("default", "my-agent"),
			},
			reqName:         "my-sr",
			reqNamespace:    "default",
			wantErr:         false,
			wantCondition:   metav1.ConditionTrue,
			wantReason:      "ScheduleAccepted",
			wantNextRunTime: true,
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

			// Check next run time
			if tt.wantNextRunTime {
				assert.NotNil(t, sr.Status.NextRunTime)
			} else {
				assert.Nil(t, sr.Status.NextRunTime)
			}

			// Check observed generation
			assert.Equal(t, int64(1), sr.Status.ObservedGeneration)
		})
	}
}
