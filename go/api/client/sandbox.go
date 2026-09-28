package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sandboxapi "github.com/kagent-dev/kagent/go/api/sandbox"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// SandboxClient uses the control-plane connection for lifecycle and guest calls.
// Process starts are never retried: a lost response can mean a running command.
type SandboxClient struct {
	client *baseClient
}

func (c *SandboxClient) call(ctx context.Context) (*grpc.ClientConn, context.Context, context.CancelFunc, error) {
	conn, err := c.client.grpcConnection()
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := c.client.grpcCallContext(ctx)
	return conn, ctx, cancel, nil
}

func (c *SandboxClient) guestCall(ctx context.Context, sandboxID string) (*grpc.ClientConn, context.Context, context.CancelFunc, error) {
	if sandboxID == "" {
		return nil, nil, nil, errors.New("sandbox ID is required")
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(sandboxapi.IDHeader, sandboxID)
	return c.call(metadata.NewOutgoingContext(ctx, md))
}

func (c *SandboxClient) ListSandboxTemplates(ctx context.Context, request *apiv1alpha1.ListSandboxTemplatesRequest) (*apiv1alpha1.ListSandboxTemplatesResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxTemplateServiceClient(conn).ListSandboxTemplates(ctx, request)
}

func (c *SandboxClient) CreateSandbox(ctx context.Context, request *apiv1alpha1.CreateSandboxRequest) (*apiv1alpha1.CreateSandboxResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).CreateSandbox(ctx, request)
}

func (c *SandboxClient) GetSandbox(ctx context.Context, request *apiv1alpha1.GetSandboxRequest) (*apiv1alpha1.GetSandboxResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).GetSandbox(ctx, request)
}

func (c *SandboxClient) ListSandboxes(ctx context.Context, request *apiv1alpha1.ListSandboxesRequest) (*apiv1alpha1.ListSandboxesResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).ListSandboxes(ctx, request)
}

func (c *SandboxClient) SuspendSandbox(ctx context.Context, request *apiv1alpha1.SuspendSandboxRequest) (*apiv1alpha1.SuspendSandboxResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).SuspendSandbox(ctx, request)
}

func (c *SandboxClient) ResumeSandbox(ctx context.Context, request *apiv1alpha1.ResumeSandboxRequest) (*apiv1alpha1.ResumeSandboxResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).ResumeSandbox(ctx, request)
}

func (c *SandboxClient) DeleteSandbox(ctx context.Context, request *apiv1alpha1.DeleteSandboxRequest) (*apiv1alpha1.DeleteSandboxResponse, error) {
	conn, ctx, cancel, err := c.call(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return apiv1alpha1.NewSandboxServiceClient(conn).DeleteSandbox(ctx, request)
}

func (c *SandboxClient) StartProcess(ctx context.Context, sandboxID string, request *guestpb.StartProcessRequest) (*guestpb.StartProcessResponse, error) {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return guestpb.NewProcessServiceClient(conn).StartProcess(ctx, request)
}

func (c *SandboxClient) GetProcess(ctx context.Context, sandboxID string, request *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return guestpb.NewProcessServiceClient(conn).GetProcess(ctx, request)
}

func (c *SandboxClient) KillProcess(ctx context.Context, sandboxID string, request *guestpb.KillProcessRequest) (*guestpb.KillProcessResponse, error) {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return guestpb.NewProcessServiceClient(conn).KillProcess(ctx, request)
}

// ReadProcessOutputs drains the upstream stream. The caller owns continuation
// offsets and whether to follow output; callbacks may stop the stream with an error.
func (c *SandboxClient) ReadProcessOutputs(ctx context.Context, sandboxID string, request *guestpb.StreamProcessOutputsRequest, receive func(*guestpb.OutputChunk) error) error {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return err
	}
	defer cancel()
	stream, err := guestpb.NewProcessServiceClient(conn).StreamProcessOutputs(ctx, request)
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
		if err := receive(chunk); err != nil {
			return err
		}
	}
}

func (c *SandboxClient) ReadFile(ctx context.Context, sandboxID, path string, out io.Writer) error {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return err
	}
	defer cancel()
	stream, err := guestpb.NewFileSystemServiceClient(conn).ReadFile(ctx, &guestpb.ReadFileRequest{Path: path})
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
		n, err := out.Write(chunk.Data)
		if err != nil {
			return err
		}
		if n != len(chunk.Data) {
			return io.ErrShortWrite
		}
	}
}

// WriteFile streams bytes without base64. The server's file transfer limit
// applies. Failed or interrupted writes can leave a partial remote file.
func (c *SandboxClient) WriteFile(ctx context.Context, sandboxID, path string, mode uint32, in io.Reader) (*guestpb.WriteFileResponse, error) {
	conn, ctx, cancel, err := c.guestCall(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	stream, err := guestpb.NewFileSystemServiceClient(conn).WriteFile(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&guestpb.WriteFileRequest{Path: path, Mode: mode}); err != nil {
		if errors.Is(err, io.EOF) {
			_, receiveErr := stream.CloseAndRecv()
			return nil, errors.Join(io.ErrShortWrite, receiveErr)
		}
		return nil, err
	}
	buffer := make([]byte, 256<<10)
	var written int64
	for {
		n, readErr := in.Read(buffer)
		if n > 0 {
			if err := stream.Send(&guestpb.WriteFileRequest{Chunk: buffer[:n]}); err != nil {
				if errors.Is(err, io.EOF) {
					_, receiveErr := stream.CloseAndRecv()
					return nil, errors.Join(io.ErrShortWrite, receiveErr)
				}
				return nil, err
			}
			written += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	result, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	if result.BytesWritten != written {
		return nil, fmt.Errorf("file transfer acknowledged %d bytes, sent %d", result.BytesWritten, written)
	}
	return result, nil
}
