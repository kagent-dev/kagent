package database

import (
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPushPendingDeliveryTracksRetriesAndLeases(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	assertPending := func(want bool) {
		t.Helper()
		pending, err := client.HasPendingPushDelivery(ctx)
		require.NoError(t, err)
		require.Equal(t, want, pending)
	}
	assertPending(false)
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	assertPending(true)
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	assertPending(true) // An active lease must still be checked for abandonment.
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))
	assertPending(true) // Future retries are outstanding even when not claimable.
	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked)
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_registration SET next_attempt_at = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, delivery.ID))
	delivery, err = client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	assertPending(false)
}

func TestPushPendingDeliveryExcludesCanceledWork(t *testing.T) {
	for _, action := range []string{"delete", "replace"} {
		t.Run(action, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			config := &a2a.PushConfig{ID: "callback", URL: "http://receiver"}
			session, task := waitingTaskWithPushFixture(t, client, config)
			pending, err := client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.True(t, pending)
			if action == "delete" {
				require.NoError(t, client.DeleteTaskPushConfig(ctx, session.Id, string(task.ID), config.ID))
			} else {
				config.URL = "http://replacement"
				require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), config))
			}
			pending, err = client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}

func TestPushDeliveryConcurrentClaimsAcrossReplicas(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	start := make(chan struct{})
	results := make(chan *PushDelivery, 8)
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			<-start
			delivery, err := NewClient(sharedDB, "public").ClaimDuePushDelivery(ctx)
			results <- delivery
			errors <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var claimed []*PushDelivery
	for delivery := range results {
		if delivery != nil {
			claimed = append(claimed, delivery)
		}
	}
	require.Len(t, claimed, 1, "replicas must not claim an active delivery twice")
	require.NoError(t, client.FinishPushDelivery(ctx, *claimed[0], true))
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPushRegistrationExpiresUnacceptedInput(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _ := waitingTaskFixture(t, client)
	config := &a2a.PushConfig{ID: "default", URL: "http://receiver"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "unaccepted", "", config))
	require.NoError(t, execSQL(ctx, client.db, `UPDATE session_push_registration SET created_at = clock_timestamp() - interval '11 minutes' WHERE initial_message_id = $1`, "unaccepted"))
	require.NoError(t, client.ExpireUnboundPushRegistrations(ctx))
	var closed bool
	require.NoError(t, client.db.QueryRow(ctx, `SELECT closed_at IS NOT NULL FROM session_push_registration WHERE initial_message_id = $1`, "unaccepted").Scan(&closed))
	require.True(t, closed)
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "unaccepted", "", config))
	require.ErrorIs(t, client.RegisterSessionPushNotification(ctx, session.Id, "unaccepted", "", &a2a.PushConfig{ID: "default", URL: "http://changed"}), ErrIdempotencyConflict)
}

func TestPushExpirationDoesNotCloseRegistrationBoundWhileWaiting(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	config := &a2a.PushConfig{ID: "callback", URL: "http://receiver"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "pending", "", config))
	require.NoError(t, execSQL(ctx, client.db, `UPDATE session_push_registration SET created_at = clock_timestamp() - interval '11 minutes' WHERE initial_message_id = $1`, "pending"))

	tx, err := client.db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE session_push_registration SET task_id = $2 WHERE initial_message_id = $1`, "pending", task.ID)
	require.NoError(t, err)

	expired := make(chan error, 1)
	go func() { expired <- client.ExpireUnboundPushRegistrations(ctx) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := client.db.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
					AND query LIKE '%UPDATE session_push_registration SET closed_at%'
					AND cardinality(pg_blocking_pids(pid)) > 0)
		`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "expiration should wait for the binding transaction")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-expired)
	var bound, closed bool
	require.NoError(t, client.db.QueryRow(ctx, `
		SELECT task_id IS NOT NULL, closed_at IS NOT NULL FROM session_push_registration
		WHERE initial_message_id = $1
	`, "pending").Scan(&bound, &closed))
	require.True(t, bound)
	require.False(t, closed)
}

