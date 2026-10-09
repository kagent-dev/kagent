package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/mockllm"
	"github.com/kagent-dev/mockmcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The egress credential contract: a client covered by a Secret-backed binding
// sends the binding's header carrying a placeholder, and Substrate's egress
// gateway replaces the value with the Secret's. The gateway replaces only a
// header the request carries, so a client that sends none reaches its upstream
// without the credential. Each case records what its upstream received and
// requires the Secret value on every request and the placeholder on none.

func TestModelCredentialDelivery(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		const token = "gateway-injection-e2e-token"
		header, wantCredential := "Authorization", "Bearer "+token
		if harness.name == claudeE2EHarness {
			header, wantCredential = "X-Api-Key", token
		}
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		secret := createCredentialSecret(t, kube, token)
		origin, received := startCredentialMock(t, header, wantCredential)
		model := harness.createModel(t, kube, reachableModelURL(t, origin), nil)
		before := model.DeepCopy()
		model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
		require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(before)))
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-credentials-", Namespace: "kagent", Labels: harness.labels()},
			Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly."},
		}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task status: %+v", task.Status)
		// Claude Code also calls the model host's /api routes, some without a key.
		requireEgressCredential(t, received.Requests(), header, wantCredential, modelAPIRoute)
	})
}

func TestMCPCredentialDelivery(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		testMCPCredentialDelivery(t, harness, harness.name, "")
	})
}

// With KAGENT_PROPAGATE_TOKEN, Claude reaches MCP servers through a loopback
// forwarder. A server whose Authorization is Secret-backed owns it: the
// upstream gets the Secret on a turn without a caller credential and on a turn
// with one, never the caller's token.
func TestClaudePropagatingMCPCredentialDelivery(t *testing.T) {
	t.Parallel()
	interactionTarget(t)
	propagating := cloneHarness(t, interactionKubeClient(t), claudeE2EHarness, "claude-propagate-", func(harness *v1alpha3.Harness) {
		harness.Spec.Env = append(harness.Spec.Env, v1alpha3.RuntimeEnvVar{Name: "KAGENT_PROPAGATE_TOKEN", Value: "true"})
	})
	testMCPCredentialDelivery(t, testHarness{name: claudeE2EHarness, runtimeLabel: "claude"}, propagating.Name, "", "Bearer e2e-caller-token")
}

// testMCPCredentialDelivery sends one turn per caller credential, each in its
// own session; an empty credential sends none.
func testMCPCredentialDelivery(t *testing.T, harness testHarness, harnessName string, callerCredentials ...string) {
	t.Helper()
	const credential = "Bearer mcp-egress-e2e-token"
	target := interactionTarget(t)
	secret := createCredentialSecret(t, interactionKubeClient(t), credential)
	mcpURL, mcpServer := startMCPMock(t)
	headersFrom := []v1alpha3.ValueRef{{
		Name:      "Authorization",
		ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: secret.Name, Key: "token"},
	}}
	template, _ := createMCPServerInteractionTemplate(t, harness, harnessName, mcpURL, headersFrom, false)
	for _, callerCredential := range callerCredentials {
		fixture := newInteractionFixtureForHarnessTemplate(t, target, harnessName, template)
		if callerCredential != "" {
			fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, "authorization", callerCredential)
		}
		_, _, task := fixture.send(t, "Add 3 and 5 using the configured MCP server.")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "caller credential %q: task text: %q", callerCredential, taskText(task))
		require.Contains(t, taskText(task), "result is 8")
	}
	requireEgressCredential(t, mcpServer.Requests(), "Authorization", credential, everyRoute)
}

