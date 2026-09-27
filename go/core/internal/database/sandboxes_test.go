package database

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func sandboxFixture(t *testing.T, client *Client, revision string) (*apiv1alpha1.Sandbox, SandboxCreateOptions) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, client.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", DesiredRevision: revision}))
	require.NoError(t, client.RecordSandboxRevision(ctx, SandboxRevision{RuntimeArtifact: RuntimeArtifact{Revision: revision, Kind: "sandbox", Namespace: "team-a", ActorTemplateAtespace: "team-a", ActorTemplateName: revision, ActorTemplateUID: "actor-uid"},
		SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", SourceSnapshot: []byte(`{}`)}, true))
	hash := sha256.Sum256([]byte("request"))
	return &apiv1alpha1.Sandbox{Id: uuid.NewString(), Creator: "alice", SandboxTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "scratch"}, Name: "experiment"},
		SandboxCreateOptions{RequestHash: hash[:], TemplateUID: "uid", TTL: time.Hour}
}

func finishSandbox(t *testing.T, client *Client, id string, kind apiv1alpha1.RuntimeOperation) *apiv1alpha1.Sandbox {
	t.Helper()
	op, err := client.BeginSandboxOperation(t.Context(), id, kind)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSandboxOperation(t.Context(), id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	result, err := client.FinishSandboxOperation(t.Context(), id, op.ID, executor, "")
	require.NoError(t, err)
	return result
}

func TestSandboxCreationPinsRetriesAndRetainsTombstones(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	input, options := sandboxFixture(t, client, "revision-1")
	original := proto.CloneOf(input)
	created, wasCreated, err := client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.True(t, proto.Equal(original, input), "creation must not mutate the caller's protobuf")
	require.Equal(t, "experiment", created.Name)
	require.Equal(t, time.Hour, created.ExpiresAt.AsTime().Sub(created.CreatedAt.AsTime()))
	ready := finishSandbox(t, client, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ready.State)
	_, _ = sandboxFixture(t, client, "revision-2")
	options.TTL = 2 * time.Hour
	retried, wasCreated, err := client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, "revision-1", retried.PreparedRevision)
	require.Equal(t, created.ExpiresAt, retried.ExpiresAt)
	options.RequestHash = make([]byte, 32)
	_, _, err = client.CreateSandbox(ctx, input, "create", options)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	hash := sha256.Sum256([]byte("request"))
	options.RequestHash = hash[:]
	require.NoError(t, client.RetireSandboxTemplateIdentities(ctx, "team-a", "scratch"))
	_, _, err = client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	_, err = client.GetSandbox(ctx, created.Id, "mallory")
	require.ErrorIs(t, err, ErrNotFound)
	deleted := finishSandbox(t, client, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	_, _, err = client.CreateSandbox(ctx, input, "create", options)
	require.ErrorIs(t, err, ErrFailedPrecondition)
	garbage, err := client.BeginRuntimeRevisionDeletion(ctx, "revision-1")
	require.NoError(t, err)
	require.NotNil(t, garbage)
}

func TestSandboxConcurrentCreation(t *testing.T) {
	const requests = 12
	for _, tc := range []struct {
		name       string
		distinct   bool
		conflicts  bool
		wantCount  int
		wantErrors int
	}{
		{name: "duplicate requests", wantCount: 1},
		{name: "distinct requests exceed former owner limit", distinct: true, wantCount: requests},
		{name: "conflicting retries", conflicts: true, wantCount: 1, wantErrors: requests / 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t))
			input, options := sandboxFixture(t, client, "revision")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			type result struct {
				instance *apiv1alpha1.Sandbox
				created  bool
				err      error
			}
			start := make(chan struct{})
			results := make(chan result, requests)
			for i := range requests {
				request := proto.CloneOf(input)
				request.Id = uuid.NewString()
				requestID := "create"
				if tc.distinct {
					requestID = uuid.NewString()
				}
				options := options
				if tc.conflicts && i%2 == 0 {
					hash := sha256.Sum256([]byte("different request"))
					options.RequestHash = hash[:]
				}
				go func() {
					<-start
					instance, created, err := client.CreateSandbox(ctx, request, requestID, options)
					results <- result{instance: instance, created: created, err: err}
				}()
			}
			close(start)
			var first *apiv1alpha1.Sandbox
			created, conflicts := 0, 0
			for range requests {
				result := <-results
				if result.err != nil {
					require.ErrorIs(t, result.err, ErrIdempotencyConflict)
					require.False(t, result.created)
					conflicts++
					continue
				}
				if result.created {
					created++
				}
				if first == nil {
					first = result.instance
				} else if !tc.distinct {
					require.True(t, proto.Equal(first, result.instance), "all matching retries must return the winning resource")
				}
			}
			require.Equal(t, tc.wantCount, created)
			require.Equal(t, tc.wantErrors, conflicts)
			instances, err := client.ListSandboxes(ctx, input.Creator, "", 100, nil)
			require.NoError(t, err)
			require.Len(t, instances, tc.wantCount)
		})
	}
}