func TestPushRegistrationConcurrentConfiguration(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	results := make(chan error, 2)
	for _, endpoint := range []string{"http://one", "http://two"} {
		go func() {
			results <- client.RegisterSessionPushNotification(t.Context(), session.Id, "input", "", &a2a.PushConfig{ID: "default", URL: endpoint})
		}()
	}
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, ErrIdempotencyConflict)
	} else {
		require.ErrorIs(t, first, ErrIdempotencyConflict)
		require.NoError(t, second)
	}
	for _, config := range []*a2a.PushConfig{{URL: "http://one"}, {ID: "default"}} {
		require.Error(t, client.RegisterSessionPushNotification(t.Context(), session.Id, "invalid", "", config))
	}
	require.Error(t, client.RegisterSessionPushNotification(t.Context(), session.Id, "", "", &a2a.PushConfig{ID: "default", URL: "http://one"}))
}

func TestTaskPushConfigManagement(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	initial := &a2a.PushConfig{ID: "default", URL: "http://initial"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "initial", "", initial))
	// Registration binds an already accepted input in the same transaction.
	got, err := client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "default")
	require.NoError(t, err)
	require.Equal(t, "http://initial", got.URL)
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), &a2a.PushConfig{ID: "second", URL: "http://second"}))
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery, "registration does not replay the published waiting state")
	rows, err := client.ListTaskPushConfigs(ctx, session.Id, string(task.ID), "", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"default", "second"}, []string{rows[0].ID, rows[1].ID})
	page, err := client.ListTaskPushConfigs(ctx, session.Id, string(task.ID), "default", 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "second", page[0].ID)
	// Replacing the embedded config keeps its initial-send receipt immutable.
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), &a2a.PushConfig{ID: "default", URL: "http://replacement"}))
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "initial", "", initial))
	got, err = client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "default")
	require.NoError(t, err)
	require.Equal(t, "http://replacement", got.URL)
	require.NoError(t, client.DeleteTaskPushConfig(ctx, session.Id, string(task.ID), "default"))
	require.NoError(t, client.DeleteTaskPushConfig(ctx, session.Id, string(task.ID), "default"))
	_, err = client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "default")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "initial", "", initial))
	_, err = client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "default")
	require.ErrorIs(t, err, ErrNotFound, "initial retry must not resurrect deleted configuration")
	// Explicit Create after Delete may reuse the configuration ID.
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), &a2a.PushConfig{ID: "default", URL: "http://new"}))
	got, err = client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "default")
	require.NoError(t, err)
	require.Equal(t, "http://new", got.URL)
	other, otherTask := waitingTaskFixture(t, client)
	_, err = client.GetTaskPushConfig(ctx, other.Id, string(task.ID), "default")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, client.SaveTaskPushConfig(ctx, other.Id, string(task.ID), &a2a.PushConfig{ID: "cross", URL: "http://cross"}), ErrNotFound)
	require.NotEqual(t, task.ID, otherTask.ID)
}

func TestPushRegistrationFingerprintRejectsChangedSend(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	original := &a2a.PushConfig{
		ID: "callback", URL: "http://original",
	}
	taskID := string(task.ID)
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "input", taskID, original))
	task.History = append(task.History, &a2a.Message{ID: "input", Role: a2a.MessageRoleUser})
	task.Status.State = a2a.TaskStateWorking
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery, "known-task registration is future-only")
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, taskID,
		&a2a.PushConfig{ID: "callback", URL: "http://replacement"}))
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "input", taskID, original))
	current, err := client.GetTaskPushConfig(ctx, session.Id, taskID, "callback")
	require.NoError(t, err)
	require.Equal(t, "http://replacement", current.URL)

	for _, test := range []struct {
		name   string
		taskID string
		config a2a.PushConfig
	}{
		{name: "task", taskID: "", config: *original},
		{name: "id", taskID: taskID, config: a2a.PushConfig{ID: "changed", URL: original.URL}},
		{name: "url", taskID: taskID, config: a2a.PushConfig{ID: original.ID, URL: "http://changed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorIs(t, client.RegisterSessionPushNotification(ctx, session.Id, "input", test.taskID, &test.config), ErrIdempotencyConflict)
		})
	}
	current, err = client.GetTaskPushConfig(ctx, session.Id, taskID, "callback")
	require.NoError(t, err)
	require.Equal(t, "http://replacement", current.URL)
}

