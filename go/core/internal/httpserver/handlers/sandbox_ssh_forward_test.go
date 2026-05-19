package handlers

import (
	"context"
	"io"
	"testing"
	"time"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func TestBuildForwardTcpSSHInit(t *testing.T) {
	init := buildForwardTcpSSHInit("sb-uuid", "tok-abc")
	require.NotNil(t, init.GetInit())
	require.Equal(t, "sb-uuid", init.GetInit().GetSandboxId())
	require.Equal(t, "ssh-proxy:sb-uuid", init.GetInit().GetServiceId())
	require.Equal(t, "tok-abc", init.GetInit().GetAuthorizationToken())
	require.NotNil(t, init.GetInit().GetSsh())
}

func TestTCPForwardConnReadWrite(t *testing.T) {
	stream := newMockForwardTcpStream()
	conn := newTCPForwardConn(stream)

	require.NoError(t, stream.pushRecv(&openshellv1.TcpForwardFrame{
		Payload: &openshellv1.TcpForwardFrame_Data{Data: []byte("SSH-2.0-test\r\n")},
	}))

	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "SSH-2.0-test\r\n", string(buf[:n]))

	_, err = conn.Write([]byte("SSH-2.0-client\r\n"))
	require.NoError(t, err)

	select {
	case frame := <-stream.sent:
		require.Equal(t, "SSH-2.0-client\r\n", string(frame.GetData()))
	default:
		t.Fatal("expected forwarded client data frame")
	}

	require.NoError(t, conn.Close())
}

func TestTCPForwardConnCloseUnblocksRead(t *testing.T) {
	stream := newMockForwardTcpStream()
	conn := newTCPForwardConn(stream)

	readDone := make(chan struct{})
	var readErr error
	go func() {
		_, readErr = conn.Read(make([]byte, 1))
		close(readDone)
	}()

	time.Sleep(50 * time.Millisecond)

	require.NoError(t, conn.Close())

	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock after Close")
	}
	require.ErrorIs(t, readErr, io.EOF)
}

// mockForwardTcpStream is a minimal OpenShell_ForwardTcpClient for unit tests.
type mockForwardTcpStream struct {
	recv chan *openshellv1.TcpForwardFrame
	sent chan *openshellv1.TcpForwardFrame
}

func newMockForwardTcpStream() *mockForwardTcpStream {
	return &mockForwardTcpStream{
		recv: make(chan *openshellv1.TcpForwardFrame, 8),
		sent: make(chan *openshellv1.TcpForwardFrame, 8),
	}
}

func (m *mockForwardTcpStream) pushRecv(frame *openshellv1.TcpForwardFrame) error {
	m.recv <- frame
	return nil
}

func (m *mockForwardTcpStream) Send(frame *openshellv1.TcpForwardFrame) error {
	m.sent <- frame
	return nil
}

func (m *mockForwardTcpStream) Recv() (*openshellv1.TcpForwardFrame, error) {
	frame, ok := <-m.recv
	if !ok {
		return nil, io.EOF
	}
	return frame, nil
}

func (m *mockForwardTcpStream) SendMsg(msg any) error {
	frame, ok := msg.(*openshellv1.TcpForwardFrame)
	if !ok {
		return io.ErrUnexpectedEOF
	}
	return m.Send(frame)
}

func (m *mockForwardTcpStream) RecvMsg(msg any) error {
	frame, err := m.Recv()
	if err != nil {
		return err
	}
	out, ok := msg.(*openshellv1.TcpForwardFrame)
	if !ok {
		return io.ErrUnexpectedEOF
	}
	proto.Reset(out)
	proto.Merge(out, frame)
	return nil
}

func (m *mockForwardTcpStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockForwardTcpStream) Trailer() metadata.MD         { return nil }
func (m *mockForwardTcpStream) CloseSend() error             { close(m.sent); return nil }
func (m *mockForwardTcpStream) Context() context.Context     { return context.Background() }
