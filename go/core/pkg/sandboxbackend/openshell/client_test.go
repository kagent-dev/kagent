package openshell

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestDial_tcpReachesReady(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	cfg := Config{
		GatewayURL:  lis.Addr().String(),
		Insecure:    true,
		DialTimeout: 2 * time.Second,
	}
	c, err := Dial(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, c.Close())
}
