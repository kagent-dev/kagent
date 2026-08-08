# LLM Provider Configuration

kagent supports multiple LLM providers. Configure them via Helm values or the dashboard.

## Supported Providers

| Provider | Helm key | API key env var |
|----------|----------|-----------------|
| OpenAI | `openAI` | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | `ANTHROPIC_API_KEY` |
| Azure OpenAI | `azureOpenAI` | `AZURE_OPENAI_API_KEY` |
| Google Gemini | `gemini` | `GOOGLE_API_KEY` |
| Google Vertex AI (Gemini) | `geminiVertexAI` | (service account — `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`) |
| Anthropic via Vertex AI | `anthropicVertexAI` | (service account — `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`) |
| Amazon Bedrock | `bedrock` | (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) |
| Mistral AI | `mistral` | `MISTRAL_API_KEY` (optional `MISTRAL_API_BASE` for custom endpoint) |
| Ollama | `ollama` | (none — local, uses `OLLAMA_API_BASE` for endpoint) |
| BYO OpenAI-compatible | custom | varies |

**Helm key convention:** Provider names are camelCase with lowercase first letter (e.g., `openAI`, `azureOpenAI`, `geminiVertexAI`).

## CLI Install

The CLI uses `KAGENT_DEFAULT_MODEL_PROVIDER` to select the provider (defaults to `openAI` if not set). Set both the provider and API key:

```bash
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI   # or anthropic, azureOpenAI, gemini, ollama
export OPENAI_API_KEY="sk-..."
kagent install --profile demo
```

For Anthropic:
```bash
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic
export ANTHROPIC_API_KEY="sk-ant-..."
kagent install --profile demo
```

## Helm Install (explicit)

### OpenAI
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=openAI \
  --set providers.openAI.apiKey=$OPENAI_API_KEY
```

### Anthropic
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=anthropic \
  --set providers.anthropic.apiKey=$ANTHROPIC_API_KEY
```

### Azure OpenAI
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=azureOpenAI \
  --set providers.azureOpenAI.apiKey=$AZURE_OPENAI_API_KEY
```

### Google Gemini
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=gemini \
  --set providers.gemini.apiKey=$GOOGLE_API_KEY
```

### Ollama (local models)
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=ollama
```

Ollama must be accessible from within the cluster.

### Mistral AI

Mistral speaks the OpenAI-compatible wire protocol (POST `/chat/completions` with a Bearer token). The runtime defaults to `https://api.mistral.ai/v1` and honors the same parameters (`temperature`, `top_p`, `max_tokens`, `timeout`).

```bash
export MISTRAL_API_KEY="..."
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=mistral \
  --set providers.mistral.apiKey=$MISTRAL_API_KEY
```

CLI install:
```bash
export KAGENT_DEFAULT_MODEL_PROVIDER=mistral
export MISTRAL_API_KEY="..."
kagent install --profile demo
```

ModelConfig example:
```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: mistral-large
  namespace: kagent
spec:
  provider: Mistral
  model: mistral-large-latest
  apiKeySecret: kagent-mistral
  apiKeySecretKey: MISTRAL_API_KEY
  mistral:
    temperature: "0.3"
    maxTokens: 4096
    # baseUrl: https://api.mistral.ai/v1   # optional, defaults to Mistral cloud
```

Available models: `mistral-large-latest`, `mistral-medium-latest`, `mistral-small-latest`, `magistral-medium-latest`, `magistral-small-latest`, `codestral-latest`, `ministral-8b-latest`, `ministral-3b-latest`, `pixtral-large-latest`, `open-mistral-nemo`. Set `MISTRAL_API_BASE` to point at a self-hosted or regional Mistral endpoint.

## ModelConfig CRD

For fine-grained control, create ModelConfig resources directly:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: my-model-config
  namespace: kagent
spec:
  provider: OpenAI
  model: gpt-4.1
  apiKeySecret: my-api-key-secret     # name of K8s Secret
  apiKeySecretKey: api-key             # key within the Secret
```

Then reference it in your Agent:
```yaml
spec:
  declarative:
    modelConfig: my-model-config
```

## Multiple Providers

You can configure multiple providers simultaneously. Create separate ModelConfig resources for each and reference the appropriate one per agent. This allows different agents to use different LLMs.

## BYO OpenAI-Compatible Provider

For self-hosted or third-party OpenAI-compatible APIs (vLLM, Together, etc.), configure as OpenAI with a custom base URL in the ModelConfig.