// Every runtime fetches skills and plugins with git at golden boot, and git
// sends no Authorization before a challenge. The binding is the golden boot's:
// a session's sandbox reaches the source's host, but the gateway replaces no
// header of its requests.
func TestGitArtifactCredentialDelivery(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		// The Secret holds the base64 of "<username>:<token>"; the binding
		// prefixes it with the Basic scheme.
		credential := base64.StdEncoding.EncodeToString([]byte("x-access-token:git-egress-e2e-token"))
		wantAuthorization := "Basic " + credential
		secret := createCredentialSecret(t, kube, credential)
		repositories := newGitFixture(t, wantAuthorization)
		server := repositories.serveThroughEgress(t)
		credentialRef := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}
		gitSource := func(repository, path string) v1alpha3.ArtifactSource {
			return v1alpha3.ArtifactSource{
				Git:  &v1alpha3.GitArtifact{URL: server + "/" + repository, Commit: repositories.commit, CredentialRef: credentialRef},
				Path: path,
			}
		}
		model := harness.createModel(t, kube, startGitSourceRequestMock(t, server+"/"+gitSkillRepository), nil)
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "git-credentials-", Namespace: "kagent", Labels: harness.labels()},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
				SystemPrompt: "Reply briefly.",
				Skills:       []v1alpha3.AgentTemplateSkill{{Name: gitFixtureSkill, Source: gitSource(gitSkillRepository, "skills/"+gitFixtureSkill)}},
				Plugins:      []v1alpha3.PluginBundle{{Source: gitSource(gitPluginRepository, "plugin"), Skills: []string{gitFixturePluginSkill}}},
			},
		}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		ready := time.Now()
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task text: %q", taskText(task))
		for _, repository := range []string{gitSkillRepository, gitPluginRepository} {
			requireEgressCredential(t, receivedBefore(repositories.received(repository), ready), "Authorization", wantAuthorization, everyRoute)
		}

		sandbox := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task = sandbox.send(t, gitSourceRequestPrompt)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task text: %q", taskText(task))
		require.Contains(t, taskText(task), "SANDBOX_SOURCE_REQUESTED")
		require.NoError(t, sandboxSourceRequestViolation(receivedSince(repositories.received(gitSkillRepository), ready), credential))
	})
}

const gitSourceRequestPrompt = "Request the private skill source from the sandbox."

//go:embed mocks/invoke_git_source_request.json
var gitSourceRequestMock string

// startGitSourceRequestMock serves the interaction mock plus a turn in which
// the sandbox's shell tool requests source twice with git: once as git does
// before a challenge, without Authorization, and once with the header the
// golden boot sends, the placeholder the gateway replaces where it binds a
// credential.
func startGitSourceRequestMock(t *testing.T, source string) string {
	t.Helper()
	config, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
	require.NoError(t, err)
	const name = "invoke_git_source_request.json"
	request, err := mockllm.LoadConfigFromFile(name, fstest.MapFS{name: {Data: []byte(strings.ReplaceAll(gitSourceRequestMock, "{{SOURCE_URL}}", source))}})
	require.NoError(t, err)
	config.OpenAI = append(config.OpenAI, request.OpenAI...)
	config.OpenAIResponse = append(config.OpenAIResponse, request.OpenAIResponse...)
	config.Anthropic = append(config.Anthropic, request.Anthropic...)
	return reachableModelURL(t, startMockLLMConfig(t, config))
}

func receivedBefore(received []mockmcp.RecordedRequest, at time.Time) []mockmcp.RecordedRequest {
	return slices.DeleteFunc(received, func(request mockmcp.RecordedRequest) bool { return !request.ReceivedAt.Before(at) })
}

func receivedSince(received []mockmcp.RecordedRequest, at time.Time) []mockmcp.RecordedRequest {
	return slices.DeleteFunc(received, func(request mockmcp.RecordedRequest) bool { return request.ReceivedAt.Before(at) })
}

