package grpcserver

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"time"

	"github.com/go-logr/logr"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var forwardedMetadataKeys = map[string]string{
	"authorization": "Authorization",
	"x-user-id":     "X-User-Id",
	"x-agent-name":  "X-Agent-Name",
	"x-share-token": "X-Share-Token",
}

func authenticationUnaryInterceptor(authenticator auth.AuthProvider, shareStore ShareStore, policies MethodPolicies) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		authenticatedContext, err := authenticate(ctx, info.FullMethod, authenticator, shareStore, policies)
		if err != nil {
			return nil, err
		}
		return handler(authenticatedContext, req)
	}
}

func authenticationStreamInterceptor(authenticator auth.AuthProvider, shareStore ShareStore, policies MethodPolicies) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		authenticatedContext, err := authenticate(stream.Context(), info.FullMethod, authenticator, shareStore, policies)
		if err != nil {
			return err
		}
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: authenticatedContext})
	}
}

func authenticate(ctx context.Context, fullMethod string, authenticator auth.AuthProvider, shareStore ShareStore, policies MethodPolicies) (context.Context, error) {
	access, ok := policies[fullMethod]
	if !ok {
		return ctx, status.Error(codes.PermissionDenied, "RPC authorization policy is not configured")
	}
	if access == AccessPublic {
		return ctx, nil
	}
	if authenticator == nil {
		return ctx, status.Error(codes.Unauthenticated, "authentication is not configured")
	}

	headers := incomingHTTPHeaders(ctx)
	session, err := authenticator.Authenticate(ctx, headers, url.Values{})
	if err != nil || session == nil {
		return ctx, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	authenticatedContext := auth.AuthSessionTo(ctx, session)
	shareToken := headers.Get("X-Share-Token")
	if shareToken == "" {
		return authenticatedContext, nil
	}
	if shareStore == nil {
		return ctx, status.Error(codes.Internal, "share-token validation is unavailable")
	}

	// Only the digest is stored, which is what stops a database dump being a set of
	// working share links — so the token is hashed the same way it was on creation.
	digest := sha256.Sum256([]byte(shareToken))
	instanceShare, err := shareStore.GetAgentInstanceShareByTokenHash(authenticatedContext, digest[:])
	if err != nil {
		if errors.Is(err, dbpkg.ErrNotFound) {
			return ctx, status.Error(codes.PermissionDenied, "invalid or expired share token")
		}
		return ctx, status.Error(codes.Internal, "failed to validate share token")
	}
	// READ_WRITE also allows A2A send and cancel; anything else is read-only.
	readOnly := instanceShare.Permission != agentInstanceShareReadWrite
	if readOnly && access != AccessPublic && access != AccessRead {
		return ctx, status.Error(codes.PermissionDenied, "this share link is read-only")
	}
	return auth.ShareContextTo(authenticatedContext, &auth.ShareContext{
		Token: shareToken,
		// The owner, not the visitor: the token widens what this account may reach
		// to what the owner can see, and the instance read runs as the owner.
		UserID:          instanceShare.OwnerUserID,
		ReadOnly:        readOnly,
		AgentInstanceID: instanceShare.InstanceID.String(),
	}), nil
}

// agentInstanceShareReadWrite is the permission that allows more than reading.
//
// Spelled as the column's own value rather than derived from the proto enum: the
// database stores 'READ_ONLY' or 'READ_WRITE' under a CHECK constraint, and that
// string is what this has to match.
const agentInstanceShareReadWrite = "READ_WRITE"

func incomingHTTPHeaders(ctx context.Context) http.Header {
	headers := make(http.Header)
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return headers
	}
	for metadataKey, headerName := range forwardedMetadataKeys {
		for _, value := range md.Get(metadataKey) {
			headers.Add(headerName, value)
		}
	}
	return headers
}

func recoverUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ctrllog.FromContext(ctx).Error(fmt.Errorf("panic: %v", recovered), "Recovered panic in gRPC request", "method", info.FullMethod, "stack", string(debug.Stack()))
			err = status.Error(codes.Internal, "internal server error")
		}
	}()
	return handler(ctx, req)
}

func recoverStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ctrllog.FromContext(stream.Context()).Error(fmt.Errorf("panic: %v", recovered), "Recovered panic in gRPC stream", "method", info.FullMethod, "stack", string(debug.Stack()))
			err = status.Error(codes.Internal, "internal server error")
		}
	}()
	return handler(srv, stream)
}

func loggingUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	log := requestLogger(ctx, info.FullMethod, "unary")
	ctx = ctrllog.IntoContext(ctx, log)
	response, err := handler(ctx, req)
	log.Info("RPC completed", "code", status.Code(err).String(), "duration", time.Since(start))
	return response, err
}

func loggingStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	log := requestLogger(stream.Context(), info.FullMethod, "stream")
	wrapped := &contextServerStream{ServerStream: stream, ctx: ctrllog.IntoContext(stream.Context(), log)}
	err := handler(srv, wrapped)
	log.Info("RPC completed", "code", status.Code(err).String(), "duration", time.Since(start))
	return err
}

func requestLogger(ctx context.Context, method, rpcType string) logr.Logger {
	values := []any{"method", method, "rpc_type", rpcType}
	if remotePeer, ok := peer.FromContext(ctx); ok {
		values = append(values, "peer", remotePeer.Addr.String())
	}
	return ctrllog.FromContext(ctx).WithName("grpc").WithValues(values...)
}

func errorMappingUnaryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	response, err := handler(ctx, req)
	return response, mapError(err)
}

func errorMappingStreamInterceptor(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return mapError(handler(srv, stream))
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	}
	if code := serviceerrors.CodeOf(err); code != "" {
		if code == serviceerrors.CodeInternal {
			return status.Error(codes.Internal, "internal server error")
		}
		return status.Error(serviceErrorCode(code), serviceerrors.MessageOf(err))
	}
	return status.Error(codes.Internal, "internal server error")
}

func serviceErrorCode(code serviceerrors.Code) codes.Code {
	switch code {
	case serviceerrors.CodeInvalidArgument:
		return codes.InvalidArgument
	case serviceerrors.CodeUnauthenticated:
		return codes.Unauthenticated
	case serviceerrors.CodePermissionDenied:
		return codes.PermissionDenied
	case serviceerrors.CodeNotFound:
		return codes.NotFound
	case serviceerrors.CodeAlreadyExists:
		return codes.AlreadyExists
	case serviceerrors.CodeFailedPrecondition:
		return codes.FailedPrecondition
	case serviceerrors.CodeResourceExhausted:
		return codes.ResourceExhausted
	case serviceerrors.CodeAborted:
		return codes.Aborted
	case serviceerrors.CodeUnavailable:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context {
	return s.ctx
}

type serverMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newServerMetrics(registerer prometheus.Registerer) (*serverMetrics, error) {
	metrics := &serverMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kagent_grpc_server_requests_total",
			Help: "Total number of completed kagent gRPC requests.",
		}, []string{"method", "rpc_type", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kagent_grpc_server_request_duration_seconds",
			Help:    "Duration of completed kagent gRPC requests in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "rpc_type"}),
	}
	if registerer == nil {
		return metrics, nil
	}
	if err := registerCollector(registerer, metrics.requests); err != nil {
		return nil, err
	}
	if err := registerCollector(registerer, metrics.duration); err != nil {
		return nil, err
	}
	return metrics, nil
}

func registerCollector(registerer prometheus.Registerer, collector prometheus.Collector) error {
	if err := registerer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			return nil
		}
		return err
	}
	return nil
}

func (m *serverMetrics) unaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	response, err := handler(ctx, req)
	m.observe(info.FullMethod, "unary", start, err)
	return response, err
}

func (m *serverMetrics) streamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, stream)
	m.observe(info.FullMethod, "stream", start, err)
	return err
}

func (m *serverMetrics) observe(method, rpcType string, start time.Time, err error) {
	m.requests.WithLabelValues(method, rpcType, status.Code(err).String()).Inc()
	m.duration.WithLabelValues(method, rpcType).Observe(time.Since(start).Seconds())
}
