// Package managedagents launches stock Anthropic workers through kagent services.
package managedagents

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/cenkalti/backoff/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	sandboxLifetime  = time.Hour
	provisionTimeout = 2 * time.Minute
	startTimeout     = 10 * time.Second
	shutdownTimeout  = 60 * time.Second
	forceStopTimeout = 20 * time.Second
	noHeartbeat      = "NO_HEARTBEAT"
)

// Config for the Anthropic managed agent launcher.
type Config struct {
	EnvironmentID     string
	EnvironmentKey    string
	BaseURL           string
	TemplateNamespace string
	TemplateName      string
}

func (c Config) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"environment ID", c.EnvironmentID},
		{"environment key", c.EnvironmentKey},
		{"template namespace", c.TemplateNamespace},
		{"template name", c.TemplateName},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("managed agents %s is required", field.name)
		}
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("managed agents base URL must be an HTTP(S) URL without credentials, query, or fragment")
		}
	}
	return nil
}

// SandboxService is the in-process sandbox lifecycle and guest process boundary.
type SandboxService interface {
	Create(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error)
	Resume(context.Context, string) (*apiv1.Sandbox, error)
	StartProcess(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error)
	GetProcess(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error)
	Suspend(context.Context, string) (*apiv1.Sandbox, error)
}

var _ SandboxService = (*sandbox.Service)(nil)

type Launcher struct {
	config    Config
	sandboxes SandboxService
	anthropic anthropic.Client
	log       *slog.Logger
	processMu sync.Mutex
	// Session IDs identify sandboxes before Create returns their runtime IDs.
	processes map[string]*sandboxProcesses
}

type sandboxProcesses struct {
	sandboxID  string
	processIDs map[string]string // Process ID to handed-off work ID; never credentials.
	busy       chan struct{}
	// A partial Suspend can disable guest access; retry it without GetProcess.
	suspending bool
}

func New(cfg Config, sandboxes SandboxService, log *slog.Logger) (*Launcher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if sandboxes == nil || log == nil {
		return nil, errors.New("managed agents sandbox client and logger are required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/"
	}
	anthropicClient := anthropic.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(""),
		option.WithAuthToken(cfg.EnvironmentKey),
		option.WithHeaderDel("X-Api-Key"),
		option.WithMaxRetries(0),
		option.WithRequestTimeout(10*time.Second),
	)
	return &Launcher{
		config: cfg, sandboxes: sandboxes, anthropic: anthropicClient, log: log,
		processes: make(map[string]*sandboxProcesses),
	}, nil
}

func (l *Launcher) lockSandbox(ctx context.Context, sessionID string) (*sandboxProcesses, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.processMu.Lock()
		entry := l.processes[sessionID]
		if entry == nil {
			entry = &sandboxProcesses{processIDs: make(map[string]string)}
			l.processes[sessionID] = entry
		}
		if entry.busy == nil {
			entry.busy = make(chan struct{})
			l.processMu.Unlock()
			return entry, nil
		}
		done := entry.busy
		l.processMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
			// Always re-read the map: the previous holder may have removed
			// an empty record. Waiters never operate on a detached gate.
		}
	}
}

func (l *Launcher) unlockSandbox(sessionID string, entry *sandboxProcesses) {
	l.processMu.Lock()
	defer l.processMu.Unlock()
	close(entry.busy)
	entry.busy = nil
	if len(entry.processIDs) == 0 {
		delete(l.processes, sessionID)
	}
}