// sandboxSourceRequestViolation reports the first request of a session's
// sandbox that the gateway gave the golden boot's credential: every request
// reached the source (a denied one is never recorded), a request without
// Authorization arrived without it, and one with the placeholder arrived with
// the placeholder, not the Secret value.
func sandboxSourceRequestViolation(received []mockmcp.RecordedRequest, credential string) error {
	placeholder := "Basic " + translator.CredentialPlaceholder
	var bare, unreplaced int
	for i, request := range received {
		where := fmt.Sprintf("request %d of %d (%s %s)", i+1, len(received), request.Method, request.Path)
		for name, values := range request.Headers {
			for _, value := range values {
				if strings.Contains(value, credential) {
					return fmt.Errorf("%s: header %s carries the golden boot's credential", where, name)
				}
			}
		}
		switch values := request.Headers.Values("Authorization"); {
		case len(values) == 0:
			bare++
		case len(values) == 1 && values[0] == placeholder:
			unreplaced++
		default:
			return fmt.Errorf("%s: Authorization = %q, want none or the placeholder the sandbox sent", where, values)
		}
	}
	if bare == 0 || unreplaced == 0 {
		return fmt.Errorf("the source received %d requests without Authorization and %d with the placeholder, want both", bare, unreplaced)
	}
	return nil
}

func requireEgressCredential(t *testing.T, received []mockmcp.RecordedRequest, header, credential string, authenticated func(path string) bool) {
	t.Helper()
	require.NoError(t, egressCredentialViolation(received, header, credential, authenticated))
}

// egressCredentialViolation reports the first request that breaks the
// contract: at least one request on an authenticated route, each with the
// credential as the only value of header, and no request with the
// placeholder in any header.
func egressCredentialViolation(received []mockmcp.RecordedRequest, header, credential string, authenticated func(path string) bool) error {
	var routed int
	for i, request := range received {
		where := fmt.Sprintf("request %d of %d (%s %s)", i+1, len(received), request.Method, request.Path)
		for name, values := range request.Headers {
			for _, value := range values {
				if strings.Contains(value, translator.CredentialPlaceholder) {
					return fmt.Errorf("%s: header %s carries the placeholder", where, name)
				}
			}
		}
		if !authenticated(request.Path) {
			continue
		}
		routed++
		if values := request.Headers.Values(header); len(values) != 1 || values[0] != credential {
			return fmt.Errorf("%s: %s = %q, want only the Secret value", where, header, values)
		}
	}
	if routed == 0 {
		return errors.New("no authenticated request reached the upstream")
	}
	return nil
}

func everyRoute(string) bool { return true }

func modelAPIRoute(path string) bool { return strings.HasPrefix(path, "/v1/") }

func createCredentialSecret(t *testing.T, kube ctrlclient.Client, value string) *corev1.Secret {
	t.Helper()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-credential-", Namespace: "kagent"}, StringData: map[string]string{"token": value}}
	require.NoError(t, kube.Create(t.Context(), secret))
	t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), secret)) })
	return secret
}

// requestRecorder keeps every request an upstream receives, without its body.
type requestRecorder struct {
	mu       sync.Mutex
	requests []mockmcp.RecordedRequest
}

func (r *requestRecorder) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		r.mu.Lock()
		r.requests = append(r.requests, mockmcp.RecordedRequest{
			Method: request.Method, Path: request.URL.Path, Headers: request.Header.Clone(), ReceivedAt: time.Now(),
		})
		r.mu.Unlock()
		next.ServeHTTP(w, request)
	})
}

func (r *requestRecorder) Requests() []mockmcp.RecordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mockmcp.RecordedRequest(nil), r.requests...)
}

// startCredentialMock serves the mock LLM routes itself rather than behind a
// proxy: a second streaming connection could fail independently of
// credential delivery. A request without the credential matches no mock.
func startCredentialMock(t *testing.T, header, value string) (string, *requestRecorder) {
	t.Helper()
	config, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
	require.NoError(t, err)
	match := mockllm.HeaderMatch{Name: header, Value: value, MatchType: mockllm.MatchTypeExact}
	for i := range config.OpenAI {
		config.OpenAI[i].Match.Headers = append(config.OpenAI[i].Match.Headers, match)
	}
	for i := range config.OpenAIResponse {
		config.OpenAIResponse[i].Match.Headers = append(config.OpenAIResponse[i].Match.Headers, match)
	}
	for i := range config.Anthropic {
		config.Anthropic[i].Match.Headers = append(config.Anthropic[i].Match.Headers, match)
	}
	routes := http.NewServeMux()
	routes.HandleFunc("POST /v1/chat/completions", mockllm.NewOpenAIProvider(config.OpenAI).Handle)
	routes.HandleFunc("POST /v1/responses", mockllm.NewOpenAIResponseProvider(config.OpenAIResponse).Handle)
	routes.HandleFunc("POST /v1/messages", mockllm.NewAnthropicProvider(config.Anthropic).Handle)
	received := &requestRecorder{}
	return startHostServer(t, received.record(routes)).URL, received
}

