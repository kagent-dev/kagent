// E2E coverage for RemoteMCPServer TLS support and the tool-server
// companion-Secret API. These tests stand up a real mockmcp fixture
// (in-process, optionally with TLS) on the test host, install matching
// kagent resources in the cluster, drive an agent invocation through
// the A2A endpoint, and assert on the mockmcp request recorder.
//
// Prerequisites (mirror the existing e2e tests in invoke_api_test.go):
//
//   - A kind cluster with kagent installed.
//   - `kubectl port-forward -n kagent deployments/kagent-controller 8083`
//     (or KAGENT_URL set) so the tests can reach the HTTP API.
//   - The cluster must be able to dial the test host on `host.docker.internal`
//     (Mac) / `172.17.0.1` (Linux) — same indirection mockllm uses;
//     buildK8sURL() in invoke_api_test.go does the translation.

package e2e_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/mockmcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockmcpFixture is a started mockmcp.Server plus the host-visible base
// URL agents and the controller should dial. baseURL routes through
// host.docker.internal (Mac) / 172.17.0.1 (Linux) so in-cluster pods can
// reach the test host — same shape as setupMockServer() does for the
// mock LLM.
type mockmcpFixture struct {
	server      *mockmcp.Server
	baseURL     string // dial URL from inside the cluster
	mcpURL      string // dial URL with the /mcp path appended
	sseURL      string // dial URL with the /sse path appended
	caCertPEM   []byte // when TLS, the PEM bundle to install as a Secret (nil for plaintext fixtures)
	hostBindURL string // local 127.0.0.1 URL for debug logging — NOT the cluster-facing URL
}

// setupMockMCP starts a mockmcp fixture. When withTLS is true a fresh
// self-signed cert is minted for the duration of the test and returned
// in caCertPEM so the caller can write it into a Secret for the RMS to
// reference. Server is registered with t.Cleanup; callers don't manage
// shutdown.
func setupMockMCP(t *testing.T, withTLS bool, opts mockmcp.Options) *mockmcpFixture {
	t.Helper()

	// Bind on all interfaces so the controller pod (inside kind) can
	// reach the listener via the kind-network gateway IP. Binding to
	// "127.0.0.1:0" rejects connections from outside the loopback
	// interface, which is what kind pod traffic looks like after
	// routing through the network gateway. Ephemeral port still keeps
	// concurrent tests from fighting.
	if opts.Addr == "" {
		opts.Addr = ":0"
	}
	opts.RecordRequests = true

	var caPEM []byte
	if withTLS {
		// Mint a self-signed CA + server cert. SANs must include
		// whichever host alias / IP the in-cluster pod ends up
		// dialing — that varies by network setup:
		//   * Mac without override: host.docker.internal (DNS)
		//   * Linux Docker default bridge: 172.17.0.1
		//   * kind on Linux: kind network gateway (often 172.18.0.1)
		//     — passed via KAGENT_LOCAL_HOST in CI
		dnsNames, ips := certSubjectAltNames()
		certPEM, keyPEM, ca := generateSelfSignedCert(t, dnsNames, ips)
		opts.CertPEM = certPEM
		opts.KeyPEM = keyPEM
		caPEM = ca
	}

	server, err := mockmcp.NewServer(opts)
	require.NoError(t, err)

	hostURL, err := server.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	})

	// buildK8sURL hard-codes "http://" — fine for the mockllm path it
	// was written for, but mockmcp's TLS scenarios serve HTTPS and the
	// scheme must round-trip to the controller's dial config. Reuse
	// buildK8sURL to pick the right host (kind gateway IP / docker
	// alias), then restore the actual scheme mockmcp listened on.
	clusterURL := strings.Replace(buildK8sURL(hostURL), "http://", schemeOf(hostURL)+"://", 1)

	return &mockmcpFixture{
		server:      server,
		baseURL:     clusterURL,
		mcpURL:      clusterURL + mockmcp.MCPPath,
		sseURL:      clusterURL + mockmcp.SSEPath,
		caCertPEM:   caPEM,
		hostBindURL: hostURL,
	}
}

