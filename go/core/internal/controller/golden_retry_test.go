package controller

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const goldenCrashMessage = "GoldenActorCrashed: golden actor crashed before its snapshot was taken"

// withGoldenStatus returns a copy because observations keep the template they read.
func withGoldenStatus(template *ateapipb.ActorTemplate, golden *ateapipb.GoldenSnapshotStatus) *ateapipb.ActorTemplate {
	updated := proto.CloneOf(template)
	updated.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: golden}
	return updated
}

func crashed(template *ateapipb.ActorTemplate) *ateapipb.ActorTemplate {
	return withGoldenStatus(template, &ateapipb.GoldenSnapshotStatus{ErrorMessage: goldenCrashMessage})
}

func TestGoldenRetryPolicyJitterBounds(t *testing.T) {
	policy := defaultGoldenRetryPolicy
	require.Equal(t, []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute}, policy.jitteredDelays(func() float64 { return 0 }))
	require.Equal(t, []time.Duration{18 * time.Second, 72 * time.Second, 288 * time.Second}, policy.jitteredDelays(func() float64 { return 1 }))
}

func TestPreparationRetriesGoldenCrash(t *testing.T) {
	for _, recovers := range []bool{true, false} {
		name := "budget exhausted"
		if recovers {
			name = "recovers on retry"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, _ := newPreparationTestCollections(t, "microvm")
				initial := collections.Reconciliations.List()[0]
				templates := &fakeActorTemplates{uids: []string{"uid-2", "uid-3", "uid-4"}}
				templates.template = crashed(initial.Target.ActorTemplate)
				templates.template.Metadata.Uid = "uid-1"
				store := &fakeRuntimeRevisionStore{}
				backend := &preparationTestBackend{templates: templates, store: store}
				statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
				r := newReconciler(collections, backend, backend, statusClient)
				r.random = func() float64 { return 1 }
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go func() { assert.NoError(t, r.Start(ctx)) }()
				ready := func() *metav1.Condition {
					published, err := statusClient.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
					require.NoError(t, err)
					return apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady)
				}
				synctest.Wait()

				condition := ready()
				require.Equal(t, "ActorTemplateRetrying", condition.Reason)
				require.Contains(t, condition.Message, "retry 1 of 3")
				require.Contains(t, condition.Message, goldenCrashMessage)
				require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
				backend.inspect(func() {
					require.Equal(t, 1, store.goldenAttempts, "repeated passes over one crash must spend one retry")
					require.Empty(t, templates.deletedUIDs, "the crashed template stays until its backoff ends")
				})

				delays := []time.Duration{18 * time.Second, 72 * time.Second, 288 * time.Second}
				for retry, delay := range delays {
					condition := ready()
					require.Equal(t, "ActorTemplateRetrying", condition.Reason)
					require.Contains(t, condition.Message, fmt.Sprintf("retry %d of 3", retry+1))
					time.Sleep(delay - 2*time.Second)
					synctest.Wait()
					backend.inspect(func() { require.Len(t, templates.deletedUIDs, retry, "no recreate before the backoff ends") })
					time.Sleep(3 * time.Second)
					synctest.Wait()
					backend.inspect(func() {
						require.Len(t, templates.deletedUIDs, retry+1)
						require.Equal(t, store.goldenCrashUID, templates.deletedUIDs[retry], "the delete must name the crashed template's UID")
						require.NotNil(t, templates.template, "the next pass recreates the template under the same name")
						require.Equal(t, initial.Target.ActorTemplate.GetMetadata().GetName(), templates.template.GetMetadata().GetName())
					})
					require.Equal(t, "ActorTemplatePending", ready().Reason)
					if recovers {
						backend.inspect(func() {
							templates.template = withGoldenStatus(templates.template, &ateapipb.GoldenSnapshotStatus{
								GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
							})
						})
						time.Sleep(2 * time.Second)
						synctest.Wait()
						require.Equal(t, metav1.ConditionTrue, ready().Status)
						backend.inspect(func() { require.True(t, store.markedSuccessful) })
						cancel()
						synctest.Wait()
						return
					}
					backend.inspect(func() { templates.template = crashed(templates.template) })
					time.Sleep(time.Second)
					synctest.Wait()
				}

				condition = ready()
				require.Equal(t, "ActorTemplateFailed", condition.Reason, "an exhausted budget must be terminal")
				require.Contains(t, condition.Message, "crashed 4 times")
				require.Contains(t, condition.Message, goldenCrashMessage)
				time.Sleep(30 * time.Minute)
				synctest.Wait()
				backend.inspect(func() {
					require.Len(t, templates.deletedUIDs, 3, "no retry after the budget")
					require.Equal(t, 3, store.goldenAttempts)
				})
				cancel()
				synctest.Wait()
			})
		})
	}
}