// startHostServer listens on every interface so runtimes reach the server
// through the host address.
func startHostServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	require.NoError(t, server.Listener.Close())
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

const (
	gitSkillRepository    = "skill.git"
	gitPluginRepository   = "plugin.git"
	gitFixtureSkill       = "egress-skill"
	gitFixturePluginSkill = "egress-plugin-skill"
)

// gitFixture serves smart-HTTP repositories through git http-backend and
// answers 401 to any request without the expected Authorization, as a private
// forge does. Every repository starts on the same commit on main: a standalone
// skill under skills/ and an Agent Plugins package under plugin/.
type gitFixture struct {
	commit       string
	repositories string
	handler      http.Handler
	recorders    map[string]*requestRecorder
}

func newGitFixture(t *testing.T, authorization string) *gitFixture {
	t.Helper()
	return newGitFixtureOf(t, authorization, gitSkillRepository, gitPluginRepository)
}

// newGitFixtureOf serves the named bare repositories, each a path ending in
// .git such as "owner/name.git".
func newGitFixtureOf(t *testing.T, authorization string, names ...string) *gitFixture {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err, "the git fixture needs git and its http-backend")
	root := t.TempDir()
	work, repositories := filepath.Join(root, "work"), filepath.Join(root, "repositories")
	for name, content := range map[string]string{
		"skills/" + gitFixtureSkill + "/SKILL.md":              "---\nname: " + gitFixtureSkill + "\ndescription: Fetched through the egress gateway.\n---\n",
		"plugin/plugin.json":                                   `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"egress.e2e"}`,
		"plugin/skills/" + gitFixturePluginSkill + "/SKILL.md": "---\nname: " + gitFixturePluginSkill + "\ndescription: Fetched through the egress gateway.\n---\n",
	} {
		path := filepath.Join(work, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	runFixtureGit(t, work, "init", "--quiet", "--initial-branch=main")
	runFixtureGit(t, work, "add", ".")
	runFixtureGit(t, work, "-c", "user.name=kagent e2e", "-c", "user.email=e2e@kagent.dev", "commit", "--quiet", "--message", "Egress credential fixture")
	fixture := &gitFixture{commit: strings.TrimSpace(runFixtureGit(t, work, "rev-parse", "HEAD")), repositories: repositories, recorders: map[string]*requestRecorder{}}
	for _, repository := range names {
		bare := filepath.Join(repositories, filepath.FromSlash(repository))
		runFixtureGit(t, root, "clone", "--quiet", "--bare", work, bare)
		// The runtime fetches its pinned commit by ID, not by ref.
		runFixtureGit(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
		fixture.recorders[repository] = &requestRecorder{}
	}
	backend := &cgi.Handler{
		Path: gitPath, Args: []string{"http-backend"}, Dir: root,
		Env: append(isolatedGitEnvironment(), "GIT_PROJECT_ROOT="+repositories, "GIT_HTTP_EXPORT_ALL=1"),
	}
	fixture.handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		recorder := fixture.recorders[gitRepositoryOf(request.URL.Path)]
		if recorder == nil {
			http.NotFound(w, request)
			return
		}
		recorder.record(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != authorization {
				w.Header().Set("WWW-Authenticate", `Basic realm="kagent-e2e"`)
				http.Error(w, "credential required", http.StatusUnauthorized)
				return
			}
			backend.ServeHTTP(w, request)
		})).ServeHTTP(w, request)
	})
	return fixture
}

