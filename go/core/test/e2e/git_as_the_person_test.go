package e2e_test

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed mocks/invoke_git_as_the_person.json
var gitAsThePersonMocks embed.FS

// The forge a Session works on as the person: its git and API on one host
// that verifies the Session Actor's own JWT, the provider instance the
// controller configures for the test (KAGENT_SESSION_ACTOR_JWT_PROVIDERS), and
// the same forge under a second name, granted as a provider whose credential
// the egress gateway cannot resolve: no credential provider of the gateway
// answers the authority the controller names (KAGENT_SESSION_CREDENTIAL_AUTHORITY),
// as none answers for a person who has not signed in. The probe of
// invoke_git_as_the_person.json names both hosts.
const (
	forgeService         = "e2e-forge"
	forgeRefusedService  = "e2e-forge-refused"
	forgeHost            = forgeService + ".kagent.svc.cluster.local"
	forgeRefusedHost     = forgeRefusedService + ".kagent.svc.cluster.local"
	forgeProvider        = "e2e-forge"
	forgeAudience        = "https://e2e-forge.kagent.dev"
	forgeRefusedProvider = "e2e-forge-refused"
	forgeAuthority       = "credentials.e2e.kagent.dev"
	// gitAsThePersonCaller is the person of the test's turns, the subject of
	// the token trusted-proxy authentication reads.
	gitAsThePersonCaller = "alice@example.com"
	gitAsThePersonPrompt = "Work on the repository as the person."
	gitAsThePersonDone   = "GIT_AS_THE_PERSON_DONE"
)

// forgeRequest is a request the forge received: the host it was sent to and
// the Authorization it carried.
type forgeRequest struct {
	Host, Method, Path, Authorization string
	ReceivedAt                        time.Time
}

// forge serves the workspace repositories over smart HTTP, push included,
// and GitHub's REST API under /api/v3, as a GitHub Enterprise host does. It
// accepts the workspace sync's Basic credential on git, and on both the
// Actor's JWT bound to forgeAudience, whose subject the API answers as the
// login; it records every request.
type forge struct {
	*gitFixture
	sync     string
	mu       sync.Mutex
	received []forgeRequest
}

func newForge(t *testing.T) *forge {
	t.Helper()
	f := &forge{sync: "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+workspaceGitToken))}
	f.gitFixture = newGitFixtureAuthorizing(t, func(request *http.Request) bool {
		authorization := request.Header.Get("Authorization")
		_, actor := forgeActor(authorization)
		return authorization == f.sync || actor
	}, workspaceOriginPaths()...)
	for _, repository := range workspaceOriginPaths() {
		runFixtureGit(t, filepath.Join(f.repositories, filepath.FromSlash(repository)), "config", "http.receivepack", "true")
	}
	return f
}

func (f *forge) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	f.received = append(f.received, forgeRequest{
		Host: strings.ToLower(request.Host), Method: request.Method, Path: request.URL.Path,
		Authorization: request.Header.Get("Authorization"), ReceivedAt: time.Now(),
	})
	f.mu.Unlock()
	if request.URL.Path != "/api/v3/user" {
		f.handler.ServeHTTP(w, request)
		return
	}
	subject, actor := forgeActor(request.Header.Get("Authorization"))
	if !actor {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"login": subject})
}

// requestsSince are the requests to host received at or after at.
func (f *forge) requestsSince(host string, at time.Time) []forgeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(f.received), func(request forgeRequest) bool {
		return request.Host != host || request.ReceivedAt.Before(at)
	})
}

// author is the author and committer of branch's head in repository.
func (f *forge) author(t *testing.T, repository, branch string) string {
	t.Helper()
	return strings.TrimSpace(runFixtureGit(t, filepath.Join(f.repositories, filepath.FromSlash(repository)), "log", "-1", "--format=%ae %ce", "refs/heads/"+branch))
}

