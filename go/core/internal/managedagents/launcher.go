// Package managedagents launches stock Anthropic workers through kagent services.
package managedagents

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	sandboxLifetime  = 12 * time.Hour
	provisionTimeout = 2 * time.Minute
	startTimeout     = 10 * time.Second
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
	Get(context.Context, string) (*apiv1.Sandbox, error)
	List(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error)
	Create(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error)
	Resume(context.Context, string) (*apiv1.Sandbox, error)
	StartProcess(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error)
	GetProcess(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error)
	StreamProcessOutput(context.Context, string, *guestpb.StreamProcessOutputRequest, func(*guestpb.ProcessOutput) error) error
	Suspend(context.Context, string) (*apiv1.Sandbox, error)
}

var _ SandboxService = (*sandbox.Service)(nil)

type Launcher struct {
	config    Config
	sandboxes SandboxService
	anthropic anthropic.Client
	log       *slog.Logger
	processMu sync.Mutex
	// Keep sandbox associations even when a session has no running worker.
	processes map[string]*sandboxWorker
}

type sandboxWorker struct {
	sandboxID string
	workID    string
	processID string
	busy      chan struct{}
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
		processes: make(map[string]*sandboxWorker),
	}, nil
}

func (l *Launcher) lockSandbox(ctx context.Context, sessionID string) (*sandboxWorker, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.processMu.Lock()
		entry := l.processes[sessionID]
		if entry == nil {
			entry = &sandboxWorker{}
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

func (l *Launcher) unlockSandbox(sessionID string, entry *sandboxWorker) {
	l.processMu.Lock()
	defer l.processMu.Unlock()
	close(entry.busy)
	entry.busy = nil
	if entry.sandboxID == "" {
		delete(l.processes, sessionID)
	}
}

func (l *Launcher) Start(ctx context.Context) error {
	pollCtx, cancel := context.WithCancel(ctx)
	var launches sync.WaitGroup
	defer func() {
		cancel()
		launches.Wait()
	}()
	if err := l.recoverWorkers(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	launches.Go(func() { l.collectPeriodically(pollCtx) })
	poller := environments.NewWorkPoller(pollCtx, l.anthropic, environments.WorkPollerOptions{
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
		if ctx.Err() != nil {
			break
		}
		work := poller.Current()
		launches.Go(func() {
			// Preserve authentication values while allowing an accepted operation
			// to finish when the manager requests a graceful drain.
			operationCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout+2*collectionTimeout)
			defer stop()
			if err := l.launch(operationCtx, work); err != nil {
				// SDK errors may contain request/response bodies, including scoped
				// credentials. Log only their category and stable work identity.
				l.log.ErrorContext(ctx, "failed to launch managed agent", "work_id", work.ID, "http_status", upstreamStatus(err), "grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
			}
		})
	}
	if err := poller.Err(); err != nil && ctx.Err() == nil {
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
	ctx, operationCancel := context.WithTimeout(ctx, provisionTimeout+2*collectionTimeout)
	defer operationCancel()
	attemptCtx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()
	entry, err := l.lockSandbox(attemptCtx, work.Data.ID)
	if err != nil {
		return err
	}
	defer l.unlockSandbox(work.Data.ID, entry)
	if entry.workID != "" && entry.workID == work.ID {
		if work.EnvironmentID != l.config.EnvironmentID || work.Data.Type != "session" {
			return errors.New("invalid managed agents tracked assignment")
		}
		state, err := l.inspectWorker(attemptCtx, entry)
		if err != nil {
			return err
		}
		if state == workerLive {
			return nil
		}
	}
	if work.ID == "" || work.EnvironmentID != l.config.EnvironmentID || work.Data.ID == "" || work.Secret == "" || work.Data.Type != "session" {
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
	var provisionErr error
	blockedByWorker := entry.sandboxID != ""
	if blockedByWorker {
		_, provisionErr = backoff.Retry(attemptCtx, func() (struct{}, error) {
			state, err := l.finishWorker(attemptCtx, entry)
			if err != nil {
				if !recoveryRetryable(err) {
					return struct{}{}, backoff.Permanent(err)
				}
				return struct{}{}, err
			}
			if state == workerLive {
				return struct{}{}, errors.New("previous managed agent CLI is still active")
			}
			return struct{}{}, nil
		}, backoff.WithBackOff(backoff.NewConstantBackOff(100*time.Millisecond)), backoff.WithMaxElapsedTime(0))
		blockedByWorker = provisionErr != nil
	}
	var sandbox *apiv1.Sandbox
	if provisionErr == nil {
		sandbox, provisionErr = l.provision(attemptCtx, work, entry)
	}
	close(halt)
	result := <-done
	if result.err != nil {
		return result.err
	}
	if provisionErr != nil {
		if !blockedByWorker && ctx.Err() == nil && time.Now().Before(result.lease.expiresAt()) {
			return errors.Join(provisionErr, l.stopWork(ctx, work.ID))
		}
		return provisionErr
	}
	// Leave time for the CLI's first heartbeat; never start on an expired lease.
	remaining := time.Until(result.lease.expiresAt()) - 2*time.Second
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
		Command: []string{"ant", "beta:worker", "run", "--workdir", "/data/workspace", "--log-format", "json", "--max-idle", "5m"},
		Cwd:     "/data/workspace",
		Env:     env,
		Timeout: durationpb.New(sandboxLifetime),
	})
	if err != nil {
		// Prototype assumption: StartProcess returns a definitive outcome.
		// Do not retry it; ambiguous response recovery is deferred.
		if ctx.Err() == nil && time.Now().Before(result.lease.expiresAt()) {
			err = errors.Join(err, l.stopWork(ctx, work.ID))
		}
		return fmt.Errorf("failed to start managed agent CLI: %w", err)
	}
	if process.GetProcessId() == "" || (process.GetState() == guestpb.ProcessState_PROCESS_STATE_EXITED && process.GetExitCode() != 0) {
		return errors.Join(errors.New("managed agent CLI did not start successfully"), l.stopWork(ctx, work.ID))
	}
	entry.sandboxID = sandbox.GetId()
	entry.workID = work.ID
	entry.processID = process.GetProcessId()
	l.log.InfoContext(ctx, "started managed agent CLI", "work_id", work.ID, "sandbox_id", sandbox.GetId(), "process_id", process.GetProcessId())
	return nil
}

func (l *Launcher) stopWork(ctx context.Context, workID string) error {
	if ctx.Err() != nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
		Name:            sandboxNamePrefix + sessionID,
		SandboxTemplate: &apiv1.ResourceReference{Namespace: cfg.TemplateNamespace, Name: cfg.TemplateName},
		Ttl:             durationpb.New(sandboxLifetime),
	}
}

func replacementRequest(cfg Config, sessionID, expiredID string) *apiv1.CreateSandboxRequest {
	request := createRequest(cfg, sessionID)
	request.RequestId += "-" + expiredID
	return request
}

func sandboxExpired(instance *apiv1.Sandbox) bool {
	expires := instance.GetExpiresAt()
	return expires != nil && expires.IsValid() && !time.Now().Before(expires.AsTime())
}

func (l *Launcher) provision(ctx context.Context, work *anthropic.BetaSelfHostedWork, entry *sandboxWorker) (*apiv1.Sandbox, error) {
	request := createRequest(l.config, work.Data.ID)
	var sandbox *apiv1.Sandbox
	discovered := false
	retryBackoff := backoff.NewExponentialBackOff()
	// Jitter spans 0.5-1 seconds initially and stays capped at 5 seconds.
	retryBackoff.InitialInterval = 750 * time.Millisecond
	retryBackoff.RandomizationFactor = 1.0 / 3
	retryBackoff.Multiplier = 2
	retryBackoff.MaxInterval = 3750 * time.Millisecond
	return backoff.Retry(ctx, func() (*apiv1.Sandbox, error) {
		if !discovered {
			found, err := l.savedSandbox(ctx, entry)
			if err != nil {
				if !recoveryRetryable(err) {
					return nil, backoff.Permanent(err)
				}
				return nil, err
			}
			sandbox = found
			discovered = true
		}
		if sandboxExpired(sandbox) {
			request = replacementRequest(l.config, work.Data.ID, sandbox.GetId())
			sandbox = nil
		}
		if sandbox != nil && sandboxUnavailable(sandbox) {
			return nil, backoff.Permanent(errors.New("managed agents sandbox is unavailable"))
		}
		var err error
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if sandbox == nil {
			created, createErr := l.sandboxes.Create(callCtx, request)
			if createErr != nil {
				// Failed receipts convey no resource or expiry evidence. A lost
				// creation can be recovered only while that receipt remains live.
				if !retryable(createErr) {
					return nil, backoff.Permanent(fmt.Errorf("managed agents sandbox receipt unavailable without saved successor expiry evidence: %w", createErr))
				}
				return nil, createErr
			}
			if uuid.Validate(created.GetId()) != nil {
				return nil, backoff.Permanent(errors.New("sandbox API returned an invalid sandbox"))
			}
			sandbox = created
			entry.sandboxID = created.GetId()
		}
		if sandboxExpired(sandbox) {
			return nil, errors.New("managed agents sandbox expired during provisioning")
		}
		if sandboxUnavailable(sandbox) {
			return nil, backoff.Permanent(errors.New("managed agents sandbox is unavailable"))
		}
		if !sandboxExpired(sandbox) && sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_READY && sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return sandbox, nil
		}
		if sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
			sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME ||
			sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND {
			var resumed *apiv1.Sandbox
			resumed, err = l.sandboxes.Resume(callCtx, sandbox.GetId())
			if err == nil {
				if resumed.GetId() != sandbox.GetId() {
					return nil, backoff.Permanent(errors.New("managed agents resumed sandbox identity mismatch"))
				}
				sandbox = resumed
			}
		} else {
			var observed *apiv1.Sandbox
			observed, err = l.sandboxes.Get(callCtx, sandbox.GetId())
			if err == nil {
				if identityErr := l.validateSavedSandbox(observed, sandbox.GetId()); identityErr != nil {
					return nil, backoff.Permanent(identityErr)
				}
				sandbox = observed
			}
		}
		if err != nil && !retryable(err) {
			return nil, backoff.Permanent(fmt.Errorf("failed to provision managed agents sandbox: %w", err))
		}
		if err == nil {
			if sandbox.GetState() == apiv1.RuntimeState_RUNTIME_STATE_READY && sandbox.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
				return sandbox, nil
			}
			if sandboxUnavailable(sandbox) && !sandboxExpired(sandbox) {
				return nil, backoff.Permanent(errors.New("managed agents sandbox is unavailable"))
			}
			return nil, errors.New("managed agents sandbox is not ready")
		}
		return nil, err
	}, backoff.WithBackOff(retryBackoff), backoff.WithMaxElapsedTime(0))
}

func sandboxUnavailable(instance *apiv1.Sandbox) bool {
	return instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE ||
		instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETED || instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETING ||
		(instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_FAILED && instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE)
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
