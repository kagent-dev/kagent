# Kagent Helm Chart

These Helm charts install kagent-crds,kagent, it is required that the Kagent CRDs chart to be installed first.

## Installation

### Using Helm

```bash
# First, install the required CRDs
helm install kagent-crds ./helm/kagent-crds/  --namespace kagent

# Then install Kagent with default provider 
# --set providers.default=openAI enabled by default, but you need to provide your openAI apikey
helm install kagent ./helm/kagent/ --namespace kagent --set providers.openAI.apiKey=your-openai-api-key

# Or with optional providers if you prefer local ollama provider or anthropic
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=ollama
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=openAI       --set providers.openAI.apiKey=your-openai-api-key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=anthropic    --set providers.anthropic.apiKey=your-anthropic-api-key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=azureOpenAI  --set providers.azureOpenAI.apiKey=your-openai-api-key
```

### Using Make

```bash
# export your openAI key
export OPENAI_API_KEY=your-openai-api-key
export ANTHROPIC_API_KEY=your-anthropic-api-key
export AZUREOPENAI_API_KEY=your-azure-api-key

# install the kagent charts with openAI provider 
make KAGENT_DEFAULT_MODEL_PROVIDER=openAI helm-install

# install charts with anthropic provider
make KAGENT_DEFAULT_MODEL_PROVIDER=anthropic helm-install

# install charts with anthropic provider
make KAGENT_DEFAULT_MODEL_PROVIDER=azureOpenAI helm-install

# install charts with ollama provider
make KAGENT_DEFAULT_MODEL_PROVIDER=ollama helm-install
```

### Using kagent cli

```bash
## make sure have env variable with your API_KEY
export OPENAI_API_KEY=your-openai-api-key
export ANTHROPIC_API_KEY=your-anthropic-api-key
export AZURE_API_KEY=your-azure-api-key

#default provider is openAI but you can select from the list 
export KAGENT_DEFAULT_MODEL_PROVIDER=ollama
export KAGENT_DEFAULT_MODEL_PROVIDER=azureOpenAI
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic

# use local helm chart to install kagent with openAI provider
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
export KAGENT_HELM_REPO=./helm/
make kagent-cli-install

# use local helm chart to install kagent with ollama provider
export KAGENT_DEFAULT_MODEL_PROVIDER=ollama
export KAGENT_HELM_REPO=./helm/
make kagent-cli-install

```

## Upgrading

When upgrading, make sure to upgrade both charts:

```bash
# First, upgrade the CRDs
helm upgrade kagent-crds ./helm/kagent-crds/  --namespace kagent

# Then upgrade Kagent
helm upgrade kagent ./helm/kagent/ --namespace kagent
```

## Uninstallation

To properly uninstall Kagent:

```bash
# First, uninstall Kagent
helm uninstall kagent --namespace kagent

# To completely remove all resources including CRDs (optional):
helm uninstall kagent-crds --namespace kagent
```

**Note**: Uninstalling the CRDs chart will delete all custom resources of those types across all namespaces.

## Observability Configuration

Kagent supports OpenTelemetry tracing and logging for comprehensive observability:

### Enable Tracing

```bash
# Basic tracing setup
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.openAI.apiKey=your-openai-api-key \
  --set otel.tracing.enabled=true \
  --set otel.tracing.exporter.otlp.endpoint=http://your-collector:4317

# With custom timeout and insecure connection
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.openAI.apiKey=your-openai-api-key \
  --set otel.tracing.enabled=true \
  --set otel.tracing.exporter.otlp.endpoint=http://your-collector:4317 \
  --set otel.tracing.exporter.otlp.timeout=30 \
  --set otel.tracing.exporter.otlp.insecure=true
```

### Enable Logging

```bash
# Enable correlated logging with traces
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.openAI.apiKey=your-openai-api-key \
  --set otel.logging.enabled=true \
  --set otel.logging.exporter.otlp.endpoint=http://your-collector:4317
```

### Complete Observability Setup

```bash
# Enable both tracing and logging
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.openAI.apiKey=your-openai-api-key \
  --set otel.tracing.enabled=true \
  --set otel.tracing.exporter.otlp.endpoint=http://your-collector:4317 \
  --set otel.logging.enabled=true \
  --set otel.logging.exporter.otlp.endpoint=http://your-collector:4317
```

### Values Configuration

You can also configure observability via values.yaml:

```yaml
otel:
  tracing:
    enabled: true
    exporter:
      otlp:
        endpoint: http://your-collector:4317
        timeout: 15
        insecure: true
  logging:
    enabled: true
    exporter:
      otlp:
        endpoint: http://your-collector:4317
        timeout: 15
        insecure: true
```

### Local Development with Jaeger

```bash
# Start local Jaeger/OTEL collector (from repo root)
make otel-local  # Jaeger UI at http://localhost:16686

# Install with local collector
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.openAI.apiKey=your-openai-api-key \
  --set otel.tracing.enabled=true \
  --set otel.tracing.exporter.otlp.endpoint=http://localhost:4317
```

### What Gets Instrumented

- **A2A Communication**: End-to-end spans for agent-to-agent calls
- **GenAI SDKs**: OpenAI, Anthropic, and other LLM provider calls
- **HTTP Requests**: FastAPI and HTTPX instrumentation
- **Application Logs**: Correlated with TraceID/SpanID for debugging

## Why Separate CRDs?

Helm has a limitation where CRDs are installed but not removed during uninstallation. 
By separating CRDs into their own chart, we can:

1. Allow proper version control of CRDs
2. Enable users to choose when to remove CRDs (which is destructive)
3. Follow Helm best practices