// gitRepositoryOf returns the repository a smart-HTTP request path names: the
// path up to its first segment ending in .git.
func gitRepositoryOf(path string) string {
	path = strings.TrimPrefix(path, "/")
	end := strings.Index(path+"/", ".git/")
	if end < 0 {
		return ""
	}
	return path[:end+len(".git")]
}

func (f *gitFixture) received(repository string) []mockmcp.RecordedRequest {
	return f.recorders[repository].Requests()
}

// head returns the commit main of repository points at.
func (f *gitFixture) head(t *testing.T, repository string) string {
	t.Helper()
	return strings.TrimSpace(runFixtureGit(t, filepath.Join(f.repositories, filepath.FromSlash(repository)), "rev-parse", "main"))
}

// push commits path with content on top of main of repository, as a push to
// the forge would, and returns the new head.
func (f *gitFixture) push(t *testing.T, repository, path, content string) string {
	t.Helper()
	bare, work := filepath.Join(f.repositories, filepath.FromSlash(repository)), t.TempDir()
	runFixtureGit(t, work, "clone", "--quiet", bare, ".")
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(work, filepath.FromSlash(path))), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, filepath.FromSlash(path)), []byte(content), 0o644))
	runFixtureGit(t, work, "add", ".")
	runFixtureGit(t, work, "-c", "user.name=kagent e2e", "-c", "user.email=e2e@kagent.dev", "commit", "--quiet", "--message", "Change "+path)
	runFixtureGit(t, work, "push", "--quiet", "origin", "HEAD:main")
	return f.head(t, repository)
}

// serveThroughEgress serves the fixture over HTTPS on the host under a cluster
// Service name, which a credential binding can match, and returns its base
// URL. The egress gateway dials the upstream itself, so the certificate comes
// from the CA it trusts there (atenetEgress.upstreamTrust).
func (f *gitFixture) serveThroughEgress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	base, err := url.Parse(reachableServerURL(t, "https://127.0.0.1:"+port, ""))
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(f.handler)
	require.NoError(t, server.Listener.Close())
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{egressUpstreamCertificate(t, base.Hostname())}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return base.String()
}

func egressUpstreamCertificate(t *testing.T, hostname string) tls.Certificate {
	t.Helper()
	certificateFile, keyFile := kagentenv.E2EEgressCAFile.Get(), kagentenv.E2EEgressCAKeyFile.Get()
	if certificateFile == "" || keyFile == "" {
		t.Fatalf("%s and %s must name the CA that Substrate's egress gateway trusts on upstream HTTPS", kagentenv.E2EEgressCAFile.Name(), kagentenv.E2EEgressCAKeyFile.Name())
	}
	authority, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	require.NoError(t, err)
	return issueServerCertificate(t, authority, hostname)
}

func issueServerCertificate(t *testing.T, authority tls.Certificate, hostname string) tls.Certificate {
	t.Helper()
	issuer, err := x509.ParseCertificate(authority.Certificate[0])
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	require.NoError(t, err)
	now := time.Now()
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: hostname}, DNSNames: []string{hostname},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, issuer, &key.PublicKey, authority.PrivateKey)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der, authority.Certificate[0]}, PrivateKey: key}
}

// isolatedGitEnvironment keeps the runner's git configuration, such as the
// credential header actions/checkout leaves in it, out of fixture commands.
func isolatedGitEnvironment() []string {
	return []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
}

func runFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), isolatedGitEnvironment()...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), output)
	return string(output)
}