func (l *Launcher) Start(ctx context.Context) error {
	originalCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	var launches sync.WaitGroup
	defer func() {
		cancel()
		launches.Wait()
		// Join launches before taking the snapshot: a successful StartProcess
		// response still owns its work even if cancellation raced the response.
		l.shutdown(context.WithoutCancel(originalCtx), shutdownTimeout, forceStopTimeout)
	}()
	launches.Go(func() { l.collectPeriodically(ctx) })
	poller := environments.NewWorkPoller(ctx, l.anthropic, environments.WorkPollerOptions{
		EnvironmentID:      l.config.EnvironmentID,
		EnvironmentKey:     l.config.EnvironmentKey,
		AutoStop:           param.NewOpt(false),
		ReclaimOlderThanMs: param.NewOpt(int64(30_000)),
		Logger:             slog.New(sdkLogHandler{Handler: l.log.Handler()}),
	})
	defer func() {
		if err := poller.Close(); err != nil {
			l.log.ErrorContext(ctx, "failed to close managed agents poller", "http_status", upstreamStatus(err))
		}
	}()
	for poller.Next() {
		work := poller.Current()
		launches.Go(func() {
			if err := l.launch(ctx, work); err != nil {
				// SDK errors may contain request/response bodies, including scoped
				// credentials. Log only their category and stable work identity.
				l.log.ErrorContext(ctx, "failed to launch managed agent", "work_id", work.ID, "http_status", upstreamStatus(err), "grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
			}
		})
	}
	if err := poller.Err(); err != nil {
		return fmt.Errorf("managed agents poller stopped (HTTP status %d)", upstreamStatus(err))
	}
	return nil
}

// SDK API errors can quote entire response bodies, including work credentials.
type sdkLogHandler struct{ slog.Handler }

var _ slog.Handler = sdkLogHandler{}

func (h sdkLogHandler) Handle(ctx context.Context, record slog.Record) error {
	safe := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		safe.AddAttrs(safeSDKAttr(attr))
		return true
	})
	return h.Handler.Handle(ctx, safe)
}

func (h sdkLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	safe := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		safe[i] = safeSDKAttr(attr)
	}
	return sdkLogHandler{Handler: h.Handler.WithAttrs(safe)}
}

func (h sdkLogHandler) WithGroup(name string) slog.Handler {
	return sdkLogHandler{Handler: h.Handler.WithGroup(name)}
}

func safeSDKAttr(attr slog.Attr) slog.Attr {
	if err, ok := attr.Value.Any().(error); ok {
		return slog.Int("http_status", upstreamStatus(err))
	}
	return attr
}

func (l *Launcher) launch(ctx context.Context, work *anthropic.BetaSelfHostedWork) error {
	attemptCtx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()
	if work.EnvironmentID != l.config.EnvironmentID || work.Data.ID == "" || work.Secret == "" || work.Data.Type != "session" {
		return errors.Join(errors.New("invalid managed agents assignment"), l.stopWork(ctx, work.ID))
	}
	lease, err := l.heartbeat(attemptCtx, work.ID, leaseState{token: noHeartbeat, ttl: 30 * time.Second})
	if err != nil {
		// A failed heartbeat may already have rotated the token server-side.
		// Leave uncertain ownership to expiry instead of force-stopping it.
		return err
	}
	halt := make(chan struct{})
	done := make(chan heartbeatResult, 1)
	go l.keepAlive(attemptCtx, cancel, work.ID, lease, halt, done)
	entry, provisionErr := l.lockSandbox(attemptCtx, work.Data.ID)
	var sandbox *apiv1.Sandbox
	if provisionErr == nil {
		defer l.unlockSandbox(work.Data.ID, entry)
		sandbox, provisionErr = l.provision(attemptCtx, work.Data.ID)
	}
	close(halt)
	result := <-done
	if result.err != nil {
		return result.err
	}
	if provisionErr != nil {
		if time.Now().Before(result.lease.expiresAt()) {
			return errors.Join(provisionErr, l.stopWork(ctx, work.ID))
		}
		return provisionErr
	}
	// Leave time for the CLI's first heartbeat; never start on an expired lease.
	remaining := time.Until(result.lease.expiresAt()) - time.Second
	if remaining <= 0 {
		return errors.New("managed agents lease expired before handoff")
	}
	startCtx, stop := context.WithTimeout(attemptCtx, min(startTimeout, remaining))
	defer stop()
	env := map[string]string{
		"ANTHROPIC_ENVIRONMENT_ID":      l.config.EnvironmentID,
		"ANTHROPIC_SESSION_ID":          work.Data.ID,
		"ANTHROPIC_WORK_ID":             work.ID,
		"ANTHROPIC_WORK_SECRET":         work.Secret,
		"ANTHROPIC_WORK_LAST_HEARTBEAT": result.lease.token,
	}
	if l.config.BaseURL != "" {
		env["ANTHROPIC_BASE_URL"] = l.config.BaseURL
	}
	process, err := l.sandboxes.StartProcess(startCtx, sandbox.GetId(), &guestpb.StartProcessRequest{
		Command: []string{"ant", "beta:worker", "run", "--workdir", "/data/workspace", "--log-format", "json"},
		Cwd:     "/data/workspace",
		Env:     env,
		Timeout: durationpb.New(sandboxLifetime),
	})
	if err != nil {
		// Prototype assumption: StartProcess returns a definitive outcome.
		// Do not retry it; ambiguous response recovery is deferred.
		if time.Now().Before(result.lease.expiresAt()) {
			err = errors.Join(err, l.stopWork(ctx, work.ID))
		}
		return fmt.Errorf("failed to start managed agent CLI: %w", err)
	}
	if process.GetProcessId() == "" || (process.GetState() == guestpb.ProcessState_PROCESS_STATE_EXITED && process.GetExitCode() != 0) {
		return errors.Join(errors.New("managed agent CLI did not start successfully"), l.stopWork(ctx, work.ID))
	}
	entry.sandboxID = sandbox.GetId()
	entry.processIDs[process.GetProcessId()] = work.ID
	entry.suspending = false
	l.log.InfoContext(ctx, "started managed agent CLI", "work_id", work.ID, "sandbox_id", sandbox.GetId(), "process_id", process.GetProcessId())
	return nil
}

