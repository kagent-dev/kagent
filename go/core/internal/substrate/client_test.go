package substrate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestAteAPITLSConfig(t *testing.T) {
	cfg, err := ateAPITLSConfig(Config{})
	require.NoError(t, err)
	require.False(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	cert := newTestTLSCert(t)
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	require.NoError(t, err)
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})...)
	path := filepath.Join(t.TempDir(), "bundle.pem")
	require.NoError(t, os.WriteFile(path, bundle, 0o600))
	cfg, err = ateAPITLSConfig(Config{CAFile: path, ClientCertFile: path})
	require.NoError(t, err)
	require.NotNil(t, cfg.RootCAs)
	loaded, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Certificate)
}

func TestDial_verifiedTLSReachesReady(t *testing.T) {
	cert := newTestTLSCert(t)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0o600))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	c, err := Dial(context.Background(), Config{
		AteAPIEndpoint: lis.Addr().String(),
		CAFile:         caFile,
		DialTimeout:    2 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, c.Close())
}

func TestEnsureAtespace(t *testing.T) {
	t.Run("returns nil when substrate reports AlreadyExists", func(t *testing.T) {
		fake := &createAtespaceFake{err: status.Error(codes.AlreadyExists, "Atespace kagent already exists")}
		c := &Client{ControlClient: fake}

		require.NoError(t, c.EnsureAtespace(context.Background(), "kagent"))
		require.Equal(t, "kagent", fake.lastName)
	})

	t.Run("returns nil on successful create", func(t *testing.T) {
		fake := &createAtespaceFake{}
		c := &Client{ControlClient: fake}

		require.NoError(t, c.EnsureAtespace(context.Background(), "kagent"))
	})

	t.Run("propagates non-AlreadyExists errors", func(t *testing.T) {
		fake := &createAtespaceFake{err: status.Error(codes.Internal, "boom")}
		c := &Client{ControlClient: fake}

		err := c.EnsureAtespace(context.Background(), "kagent")
		require.Error(t, err)
		require.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("propagates non-gRPC errors", func(t *testing.T) {
		fake := &createAtespaceFake{err: errors.New("dial failed")}
		c := &Client{ControlClient: fake}

		err := c.EnsureAtespace(context.Background(), "kagent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "dial failed")
	})
}

// createAtespaceFake is a partial ControlClient stand-in that captures the last
// CreateAtespace request and returns a preset error. All other methods panic.
type createAtespaceFake struct {
	ateapipb.ControlClient
	lastName string
	err      error
}

type createActorFake struct {
	ateapipb.ControlClient
	actor *ateapipb.Actor
}

type listActorTemplatesFake struct {
	ateapipb.ControlClient
	pageTokens []string
}

type deleteActorTemplateFake struct {
	ateapipb.ControlClient
	template *ateapipb.ObjectRef
	actor    *ateapipb.DeleteActorRequest
}

func (f *createActorFake) CreateActor(_ context.Context, in *ateapipb.CreateActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	f.actor = in.GetActor()
	return f.actor, nil
}

func TestCreateActorUsesStableTemplateRef(t *testing.T) {
	fake := &createActorFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
	_, err := client.CreateActor(t.Context(), "team-a", "actor", "team-a", "template")
	require.NoError(t, err)
	require.Equal(t, &ateapipb.ObjectRef{Atespace: "team-a", Name: "template"}, fake.actor.GetActorTemplate())
}

func (f *listActorTemplatesFake) ListActorTemplates(_ context.Context, in *ateapipb.ListActorTemplatesRequest, _ ...grpc.CallOption) (*ateapipb.ListActorTemplatesResponse, error) {
	f.pageTokens = append(f.pageTokens, in.GetOptions().GetPageToken())
	name := "first"
	next := "next"
	if in.GetOptions().GetPageToken() != "" {
		name = "second"
		next = ""
	}
	return &ateapipb.ListActorTemplatesResponse{
		ActorTemplates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Atespace: in.GetAtespace(), Name: name}}},
		NextPageToken:  next,
	}, nil
}

func TestListActorTemplatesFollowsPagination(t *testing.T) {
	fake := &listActorTemplatesFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
	templates, err := client.ListActorTemplates(t.Context(), "team-a")
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, []string{templates[0].GetMetadata().GetName(), templates[1].GetMetadata().GetName()})
	require.Equal(t, []string{"", "next"}, fake.pageTokens)
}

func (f *deleteActorTemplateFake) DeleteActorTemplate(_ context.Context, in *ateapipb.DeleteActorTemplateRequest, _ ...grpc.CallOption) (*ateapipb.ActorTemplate, error) {
	f.template = in.GetActorTemplate()
	return nil, status.Error(codes.NotFound, "already deleted")
}

