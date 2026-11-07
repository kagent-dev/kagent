package checkpoint

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// This suite uses real Actors, snapshot copies, UID-conditional deletion and
// PostgreSQL. Faults discard actual RPC results or prevent one RPC from being
// delivered, or deny reads/deletes in an exclusively owned object-store bucket;
// no backend response or persisted lifecycle state is fabricated.
// The configuration is test-only; CI runs this after installing Substrate.
func TestCheckpointRecoveryWithSubstrate(t *testing.T) {
	path := os.Getenv("KAGENT_E2E_CHECKPOINT_SUBSTRATE_CONFIG")
	if path == "" {
		t.Skip("requires real Substrate; run core/test/e2e/checkpoint-recovery.sh")
	}
	var config struct {
		Endpoint             string `json:"endpoint"`
		ServerName           string `json:"serverName"`
		CAFile               string `json:"caFile"`
		ClientCertFile       string `json:"clientCertFile"`
		Atespace             string `json:"atespace"`
		Image                string `json:"image"`
		ObjectStoreEndpoint  string `json:"objectStoreEndpoint"`
		ObjectStoreAccessKey string `json:"objectStoreAccessKey"`
		ObjectStoreSecretKey string `json:"objectStoreSecretKey"`
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &config))
	require.NotEmpty(t, config.Endpoint)
	require.NotEmpty(t, config.Atespace)
	require.NotEmpty(t, config.Image)
	certificate, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientCertFile)
	require.NoError(t, err)
	ca, err := os.ReadFile(config.CAFile)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(ca))
	conn, err := grpc.NewClient(config.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: config.ServerName,
	})))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := &substrate.Client{ControlClient: ateapipb.NewControlClient(conn)}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	t.Cleanup(cancel)
	template, err := client.CreateActorTemplate(ctx, &ateapipb.ActorTemplate{
		Metadata:  &ateapipb.ResourceMetadata{Atespace: config.Atespace, Name: "checkpoint-recovery-" + uuid.NewString()},
		Resources: &ateapipb.ResourceRequirements{Limits: &ateapipb.Resources{CpuMillicores: 500, MemoryBytes: 256 << 20}},
		Containers: []*ateapipb.Container{{Name: "source", Image: config.Image,
			Command:      []string{"/bin/sh", "-c", "echo checkpoint-recovery > /data/proof; exec sleep 3600"},
			VolumeMounts: []*ateapipb.VolumeMount{{Name: "data", MountPath: "/data"}},
		}},
		Volumes: []*ateapipb.Volume{{Name: "data", DurableDir: &ateapipb.DurableDirVolumeSource{}}},
		SnapshotConfig: &ateapipb.SnapshotConfig{PreferredFidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
			GoldenSnapshotConfig: &ateapipb.GoldenSnapshotConfig{Mode: ateapipb.GoldenSnapshotMode_GOLDEN_SNAPSHOT_MODE_DISABLED}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		require.NoError(t, client.DeleteActorTemplate(cleanup, config.Atespace, template.Metadata.Name, template.Metadata.Uid))
	})
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	require.NoError(t, dbtest.Migrate(dsn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	newStore := func() *database.Client { return database.NewClient(pool, "public") }
	owner := auth.AuthSessionTo(ctx, testSession{userID: "alice"})
	newSourceForTemplate := func(t *testing.T, template *ateapipb.ActorTemplate) (*database.Client, *apiv1.Session, string) {
		t.Helper()
		name := substrate.ActorName(uuid.NewString())
		created, err := client.CreateActor(ctx, config.Atespace, name, config.Atespace, template.Metadata.Name)
		require.NoError(t, err)
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			err := client.DeleteActor(cleanup, config.Atespace, name, created.GetMetadata().GetUid())
			if status.Code(err) != codes.NotFound {
				require.NoError(t, err)
			}
		})
		_, err = client.ResumeActor(ctx, config.Atespace, name)
		require.NoError(t, err)
		actor, err := client.SuspendActor(ctx, config.Atespace, name)
		require.NoError(t, err)
		require.NotEmpty(t, substrate.SnapshotURI(substrate.LatestActorSnapshot(actor)))
		store := newStore()
		session, head := recoverySourceForActor(t, store, actor)
		return store, session, head
	}
	newSource := func(t *testing.T) (*database.Client, *apiv1.Session, string) {
		return newSourceForTemplate(t, template)
	}
	deleteCheckpoint := func(t *testing.T, service *Service, id string) {
		t.Helper()
		stop := startRecovery(t, service)
		require.NoError(t, service.Delete(owner, id))
		stop()
		_, err := service.store.GetCheckpointOperation(ctx, id, "alice")
		require.ErrorIs(t, err, database.ErrNotFound)
		_, err = client.GetTag(ctx, config.Atespace, tagName(id))
		require.Equal(t, codes.NotFound, status.Code(err))
	}
	cleanupTag := func(t *testing.T, id string) {
		t.Helper()
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			tag, err := client.GetTag(cleanup, config.Atespace, tagName(id))
			if status.Code(err) == codes.NotFound {
				return
			}
			require.NoError(t, err)
			require.NoError(t, client.DeleteTag(cleanup, config.Atespace, tagName(id), tag.GetMetadata().GetUid()))
		})
	}

	t.Run("Given an abandoned reservation, when a replacement starts, assert that it publishes a real retained copy", func(t *testing.T) {
		store, source, head := newSource(t)
		service := NewService(store, testAuthorizer{}, client, nil)
		request := uuid.NewString()
		cp, _, err := service.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		replacement := NewService(newStore(), testAuthorizer{}, client, nil)
		stop := startRecovery(t, replacement)
		ready, err := replacement.Create(owner, source.Id, request, head)
		require.NoError(t, err)
		stop()
		require.Equal(t, cp.Id, ready.Id)
		tag, err := client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.NoError(t, err)
		require.NotEmpty(t, substrate.SnapshotURI(tag.GetStatus().GetSnapshot()))
		clone := substrate.ActorName(uuid.NewString())
		cloned, err := client.CreateActorFromTag(ctx, config.Atespace, clone, config.Atespace, template.Metadata.Name, config.Atespace, tagName(cp.Id))
		require.NoError(t, err)
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			if err := client.DeleteActor(cleanup, config.Atespace, clone, cloned.GetMetadata().GetUid()); status.Code(err) != codes.NotFound {
				require.NoError(t, err)
			}
		})
		_, err = client.ResumeActor(ctx, config.Atespace, clone)
		require.NoError(t, err, "the recovered tag must contain a usable snapshot")
		require.NoError(t, client.DeleteActor(ctx, config.Atespace, clone, cloned.GetMetadata().GetUid()))
		deleteCheckpoint(t, replacement, cp.Id)
	})

	t.Run("Given a lost successful tag response, when recovery restarts, assert that it adopts the same backend UID", func(t *testing.T) {
		store, source, head := newSource(t)
		lost := &faultedSubstrateTags{Client: client, loseCreateResponse: true}
		service := NewService(store, testAuthorizer{}, lost, nil)
		request := uuid.NewString()
		cp, _, err := service.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Error(t, service.recoverOperation(ctx, op))
		tag, err := client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.NoError(t, err)
		pending, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, pending.Checkpoint.State)
		replacement := NewService(newStore(), testAuthorizer{}, client, nil)
		require.NoError(t, replacement.recoverOperation(ctx, pending))
		published, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, tag.Metadata.Uid, published.TagUID)
		deleteCheckpoint(t, replacement, cp.Id)
	})

	t.Run("Given a completed copy without database publication, when the worker restarts, assert that the original tag is published", func(t *testing.T) {
		store, source, head := newSource(t)
		fault := &failedCheckpointPublish{Client: store, entered: make(chan struct{})}
		old := NewService(fault, testAuthorizer{}, client, nil)
		request := uuid.NewString()
		cp, _, err := old.beginCreate(owner, source.Id, request, head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		attempt, cancelAttempt := context.WithCancel(ctx)
		defer cancelAttempt()
		done := make(chan error, 1)
		go func() { done <- old.recoverOperation(attempt, op) }()
		select {
		case <-fault.entered:
		case err := <-done:
			t.Fatalf("copy failed before reaching database publication: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		tag, err := client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.NoError(t, err)
		cancelAttempt()
		require.ErrorIs(t, <-done, context.Canceled)
		replacement := NewService(newStore(), testAuthorizer{}, client, nil)
		require.NoError(t, replacement.recoverOperation(ctx, op))
		ready, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, tag.Metadata.Uid, ready.TagUID)
		deleteCheckpoint(t, replacement, cp.Id)
	})

	t.Run("Given a source deleted after reservation, when tag creation fails, assert that failed checkpoint deletion releases its row", func(t *testing.T) {
		store, source, head := newSource(t)
		service := NewService(store, testAuthorizer{}, client, nil)
		cp, _, err := service.beginCreate(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		actor, err := client.GetActor(ctx, config.Atespace, substrate.ActorName(source.Id))
		require.NoError(t, err)
		require.NoError(t, client.DeleteActor(ctx, config.Atespace, substrate.ActorName(source.Id), actor.GetMetadata().GetUid()))
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.NoError(t, service.recoverOperation(ctx, op))
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED)
		_, err = store.BeginSessionOperation(ctx, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.NoError(t, err, "failed creation must stop blocking the source")
		deleteCheckpoint(t, NewService(newStore(), testAuthorizer{}, client, nil), cp.Id)
	})

	t.Run("Given failed compensation, when recovery restarts, assert that cleanup precedes reopening the source", func(t *testing.T) {
		store, source, head := newSource(t)
		fault := &faultedSubstrateTags{Client: client, failDelete: true, afterCreate: func() error {
			_, err := client.ResumeActor(ctx, config.Atespace, substrate.ActorName(source.Id))
			return err
		}}
		service := NewService(store, testAuthorizer{}, fault, nil)
		cp, _, err := service.beginCreate(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Error(t, service.recoverOperation(ctx, op))
		pending, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.NotNil(t, pending.Checkpoint.Failure)
		require.NotEmpty(t, pending.TagUID)
		_, err = store.BeginSessionOperation(ctx, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.ErrorIs(t, err, database.ErrConflict)
		replacement := NewService(newStore(), testAuthorizer{}, client, nil)
		require.NoError(t, replacement.recoverOperation(ctx, op), "a stale discovery must resume persisted compensation")
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED)
		_, err = client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.Equal(t, codes.NotFound, status.Code(err))
		_, err = store.BeginSessionOperation(ctx, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.NoError(t, err)
		deleteCheckpoint(t, replacement, cp.Id)
	})

	t.Run("Given a partially copied tag and denied object cleanup, when recovery restarts after storage repair, assert that cleanup precedes releasing source references", func(t *testing.T) {
		objects, bucket := checkpointObjectStore(t, config.ObjectStoreEndpoint, config.ObjectStoreAccessKey, config.ObjectStoreSecretKey)
		privateTemplate := proto.Clone(template).(*ateapipb.ActorTemplate)
		privateTemplate.Metadata = &ateapipb.ResourceMetadata{Atespace: config.Atespace, Name: "checkpoint-partial-" + uuid.NewString()}
		privateTemplate.Status = nil
		privateTemplate.SnapshotConfig.StorageLocation = "gs://" + bucket
		privateTemplate, err := client.CreateActorTemplate(ctx, privateTemplate)
		require.NoError(t, err)
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			require.NoError(t, client.DeleteActorTemplate(cleanup, config.Atespace, privateTemplate.Metadata.Name, privateTemplate.Metadata.Uid))
		})
		store, source, head := newSourceForTemplate(t, privateTemplate)
		actor, err := client.GetActor(ctx, config.Atespace, substrate.ActorName(source.Id))
		require.NoError(t, err)
		snapshot, err := url.Parse(substrate.SnapshotURI(substrate.LatestActorSnapshot(actor)))
		require.NoError(t, err)
		require.Equal(t, bucket, snapshot.Host)
		prefix := strings.Trim(snapshot.Path, "/") + "/"
		// Substrate copies in sorted order with eight workers. More than eight
		// preceding objects make the final denied read occur after real copies.
		for i := range 64 {
			_, err := objects.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(fmt.Sprintf("%szz-copy-probe/%04d", prefix, i)), Body: strings.NewReader("partial-copy-proof")})
			require.NoError(t, err)
		}
		deniedKey := prefix + "zz-copy-probe/9999-denied"
		_, err = objects.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(deniedKey), Body: strings.NewReader("deny this read")})
		require.NoError(t, err)
		policy, err := json.Marshal(map[string]any{
			"Version": "2012-10-17", "Statement": []any{
				map[string]any{"Effect": "Deny", "Principal": "*", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::" + bucket + "/" + deniedKey},
				map[string]any{"Effect": "Deny", "Principal": "*", "Action": "s3:DeleteObject", "Resource": "arn:aws:s3:::" + bucket + "/atespaces/" + config.Atespace + "/tags/*"},
			},
		})
		require.NoError(t, err)
		service := NewService(store, testAuthorizer{}, client, nil)
		cp, _, err := service.beginCreate(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		_, err = objects.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(bucket), Policy: aws.String(string(policy))})
		require.NoError(t, err)
		// Remove the denial before any Actor/tag/template cleanup, even on failure.
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			_, err := objects.DeleteBucketPolicy(cleanup, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
			require.NoError(t, err)
		})
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		copyErr := service.recoverOperation(ctx, op)
		require.Error(t, copyErr, "the actual object read must fail")
		t.Logf("expected object-store copy failure: %v", copyErr)
		tag, err := client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.NoError(t, err)
		require.Empty(t, substrate.SnapshotURI(tag.GetStatus().GetSnapshot()))
		tagPrefix := "atespaces/" + config.Atespace + "/tags/" + tag.Metadata.Uid + "/"
		require.NotEmpty(t, checkpointObjectKeys(t, ctx, objects, bucket, tagPrefix), "copy failure must leave actual destination objects")
		replacement := NewService(newStore(), testAuthorizer{}, client, nil)
		cleanupErr := replacement.recoverOperation(ctx, op)
		require.Error(t, cleanupErr, "the real object deletion must also fail")
		t.Logf("expected object-store cleanup failure: %v", cleanupErr)
		pending, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, pending.Checkpoint.State)
		require.NotNil(t, pending.Checkpoint.Failure)
		require.Equal(t, tag.Metadata.Uid, pending.TagUID)
		_, err = store.BeginSessionOperation(ctx, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.ErrorIs(t, err, database.ErrConflict, "partial tag cleanup must retain source admission")
		_, err = objects.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
		require.NoError(t, err)
		replacement = NewService(newStore(), testAuthorizer{}, client, nil)
		require.NoError(t, replacement.recoverOperation(ctx, pending))
		waitCheckpointPhase(t, store, cp.Id, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED)
		require.Empty(t, checkpointObjectKeys(t, ctx, objects, bucket, tagPrefix), "all partially copied objects must be collected")
		_, err = store.BeginSessionOperation(ctx, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		require.NoError(t, err, "storage repair and cleanup must reopen the source")
		deleteCheckpoint(t, replacement, cp.Id)
	})

	t.Run("Given a lost delete response, when recovery restarts, assert that an absent real tag completes deletion", func(t *testing.T) {
		store, source, head := newSource(t)
		service := NewService(store, testAuthorizer{}, client, nil)
		stop := startRecovery(t, service)
		cp, err := service.Create(owner, source.Id, uuid.NewString(), head)
		require.NoError(t, err)
		cleanupTag(t, cp.Id)
		stop()
		_, err = service.beginDelete(owner, cp.Id)
		require.NoError(t, err)
		op, err := store.GetCheckpointOperation(ctx, cp.Id, "alice")
		require.NoError(t, err)
		lost := NewService(store, testAuthorizer{}, &faultedSubstrateTags{Client: client, loseDeleteResponse: true}, nil)
		require.Error(t, lost.recoverOperation(ctx, op))
		_, err = client.GetTag(ctx, config.Atespace, tagName(cp.Id))
		require.Equal(t, codes.NotFound, status.Code(err))
		deleteCheckpoint(t, NewService(newStore(), testAuthorizer{}, client, nil), cp.Id)
	})
}

type faultedSubstrateTags struct {
	*substrate.Client
	loseCreateResponse bool
	loseDeleteResponse bool
	failDelete         bool
	afterCreate        func() error
}

func (f *faultedSubstrateTags) CreateTag(ctx context.Context, space, name, actor string) (*ateapipb.Tag, error) {
	tag, err := f.Client.CreateTag(ctx, space, name, actor)
	if err != nil {
		return nil, err
	}
	if f.afterCreate != nil {
		if err := f.afterCreate(); err != nil {
			return nil, err
		}
	}
	if f.loseCreateResponse {
		return nil, status.Error(codes.Unavailable, "injected loss of successful CreateTag response")
	}
	return tag, nil
}

func (f *faultedSubstrateTags) DeleteTag(ctx context.Context, space, name, uid string) error {
	if f.failDelete {
		return status.Error(codes.Unavailable, "injected failure to deliver DeleteTag")
	}
	if err := f.Client.DeleteTag(ctx, space, name, uid); err != nil {
		return err
	}
	if f.loseDeleteResponse {
		return status.Error(codes.Unavailable, "injected loss of successful DeleteTag response")
	}
	return nil
}

var _ tagClient = (*faultedSubstrateTags)(nil)
