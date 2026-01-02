# OpenShift Deployment Guide

This guide covers OpenShift-specific deployment considerations for kagent.

## Prerequisites

- OpenShift 4.12+
- `oc` CLI configured
- Helm 3.x

## Installation

```bash
# Create namespace
oc new-project kagent

# Install CRDs
helm install kagent-crds ./helm/kagent-crds/ -n kagent

# Install kagent with OpenShift Route
helm install kagent ./helm/kagent/ -n kagent \
  --set providers.openAI.apiKey=$OPENAI_API_KEY \
  --set openshift.enabled=true
```

## Security Context Constraints

kagent runs with `restricted-v2` SCC by default. No special SCCs required.

For custom SCCs:

```bash
# View current SCC
oc get pod -n kagent -o yaml | grep -A5 securityContext

# Grant specific SCC (if needed)
oc adm policy add-scc-to-user anyuid -z kagent-controller -n kagent
```

## Routes

The Helm chart creates an OpenShift Route when `openshift.enabled=true`:

```yaml
# values.yaml
openshift:
  enabled: true
  route:
    host: kagent.apps.example.com  # optional
    tls:
      termination: edge
```

Access the UI:

```bash
oc get route kagent-ui -n kagent -o jsonpath='{.spec.host}'
```

## Pod Security Standards

kagent is compatible with PSS `restricted` profile:

| Setting | Value |
|---------|-------|
| `runAsNonRoot` | true |
| `allowPrivilegeEscalation` | false |
| `capabilities.drop` | ALL |
| `seccompProfile` | RuntimeDefault |

## High Availability

```yaml
# values-ha.yaml
controller:
  replicas: 2

ui:
  replicas: 2

pdb:
  enabled: true
  controller:
    minAvailable: 1
  ui:
    minAvailable: 1
```

```bash
helm upgrade kagent ./helm/kagent/ -n kagent -f values-ha.yaml
```

## Troubleshooting

```bash
# Check pod status
oc get pods -n kagent

# Check SCC violations
oc get events -n kagent | grep -i scc

# View controller logs
oc logs -l app.kubernetes.io/component=controller -n kagent

# Check Route status
oc describe route kagent-ui -n kagent
```

## See Also

- [Installation Guide](https://kagent.dev/docs/kagent/introduction/installation)
- [Helm Chart README](../helm/README.md)
