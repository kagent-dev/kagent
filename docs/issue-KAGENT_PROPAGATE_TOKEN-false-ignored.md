# KAGENT_PROPAGATE_TOKEN=false still propagates token to MCP tool calls

## Summary

Setting `KAGENT_PROPAGATE_TOKEN: "false"` on the k8s agent (or any ADK agent) does not disable token propagation. The user's Bearer token is still sent to the tool server (e.g. kagent-tools) when the agent makes MCP calls.

## Root cause

In `python/packages/kagent-adk/src/kagent/adk/cli.py`, the env var is read as a raw string and used in a boolean context:

```python
propagate_token = os.getenv("KAGENT_PROPAGATE_TOKEN")

def create_sts_integration() -> Optional[ADKTokenPropagationPlugin]:
    if sts_well_known_uri or propagate_token:  # bug: "false" is truthy in Python
        ...
        return ADKTokenPropagationPlugin(sts_integration)
```

In Python, any non-empty string is truthy. So when the Agent deployment has:

```yaml
env:
  - name: KAGENT_PROPAGATE_TOKEN
    value: "false"
```

`propagate_token` is the string `"false"`, and `if propagate_token` is `True`. The token propagation plugin is therefore always added whenever the env var is **set**, regardless of value. Only leaving the env var **unset** currently disables propagation.

## Expected behavior

- `KAGENT_PROPAGATE_TOKEN=true` (or `"true"`) → enable token propagation (user token sent to MCP tools).
- `KAGENT_PROPAGATE_TOKEN=false` (or `"false"`) or unset → do **not** propagate token.

## Suggested fix

Treat only the literal value `"true"` (case-insensitive) as enabled:

```python
# Only treat as True when explicitly "true"; "false" or unset = do not propagate
propagate_token = os.getenv("KAGENT_PROPAGATE_TOKEN", "").lower() == "true"
```

## Affected code

- **File:** `python/packages/kagent-adk/src/kagent/adk/cli.py`
- **Lines:** ~29 and the `create_sts_integration()` condition at ~33
- **Entrypoints:** Both `static` and `run` commands use `create_sts_integration()`, so both are affected.

## Impact

- Users who set `KAGENT_PROPAGATE_TOKEN: "false"` for security or architecture reasons (e.g. tool server should use in-cluster SA only) still have the user token sent on every MCP call.
- Relies on omitting the env var entirely to disable propagation, which is unclear and easy to get wrong when copying configs that set it to `"true"`.

## Reproducing

1. Deploy an ADK agent (e.g. k8s-agent) with `KAGENT_PROPAGATE_TOKEN: "false"` in the deployment env.
2. Send a message to the agent that triggers an MCP tool call (e.g. “list namespaces”).
3. Observe tool server logs or network: Bearer token is still present on the request from the agent to the tool server.

## Labels (suggested)

- bug
- area/agents or area/adk
- priority/medium (security and correctness)


## Summary

- Add context.Context as the first parameter to all Client interface methods in the database layer
- Update gormClient, fakeClient, and all HTTP handler/controller call sites to pass context through
- Enables request cancellation and tracing to propagate from HTTP handlers and the controller reconciler down to database operations

# Testing

- Existing unit tests updated to pass context.Background() — all pass locally
- Fake client updated to match new interface signatures
- No behavioral changes; purely a mechanical refactor to follow Go context idioms