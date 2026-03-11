# LLM Provider Configuration

kagent supports multiple LLM providers. Configure them via Helm values or the dashboard.

## Supported Providers

| Provider | Helm key | API key env var |
|----------|----------|-----------------|
| OpenAI | `openAI` | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | `ANTHROPIC_API_KEY` |
| Azure OpenAI | `azureOpenAI` | `OPENAI_API_KEY` |
| Google Gemini | `gemini` | `GEMINI_API_KEY` |
| Google Vertex AI | `GeminiVertexAI` | (service account) |
| Anthropic via Vertex AI | `AnthropicVertexAI` | (service account) |
| Amazon Bedrock | `Bedrock` | (IAM credentials) |
| Ollama | `Ollama` | (none — local) |
| BYO OpenAI-compatible | custom | varies |

## CLI Install (quick)

The CLI sets the default provider based on which env var is set:

```bash
export OPENAI_API_KEY="sk-..."
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
  --set providers.azureOpenAI.apiKey=$OPENAI_API_KEY
```

### Google Gemini
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=gemini \
  --set providers.gemini.apiKey=$GEMINI_API_KEY
```

### Ollama (local models)
```bash
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=ollama
```

Ollama must be accessible from within the cluster.

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
