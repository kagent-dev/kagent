package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
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
	requestHandler := a2asrv.NewHandler(executor, handlerOpts...)
	jsonrpcHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	if maxContentLength := getMaxContentLength(logger); maxContentLength != nil {
		jsonrpcHandler = withRequestSizeLimit(jsonrpcHandler, *maxContentLength)
	}

	mux := http.NewServeMux()
	RegisterHealthEndpoints(mux)
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&agentCard))
	mux.Handle("/", jsonrpcHandler)

	grpcServer := grpc.NewServer()
	a2agrpc.NewHandler(requestHandler).RegisterWith(grpcServer)
	healthServer := health.NewServer()
	healthServer.SetServingStatus(a2apb.A2AService_ServiceDesc.ServiceName, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	handlerMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	// Health and agent-card requests are neither traced nor flushed; only A2A
	// requests get an inbound server span and a span flush.
	isA2ARequest := func(r *http.Request) bool {
		switch {
		case strings.HasPrefix(r.URL.Path, "/grpc.health.v1.Health/"):
			return false
		case r.URL.Path == "/health", r.URL.Path == "/healthz", r.URL.Path == a2asrv.WellKnownAgentCardPath:
			return false
		default:
			return true
		}
	}
	// Wrap the whole server mux to enable trace context extraction and an inbound
	// HTTP server span for each request.
	instrumentedHandler := otelhttp.NewHandler(
		handlerMux,
		"a2a-server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
		otelhttp.WithFilter(isA2ARequest),
	)
	// Pre-response span flushing is opt-in via KAGENT_PRE_RESPONSE_TRACE_FLUSH
	// (the controller sets it on Agent Substrate actors): a checkpoint/suspend
	// runtime freezes as soon as the response body closes, making this the only
	// reliable export window. Everywhere else the batch exporter's timer
	// suffices, and a per-request flush would only add export churn and, during
	// a collector outage, response-tail latency.
	//
	// When enabled, flush after the otelhttp server span ends (when the inner
	// handler returns) but before net/http closes the response body — a flush
	// issued inside the executor can never include the still-open server span.
	handler := http.Handler(instrumentedHandler)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("KAGENT_PRE_RESPONSE_TRACE_FLUSH")), "true") {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			instrumentedHandler.ServeHTTP(w, r)
			if isA2ARequest(r) {
				telemetry.ForceFlush(r.Context())
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