// schemeOf returns "http" or "https" depending on the URL's prefix.
// Used by setupMockMCP to preserve mockmcp's actual listening scheme
// when buildK8sURL rewrites the host (which it does by string-edit,
// not by url.Parse, so it can't be asked to preserve the scheme
// itself without changing its signature for one caller).
func schemeOf(rawURL string) string {
	if strings.HasPrefix(rawURL, "https://") {
		return "https"
	}
	return "http"
}

// certSubjectAltNames returns the DNS-name and IP SAN lists for the
// mockmcp self-signed cert. Mirrors buildK8sURL's host-selection
// logic so the cert validates the same host the controller actually
// dials. Includes liberal fallbacks (kind default + Docker default)
// so the local-dev path doesn't require an explicit env override.
func certSubjectAltNames() ([]string, []net.IP) {
	dns := []string{"localhost", "host.docker.internal"}
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("172.17.0.1"), // Docker default bridge
		net.ParseIP("172.18.0.1"), // common kind gateway
	}

	// KAGENT_LOCAL_HOST is the dynamic gateway IP CI detects from
	// `docker network inspect kind`. Include it as a SAN so the
	// controller's TLS verification accepts whatever IP the runner
	// happens to have allocated. buildK8sURL's OS-default fallbacks
	// (host.docker.internal on darwin, 172.17.0.1 on linux) are
	// already covered by the static DNS/IP lists above.
	if override := os.Getenv("KAGENT_LOCAL_HOST"); override != "" {
		if ip := net.ParseIP(override); ip != nil {
			ips = append(ips, ip)
		} else {
			dns = append(dns, override)
		}
	}

	return dns, ips
}

// createCASecret writes the CA PEM produced by setupMockMCP into a
// Kubernetes Secret in the kagent namespace under the given name and
// key. Registers for cleanup. Returns the Secret so the caller can
// reference it from RMS.spec.tls.
func createCASecret(t *testing.T, cli client.Client, name, key string, ca []byte) *corev1.Secret {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kagent"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{key: ca},
	}
	require.NoError(t, cli.Create(t.Context(), secret))
	cleanup(t, cli, secret)
	return secret
}

// createRMS creates the RMS in the kagent namespace via the typed
// client and waits for Status.DiscoveredTools to populate — that's the
// signal the controller's tool-discovery code path completed end-to-end
// (including the TLS handshake when applicable). Returns the RMS so the
// caller can assert on Status if needed.
func createRMS(t *testing.T, cli client.Client, spec v1alpha2.RemoteMCPServerSpec) *v1alpha2.RemoteMCPServer {
	t.Helper()
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-rms-",
			Namespace:    "kagent",
		},
		Spec: spec,
	}
	require.NoError(t, cli.Create(t.Context(), rms))
	cleanup(t, cli, rms)

	// Poll Status.DiscoveredTools — populates only after a successful
	// ListTools call against the upstream, which is the end-to-end
	// signal we want.
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		fresh := &v1alpha2.RemoteMCPServer{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: rms.Namespace, Name: rms.Name}, fresh); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if len(fresh.Status.DiscoveredTools) > 0 {
			*rms = *fresh
			return true, nil
		}
		return false, nil
	})
	require.NoError(t, pollErr, "RMS Status.DiscoveredTools did not populate within timeout")
	return rms
}