func TestGitFixtureRequiresCredential(t *testing.T) {
	const authorization = "Basic Zml4dHVyZTpjcmVkZW50aWFs"
	fixture := newGitFixture(t, authorization)
	server := httptest.NewServer(fixture.handler)
	t.Cleanup(server.Close)
	for _, test := range []struct {
		name, header string
		wantFetch    bool
	}{
		{name: "missing"},
		{name: "placeholder", header: "Basic " + translator.CredentialPlaceholder},
		{name: "credential", header: authorization, wantFetch: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := t.TempDir()
			runFixtureGit(t, destination, "init", "--quiet")
			args := []string{"fetch", "--depth", "1", server.URL + "/" + gitSkillRepository, fixture.commit}
			if test.header != "" {
				args = append([]string{"-c", "http.extraHeader=Authorization: " + test.header}, args...)
			}
			command := exec.CommandContext(t.Context(), "git", args...)
			command.Dir = destination
			command.Env = append(os.Environ(), isolatedGitEnvironment()...)
			output, err := command.CombinedOutput()
			if !test.wantFetch {
				require.Error(t, err, "%s", output)
				return
			}
			require.NoError(t, err, "%s", output)
			runFixtureGit(t, destination, "checkout", "--quiet", "--detach", "FETCH_HEAD")
			_, err = os.Stat(filepath.Join(destination, "plugin", "skills", gitFixturePluginSkill, "SKILL.md"))
			require.NoError(t, err)
		})
	}
	received := fixture.received(gitSkillRepository)
	require.NotEmpty(t, received)
	require.Empty(t, fixture.received(gitPluginRepository))
}

func TestCredentialMockRequiresHeader(t *testing.T) {
	for _, provider := range []struct {
		name, path, header, value, body, terminal string
		missingStatus                             int
	}{
		{
			name: "chat completions", path: "/v1/chat/completions", header: "Authorization", value: "Bearer credential-test",
			body: `{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"What is 2+2?"}],"stream":true}`, terminal: "data: [DONE]",
			missingStatus: http.StatusNotFound,
		},
		{
			name: "responses", path: "/v1/responses", header: "Authorization", value: "Bearer credential-test",
			body: `{"model":"gpt-5.2-codex","input":"What is 2+2?","stream":true}`, terminal: "event: response.completed",
			missingStatus: http.StatusNotFound,
		},
		{
			name: "anthropic", path: "/v1/messages", header: "X-Api-Key", value: "credential-test",
			body: `{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"What is 2+2?"}]}],"stream":true}`, terminal: "event: message_stop",
			missingStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(provider.name, func(t *testing.T) {
			origin, received := startCredentialMock(t, provider.header, provider.value)
			tests := []struct {
				name, value string
				wantStatus  int
			}{
				{name: "missing", wantStatus: provider.missingStatus},
				{name: "wrong", value: provider.value + "-wrong", wantStatus: http.StatusNotFound},
				{name: "correct", value: provider.value, wantStatus: http.StatusOK},
			}
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+provider.path, strings.NewReader(provider.body))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					if provider.name == "anthropic" {
						request.Header.Set("Anthropic-Version", "2023-06-01")
					}
					if test.value != "" {
						request.Header.Set(provider.header, test.value)
					}
					response, err := http.DefaultClient.Do(request)
					require.NoError(t, err)
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.Equal(t, test.wantStatus, response.StatusCode, "%s", body)
					if test.wantStatus == http.StatusOK {
						require.Contains(t, string(body), provider.terminal)
					}
				})
			}
			require.Len(t, received.Requests(), len(tests))
		})
	}
}

