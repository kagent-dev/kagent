package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestServerGetVersionAndHealth(t *testing.T) {
	oldVersion, oldCommit, oldDate := version.Version, version.GitCommit, version.BuildDate
	version.Version, version.GitCommit, version.BuildDate = "v1.2.3", "abc123", "2026-07-28"
	t.Cleanup(func() {
		version.Version, version.GitCommit, version.BuildDate = oldVersion, oldCommit, oldDate
	})

	listener := bufconn.Listen(1024 * 1024)
	server, err := New(Config{
		Listener:   listener,
		Registerer: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		cancel()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	response, err := apiv1alpha1.NewSystemServiceClient(connection).GetVersion(t.Context(), &apiv1alpha1.GetVersionRequest{})
	if err != nil {
		cancel()
		t.Fatalf("GetVersion() error = %v", err)
	}
	if response.GetKagentVersion() != "v1.2.3" || response.GetGitCommit() != "abc123" || response.GetBuildDate() != "2026-07-28" {
		t.Fatalf("GetVersion() = %+v", response)
	}

	healthResponse, err := grpc_health_v1.NewHealthClient(connection).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		cancel()
		t.Fatalf("Health.Check() error = %v", err)
	}
	if healthResponse.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Health.Check() status = %v", healthResponse.GetStatus())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gRPC server did not stop after context cancellation")
	}
}

func TestNewRejectsPartialTLSConfiguration(t *testing.T) {
	_, err := New(Config{TLSCertFile: "cert.pem"})
	if err == nil {
		t.Fatal("New() error = nil, want partial TLS configuration error")
	}
}
