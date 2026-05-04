package handlers

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Default when OPENSHELL_GRPC_ADDR is unset and the WebSocket start frame omits grpc_address.
	// Short name "openshell:8080" only resolves if a Service "openshell" exists in the controller's
	// namespace; OpenShell is often installed in its own namespace (e.g. openshell).
	defaultOpenshellGRPCAddr   = "openshell.openshell.svc.cluster.local:8080"
	openshellGRPCEnv           = "OPENSHELL_GRPC_ADDR"
	defaultSandboxSSHLaunchCmd = "openclaw tui"

	sandboxSSHWSReadBufSize      = 4096
	sandboxSSHHandshakeTimeout   = 90 * time.Second
	sandboxSSHWSWriteDeadline    = 15 * time.Second
	sandboxSSHDefaultCols        = 120
	sandboxSSHDefaultRows        = 36
	sandboxSSHCopyBufSize        = 32 * 1024
	sandboxSSHGatewayDialTimeout = 30 * time.Second
	sandboxSSHClientConnTimeout  = 60 * time.Second
	sandboxSSHUser               = "sandbox"
	sandboxSSHPTYTerm            = "xterm-256color"
)

type sshStartMsg struct {
	SandboxName   string `json:"sandbox_name"`
	GRPCAddress   string `json:"grpc_address,omitempty"`
	Cols          int    `json:"cols,omitempty"`
	Rows          int    `json:"rows,omitempty"`
	PlainShell    bool   `json:"plain_shell,omitempty"`
	LaunchCommand string `json:"launch_command,omitempty"`
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
// session via OpenShell gRPC from inside the cluster, opens an HTTP CONNECT tunnel and SSH shell,
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

	ctx, cancel := context.WithTimeout(r.Context(), sandboxSSHHandshakeTimeout)
	defer cancel()

	sshClient, session, stdin, stdout, stderr, err := h.dialOpenshellShellSession(
		ctx, grpcAddr, start.SandboxName, start.Rows, start.Cols, start.PlainShell, start.LaunchCommand)
	if err != nil {
		log.Info("openshell ssh session failed", "error", err)
		closeWSWithError(wsConn, err.Error())
		return
	}
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

	streamDone := make(chan struct{})
	var streamWG sync.WaitGroup
	streamWG.Add(2)
	go copySSHStreamToWebSocket(stdout, writeWS, &streamWG)
	go copySSHStreamToWebSocket(stderr, writeWS, &streamWG)
	go func() {
		streamWG.Wait()
		close(streamDone)
	}()

	select {
	case <-copyDone:
	case <-streamDone:
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

func resolveSandboxSSHRemoteCommand(plainShell bool, launchCommandFromClient string) (plain bool, execCmd string) {
	if plainShell {
		return true, ""
	}
	cmd := strings.TrimSpace(launchCommandFromClient)
	if cmd == "" {
		cmd = defaultSandboxSSHLaunchCmd
	}
	return false, cmd
}

func (h *Handlers) dialOpenshellShellSession(
	ctx context.Context,
	grpcAddr, sandboxName string,
	rows, cols int,
	plainShell bool,
	launchCommandFromClient string,
) (
	sshClient *ssh.Client,
	session *ssh.Session,
	stdin io.WriteCloser,
	stdout io.Reader,
	stderr io.Reader,
	err error,
) {
	grpcConn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("grpc dial %q: %w", grpcAddr, err)
	}
	defer grpcConn.Close()

	cli := openshellv1.NewOpenShellClient(grpcConn)
	sandboxID, sshRes, err := openshellCreateSSHSession(ctx, cli, sandboxName)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	tunnelConn, dialHost, err := openshellDialHTTPConnectTunnel(ctx, grpcAddr, sshRes, sandboxID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return openSSHSessionOverTunnel(tunnelConn, dialHost, rows, cols, plainShell, launchCommandFromClient)
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

	token := sshRes.GetToken()
	gwHost := sshRes.GetGatewayHost()
	gwPort := sshRes.GetGatewayPort()
	scheme := strings.ToLower(strings.TrimSpace(sshRes.GetGatewayScheme()))
	connectPath := sshRes.GetConnectPath()
	if token == "" || gwHost == "" || gwPort == 0 || scheme == "" || connectPath == "" {
		return "", nil, errors.New("CreateSshSession returned incomplete tunnel fields")
	}
	return sandboxID, sshRes, nil
}

func openshellDialHTTPConnectTunnel(
	ctx context.Context,
	grpcAddr string,
	sshRes *openshellv1.CreateSshSessionResponse,
	sandboxID string,
) (tunnelConn net.Conn, dialHost string, err error) {
	token := sshRes.GetToken()
	gwHost := sshRes.GetGatewayHost()
	gwPort := sshRes.GetGatewayPort()
	scheme := strings.ToLower(strings.TrimSpace(sshRes.GetGatewayScheme()))
	connectPath := sshRes.GetConnectPath()
	sid := sshRes.GetSandboxId()
	if sid == "" {
		sid = sandboxID
	}

	dialHost, err = resolveGatewayDialHost(gwHost, grpcAddr)
	if err != nil {
		return nil, "", err
	}
	if dialHost != gwHost {
		log := ctrllog.FromContext(ctx)
		log.Info("using cluster-reachable host for OpenShell gateway (CreateSshSession returned loopback)",
			"gateway_host", gwHost, "dial_host", dialHost)
	}

	rawConn, err := dialGateway(scheme, dialHost, int(gwPort))
	if err != nil {
		return nil, "", err
	}

	tunnelConn, err = completeHTTPConnect(ctx, rawConn, dialHost, connectPath, sid, token)
	if err != nil {
		_ = rawConn.Close()
		return nil, "", err
	}
	return tunnelConn, dialHost, nil
}

func openSSHSessionOverTunnel(
	tunnelConn net.Conn,
	dialHost string,
	rows, cols int,
	plainShell bool,
	launchCommandFromClient string,
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
	useShell, remoteCmd := resolveSandboxSSHRemoteCommand(plainShell, launchCommandFromClient)
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

func dialGateway(scheme, host string, port int) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: sandboxSSHGatewayDialTimeout}
	switch scheme {
	case "https":
		serverName := host
		if strings.HasPrefix(host, "[") && strings.Contains(host, "]") {
			serverName = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
		return tls.DialWithDialer(&d, "tcp", addr, &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: serverName,
		})
	case "http":
		return d.Dial("tcp", addr)
	default:
		return nil, fmt.Errorf("unsupported gateway_scheme %q", scheme)
	}
}

func completeHTTPConnect(ctx context.Context, conn net.Conn, gatewayHost, connectPath, sandboxID, token string) (net.Conn, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}
	req := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nX-Sandbox-Id: %s\r\nX-Sandbox-Token: %s\r\n\r\n",
		connectPath,
		gatewayHost,
		sandboxID,
		token,
	)
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("CONNECT write: %w", err)
	}

	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("CONNECT read status: %w", err)
	}
	if !strings.Contains(statusLine, " 200 ") {
		return nil, fmt.Errorf("CONNECT failed: %s", strings.TrimSpace(statusLine))
	}
	for {
		line, rerr := br.ReadString('\n')
		if rerr != nil {
			return nil, fmt.Errorf("CONNECT read headers: %w", rerr)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return &prefixReaderConn{Conn: conn, br: br}, nil
}

type prefixReaderConn struct {
	net.Conn
	br *bufio.Reader
}

func (p *prefixReaderConn) Read(b []byte) (int, error) {
	if p.br != nil && p.br.Buffered() > 0 {
		n, err := p.br.Read(b)
		if p.br.Buffered() == 0 {
			p.br = nil
		}
		return n, err
	}
	return p.Conn.Read(b)
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