func TestEmbeddedPushDoesNotReplaceExplicitConfiguration(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	explicit := &a2a.PushConfig{ID: "shared", URL: "http://explicit"}
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), explicit))
	embedded := &a2a.PushConfig{ID: "shared", URL: "http://embedded"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "later-input", string(task.ID), embedded))
	task.History = append(task.History, &a2a.Message{ID: "later-input", Role: a2a.MessageRoleUser})
	task.Status.State = a2a.TaskStateWorking
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	got, err := client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "shared")
	require.NoError(t, err)
	require.Equal(t, explicit.URL, got.URL)
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "later-input", string(task.ID), embedded))
	require.NoError(t, client.DeleteTaskPushConfig(ctx, session.Id, string(task.ID), "shared"))
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "later-input", string(task.ID), embedded))
	_, err = client.GetTaskPushConfig(ctx, session.Id, string(task.ID), "shared")
	require.ErrorIs(t, err, ErrNotFound, "retry must not resurrect a deleted explicit config")
}

func TestContinuationPushBindsWithAcceptedTaskWrite(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	config := &a2a.PushConfig{ID: "continuation", URL: "http://receiver"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "follow-up", string(task.ID), config))
	_, err := client.GetTaskPushConfig(ctx, session.Id, string(task.ID), config.ID)
	require.ErrorIs(t, err, ErrNotFound)
	task.History = append(task.History, &a2a.Message{ID: "follow-up", Role: a2a.MessageRoleUser})
	task.Status.State = a2a.TaskStateWorking
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))

	got, err := client.GetTaskPushConfig(ctx, session.Id, string(task.ID), config.ID)
	require.NoError(t, err)
	require.Equal(t, config.URL, got.URL)
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery, "working state is not a notification boundary")
}

func TestUnacceptedContinuationDoesNotSubscribeToTask(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	taskID := string(task.ID)
	config := &a2a.PushConfig{ID: "unaccepted", URL: "http://receiver"}
	require.NoError(t, client.RegisterSessionPushNotification(ctx, session.Id, "never-accepted", taskID, config))

	// Another task transition must not notify a callback from an input that
	// never reached the task write.
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, taskID)
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("complete without accepting input"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, taskID, version))
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery)
}

func TestPushDeliveryRetriesAndRejectsTerminalRegistration(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	config := &a2a.PushConfig{ID: "callback", URL: "http://receiver"}
	session, task := waitingTaskWithPushFixture(t, client, config)
	first, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NoError(t, client.FinishPushDelivery(ctx, *first, false))
	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked, "a failed delivery waits for its retry time")
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_registration SET next_attempt_at = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, first.ID))
	second, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.Payload, second.Payload)
	require.NotEqual(t, first.ClaimToken, second.ClaimToken)
	require.NoError(t, client.FinishPushDelivery(ctx, *first, true))
	require.NoError(t, client.FinishPushDelivery(ctx, *second, true))
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), config))
	blocked, err = client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked, "identical Create must not enqueue a duplicate")

	task.Status.State = a2a.TaskStateCompleted
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("complete for push"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	terminal, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, terminal)
	require.Equal(t, first.ID, terminal.ID)
	require.NotEqual(t, first.Sequence, terminal.Sequence)
	require.NoError(t, client.FinishPushDelivery(ctx, *terminal, true))
	late := &a2a.PushConfig{ID: "late", URL: "http://late"}
	require.ErrorIs(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), late), ErrFailedPrecondition)
	require.ErrorIs(t, client.RegisterSessionPushNotification(ctx, session.Id, "late-input", string(task.ID), late), ErrFailedPrecondition)
	require.ErrorIs(t, client.RegisterSessionPushNotification(ctx, session.Id, "initial", "", late), ErrFailedPrecondition,
		"an accepted initial send cannot add a callback after completion")
	lateDelivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, lateDelivery)
}

