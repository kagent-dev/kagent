# Runtime Feature Matrix

This document tracks feature support across Python and Go ADK runtimes in kagent.

## Feature Support Status

| Feature | CRD Field | AgentConfig Field | Python Runtime | Go Runtime | Notes |
|---------|-----------|-------------------|----------------|------------|-------|
| **Code Execution** | `spec.declarative.executeCodeBlocks` | `execute_code` | ✅ Supported | ❌ Not Planned | Being deprecated in Python; will not be implemented in Go |
| **Memory Tools** | `spec.declarative.memory` | `memory` | ✅ Supported | ✅ Supported | ✓ Feature parity (PR #1444) |
| **Context Compression** | `spec.declarative.context.compaction` | `context_config.compaction` | ✅ Supported | ✅ Supported | ✓ Feature parity (custom implementation until upstream release) |
| **Model Config** | `spec.declarative.modelConfig` | `model` | ✅ Supported | ✅ Supported | ✓ Feature parity |
| **Tools (MCP)** | `spec.declarative.tools` | `http_tools`, `sse_tools` | ✅ Supported | ✅ Supported | ✓ Feature parity |
| **Remote Agents** | `spec.declarative.tools[].agent` | `remote_agents` | ✅ Supported | ✅ Supported | ✓ Feature parity |
| **Streaming** | `spec.declarative.stream` | `stream` | ✅ Supported | ✅ Supported | ✓ Feature parity |
| **System Message** | `spec.declarative.systemMessage` | `instruction` | ✅ Supported | ✅ Supported | ✓ Feature parity |

## Legend

- ✅ **Supported**: Feature is fully implemented and tested
- 🚧 **In Progress**: Feature is currently being implemented
- ❌ **Not Planned**: Feature will not be implemented
- ❌ **Planned**: Feature is planned for future implementation
- 🚫 **Blocked on Upstream**: Feature requires upstream library support that doesn't exist yet

## Implementation Details

### Code Execution
**Status**: Not planned for Go runtime

Python ADK uses `SandboxedLocalCodeExecutor()` to execute Python code blocks from LLM responses. This feature is being deprecated in the Python runtime and will not be implemented in Go. Users configuring this field with `runtime: go` will receive a warning.

**References**:
- Python: `kagent/adk/types.py:378`
- Upstream issue: https://github.com/google/adk-python/issues/3921

### Memory Tools
**Status**: ✅ Supported in both runtimes (as of PR #1444)

Memory provides long-term knowledge across sessions using vector storage and semantic search. The upstream `google.golang.org/adk/memory` package provides the necessary functionality.

**Python Implementation**:
- Adds 3 tools: `PrefetchMemoryTool`, `LoadMemoryTool`, `SaveMemoryTool`
- Auto-save callback every 5 user turns
- Memory suffix added to system instruction
- Reference: `kagent/adk/types.py:404-458`

**Go Implementation** (✅ implemented):
- Custom `KagentMemoryService` that stores memories via Kagent backend API (pgvector)
- Adds `loadmemorytool.New()` and `preloadmemorytool.New()` to agent tools
- Memory service configured in runner config with embedding and LLM support
- TTL configuration passed through AgentConfig
- Connects to Kagent API at `http://kagent-controller:8083` (configurable via `KAGENT_API_URL`)
- **Embedding generation**: Uses OpenAI/Azure OpenAI to generate 768-dimensional vectors
- **Session summarization**: Uses LLM to extract key facts before storage
- Reference: `go/adk/pkg/agent/agent.go:44-50`, `go/adk/pkg/runner/adapter.go`, `go/adk/pkg/memory/kagent_service.go`, `go/adk/pkg/embedding/embedding.go`

**Upstream References**:
- Go package: https://pkg.go.dev/google.golang.org/adk/memory
- Documentation: https://google.github.io/adk-docs/sessions/memory/

**Testing**:
- Smoke test: `go/adk/pkg/agent/agent_test.go:TestAgentConfigFieldUsage`
- E2E test: Pending PR #1452 (requires runtime field to select Go runtime)

### Context Compression
**Status**: ✅ Supported in both runtimes (Go uses custom implementation)

Context compression summarizes older conversation messages to reduce token usage while preserving important information.

**Python Implementation**:
- Uses `build_adk_context_configs()` to translate CRD config
- Configured in A2A executor with `EventsCompactionConfig`
- Reference: `kagent/adk/_a2a.py:117-120`, `kagent/adk/types.py:524-558`

**Go Implementation** (✅ custom implementation):
- **Custom compaction package** at `go/adk/pkg/compaction/` that mirrors upstream API from PR #300
- Uses `CompactingSessionService` wrapper to intercept session operations
- Automatically triggers compaction based on `CompactionInterval` setting
- LLM-based summarization of old conversation history
- Stores compaction metadata in event StateDelta
- Reference: `go/adk/pkg/compaction/`, `go/adk/pkg/runner/adapter.go:45-65`

**Migration Path**:
- Current: Custom implementation works with Go ADK v0.6.0
- Future: When upstream releases PR #300 (compaction in runner.Config), we can deprecate custom package
- API surface designed to match upstream for easy transition

**Configuration**:
- `compaction_interval`: Number of turns before compaction (default: 5)
- `overlap_size`: Number of recent turns to keep for context (default: 2)
- `prompt_template`: Optional custom system prompt for summarization

**Upstream References**:
- Python documentation: https://arjunprabhulal.com/adk-context-management/
- Go ADK PR #300: https://github.com/google/adk-go/pull/300 (not yet released)

## Validation

### Soft Validation (Current)

When users configure unsupported features with Go runtime, the controller sets a Warning condition but allows the Agent to be created. This provides user feedback without breaking existing deployments.

Example warning:
```
Condition: Warning
Reason: UnsupportedFeatures
Message: Context compression is not supported in Go runtime and will be ignored. Consider using runtime: python or removing the context configuration.
```

### Future: Strict Validation

Once all features reach parity, we may add webhook validation that rejects Agent creation when unsupported features are configured. This will be a gradual transition with clear migration guidance.

## Adding New Features

When adding a new feature to either runtime, follow this checklist:

1. **CRD**: Add field to `DeclarativeAgentSpec` in `go/api/v1alpha2/agent_types.go`
2. **AgentConfig**: Add field to `AgentConfig` struct in `go/api/adk/types.go`
3. **Translation**: Update controller translator to populate AgentConfig field
4. **Python Runtime**: Implement in `kagent/adk/types.py` `to_agent()` method
5. **Go Runtime**: Implement in `go/adk/pkg/agent/agent.go` `CreateGoogleADKAgent()`
6. **Tests**: Add smoke test and E2E test for both runtimes
7. **Documentation**: Update this matrix and add godoc comments
8. **CI**: Verify smoke test catches missing implementation

## Parity Enforcement

We use test-based enforcement to prevent feature gaps:

- **Smoke test** in `go/adk/pkg/agent/agent_test.go` verifies all `AgentConfig` fields are handled
- **E2E tests** compare behavior across runtimes
- **CI checks** fail if tests don't pass

## Related Issues & PRs

- #1444: Go/Python ADK Feature Parity (this issue)
- #1445: Runtime field for Declarative Agent CRD
- PR #1452: Add runtime field to CRD

## Maintenance

This matrix should be updated whenever:
- A new feature is added to either runtime
- A feature reaches parity or is deprecated
- Upstream ADK libraries add new capabilities
- Validation strategy changes

Last updated: 2026-03-06
