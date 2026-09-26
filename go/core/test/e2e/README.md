# End-to-end tests

The suite exercises the public API against a clean Kind installation. It does
not reconcile Kubernetes resources itself: installation creates the Harness
fixtures, and each test owns the templates and API resources it creates.

Shared AgentTemplate and Session tests use `forEachHarness`, which runs
every case on kagent, Codex, Claude, and configured BYO (the Go ADK image through
the BYO compiler). Each harness gets a named subtest and independent resources,
mock servers, and cleanup. The same assertions run against all model protocols.
The opaque BYO fixture has its own test because it does not consume managed
agent configuration.

Until Substrate supplies runtime credentials (#1660), TaskStore uses a temporary
unsigned identity header in every deployment; no test authentication flag is
needed. Run these prerelease builds in isolated deployments. Verified actor
credentials will replace this path outright.

Add portable tests using this pattern:

```go
func TestAgentTemplateBehavior(t *testing.T) {
    t.Parallel()
    forEachHarness(t, func(t *testing.T, harness testHarness) {
        fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
        // Exercise the public API and assert the behavior.
    })
}
```

There is no supported-harness allowlist for individual cases. When a behavior
cannot run on a harness, call `t.Skip` inside that harness's subtest and explain
the limitation. Use explicit harness names so a newly added harness runs by
default. Missing Harness fixtures, preparation failures, and runtime errors
must fail, not skip. Keep genuinely harness-specific features (native tool
events, SDK tracing, kagent compaction) in separate tests.

Current explicit gaps are structured output outside kagent, native Codex/Claude
ask-user model fixtures, and the Codex shared-subagent model fixture. These
gaps appear as skips in verbose/JSON test output, including in CI.

Run one behavior across all harnesses, or select one harness for debugging:

```bash
go test ./core/test/e2e -run '^TestSessionInteraction$' -v -count=1
go test ./core/test/e2e -run '^TestSessionInteraction$/^codex$' -v -count=1
```

Run these commands from `go/` with `KUBECONFIG` pointing to the test Kind cluster
and `KAGENT_E2E_API_URL` set. The existing CI E2E command runs the whole matrix
without an additional flag. Both runners allow 30 minutes and finish the matrix
after a failure so all harness results are visible. `-parallel` still bounds
concurrent test scenarios; harness subtests run sequentially, and the controller
restart case stays sequential with respect to the rest of the suite.

Render the lifecycle fixtures with the digest-pinned runtime image built for
the test, then run the lifecycle test:

On Kind, install Substrate with the atelet registry rewrite used by its Kind
overlay:

```yaml
atelet:
  extraArgs:
    - --localhost-registry-replacement=kind-registry:5000
```

```bash
KAGENT_E2E_RUNTIME_IMAGE=<registry>/kagent-dev/kagent/golang-adk@sha256:<digest> \
KAGENT_E2E_BYO_IMAGE=<registry>/kagent-dev/kagent/byo-a2a@sha256:<digest> \
KAGENT_E2E_CLAUDE_IMAGE=<registry>/kagent-dev/kagent/claude-harness@sha256:<digest> \
KAGENT_E2E_CODEX_IMAGE=<registry>/kagent-dev/kagent/codex-harness@sha256:<digest> \
  envsubst < go/core/test/e2e/manifests/lifecycle.yaml.tmpl | kubectl apply -f -
KAGENT_E2E_API_URL=http://<controller-address>:8083 make -C go e2e
```

`TestSessionInteraction` starts the deterministic mock LLM on the test
host and translates its listener to the host address reachable from the
cluster (`172.17.0.1` on Linux and `host.docker.internal` on macOS). Set
`KAGENT_LOCAL_HOST` when the cluster uses a different host address.

`TestMCPInteraction` starts `mockmcp` on the same reachable host, registers it
as a `RemoteMCPServer`, and verifies an actual `tools/call` request.

`TestSessionContextCompaction` clones the `kagent` Harness into one whose
`spec.kagent.compaction` fires a sliding window after two turns, with a
dedicated summarizer `ModelConfig` pointing at the same mock LLM behind a
recording proxy. It checks that the runtime calls the summarizer model once,
and that the third turn's model request carries the summary instead of the
compacted turns.

`TestOpaqueBYOAgentInteraction` uses the fixture built by `make build-byo-a2a`;
`TestMCPInteraction/byo-adk` runs the Go ADK image through the BYO adapter.

The `TestMCPSessionInteraction`, `TestMCPAskUserContinuation`, and
`TestMCPCancelTask` cases exercise the controller's public `/mcp` endpoint on
port 8083, including MCP Tasks polling, synchronous fallback, A2A task identity,
input continuation, and cancellation.

`mocks/` contains the deterministic LLM responses used by interaction tests.

`TestSessionHTTPInteraction` discovers an Agent's Agent Card, invokes
the advertised JSON-RPC interface, streams a second turn, and checks task
persistence across HTTP and gRPC. `TestSessionHTTPResubscribeAndCancel`
subscribes to an active HTTP task and verifies cancellation on both SSE streams.
Both cases run across the harness matrix without a session routing header.

For local interaction debugging, start any retained response fixture from the
`go` directory:

```bash
go run ./core/hack/mockllm invoke_mcp_agent.json
```