// serve serves the forge over HTTPS on the test host under its two Service
// names, the hosts the Sessions reach it at, and returns its git base URL.
func (f *forge) serve(t *testing.T) string {
	t.Helper()
	listener, _ := listenOnHost(t)
	serveTLSThroughEgress(t, listener, f, forgeHost, forgeRefusedHost)
	kube := interactionKubeClient(t)
	for _, service := range []string{forgeService, forgeRefusedService} {
		require.NoError(t, exposeOnService(t.Context(), kube, service, 443, listener))
		t.Cleanup(func() {
			_ = kube.Delete(t.Context(), &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: service, Namespace: "kagent"}})
		})
	}
	return "https://" + forgeHost
}

// forgeActor returns the subject of an Authorization that carries a JWT
// bound to forgeAudience for a Substrate Actor, as the egress gateway mints
// it. The forge trusts the gateway and reads the claims without verifying
// the signature.
func forgeActor(authorization string) (string, bool) {
	token, bearer := strings.CutPrefix(authorization, "Bearer ")
	parts := strings.Split(token, ".")
	if !bearer || len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		Subject  string          `json:"sub"`
		Audience json.RawMessage `json:"aud"`
	}
	if json.Unmarshal(payload, &claims) != nil || !strings.HasPrefix(claims.Subject, "actor/") {
		return "", false
	}
	var audiences []string
	if json.Unmarshal(claims.Audience, &audiences) != nil {
		var audience string
		if json.Unmarshal(claims.Audience, &audience) != nil {
			return "", false
		}
		audiences = []string{audience}
	}
	return claims.Subject, slices.Contains(audiences, forgeAudience)
}

// requireActorCredential fails unless every request among received that
// carries an Authorization carries the JWT of the Session's Actor for the
// forge's audience, never the placeholder the sandbox sent, and some
// request to path within received carries it.
func requireActorCredential(t *testing.T, received []forgeRequest, sessionID, path string) {
	t.Helper()
	actor := "/" + substrate.ActorName(sessionID)
	reached := false
	for _, request := range received {
		if request.Authorization == "" {
			continue
		}
		require.NotContains(t, request.Authorization, workspaceCredentialPlaceholder, "%s %s carried the sandbox's placeholder", request.Method, request.Path)
		subject, ok := forgeActor(request.Authorization)
		require.True(t, ok, "%s %s carried no actor JWT for %s", request.Method, request.Path, forgeAudience)
		require.True(t, strings.HasSuffix(subject, actor), "%s %s carried the JWT of %s, not of the Session's Actor %s", request.Method, request.Path, subject, actor)
		reached = reached || strings.Contains(request.Path, path)
	}
	require.True(t, reached, "no request to %s carried the Actor's JWT", path)
}

// workspaceCredentialPlaceholder is the inert credential a workspace turn's
// git and gh send (workspace.CredentialPlaceholder).
const workspaceCredentialPlaceholder = "kagent-credential-injected"

// gitAsThePersonProbe runs the probe turn of invoke_git_as_the_person.json in
// session and returns its output as key=value lines.
func gitAsThePersonProbe(t *testing.T, session *interactionFixture, shellTool string) map[string]string {
	t.Helper()
	_, _, task := session.send(t, gitAsThePersonPrompt)
	output := toolOutput(t, getTask(t, session, task.ID), shellTool)
	t.Logf("session %s probe output:\n%s", session.sessionID, output)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task %q", taskText(task))
	require.Contains(t, taskText(task), gitAsThePersonDone)
	probed := map[string]string{}
	for line := range strings.Lines(output) {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			probed[key] = value
		}
	}
	require.Equal(t, "done", probed["probe"], "probe output: %s", output)
	return probed
}

