# End-to-end tests

The suite exercises the public API against a clean Kind installation. It does
not reconcile Kubernetes resources itself: installation creates the `kagent`
Harness and `smoke` AgentTemplate, and each test owns the API resources it
creates.

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
  envsubst < go/core/test/e2e/manifests/lifecycle.yaml.tmpl | kubectl apply -f -
KAGENT_E2E_GRPC_TARGET=<controller-address>:8084 make -C go e2e
```

`TestAgentInstanceInteraction` starts the deterministic mock LLM on the test
host and translates its listener to the host address reachable from the
cluster (`172.17.0.1` on Linux and `host.docker.internal` on macOS). Set
`KAGENT_LOCAL_HOST` when the cluster uses a different host address.

`TestMCPInteraction` starts `mockmcp` on the same reachable host, registers it
as a `RemoteMCPServer`, and verifies an actual `tools/call` request.

`mocks/` contains the deterministic LLM responses used by interaction tests.

For local interaction debugging, start any retained response fixture from the
`go` directory:

```bash
go run ./core/hack/mockllm invoke_mcp_agent.json
```
