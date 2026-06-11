package substrate

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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