// The caller must first join launches and collection.
func (l *Launcher) stopWorkers(ctx context.Context, force bool) map[string]bool {
	l.processMu.Lock()
	workIDs := make(map[string]struct{})
	for _, entry := range l.processes {
		for _, workID := range entry.processIDs {
			workIDs[workID] = struct{}{}
		}
	}
	l.processMu.Unlock()

	stopped := make(map[string]bool)
	var mu sync.Mutex
	var stops sync.WaitGroup
	for workID := range workIDs {
		stops.Go(func() {
			_, err := l.anthropic.Beta.Environments.Work.Stop(ctx, workID, anthropic.BetaEnvironmentWorkStopParams{
				EnvironmentID:                 l.config.EnvironmentID,
				BetaSelfHostedWorkStopRequest: anthropic.BetaSelfHostedWorkStopRequestParam{Force: param.NewOpt(force)},
			})
			if err == nil || upstreamStatus(err) == http.StatusConflict {
				mu.Lock()
				stopped[workID] = true
				mu.Unlock()
			} else {
				// Never log the SDK error: it may contain upstream bodies or credentials.
				category := "request_failed"
				switch {
				case errors.Is(err, context.DeadlineExceeded):
					category = "deadline_exceeded"
				case errors.Is(err, context.Canceled):
					category = "canceled"
				case upstreamStatus(err) != 0:
					category = "upstream"
				}
				l.log.ErrorContext(ctx, "failed to request managed agent stop", "work_id", workID, "force", force,
					"http_status", upstreamStatus(err), "error_category", category)
			}
		})
	}
	stops.Wait()
	return stopped
}

func (l *Launcher) stopWork(ctx context.Context, workID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_, err := l.anthropic.Beta.Environments.Work.Stop(cleanupCtx, workID, anthropic.BetaEnvironmentWorkStopParams{
		EnvironmentID:                 l.config.EnvironmentID,
		BetaSelfHostedWorkStopRequest: anthropic.BetaSelfHostedWorkStopRequestParam{Force: param.NewOpt(true)},
	})
	if err != nil {
		return fmt.Errorf("failed to stop unlaunched managed agent work: %w", err)
	}
	return nil
}