func TestEgressCredentialViolation(t *testing.T) {
	const credential = "Bearer secret"
	placeholder := "Bearer " + translator.CredentialPlaceholder
	request := func(path string, headers http.Header) mockmcp.RecordedRequest {
		return mockmcp.RecordedRequest{Method: http.MethodPost, Path: path, Headers: headers}
	}
	for _, test := range []struct {
		name          string
		received      []mockmcp.RecordedRequest
		authenticated func(string) bool
		wantErr       string
	}{
		{name: "replaced", received: []mockmcp.RecordedRequest{request("/v1/messages", http.Header{"Authorization": {credential}})}},
		{name: "no request", wantErr: "no authenticated request reached the upstream"},
		{
			name:     "header never sent",
			received: []mockmcp.RecordedRequest{request("/v1/messages", http.Header{"Authorization": {credential}}), request("/v1/messages", http.Header{})},
			wantErr:  "request 2 of 2 (POST /v1/messages)",
		},
		{name: "placeholder kept", received: []mockmcp.RecordedRequest{request("/v1/messages", http.Header{"Authorization": {placeholder}})}, wantErr: "carries the placeholder"},
		{
			name:     "credential appended",
			received: []mockmcp.RecordedRequest{request("/v1/messages", http.Header{"Authorization": {credential, credential}})},
			wantErr:  "want only the Secret value",
		},
		{
			name:     "placeholder in another header",
			received: []mockmcp.RecordedRequest{request("/v1/messages", http.Header{"Authorization": {credential}, "X-Api-Key": {placeholder}})},
			wantErr:  "header X-Api-Key carries the placeholder",
		},
		{
			name:          "unauthenticated route without the header",
			received:      []mockmcp.RecordedRequest{request("/api/hello", http.Header{}), request("/v1/messages", http.Header{"Authorization": {credential}})},
			authenticated: modelAPIRoute,
		},
		{
			name:          "unauthenticated route with the placeholder",
			received:      []mockmcp.RecordedRequest{request("/api/hello", http.Header{"Authorization": {placeholder}}), request("/v1/messages", http.Header{"Authorization": {credential}})},
			authenticated: modelAPIRoute,
			wantErr:       "request 1 of 2 (POST /api/hello): header Authorization carries the placeholder",
		},
		{
			name:          "only unauthenticated routes",
			received:      []mockmcp.RecordedRequest{request("/api/hello", http.Header{})},
			authenticated: modelAPIRoute,
			wantErr:       "no authenticated request reached the upstream",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			authenticated := test.authenticated
			if authenticated == nil {
				authenticated = everyRoute
			}
			err := egressCredentialViolation(test.received, "Authorization", credential, authenticated)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestSandboxSourceRequestViolation(t *testing.T) {
	const credential = "c2VjcmV0"
	placeholder := "Basic " + translator.CredentialPlaceholder
	request := func(headers http.Header) mockmcp.RecordedRequest {
		return mockmcp.RecordedRequest{Method: http.MethodGet, Path: "/skill.git/info/refs", Headers: headers}
	}
	bare, unreplaced := request(http.Header{}), request(http.Header{"Authorization": {placeholder}})
	for _, test := range []struct {
		name     string
		received []mockmcp.RecordedRequest
		wantErr  string
	}{
		{name: "neither replaced", received: []mockmcp.RecordedRequest{bare, unreplaced, bare}},
		{name: "credential injected", received: []mockmcp.RecordedRequest{bare, request(http.Header{"Authorization": {"Basic " + credential}})}, wantErr: "request 2 of 2 (GET /skill.git/info/refs): header Authorization carries the golden boot's credential"},
		{name: "credential in another header", received: []mockmcp.RecordedRequest{bare, unreplaced, request(http.Header{"X-Token": {credential}})}, wantErr: "header X-Token carries"},
		{name: "another Authorization", received: []mockmcp.RecordedRequest{bare, request(http.Header{"Authorization": {"Bearer other"}})}, wantErr: "want none or the placeholder"},
		{name: "placeholder request denied", received: []mockmcp.RecordedRequest{bare}, wantErr: "1 requests without Authorization and 0 with the placeholder"},
		{name: "nothing reached the source", wantErr: "0 requests without Authorization and 0 with the placeholder"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := sandboxSourceRequestViolation(test.received, credential)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

// The mock's turn compiles for every harness's model protocol with the source
// in each shell command.
func TestGitSourceRequestMockNamesTheSource(t *testing.T) {
	const source = "https://git.example.svc.cluster.local:8443/skill.git"
	const name = "invoke_git_source_request.json"
	config, err := mockllm.LoadConfigFromFile(name, fstest.MapFS{name: {Data: []byte(strings.ReplaceAll(gitSourceRequestMock, "{{SOURCE_URL}}", source))}})
	require.NoError(t, err)
	require.Len(t, config.OpenAI, 2)
	require.Len(t, config.OpenAIResponse, 2)
	require.Len(t, config.Anthropic, 3)
	require.Equal(t, 6, strings.Count(gitSourceRequestMock, "{{SOURCE_URL}}"), "two git requests in each of three tool calls")
}