func TestPushDeliveryBackoffAndAttemptLimit(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})

	var final *PushDelivery
	for attempt := 1; attempt <= pushMaxDeliveryAttempts; attempt++ {
		delivery, err := client.ClaimDuePushDelivery(ctx)
		require.NoError(t, err)
		require.NotNil(t, delivery)
		require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))
		if attempt == pushMaxDeliveryAttempts {
			final = delivery
			assertPushDeliveryRetired(t, client, delivery.ID)
			break
		}

		var count int
		var delaySeconds float64
		require.NoError(t, client.db.QueryRow(ctx, `
            SELECT attempt_count,
                EXTRACT(EPOCH FROM (next_attempt_at - clock_timestamp()))::double precision
            FROM session_push_registration WHERE id = $1
        `, delivery.ID).Scan(&count, &delaySeconds))
		require.Equal(t, attempt, count)
		wantSeconds := float64(min(5<<(attempt-1), 300))
		require.InDelta(t, wantSeconds, delaySeconds, 1)
		blocked, err := client.ClaimDuePushDelivery(ctx)
		require.NoError(t, err)
		require.Nil(t, blocked)
		require.NoError(t, execSQL(ctx, client.db, `
            UPDATE session_push_registration SET next_attempt_at = clock_timestamp() - interval '1 second'
            WHERE id = $1
        `, delivery.ID))
	}
	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked, "exhausted delivery must not be claimed again")
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending, "exhausted work must not keep fast polling active")
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	next, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, next, "a new publication resets exhausted retries")
	require.Equal(t, final.ID, next.ID, "exhaustion retains the callback registration")
	require.NotEqual(t, final.Sequence, next.Sequence)
	require.NoError(t, client.FinishPushDelivery(ctx, *final, true))
	require.NoError(t, client.FinishPushDelivery(ctx, *final, false))
	current, err := client.IsPushDeliveryCurrent(ctx, *next)
	require.NoError(t, err)
	require.True(t, current, "an exhausted generation cannot clear newer work")
	var count int
	require.NoError(t, client.db.QueryRow(ctx, `SELECT attempt_count FROM session_push_registration WHERE id = $1`, next.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPushDeliveryExpiredFinalLeaseExhaustsDelivery(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_registration SET attempt_count = $2,
            lease_until = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, delivery.ID, pushMaxDeliveryAttempts))

	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked)
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending)
	assertPushDeliveryRetired(t, client, delivery.ID)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))
	current, err := client.IsPushDeliveryCurrent(ctx, *delivery)
	require.NoError(t, err)
	require.False(t, current, "late acknowledgement cannot revive an exhausted delivery")
	blocked, err = client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	next, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, next, "expiring the final lease retains the registration for future publications")
	require.Equal(t, delivery.ID, next.ID)
	require.NotEqual(t, delivery.Sequence, next.Sequence)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))
	current, err = client.IsPushDeliveryCurrent(ctx, *next)
	require.NoError(t, err)
	require.True(t, current, "an expired final claim cannot clear newer work")
}

func TestPushDeliveryCoalescesPublishedBoundaries(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	task.Artifacts = []*a2a.Artifact{{ID: "result", Parts: a2a.ContentParts{a2a.NewTextPart("done")}}}
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("second boundary"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))

	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.Equal(t, version, delivery.Sequence)
	update := pushStatusUpdate(t, delivery)
	require.Equal(t, a2a.TaskStateCompleted, update.Status.State)
	require.Equal(t, task.ID, update.TaskID)
	require.Equal(t, task.ContextID, update.ContextID)
	stored, err := client.GetSessionTask(ctx, session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.NotEmpty(t, stored.History, "coalescing retains task history")
	require.Equal(t, task.Artifacts, stored.Artifacts, "artifacts remain available through GetTask")
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	next, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, next, "settlement retries must not rearm acknowledged notifications")
}