// waitForAPIDiscoveredTools polls GET /api/toolservers until the named
// RemoteMCPServer's DiscoveredTools list is non-empty, then returns the
// tools. The CRD status field and this HTTP response come from
// different write paths in the reconciler (the field is set by
// setRemoteMCPServerStatusConditions, the API serves rows the
// reconciler persisted via RefreshToolsForServer), so a working CRD
// status doesn't strictly imply the API has caught up — poll instead
// of assuming.
func waitForAPIDiscoveredTools(t *testing.T, namespace, name string) []*v1alpha2.MCPTool {
	t.Helper()
	ref := namespace + "/" + name
	var matched []*v1alpha2.MCPTool

	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, kagentURL()+"/api/toolservers", nil)
		if err != nil {
			return false, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false, nil
		}
		var list httpapi.StandardResponse[[]httpapi.ToolServerResponse]
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return false, err
		}
		for _, ts := range list.Data {
			if ts.Ref == ref && len(ts.DiscoveredTools) > 0 {
				matched = ts.DiscoveredTools
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for tools on /api/toolservers for %s", ref)
	return matched
}

// generateSelfSignedCert mints an ECDSA self-signed certificate scoped
// to the given DNS names + IPs. Returns the certificate PEM, the key
// PEM (both bundled for mockmcp), and a CA PEM identical to the
// certificate (since it's self-signed the cert IS its own trust
// anchor). For more elaborate chains the CA could be separate, but
// self-signed-as-CA is sufficient for what RMS TLS needs to exercise:
// the controller pins a single PEM as RootCAs.
func generateSelfSignedCert(t *testing.T, dnsNames []string, ips []net.IP) (certPEM, keyPEM, caPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "kagent-e2e-mockmcp"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              dnsNames,
		IPAddresses:           ips,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	// Self-signed: the cert IS the CA bundle.
	return cert, keyPEM, cert
}

// TestE2E_RMS_PrivateCAUpstream exercises the production path:
// RMS.spec.tls.caCertSecretRef pins a CA Secret, the controller reads
// the Secret to build its tool-discovery http.Client, and the agent
// pod mounts the same Secret to use during invocation. The mockmcp
// fixture is the upstream MCP server; its request recorder lets us
// assert that the controller's ListTools call AND the agent's
// tools/call both reached the fixture over TLS with the right cert
// chain.
func TestE2E_RMS_PrivateCAUpstream(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_remotemcpserver_tls_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)

	mcp := setupMockMCP(t, true, mockmcp.Options{})
	caSecret := createCASecret(t, cli, "e2e-rms-tls-ca", "ca.crt", mcp.caCertPEM)

	rms := createRMS(t, cli, v1alpha2.RemoteMCPServerSpec{
		Description: "Private-CA HTTPS upstream",
		URL:         mcp.mcpURL,
		Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		TLS: &v1alpha2.TLSConfig{
			CACertSecretRef: caSecret.Name,
			CACertSecretKey: "ca.crt",
		},
	})

	// Sanity-check the controller's ListTools call landed on mockmcp.
	requests := mcp.server.Requests()
	assert.NotEmpty(t, requests, "mockmcp should have recorded the controller's ListTools call")

	// DiscoveredTools should include mockmcp's default tool set on the
	// CRD status (the K8s API surface).
	toolNames := make(map[string]bool)
	for _, tool := range rms.Status.DiscoveredTools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["add_numbers"], "expected add_numbers in Status.DiscoveredTools, got %v", toolNames)

	// And on the kagent HTTP API (what the UI calls). The reconciler
	// persists the post-ListTools result to the DB at
	// reconciler.go:RefreshToolsForServer, separate from the CRD status
	// write — verifying both confirms the controller "shows" tools on
	// every surface an operator might look at.
	apiTools := waitForAPIDiscoveredTools(t, rms.Namespace, rms.Name)
	apiNames := make(map[string]bool)
	for _, tool := range apiTools {
		apiNames[tool.Name] = true
	}
	assert.True(t, apiNames["add_numbers"], "expected add_numbers via /api/toolservers, got %v", apiNames)

	// Drive an agent invocation through the A2A endpoint. The mock LLM
	// is preprogrammed to issue a tools/call to add_numbers; mockmcp
	// answers it via the request recorder.
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupAgent(t, cli, modelCfg.Name, []*v1alpha2.Tool{{
		Type: v1alpha2.ToolProviderType_McpServer,
		McpServer: &v1alpha2.McpServerTool{
			TypedReference: v1alpha2.TypedReference{
				ApiGroup: "kagent.dev",
				Kind:     "RemoteMCPServer",
				Name:     rms.Name,
			},
			ToolNames: []string{"add_numbers"},
		},
	}})
	a2aClient := setupA2AClient(t, agent)

	runSyncTest(t, a2aClient, "add 2 and 3", "5", nil)

	// The agent's tools/call should also have reached mockmcp.
	postInvoke := mcp.server.Requests()
	assert.Greater(t, len(postInvoke), len(requests),
		"mockmcp should have recorded additional requests from the agent's tools/call")
}