func (f *deleteActorTemplateFake) DeleteActor(_ context.Context, in *ateapipb.DeleteActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	f.actor = in
	return &ateapipb.Actor{}, nil
}

func TestDeleteActorTemplateRetriesNotFound(t *testing.T) {
	fake := &deleteActorTemplateFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
	require.NoError(t, client.DeleteActorTemplate(t.Context(), "team-a", "template"))
	require.Equal(t, &ateapipb.ObjectRef{Atespace: "team-a", Name: "template"}, fake.template)
	require.Nil(t, fake.actor, "Substrate owns golden Actor cleanup")
}

func (f *createAtespaceFake) CreateAtespace(_ context.Context, in *ateapipb.CreateAtespaceRequest, _ ...grpc.CallOption) (*ateapipb.Atespace, error) {
	f.lastName = in.GetAtespace().GetMetadata().GetName()
	if f.err != nil {
		return nil, f.err
	}
	return &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: f.lastName}}, nil
}

func newTestTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// listWorkersFake pages workers the way ate-api does: one row per page, and an empty
// token on the last.
type listWorkersFake struct {
	ateapipb.ControlClient
	pageTokens []string
}

func (f *listWorkersFake) ListWorkers(_ context.Context, in *ateapipb.ListWorkersRequest, _ ...grpc.CallOption) (*ateapipb.ListWorkersResponse, error) {
	f.pageTokens = append(f.pageTokens, in.GetOptions().GetPageToken())
	name, next := "first", "next"
	if in.GetOptions().GetPageToken() != "" {
		name = "second"
		next = ""
	}
	return &ateapipb.ListWorkersResponse{
		Workers:       []*ateapipb.Worker{{WorkerPod: name}},
		NextPageToken: next,
	}, nil
}

func TestListWorkersPageReturnsOnePageAndItsToken(t *testing.T) {
	fake := &listWorkersFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}

	workers, next, err := client.ListWorkersPage(t.Context(), 25, "")
	require.NoError(t, err)
	require.Len(t, workers, 1)
	require.Equal(t, "next", next)
	require.Equal(t, []string{""}, fake.pageTokens)
}

// listActorsFake records what each page was asked for, so a caller that drops the page
// size or re-reads page one is visible.
type listActorsFake struct {
	ateapipb.ControlClient
	requests []*ateapipb.ListActorsRequest
}

func (f *listActorsFake) ListActors(_ context.Context, in *ateapipb.ListActorsRequest, _ ...grpc.CallOption) (*ateapipb.ListActorsResponse, error) {
	f.requests = append(f.requests, in)
	name, next := "first", "next"
	if in.GetOptions().GetPageToken() != "" {
		name = "second"
		next = ""
	}
	return &ateapipb.ListActorsResponse{
		Actors:        []*ateapipb.Actor{{Metadata: &ateapipb.ResourceMetadata{Name: name}}},
		NextPageToken: next,
	}, nil
}

func TestListActorsPagePassesPageSizeAndTokenThrough(t *testing.T) {
	fake := &listActorsFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}

	actors, next, err := client.ListActorsPage(t.Context(), "team-a", 25, "cursor")
	require.NoError(t, err)
	require.Equal(t, "second", actors[0].GetMetadata().GetName())
	require.Empty(t, next)
	require.Len(t, fake.requests, 1)
	require.Equal(t, "team-a", fake.requests[0].GetAtespace())
	require.Equal(t, int32(25), fake.requests[0].GetOptions().GetPageSize())
	require.Equal(t, "cursor", fake.requests[0].GetOptions().GetPageToken())
}

// stuckPageFake answers every request with the token it was given, which is a server
// that is not advancing.
type stuckPageFake struct {
	ateapipb.ControlClient
	reads int
}

func (f *stuckPageFake) ListActorTemplates(_ context.Context, in *ateapipb.ListActorTemplatesRequest, _ ...grpc.CallOption) (*ateapipb.ListActorTemplatesResponse, error) {
	f.reads++
	return &ateapipb.ListActorTemplatesResponse{
		ActorTemplates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Name: "template"}}},
		NextPageToken:  "stuck",
	}, nil
}

// A drain that follows a repeated token re-reads the same page until whatever cap sits
// above it, spending a request per attempt on a backend that is already misbehaving.
func TestListActorTemplatesRefusesAPageTokenThatDoesNotAdvance(t *testing.T) {
	fake := &stuckPageFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}

	_, err := client.ListActorTemplates(t.Context(), "team-a")
	require.ErrorContains(t, err, "repeated page token")
	// Caught on the second read, where the reason is still obvious — not after
	// maxDrainPages of them.
	require.Equal(t, 2, fake.reads)
}