// toolOutput is the text the task's one call of toolName returned: the string
// values of its function_response's response.
func toolOutput(t *testing.T, task *a2atype.Task, toolName string) string {
	t.Helper()
	var outputs []string
	for _, part := range taskParts(task) {
		if partType, _ := part.Metadata[apia2a.PartTypeMetadataKey].(string); partType != "function_response" {
			continue
		}
		if data, ok := part.Data().(map[string]any); ok && data["name"] == toolName {
			outputs = append(outputs, strings.Join(stringValues(data["response"]), "\n"))
		}
	}
	require.Len(t, outputs, 1, "function_response parts of %s in task %s", toolName, task.ID)
	return outputs[0]
}

// stringValues lists the strings in a decoded JSON value, depth first.
func stringValues(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		var values []string
		for _, item := range value {
			values = append(values, stringValues(item)...)
		}
		return values
	case map[string]any:
		var values []string
		for _, key := range slices.Sorted(maps.Keys(value)) {
			values = append(values, stringValues(value[key])...)
		}
		return values
	}
	return nil
}

// A Declarative agent on the kagent Harness, and the same on the claude
// Harness, works on a private repository as the person who sent the turn,
// through the shell tool in its Session's directory: git fetches and pushes
// and gh calls the forge's API with the person's credential, which the egress
// gateway sets on the forge's host (the Session Actor's JWT for the forge's
// audience) in place of the placeholder git and gh send; the sandbox's
// environment, /data and the Session's directory hold no JWT, and the commit
// is the person's. A caller whose credential the gateway cannot resolve gets
// the gateway's refusal in the tool result, and a Session of the same Agent
// without a grant sends no credential and gets the gateway's 403. Each Session
// writes only its own directory and never the mirrors.
//
// It stays sequential: it rolls the controller onto trusted-proxy
// authentication, which resolves the person of a turn, and onto the forge's
// providers.
func TestGitAsThePersonKagentHarness(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	withControllerEnv(t, target, map[string]string{
		kagentenv.AuthMode.Name():                   kagentenv.AuthModeTrustedProxy,
		kagentenv.SessionActorJWTProviders.Name():   fmt.Sprintf(`{%q:{"hostname":%q,"audience":%q}}`, forgeProvider, forgeHost, forgeAudience),
		kagentenv.SessionCredentialAuthority.Name(): forgeAuthority,
	})
	caller := []string{"authorization", "Bearer " + unsignedToken(t, map[string]any{"sub": gitAsThePersonCaller})}

	origin := newForge(t)
	fixture := newWorkspaceFixtureFrom(t, origin.gitFixture, origin.serve(t), origin.sync)
	// The turn fetches; the Session's setup fetching changed repositories is
	// TestGoADKSessionWorkspace's.
	fixture.Changed = nil
	modelURL := reachableModelURL(t, startMockLLMServer(t, gitAsThePersonMocks, "mocks/invoke_git_as_the_person.json"))

	own := map[string]string{}
	for _, harness := range []struct {
		testHarness
		shellTool string
		// identity is false where the Harness sets no git identity of the
		// turn's person, so git's own error stands and the push carries the
		// fetched commit.
		identity bool
	}{
		{testHarness{name: "kagent", runtimeLabel: "kagent"}, "bash", true},
		{testHarness{name: claudeE2EHarness, runtimeLabel: "claude"}, "Bash", false},
	} {
		t.Run(harness.runtimeLabel, func(t *testing.T) {
			model := harness.createModel(t, kube, modelURL, nil)
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "git-as-the-person-", Namespace: "kagent", Labels: harness.labels()},
				Spec: v1alpha3.AgentTemplateSpec{
					ModelConfig:  &corev1.LocalObjectReference{Name: model.Name},
					SystemPrompt: "Work on the repository with the shell tool.",
				},
			}
			createAndWaitInteractionTemplate(t, harness.testHarness, kube, template)
			session := func(t *testing.T, configure func(*apiv1alpha1.CreateSessionRequest)) *interactionFixture {
				t.Helper()
				created := newInteractionFixtureAs(t, target, template.Name, caller, configure)
				created.retryWindow = workspaceSendRetryWindow
				return created
			}

			t.Run("as the person", func(t *testing.T) {
				granted := session(t, fixture.admit(fixture.grantFor(t, gitAsThePersonCaller)))
				started := time.Now()
				probed := gitAsThePersonProbe(t, granted, harness.shellTool)
				require.Equal(t, "/workspace", probed["pwd"], "the shell runs in the Session's directory")
				require.Equal(t, workspaceCredentialPlaceholder, probed["gh_token"], "gh holds the placeholder only")
				require.Equal(t, "ok", probed["fetch"])
				require.Equal(t, "ok", probed["push"])
				require.True(t, strings.HasPrefix(probed["gh"], "ok actor/") && strings.HasSuffix(probed["gh"], "/"+substrate.ActorName(granted.sessionID)),
					"gh api user answered as the Session's Actor: %s", probed["gh"])
				requireActorCredential(t, origin.requestsSince(forgeHost, started), granted.sessionID, "git-receive-pack")
				requireActorCredential(t, origin.requestsSince(forgeHost, started), granted.sessionID, "/api/v3/user")
				require.Equal(t, "none", probed["jwt_env"], "the sandbox's environment holds no JWT")
				require.Equal(t, "none", probed["jwt_files"], "/data and the Session's directory hold no JWT")
				require.Equal(t, "read-only", probed["mirrors"])
				if harness.identity {
					require.Equal(t, "ok", probed["commit"])
					require.Equal(t, gitAsThePersonCaller+" "+gitAsThePersonCaller, origin.author(t, "e2e/alpha.git", probed["branch"]), "the pushed commit is the person's")
				} else {
					t.Logf("the %s Harness sets no git identity of the turn's person: commit %s", harness.runtimeLabel, probed["commit"])
				}
				own[granted.sessionID] = probed["own"]
			})

			t.Run("refused credential", func(t *testing.T) {
				refused := session(t, fixture.admit(fixture.grantFor(t, gitAsThePersonCaller,
					grant.Provider{Hostname: forgeRefusedHost, Scheme: "Bearer", Provider: forgeRefusedProvider})))
				started := time.Now()
				probed := gitAsThePersonProbe(t, refused, harness.shellTool)
				require.True(t, strings.HasPrefix(probed["refused_push"], "failed "), "the push to the refused host failed: %s", probed["refused_push"])
				require.True(t, strings.HasPrefix(probed["refused_gh"], "failed "), "gh on the refused host failed: %s", probed["refused_gh"])
				for _, request := range origin.requestsSince(forgeRefusedHost, started) {
					require.Empty(t, request.Authorization, "%s %s reached the forge with a credential", request.Method, request.Path)
				}
				require.Equal(t, "none", probed["jwt_env"])
				require.Equal(t, "none", probed["jwt_files"])
				own[refused.sessionID] = probed["own"]
			})

			t.Run("without a grant", func(t *testing.T) {
				started := time.Now()
				probed := gitAsThePersonProbe(t, session(t, nil), harness.shellTool)
				require.Equal(t, "/data/workspace", probed["pwd"])
				for _, key := range []string{"fetch", "gh"} {
					require.True(t, strings.HasPrefix(probed[key], "failed "), "%s failed: %s", key, probed[key])
					require.Contains(t, probed[key], "403", "%s got the gateway's 403", key)
				}
				require.Empty(t, origin.requestsSince(forgeHost, started), "a Session without a grant reached the forge")
			})
		})
	}

	// Each Session on the volume wrote its own directory alone, and none the
	// mirrors.
	script := "set -eu\n"
	for _, id := range slices.Sorted(maps.Keys(own)) {
		script += fmt.Sprintf("test \"$(cat /volume/sessions/%s/OWN)\" = %s\n", id, shellQuote(own[id]))
	}
	script += "test ! -e /volume/mirrors/written\n" + workspaceMirrorHeadsScript("/volume/mirrors")
	report := runWorkspaceJob(t, kube, fixture.Claim, script, []workspaceMount{{Path: "/volume", ReadOnly: true}}, nil)
	require.Equal(t, fixture.Heads, parseWorkspaceHeads(t, report), "the mirrors are unchanged")
}

