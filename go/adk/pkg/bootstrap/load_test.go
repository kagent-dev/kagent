package bootstrap

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	contract "github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

type server struct {
	runtimev1.UnimplementedBootstrapServiceServer
	bundle  *runtimev1.BootstrapBundle
	pending atomic.Int32
}

func (s *server) GetBootstrapConfig(ctx context.Context, _ *runtimev1.BootstrapServiceGetBootstrapConfigRequest) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get("authorization"); len(values) != 1 || values[0] != "Bearer placeholder" {
		return nil, status.Error(codes.Unauthenticated, "gateway credential placeholder is required")
	}
	if s.pending.Add(-1) >= 0 {
		return nil, status.Error(codes.Unavailable, "template binding pending")
	}
	return &runtimev1.BootstrapServiceGetBootstrapConfigResponse{Bundle: s.bundle}, nil
}

// Given a pending template binding, when startup retries and then restores offline,
// assert that both invocations load the pinned bundle.
func TestFetchThenRestoreWithoutServer(t *testing.T) {
	bundle, err := contract.NewBundle(strings.Repeat("a", 64), []byte(`{"instruction":"`+strings.Repeat("x", 40000)+`"}`), []byte(`{"name":"agent"}`))
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	svc := grpc.NewServer()
	backend := &server{bundle: bundle}
	backend.pending.Store(2)
	runtimev1.RegisterBootstrapServiceServer(svc, backend)
	go func() { _ = svc.Serve(listener) }()
	t.Cleanup(svc.Stop)
	descriptor := contract.Descriptor{FormatVersion: 1, ControlPlaneURL: "http://" + listener.Addr().String(), Revision: bundle.Revision, SHA256: bundle.Sha256}
	raw, err := json.Marshal(descriptor)
	require.NoError(t, err)
	directory := filepath.Join(t.TempDir(), "bootstrap")
	loaded, err := Load(t.Context(), string(raw), directory, "")
	require.NoError(t, err)
	require.Equal(t, bundle.ConfigJson, loaded.ConfigJson)
	svc.Stop()
	loaded, err = Load(t.Context(), string(raw), directory, "")
	require.NoError(t, err)
	require.Equal(t, bundle.Sha256, loaded.Sha256)
	info, err := os.Stat(filepath.Join(directory, "bundle.pb"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoError(t, os.WriteFile(filepath.Join(directory, "bundle.pb"), []byte("corrupt"), 0o600))
	_, err = Load(t.Context(), string(raw), directory, "")
	require.Error(t, err)
}
