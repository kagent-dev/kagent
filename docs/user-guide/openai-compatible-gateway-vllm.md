# OpenAI-compatible gateway with self-hosted vLLM

This guide covers a common self-hosted pattern that is not spelled out in the hosted-provider docs:

```
kagent agent  →  OpenAI-compatible gateway (Bifrost, LiteLLM, …)  →  vLLM
```

kagent treats the gateway as an OpenAI provider: configure a `ModelConfig` with `provider: OpenAI` and a custom `openAI.baseUrl`. The gateway routes requests to vLLM using its own model identifiers.

For the general BYO OpenAI-compatible setup (secrets, TLS, Cohere example), see [BYO OpenAI-compatible model](https://kagent.dev/docs/kagent/supported-providers/byo-openai) on kagent.dev.

## Prerequisites

- kagent installed and running
- An OpenAI-compatible gateway reachable from agent pods (for example [Bifrost](https://github.com/maximhq/bifrost) or [LiteLLM](https://docs.litellm.ai/))
- vLLM serving a model behind that gateway
- A Kubernetes namespace for your `ModelConfig` and agent (for example `kagent`)

## 1. Launch vLLM with tool calling enabled

kagent's declarative Python runtime always registers at least one built-in tool (`ask_user`) on every agent — even when you configure no tools yourself. As a result, every chat completion request kagent sends includes a non-empty `tools` array with `tool_choice: "auto"`. The vLLM backend **must** support automatic tool choice or every agent turn fails, including on agents with no user-configured tools.

Start vLLM with at least:

```bash
vllm serve Qwen/Qwen2.5-7B-Instruct \
  --enable-auto-tool-choice \
  --tool-call-parser hermes
```

Notes:

- `--enable-auto-tool-choice` is required for `tool_choice: "auto"`.
- `--tool-call-parser` must match your model family. The [vLLM tool calling docs](https://docs.vllm.ai/en/latest/features/tool_calling.html) are the source of truth for parser names — at the time of writing, Qwen2.5 uses `hermes` and Llama 3.1 uses `llama3_json`, but parsers are added and renamed across vLLM releases. Always check the docs for your exact model and vLLM version.

Register the model in your gateway using the identifier your gateway expects (often provider-prefixed). Example LiteLLM-style name: `vllm/Qwen/Qwen2.5-7B-Instruct`.

## 2. Point the gateway at vLLM

Configure Bifrost, LiteLLM, or your gateway so the model name above routes to the vLLM OpenAI endpoint (typically `http://<vllm-host>:8000/v1`).

Keep note of:

- Gateway base URL — the port depends on your gateway (LiteLLM defaults to `4000`, Bifrost to `8080`) and must include the `/v1` path if your gateway exposes the OpenAI API there
- Gateway API key (if required)
- **Gateway model ID** — the string you pass in chat completion `model` requests

## 3. Create a ModelConfig in kagent

Use the gateway model ID in `spec.model`, not necessarily the bare Hugging Face name vLLM serves internally.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gateway-api-key
  namespace: kagent
type: Opaque
stringData:
  key: sk-gateway-example   # replace with your gateway key, or any placeholder if auth is disabled
---
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: qwen-vllm-via-gateway
  namespace: kagent
spec:
  provider: OpenAI
  # Use the gateway routing ID, not the raw vLLM model name.
  model: vllm/Qwen/Qwen2.5-7B-Instruct
  apiKeySecret: gateway-api-key
  apiKeySecretKey: key
  openAI:
    # Adjust host and port for your gateway (LiteLLM defaults to 4000, Bifrost to 8080)
    baseUrl: http://litellm.kagent.svc.cluster.local:4000/v1
```

| Field | What to set |
| --- | --- |
| `provider` | Always `OpenAI` for OpenAI-compatible gateways |
| `model` | Gateway routing identifier (for example `vllm/Qwen/...`) |
| `openAI.baseUrl` | Gateway OpenAI API base URL |
| `apiKeySecret` / `apiKeySecretKey` | Secret holding the gateway API key |

Reference the `ModelConfig` from your agent via `spec.declarative.modelConfig`. The `ModelConfig` must live in the **same namespace** as the agent.

## 4. Verify

1. Confirm the gateway can reach vLLM (`curl` the gateway `/v1/models` or equivalent).
2. Create or update an agent that references the `ModelConfig`.
3. Open the agent in the kagent UI and send a message that should invoke a tool.

If tool calling works, the agent completes turns normally. If vLLM was started without tool-calling flags, requests fail before useful output appears.

## Troubleshooting

### `provider API error (status 400)` on every agent message

**Likely cause:** vLLM was started without `--enable-auto-tool-choice` and a matching `--tool-call-parser`.

kagent always sends a `tools` array with `tool_choice: "auto"` — the runtime injects a built-in `ask_user` tool on every declarative agent, so this happens even when the agent has no tools configured. vLLM rejects that request shape unless auto tool choice is enabled at startup, and the error surfaces in kagent as a generic 400 with no hint of the cause.

**Fix:** Restart vLLM with the flags from step 1, then retry.

### Model not found / empty model dropdown in the UI

**Likely cause:** `ModelConfig` namespace does not match the agent namespace, or `spec.model` does not match any name the gateway knows.

**Fix:**

- Create the `ModelConfig` in the same namespace as the agent.
- Set `spec.model` to the gateway's routing ID (check the gateway config or `GET /v1/models`).

### Connection errors to the gateway

- Ensure `openAI.baseUrl` is reachable from agent pods (cluster DNS, correct port, `/v1` suffix).
- For TLS with a private CA, see [TLS configuration](https://kagent.dev/docs/kagent/supported-providers/byo-openai#tls-configuration) on the BYO OpenAI page.

## Example manifest

See [`examples/modelconfig-openai-gateway-vllm.yaml`](../../examples/modelconfig-openai-gateway-vllm.yaml) for a copy-paste starting point.

## Related

- [BYO OpenAI-compatible model](https://kagent.dev/docs/kagent/supported-providers/byo-openai) — secrets, base URL, TLS
- [ModelConfig TLS examples](../../examples/modelconfig-with-tls.yaml) — LiteLLM with custom CA certificates
- [vLLM tool calling](https://docs.vllm.ai/en/latest/features/tool_calling.html) — parser flags by model family
- [Issue #959](https://github.com/kagent-dev/kagent/issues/959) — native vLLM `ModelConfig` provider (future)