func pushStatusUpdate(t *testing.T, delivery *PushDelivery) *a2a.TaskStatusUpdateEvent {
	t.Helper()
	wire := &a2apb.StreamResponse{}
	require.NoError(t, proto.Unmarshal(delivery.Payload, wire))
	event, err := pbconv.FromProtoStreamResponse(wire)
	require.NoError(t, err)
	update, ok := event.(*a2a.TaskStatusUpdateEvent)
	require.True(t, ok)
	require.Nil(t, wire.GetTask(), "callbacks retain the statusUpdate payload")
	return update
}

func TestPushDeliveryExpiredLeaseFencesOldWorker(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	first, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_registration SET lease_until = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, first.ID))
	second, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, first.ID, second.ID)
	require.NotEqual(t, first.ClaimToken, second.ClaimToken)
	require.NoError(t, client.FinishPushDelivery(ctx, *first, true))
	current, err := client.IsPushDeliveryCurrent(ctx, *second)
	require.NoError(t, err)
	require.True(t, current)
	require.NoError(t, client.FinishPushDelivery(ctx, *second, true))
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPushDeliveryReplacementAndDeleteCancelOldWork(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client,
		&a2a.PushConfig{ID: "callback", URL: "http://old"})
	old, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, old)
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID),
		&a2a.PushConfig{ID: "callback", URL: "http://new"}))
	current, err := client.IsPushDeliveryCurrent(ctx, *old)
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, client.FinishPushDelivery(ctx, *old, true))
	noReplay, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, noReplay, "replacement waits for a new published boundary")
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("replace push boundary"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	newDelivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, newDelivery)
	require.Equal(t, "http://new", newDelivery.URL)
	require.NoError(t, client.DeleteTaskPushConfig(ctx, session.Id, string(task.ID), "callback"))
	current, err = client.IsPushDeliveryCurrent(ctx, *newDelivery)
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, client.FinishPushDelivery(ctx, *newDelivery, true))
	remaining, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, remaining)
}

func TestPushDeliverySupersededOutcomeCannotChangeNewGeneration(t *testing.T) {
	for _, delivered := range []bool{true, false} {
		name := "failure"
		if delivered {
			name = "acknowledgement"
		}
		t.Run(name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
			old, err := client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.NotNil(t, old)
			task.Status.State = a2a.TaskStateCompleted
			require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
			current, err := client.IsPushDeliveryCurrent(ctx, *old)
			require.NoError(t, err)
			require.False(t, current)
			require.NoError(t, client.FinishPushDelivery(ctx, *old, delivered))
			next, err := client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.NotNil(t, next, "old outcomes cannot clear or delay unclaimed new work")
			require.Equal(t, old.ID, next.ID)
			require.NotEqual(t, old.Sequence, next.Sequence)
			require.NotEqual(t, old.ClaimToken, next.ClaimToken)
			require.NoError(t, client.FinishPushDelivery(ctx, *old, delivered))
			current, err = client.IsPushDeliveryCurrent(ctx, *next)
			require.NoError(t, err)
			require.True(t, current, "old outcomes cannot alter a newer claim")
			require.NoError(t, client.FinishPushDelivery(ctx, *next, true))
			pending, err := client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}

func TestPushDeliveryResumeClearsWaitingWork(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    string
		waiting a2a.TaskState
		active  a2a.TaskState
	}{
		{"pending input", "pending", a2a.TaskStateInputRequired, a2a.TaskStateWorking},
		{"leased input", "leased", a2a.TaskStateInputRequired, a2a.TaskStateWorking},
		{"input retry", "retry", a2a.TaskStateInputRequired, a2a.TaskStateWorking},
		{"auth to working", "pending", a2a.TaskStateAuthRequired, a2a.TaskStateWorking},
		{"input to submitted", "pending", a2a.TaskStateInputRequired, a2a.TaskStateSubmitted},
		{"auth to submitted", "pending", a2a.TaskStateAuthRequired, a2a.TaskStateSubmitted},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
			if task.Status.State != test.waiting {
				task.Status.State = test.waiting
				require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
			}
			var old *PushDelivery
			if test.mode != "pending" {
				var err error
				old, err = client.ClaimDuePushDelivery(ctx)
				require.NoError(t, err)
				require.NotNil(t, old)
				if test.mode == "retry" {
					require.NoError(t, client.FinishPushDelivery(ctx, *old, false))
				}
			}
			_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
			require.NoError(t, err)
			dispatch := uuid.New()
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatch, "continue"))
			input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
			input.ID, input.TaskID, input.ContextID = "continue", task.ID, task.ContextID
			task.History = append(task.History, input)
			version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("input-only"), task, input, dispatch.String())
			require.NoError(t, err)
			pending, err := client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.True(t, pending, "reservation and input-only saves do not prove execution resumed")
			_, err = client.RevokeSessionDispatch(ctx, session.Id, dispatch, "continue")
			require.NoError(t, err)
			task.Status = a2a.TaskStatus{State: test.active}
			_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("rejected resume"), task, task, dispatch.String())
			require.ErrorIs(t, err, ErrFailedPrecondition)
			pending, err = client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.True(t, pending, "a failed resume must leave pending work intact")
			dispatch = uuid.New()
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatch, ""))
			_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("resume"), task, task, dispatch.String())
			require.NoError(t, err)
			pending, err = client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.False(t, pending)
			if old != nil {
				require.NoError(t, client.FinishPushDelivery(ctx, *old, true))
				require.NoError(t, client.FinishPushDelivery(ctx, *old, false))
				current, err := client.IsPushDeliveryCurrent(ctx, *old)
				require.NoError(t, err)
				require.False(t, current)
			}
			next, err := client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.Nil(t, next, "resumption does not create a progress notification")
		})
	}
}

