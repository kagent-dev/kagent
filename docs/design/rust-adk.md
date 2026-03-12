# Design: Rust ADK Runtime for Kagent

## 1. Overview

Kagent currently supports two agent runtimes: Python ADK (default, full-featured) and Go ADK (faster startup, added in PR #1284). This document designs a third runtime based on [adk-rust](https://github.com/zavora-ai/adk-rust) (v0.3.x), a Rust framework for building AI agents with A2A protocol, MCP tool support, and 15+ LLM providers.

The Rust runtime would give kagent users a third option optimized for fast startup, low memory footprint, and single-binary deployment — while reusing adk-rust's existing model integrations and A2A implementation rather than building from scratch.

**Prior art:** The Go ADK integration serves as the primary reference. It added a `DeclarativeRuntime` enum to the Agent CRD, a config loader that reads the controller-generated `config.json`, and custom session/task persistence backed by the kagent controller HTTP API. The Rust ADK follows the same pattern.

## 2. Goals & Non-Goals

### Goals

- Add `rust` as a third `DeclarativeRuntime` option in the Agent CRD
- Support both declarative (CRD-defined) and BYO (custom binary) Rust agents
- Read the existing `config.json` format — no controller-side changes needed
- Reuse adk-rust's model providers, MCP client, and A2A server
- Persist sessions and tasks to the kagent controller DB via HTTP API
- Support all model providers kagent currently supports (OpenAI, Anthropic, Gemini, Ollama, Bedrock)
- Produce a container image following the existing `golang-adk` naming pattern

### Non-Goals

- Contributing code upstream to adk-rust (we implement kagent-specific pieces in this repo)
- Feature parity with Python ADK on day one (memory, code execution can follow)
- HITL support at launch (explored but not required — see Section 5.7)
- Supporting adk-rust features kagent doesn't use (RAG, browser automation, voice agents, graph workflows)

## 3. Architecture

The Rust ADK runtime occupies the same slot as Python and Go — it runs as a container in the agent pod, reads config from a mounted Secret, and exposes A2A on port 8080.

```
                    ┌──────────────────────────────────────────────┐
                    │              kagent controller               │
                    │                                              │
                    │  Agent CRD ──► Translator ──► Deployment     │
                    │  (runtime: rust)    │         (rust-adk img) │
                    │                     ▼                        │
                    │              config.json Secret              │
                    └──────────────┬───────────────────────────────┘
                                   │ mounted at /config
                                   ▼
                    ┌──────────────────────────────────────────────┐
                    │            Rust ADK Agent Pod                │
                    │                                              │
                    │  ┌─────────────────────────────────────────┐ │
                    │  │  kagent-adk-rust binary                 │ │
                    │  │                                         │ │
                    │  │  Config Loader ──► Agent Builder         │ │
                    │  │       │                │                 │ │
                    │  │       ▼                ▼                 │ │
                    │  │  Model (adk-rust) + MCP Tools (adk-rust)│ │
                    │  │       │                │                 │ │
                    │  │       ▼                ▼                 │ │
                    │  │  Runner + A2A Server (adk-rust)          │ │
                    │  │       │                                  │ │
                    │  │       ├──► KagentSessionService (ours)   │ │
                    │  │       └──► KagentTaskPersistence (ours)  │ │
                    │  └─────────────────────────────────────────┘ │
                    │           │              ▲                    │
                    │      port 8080      /config mount             │
                    └───────────┼──────────────┘───────────────────┘
                                │
                         A2A protocol (HTTP/SSE)
                                │
                    ┌───────────▼──────────────────────────────────┐
                    │         kagent controller HTTP API           │
                    │  POST /api/sessions, /api/tasks, etc.       │
                    └─────────────────────────────────────────────┘
```

**What we build vs. what we reuse:**

| Component | Source | Why |
|-----------|--------|-----|
| A2A server | adk-rust (`adk-server`) | Standard A2A protocol, SSE streaming |
| Model providers | adk-rust (`adk-model`) | 15+ providers already implemented |
| MCP client | adk-rust (`adk-tool`) | HTTP + SSE transports, auth support |
| Agent runtime | adk-rust (`adk-agent`, `adk-runner`) | LLM agent, runner lifecycle |
| Config loader | **ours** (`kagent-adk`) | Maps kagent's config.json → adk-rust builders |
| Session service | **ours** (`kagent-adk`) | Implements adk-rust's `SessionService` trait, backed by kagent HTTP API |
| Task persistence | **ours** (`kagent-adk`) | Intercepts events to persist tasks to kagent HTTP API |
| Auth/token | **ours** (`kagent-adk`) | K8s SA token refresh, same pattern as Go ADK |
| Entry point | **ours** (`kagent-adk`) | `main.rs` wiring config → runner → server |

## 4. Crate Structure

Single crate to start, with modules mirroring the Go ADK's package structure:

```
rust/
├── Cargo.toml              # Workspace root (if we add more crates later)
├── Dockerfile
└── crates/
    └── kagent-adk/
        ├── Cargo.toml      # Dependencies: adk-rust crates, serde, reqwest, tokio
        ├── src/
        │   ├── main.rs             # Entry point: load config, build agent, start server
        │   ├── config/
        │   │   ├── mod.rs
        │   │   ├── types.rs        # Serde structs matching config.json schema
        │   │   ├── loader.rs       # Load + validate config.json and agent-card.json
        │   │   └── builder.rs      # Config → adk-rust agent construction
        │   ├── session/
        │   │   ├── mod.rs
        │   │   └── service.rs      # KagentSessionService (impl SessionService)
        │   ├── task/
        │   │   ├── mod.rs
        │   │   └── store.rs        # Task persistence via event interception
        │   ├── auth/
        │   │   ├── mod.rs
        │   │   └── token.rs        # K8s token refresh + HTTP client middleware
        │   ├── a2a/
        │   │   ├── mod.rs
        │   │   └── converter.rs    # Event conversion (if needed for UI compatibility)
        │   └── hitl/               # Optional — see Section 5.7
        │       ├── mod.rs
        │       └── approval.rs
        └── examples/
            └── byo/
                └── main.rs         # BYO agent example (programmatic construction)
```

**Key dependencies (from adk-rust):**
- `adk-core` — Agent trait, Content types, Tool trait
- `adk-agent` — LlmAgentBuilder, workflow agents
- `adk-model` — OpenAI, Anthropic, Gemini, Ollama, Bedrock models
- `adk-tool` — McpToolset, FunctionTool
- `adk-runner` — Runner, RunConfig, session management
- `adk-server` — A2A server, REST API, SSE streaming
- `adk-session` — SessionService trait (we implement it)

## 5. Key Components

### 5.1 Config Loader

The config loader deserializes the controller-generated `config.json` into typed Rust structs, then constructs an adk-rust agent via the builder pattern.

**Config types** (`config/types.rs`): Serde structs matching `go/api/adk/types.go`. This is the contract between the controller and all runtimes — it must not diverge.

```rust
#[derive(Deserialize)]
pub struct AgentConfig {
    pub model: Model,
    pub description: Option<String>,
    pub instruction: Option<String>,
    pub http_tools: Option<Vec<HttpMcpServerConfig>>,
    pub sse_tools: Option<Vec<SseMcpServerConfig>>,
    pub remote_agents: Option<Vec<RemoteAgentConfig>>,
    pub stream: Option<bool>,
    pub memory: Option<MemoryConfig>,
    pub execute_code: Option<bool>,
    pub context_config: Option<AgentContextConfig>,
}

#[derive(Deserialize)]
#[serde(tag = "type")]
pub enum Model {
    #[serde(rename = "openai")]
    OpenAI(OpenAIConfig),
    #[serde(rename = "anthropic")]
    Anthropic(AnthropicConfig),
    #[serde(rename = "gemini")]
    Gemini(GeminiConfig),
    #[serde(rename = "ollama")]
    Ollama(OllamaConfig),
    #[serde(rename = "bedrock")]
    Bedrock(BedrockConfig),
    // ... other variants matching Go's Model interface
}
```

**Agent construction** (`config/builder.rs`): Maps config → adk-rust builders:

```rust
pub fn build_agent(config: &AgentConfig, app_name: &str) -> Result<Arc<dyn Agent>> {
    let model = build_model(&config.model)?;
    let mut builder = LlmAgentBuilder::new(app_name)
        .model(model);

    if let Some(instruction) = &config.instruction {
        builder = builder.instruction(instruction);
    }
    if let Some(description) = &config.description {
        builder = builder.description(description);
    }

    // Add MCP toolsets
    for tool_config in config.http_tools.iter().flatten() {
        let toolset = build_mcp_toolset(tool_config)?;
        builder = builder.toolset(toolset);
    }

    Ok(Arc::new(builder.build()?))
}
```

**Model dispatch:** Match on the `Model` enum variant and construct the corresponding adk-rust model. Since adk-rust already implements all required providers, this is primarily a field-mapping exercise:

```rust
fn build_model(config: &Model) -> Result<Arc<dyn Llm>> {
    match config {
        Model::OpenAI(c) => {
            let mut model = OpenAiModel::new(&c.api_key, &c.model);
            if let Some(base_url) = &c.base_url {
                model = model.base_url(base_url);
            }
            // map temperature, max_tokens, etc.
            Ok(Arc::new(model))
        }
        Model::Anthropic(c) => { /* similar */ }
        // ...
    }
}
```

**Validation:** Follows the same rules as Go's `ValidateAgentConfigUsage` — model is required, tool URLs must be present, warn on empty instruction.

**Environment variables:** Same as Go ADK — `KAGENT_URL`, `KAGENT_NAME`, `KAGENT_NAMESPACE`, `CONFIG_DIR`, `PORT`.

### 5.2 Session Service

Implements adk-rust's `SessionService` trait backed by the kagent controller HTTP API. This is a direct port of the Go ADK's `KAgentSessionService`.

**adk-rust trait we implement:**

```rust
#[async_trait]
pub trait SessionService: Send + Sync {
    async fn create(&self, req: CreateRequest) -> Result<Box<dyn Session>>;
    async fn get(&self, req: GetRequest) -> Result<Box<dyn Session>>;
    async fn list(&self, req: ListRequest) -> Result<Vec<Box<dyn Session>>>;
    async fn delete(&self, req: DeleteRequest) -> Result<()>;
    async fn append_event(&self, session_id: &str, event: Event) -> Result<()>;
}
```

**HTTP endpoints consumed** (same as Go ADK):

| Operation | Endpoint | Notes |
|-----------|----------|-------|
| Create session | `POST /api/sessions` | Body: `{user_id, agent_ref, id?, name?}` |
| Get session | `GET /api/sessions/{id}?user_id={uid}&limit=-1` | Returns session + events |
| Delete session | `DELETE /api/sessions/{id}?user_id={uid}` | |
| Append event | `POST /api/sessions/{id}/events?user_id={uid}` | Event data is double-JSON-encoded (match Go behavior) |

**Key behaviors to preserve:**
- Session ID defaults to A2A context ID
- User ID uses `A2A_USER_` prefix
- Events are JSON-serialized twice (bytes → string in request body) — this is how the Go ADK does it and the controller expects it
- Session name extracted from first text part of message (max 20 chars)
- `append_event` uses a detached context with 30s timeout (client disconnect doesn't interrupt persistence)

**Implementation:** Uses `reqwest` with a custom middleware layer for token injection (see Section 5.5 Auth).

### 5.3 Task Persistence

The Go ADK has a `KAgentTaskStore` that implements Google ADK's task store interface and POSTs to `/api/tasks`. adk-rust's A2A executor does **not** expose a `TaskStore` trait — tasks are managed in-flight by the executor.

**Approach: Event stream interception**

We intercept the A2A event stream at the server boundary to capture completed tasks and persist them. Two options for where to intercept:

**Option A — Plugin-based:** Use adk-rust's `Plugin` system to hook `after_run`, capturing the final task state and persisting it.

**Option B — A2A handler wrapper:** Wrap adk-rust's A2A request handler to intercept the response stream, collect events, and POST the final task to `/api/tasks` after the stream completes.

Option B is more reliable since it operates at the A2A protocol level (same abstraction as the Go ADK's task store) rather than the agent execution level.

Let's go with B

**Partial event cleanup:** Following the Go ADK pattern, strip events marked with `adk_partial` or `kagent_adk_partial` metadata before persisting — these are streaming intermediates that shouldn't be in the final history.

**HTTP contract:**

| Operation | Endpoint | Notes |
|-----------|----------|-------|
| Save task | `POST /api/tasks` | Full A2A Task JSON, partials stripped |
| Get task | `GET /api/tasks/{id}` | Wrapped in `StandardResponse` |

### 5.4 A2A Integration

adk-rust implements A2A protocol v0.3.0 with SSE streaming. The server exposes:
- `GET /.well-known/agent.json` — agent card discovery
- `POST /a2a` — JSON-RPC 2.0 (message/send, message/stream, tasks/get, tasks/cancel)
- `POST /a2a/stream` — SSE streaming

**EventQueue / artifact mirroring question:**

The Go ADK needed an `EventQueue` wrapper because Google's ADK streams text as `TaskArtifactUpdateEvent`, but kagent's UI expects text in `TaskStatusUpdateEvent`. We investigated how adk-rust handles this:

- adk-rust streams text content through its A2A server as both `TaskStatusUpdateEvent` (for working state with message) and `TaskArtifactUpdateEvent` (for result artifacts)
- The Python ADK avoids this problem entirely by converting events directly to `TaskStatusUpdateEvent` at the source

**Recommendation:** Test adk-rust's default A2A streaming against kagent's UI. If the UI displays streaming text correctly, no wrapper is needed. If not, implement a lightweight event converter (in `a2a/converter.rs`) that mirrors the Python ADK approach — convert agent events directly to `TaskStatusUpdateEvent` with text in message parts. This is simpler than the Go ADK's `EventQueue` approach.

**Agent card:** Loaded from `agent-card.json` (same as Go ADK). adk-rust's server accepts an agent card at startup via `ServerConfig`.

### 5.5 Model Providers

Reuse adk-rust's built-in model implementations directly. The config loader (Section 5.1) maps `config.json` model fields to adk-rust's model constructors.

**Provider mapping:**

| config.json `type` | adk-rust crate/type | Notes |
|---------------------|---------------------|-------|
| `openai` | `adk-model::OpenAiModel` | base_url, temperature, max_tokens, etc. |
| `anthropic` | `adk-model::AnthropicModel` | base_url, temperature, top_k, etc. |
| `gemini` | `adk-model::GeminiModel` | API key or Vertex AI |
| `ollama` | `adk-model::OllamaModel` | Custom base URL |
| `bedrock` | `adk-model::BedrockModel` | AWS region |
| `azure_openai` | `adk-model::OpenAiModel` | With Azure base_url + headers |
| `gemini_vertex_ai` | `adk-model::GeminiModel` | With Vertex AI config |

**TLS configuration:** kagent's config supports custom CA certs, insecure skip verify, and disabling system CAs. adk-rust's models use `reqwest` internally, so TLS can be configured at the `reqwest::Client` level. We may need to pass a pre-configured client to the model constructors — verify whether adk-rust supports this.

**API key passthrough:** The Go ADK supports forwarding the request's Bearer token as the model API key (`api_key_passthrough`). Implement the same pattern in the Rust config builder.

### 5.6 MCP Tools

Reuse adk-rust's `McpToolset` from the `adk-tool` crate. It supports:
- HTTP transport (Streamable HTTP) — maps to kagent's `HttpMcpServerConfig`
- SSE transport — maps to kagent's `SseMcpServerConfig`
- Tool filtering — specify which tools to expose
- Auth headers — bearer token, custom headers

**Mapping from config:**

```rust
fn build_mcp_toolset(config: &HttpMcpServerConfig) -> Result<McpToolset> {
    let mut toolset = McpToolset::new(&config.params.url);

    // Headers
    for (key, value) in &config.params.headers {
        toolset = toolset.header(key, value);
    }

    // Tool filtering
    if !config.tools.is_empty() {
        toolset = toolset.filter_tools(&config.tools);
    }

    // TLS config (if supported by adk-rust's McpToolset)
    // ...

    Ok(toolset)
}
```

**`require_approval` field:** This ties into HITL (Section 5.7). If HITL is not implemented at launch, tools with `require_approval` are still loaded but approval is not enforced.

### 5.7 HITL (Human-in-the-Loop)

**Status: Exploratory — not required for launch.**

kagent's HITL system works as follows:
1. Agent attempts to call a tool marked with `require_approval`
2. Runtime pauses execution, emits `InputRequired` task state with approval details
3. UI shows approval dialog
4. User approves/rejects via A2A message
5. Runtime resumes or skips the tool call

**adk-rust extension point:** The `BeforeToolCallback` fires before every tool execution. It receives the tool name and arguments, and can return `Some(Content)` to short-circuit the tool call (rejection) or `None` to proceed (approval).

**Possible implementation:**

```rust
let approval_callback: BeforeToolCallback = Arc::new(move |ctx, tool_name, args| {
    Box::pin(async move {
        if requires_approval(tool_name) {
            // 1. Emit InputRequired status via A2A
            // 2. Wait for user response (approval/rejection)
            // 3. Return None to proceed or Some(rejection_content) to skip
        }
        None // proceed normally for non-approval tools
    })
});
```

**Challenge:** The callback is synchronous with respect to the agent execution loop. Pausing execution to wait for user input requires blocking the callback's future until the A2A server receives an approval message. This likely requires a shared channel between the A2A handler and the callback.

**The Go ADK solves this** by detecting `InputRequired` state in the `afterExecute` callback and enriching the response with structured approval data (`DataPart` with tool call details). The Python ADK uses Google ADK's built-in `request_confirmation()`.

**Recommendation:** Defer HITL to a follow-up. The callback mechanism exists in adk-rust, but wiring it to the A2A request/response cycle needs careful design. When implemented, follow the Go ADK's pattern of enriching the `InputRequired` response with structured tool call data so the UI can render the approval dialog.

## 6. CRD & Translator Changes

### CRD Type Change

Add `rust` to the `DeclarativeRuntime` enum in `go/api/v1alpha2/agent_types.go`:

```go
const (
    DeclarativeRuntime_Python DeclarativeRuntime = "python"
    DeclarativeRuntime_Go     DeclarativeRuntime = "go"
    DeclarativeRuntime_Rust   DeclarativeRuntime = "rust"  // new
)
```

Update the kubebuilder validation marker:

```go
// +kubebuilder:validation:Enum=python;go;rust
```

### Translator Changes

**Image repository** (`deployments.go` — `getRuntimeImageRepository`):

Add a case for Rust, following the Go pattern of deriving from the base repository:

```go
case v1alpha2.DeclarativeRuntime_Rust:
    pythonRepo := DefaultImageConfig.Repository
    lastSlash := strings.LastIndex(pythonRepo, "/")
    if lastSlash == -1 {
        return "rust-adk"
    }
    return pythonRepo[:lastSlash] + "/rust-adk"
```

Image name: `rust-adk` (following `golang-adk` convention).

**Readiness probe** (`adk_api_translator.go` — `getRuntimeProbeConfig`):

Rust startup should be comparable to Go (single binary, no interpreter):

```go
case v1alpha2.DeclarativeRuntime_Rust:
    return probeConfig{
        InitialDelaySeconds: 1,
        TimeoutSeconds:      5,
        PeriodSeconds:       1,
    }
```

### Code Generation

After CRD changes: `make controller-manifests` to regenerate DeepCopy methods and CRD YAML, then copy to Helm chart.

### Golden Files

`UPDATE_GOLDEN=true make -C go test` to regenerate translator test outputs, then verify only the expected Rust-related changes appear.

## 7. Build & Container Image

### Dockerfile

Multi-stage build at `rust/Dockerfile`:

```dockerfile
# Stage 1: Build
FROM rust:1.85-slim AS builder
WORKDIR /build
COPY . .
RUN cargo build --release --bin kagent-adk

# Stage 2: Runtime
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=builder /build/target/release/kagent-adk /app
USER 65532:65532
ENTRYPOINT ["/app"]
```

**Notes:**
- `distroless/cc-debian12` matches the Go ADK's base image (provides C runtime for any native deps)
- Separate Dockerfile from Go (unlike Go which uses one Dockerfile with `BUILD_PACKAGE` arg) because Rust has a completely different toolchain
- Build caching: consider mounting a Cargo registry cache volume for faster rebuilds

### Makefile Targets

Add to the root Makefile:

```makefile
RUST_ADK_IMAGE_NAME ?= rust-adk
RUST_ADK_IMAGE_TAG ?= $(VERSION)
RUST_ADK_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(RUST_ADK_IMAGE_NAME):$(RUST_ADK_IMAGE_TAG)

.PHONY: build-rust-adk
build-rust-adk: buildx-create
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -t $(RUST_ADK_IMG) -f rust/Dockerfile ./rust
```

Add `build-rust-adk` to the `build` and `push` aggregate targets.

### Container Arguments

Same as Go and Python: `--host 0.0.0.0 --port 8080 --filepath /config`

The binary should accept these via `clap` or similar CLI arg parser.

## 8. Testing Strategy

### Unit Tests

- **Config loader:** Test deserialization of all model types, tool configurations, edge cases (missing optional fields, unknown fields)
- **Session service:** Mock the kagent HTTP API, verify correct endpoint calls, double-encoding of events
- **Auth/token:** Test token refresh, header injection
- **Builder:** Test that config correctly maps to adk-rust agent construction

### Golden File Tests

Add a Rust runtime test case to the translator's golden file tests:
- Input: Agent CRD with `runtime: rust`
- Expected output: Deployment with `rust-adk` image, fast readiness probe, correct container args

Regenerate with `UPDATE_GOLDEN=true make -C go test`.

### E2E Tests

Add to `go/core/test/e2e/invoke_api_test.go`, mirroring the Go ADK tests:

```go
func TestE2EInvokeRustADKAgent(t *testing.T) {
    // Create agent with Runtime: v1alpha2.DeclarativeRuntime_Rust
    // Test sync and streaming invocation
    // Validate agent reaches Ready state
}
```

Requires:
- `make push-test-agent` updated to build the Rust test agent image
- Mock LLM server reachable from the Rust binary (same pattern as Go/Python tests)

### Integration Test (Rust-side)

Within `rust/crates/kagent-adk/`, add Rust integration tests that:
- Start a mock kagent HTTP API
- Load a test `config.json`
- Construct an agent and verify it starts
- Send an A2A message and verify session/task persistence

## 9. Open Questions & Risks

### Open Questions

1. **TLS configuration passthrough:** Does adk-rust's model layer support injecting a pre-configured `reqwest::Client` for custom CA certs and insecure skip verify? If not, we may need to fork or contribute TLS support.

2. **A2A streaming compatibility:** Does adk-rust's default A2A SSE streaming work with kagent's UI, or do we need an event converter? Needs empirical testing.

3. **Task persistence hook point:** The plugin-based approach (Option A) vs A2A handler wrapper (Option B) for task persistence needs prototyping to determine which is cleaner. Option B is recommended but may require modifying how we wire up adk-rust's server.

4. **`azure_openai` and `gemini_vertex_ai` mapping:** Verify that adk-rust's OpenAI model supports Azure-style endpoints (custom base URL + API version header) and that Gemini supports Vertex AI authentication.

5. **Memory support:** The Go ADK integrates kagent's memory service (embedding + vector storage). adk-rust has its own `adk-memory` crate. Determine whether to use adk-rust's memory or implement kagent-specific memory tools (like Go does). Not required at launch.

### Risks

| Risk | Severity | Mitigation |
|------|----------|- -----------|
| **adk-rust maturity** (v0.3.x, 4 months old, small team) | Medium | Pin to specific version, vendor if needed. Monitor for breaking changes. |
| **adk-rust breaking API changes** | Medium | Pin exact version in Cargo.toml. Evaluate update cost before upgrading. |
| **HITL complexity** | Low | Deferred. BeforeToolCallback exists as extension point when ready. |
| **Build time** | Low | Rust compiles slowly but only affects CI/developer builds, not runtime. Use Docker layer caching and `cargo-chef` for faster rebuilds. |
| **Upstream model adapter gaps** | Low | adk-rust claims 15+ providers. Verify kagent-critical ones (OpenAI, Anthropic, Gemini, Ollama, Bedrock) work with kagent's config fields before committing. |
| **License** | Medium | adk-rust's license shows "NOASSERTION" on GitHub. Verify license compatibility before depending on it. |
