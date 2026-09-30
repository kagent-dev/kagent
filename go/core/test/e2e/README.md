# End-to-end tests

The standalone sandbox suite needs a Substrate WorkerPool with available capacity.
Set `controller.sandbox.guestImage.registry`, `.repository`, and `.digest` to the
guest image built for the test. The controller passes the pinned image reference
unchanged to Substrate.
It creates and cleans up its own SandboxTemplates. Run from `go/`:

```sh
KAGENT_E2E_API_URL=http://<controller-address>:8083 \
KAGENT_E2E_SANDBOX_NAMESPACE=kagent \
KAGENT_E2E_SANDBOX_WORKER_POOL=kagent-default \
KAGENT_E2E_RUNTIME_IMAGE=<digest-pinned-go-adk-image> \
  go test ./core/test/e2e -run '^TestSandbox' -v -count=1 -timeout 15m
```

The controller restart test needs a stable service endpoint (NodePort or ingress);
`kubectl port-forward` exits when the selected pod is replaced. The suite tests
public gRPC/MCP calls, owner isolation, binary files, process execution/output,
suspend/resume, expiration, template revision retention, and controller restart.
The agent test uses the Helm-installed `kagent-api` RemoteMCPServer, a deterministic
local model, and a real Go ADK Session to create a sandbox through MCP as its
invoking user. Install the chart as release `kagent` in the test namespace with
its default naming; the test requires that registration and leaves it intact.
Once a cluster API URL is set, missing `KAGENT_E2E_RUNTIME_IMAGE` fails the tests
before provisioning. The same digest-pinned Go ADK image supplies both the agent
runtime and sandbox tools.

`TestSessionIdleExpiration` runs its harness cases in parallel so they share the
idle period and expiration sweep. Its parent stays sequential to isolate the
controller rollouts from the rest of the suite. It temporarily sets
`KAGENT_SESSION_IDLE_TTL=30s` and `KAGENT_SESSION_EXPIRATION_POLL_INTERVAL=5s` on the
controller Deployment and restores its environment afterward, rolling the
controller both times and waiting for a
fresh API connection after each rollout. Use a disposable test cluster and a
stable API endpoint for this case. It verifies completed-turn
expiration, Actor removal, NotFound, and a fresh conversation from the same
caller/request ID. PostgreSQL-backed service tests cover active/waiting turns,
dispatch and quiescence races, retry recovery, and retained checkpoint history.
Preparation and runtime failures also fail the tests. CI builds the guest image
separately and passes its digest to Helm before installing. Substrate rewrites
the runner's `localhost:5001` registry address when workers pull the image.

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
        t.Parallel()
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
after a failure so all harness results are visible. Independent harness subtests
run in parallel, sharing the suite's `-parallel` budget. Each case owns its mocks,
configuration, Sessions, and cleanup; installed Harnesses are read-only fixtures.
The inline/referenced configuration matrix also runs its configuration cases in
parallel. Steps that share a Session, such as the CLI output-format subtests,
remain sequential within each harness.

Independent sandbox scenarios, opaque BYO invocation, and completed-chat trace
checks also run in parallel. Trace cases share the receiver but select spans by
their own trace IDs; the receiver is cleared once before starting those cases.

Controller restart cases stay sequential with respect to the rest of the suite.
The scheduled-run restart test starts all selected harness executions before one
shared controller restart, then releases their model responses and checks recovery
in parallel. Each harness retains its own Session, task, and prompt-count assertions.

Each gRPC fixture calls `grpc.health.v1.Health/Check` on its own connection before
creating resources and requires a `SERVING` response. The read-only probe waits up
to one minute for transport readiness, including retries of failed dials after
controller rollouts. Subsequent calls keep their normal failure and retry behavior;
fixture setup does not retry mutations or suppress errors returned by the server.

CI runs four concurrent scenarios on four Substrate worker pods. Substrate
v0.3.0-alpha3 enables multiple actors per worker by default (`--max-actors=1000`),
so test concurrency is no longer limited to the worker count. A scenario may need
multiple actors for subagents or template preparation; four scenarios is not a
four-actor cap. Parallel harness subtests share the same `-parallel` budget as
other scenarios; they do not multiply it. Go still isolates controller restart
tests from parallel scenarios.

CI runs separate gVisor and Cloud Hypervisor jobs in parallel on Blacksmith.
The Cloud Hypervisor job first checks that `/dev/kvm` can create a VM, then
mounts it into Kind and installs Substrate's pinned microVM assets and
`SandboxConfig`. A runner without nested KVM fails that job before image builds.
Each runtime uploads its own `e2e-logs-gvisor` or `e2e-logs-microvm` artifact.

The same setup is available locally:
`KIND_SANDBOX_CLASS=microvm make create-kind-cluster` checks KVM and mounts it
into the Kind node. After installing Substrate, run
`SUBSTRATE_VERSION=0.3.0-alpha3 bash scripts/kind/setup-microvm.sh`.
It fetches the matching Substrate release to use its asset installer and caches
the downloaded assets under `.cache/substrate/microvm-assets/`.

To compare four versus eight on the same revision and runner, manually dispatch
the CI workflow with `e2e_parallel` set to `4` or `8`. Compare the `Run e2e tests`
step duration and failures over repeated runs; doubling concurrency does not
guarantee a speedup on the four-vCPU runner. Locally, use
`make -C go e2e E2E_PARALLEL=8` (the local default remains two).

CI uploads a log artifact per runtime with test output, the final controller's logs,
and worker/Substrate logs streamed during the suite. Use it to investigate an
earlier timeout: subsequent actor activity can displace the failure from the
200-line tails printed at the end of the job.

Substrate selects randomly among workers with room, rather than preferring the
fullest worker. Worker CPU/memory limits can be set through
`substrateWorkerPool.template.resources`, but agent ActorTemplates currently omit
resource limits, so these do not provide a per-agent packing budget. Standalone
sandbox actors do declare limits. Resource-based packing for agents first needs
measured actor sizes and limits in the runtime configuration; it is not needed to
use the existing multi-actor workers for this concurrency trial.

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
`KAGENT_E2E_LOCAL_HOST` when the cluster uses a different host address.

Substrate alpha3 policies require DNS names and explicit protocols and ports.
The mocks use Kubernetes Service names. For tracing, configure the controller's
OTLP endpoint as `http://e2e-otlp.kagent.svc.cluster.local:14317` and apply
`manifests/tracing.yaml.tmpl` with `KAGENT_E2E_LOCAL_HOST` set to the test host's
reachable IPv4 address. Its Service and EndpointSlice route to the receiver.

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
