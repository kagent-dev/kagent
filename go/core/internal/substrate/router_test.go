package substrate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestRouterTransportMutualTLSAndCertificateRotation(t *testing.T) {
	serverCert := newTestTLSCert(t)
	clientCerts := []tls.Certificate{newTestTLSCert(t), newTestTLSCert(t)}
	clientCAs := x509.NewCertPool()
	for _, cert := range clientCerts {
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		clientCAs.AddCert(parsed)
	}
	observed := make(chan []byte, 2)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		VerifyConnection: func(state tls.ConnectionState) error {
			observed <- state.PeerCertificates[0].Raw
			return nil
		},
	})))
	healthpb.RegisterHealthServer(server, health.NewServer())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert.Certificate[0]}), 0o600))
	clientFile := filepath.Join(t.TempDir(), "client.pem")
	cfg := &configv1.SubstrateConfig{RouterUrl: new("https://" + listener.Addr().String()), CaFile: caFile, ClientCertFile: clientFile}
	target, transport, err := RouterTransport(cfg)
	require.NoError(t, err)
	for _, cert := range clientCerts {
		key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
		require.NoError(t, err)
		bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})...)
		require.NoError(t, os.WriteFile(clientFile, bundle, 0o600))
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(transport))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		cancel()
		require.NoError(t, conn.Close())
		require.NoError(t, err)
		require.Equal(t, cert.Certificate[0], <-observed, "new connections must present the rotated certificate")
	}

	for _, test := range []struct {
		name string
		cfg  *configv1.SubstrateConfig
	}{
		{name: "missing client certificate", cfg: &configv1.SubstrateConfig{RouterUrl: cfg.RouterUrl, CaFile: caFile}},
		{name: "untrusted server", cfg: &configv1.SubstrateConfig{RouterUrl: cfg.RouterUrl, ClientCertFile: clientFile}},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, transport, err := RouterTransport(test.cfg)
			require.NoError(t, err)
			conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(transport))
			require.NoError(t, err)
			defer conn.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
			require.Error(t, err)
		})
	}
}
