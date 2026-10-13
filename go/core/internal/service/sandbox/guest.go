package sandbox

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const maxFileBytes = 64 << 20

type GuestDialer struct {
	conn          *grpc.ClientConn
	authenticator auth.AuthProvider
	endpoint      string
}

func NewGuestDialer(routerURL string, authenticator auth.AuthProvider) (*GuestDialer, error) {
	router, err := url.Parse(routerURL)
	if err != nil || router.Host == "" || (router.Scheme != "http" && router.Scheme != "https") {
		return nil, fmt.Errorf("invalid sandbox router URL")
	}
	transport := insecure.NewCredentials()
	if router.Scheme == "https" {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: router.Hostname()})
	}
	conn, err := grpc.NewClient(router.Host, grpc.WithTransportCredentials(transport))
	if err != nil {
		return nil, err
	}
	return &GuestDialer{conn: conn, authenticator: authenticator, endpoint: routerURL}, nil
}

func (d *GuestDialer) Close() error { return d.conn.Close() }

// context replaces outgoing metadata. A caller cannot redirect a guest request
// with an incoming actor header, authority, environment ID, or gRPC target.
func (d *GuestDialer) context(ctx context.Context, instance *apiv1alpha1.Sandbox, revision *database.SandboxRevision) (context.Context, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, nil)
	if err != nil {
		return nil, err
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Guest access requires authentication", nil)
	}
	if err := d.authenticator.UpstreamAuth(request, session, auth.Principal{User: auth.User{ID: instance.Creator}}); err != nil {
		return nil, err
	}
	md := metadata.MD{}
	for key, values := range request.Header {
		md[strings.ToLower(key)] = values
	}
	md.Set("ate-target-actor", revision.ActorTemplateAtespace+"/"+substrate.ActorName(instance.Id))
	return metadata.NewOutgoingContext(ctx, md), nil
}

func (s *Service) guestAccess(ctx context.Context, id string, verb auth.Verb) (context.Context, context.CancelFunc, error) {
	if err := uuid.Validate(id); err != nil {
		return nil, nil, serviceerrors.NewInvalidArgument("Sandbox ID must be a UUID", err)
	}
	instance, err := s.authorized(ctx, id, verb)
	if err != nil {
		return nil, nil, err
	}
	if instance.State != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY || !time.Now().Before(instance.ExpiresAt.AsTime()) {
		return nil, nil, serviceerrors.NewFailedPrecondition("Sandbox is not ready or has expired", database.ErrFailedPrecondition)
	}
	revision, err := s.config.Store.GetSandboxRevision(ctx, instance.PreparedRevision)
	if err != nil {
		return nil, nil, serviceerrors.NewUnavailable("Cannot resolve sandbox runtime", err)
	}
	guestCtx, err := s.config.Guests.context(ctx, instance, revision)
	if err != nil {
		return nil, nil, err
	}
	guestCtx, cancel := context.WithDeadline(guestCtx, instance.ExpiresAt.AsTime())
	return guestCtx, cancel, nil
}

func (s *Service) StartProcess(ctx context.Context, sandboxID string, request *guestpb.StartProcessRequest) (*guestpb.StartProcessResponse, error) {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	defer cancelGuest()
	callCtx, cancel := context.WithTimeout(guestCtx, 30*time.Second)
	defer cancel()
	return guestpb.NewProcessServiceClient(s.config.Guests.conn).StartProcess(callCtx, request)
}

func (s *Service) GetProcess(ctx context.Context, sandboxID string, request *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbGet)
	if err != nil {
		return nil, err
	}
	defer cancelGuest()
	callCtx, cancel := context.WithTimeout(guestCtx, 30*time.Second)
	defer cancel()
	return guestpb.NewProcessServiceClient(s.config.Guests.conn).GetProcess(callCtx, request)
}

func (s *Service) KillProcess(ctx context.Context, sandboxID string, request *guestpb.KillProcessRequest) (*guestpb.KillProcessResponse, error) {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	defer cancelGuest()
	callCtx, cancel := context.WithTimeout(guestCtx, 30*time.Second)
	defer cancel()
	return guestpb.NewProcessServiceClient(s.config.Guests.conn).KillProcess(callCtx, request)
}

func (s *Service) StreamProcessOutputs(ctx context.Context, sandboxID string, request *guestpb.StreamProcessOutputsRequest, send func(*guestpb.OutputChunk) error) error {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbGet)
	if err != nil {
		return err
	}
	defer cancelGuest()
	stream, err := guestpb.NewProcessServiceClient(s.config.Guests.conn).StreamProcessOutputs(guestCtx, request)
	if err != nil {
		return err
	}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := send(chunk); err != nil {
			return err
		}
	}
}

func (s *Service) ReadFile(ctx context.Context, sandboxID string, request *guestpb.ReadFileRequest, send func(*guestpb.FileChunk) error) error {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbGet)
	if err != nil {
		return err
	}
	defer cancelGuest()
	stream, err := guestpb.NewFileSystemServiceClient(s.config.Guests.conn).ReadFile(guestCtx, request)
	if err != nil {
		return err
	}
	var total int64
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		total += int64(len(chunk.Data))
		if total > maxFileBytes {
			return serviceerrors.NewResourceExhausted("File exceeds 64 MiB transfer limit", nil)
		}
		if err := send(chunk); err != nil {
			return err
		}
	}
}

func (s *Service) WriteFile(ctx context.Context, sandboxID string, recv func() (*guestpb.WriteFileRequest, error)) (*guestpb.WriteFileResponse, error) {
	guestCtx, cancelGuest, err := s.guestAccess(ctx, sandboxID, auth.VerbUpdate)
	if err != nil {
		return nil, err
	}
	defer cancelGuest()
	stream, err := guestpb.NewFileSystemServiceClient(s.config.Guests.conn).WriteFile(guestCtx)
	if err != nil {
		return nil, err
	}
	var total int64
	for {
		chunk, err := recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		total += int64(len(chunk.GetChunk()))
		if total > maxFileBytes {
			return nil, serviceerrors.NewResourceExhausted("File exceeds 64 MiB transfer limit", nil)
		}
		if err := stream.Send(chunk); errors.Is(err, io.EOF) {
			// Receive the guest's status when it rejects the write early.
			return stream.CloseAndRecv()
		} else if err != nil {
			return nil, err
		}
	}
	return stream.CloseAndRecv()
}
