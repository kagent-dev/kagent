package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Default when OPENSHELL_GRPC_ADDR is unset and the WebSocket start frame omits grpc_address.
	// Short name "openshell:8080" only resolves if a Service "openshell" exists in the controller's
	// namespace; OpenShell is often installed in its own namespace (e.g. openshell).
	defaultOpenshellGRPCAddr = "openshell.openshell.svc.cluster.local:8080"
	openshellGRPCEnv         = "OPENSHELL_GRPC_ADDR"

	sandboxSSHWSReadBufSize     = 4096
	sandboxSSHHandshakeTimeout  = 90 * time.Second
	sandboxSSHWSWriteDeadline   = 15 * time.Second
	sandboxSSHDefaultCols       = 120
	sandboxSSHDefaultRows       = 36
	sandboxSSHCopyBufSize       = 32 * 1024
	sandboxSSHClientConnTimeout = 60 * time.Second
	sandboxSSHUser              = "sandbox"
	sandboxSSHPTYTerm           = "xterm-256color"
)

type sshStartMsg struct {
	SandboxName    string `json:"sandbox_name"`
	GRPCAddress    string `json:"grpc_address,omitempty"`
	Cols           int    `json:"cols,omitempty"`
	Rows           int    `json:"rows,omitempty"`
	PlainShell     bool   `json:"plain_shell,omitempty"`
	LaunchCommand  string `json:"launch_command,omitempty"`
	HarnessBackend string `json:"harness_backend,omitempty"`
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type wsCtrlMsg struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// HandleSandboxSSHWebSocket upgrades to WebSocket, accepts one JSON start frame, mints an SSH
// session via OpenShell gRPC from inside the cluster, opens a ForwardTcp relay and SSH shell,
// then proxies terminal I/O (same wire protocol as scripts/openshell-ssh-ws.mjs).
func (h *Handlers) HandleSandboxSSHWebSocket(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sandbox-ssh-ws")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	up := websocket.Upgrader{
		ReadBufferSize:  sandboxSSHWSReadBufSize,
		WriteBufferSize: sandboxSSHWSReadBufSize,
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}
	wsConn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Info("websocket upgrade failed", "error", err)
		return
	}

	start, err := readSandboxSSHStart(wsConn)
	if err != nil {
		closeWSWithError(wsConn, err.Error())
		return
	}
	grpcAddr := resolveOpenshellGRPCAddr(start)

	log.Info("openshell gRPC target", "addr", grpcAddr)

	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), sandboxSSHHandshakeTimeout)
	defer handshakeCancel()

	// ForwardTcp must outlive the handshake; do not tie the relay stream to handshakeCtx.
	grpcConn, sshClient, session, stdin, stdout, stderr, err := h.dialOpenshellShellSession(
		handshakeCtx, r.Context(), grpcAddr, start.SandboxName, start.Rows, start.Cols, start.PlainShell, start.LaunchCommand, start.HarnessBackend)
	if err != nil {
		log.Info("openshell ssh session failed", "error", err)
		closeWSWithError(wsConn, err.Error())
		return
	}
	defer grpcConn.Close()
	defer func() {
		_ = session.Close()
		_ = sshClient.Close()
	}()

	sendWSCtrl(wsConn, "ready", "")

	var wsWriteMu sync.Mutex
	writeWS := func(messageType int, p []byte) error {
		wsWriteMu.Lock()
		defer wsWriteMu.Unlock()
		deadline := time.Now().Add(sandboxSSHWSWriteDeadline)
		_ = wsConn.SetWriteDeadline(deadline)
		return wsConn.WriteMessage(messageType, p)
	}

	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		runSandboxSSHWSReader(wsConn, session, stdin)
	}()

	var streamWG sync.WaitGroup
	streamWG.Add(2)
	go copySSHStreamToWebSocket(stdout, writeWS, &streamWG)
	go copySSHStreamToWebSocket(stderr, writeWS, &streamWG)
	go func() {
		streamWG.Wait()
		log.Info("ssh stdout/stderr copy finished")
	}()

	select {
	case <-copyDone:
	case <-r.Context().Done():
	}
	_ = wsConn.Close()
	<-copyDone
}

