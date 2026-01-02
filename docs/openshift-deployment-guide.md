# OpenShift Deployment Guide

This guide covers deploying kagent on Red Hat OpenShift Container Platform. OpenShift provides additional security features and enterprise capabilities on top of Kubernetes, which require specific configuration considerations.

## Table of Contents

- [Prerequisites](#prerequisites)
- [OpenShift vs Kubernetes: Key Differences](#openshift-vs-kubernetes-key-differences)
- [Security Context Constraints (SCCs)](#security-context-constraints-sccs)
  - [Understanding SCCs](#understanding-sccs)
  - [Configuring kagent for SCCs](#configuring-kagent-for-sccs)
  - [Creating Custom SCCs](#creating-custom-sccs)
- [Pod Security Standards (PSS)](#pod-security-standards-pss)
  - [restricted-v2 Compatibility](#restricted-v2-compatibility)
  - [Configuring Security Contexts](#configuring-security-contexts)
- [Routes vs Ingress](#routes-vs-ingress)
  - [Automatic Route Creation](#automatic-route-creation)
  - [Manual Route Configuration](#manual-route-configuration)
  - [TLS Configuration](#tls-configuration)
- [Namespace Patterns and Multi-Tenancy](#namespace-patterns-and-multi-tenancy)
  - [Single-Namespace Deployment](#single-namespace-deployment)
  - [Multi-Namespace Deployment](#multi-namespace-deployment)
  - [Project-Based Isolation](#project-based-isolation)
- [Deployment Scenarios](#deployment-scenarios)
  - [Basic Deployment](#basic-deployment)
  - [Production Deployment](#production-deployment)
  - [Air-Gapped Deployment](#air-gapped-deployment)
- [Troubleshooting](#troubleshooting)

## Prerequisites

Before deploying kagent on OpenShift, ensure you have:

- OpenShift Container Platform 4.12 or later
- `oc` CLI installed and configured
- Cluster-admin or project-admin privileges (depending on deployment scope)
- Helm 3.x installed
- Access to container images (either from `cr.kagent.dev` or your internal registry)

## OpenShift vs Kubernetes: Key Differences

OpenShift extends Kubernetes with additional security and operational features:

| Feature | Kubernetes | OpenShift |
|---------|-----------|-----------|
| Ingress | Ingress resources | Routes (native), Ingress (supported) |
| Security | Pod Security Standards | Security Context Constraints (SCCs) |
| Projects | Namespaces | Projects (namespaces with additional annotations) |
| Registry | External registries | Built-in integrated registry |
| Networking | CNI plugins | OpenShift SDN or OVN-Kubernetes |

## Security Context Constraints (SCCs)

### Understanding SCCs

Security Context Constraints (SCCs) are OpenShift's mechanism for controlling pod security. They define what actions pods can perform and what resources they can access. SCCs are more granular than Kubernetes Pod Security Standards.

OpenShift includes several built-in SCCs:

| SCC | Description | Use Case |
|-----|-------------|----------|
| `restricted-v2` | Most restrictive, default for authenticated users | Standard workloads |
| `restricted` | Legacy restricted policy | Backward compatibility |
| `nonroot-v2` | Allows any non-root UID | Workloads needing specific UIDs |
| `hostnetwork` | Allows host networking | Networking components |
| `privileged` | Full access | System components only |

### Configuring kagent for SCCs

The kagent Helm chart supports security context configuration through `values.yaml`. For most OpenShift deployments, the default `restricted-v2` SCC works well with proper security context settings.

To verify which SCC your pods are using:

```bash
oc get pod -n kagent -o yaml | grep scc
```

Configure security contexts in your `values.yaml`:

```yaml
# Pod-level security context (applies to all containers in the pod)
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Container-level security context
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Controller-specific security context (optional override)
controller:
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
    runAsNonRoot: true
```

### Creating Custom SCCs

If your deployment requires capabilities not provided by built-in SCCs, create a custom SCC:

```yaml
apiVersion: security.openshift.io/v1
kind: SecurityContextConstraints
metadata:
  name: kagent-scc
allowHostDirVolumePlugin: false
allowHostIPC: false
allowHostNetwork: false
allowHostPID: false
allowHostPorts: false
allowPrivilegeEscalation: false
allowPrivilegedContainer: false
allowedCapabilities: []
defaultAddCapabilities: []
fsGroup:
  type: MustRunAs
readOnlyRootFilesystem: false
requiredDropCapabilities:
  - ALL
runAsUser:
  type: MustRunAsNonRoot
seLinuxContext:
  type: MustRunAs
supplementalGroups:
  type: RunAsAny
volumes:
  - configMap
  - downwardAPI
  - emptyDir
  - persistentVolumeClaim
  - projected
  - secret
users:
  - system:serviceaccount:kagent:kagent-controller
  - system:serviceaccount:kagent:kagent-ui
```

Apply the custom SCC:

```bash
oc apply -f kagent-scc.yaml
```

## Pod Security Standards (PSS)

### restricted-v2 Compatibility

OpenShift 4.12+ uses the `restricted-v2` SCC by default, which aligns with the Kubernetes Pod Security Standards `restricted` profile. The kagent Helm chart is designed to be compatible with this profile.

Key requirements for `restricted-v2` compatibility:

1. **Non-root execution**: Containers must run as non-root users
2. **Read-only root filesystem**: Recommended but not required
3. **No privilege escalation**: `allowPrivilegeEscalation: false`
4. **Dropped capabilities**: All capabilities should be dropped
5. **Seccomp profile**: Must use `RuntimeDefault` or `Localhost`

### Configuring Security Contexts

For full `restricted-v2` compliance, use this configuration:

```yaml
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
```

Note: If using SQLite storage (the default), the controller needs write access to the SQLite volume. The chart mounts this as an emptyDir, which works with read-only root filesystem.

## Routes vs Ingress

### Automatic Route Creation

The kagent Helm chart automatically creates an OpenShift Route when deployed on OpenShift. This is detected via the `route.openshift.io/v1` API availability.

The Route is created for the UI service with the following defaults:

- TLS termination: Edge
- Insecure traffic: Redirect to HTTPS

To verify the Route was created:

```bash
oc get route -n kagent
```

### Manual Route Configuration

If you need custom Route configuration, you can disable the automatic Route and create your own:

First, examine the auto-generated Route:

```bash
oc get route kagent-ui -n kagent -o yaml
```

Create a custom Route:

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: kagent-ui
  namespace: kagent
  labels:
    app.kubernetes.io/name: kagent
    app.kubernetes.io/component: ui
spec:
  host: kagent.apps.your-cluster.example.com
  to:
    kind: Service
    name: kagent-ui
    weight: 100
  port:
    targetPort: 8080
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
  wildcardPolicy: None
```

### TLS Configuration

For production deployments, configure TLS certificates:

**Using cluster default certificate (edge termination):**

```yaml
spec:
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
```

**Using custom certificate:**

```yaml
spec:
  tls:
    termination: edge
    certificate: |
      -----BEGIN CERTIFICATE-----
      ...
      -----END CERTIFICATE-----
    key: |
      -----BEGIN RSA PRIVATE KEY-----
      ...
      -----END RSA PRIVATE KEY-----
    caCertificate: |
      -----BEGIN CERTIFICATE-----
      ...
      -----END CERTIFICATE-----
    insecureEdgeTerminationPolicy: Redirect
```

**Passthrough to application TLS:**

```yaml
spec:
  tls:
    termination: passthrough
```

## Namespace Patterns and Multi-Tenancy

### Single-Namespace Deployment

The simplest deployment pattern puts all kagent components in a single namespace:

```bash
# Create project (OpenShift's enhanced namespace)
oc new-project kagent

# Install CRDs (cluster-scoped, requires cluster-admin)
helm install kagent-crds ./helm/kagent-crds -n kagent

# Install kagent
helm install kagent ./helm/kagent -n kagent \
  --set providers.openAI.apiKey=$OPENAI_API_KEY
```

### Multi-Namespace Deployment

For larger deployments, you may want to separate components:

```yaml
# values-multi-namespace.yaml

# Override namespace for CRDs (installed separately)
namespaceOverride: ""

# Controller watches specific namespaces
controller:
  watchNamespaces:
    - kagent-agents-dev
    - kagent-agents-staging
    - kagent-agents-prod
```

Deploy agents in separate namespaces:

```bash
# Create agent namespaces
oc new-project kagent-agents-dev
oc new-project kagent-agents-staging
oc new-project kagent-agents-prod

# Label namespaces for controller discovery
oc label namespace kagent-agents-dev kagent.dev/managed=true
oc label namespace kagent-agents-staging kagent.dev/managed=true
oc label namespace kagent-agents-prod kagent.dev/managed=true
```

### Project-Based Isolation

OpenShift Projects provide additional isolation through:

1. **Network Policies**: Automatic network isolation between projects
2. **RBAC**: Project-scoped roles and bindings
3. **Resource Quotas**: Per-project resource limits

Configure network policies to allow kagent controller communication:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-kagent-controller
  namespace: kagent-agents-dev
spec:
  podSelector: {}
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kagent
```

## Deployment Scenarios

### Basic Deployment

For development or testing environments:

```bash
# Create project
oc new-project kagent

# Install CRDs
helm install kagent-crds ./helm/kagent-crds -n kagent

# Install with minimal configuration
helm install kagent ./helm/kagent -n kagent \
  --set providers.default=ollama \
  --set providers.ollama.config.host=ollama.kagent.svc.cluster.local:11434
```

### Production Deployment

For production environments, consider these settings:

```yaml
# values-production.yaml

# Use PostgreSQL for persistence
database:
  type: postgres
  postgres:
    url: postgres://user:pass@postgresql.kagent.svc.cluster.local:5432/kagent

# Security hardening
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Resource limits
controller:
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 2
      memory: 1Gi
  replicas: 2

ui:
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: 1
      memory: 512Mi
  replicas: 2

# Enable observability
otel:
  tracing:
    enabled: true
    exporter:
      otlp:
        endpoint: http://jaeger-collector.observability.svc.cluster.local:4317
        insecure: true
```

Deploy with production values:

```bash
helm install kagent ./helm/kagent -n kagent \
  -f values-production.yaml \
  --set providers.openAI.apiKeySecretRef=kagent-openai
```

### Air-Gapped Deployment

For disconnected or air-gapped environments:

1. **Mirror images to internal registry:**

```bash
# Pull images
podman pull cr.kagent.dev/kagent-dev/kagent/controller:latest
podman pull cr.kagent.dev/kagent-dev/kagent/ui:latest
podman pull cr.kagent.dev/kagent-dev/kagent/app:latest

# Tag for internal registry
podman tag cr.kagent.dev/kagent-dev/kagent/controller:latest \
  registry.internal.example.com/kagent/controller:latest

# Push to internal registry
podman push registry.internal.example.com/kagent/controller:latest
```

2. **Configure Helm values for internal registry:**

```yaml
# values-airgapped.yaml

registry: registry.internal.example.com/kagent

imagePullSecrets:
  - name: internal-registry-secret

controller:
  image:
    registry: registry.internal.example.com
    repository: kagent/controller

ui:
  image:
    registry: registry.internal.example.com
    repository: kagent/ui
```

3. **Create image pull secret:**

```bash
oc create secret docker-registry internal-registry-secret \
  --docker-server=registry.internal.example.com \
  --docker-username=user \
  --docker-password=password \
  -n kagent
```

## Troubleshooting

### Common Issues

**1. Pods stuck in CrashLoopBackOff with permission errors**

This usually indicates SCC issues. Check which SCC is being used:

```bash
oc get pod -n kagent -o yaml | grep scc
oc adm policy who-can use scc restricted-v2
```

**2. Route not accessible**

Verify the Route is created and has an assigned host:

```bash
oc get route -n kagent
oc describe route kagent-ui -n kagent
```

Check if the router pods are running:

```bash
oc get pods -n openshift-ingress
```

**3. Controller cannot watch other namespaces**

Ensure proper RBAC is configured for cross-namespace access:

```bash
oc get clusterrolebinding | grep kagent
oc auth can-i list agents --as=system:serviceaccount:kagent:kagent-controller -n other-namespace
```

**4. Image pull errors in air-gapped environment**

Verify the image pull secret is correctly configured:

```bash
oc get secret internal-registry-secret -n kagent
oc describe pod <pod-name> -n kagent | grep -A5 "Events"
```

### Useful Commands

```bash
# View all kagent resources
oc get all -n kagent

# Check pod security context
oc get pod <pod-name> -n kagent -o jsonpath='{.spec.securityContext}'

# View SCC for a specific service account
oc adm policy who-can use scc restricted-v2 -n kagent

# Debug network connectivity
oc debug pod/<pod-name> -n kagent

# View controller logs
oc logs -f deployment/kagent-controller -n kagent

# View UI logs
oc logs -f deployment/kagent-ui -n kagent
```

### Getting Help

If you encounter issues:

1. Check the [kagent documentation](https://kagent.dev/docs/kagent)
2. Search existing [GitHub issues](https://github.com/kagent-dev/kagent/issues)
3. Join the [CNCF Slack #kagent channel](https://cloud-native.slack.com/archives/C08ETST0076)
4. Join the [kagent Discord server](https://discord.gg/Fu3k65f2k3)