// TestE2E_RMS_DisableVerify confirms the test-fixture escape hatch
// works end-to-end: an RMS with disableVerify=true (and no Secret)
// connects successfully to the same self-signed fixture that
// PrivateCAUpstream uses. The point of the test is to prove the
// controller actually skipped verification — if disableVerify
// silently fell back to system trust, the handshake would fail on
// the self-signed cert.
func TestE2E_RMS_DisableVerify(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_remotemcpserver_tls_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)

	mcp := setupMockMCP(t, true, mockmcp.Options{})

	rms := createRMS(t, cli, v1alpha2.RemoteMCPServerSpec{
		Description: "Skip-verify HTTPS upstream",
		URL:         mcp.mcpURL,
		Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		TLS: &v1alpha2.TLSConfig{
			DisableVerify: true,
		},
	})

	assert.NotEmpty(t, rms.Status.DiscoveredTools,
		"DiscoveredTools should populate even though the upstream cert isn't trusted by system CAs")

	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupAgent(t, cli, modelCfg.Name, []*v1alpha2.Tool{{
		Type: v1alpha2.ToolProviderType_McpServer,
		McpServer: &v1alpha2.McpServerTool{
			TypedReference: v1alpha2.TypedReference{
				ApiGroup: "kagent.dev",
				Kind:     "RemoteMCPServer",
				Name:     rms.Name,
			},
			ToolNames: []string{"add_numbers"},
		},
	}})
	a2aClient := setupA2AClient(t, agent)

	runSyncTest(t, a2aClient, "add 2 and 3", "5", nil)
}

// TestE2E_RMS_SSE_TLS exercises the SSE-transport-with-TLS code path.
// SSE and Streamable HTTP go through different translator functions
// (translateSseHttpTool vs translateStreamableHttpTool) and the Python
// runtime has separate factory-installation logic per transport. Both
// must work against a real /sse endpoint backed by a private CA.
func TestE2E_RMS_SSE_TLS(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_remotemcpserver_tls_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)

	mcp := setupMockMCP(t, true, mockmcp.Options{})
	caSecret := createCASecret(t, cli, "e2e-rms-sse-ca", "ca.crt", mcp.caCertPEM)

	rms := createRMS(t, cli, v1alpha2.RemoteMCPServerSpec{
		Description: "Private-CA SSE upstream",
		URL:         mcp.sseURL,
		Protocol:    v1alpha2.RemoteMCPServerProtocolSse,
		TLS: &v1alpha2.TLSConfig{
			CACertSecretRef: caSecret.Name,
			CACertSecretKey: "ca.crt",
		},
	})

	// Find the actual mockmcp request path the controller landed on to
	// confirm SSE was used (not Streamable HTTP). The mockmcp.SSEPath
	// is the request side; the recorder captures it on every request.
	requests := mcp.server.Requests()
	require.NotEmpty(t, requests, "mockmcp should have recorded the controller's ListTools call over SSE")
	var sawSSE bool
	for _, req := range requests {
		if req.Path == mockmcp.SSEPath {
			sawSSE = true
			break
		}
	}
	assert.True(t, sawSSE, "expected at least one request on the SSE path (%s); recorded paths: %v",
		mockmcp.SSEPath, recordedPaths(requests))

	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupAgent(t, cli, modelCfg.Name, []*v1alpha2.Tool{{
		Type: v1alpha2.ToolProviderType_McpServer,
		McpServer: &v1alpha2.McpServerTool{
			TypedReference: v1alpha2.TypedReference{
				ApiGroup: "kagent.dev",
				Kind:     "RemoteMCPServer",
				Name:     rms.Name,
			},
			ToolNames: []string{"add_numbers"},
		},
	}})
	a2aClient := setupA2AClient(t, agent)

	runSyncTest(t, a2aClient, "add 2 and 3", "5", nil)
}