func readSandboxSSHStart(wsConn *websocket.Conn) (sshStartMsg, error) {
	_, raw, err := wsConn.ReadMessage()
	if err != nil {
		return sshStartMsg{}, fmt.Errorf("failed to read start frame: %w", err)
	}
	var start sshStartMsg
	if err := json.Unmarshal(raw, &start); err != nil {
		return sshStartMsg{}, errors.New("first frame must be JSON start payload")
	}
	start.SandboxName = strings.TrimSpace(start.SandboxName)
	if start.SandboxName == "" {
		return sshStartMsg{}, errors.New("sandbox_name is required")
	}
	if start.Cols <= 0 {
		start.Cols = sandboxSSHDefaultCols
	}
	if start.Rows <= 0 {
		start.Rows = sandboxSSHDefaultRows
	}
	return start, nil
}

func resolveOpenshellGRPCAddr(start sshStartMsg) string {
	grpcAddr := strings.TrimSpace(start.GRPCAddress)
	if grpcAddr == "" {
		grpcAddr = strings.TrimSpace(os.Getenv(openshellGRPCEnv))
	}
	if grpcAddr == "" {
		grpcAddr = defaultOpenshellGRPCAddr
	}
	return grpcAddr
}

func closeWSWithError(ws *websocket.Conn, msg string) {
	sendWSCtrl(ws, "error", msg)
	_ = ws.Close()
}

func runSandboxSSHWSReader(wsConn *websocket.Conn, session *ssh.Session, stdin io.Writer) {
	for {
		mt, payload, rerr := wsConn.ReadMessage()
		if rerr != nil {
			return
		}
		handleSandboxSSHWSInbound(mt, payload, session, stdin)
	}
}

func handleSandboxSSHWSInbound(mt int, payload []byte, session *ssh.Session, stdin io.Writer) {
	switch mt {
	case websocket.TextMessage:
		if tryHandleSSHResize(payload, session) {
			return
		}
		_, _ = stdin.Write(payload)
	case websocket.BinaryMessage:
		_, _ = stdin.Write(payload)
	}
}

// parseSSHResizePayload parses a browser JSON resize control frame into PTY rows/cols.
func parseSSHResizePayload(payload []byte) (rows, cols int, ok bool) {
	if len(payload) == 0 || payload[0] != '{' {
		return 0, 0, false
	}
	var rm resizeMsg
	if json.Unmarshal(payload, &rm) != nil || rm.Type != "resize" || rm.Cols <= 0 || rm.Rows <= 0 {
		return 0, 0, false
	}
	return rm.Rows, rm.Cols, true
}

func tryHandleSSHResize(payload []byte, session *ssh.Session) bool {
	rows, cols, ok := parseSSHResizePayload(payload)
	if !ok {
		return false
	}
	_ = session.WindowChange(rows, cols)
	return true
}

func sendWSCtrl(ws *websocket.Conn, typ, msg string) {
	payload, _ := json.Marshal(wsCtrlMsg{Type: typ, Message: msg})
	_ = ws.WriteMessage(websocket.TextMessage, payload)
}

// copySSHStreamToWebSocket forwards one SSH session stream (stdout or stderr) to the browser WebSocket.
func copySSHStreamToWebSocket(r io.Reader, writeWS func(messageType int, p []byte) error, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, sandboxSSHCopyBufSize)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if werr := writeWS(websocket.BinaryMessage, buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}

func (h *Handlers) dialOpenshellShellSession(
	handshakeCtx, streamCtx context.Context,
	grpcAddr, sandboxName string,
	rows, cols int,
	plainShell bool,
	launchCommandFromClient string,
	harnessBackend string,
) (
	grpcConn *grpc.ClientConn,
	sshClient *ssh.Client,
	session *ssh.Session,
	stdin io.WriteCloser,
	stdout io.Reader,
	stderr io.Reader,
	err error,
) {
	grpcConn, err = grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("grpc dial %q: %w", grpcAddr, err)
	}

	cli := openshellv1.NewOpenShellClient(grpcConn)
	sandboxID, sshRes, err := openshellCreateSSHSession(handshakeCtx, cli, sandboxName)
	if err != nil {
		_ = grpcConn.Close()
		return nil, nil, nil, nil, nil, nil, err
	}

	if sid := strings.TrimSpace(sshRes.GetSandboxId()); sid != "" {
		sandboxID = sid
	}
	tunnelConn, err := openshellDialForwardTcp(streamCtx, cli, sandboxID, sshRes.GetToken())
	if err != nil {
		_ = grpcConn.Close()
		return nil, nil, nil, nil, nil, nil, err
	}

	sshClient, session, stdin, stdout, stderr, err = openSSHSessionOverTunnel(
		tunnelConn, "openshell", rows, cols, plainShell, launchCommandFromClient, harnessBackend,
	)
	if err != nil {
		_ = grpcConn.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	return grpcConn, sshClient, session, stdin, stdout, stderr, nil
}

