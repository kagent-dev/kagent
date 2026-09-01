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
	"google.golang.org/grpc/status"
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
	require.Empty(t, fake.actor.GetActorTemplateNamespace())
	require.Empty(t, fake.actor.GetActorTemplateName())
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

func TestDeleteActorTemplateAlsoDeletesGoldenActorOnRetry(t *testing.T) {
	fake := &deleteActorTemplateFake{}
	client := &Client{ControlClient: fake, cfg: Config{CallTimeout: time.Second}}
	require.NoError(t, client.DeleteActorTemplate(t.Context(), "team-a", "template", "template-uid"))
	require.Equal(t, &ateapipb.ObjectRef{Atespace: "team-a", Name: "template"}, fake.template)
	require.Equal(t, &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "template-uid"}, fake.actor.GetActor())
	require.True(t, fake.actor.GetAnyState())
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
