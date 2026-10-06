package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPushRegistrationExpiresUnacceptedInput(t *testing.T) {
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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
	client := NewClient(setupTestDB(t))
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

func TestPushOutboxRetriesAndRejectsTerminalRegistration(t *testing.T) {
	client := NewClient(setupTestDB(t))
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
        UPDATE session_push_outbox SET next_attempt_at = clock_timestamp() - interval '1 second'
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
	require.NotEqual(t, first.ID, terminal.ID)
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

func TestPushOutboxBackoffAndAttemptLimit(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})

	for attempt := 1; attempt <= 10; attempt++ {
		delivery, err := client.ClaimDuePushDelivery(ctx)
		require.NoError(t, err)
		require.NotNil(t, delivery)
		require.NoError(t, client.FinishPushDelivery(ctx, *delivery, false))

		var state string
		var count int
		var delaySeconds float64
		require.NoError(t, client.db.QueryRow(ctx, `
            SELECT state, attempt_count,
                EXTRACT(EPOCH FROM (next_attempt_at - clock_timestamp()))::double precision
            FROM session_push_outbox WHERE id = $1
        `, delivery.ID).Scan(&state, &count, &delaySeconds))
		require.Equal(t, attempt, count)
		if attempt == 10 {
			require.Equal(t, "failed", state)
			break
		}
		require.Equal(t, "pending", state)
		wantSeconds := float64(min(5<<(attempt-1), 300))
		require.InDelta(t, wantSeconds, delaySeconds, 1)
		blocked, err := client.ClaimDuePushDelivery(ctx)
		require.NoError(t, err)
		require.Nil(t, blocked)
		require.NoError(t, execSQL(ctx, client.db, `
            UPDATE session_push_outbox SET next_attempt_at = clock_timestamp() - interval '1 second'
            WHERE id = $1
        `, delivery.ID))
	}
	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked, "exhausted delivery must not be claimed again")
}

func TestPushOutboxExpiredFinalLeaseExhaustsDelivery(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	delivery, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, delivery)
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_outbox SET attempt_count = $2,
            lease_until = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, delivery.ID, 10))

	blocked, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked)
	var state, lastError string
	require.NoError(t, client.db.QueryRow(ctx, `
        SELECT state, last_error FROM session_push_outbox WHERE id = $1
    `, delivery.ID).Scan(&state, &lastError))
	require.Equal(t, "failed", state)
	require.Equal(t, "delivery lease expired after final attempt", lastError)
	require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	require.NoError(t, client.db.QueryRow(ctx, `
        SELECT state FROM session_push_outbox WHERE id = $1
    `, delivery.ID).Scan(&state))
	require.Equal(t, "failed", state, "late acknowledgement cannot revive an exhausted delivery")
}

func TestPushOutboxKeepsDistinctPublishedBoundaries(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("second boundary"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))

	states := map[a2a.TaskState]bool{}
	for range 2 {
		delivery, err := client.ClaimDuePushDelivery(ctx)
		require.NoError(t, err)
		require.NotNil(t, delivery)
		wire := &a2apb.StreamResponse{}
		require.NoError(t, proto.Unmarshal(delivery.Payload, wire))
		event, err := pbconv.FromProtoStreamResponse(wire)
		require.NoError(t, err)
		update, ok := event.(*a2a.TaskStatusUpdateEvent)
		require.True(t, ok)
		states[update.Status.State] = true
		require.NoError(t, client.FinishPushDelivery(ctx, *delivery, true))
	}
	require.Equal(t, map[a2a.TaskState]bool{a2a.TaskStateInputRequired: true, a2a.TaskStateCompleted: true}, states)
	next, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, next, "repeated settlement must not duplicate notifications")
}

func TestPushOutboxExpiredLeaseFencesOldWorker(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	_, _ = waitingTaskWithPushFixture(t, client, &a2a.PushConfig{ID: "callback", URL: "http://receiver"})
	first, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NoError(t, execSQL(ctx, client.db, `
        UPDATE session_push_outbox SET lease_until = clock_timestamp() - interval '1 second'
        WHERE id = $1
    `, first.ID))
	second, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, first.ID, second.ID)
	require.NotEqual(t, first.ClaimToken, second.ClaimToken)
	require.NoError(t, client.FinishPushDelivery(ctx, *first, true))
	var state string
	require.NoError(t, client.db.QueryRow(ctx, `SELECT state FROM session_push_outbox WHERE id = $1`, first.ID).Scan(&state))
	require.Equal(t, "sending", state)
	require.NoError(t, client.FinishPushDelivery(ctx, *second, true))
	require.NoError(t, client.db.QueryRow(ctx, `SELECT state FROM session_push_outbox WHERE id = $1`, first.ID).Scan(&state))
	require.Equal(t, "delivered", state)
}

func TestPushOutboxReplacementAndDeleteCancelOldWork(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskWithPushFixture(t, client,
		&a2a.PushConfig{ID: "callback", URL: "http://old"})
	old, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.NotNil(t, old)
	require.NoError(t, client.SaveTaskPushConfig(ctx, session.Id, string(task.ID),
		&a2a.PushConfig{ID: "callback", URL: "http://new"}))
	var state string
	require.NoError(t, client.db.QueryRow(ctx, `SELECT state FROM session_push_outbox WHERE id = $1`, old.ID).Scan(&state))
	require.Equal(t, "canceled", state)
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
	require.NoError(t, client.db.QueryRow(ctx, `SELECT state FROM session_push_outbox WHERE id = $1`, newDelivery.ID).Scan(&state))
	require.Equal(t, "canceled", state)
	require.NoError(t, client.FinishPushDelivery(ctx, *newDelivery, true))
	remaining, err := client.ClaimDuePushDelivery(ctx)
	require.NoError(t, err)
	require.Nil(t, remaining)
}
