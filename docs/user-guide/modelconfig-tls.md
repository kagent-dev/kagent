# ModelConfig TLS Configuration Guide

This guide explains how to configure SSL/TLS settings for ModelConfig resources to enable agents to connect to internal LiteLLM gateways or LLM providers that use self-signed certificates or custom certificate authorities.

## Table of Contents

- [Overview](#overview)
- [When Do You Need TLS Configuration?](#when-do-you-need-tls-configuration)
- [Prerequisites](#prerequisites)
- [Configuration Modes](#configuration-modes)
- [Step-by-Step Configuration](#step-by-step-configuration)
- [Complete Examples](#complete-examples)
- [Security Best Practices](#security-best-practices)
- [Troubleshooting](#troubleshooting)

## Overview

Kagent's ModelConfig CRD supports TLS configuration to enable agents to connect to LLM providers that use:
- Self-signed certificates
- Internal/corporate certificate authorities (CAs)
- Custom PKI infrastructure

By default, agents trust system CA certificates (the standard public CAs bundled with the operating system). The TLS configuration allows you to:
1. Add custom CA certificates in addition to system CAs
2. Use only custom CA certificates (disable system CAs)
3. Disable SSL verification entirely (development/testing only)

## When Do You Need TLS Configuration?

You need TLS configuration when:

- **Internal LiteLLM Gateway**: Your organization runs LiteLLM behind a firewall with self-signed certificates
- **Corporate PKI**: Your LLM provider uses certificates signed by your organization's internal CA
- **Development/Testing**: You want to quickly test without certificate management (not recommended for production)
- **Private Cloud**: Your cloud provider uses custom certificate authorities

You **do not need** TLS configuration when:
- Connecting to public LLM providers (OpenAI, Anthropic, Google, etc.)
- Your internal services use certificates signed by public CAs
- Your Kubernetes cluster's certificate management handles everything

## Prerequisites

Before configuring TLS, you need:

1. **CA Certificate File**: The root or intermediate CA certificate in PEM format that signed your server's certificate
2. **Kubernetes Secret**: A Secret in the same namespace as your ModelConfig to store the CA certificate
3. **RBAC Permissions**: Agent service accounts need read access to the Secret (see [RBAC Configuration](#rbac-configuration))

## Configuration Modes

ModelConfig supports three TLS modes:

### Mode 1: System CAs + Custom CA (Recommended)

**Use case**: Connect to both public services and internal services with custom certificates.

```yaml
spec:
  tls:
    caCertSecretRef: internal-ca-cert
    caCertSecretKey: ca.crt
    useSystemCAs: true  # Default
    verifyDisabled: false  # Default
```

- Trusts standard public CAs (Let's Encrypt, DigiCert, etc.)
- Also trusts your custom CA
- Most flexible and secure option

### Mode 2: Custom CA Only

**Use case**: Strict corporate environments where only internal CAs should be trusted.

```yaml
spec:
  tls:
    caCertSecretRef: internal-ca-cert
    caCertSecretKey: ca.crt
    useSystemCAs: false
    verifyDisabled: false
```

- Trusts only your custom CA certificate
- Rejects all public CA certificates
- More restrictive security posture

### Mode 3: Verification Disabled (Development Only)

**Use case**: Local development or testing environments only.

```yaml
spec:
  tls:
    verifyDisabled: true
```

**WARNING**: This disables all SSL verification. Agents will accept any certificate, including invalid or malicious ones. **Never use this in production.**

When this mode is enabled, prominent warnings will appear in agent logs:
```
============================================================
⚠️  SSL VERIFICATION DISABLED ⚠️
============================================================
SSL certificate verification is disabled.
This should ONLY be used in development/testing.
Production deployments MUST use proper certificates.
============================================================
```

## Step-by-Step Configuration

### Step 1: Obtain CA Certificate

Get the CA certificate that signed your server's certificate. The certificate must be in PEM format.

**Example PEM format:**
```
-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJAKL0UG+mRkmgMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
... (certificate content) ...
-----END CERTIFICATE-----
```

**How to obtain the certificate:**

1. **From your security team**: Contact your IT/security team for the internal CA certificate
2. **From server certificate**: Extract the CA from your server's certificate chain:
   ```bash
   openssl s_client -connect your-server.internal.corp:8080 -showcerts
   ```
3. **From certificate file**: If you have the CA certificate file, ensure it's in PEM format:
   ```bash
   openssl x509 -in ca-cert.crt -text -noout
   ```

### Step 2: Create Kubernetes Secret

Create a Secret containing the CA certificate in the same namespace as your ModelConfig:

```bash
kubectl create secret generic internal-ca-cert \
  --from-file=ca.crt=/path/to/ca-cert.pem \
  --namespace=kagent
```

**Verify the Secret was created:**
```bash
kubectl get secret internal-ca-cert -n kagent
```

**YAML equivalent:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: internal-ca-cert
  namespace: kagent
type: Opaque
stringData:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    MIIDXTCCAkWgAwIBAgIJAKL0UG+mRkmgMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
    ... (certificate content) ...
    -----END CERTIFICATE-----
```

**Certificate bundles** (multiple CAs):
If you need to trust multiple CA certificates, concatenate them in the same file:
```yaml
stringData:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    ... (root CA certificate) ...
    -----END CERTIFICATE-----
    -----BEGIN CERTIFICATE-----
    ... (intermediate CA certificate) ...
    -----END CERTIFICATE-----
```

### Step 3: Create ModelConfig with TLS Configuration

Create a ModelConfig that references the Secret:

```yaml
apiVersion: kagent.dev/v1alpha1
kind: ModelConfig
metadata:
  name: litellm-internal
  namespace: kagent
spec:
  provider: OpenAI  # LiteLLM presents an OpenAI-compatible API
  model: gpt-4
  apiKeySecretRef: litellm-api-key
  apiKeySecretKey: key
  openAI:
    baseUrl: https://litellm.internal.corp:8080
  tls:
    verifyDisabled: false
    caCertSecretRef: internal-ca-cert
    caCertSecretKey: ca.crt
    useSystemCAs: true
```

**Apply the ModelConfig:**
```bash
kubectl apply -f modelconfig.yaml
```

### Step 4: Create Agent Using ModelConfig

Create an Agent that references the ModelConfig:

```yaml
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: kagent
spec:
  framework: ADK
  modelConfigName: litellm-internal
  # ... other agent configuration ...
```

**Apply the Agent:**
```bash
kubectl apply -f agent.yaml
```

### Step 5: Verify Configuration

**Check agent pod logs:**
```bash
kubectl logs -n kagent deployment/agent-my-agent -f
```

Look for TLS configuration logs:
```
INFO: TLS Mode: Custom CA + System CAs (additive)
INFO: Using system CA certificates
INFO: Custom CA certificate loaded from: /etc/ssl/certs/custom/ca.crt
INFO: Certificate subject: CN=Internal CA,O=MyOrg,OU=IT
INFO: Certificate serial number: 0x1a2b3c4d5e6f
INFO: Certificate valid from 2024-01-01 00:00:00+00:00 to 2025-01-01 00:00:00+00:00
```

**Test connectivity:**
```bash
kubectl exec -n kagent deployment/agent-my-agent -- curl -v https://litellm.internal.corp:8080/health
```

## Complete Examples

### Example 1: Internal LiteLLM with Self-Signed Certificate

**Scenario**: LiteLLM running at `https://litellm.internal.corp:8080` with self-signed certificate.

```yaml
---
# Secret with CA certificate
apiVersion: v1
kind: Secret
metadata:
  name: litellm-ca-cert
  namespace: kagent
type: Opaque
stringData:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    MIIDXTCCAkWgAwIBAgIJAKL0UG+mRkmgMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
    BAYTAlVTMQswCQYDVQQIDAJDQTEWMBQGA1UEBwwNU2FuIEZyYW5jaXNjbzENMAsG
    A1UECgwETXlPcmcxDzANBgNVBAsMBlByaXZhdGUxDDAKBgNVBAMMA0NBMDAeFw0y
    ... (full certificate content) ...
    -----END CERTIFICATE-----

---
# Secret with LiteLLM API key
apiVersion: v1
kind: Secret
metadata:
  name: litellm-api-key
  namespace: kagent
type: Opaque
stringData:
  key: sk-litellm-1234567890abcdef

---
# ModelConfig with TLS configuration
apiVersion: kagent.dev/v1alpha1
kind: ModelConfig
metadata:
  name: litellm-internal
  namespace: kagent
spec:
  provider: OpenAI
  model: gpt-4
  apiKeySecretRef: litellm-api-key
  apiKeySecretKey: key
  openAI:
    baseUrl: https://litellm.internal.corp:8080
  tls:
    caCertSecretRef: litellm-ca-cert
    caCertSecretKey: ca.crt
    useSystemCAs: true
    verifyDisabled: false

---
# Agent using the ModelConfig
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: internal-agent
  namespace: kagent
spec:
  framework: ADK
  modelConfigName: litellm-internal
  card:
    name: internal-agent
    description: Agent using internal LiteLLM gateway
```

### Example 2: Development Environment with Verification Disabled

**Scenario**: Local testing environment where SSL verification is temporarily disabled.

```yaml
apiVersion: kagent.dev/v1alpha1
kind: ModelConfig
metadata:
  name: litellm-dev
  namespace: kagent-dev
spec:
  provider: OpenAI
  model: gpt-4
  apiKeySecretRef: litellm-api-key
  apiKeySecretKey: key
  openAI:
    baseUrl: https://localhost:8080
  tls:
    verifyDisabled: true  # ⚠️ Development only!
```

**Note**: Agents will log prominent warnings when verification is disabled.

### Example 3: Corporate PKI with Custom CA Only

**Scenario**: Enterprise environment with strict security policy - only trust corporate CAs.

```yaml
apiVersion: kagent.dev/v1alpha1
kind: ModelConfig
metadata:
  name: corporate-llm
  namespace: kagent
spec:
  provider: OpenAI
  model: gpt-4-corporate
  apiKeySecretRef: corporate-api-key
  apiKeySecretKey: key
  openAI:
    baseUrl: https://ai-platform.corp.internal:443
  tls:
    caCertSecretRef: corporate-ca-bundle
    caCertSecretKey: ca-bundle.crt
    useSystemCAs: false  # Only trust corporate CAs
    verifyDisabled: false
```

## Security Best Practices

### DO

- **Use Kubernetes Secrets** to store CA certificates securely
- **Enable verification** (`verifyDisabled: false`) in production environments
- **Rotate certificates** before expiration (Kagent logs warnings for expiring certificates)
- **Use namespace isolation** - Secrets should be in the same namespace as ModelConfig
- **Limit RBAC permissions** - Grant agents read-only access to specific Secrets
- **Monitor certificate expiry** - Check agent logs for expiration warnings
- **Use certificate bundles** when you need to trust multiple CAs

### DO NOT

- **Disable verification in production** - This bypasses all SSL security
- **Commit secrets to Git** - Use GitOps tools that support sealed secrets or external secret management
- **Share certificates across environments** - Use different certificates for dev, staging, and production
- **Use inline certificates in YAML** - Always use Kubernetes Secrets
- **Grant broad Secret access** - Use RBAC to limit which Secrets an agent can read

### Certificate Lifecycle

1. **Installation**: Create Secret, reference in ModelConfig
2. **Validation**: Agent validates certificate at startup (warnings only, non-blocking)
3. **Renewal**: Update Secret with new certificate, restart agent pods
4. **Expiration**: Agent logs warnings 30 days before expiration

**Certificate update workflow:**
```bash
# Update Secret with new certificate
kubectl create secret generic internal-ca-cert \
  --from-file=ca.crt=/path/to/new-ca-cert.pem \
  --namespace=kagent \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart agent pods to pick up new certificate
kubectl rollout restart deployment/agent-my-agent -n kagent
```

## RBAC Configuration

Agents need read access to Secrets containing CA certificates. Configure RBAC appropriately:

```yaml
---
# Role granting read access to specific Secret
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-secret-reader
  namespace: kagent
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["internal-ca-cert"]  # Specific secret only

---
# RoleBinding associating Role with agent ServiceAccount
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-secret-reader-binding
  namespace: kagent
subjects:
  - kind: ServiceAccount
    name: default  # Or your custom agent ServiceAccount
    namespace: kagent
roleRef:
  kind: Role
  name: agent-secret-reader
  apiGroup: rbac.authorization.k8s.io
```

**Security Note**: Limit Secret access to only the Secrets needed by each agent. Use `resourceNames` to grant access to specific Secrets rather than all Secrets in the namespace.

## Troubleshooting

If you encounter SSL/TLS errors, see:
- [SSL/TLS Troubleshooting Guide](../troubleshooting/ssl-errors.md) - Detailed debugging steps
- [ModelConfig API Reference](../api-reference/modelconfig.md) - Complete field documentation

**Common issues:**

1. **Certificate not found**: Verify Secret is mounted correctly
   ```bash
   kubectl exec deployment/agent-my-agent -- ls -la /etc/ssl/certs/custom/
   ```

2. **Certificate verification failed**: Check certificate chain and validity
   ```bash
   openssl verify -CAfile ca-cert.pem server-cert.pem
   ```

3. **Secret not accessible**: Verify RBAC permissions
   ```bash
   kubectl auth can-i get secrets/internal-ca-cert --as=system:serviceaccount:kagent:default -n kagent
   ```

For more troubleshooting steps, see the [SSL Errors Troubleshooting Guide](../troubleshooting/ssl-errors.md).

## Next Steps

- Learn about [Secret management best practices](./secret-management.md)
- Configure [multiple ModelConfigs with different TLS settings](./advanced-modelconfig.md)
- Set up [certificate rotation automation](./certificate-rotation.md)
- Read the [ModelConfig API reference](../api-reference/modelconfig.md)

## Related Resources

- [Kubernetes Secrets Documentation](https://kubernetes.io/docs/concepts/configuration/secret/)
- [OpenSSL Command Reference](https://www.openssl.org/docs/man1.1.1/man1/openssl.html)
- [TLS/SSL Best Practices](https://ssl-config.mozilla.org/)
