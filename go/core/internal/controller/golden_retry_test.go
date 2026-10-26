package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
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

// maxJitter is wait.Jitter with its random draw at 1.
func maxJitter(duration time.Duration, maxFactor float64) time.Duration {
	return duration + time.Duration(maxFactor*float64(duration))
}

// Substrate has no structured golden failure reason, so isGoldenCrash parses
// error_message. Fail here when a Substrate bump changes how it is built.
func TestGoldenCrashFormatMatchesSubstrate(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "go.ate.dev/substrate").Output()
	require.NoError(t, err)
	source, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "cmd/ateapi/internal/controlapi/template_reconciler.go"))
	require.NoError(t, err)
	for _, pinned := range []string{
		`reasonGoldenActorCrashed\s*=\s*"GoldenActorCrashed"\n`,
		`r\.fail\(ctx, tmpl, reasonGoldenActorCrashed, "golden actor crashed before its snapshot was taken"\)`,
		`ErrorMessage = truncateUTF8\(reason\+": "\+msg, maxGoldenErrorMessageLen\)`,
	} {
		require.Regexp(t, pinned, string(source), "Substrate changed its golden crash error_message; update goldenCrashPrefix and goldenCrashMessage")
	}
	require.Equal(t, "GoldenActorCrashed"+": "+"golden actor crashed before its snapshot was taken", goldenCrashMessage)
	require.True(t, isGoldenCrash(crashed(&ateapipb.ActorTemplate{})))
	for _, terminal := range []string{"GoldenActorInvalid: bad", "GoldenTagConflict: golden tag belongs to another actor or template", "golden actor crashed"} {
		require.False(t, isGoldenCrash(withGoldenStatus(&ateapipb.ActorTemplate{}, &ateapipb.GoldenSnapshotStatus{ErrorMessage: terminal})), terminal)
	}
}

func TestPreparationRetriesGoldenCrash(t *testing.T) {
	for _, recovers := range []bool{true, false} {
		name := "keeps retrying past the fast backoffs"
		if recovers {
			name = "recovers on retry"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, _ := newPreparationTestCollections(t, "microvm")
				initial := collections.Reconciliations.List()[0]
				templates := &fakeActorTemplates{queuedUIDs: []string{"uid-2", "uid-3", "uid-4", "uid-5", "uid-6"}}
				templates.template = crashed(initial.Target.ActorTemplate)
				templates.template.Metadata.Uid = "uid-1"
				store := &fakeRuntimeRevisionStore{}
				backend := &preparationTestBackend{templates: templates, store: store}
				statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
				r := newReconciler(collections, backend, backend, statusClient)
				r.golden.jitter = maxJitter
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
				require.Contains(t, condition.Message, "retry 1 at")
				require.Contains(t, condition.Message, goldenCrashMessage)
				for range 2 {
					r.agents.Add(initial.ResourceName())
					synctest.Wait()
				}
				require.Equal(t, condition.Message, ready().Message, "repeated passes over one crash must keep its retry and schedule")
				backend.inspect(func() { require.Empty(t, templates.deletedUIDs, "the crashed template stays until its backoff ends") })

				delays := []time.Duration{18 * time.Second, 72 * time.Second, 288 * time.Second, 12 * time.Minute, 12 * time.Minute}
				for retry, delay := range delays {
					condition := ready()
					require.Equal(t, "ActorTemplateRetrying", condition.Reason, "ActorTemplateFailed is reserved for terminal golden errors")
					require.Contains(t, condition.Message, fmt.Sprintf("retry %d at", retry+1))
					require.Contains(t, condition.Message, goldenCrashMessage)
					time.Sleep(delay - 2*time.Second)
					synctest.Wait()
					backend.inspect(func() { require.Len(t, templates.deletedUIDs, retry, "no recreate before the backoff ends") })
					time.Sleep(3 * time.Second)
					synctest.Wait()
					backend.inspect(func() {
						require.Len(t, templates.deletedUIDs, retry+1)
						require.Equal(t, fmt.Sprintf("uid-%d", retry+1), templates.deletedUIDs[retry], "the delete must name the crashed template's UID")
						require.NotNil(t, templates.template, "the next pass recreates the template under the same name")
						require.Equal(t, initial.Target.ActorTemplate.GetMetadata().GetName(), templates.template.GetMetadata().GetName())
					})
					condition = ready()
					require.Equal(t, "ActorTemplatePending", condition.Reason)
					require.Equal(t, fmt.Sprintf("rebuilding after golden crash, retry %d; waiting for the ActorTemplate golden snapshot", retry+1), condition.Message)
					if recovers || retry == len(delays)-1 {
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
			})
		})
	}
}

func TestPreparationGoldenRetrySurvivesRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collections, _ := newPreparationTestCollections(t, "microvm")
		initial := collections.Reconciliations.List()[0]
		templates := &fakeActorTemplates{queuedUIDs: []string{"uid-2"}}
		templates.template = crashed(initial.Target.ActorTemplate)
		templates.template.Metadata.Uid = "uid-1"
		store := &fakeRuntimeRevisionStore{}
		statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
		message := func() string {
			published, err := statusClient.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
			require.NoError(t, err)
			return apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady).Message
		}
		retryMessage := func(at time.Time) string {
			return fmt.Sprintf("golden build crashed, retry 1 at %s: %s", at.UTC().Format(time.RFC3339), goldenCrashMessage)
		}
		start := func(collections Collections) (*preparationTestBackend, context.CancelFunc) {
			backend := &preparationTestBackend{templates: templates, store: store}
			r := newReconciler(collections, backend, backend, statusClient)
			r.golden.jitter = maxJitter
			ctx, cancel := context.WithCancel(t.Context())
			go func() { assert.NoError(t, r.Start(ctx)) }()
			synctest.Wait()
			return backend, cancel
		}

		backend, stop := start(collections)
		retryAt := time.Now().Add(18 * time.Second)
		require.Equal(t, retryMessage(retryAt), message())
		time.Sleep(time.Until(retryAt) / 2)
		stop()
		synctest.Wait()

		// Move the stored retry time so the old timer, firing at the old time,
		// would show up as an early recreate.
		storedRetryAt := retryAt.Add(10 * time.Second)
		backend.inspect(func() { store.goldenRetryAt = storedRetryAt })
		restarted, _ := newPreparationTestCollections(t, "microvm")
		backend, stop = start(restarted)
		defer stop()
		time.Sleep(time.Until(retryAt) + time.Second)
		synctest.Wait()
		backend.inspect(func() { require.Empty(t, templates.deletedUIDs, "the stopped controller's timer must stay idle") })
		time.Sleep(time.Until(storedRetryAt) - time.Millisecond)
		synctest.Wait()
		require.Equal(t, retryMessage(storedRetryAt), message(), "a restart must not spend another retry")
		backend.inspect(func() { require.Empty(t, templates.deletedUIDs, "no recreate before the stored retry time") })
		time.Sleep(time.Millisecond)
		synctest.Wait()
		backend.inspect(func() {
			require.Equal(t, []string{"uid-1"}, templates.deletedUIDs, "the restarted controller recreates at the stored retry time")
			require.Equal(t, "uid-2", templates.template.GetMetadata().GetUid())
		})
		stop()
		synctest.Wait()
	})
}

func TestPreparationGoldenRetryKeepsRetryingOnDeleteError(t *testing.T) {
	for _, code := range []codes.Code{codes.Aborted, codes.Internal} {
		t.Run(code.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, _ := newPreparationTestCollections(t, "microvm")
				initial := collections.Reconciliations.List()[0]
				templates := &fakeActorTemplates{queuedUIDs: []string{"uid-2"}, deleteErr: status.Error(code, "delete failed")}
				templates.template = crashed(initial.Target.ActorTemplate)
				templates.template.Metadata.Uid = "uid-1"
				backend := &preparationTestBackend{templates: templates, store: &fakeRuntimeRevisionStore{}}
				statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
				r := newReconciler(collections, backend, backend, statusClient)
				r.golden.jitter = maxJitter
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go func() { assert.NoError(t, r.Start(ctx)) }()
				ready := func() *metav1.Condition {
					published, err := statusClient.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
					require.NoError(t, err)
					return apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady)
				}
				synctest.Wait()

				time.Sleep(18*time.Second + 5*time.Second)
				synctest.Wait()
				condition := ready()
				require.Equal(t, "ActorTemplateRetrying", condition.Reason, "a failed delete must not hide the crash")
				require.Equal(t, fmt.Sprintf("golden build crashed, retry 1 waits to delete the crashed template (%s): %s", code, goldenCrashMessage), condition.Message)
				backend.inspect(func() {
					require.Greater(t, len(templates.deletedUIDs), 1, "the delete must be requeued")
					templates.deleteErr = nil
				})
				time.Sleep(30 * time.Second)
				synctest.Wait()
				backend.inspect(func() { require.Equal(t, "uid-2", templates.template.GetMetadata().GetUid()) })
				condition = ready()
				require.Equal(t, "ActorTemplatePending", condition.Reason)
				require.Contains(t, condition.Message, "rebuilding after golden crash, retry 1;")
				cancel()
				synctest.Wait()
			})
		})
	}
}

type failingRecordStore struct {
	*database.Client
	recordErr error
}

func (s *failingRecordStore) RecordRuntimeRevision(ctx context.Context, revision database.RuntimeRevision, options database.RecordRevisionOptions) error {
	if s.recordErr != nil {
		return s.recordErr
	}
	return s.Client.RecordRuntimeRevision(ctx, revision, options)
}

// A rebuild created after the crash was deleted, but never recorded, must not
// wedge GC on the recorded crash's UID.
func TestRuntimeRevisionGCCollectsUnrecordedGoldenRebuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	ctx := t.Context()
	dsn := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := database.Connect(ctx, &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	collections, _ := newPreparationTestCollections(t, "microvm")
	initial := collections.Reconciliations.List()[0]
	revision := initial.Target.RevisionID.String()
	templates := &fakeActorTemplates{queuedUIDs: []string{"uid-2"}}
	templates.template = crashed(initial.Target.ActorTemplate)
	templates.template.Metadata.Uid = "uid-1"
	recording := &failingRecordStore{Client: store}
	r := newReconciler(collections, templates, recording, kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3())
	r.golden.policy = goldenRetryPolicy{Delays: []time.Duration{0}}

	require.NoError(t, r.reconcileAgent(ctx, initial.ResourceName()))
	require.Equal(t, []string{"uid-1"}, templates.deletedUIDs)
	recording.recordErr = errors.New("leader lease lost")
	require.ErrorIs(t, r.reconcileAgent(ctx, initial.ResourceName()), recording.recordErr)
	require.Equal(t, "uid-2", templates.template.GetMetadata().GetUid())
	require.NoError(t, store.RetireAgentIdentities(ctx, initial.Agent.Namespace, initial.Agent.Name))

	collector, _ := newTestRuntimeRevisionGC(t, store, templates)
	require.NoError(t, collector.collect(ctx, revision))
	require.Equal(t, []string{"uid-1", "uid-2"}, templates.deletedUIDs, "GC must delete the rebuild by its own UID")
	require.Nil(t, templates.template)
	_, err = store.GetRuntimeRevision(ctx, revision)
	require.ErrorIs(t, err, database.ErrNotFound)
}
