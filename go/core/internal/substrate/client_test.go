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

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestClientTLSConfig(t *testing.T) {
	cfg, err := clientTLSConfig("", "")
	require.NoError(t, err)
	require.False(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	path := writeTLSBundle(t, newTestTLSCert(t))
	cfg, err = clientTLSConfig(path, path)
	require.NoError(t, err)
	require.NotNil(t, cfg.RootCAs)
	loaded, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Certificate)
}

func TestRouterTransport(t *testing.T) {
	for _, test := range []struct {
		name         string
		router       Router
		wantTarget   string
		wantProtocol string
		wantErr      string
	}{
		{name: "http ignores TLS files", router: Router{URL: "http://router:80", CAFile: "/missing", ClientCertFile: "/missing"}, wantTarget: "router:80", wantProtocol: "insecure"},
		{name: "https", router: Router{URL: "https://router:443"}, wantTarget: "router:443", wantProtocol: "tls"},
		{name: "https with missing CA file", router: Router{URL: "https://router:443", CAFile: "/missing"}, wantErr: "read CA file"},
		{name: "missing host", router: Router{URL: "http://"}, wantErr: "must include a host"},
		{name: "unsupported scheme", router: Router{URL: "ftp://router"}, wantErr: "must use http or https"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, transport, err := test.router.Transport()
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantTarget, target)
			require.Equal(t, test.wantProtocol, transport.Info().SecurityProtocol)
		})
	}
}

// The router's sender policy relies on seeing the controller certificate.
func TestRouterTransportPresentsClientCertificate(t *testing.T) {
	cert := newTestTLSCert(t)
	bundle := writeTLSBundle(t, cert)
	pool := x509.NewCertPool()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	pool.AddCert(leaf)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	peers := make(chan []*x509.Certificate, 1)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS12,
		})),
		grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
			p, _ := peer.FromContext(stream.Context())
			peers <- p.AuthInfo.(credentials.TLSInfo).State.PeerCertificates
			return status.Error(codes.Unimplemented, "observed")
		}),
	)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	call := func(router Router) error {
		target, transport, err := router.Transport()
		require.NoError(t, err)
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(transport))
		require.NoError(t, err)
		defer conn.Close() //nolint:errcheck
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		return conn.Invoke(ctx, "/test.Router/Call", &emptypb.Empty{}, &emptypb.Empty{})
	}
	url := "https://" + lis.Addr().String()
	require.Equal(t, codes.Unavailable, status.Code(call(Router{URL: url, CAFile: bundle})), "the router requires a client certificate")
	require.Equal(t, codes.Unimplemented, status.Code(call(Router{URL: url, CAFile: bundle, ClientCertFile: bundle})))
	require.Equal(t, cert.Certificate[0], (<-peers)[0].Raw)
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
	f.pageTokens = append(f.pageTokens, in.GetPageToken())
	name := "first"
	next := "next"
	if in.GetPageToken() != "" {
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
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// writeTLSBundle writes the certificate and key to one PEM file, as pod
// certificate credential bundles are mounted.
func writeTLSBundle(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	require.NoError(t, err)
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})...)
	path := filepath.Join(t.TempDir(), "bundle.pem")
	require.NoError(t, os.WriteFile(path, bundle, 0o600))
	return path
}

// listWorkersFake pages workers the way ate-api does: one row per page, and an empty
// token on the last.
type listWorkersFake struct {
	ateapipb.ControlClient
	pageTokens []string
}

func (f *listWorkersFake) ListWorkers(_ context.Context, in *ateapipb.ListWorkersRequest, _ ...grpc.CallOption) (*ateapipb.ListWorkersResponse, error) {
	f.pageTokens = append(f.pageTokens, in.GetPageToken())
	name, next := "first", "next"
	if in.GetPageToken() != "" {
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
	if in.GetPageToken() != "" {
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
	require.Equal(t, int32(25), fake.requests[0].GetPageSize())
	require.Equal(t, "cursor", fake.requests[0].GetPageToken())
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
