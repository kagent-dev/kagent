package substrate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
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
	cfg := ateAPITLSConfig(false)
	require.False(t, cfg.InsecureSkipVerify)

	cfg = ateAPITLSConfig(true)
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestDial_tlsSkipVerifyReachesReady(t *testing.T) {
	cert := newTestTLSCert(t)

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
		Insecure:       true,
		DialTimeout:    2 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, c.Close())
}

func TestBearerTokenFile(t *testing.T) {
	t.Run("reads and trims token", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(path, []byte(" test-token\n"), 0o600))

		creds := bearerTokenFile{path: path, requireTLS: true}
		md, err := creds.GetRequestMetadata(context.Background())
		require.NoError(t, err)
		require.Equal(t, "Bearer test-token", md["authorization"])
		require.True(t, creds.RequireTransportSecurity())
	})

	t.Run("allows insecure transport when configured", func(t *testing.T) {
		creds := bearerTokenFile{requireTLS: false}
		require.False(t, creds.RequireTransportSecurity())
	})

	t.Run("rejects empty token", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(path, []byte(" \n"), 0o600))

		_, err := bearerTokenFile{path: path}.GetRequestMetadata(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "is empty")
	})

	t.Run("wraps read errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")

		_, err := bearerTokenFile{path: path}.GetRequestMetadata(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "read bearer token file")
	})
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

func (f *createAtespaceFake) CreateAtespace(_ context.Context, in *ateapipb.CreateAtespaceRequest, _ ...grpc.CallOption) (*ateapipb.CreateAtespaceResponse, error) {
	f.lastName = in.GetName()
	if f.err != nil {
		return nil, f.err
	}
	return &ateapipb.CreateAtespaceResponse{Atespace: &ateapipb.Atespace{Name: in.GetName()}}, nil
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
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