func TestPushDeliveryOldMutationRetryCannotClearLaterBoundary(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	_, firstVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(waiting.ID))
	require.NoError(t, err)
	working := *waiting
	working.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	activeVersion, err := client.UpdateSessionTask(ctx, session.Id, firstVersion, taskMutationHash("active"), &working, &working, "")
	require.NoError(t, err)
	waiting.Status.State = a2a.TaskStateAuthRequired
	boundaryVersion, err := client.UpdateSessionTask(ctx, session.Id, activeVersion, taskMutationHash("auth"), waiting, waiting, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(waiting.ID), boundaryVersion))
	version, err := client.UpdateSessionTask(ctx, session.Id, firstVersion, taskMutationHash("active"), &working, &working, "")
	require.NoError(t, err)
	require.Equal(t, activeVersion, version)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(waiting.ID), firstVersion))
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.Equal(t, boundaryVersion, delivery.Sequence)
	require.Equal(t, a2a.TaskStateAuthRequired, pushStatusUpdate(t, delivery).Status.State)
}

func TestPushDeliveryCapturesLatestPublishedProjection(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	_, boundaryVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("updated question"))
	task.Artifacts = []*a2a.Artifact{{ID: "draft", Parts: a2a.ContentParts{a2a.NewTextPart("published")}}}
	_, err = client.UpdateSessionTask(ctx, session.Id, boundaryVersion, taskMutationHash("update while waiting"), task, task, "")
	require.NoError(t, err)
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.Equal(t, boundaryVersion, delivery.Sequence, "same-state updates do not create a new trigger")
	update := pushStatusUpdate(t, delivery)
	require.Equal(t, task.Status.Message, update.Status.Message)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	task.Status.Message = a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("another question"))
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	delivery, err = client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery, "ordinary same-state updates do not rearm delivered notifications")
}

