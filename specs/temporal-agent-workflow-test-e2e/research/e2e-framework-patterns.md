# E2E Framework Patterns

## Key Helpers

| Function | Purpose |
|----------|---------|
| `setupK8sClient(t, includeV1Alpha1)` | Creates controller-runtime K8s client with CRD schemes |
| `setupMockServer(t, mockFile)` | Starts mockllm server, returns (baseURL, stopFunc) |
| `setupModelConfig(t, cli, baseURL)` | Creates ModelConfig CR pointing to mock server |
| `setupAgent(t, cli, modelCfg, tools)` | Creates agent + waits for Ready + endpoint |
| `setupAgentWithOptions(t, cli, modelCfg, tools, opts)` | Same with custom AgentOptions |
| `setupA2AClient(t, agent)` | Creates A2A client for agent |
| `runSyncTest(t, client, msg, expected, artifacts, ctxID...)` | Send sync message, verify response contains expected text |
| `runStreamingTest(t, client, msg, expected, ctxID...)` | Send streaming message, verify SSE events contain expected text |
| `waitForEndpoint(t, ns, name)` | Poll A2A URL until non-5xx (60s timeout) |
| `cleanup(t, cli, objects...)` | Register t.Cleanup to delete resources; on failure prints debug info |
| `buildK8sURL(url)` | Convert localhost → host.docker.internal (macOS) / 172.17.0.1 (Linux) |
| `kagentBaseURL()` | Returns KAGENT_URL env or http://localhost:8083 |

## AgentOptions Struct

```go
type AgentOptions struct {
    Name            string
    SystemMessage   string
    Stream          bool
    Env             []corev1.EnvVar
    Skills          *v1alpha2.SkillForAgent
    ExecuteCode     *bool
    ImageRepository *string           // e.g., "kagent-dev/kagent/golang-adk"
    Memory          *v1alpha2.MemorySpec
    PromptTemplate  *v1alpha2.PromptTemplateSpec
}
```

## Mock LLM Server

- Package: `github.com/kagent-dev/mockllm`
- Config loaded from embedded `mocks/*.json` via `go:embed`
- JSON format: `{ "openai": [ { "name", "match": { "match_type", "message" }, "response": { ... } } ] }`
- Match types: `contains`, `equals`, `regex`
- Returns OpenAI-compatible chat.completion responses

## Test Lifecycle

1. `setupMockServer()` → mock LLM on random port
2. `setupK8sClient()` → K8s client with CRD schemes
3. `setupModelConfig()` → ModelConfig CR pointing to mock
4. `setupAgent()` / `setupTemporalAgent()` → Agent CR + wait for Ready
5. `setupA2AClient()` → A2A client
6. `runSyncTest()` / `runStreamingTest()` → send message, verify
7. `cleanup()` via `t.Cleanup()` → delete resources (skip if SKIP_CLEANUP=1 and failed)

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `KAGENT_URL` | `http://localhost:8083` | Controller API base URL |
| `KAGENT_LOCAL_HOST` | auto-detect | K8s-accessible host for mock servers |
| `SKIP_CLEANUP` | unset | Preserve failed test resources |
| `TEMPORAL_ENABLED` | unset | Enable temporal E2E tests |
| `TEMPORAL_CRASH_RECOVERY_TEST` | unset | Enable destructive crash test |
| `TEMPORAL_UI_TEST` | unset | Enable tctl-based UI test |

## Temporal-Specific Helpers (already exist)

| Function | Purpose |
|----------|---------|
| `skipIfNoTemporal(t)` | Skip if TEMPORAL_ENABLED unset |
| `waitForTemporalReady(t)` | Poll temporal-server deployment (120s) |
| `waitForNATSReady(t)` | Poll nats deployment (120s) |
| `setupTemporalAgent(t, cli, modelCfg, opts)` | Create agent with temporal.enabled + golang-adk image |
