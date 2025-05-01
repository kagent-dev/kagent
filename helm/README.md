# Kagent Helm Chart

These Helm charts install kagent-crds,kagent, it is required that the Kagent CRDs chart to be installed first.

## Installation

### Using Helm

```bash
# First, install the required CRDs
helm install kagent-crds ./helm/kagent-crds/  --namespace kagent

# Then install Kagent with default openAI provider enabled
helm install kagent ./helm/kagent/ --namespace kagent --set providers.openAI.apiKey=abcde

# if you prefer local ollama provider 
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=ollama

# Then install Kagent with openAI provider enabled
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=anthropic --set providers.anthropic.apiKey=abcde
```

### Using Make

```bash
# export your openAI key
export OPENAI_API_KEY=abcde

# install the kagent charts with openAI provider 
make KAGENT_DEFAULT_MODEL_PROVIDER=openAI helm-install

# install charts with ollama provider
make KAGENT_DEFAULT_MODEL_PROVIDER=ollama helm-install

# install charts with anthropic provider
make KAGENT_DEFAULT_MODEL_PROVIDER=anthropic helm-install
```

### Using kagent cli

```bash
#build kagent cli
make build-cli

## make sure have env variable with your API_KEY
export OPENAI_API_KEY=abcde
export ANTHROPIC_API_KEY=abcde
export AZURE_API_KEY=abcde

#default provider is openAI but you can select from the list 
export KAGENT_DEFAULT_MODEL_PROVIDER=ollama
export KAGENT_DEFAULT_MODEL_PROVIDER=azureOpenAI
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI

# use local helm chart
export KAGENT_HELM_REPO=./helm/

#run kagent
./go/bin/kagent-local
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

## Why Separate CRDs?

Helm has a limitation where CRDs are installed but not removed during uninstallation. 
By separating CRDs into their own chart, we can:

1. Allow proper version control of CRDs
2. Enable users to choose when to remove CRDs (which is destructive)
3. Follow Helm best practices
