package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/stats"

	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const (
	a2aMaxContentLengthEnvVar = "A2A_MAX_CONTENT_LENGTH"
	defaultMaxContentLength   = int64(10 * 1024 * 1024)
)

// ServerConfig holds configuration for the A2A server.
type ServerConfig struct {
	Host            string
	Port            string
	ShutdownTimeout time.Duration
	// HealthPaths are literal exact paths served by HealthHandler and excluded from tracing.
	HealthPaths   []string
	HealthHandler http.Handler

	// Telemetry is the compiler-owned identity every invocation span reports.
	// Request identity is added by whichever component resolves it, never here.
	Telemetry tracing.RuntimeTelemetry
	Flush     func(context.Context) error
}

// A2AServer wraps the A2A server with health endpoints and graceful shutdown.
type A2AServer struct {
	httpServer   *http.Server
	readyServer  *http.Server
	grpcServer   *grpc.Server
	healthServer *health.Server
	logger       *slog.Logger
	config       ServerConfig
	listenErr    chan error
}

// NewA2AServer creates a new A2A server using a2asrv.
func NewA2AServer(agentCard a2atype.AgentCard, executor a2asrv.AgentExecutor, logger *slog.Logger, config ServerConfig, handlerOpts ...a2asrv.RequestHandlerOption) (*A2AServer, error) {
	handlerOpts = append(handlerOpts, a2asrv.WithCallInterceptors(
		newInvocationInterceptor(logger, config.Telemetry, config.Flush)))
	requestHandler := a2asrv.NewHandler(executor, handlerOpts...)
	jsonrpcHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	if maxContentLength := getMaxContentLength(logger); maxContentLength != nil {
		jsonrpcHandler = withRequestSizeLimit(jsonrpcHandler, *maxContentLength)
	}

	healthPaths := defaultHealthPaths()
	if config.HealthPaths != nil {
		for _, path := range config.HealthPaths {
			// Mux patterns (subtrees, wildcards) would route what the exact-match tracing filter misses.
			if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.ContainsAny(path, "{} \t") {
				return nil, fmt.Errorf("health path %q must be a literal path", path)
			}
		}
		healthPaths = slices.Clone(config.HealthPaths)
	}
	healthHandler := config.HealthHandler
	if healthHandler == nil {
		healthHandler = defaultHealthHandler
	}
	mux := http.NewServeMux()
	registerHealthEndpoints(mux, healthPaths, healthHandler)
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&agentCard))
	mux.Handle("/", jsonrpcHandler)

	grpcServer := grpc.NewServer(grpc.StatsHandler(rpcEndSignal{otelgrpc.NewServerHandler(
		otelgrpc.WithFilter(filters.Not(filters.HealthCheck())))}))
	a2agrpc.NewHandler(requestHandler).RegisterWith(grpcServer)
	healthServer := health.NewServer()
	healthServer.SetServingStatus(a2apb.A2AService_ServiceDesc.ServiceName, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	isGRPC := func(r *http.Request) bool {
		return r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
	}
	// Health and agent-card requests are neither traced nor flushed.
	isA2ARequest := func(r *http.Request) bool {
		switch {
		case strings.HasPrefix(r.URL.Path, "/grpc.health.v1.Health/"):
			return false
		case r.URL.Path == a2asrv.WellKnownAgentCardPath, slices.Contains(healthPaths, r.URL.Path):
			return false
		default:
			return true
		}
	}
	// gRPC calls get their SERVER span from otelgrpc, so otelhttp sees only
	// the JSON-RPC and HTTP paths.
	httpHandler := otelhttp.NewHandler(mux, "a2a-server", otelhttp.WithFilter(isA2ARequest))
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGRPC(r) {
			grpcServer.ServeHTTP(w, r)
			return
		}
		httpHandler.ServeHTTP(w, r)
	})
	// Flush again on handler return for errors and non-quiescent responses.
	// Quiescent events must flush earlier: the gateway may suspend or pause
	// the actor immediately upon receiving the event, before HTTP body close.
	handler := http.Handler(routed)
	if config.Flush != nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isA2ARequest(r) {
				routed.ServeHTTP(w, r)
				return
			}
			// One flush record per request: after the quiescent flush fails, the
			// handler-return flush is skipped, so the stream ends without a second
			// flush budget and the gateway's drain stays short.
			ended := make(chan struct{})
			r = r.WithContext(context.WithValue(telemetry.WithFlushRecord(r.Context()), rpcEndedKey{}, ended))
			routed.ServeHTTP(w, r)
			// grpc-go returns from ServeHTTP before the stream goroutine runs
			// stats.End, where otelgrpc ends the SERVER span. The response is
			// finished only when this handler returns, so wait for it here.
			if isGRPC(r) {
				select {
				case <-ended:
				case <-time.After(rpcEndWait):
				}
			}
			if err := config.Flush(r.Context()); err != nil {
				logger.ErrorContext(r.Context(), "failed to flush traces after A2A handler", "error", err)
			}
		})
	}

	addr := ":" + config.Port
	if config.Host != "" {
		addr = net.JoinHostPort(config.Host, config.Port)
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &A2AServer{
		httpServer: &http.Server{
			Addr:      addr,
			Handler:   handler,
			Protocols: protocols,
		},
		readyServer: &http.Server{Addr: ":8081", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/readyz" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		})},
		grpcServer:   grpcServer,
		healthServer: healthServer,
		logger:       logger,
		config:       config,
	}, nil
}

// rpcEndWait bounds the wait for stats.End, for a call that never starts a
// stream and so never ends one.
const rpcEndWait = 500 * time.Millisecond

type rpcEndedKey struct{}

// rpcEndSignal closes the request's channel once the wrapped handler has
// processed stats.End.
type rpcEndSignal struct{ stats.Handler }

func (h rpcEndSignal) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	h.Handler.HandleRPC(ctx, rpcStats)
	if _, ok := rpcStats.(*stats.End); ok {
		if ended, _ := ctx.Value(rpcEndedKey{}).(chan struct{}); ended != nil {
			close(ended)
		}
	}
}

