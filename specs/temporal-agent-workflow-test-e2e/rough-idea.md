# Rough Idea

End-to-end testing for the Temporal agent workflow feature in kagent.

The existing E2E test file (`go/core/test/e2e/temporal_test.go`) has skeleton tests but they need to be validated against the actual deployed infrastructure. Key areas to test:

1. **Infrastructure readiness** -- Temporal server + NATS are deployed and healthy
2. **CRD translation** -- Agent with `temporal.enabled: true` gets correct env vars and config
3. **Workflow execution** -- A2A message triggers Temporal workflow, returns correct response (sync + streaming)
4. **Crash recovery** -- Pod restart mid-execution, workflow resumes
5. **Fallback path** -- Agent without temporal spec still works via synchronous path
6. **Custom timeout/retry** -- Custom TemporalSpec persists and is applied
7. **Temporal UI plugin** -- Accessible via plugin proxy
8. **Workflow visibility** -- Executed workflows appear in Temporal server

Current state:
- E2E tests exist but are gated behind `TEMPORAL_ENABLED=1` env var
- Mock LLM server (`mockllm`) is used for deterministic responses
- Tests use the existing E2E framework (Kind cluster, K8s client, A2A client)
- Some tests have additional gates (`TEMPORAL_CRASH_RECOVERY_TEST=1`, `TEMPORAL_UI_TEST=1`)

Need to determine:
- Are these tests actually passing against a real cluster?
- What's missing from the test coverage?
- How to integrate into CI?
- What helper functions are needed?