func TestPushDeliveryRegistrationConstraints(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _ := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	for _, query := range []string{
		`UPDATE session_push_registration SET task_id = NULL`,
		`UPDATE session_push_registration SET closed_at = clock_timestamp()`,
		`UPDATE session_push_registration SET next_attempt_at = NULL`,
		`UPDATE session_push_registration SET pending_event_sequence = NULL`,
		`UPDATE session_push_registration SET attempt_count = -1`,
		`UPDATE session_push_registration SET claim_token = '00000000-0000-0000-0000-000000000001'`,
		`UPDATE session_push_registration SET lease_until = clock_timestamp()`,
		`UPDATE session_push_registration SET claim_token = '00000000-0000-0000-0000-000000000001', lease_until = clock_timestamp()`,
		`UPDATE session_push_registration SET pending_event_sequence = 9223372036854775807`,
	} {
		t.Run(query, func(t *testing.T) {
			_, err := client.db.Exec(ctx, query)
			require.Error(t, err)
		})
	}
	otherTask := newSessionTask(uuid.NewString(), "another-input")
	otherTask.ContextID = session.ContextId
	_, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("another-task"), otherTask, "")
	require.NoError(t, err)
	_, err = client.db.Exec(ctx, `UPDATE session_push_registration SET task_id = $1`, otherTask.ID)
	require.Error(t, err, "a pending event must belong to the registration's task")
}