func upstreamStatus(err error) int {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

type leaseState struct {
	token   string
	ttl     time.Duration
	updated time.Time
}

func (s leaseState) expiresAt() time.Time { return s.updated.Add(s.ttl) }

type heartbeatResult struct {
	lease leaseState
	err   error
}

func (l *Launcher) heartbeat(ctx context.Context, workID string, lease leaseState) (leaseState, error) {
	beatCtx, cancel := context.WithTimeout(ctx, min(10*time.Second, lease.ttl/2))
	defer cancel()
	// Count request time against the lease conservatively.
	sent := time.Now()
	response, err := l.anthropic.Beta.Environments.Work.Heartbeat(beatCtx, workID, anthropic.BetaEnvironmentWorkHeartbeatParams{
		EnvironmentID:         l.config.EnvironmentID,
		ExpectedLastHeartbeat: param.NewOpt(lease.token),
		DesiredTTLSeconds:     param.NewOpt(int64(60)),
	})
	if err != nil {
		return lease, fmt.Errorf("failed to heartbeat managed agents work: %w", err)
	}
	if !response.LeaseExtended || response.LastHeartbeat == "" || response.TTLSeconds <= 0 ||
		response.State == anthropic.BetaSelfHostedWorkHeartbeatResponseStateStopping ||
		response.State == anthropic.BetaSelfHostedWorkHeartbeatResponseStateStopped {
		return lease, errors.New("managed agents lease is no longer active")
	}
	return leaseState{token: response.LastHeartbeat, ttl: time.Duration(response.TTLSeconds) * time.Second, updated: sent}, nil
}

func (l *Launcher) keepAlive(ctx context.Context, cancel context.CancelFunc, workID string, lease leaseState, halt <-chan struct{}, done chan<- heartbeatResult) {
	for {
		timer := time.NewTimer(max(0, time.Until(lease.updated.Add(min(30*time.Second, lease.ttl/2)))))
		select {
		case <-halt:
			timer.Stop()
			done <- heartbeatResult{lease: lease}
			return
		case <-ctx.Done():
			timer.Stop()
			done <- heartbeatResult{lease: lease}
			return
		case <-timer.C:
		}
		next, err := l.heartbeat(ctx, workID, lease)
		if err != nil {
			cancel()
			done <- heartbeatResult{lease: lease, err: err}
			return
		}
		lease = next
	}
}

func createRequest(cfg Config, sessionID string) *apiv1.CreateSandboxRequest {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.EnvironmentID+"\x00"+sessionID)))
	return &apiv1.CreateSandboxRequest{
		RequestId:       "anthropic-v1-" + hash,
		Name:            "anthropic-" + hash[:16],
		SandboxTemplate: &apiv1.ResourceReference{Namespace: cfg.TemplateNamespace, Name: cfg.TemplateName},
		Ttl:             durationpb.New(sandboxLifetime),
	}
}

func (l *Launcher) provision(ctx context.Context, sessionID string) (*apiv1.Sandbox, error) {
	request := createRequest(l.config, sessionID)
	var sandbox *apiv1.Sandbox
	retryBackoff := backoff.NewExponentialBackOff()
	// Jitter spans 0.5-1 seconds initially and stays capped at 5 seconds.
	retryBackoff.InitialInterval = 750 * time.Millisecond
	retryBackoff.RandomizationFactor = 1.0 / 3
	retryBackoff.Multiplier = 2
	retryBackoff.MaxInterval = 3750 * time.Millisecond
	return backoff.Retry(ctx, func() (*apiv1.Sandbox, error) {
		var err error
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if sandbox != nil && (sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
			sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME ||
			sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND) {
			var resumed *apiv1.Sandbox
			resumed, err = l.sandboxes.Resume(callCtx, sandbox.GetId())
			if err == nil {
				sandbox = resumed
			}
		} else {
			var created *apiv1.Sandbox
			created, err = l.sandboxes.Create(callCtx, request)
			if err == nil {
				sandbox = created
			}
		}
		cancel()
		if err != nil && !retryable(err) {
			return nil, backoff.Permanent(fmt.Errorf("failed to provision managed agents sandbox: %w", err))
		}
		if err == nil {
			if sandbox.GetId() == "" {
				return nil, backoff.Permanent(errors.New("sandbox API returned no sandbox"))
			}
			if sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_READY && sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
				return sandbox, nil
			}
			if sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE ||
				sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETED || sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETING ||
				(sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_FAILED && sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE) {
				return nil, backoff.Permanent(errors.New("managed agents sandbox is unavailable"))
			}
			return nil, errors.New("managed agents sandbox is not ready")
		}
		return nil, err
	}, backoff.WithBackOff(retryBackoff), backoff.WithMaxElapsedTime(0))
}

func retryable(err error) bool {
	// Service classifications take precedence over wrapped backend statuses.
	if code := serviceerrors.CodeOf(err); code != "" {
		switch code {
		case serviceerrors.CodeAborted, serviceerrors.CodeUnavailable, serviceerrors.CodeResourceExhausted:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