// The forge takes a Bearer JWT for its audience whose subject is an Actor,
// with aud a string or a list, and nothing else.
func TestForgeActor(t *testing.T) {
	jwt := func(claims map[string]any) string {
		payload, err := json.Marshal(claims)
		require.NoError(t, err)
		return "Bearer eyJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	}
	const actor = "actor/kagent/session-1"
	for name, test := range map[string]struct {
		authorization, subject string
		ok                     bool
	}{
		"audience list":   {jwt(map[string]any{"sub": actor, "aud": []string{"other", forgeAudience}}), actor, true},
		"audience string": {jwt(map[string]any{"sub": actor, "aud": forgeAudience}), actor, true},
		"other audience":  {jwt(map[string]any{"sub": actor, "aud": "other"}), actor, false},
		"no actor":        {jwt(map[string]any{"sub": "alice@example.com", "aud": forgeAudience}), "", false},
		"placeholder":     {"token " + workspaceCredentialPlaceholder, "", false},
		"basic":           {"Basic eC1hY2Nlc3MtdG9rZW46a2FnZW50LWNyZWRlbnRpYWwtaW5qZWN0ZWQ=", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			subject, ok := forgeActor(test.authorization)
			require.Equal(t, test.ok, ok)
			if ok {
				require.Equal(t, test.subject, subject)
			}
		})
	}
}

