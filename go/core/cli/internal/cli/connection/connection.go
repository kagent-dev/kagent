// Package connection owns CLI server connectivity and Kubernetes port-forward fallback.
package connection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kagent-dev/kagent/go/api/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errServerConnection = errors.New("error connecting to server")

const (
	defaultKAgentURL     = "http://localhost:8083"
	defaultKAgentGRPCURL = client.DefaultGRPCTarget
	defaultUserID        = "admin@kagent.dev"

	portForwardReadyTimeout = 15 * time.Second
	portForwardRetryDelay   = 100 * time.Millisecond
	kubectlErrorLimit       = 8 << 10
)

// Options contains only the settings needed to connect to kagent.
type Options struct {
	KAgentURL            string
	KAgentGRPCURL        string
	KAgentGRPCTLS        bool
	KAgentGRPCCAFile     string
	KAgentGRPCServerName string
	Namespace            string
	Verbose              bool
	Timeout              time.Duration
	UserID               string
}

func DefaultOptions() Options {
	return Options{
		KAgentURL:     defaultKAgentURL,
		KAgentGRPCURL: defaultKAgentGRPCURL,
		Namespace:     "kagent",
		Timeout:       300 * time.Second,
		UserID:        defaultUserID,
	}
}

func (o *Options) Client() *client.ClientSet {
	clientOptions := []client.ClientOption{client.WithUserID(o.UserID)}
	if o.KAgentGRPCURL != "" {
		clientOptions = append(clientOptions, client.WithGRPCTarget(o.KAgentGRPCURL))
	}
	if o.Timeout > 0 {
		clientOptions = append(clientOptions, client.WithGRPCTimeout(o.Timeout))
	}
	if o.KAgentGRPCTLS {
		clientOptions = append(clientOptions, client.WithGRPCTLS(client.GRPCTLSConfig{
			CAFile:     o.KAgentGRPCCAFile,
			ServerName: o.KAgentGRPCServerName,
		}))
	}
	return client.New(o.KAgentURL, clientOptions...)
}

func (o *Options) validate() error {
	if o.UserID == "" {
		return errors.New("caller identity is required")
	}
	if strings.IndexFunc(o.UserID, unicode.IsSpace) >= 0 {
		return errors.New("caller identity must not contain whitespace")
	}
	return nil
}

func checkServer(ctx context.Context, clientSet *client.ClientSet) error {
	if clientSet == nil {
		return errServerConnection
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := clientSet.Version.GetVersion(ctx); err != nil {
		return fmt.Errorf("%w: %w", errServerConnection, err)
	}
	return nil
}

// Connect checks the configured server and starts a port-forward only for an
// unreachable default local endpoint.
func Connect(ctx context.Context, cfg *Options) (*PortForward, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "Using caller identity %q\n", cfg.UserID)
	}

	err := checkConfiguredServer(ctx, cfg)
	if err == nil {
		return nil, nil
	}
	if !shouldPortForward(cfg, err) {
		return nil, err
	}
	return NewPortForward(ctx, cfg)
}

func shouldPortForward(cfg *Options, err error) bool {
	grpcURL := cfg.KAgentGRPCURL
	if grpcURL == "" {
		grpcURL = defaultKAgentGRPCURL
	}
	if cfg.KAgentGRPCTLS || grpcURL != defaultKAgentGRPCURL || strings.TrimRight(cfg.KAgentURL, "/") != defaultKAgentURL {
		return false
	}
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded)
}

func checkConfiguredServer(ctx context.Context, cfg *Options) (err error) {
	clientSet := cfg.Client()
	defer func() {
		err = errors.Join(err, clientSet.Close())
	}()
	return checkServer(ctx, clientSet)
}

// PortForward is a running kubectl port-forward process.
type PortForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	wait   <-chan error
	stop   sync.Once
}

// NewPortForward starts a port-forward and waits for the server to become reachable.
func NewPortForward(ctx context.Context, cfg *Options) (*PortForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent-controller", "8083:8083", "8084:8084")
	stderr := newBoundedBuffer(kubectlErrorLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start kubectl port-forward: %w", err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- cmd.Wait()
		close(wait)
	}()
	portForward := &PortForward{cmd: cmd, cancel: cancel, wait: wait}

	readyCtx, cancelReady := context.WithTimeout(ctx, portForwardReadyTimeout)
	defer cancelReady()
	ticker := time.NewTicker(portForwardRetryDelay)
	defer ticker.Stop()

	var lastErr error
	for {
		lastErr = checkConfiguredServer(readyCtx, cfg)
		if lastErr == nil {
			return portForward, nil
		}

		select {
		case processErr := <-wait:
			cancel()
			return nil, portForwardExitedError(processErr, lastErr, stderr.String())
		case <-readyCtx.Done():
			portForward.Stop()
			return nil, portForwardReadinessError(readyCtx.Err(), lastErr, stderr.String())
		case <-ticker.C:
		}
	}
}

func portForwardExitedError(processErr, serverErr error, stderr string) error {
	cause := errors.Join(processErr, serverErr)
	if cause == nil {
		cause = errServerConnection
	}
	return fmt.Errorf("kubectl port-forward exited before the server became ready%s: %w", kubectlDetails(stderr), cause)
}

func portForwardReadinessError(deadlineErr, serverErr error, stderr string) error {
	return fmt.Errorf("failed to establish connection to kagent-controller%s: %w", kubectlDetails(stderr), errors.Join(deadlineErr, serverErr))
}

func kubectlDetails(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return fmt.Sprintf(" (kubectl: %s)", stderr)
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	remaining int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{remaining: limit}
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	if len(data) > b.remaining {
		data = data[:b.remaining]
	}
	if _, err := b.buffer.Write(data); err != nil {
		return 0, err
	}
	b.remaining -= len(data)
	return written, nil
}

func (b *boundedBuffer) String() string {
	return b.buffer.String()
}

// Stop terminates the port-forward process and waits for it to be reaped.
func (p *PortForward) Stop() {
	if p == nil {
		return
	}
	p.stop.Do(func() {
		p.cancel()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.wait
	})
}
