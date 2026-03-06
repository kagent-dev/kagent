## Summary

Implements Memory tools in the Go ADK runtime and adds runtime validation to warn users about unsupported features.

## Changes

**Memory Tools** ✅
- Custom `KagentMemoryService` storing via Kagent backend API (pgvector)
- OpenAI/Azure embedding generation (768-dim vectors)
- LLM-based session summarization extracting key facts
- Tools: `preloadmemorytool`, `loadmemorytool`

**Runtime Validation** ✅
- Soft validation warns about unsupported features (including context compression)
- Uses dedicated `AgentConditionTypeUnsupportedFeatures` condition type
- Does not fail reconciliation - allows deployment with warnings

**Upgrades**
- Go ADK: v0.5.0 → v0.6.0

## Testing

- All existing tests passing
- New unit tests for `KagentMemoryService` using httptest (17 test cases)
- Smoke test for AgentConfig field usage

## Feature Parity Status

| Feature | Python | Go |
|---------|--------|-----|
| Memory Tools | ✅ | ✅ |
| Context Compression | ✅ | ❌ (future work) |
| Code Execution | ✅ | ❌ (deprecated) |

Fixes #1444