func TestAdvancePageTokenAllowsTheLastPageAndARealMove(t *testing.T) {
	next, err := AdvancePageToken("first", "second")
	require.NoError(t, err)
	require.Equal(t, "second", next)

	// An empty token is the last page, not a repeat, even from an empty one.
	next, err = AdvancePageToken("", "")
	require.NoError(t, err)
	require.Empty(t, next)
}

type deleteTagFake struct {
	ateapipb.ControlClient
	request *ateapipb.DeleteTagRequest
	err     error
}

func (f *deleteTagFake) DeleteTag(_ context.Context, request *ateapipb.DeleteTagRequest, _ ...grpc.CallOption) (*ateapipb.Tag, error) {
	f.request = request
	return nil, f.err
}

func TestDeleteTagPassesUIDPrecondition(t *testing.T) {
	for _, uid := range []string{"retained-uid", ""} {
		t.Run(uid, func(t *testing.T) {
			fake := &deleteTagFake{err: status.Error(codes.Aborted, "tag replaced")}
			client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
			require.ErrorIs(t, client.DeleteTag(t.Context(), "team-a", "checkpoint", uid), fake.err)
			require.Equal(t, "team-a", fake.request.GetTag().GetAtespace())
			require.Equal(t, "checkpoint", fake.request.GetTag().GetName())
			require.Equal(t, uid, fake.request.GetOptions().GetUid())
		})
	}
}

type suspendActorFake struct {
	ateapipb.ControlClient
	actor   *ateapipb.Actor
	request *ateapipb.SuspendActorRequest
	get     func(context.Context) (*ateapipb.Actor, error)
}

func (f *suspendActorFake) SuspendActor(_ context.Context, request *ateapipb.SuspendActorRequest, _ ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	f.request = request
	return &ateapipb.SuspendActorResponse{Actor: f.actor}, nil
}

func (f *suspendActorFake) GetActor(ctx context.Context, _ *ateapipb.GetActorRequest, _ ...grpc.CallOption) (*ateapipb.Actor, error) {
	return f.get(ctx)
}

func TestSuspendActorWaitsForLatestDurableSnapshot(t *testing.T) {
	completed := &ateapipb.Snapshot{Generation: 1, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/old"}}}
	pending := &ateapipb.Snapshot{Generation: 2, DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING}}
	actor := &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Uid: "original"}, Status: &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, Snapshots: []*ateapipb.Snapshot{pending, completed}}}
	for _, test := range []struct {
		name      string
		change    func(*ateapipb.Actor)
		wantError string
	}{
		{name: "upload completed", change: func(a *ateapipb.Actor) {
			a.Status.Snapshots[0].DurableSnapshot = &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/new"}}
		}},
		{name: "actor replaced", change: func(a *ateapipb.Actor) { a.Metadata.Uid = "replacement" }, wantError: "changed"},
		{name: "actor crashed", change: func(a *ateapipb.Actor) { a.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED }, wantError: "changed"},
		{name: "generation changed", change: func(a *ateapipb.Actor) { a.Status.Snapshots[0].Generation++ }, wantError: "changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			next := proto.CloneOf(actor)
			test.change(next)
			reads := 0
			fake := &suspendActorFake{actor: actor, get: func(context.Context) (*ateapipb.Actor, error) { reads++; return next, nil }}
			client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
			got, err := client.SuspendActor(t.Context(), "team", "actor")
			require.Equal(t, ateapipb.DurabilityMode_DURABILITY_MODE_SYNCHRONOUS, fake.request.GetOptions().GetMode())
			require.Equal(t, 1, reads)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "s3://snapshots/new", SnapshotURI(LatestActorSnapshot(got)))
		})
	}
	t.Run("cancel pending upload", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		fake := &suspendActorFake{actor: actor, get: func(context.Context) (*ateapipb.Actor, error) { cancel(); return actor, nil }}
		client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
		_, err := client.SuspendActor(ctx, "team", "actor")
		cancel()
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("already durable", func(t *testing.T) {
		durable := proto.CloneOf(actor)
		durable.Status.Snapshots = []*ateapipb.Snapshot{completed}
		client := &Client{ControlClient: &suspendActorFake{actor: durable}}
		got, err := client.SuspendActor(t.Context(), "team", "actor")
		require.NoError(t, err)
		require.Same(t, durable, got)
	})
}