func sandboxIDForSSH(sb *openshellv1.Sandbox) string {
	if sb == nil || sb.GetMetadata() == nil {
		return ""
	}
	meta := sb.GetMetadata()
	if id := strings.TrimSpace(meta.GetId()); id != "" {
		return id
	}
	return strings.TrimSpace(meta.GetName())
}

func openshellCreateSSHSession(
	ctx context.Context,
	cli openshellv1.OpenShellClient,
	sandboxName string,
) (sandboxID string, sshRes *openshellv1.CreateSshSessionResponse, err error) {
	sbRes, err := cli.GetSandbox(ctx, &openshellv1.GetSandboxRequest{Name: sandboxName})
	if err != nil {
		return "", nil, fmt.Errorf("GetSandbox: %w", err)
	}
	sandboxID = sandboxIDForSSH(sbRes.GetSandbox())
	if sandboxID == "" {
		return "", nil, fmt.Errorf("sandbox %q: response missing metadata id and name", sandboxName)
	}

	sshRes, err = cli.CreateSshSession(ctx, &openshellv1.CreateSshSessionRequest{SandboxId: sandboxID})
	if err != nil {
		return "", nil, fmt.Errorf("CreateSshSession: %w", err)
	}

	token := strings.TrimSpace(sshRes.GetToken())
	if token == "" {
		return "", nil, errors.New("CreateSshSession returned empty session token")
	}
	return sandboxID, sshRes, nil
}

// openshellDialForwardTcp opens the OpenShell ForwardTcp bidi stream used by the CLI ssh-proxy.
func openshellDialForwardTcp(
	ctx context.Context,
	cli openshellv1.OpenShellClient,
	sandboxID, token string,
) (net.Conn, error) {
	sandboxID = strings.TrimSpace(sandboxID)
	token = strings.TrimSpace(token)
	if sandboxID == "" || token == "" {
		return nil, errors.New("sandbox id and session token are required for SSH forward")
	}

	stream, err := cli.ForwardTcp(ctx)
	if err != nil {
		return nil, fmt.Errorf("ForwardTcp: %w", err)
	}

	if err := stream.Send(buildForwardTcpSSHInit(sandboxID, token)); err != nil {
		_ = stream.CloseSend()
		return nil, fmt.Errorf("ForwardTcp init: %w", err)
	}
	return newTCPForwardConn(stream), nil
}

func buildForwardTcpSSHInit(sandboxID, token string) *openshellv1.TcpForwardFrame {
	return &openshellv1.TcpForwardFrame{
		Payload: &openshellv1.TcpForwardFrame_Init{
			Init: &openshellv1.TcpForwardInit{
				SandboxId:          sandboxID,
				ServiceId:          fmt.Sprintf("ssh-proxy:%s", sandboxID),
				AuthorizationToken: token,
				Target: &openshellv1.TcpForwardInit_Ssh{
					Ssh: &openshellv1.SshRelayTarget{},
				},
			},
		},
	}
}

// tcpForwardConn adapts OpenShell ForwardTcp to net.Conn for golang.org/x/crypto/ssh.
// A background reader pumps gateway data into rbuf so ssh handshakes can read and write
// concurrently (same pattern as openshell-cli ssh-proxy's split stdin/stdout tasks).
type tcpForwardConn struct {
	stream openshellv1.OpenShell_ForwardTcpClient

	readMu   sync.Mutex
	readCond *sync.Cond
	rbuf     []byte
	recvErr  error
	closed   bool

	sendMu sync.Mutex
}

func newTCPForwardConn(stream openshellv1.OpenShell_ForwardTcpClient) *tcpForwardConn {
	c := &tcpForwardConn{stream: stream}
	c.readCond = sync.NewCond(&c.readMu)
	go c.pumpRecv()
	return c
}

func (c *tcpForwardConn) pumpRecv() {
	for {
		frame, err := c.stream.Recv()
		c.readMu.Lock()
		if c.closed {
			c.readMu.Unlock()
			return
		}
		if err != nil {
			if c.recvErr == nil {
				c.recvErr = err
			}
			c.readCond.Broadcast()
			c.readMu.Unlock()
			return
		}
		if frame != nil {
			if data := frame.GetData(); len(data) > 0 {
				c.rbuf = append(c.rbuf, data...)
				c.readCond.Broadcast()
			}
		}
		c.readMu.Unlock()
	}
}