func TestPushDeliveryClaimSkipsUncommittedPublication(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("complete"), task, task, "")
	require.NoError(t, err)
	tx, err := client.db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT id FROM session_push_registration FOR UPDATE`)
	require.NoError(t, err)
	settled := make(chan error, 1)
	go func() { settled <- client.SettleSessionTask(ctx, session.Id, string(task.ID), version) }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := client.db.QueryRow(ctx, `
            SELECT EXISTS (SELECT 1 FROM pg_stat_activity
                WHERE datname = current_database()
                  AND query LIKE '%UPDATE session_push_registration SET pending_event_sequence%'
                  AND cardinality(pg_blocking_pids(pid)) > 0)
        `).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, time.Millisecond, "publication should wait for the registration lock")
	public, err := client.GetSessionTask(ctx, session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, public.Status.State, "publication is still uncommitted")
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, delivery, "claim must skip a registration being published")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-settled)
	delivery, err = client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.Equal(t, version, delivery.Sequence)
	require.Equal(t, a2a.TaskStateCompleted, pushStatusUpdate(t, delivery).Status.State)
}

func TestPushDeliveryRegistrationsHaveIndependentClaims(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "first", URL: "http://first"})
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID), &a2a.PushConfig{ID: "second", URL: "http://second"}))
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("complete"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	first, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotEqual(t, first.ID, second.ID)
	require.ElementsMatch(t, []string{"http://first", "http://second"}, []string{first.URL, second.URL})
	require.Equal(t, version, first.Sequence)
	require.Equal(t, version, second.Sequence)
	require.Equal(t, first.Payload, second.Payload)
	require.NoError(t, client.FinishPushDelivery(ctx, *first, true))
	current, err := client.IsPushDeliveryCurrent(ctx, *second)
	require.NoError(t, err)
	require.True(t, current, "acknowledgement is scoped to one registration")
	require.NoError(t, client.FinishPushDelivery(ctx, *second, true))
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPushDeliverySessionDeletionInvalidatesClaim(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _ := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.NoError(t, deleteSession(ctx, client, session.Id))
	current, err := client.IsPushDeliveryCurrent(ctx, *delivery)
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))
	pending, err := client.HasPendingPushDelivery(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPushDeliveryInvalidTaskDoesNotBlockOtherRegistrations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*a2apb.Task)
	}{
		{name: "unreadable protobuf"},
		{name: "invalid status", change: func(task *a2apb.Task) { task.Status.State = a2apb.TaskState(999) }},
		{name: "wrong task ID", change: func(task *a2apb.Task) { task.Id = "another-task" }},
		{name: "active state", change: func(task *a2apb.Task) { task.Status.State = a2apb.TaskState_TASK_STATE_WORKING }},
		{name: "unarchived history", change: func(task *a2apb.Task) {
			task.History = []*a2apb.Message{{MessageId: "unarchived", Role: a2apb.Role_ROLE_USER}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
			badSession, badTask := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "bad", URL: "http://bad"})
			_, goodTask := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "good", URL: "http://good"})
			session, err := readSession(ctx, client.db, badSession.Id)
			require.NoError(t, err)
			stored, err := readSessionTask(ctx, client.db, session.HistoryID, string(badTask.ID))
			require.NoError(t, err)
			var registrationID uuid.UUID
			require.NoError(t, client.db.QueryRow(ctx, `
                SELECT id FROM session_push_registration WHERE history_id = $1 AND config_id = 'bad'
            `, session.HistoryID).Scan(&registrationID))
			invalidData := []byte{0xff}
			if test.change != nil {
				wire := &a2apb.Task{}
				require.NoError(t, proto.Unmarshal(stored.Data, wire))
				test.change(wire)
				invalidData, err = proto.Marshal(wire)
				require.NoError(t, err)
			}
			// Inject malformed durable data; public task writes reject these cases.
			_, err = client.db.Exec(ctx, `UPDATE session_task SET data = $1 WHERE history_id = $2 AND id = $3`,
				invalidData, session.HistoryID, badTask.ID)
			require.NoError(t, err)
			delivery, err := client.ClaimDuePushDelivery(ctx)
			require.ErrorContains(t, err, "failed to prepare push task")
			require.Nil(t, delivery)
			var attempts int
			var scheduled, unleased bool
			require.NoError(t, client.db.QueryRow(ctx, `
                SELECT attempt_count, next_attempt_at > clock_timestamp(),
                    claim_token IS NULL AND lease_until IS NULL
                FROM session_push_registration WHERE config_id = 'bad'
            `).Scan(&attempts, &scheduled, &unleased))
			require.Equal(t, 1, attempts, "preparation failure must commit its attempt")
			require.True(t, scheduled, "a bad task must wait for backoff before being selected again")
			require.True(t, unleased, "a known preparation failure does not leave an HTTP lease")
			delivery, err = client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.NotNil(t, delivery, "the worker can reach the next healthy callback")
			require.Equal(t, string(goodTask.ID), delivery.TaskID)
			require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
			delivery, err = client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.Nil(t, delivery, "the bad callback must not be immediately retried")
			for attempt := 2; attempt <= pushMaxDeliveryAttempts; attempt++ {
				_, err := client.db.Exec(ctx, `
                    UPDATE session_push_registration SET next_attempt_at = clock_timestamp() - interval '1 second'
                    WHERE config_id = 'bad'
                `)
				require.NoError(t, err)
				delivery, err := client.ClaimDuePushDelivery(ctx)
				require.Error(t, err)
				require.Nil(t, delivery)
			}
			delivery, err = client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.Nil(t, delivery, "invalid task attempts eventually exhaust the retry budget")
			pending, err := client.HasPendingPushDelivery(ctx)
			require.NoError(t, err)
			require.False(t, pending)
			assertPushDeliveryRetired(t, client, registrationID)
			// Restore the task and publish a newer state; the callback still works.
			_, err = client.db.Exec(ctx, `UPDATE session_task SET data = $1 WHERE history_id = $2 AND id = $3`,
				stored.Data, session.HistoryID, badTask.ID)
			require.NoError(t, err)
			badTask.Status.State = a2a.TaskStateCompleted
			require.NoError(t, saveRuntimeTask(t, client, badSession.Id, badTask, badTask,
				&SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
			delivery, err = client.ClaimDuePushDelivery(ctx)
			require.NoError(t, err)
			require.NotNil(t, delivery)
			require.Equal(t, registrationID, delivery.ID)
			require.Equal(t, string(badTask.ID), delivery.TaskID)
			require.Equal(t, a2a.TaskStateCompleted, pushStatusUpdate(t, delivery).Status.State)
		})
	}
}

func assertPushDeliveryRetired(t *testing.T, client *Client, registrationID uuid.UUID) {
	t.Helper()
	var retired bool
	require.NoError(t, client.db.QueryRow(t.Context(), `
        SELECT pending_event_sequence IS NULL AND attempt_count = 0 AND next_attempt_at IS NULL
            AND claim_token IS NULL AND lease_until IS NULL AND closed_at IS NULL
        FROM session_push_registration WHERE id = $1
    `, registrationID).Scan(&retired))
	require.True(t, retired, "exhaustion clears delivery state and retains an active registration")
}
