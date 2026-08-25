// Package connection owns CLI server connectivity and Kubernetes port-forward fallback.
package connection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrServerConnection = errors.New("error connecting to server")

const (
	portForwardReadyTimeout = 15 * time.Second
	portForwardRetryDelay   = 100 * time.Millisecond
	kubectlErrorLimit       = 8 << 10
)

type connectionRuntime struct {
	checkServer    func(context.Context, *client.ClientSet) error
	commandContext func(context.Context, string, ...string) *exec.Cmd
	stderr         io.Writer
	readyTimeout   time.Duration
	retryDelay     time.Duration
}

var defaultConnectionRuntime = connectionRuntime{
	checkServer:    CheckServer,
	commandContext: exec.CommandContext,
	stderr:         os.Stderr,
	readyTimeout:   portForwardReadyTimeout,
	retryDelay:     portForwardRetryDelay,
}

// CheckServer checks whether the configured server is reachable.
func CheckServer(ctx context.Context, clientSet *client.ClientSet) error {
	if clientSet == nil {
		return ErrServerConnection
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := clientSet.Version.GetVersion(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrServerConnection, err)
	}
	return nil
}

// Connect checks the configured server and starts a port-forward only for an
// unreachable default local endpoint.
func Connect(ctx context.Context, cfg *config.Config) (*PortForward, error) {
	return defaultConnectionRuntime.connect(ctx, cfg)
}

func (r connectionRuntime) connect(ctx context.Context, cfg *config.Config) (*PortForward, error) {
	if cfg.Verbose {
		fmt.Fprintf(r.stderr, "Using caller identity %q\n", cfg.UserID)
	}

	err := r.checkConfiguredServer(ctx, cfg)
	if err == nil {
		return nil, nil
	}
	if !shouldPortForward(cfg, err) {
		return nil, err
	}
	return r.newPortForward(ctx, cfg)
}

func shouldPortForward(cfg *config.Config, err error) bool {
	grpcURL := cfg.KAgentGRPCURL
	if grpcURL == "" {
		grpcURL = config.DefaultKAgentGRPCURL
	}
	if cfg.KAgentGRPCTLS || grpcURL != config.DefaultKAgentGRPCURL || strings.TrimRight(cfg.KAgentURL, "/") != config.DefaultKAgentURL {
		return false
	}
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded)
}

func (r connectionRuntime) checkConfiguredServer(ctx context.Context, cfg *config.Config) (err error) {
	clientSet := cfg.Client()
	defer func() {
		err = errors.Join(err, clientSet.Close())
	}()
	return r.checkServer(ctx, clientSet)
}

// PortForward is a running kubectl port-forward process.
type PortForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	wait   <-chan error
	stop   sync.Once
}

// NewPortForward starts a port-forward and waits for the server to become reachable.
func NewPortForward(ctx context.Context, cfg *config.Config) (*PortForward, error) {
	return defaultConnectionRuntime.newPortForward(ctx, cfg)
}

func (r connectionRuntime) newPortForward(ctx context.Context, cfg *config.Config) (*PortForward, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := r.commandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent-controller", "8083:8083", "8084:8084")
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

	readyCtx, cancelReady := context.WithTimeout(ctx, r.readyTimeout)
	defer cancelReady()
	ticker := time.NewTicker(r.retryDelay)
	defer ticker.Stop()

	var lastErr error
	for {
		lastErr = r.checkConfiguredServer(readyCtx, cfg)
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
		cause = ErrServerConnection
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
