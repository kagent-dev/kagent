package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"buf.build/go/protovalidate"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	protovalidatemiddleware "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	agentservice "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	feedbackservice "github.com/kagent-dev/kagent/go/core/internal/service/feedback"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	memoryservice "github.com/kagent-dev/kagent/go/core/internal/service/memory"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	toolservice "github.com/kagent-dev/kagent/go/core/internal/service/tool"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/v2/agentinstance"
	"github.com/kagent-dev/kagent/go/core/v2/checkpoint"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultBindAddress     = ":8084"
	DefaultMaxMessageSize  = 16 << 20
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	BindAddress           string
	MaxMessageBytes       int
	Reflection            bool
	TLSCertFile           string
	TLSKeyFile            string
	Authenticator         auth.AuthProvider
	ShareStore            ShareStore
	Registerer            prometheus.Registerer
	AgentService          *agentservice.Service
	AgentTemplateService  *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList]
	HarnessService        *kubecrud.Service[*v1alpha3.Harness, *v1alpha3.HarnessList]
	ModelService          *modelservice.Service
	ToolService           *toolservice.Service
	PromptTemplateService *prompttemplateservice.Service
	SystemService         *systemservice.Service
	FeedbackService       *feedbackservice.Service
	MemoryService         *memoryservice.Service
	AgentInstanceService  *agentinstance.Service
	CheckpointService     *checkpoint.Service
	A2AHandler            a2asrv.RequestHandler
	MethodPolicies        MethodPolicies
	Listener              net.Listener
}

type Server struct {
	config       Config
	server       *grpc.Server
	healthServer *health.Server
}

func New(config Config) (*Server, error) {
	if config.BindAddress == "" {
		config.BindAddress = DefaultBindAddress
	}
	if config.MaxMessageBytes <= 0 {
		config.MaxMessageBytes = DefaultMaxMessageSize
	}
	if config.SystemService == nil {
		config.SystemService = systemservice.NewService()
	}
	if config.MethodPolicies == nil {
		config.MethodPolicies = DefaultMethodPolicies()
	}

	metrics, err := newServerMetrics(config.Registerer)
	if err != nil {
		return nil, fmt.Errorf("create gRPC metrics: %w", err)
	}
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("create protobuf validator: %w", err)
	}

	serverOptions := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(config.MaxMessageBytes),
		grpc.MaxSendMsgSize(config.MaxMessageBytes),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor,
			metrics.unaryInterceptor,
			recoverUnaryInterceptor,
			authenticationUnaryInterceptor(config.Authenticator, config.ShareStore, config.MethodPolicies),
			protovalidatemiddleware.UnaryServerInterceptor(validator),
			errorMappingUnaryInterceptor,
		),
		grpc.ChainStreamInterceptor(
			loggingStreamInterceptor,
			metrics.streamInterceptor,
			recoverStreamInterceptor,
			authenticationStreamInterceptor(config.Authenticator, config.ShareStore, config.MethodPolicies),
			errorMappingStreamInterceptor,
		),
	}

	transportCredentials, err := loadTransportCredentials(config.TLSCertFile, config.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	if transportCredentials != nil {
		serverOptions = append(serverOptions, grpc.Creds(transportCredentials))
	}

	grpcServer := grpc.NewServer(serverOptions...)
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	apiv1alpha1.RegisterSystemServiceServer(grpcServer, newSystemServer(config.SystemService))
	if config.AgentService != nil {
		apiv1alpha1.RegisterAgentServiceServer(grpcServer, newAgentServer(config.AgentService, config.MaxMessageBytes))
	}
	if config.AgentTemplateService != nil {
		apiv1alpha1.RegisterAgentTemplateServiceServer(grpcServer, newAgentTemplateServer(config.AgentTemplateService, config.MaxMessageBytes))
	}
	// Registered separately from AgentService on purpose: this serves the
	// Harness CRD that AgentInstance pairs with an AgentTemplate, not the
	// AgentHarness CRD that AgentService's *AgentHarness RPCs serve.
	if config.HarnessService != nil {
		apiv1alpha1.RegisterHarnessServiceServer(grpcServer, newHarnessServer(config.HarnessService, config.MaxMessageBytes))
	}
	if config.ModelService != nil {
		apiv1alpha1.RegisterModelServiceServer(grpcServer, newModelServer(config.ModelService, config.MaxMessageBytes))
	}
	if config.ToolService != nil {
		apiv1alpha1.RegisterToolServiceServer(grpcServer, newToolServer(config.ToolService, config.MaxMessageBytes))
	}
	if config.PromptTemplateService != nil {
		apiv1alpha1.RegisterPromptTemplateServiceServer(grpcServer, newPromptTemplateServer(config.PromptTemplateService))
	}
	if config.FeedbackService != nil {
		apiv1alpha1.RegisterFeedbackServiceServer(grpcServer, newFeedbackServer(config.FeedbackService))
	}
	if config.MemoryService != nil {
		apiv1alpha1.RegisterMemoryServiceServer(grpcServer, newMemoryServer(config.MemoryService))
	}
	if config.AgentInstanceService != nil {
		agentinstance.RegisterGRPC(grpcServer, config.AgentInstanceService)
	}
	if config.CheckpointService != nil {
		checkpoint.RegisterGRPC(grpcServer, config.CheckpointService)
	}
	if config.A2AHandler != nil {
		a2agrpc.NewHandler(config.A2AHandler).RegisterWith(grpcServer)
	}
	if config.Reflection {
		reflection.Register(grpcServer)
	}

	return &Server{
		config:       config,
		server:       grpcServer,
		healthServer: healthServer,
	}, nil
}

type ShareStore interface {
	GetAgentInstanceShareByTokenHash(context.Context, []byte) (*dbpkg.AgentInstanceShare, error)
}

func (s *Server) Start(ctx context.Context) error {
	listener := s.config.Listener
	if listener == nil {
		var err error
		listener, err = net.Listen("tcp", s.config.BindAddress)
		if err != nil {
			return fmt.Errorf("listen for gRPC on %s: %w", s.config.BindAddress, err)
		}
	}

	log := ctrllog.FromContext(ctx).WithName("grpc-server")
	log.Info("Starting gRPC server", "address", listener.Addr().String())
	s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return fmt.Errorf("serve gRPC: %w", err)
	case <-ctx.Done():
		s.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		log.Info("Shutting down gRPC server")
		s.gracefulStop(defaultShutdownTimeout)
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("serve gRPC during shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) NeedLeaderElection() bool {
	return false
}

func (s *Server) gracefulStop(timeout time.Duration) {
	stopped := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(stopped)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stopped:
	case <-timer.C:
		s.server.Stop()
		<-stopped
	}
}

func loadTransportCredentials(certFile, keyFile string) (credentials.TransportCredentials, error) {
	if certFile == "" && keyFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, errors.New("both gRPC TLS certificate and key files must be configured")
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC TLS key pair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}), nil
}
