// Package bootstrap loads immutable configuration before a runtime becomes ready.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contract "github.com/kagent-dev/kagent/go/api/bootstrap"
	"github.com/kagent-dev/kagent/go/api/client"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
)

// FromEnvironment reads only the controller descriptor and SDK trust interface.
func FromEnvironment(ctx context.Context, getenv func(string) string) (*runtimev1.BootstrapBundle, error) {
	return Load(ctx, getenv(contract.Environment), contract.Directory, getenv("SSL_CERT_FILE"))
}

// Load uses a validated local bundle or fetches and atomically commits it. The
// directory is caller-owned durable storage; it must not be shared with untrusted writers.
func Load(ctx context.Context, rawDescriptor, directory, caFile string) (*runtimev1.BootstrapBundle, error) {
	var descriptor contract.Descriptor
	if err := json.Unmarshal([]byte(rawDescriptor), &descriptor); err != nil {
		return nil, fmt.Errorf("decode bootstrap descriptor: %w", err)
	}
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(directory); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid bootstrap directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	path := filepath.Join(directory, "bundle.pb")
	bundle, err := readBundle(path)
	if err == nil {
		if err := descriptor.Verify(bundle); err != nil {
			return nil, err
		}
		return bundle, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read durable bootstrap bundle: %w", err)
	}
	// Leave headroom within the 30-second wakeup probe. If UID binding stays
	// unavailable, fail startup; the controller golden retry recreates the template.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	options := []client.ClientOption{client.WithGRPCMaxMessageSize(contract.MaxBundleBytes)}
	if strings.HasPrefix(descriptor.ControlPlaneURL, "https://") {
		options = append(options, client.WithGRPCTLS(client.GRPCTLSConfig{CAFile: caFile}))
	}
	api, err := client.NewAPI(descriptor.ControlPlaneURL, options...)
	if err != nil {
		return nil, err
	}
	defer api.Close()
	conn, err := api.Conn()
	if err != nil {
		return nil, err
	}
	stub := runtimev1.NewBootstrapServiceClient(conn)
	// Substrate replaces existing headers; without this placeholder the gateway
	// forwards the request without minting its actor JWT.
	values, _ := metadata.FromOutgoingContext(ctx)
	values = values.Copy()
	values.Set("authorization", "Bearer placeholder")
	ctx = metadata.NewOutgoingContext(ctx, values)
	for {
		attempt, stop := context.WithTimeout(ctx, 3*time.Second)
		response, err := stub.GetBootstrapConfig(attempt, &runtimev1.BootstrapServiceGetBootstrapConfigRequest{})
		stop()
		if err == nil {
			bundle = response.GetBundle()
			break
		}
		if status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded {
			return nil, fmt.Errorf("fetch bootstrap configuration: %w", err)
		}
		timer := time.NewTimer(time.Duration(100+rand.IntN(200)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("bootstrap initialization deadline: %w", ctx.Err())
		case <-timer.C:
		}
	}
	if err := descriptor.Verify(bundle); err != nil {
		return nil, err
	}
	if err := saveBundle(path, bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

func readBundle(path string) (*runtimev1.BootstrapBundle, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > contract.MaxBundleBytes {
		return nil, fmt.Errorf("invalid durable bootstrap file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, contract.MaxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	b := &runtimev1.BootstrapBundle{}
	if err := proto.Unmarshal(raw, b); err != nil {
		return nil, err
	}
	return b, nil
}

func saveBundle(path string, bundle *runtimev1.BootstrapBundle) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create bootstrap directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid bootstrap directory")
	}
	raw, err := proto.Marshal(bundle)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".bundle-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