// TestE2E_API_ToolServerCompanionSecrets posts a ToolServerCreateRequest
// with inline SecretMaterials and asserts the controller materializes
// both the RMS and the companion Secret in a single round-trip — the
// "one POST" UX equivalent of ModelConfig's existing inline-Secret
// support. Also exercises the OwnerReference: deleting the RMS should
// cascade-delete the Secret via K8s GC.
//
// This test doesn't need mockmcp running; it stops at "did the
// controller create the resources correctly?", which is the API
// contract under test.
func TestE2E_API_ToolServerCompanionSecrets(t *testing.T) {
	cli := setupK8sClient(t, true)

	rmsName := fmt.Sprintf("e2e-rms-companion-%d", time.Now().UnixNano())
	caSecretName := rmsName + "-ca"

	body, err := json.Marshal(handlers.ToolServerCreateRequest{
		Type: "RemoteMCPServer",
		RemoteMCPServer: &v1alpha2.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rmsName,
				Namespace: "kagent",
			},
			Spec: v1alpha2.RemoteMCPServerSpec{
				Description: "Companion-Secret integration test",
				URL:         "https://nowhere.invalid/mcp",
				TLS: &v1alpha2.TLSConfig{
					CACertSecretRef: caSecretName,
					CACertSecretKey: "ca.crt",
				},
			},
		},
		Secrets: []httpapi.SecretMaterial{
			{Name: caSecretName, Key: "ca.crt", Value: "FAKE PEM CONTENT"},
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), "POST",
		kagentURL()+"/api/toolservers", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "expected 201 Created on POST /api/toolservers")

	// The RMS should exist in K8s.
	rms := &v1alpha2.RemoteMCPServer{}
	require.NoError(t, cli.Get(t.Context(),
		client.ObjectKey{Namespace: "kagent", Name: rmsName}, rms))
	// Companion-Secret deletion happens via OwnerReference GC when we
	// delete the RMS in cleanup; register this for that path.
	cleanup(t, cli, rms)

	// The Secret should exist in K8s with the expected content.
	secret := &corev1.Secret{}
	require.NoError(t, cli.Get(t.Context(),
		client.ObjectKey{Namespace: "kagent", Name: caSecretName}, secret))
	assert.Equal(t, []byte("FAKE PEM CONTENT"), secret.Data["ca.crt"])

	// OwnerReference should point back at the RMS so K8s GC cleans it up.
	require.Len(t, secret.OwnerReferences, 1)
	or := secret.OwnerReferences[0]
	assert.Equal(t, "RemoteMCPServer", or.Kind)
	assert.Equal(t, rmsName, or.Name)
	assert.Equal(t, v1alpha2.GroupVersion.Identifier(), or.APIVersion)
}

// recordedPaths is a small helper for assertion failure messages — turns
// the request recorder's snapshot into a human-readable list of paths
// when an expected request doesn't show up.
func recordedPaths(reqs []mockmcp.RecordedRequest) []string {
	paths := make([]string, len(reqs))
	for i, req := range reqs {
		paths[i] = req.Method + " " + req.Path
	}
	return paths
}