func (c *tcpForwardConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.rbuf) == 0 && c.recvErr == nil && !c.closed {
		c.readCond.Wait()
	}
	if c.closed && len(c.rbuf) == 0 {
		return 0, io.EOF
	}
	if len(c.rbuf) == 0 {
		if c.recvErr != nil {
			return 0, c.recvErr
		}
		return 0, io.EOF
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *tcpForwardConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.readMu.Lock()
	closed := c.closed
	c.readMu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if err := c.stream.Send(&openshellv1.TcpForwardFrame{
		Payload: &openshellv1.TcpForwardFrame_Data{Data: append([]byte(nil), p...)},
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *tcpForwardConn) Close() error {
	c.readMu.Lock()
	if c.closed {
		c.readMu.Unlock()
		return nil
	}
	c.closed = true
	if c.recvErr == nil {
		c.recvErr = io.EOF
	}
	c.readCond.Broadcast()
	c.readMu.Unlock()

	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.CloseSend()
}

func (c *tcpForwardConn) LocalAddr() net.Addr  { return &tcpForwardAddr{} }
func (c *tcpForwardConn) RemoteAddr() net.Addr { return &tcpForwardAddr{} }

func (c *tcpForwardConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *tcpForwardConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *tcpForwardConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type tcpForwardAddr struct{}

func (tcpForwardAddr) Network() string { return "tcp" }
func (tcpForwardAddr) String() string  { return "openshell-forward-tcp" }

func openSSHSessionOverTunnel(
	tunnelConn net.Conn,
	dialHost string,
	rows, cols int,
	plainShell bool,
	launchCommandFromClient string,
	harnessBackend string,
) (
	sshClient *ssh.Client,
	session *ssh.Session,
	stdin io.WriteCloser,
	stdout io.Reader,
	stderr io.Reader,
	err error,
) {
	sshConn, chans, reqs, err := ssh.NewClientConn(tunnelConn, dialHost, &ssh.ClientConfig{
		User: sandboxSSHUser,
		Auth: []ssh.AuthMethod{ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			return make([]string, len(questions)), nil
		})},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sandboxSSHClientConnTimeout,
	})
	if err != nil {
		_ = tunnelConn.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh handshake: %w", err)
	}
	sshClient = ssh.NewClient(sshConn, chans, reqs)

	session, err = sshClient.NewSession()
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh NewSession: %w", err)
	}

	stdin, err = session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh StdinPipe: %w", err)
	}
	stdout, err = session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh StdoutPipe: %w", err)
	}
	stderr, err = session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh StderrPipe: %w", err)
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1}
	if err := session.RequestPty(sandboxSSHPTYTerm, rows, cols, modes); err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("ssh RequestPty: %w", err)
	}
	useShell, remoteCmd := openshell.ResolveSSHRemoteCommand(plainShell, launchCommandFromClient, harnessBackend)
	if useShell {
		if err := session.Shell(); err != nil {
			_ = session.Close()
			_ = sshClient.Close()
			return nil, nil, nil, nil, nil, fmt.Errorf("ssh Shell: %w", err)
		}
	} else {
		if err := session.Start(remoteCmd); err != nil {
			_ = session.Close()
			_ = sshClient.Close()
			return nil, nil, nil, nil, nil, fmt.Errorf("ssh Start %q: %w", remoteCmd, err)
		}
	}

	return sshClient, session, stdin, stdout, stderr, nil
}

func isLoopbackHost(h string) bool {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	default:
		return false
	}
}

// resolveGatewayDialHost maps CreateSshSession's gateway_host to a TCP dial target from the controller pod.
// When OpenShell returns loopback, we dial the host from the OpenShell gRPC address (same Service, any namespace).
func resolveGatewayDialHost(gatewayHost, grpcTarget string) (string, error) {
	if !isLoopbackHost(gatewayHost) {
		return gatewayHost, nil
	}
	host, _, err := net.SplitHostPort(grpcTarget)
	if err != nil {
		return "", fmt.Errorf(
			"CreateSshSession gateway_host=%q is loopback; grpc target %q must be host:port so kagent can dial the OpenShell service: %w",
			gatewayHost, grpcTarget, err)
	}
	if host == "" {
		return "", fmt.Errorf(
			"CreateSshSession gateway_host=%q is loopback; grpc target %q has an empty host",
			gatewayHost, grpcTarget)
	}
	return host, nil
}
