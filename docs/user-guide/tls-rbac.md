# RBAC Configuration for ModelConfig TLS

This guide explains how to configure Role-Based Access Control (RBAC) permissions for agents that need to access Secrets containing TLS CA certificates.

## Table of Contents

- [Overview](#overview)
- [Why RBAC is Needed](#why-rbac-is-needed)
- [Quick Start](#quick-start)
- [Detailed Configuration](#detailed-configuration)
- [Security Best Practices](#security-best-practices)
- [Common Patterns](#common-patterns)
- [Troubleshooting](#troubleshooting)

## Overview

When you configure ModelConfig with TLS certificate references, the Kubernetes controller mounts the certificate Secrets as volumes in agent pods. For this to work, the agent's ServiceAccount must have **read** permissions on the referenced Secrets.

Without proper RBAC configuration, you'll see errors like:
```
Error: secrets "internal-ca-cert" is forbidden: User "system:serviceaccount:kagent:default"
cannot get resource "secrets" in API group "" in the namespace "kagent"
```

## Why RBAC is Needed

Kubernetes follows the **principle of least privilege**. By default:
- ServiceAccounts have minimal permissions
- Pods cannot read Secrets unless explicitly granted permission
- This prevents accidental or malicious access to sensitive data

For TLS configuration:
1. ModelConfig references a Secret containing a CA certificate
2. Controller creates a Deployment with a volume from that Secret
3. Kubernetes API validates that the ServiceAccount can read the Secret
4. If permission is denied, the pod fails to start

## Quick Start

### Minimal RBAC Configuration

Grant read access to a specific Secret:

```bash
# Create Role
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-tls-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["internal-ca-cert"]  # Specific Secret only
EOF

# Create RoleBinding
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-tls-cert-reader-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: default  # Or your custom ServiceAccount
    namespace: kagent
roleRef:
  kind: Role
  name: agent-tls-cert-reader
  apiGroup: rbac.authorization.k8s.io
EOF
```

### Verify Permissions

Test if the ServiceAccount can read the Secret:

```bash
kubectl auth can-i get secrets/internal-ca-cert \
  --as=system:serviceaccount:kagent:default \
  -n kagent
```

Expected output: `yes`

## Detailed Configuration

### Understanding the Components

**Role**: Defines what actions can be performed on which resources
- `apiGroups`: Empty string `""` for core Kubernetes resources (Secrets, ConfigMaps, etc.)
- `resources`: List of resource types (`secrets`)
- `verbs`: Actions allowed (`get`, `list`, `watch`, etc.)
- `resourceNames`: (Optional) Restrict to specific resource instances

**RoleBinding**: Associates a Role with subjects (users, groups, ServiceAccounts)
- `subjects`: Who gets the permissions
- `roleRef`: Which Role to bind

**ServiceAccount**: Identity for pods
- Each pod runs with a ServiceAccount
- Default: `default` ServiceAccount in the namespace
- Custom: Create dedicated ServiceAccounts for different agents

### Complete Example

```yaml
---
# Custom ServiceAccount for agents
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kagent-agent
  namespace: kagent

---
# Role granting read access to TLS certificate Secrets
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-tls-cert-reader
  namespace: kagent
rules:
  # Read access to specific Secrets containing CA certificates
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames:
      - internal-ca-cert
      - corporate-ca-bundle
      - litellm-ca-cert
      # Add other certificate Secrets as needed

---
# RoleBinding associating Role with ServiceAccount
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-tls-cert-reader-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: kagent-agent
    namespace: kagent
roleRef:
  kind: Role
  name: agent-tls-cert-reader
  apiGroup: rbac.authorization.k8s.io

---
# Agent using custom ServiceAccount
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: kagent
spec:
  framework: ADK
  modelConfigName: litellm-internal
  serviceAccountName: kagent-agent  # Reference custom ServiceAccount
```

## Security Best Practices

### DO

#### 1. Use Specific resourceNames

**Good** - Restrict to specific Secrets:
```yaml
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames:
      - internal-ca-cert
      - corporate-ca-bundle
```

**Bad** - Allow access to all Secrets:
```yaml
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    # No resourceNames - grants access to ALL Secrets!
```

#### 2. Use Minimal Verbs

Agents only need **read** access (`get` verb):
```yaml
verbs: ["get"]  # Read only
```

Avoid unnecessary permissions:
```yaml
verbs: ["get", "list", "watch", "create", "update", "delete"]  # Too broad!
```

#### 3. Use Namespace Isolation

Create separate namespaces for different environments:
```yaml
# Production namespace
apiVersion: v1
kind: Namespace
metadata:
  name: kagent-prod

---
# Development namespace
apiVersion: v1
kind: Namespace
metadata:
  name: kagent-dev
```

Roles and RoleBindings are namespace-scoped - they don't cross namespace boundaries.

#### 4. Use Dedicated ServiceAccounts

Create separate ServiceAccounts for different agent types:
```yaml
# ServiceAccount for internal agents
apiVersion: v1
kind: ServiceAccount
metadata:
  name: internal-agents
  namespace: kagent

---
# ServiceAccount for external agents
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-agents
  namespace: kagent
```

Assign different RBAC permissions to each ServiceAccount.

#### 5. Audit Permissions Regularly

List all RoleBindings in a namespace:
```bash
kubectl get rolebindings -n kagent
```

Review Role permissions:
```bash
kubectl describe role agent-tls-cert-reader -n kagent
```

Check which ServiceAccounts have access:
```bash
kubectl get rolebindings -n kagent -o json | \
  jq '.items[] | select(.subjects[].name == "default") | .metadata.name'
```

### DO NOT

#### 1. Don't Grant Cluster-Wide Access

**Bad** - ClusterRole with Secret access:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: agent-secrets  # Applies to ALL namespaces!
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
```

Use **Role** (namespace-scoped) instead of **ClusterRole** (cluster-wide).

#### 2. Don't Use Wildcards

**Bad** - Wildcard permissions:
```yaml
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["*"]
```

This grants access to everything in the namespace!

#### 3. Don't Share ServiceAccounts Across Environments

**Bad** - Same ServiceAccount in dev and prod:
```yaml
# Both dev and prod use "default" ServiceAccount
# If compromised in dev, prod is also affected
```

**Good** - Separate ServiceAccounts:
```yaml
# kagent-dev namespace uses "dev-agents" ServiceAccount
# kagent-prod namespace uses "prod-agents" ServiceAccount
```

#### 4. Don't Grant Write Permissions

Agents only need **read** access to Secrets. Never grant:
```yaml
verbs: ["create", "update", "patch", "delete"]  # Not needed!
```

## Common Patterns

### Pattern 1: Single Agent, Single Secret

Simplest case - one agent accessing one certificate Secret:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: my-agent-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["my-ca-cert"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: my-agent-cert-reader-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: default
    namespace: kagent
roleRef:
  kind: Role
  name: my-agent-cert-reader
  apiGroup: rbac.authorization.k8s.io
```

### Pattern 2: Multiple Agents, Shared Secret

Multiple agents in the same namespace sharing a certificate:

```yaml
# Single Role grants access to shared Secret
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: shared-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["shared-ca-cert"]

---
# All agents use the same ServiceAccount
# (or multiple RoleBindings for different ServiceAccounts)
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: all-agents-cert-reader
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: default
    namespace: kagent
roleRef:
  kind: Role
  name: shared-cert-reader
  apiGroup: rbac.authorization.k8s.io
```

### Pattern 3: Multiple Agents, Different Secrets

Different agents with different certificate requirements:

```yaml
# Agent 1 ServiceAccount and permissions
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: internal-agent
  namespace: kagent

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: internal-agent-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["internal-ca-cert"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: internal-agent-cert-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: internal-agent
    namespace: kagent
roleRef:
  kind: Role
  name: internal-agent-cert-reader
  apiGroup: rbac.authorization.k8s.io

---
# Agent 2 ServiceAccount and permissions
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-agent
  namespace: kagent

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: external-agent-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["external-ca-cert"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: external-agent-cert-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: external-agent
    namespace: kagent
roleRef:
  kind: Role
  name: external-agent-cert-reader
  apiGroup: rbac.authorization.k8s.io
```

### Pattern 4: Multiple Certificates Per Agent

Agent needs access to multiple certificate Secrets:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: multi-cert-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames:
      - primary-ca-cert
      - secondary-ca-cert
      - backup-ca-cert

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: multi-cert-reader-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: default
    namespace: kagent
roleRef:
  kind: Role
  name: multi-cert-reader
  apiGroup: rbac.authorization.k8s.io
```

## Troubleshooting

### Permission Denied Errors

**Error:**
```
Error creating pod: secrets "internal-ca-cert" is forbidden
```

**Diagnosis:**
```bash
# Check if permission exists
kubectl auth can-i get secrets/internal-ca-cert \
  --as=system:serviceaccount:kagent:default \
  -n kagent

# If output is "no", RBAC is missing or incorrect
```

**Solution:**
1. Verify Role exists and has correct resourceNames
2. Verify RoleBinding exists and references correct ServiceAccount
3. Verify ServiceAccount name matches pod's serviceAccountName

### RoleBinding Not Working

**Check RoleBinding subjects:**
```bash
kubectl get rolebinding <binding-name> -n kagent -o yaml
```

Verify:
- `subjects[].kind` is `ServiceAccount`
- `subjects[].name` matches your pod's ServiceAccount
- `subjects[].namespace` matches your pod's namespace

### Wrong Namespace

**Error:**
```
Error: secrets "internal-ca-cert" not found
```

**Diagnosis:**
```bash
# Check Secret namespace
kubectl get secret internal-ca-cert --all-namespaces

# Check ModelConfig namespace
kubectl get modelconfig <name> -n <namespace> -o jsonpath='{.metadata.namespace}'

# Check Agent namespace
kubectl get agent <name> -n <namespace> -o jsonpath='{.metadata.namespace}'
```

**Solution:**
Ensure Secret, ModelConfig, Agent, and RBAC resources are all in the **same namespace**.

### Permissions Too Broad

**Check what Secrets a ServiceAccount can access:**
```bash
# List all Secrets the ServiceAccount can read
kubectl auth can-i get secrets --as=system:serviceaccount:kagent:default -n kagent

# If output is "yes" without resourceNames restriction, permissions are too broad
```

**Solution:**
Update Role to include specific `resourceNames`:
```yaml
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["specific-secret-name"]  # Add this!
```

### Testing New RBAC Configuration

Before applying to production:

```bash
# 1. Create test namespace
kubectl create namespace kagent-test

# 2. Create Secret
kubectl create secret generic test-ca-cert \
  --from-file=ca.crt=test-cert.pem \
  -n kagent-test

# 3. Apply RBAC configuration
kubectl apply -f rbac.yaml -n kagent-test

# 4. Test permissions
kubectl auth can-i get secrets/test-ca-cert \
  --as=system:serviceaccount:kagent-test:default \
  -n kagent-test

# 5. Create test Agent
kubectl apply -f test-agent.yaml -n kagent-test

# 6. Check agent pod status
kubectl get pods -n kagent-test
kubectl logs -n kagent-test deployment/agent-<name>

# 7. Clean up
kubectl delete namespace kagent-test
```

## Security Considerations

### Namespace Isolation

Namespaces provide a security boundary:
- Roles are namespace-scoped
- RoleBindings only work within the same namespace
- Secrets cannot be accessed across namespaces

Use separate namespaces for:
- Different environments (dev, staging, prod)
- Different teams or tenants
- Different security zones (internal, external, DMZ)

### Secret Access Auditing

Enable Kubernetes audit logging to track Secret access:

```yaml
# Kubernetes audit policy
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  - level: RequestResponse
    resources:
      - group: ""
        resources: ["secrets"]
    verbs: ["get", "create", "update", "delete"]
```

Query audit logs:
```bash
# Example: Find who accessed a specific Secret
kubectl logs -n kube-system <kube-apiserver-pod> | \
  grep "internal-ca-cert" | \
  grep "secrets"
```

### Principle of Least Privilege

Always grant **minimum** permissions required:
- Use `resourceNames` to restrict to specific Secrets
- Use `verbs: ["get"]` only (read-only)
- Avoid `verbs: ["list", "watch"]` unless specifically needed
- Never use wildcard `*` in production

### Defense in Depth

RBAC is one layer of security. Also implement:
- **Network policies** to restrict pod communication
- **Pod security policies** to restrict pod capabilities
- **Secret encryption at rest** in etcd
- **External secret management** (Vault, AWS Secrets Manager, etc.)
- **Regular secret rotation**
- **Monitoring and alerting** for suspicious access

## Additional Resources

- [Kubernetes RBAC Documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [Using RBAC Authorization](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [Kubernetes Secrets Security Best Practices](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [ModelConfig TLS Configuration Guide](./modelconfig-tls.md)
- [SSL/TLS Troubleshooting Guide](../troubleshooting/ssl-errors.md)

## Next Steps

- Review your current RBAC configuration: `kubectl get roles,rolebindings -n kagent`
- Audit ServiceAccount permissions
- Implement namespace isolation for different environments
- Set up Secret rotation procedures
- Configure audit logging for Secret access