func getMaxContentLength(logger *slog.Logger) *int64 {
	value, ok := os.LookupEnv(a2aMaxContentLengthEnvVar)
	if !ok {
		maxContentLength := defaultMaxContentLength
		return &maxContentLength
	}

	trimmedValue := strings.TrimSpace(value)
	switch strings.ToLower(trimmedValue) {
	case "0", "none", "unlimited":
		return nil
	}

	maxContentLength, err := strconv.ParseInt(trimmedValue, 10, 64)
	if err != nil || maxContentLength < 0 {
		logger.Info(
			"invalid A2A request size limit, using default",
			"environment_variable", a2aMaxContentLengthEnvVar,
			"value", value,
			"default", defaultMaxContentLength,
		)
		maxContentLength = defaultMaxContentLength
	}
	return &maxContentLength
}

func withRequestSizeLimit(next http.Handler, maxContentLength int64) http.Handler {
	sizeLimitedHandler := http.MaxBytesHandler(next, maxContentLength)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxContentLength {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		sizeLimitedHandler.ServeHTTP(w, r)
	})
}

// Start initializes and starts the HTTP server.
func (s *A2AServer) Start() error {
	s.logger.Info("starting Go ADK server!", "addr", s.httpServer.Addr)

	s.listenErr = make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.listenErr <- err
		}
	}()
	go func() {
		if err := s.readyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.listenErr <- err
		}
	}()

	return nil
}

// WaitForShutdown blocks until a shutdown signal is received or the listener
// fails, then gracefully shuts down.
func (s *A2AServer) WaitForShutdown() error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case <-stop:
		s.logger.Info("shutting down server...")
	case err := <-s.listenErr:
		return fmt.Errorf("server listen failed: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	s.healthServer.Shutdown()
	grpcStopped := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(grpcStopped)
	}()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.grpcServer.Stop()
		<-grpcStopped
		return fmt.Errorf("error shutting down server: %w", err)
	}
	if err := s.readyServer.Shutdown(ctx); err != nil {
		s.grpcServer.Stop()
		<-grpcStopped
		return fmt.Errorf("error shutting down readiness server: %w", err)
	}
	<-grpcStopped

	return nil
}

// Run starts the server and waits for shutdown.
func (s *A2AServer) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	return s.WaitForShutdown()
}