// The forge takes a fetch and a push with the Actor's JWT and answers its API
// with the JWT's subject; the sync's credential fetches, anything else is
// refused, and every request is recorded with its host.
func TestForgeServesTheActor(t *testing.T) {
	origin := newForge(t)
	server := httptest.NewServer(origin)
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	claims, err := json.Marshal(map[string]any{"sub": "actor/kagent/session-1", "aud": forgeAudience})
	require.NoError(t, err)
	actor := "Bearer eyJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"

	work := t.TempDir()
	runFixtureGit(t, work, "-c", "http.extraHeader=Authorization: "+actor, "clone", "--quiet", server.URL+"/e2e/alpha.git", ".")
	require.NoError(t, os.WriteFile(filepath.Join(work, "PERSON.md"), []byte("person\n"), 0o644))
	runFixtureGit(t, work, "add", ".")
	runFixtureGit(t, work, "-c", "user.name="+gitAsThePersonCaller, "-c", "user.email="+gitAsThePersonCaller, "commit", "--quiet", "--message", "As the person")
	runFixtureGit(t, work, "-c", "http.extraHeader=Authorization: "+actor, "push", "--quiet", "origin", "HEAD:refs/heads/person")
	require.Equal(t, gitAsThePersonCaller+" "+gitAsThePersonCaller, origin.author(t, "e2e/alpha.git", "person"))
	runFixtureGit(t, t.TempDir(), "-c", "http.extraHeader=Authorization: "+origin.sync, "clone", "--quiet", server.URL+"/e2e/beta.git", ".")

	call := func(authorization string) (int, string) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v3/user", nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", authorization)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return response.StatusCode, string(body)
	}
	status, body := call(actor)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"login":"actor/kagent/session-1"}`, body)
	status, _ = call("token " + workspaceCredentialPlaceholder)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = call(origin.sync)
	require.Equal(t, http.StatusUnauthorized, status, "the sync's credential is git's alone")

	received := origin.requestsSince(host, time.Time{})
	require.NotEmpty(t, received)
	requireActorCredential(t, slices.DeleteFunc(slices.Clone(received), func(request forgeRequest) bool {
		return request.Authorization == origin.sync || strings.HasPrefix(request.Authorization, "token ")
	}), "1", "git-receive-pack")
	require.Empty(t, origin.requestsSince("elsewhere", time.Time{}))
}