func TestSandboxExpirationPaginationAndFailureGeneration(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	input, options := sandboxFixture(t, client, "revision")
	live, _, err := client.CreateSandbox(ctx, input, "live", options)
	require.NoError(t, err)
	pending, err := client.ListExpiredSandboxes(ctx, "", 100)
	require.NoError(t, err)
	require.Empty(t, pending, "pending lifecycle work is not background work")
	options.TTL = time.Second
	var ids []string
	for range 3 {
		input.Id = uuid.NewString()
		instance, _, err := client.CreateSandbox(ctx, input, uuid.NewString(), options)
		require.NoError(t, err)
		ids = append(ids, instance.Id)
	}
	require.Eventually(t, func() bool {
		expired, err := client.ListExpiredSandboxes(ctx, "", 100)
		return err == nil && len(expired) == 3
	}, 3*time.Second, 10*time.Millisecond)
	first, err := client.ListExpiredSandboxes(ctx, "", 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	second, err := client.ListExpiredSandboxes(ctx, first[1], 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, ids, append(first, second...))
	op, err := client.BeginSandboxOperation(ctx, live.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	require.NoError(t, client.RecordSandboxOperationFailure(ctx, live.Id, op.ID))
	failed, err := client.GetSandbox(ctx, live.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "RuntimeUnavailable", failed.Failure.Reason)
	completed := finishSandbox(t, client, live.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, client.RecordSandboxOperationFailure(ctx, live.Id, op.ID))
	ready, err := client.GetSandbox(ctx, live.Id, "alice")
	require.NoError(t, err)
	require.Nil(t, ready.Failure, "late failure cannot overwrite successful completion")
	require.True(t, proto.Equal(completed, ready), "late failure must not change the stored payload or update timestamp")
}

func TestSandboxLifecycleSupersedesEndedAttempts(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	input, options := sandboxFixture(t, client, "revision")
	created, _, err := client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	finishSandbox(t, client, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	suspend, err := client.BeginSandboxOperation(ctx, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSandboxOperation(ctx, created.Id, suspend.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.BeginSandboxOperation(ctx, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.ErrorIs(t, err, ErrConflict, "active execution must finish before changing intent")
	require.NoError(t, client.ReleaseRuntimeOperation(ctx, created.Id, suspend.ID, executor))
	resume, err := client.BeginSandboxOperation(ctx, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.NoError(t, err)
	require.NotEqual(t, suspend.ID, resume.ID)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, resume.Instance.Operation,
		"the recorded READY state must not hide an in-flight suspension")
	_, err = client.FinishSandboxOperation(ctx, created.Id, suspend.ID, executor, "")
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, client.RecordSandboxOperationFailure(ctx, created.Id, suspend.ID))
	current, err := client.GetSandboxOperation(ctx, created.Id)
	require.NoError(t, err)
	require.Equal(t, resume.ID, current.ID)
	require.Nil(t, current.Instance.Failure)
	executor = uuid.New()
	claimed, err = client.ClaimSandboxOperation(ctx, created.Id, resume.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, client.ReleaseRuntimeOperation(ctx, created.Id, resume.ID, executor))
	deletion, err := client.BeginSandboxOperation(ctx, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	require.NotEqual(t, resume.ID, deletion.ID)
	_, err = client.FinishSandboxOperation(ctx, created.Id, resume.ID, executor, "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.BeginSandboxOperation(ctx, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.ErrorIs(t, err, ErrFailedPrecondition, "deletion must not be canceled into leaked compute")
	finishSandbox(t, client, created.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
}
