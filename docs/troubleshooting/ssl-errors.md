# SSL/TLS Errors Troubleshooting Guide

This guide helps you diagnose and resolve SSL/TLS certificate errors when configuring ModelConfig resources to connect to LLM providers with custom certificates.

## Table of Contents

- [Quick Diagnosis](#quick-diagnosis)
- [Common Error Messages](#common-error-messages)
- [Diagnostic Commands](#diagnostic-commands)
- [Step-by-Step Debugging](#step-by-step-debugging)
- [Environment-Specific Issues](#environment-specific-issues)
- [Certificate Chain Issues](#certificate-chain-issues)
- [Getting Help](#getting-help)

## Quick Diagnosis

If you're seeing SSL/TLS errors in your agent logs, start here:

### 1. Check Agent Logs

```bash
kubectl logs -n <namespace> deployment/agent-<name> --tail=100
```

Look for error messages containing:
- `SSL`, `TLS`, `certificate`, `verify failed`, `handshake`
- Stack traces from `ssl.SSLError` or `httpx.ConnectError`

### 2. Verify Secret is Mounted

```bash
kubectl exec -n <namespace> deployment/agent-<name> -- ls -la /etc/ssl/certs/custom/
```

Expected output:
```
total 8
drwxrwxrwt 3 root root  120 Jan 15 10:30 .
drwxr-xr-x 3 root root 4096 Jan 15 10:30 ..
drwxr-xr-x 2 root root   80 Jan 15 10:30 ..data
lrwxrwxrwx 1 root root   15 Jan 15 10:30 ca.crt -> ..data/ca.crt
```

### 3. Check Environment Variables

```bash
kubectl exec -n <namespace> deployment/agent-<name> -- env | grep TLS
```

Expected output:
```
TLS_CA_CERT_PATH=/etc/ssl/certs/custom/ca.crt
TLS_VERIFY_DISABLED=false
TLS_USE_SYSTEM_CAS=true
```

### 4. Test Certificate Format

```bash
kubectl exec -n <namespace> deployment/agent-<name> -- \
  openssl x509 -in /etc/ssl/certs/custom/ca.crt -text -noout
```

If this command fails, your certificate is not in valid PEM format.

## Common Error Messages

### Error: "certificate verify failed: self signed certificate"

**Symptom:**
```
ssl.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self signed certificate (_ssl.c:1131)
```

**Cause**: Agent is trying to connect to a server with a self-signed certificate, but the CA certificate is not trusted.

**Solutions:**

1. **Verify CA certificate is mounted:**
   ```bash
   kubectl exec deployment/agent-<name> -n <namespace> -- cat /etc/ssl/certs/custom/ca.crt
   ```

2. **Verify ModelConfig TLS configuration:**
   ```bash
   kubectl get modelconfig <name> -n <namespace> -o yaml
   ```

   Check that:
   - `spec.tls.caCertSecretRef` matches your Secret name
   - `spec.tls.caCertSecretKey` matches the key in your Secret (usually `ca.crt`)
   - `spec.tls.verifyDisabled` is `false` (or not set)

3. **Verify Secret contains correct CA certificate:**
   ```bash
   kubectl get secret <secret-name> -n <namespace> -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -text -noout
   ```

4. **Verify the server certificate is actually signed by the CA:**
   ```bash
   # Get server certificate
   openssl s_client -connect <server>:<port> -showcerts 2>/dev/null | \
     openssl x509 -outform PEM > /tmp/server-cert.pem

   # Verify against CA
   openssl verify -CAfile ca-cert.pem /tmp/server-cert.pem
   ```

### Error: "certificate verify failed: unable to get local issuer certificate"

**Symptom:**
```
ssl.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: unable to get local issuer certificate (_ssl.c:1131)
```

**Cause**: The certificate chain is incomplete. The server's certificate is signed by an intermediate CA, but that intermediate CA certificate is not trusted.

**Solutions:**

1. **Get the complete certificate chain:**
   ```bash
   openssl s_client -connect <server>:<port> -showcerts
   ```

   Save all certificates in the chain (including intermediate CAs) to your Secret.

2. **Create certificate bundle with all certificates:**
   ```bash
   # Combine root CA + intermediate CA certificates
   cat intermediate-ca.crt root-ca.crt > ca-bundle.crt

   # Update Secret
   kubectl create secret generic <secret-name> \
     --from-file=ca.crt=ca-bundle.crt \
     --namespace=<namespace> \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

3. **Restart agent to pick up new certificate:**
   ```bash
   kubectl rollout restart deployment/agent-<name> -n <namespace>
   ```

### Error: "certificate has expired"

**Symptom:**
```
ssl.SSLError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: certificate has expired (_ssl.c:1131)
```

**Cause**: The CA certificate or server certificate has expired.

**Solutions:**

1. **Check certificate expiry:**
   ```bash
   # Check CA certificate
   kubectl exec deployment/agent-<name> -n <namespace> -- \
     openssl x509 -in /etc/ssl/certs/custom/ca.crt -noout -dates

   # Check server certificate
   openssl s_client -connect <server>:<port> 2>/dev/null | \
     openssl x509 -noout -dates
   ```

2. **Update with new certificate:**
   ```bash
   # Update Secret with renewed certificate
   kubectl create secret generic <secret-name> \
     --from-file=ca.crt=/path/to/new-ca-cert.pem \
     --namespace=<namespace> \
     --dry-run=client -o yaml | kubectl apply -f -

   # Restart agent
   kubectl rollout restart deployment/agent-<name> -n <namespace>
   ```

3. **Check system clock:**
   ```bash
   kubectl exec deployment/agent-<name> -n <namespace> -- date
   ```

   If the pod's clock is wrong, the certificate may appear expired when it's not.

### Error: "certificate is not yet valid"

**Symptom:**
```
WARNING: Certificate is not yet valid until 2025-06-01 00:00:00+00:00
```

**Cause**: The certificate's `notBefore` date is in the future, or the system clock is incorrect.

**Solutions:**

1. **Check certificate validity period:**
   ```bash
   kubectl exec deployment/agent-<name> -n <namespace> -- \
     openssl x509 -in /etc/ssl/certs/custom/ca.crt -noout -dates
   ```

2. **Check system clock:**
   ```bash
   kubectl exec deployment/agent-<name> -n <namespace> -- date
   ```

   Compare with actual time. If incorrect, check NTP configuration on Kubernetes nodes.

### Error: "Secret not found"

**Symptom:**
```
Error: Secret "internal-ca-cert" not found
```

**Cause**: The Secret referenced in ModelConfig does not exist or is in a different namespace.

**Solutions:**

1. **Verify Secret exists:**
   ```bash
   kubectl get secret <secret-name> -n <namespace>
   ```

2. **Check Secret is in the same namespace as ModelConfig:**
   ```bash
   kubectl get modelconfig <name> -n <namespace> -o jsonpath='{.metadata.namespace}'
   kubectl get secret <secret-name> -n <namespace>
   ```

   Secrets must be in the same namespace as the ModelConfig.

3. **Create the Secret:**
   ```bash
   kubectl create secret generic <secret-name> \
     --from-file=ca.crt=/path/to/ca-cert.pem \
     --namespace=<namespace>
   ```

### Error: "permission denied" accessing Secret

**Symptom:**
```
Error: secrets "internal-ca-cert" is forbidden: User "system:serviceaccount:<namespace>:default" cannot get resource "secrets"
```

**Cause**: The agent's ServiceAccount does not have permission to read the Secret.

**Solutions:**

1. **Check current permissions:**
   ```bash
   kubectl auth can-i get secrets/<secret-name> \
     --as=system:serviceaccount:<namespace>:<service-account> \
     -n <namespace>
   ```

2. **Create RBAC permissions:**
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: Role
   metadata:
     name: agent-secret-reader
     namespace: <namespace>
   rules:
     - apiGroups: [""]
       resources: ["secrets"]
       verbs: ["get"]
       resourceNames: ["<secret-name>"]
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: RoleBinding
   metadata:
     name: agent-secret-reader-binding
     namespace: <namespace>
   subjects:
     - kind: ServiceAccount
       name: default  # Or your custom ServiceAccount
       namespace: <namespace>
   roleRef:
     kind: Role
     name: agent-secret-reader
     apiGroup: rbac.authorization.k8s.io
   ```

3. **Apply RBAC configuration:**
   ```bash
   kubectl apply -f rbac.yaml
   ```

### Error: "hostname 'localhost' doesn't match"

**Symptom:**
```
ssl.CertificateError: hostname 'localhost' doesn't match certificate hostname 'server.example.com'
```

**Cause**: Certificate's Common Name (CN) or Subject Alternative Names (SANs) do not match the hostname in the URL.

**Solutions:**

1. **Check certificate hostnames:**
   ```bash
   openssl s_client -connect <server>:<port> 2>/dev/null | \
     openssl x509 -noout -text | grep -A1 "Subject Alternative Name"
   ```

2. **Update baseUrl to match certificate:**
   ```yaml
   spec:
     openAI:
       baseUrl: https://server.example.com:8080  # Must match certificate CN/SAN
   ```

3. **Regenerate certificate with correct SANs:**
   ```bash
   # Generate certificate with SANs for localhost and IP
   openssl req -x509 -newkey rsa:4096 -nodes \
     -keyout server-key.pem -out server-cert.pem \
     -days 365 -subj "/CN=localhost" \
     -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
   ```

## Diagnostic Commands

### Inspect Certificate Details

```bash
# View certificate subject, issuer, and validity
kubectl exec deployment/agent-<name> -n <namespace> -- \
  openssl x509 -in /etc/ssl/certs/custom/ca.crt -text -noout

# Check certificate validity dates only
kubectl exec deployment/agent-<name> -n <namespace> -- \
  openssl x509 -in /etc/ssl/certs/custom/ca.crt -noout -dates

# Check certificate subject
kubectl exec deployment/agent-<name> -n <namespace> -- \
  openssl x509 -in /etc/ssl/certs/custom/ca.crt -noout -subject

# Check certificate issuer
kubectl exec deployment/agent-<name> -n <namespace> -- \
  openssl x509 -in /etc/ssl/certs/custom/ca.crt -noout -issuer
```

### Test Server Certificate

```bash
# Connect to server and show certificate chain
openssl s_client -connect <server>:<port> -showcerts

# Connect and verify with custom CA
openssl s_client -connect <server>:<port> \
  -CAfile ca-cert.pem -verify 5

# Check server certificate expiry
echo | openssl s_client -connect <server>:<port> 2>/dev/null | \
  openssl x509 -noout -dates

# Check certificate chain depth
openssl s_client -connect <server>:<port> -showcerts 2>/dev/null | \
  grep -c "BEGIN CERTIFICATE"
```

### Verify Secret Contents

```bash
# Get Secret data (base64 encoded)
kubectl get secret <secret-name> -n <namespace> -o yaml

# Decode and view certificate
kubectl get secret <secret-name> -n <namespace> \
  -o jsonpath='{.data.ca\.crt}' | base64 -d

# Decode and parse certificate
kubectl get secret <secret-name> -n <namespace> \
  -o jsonpath='{.data.ca\.crt}' | base64 -d | \
  openssl x509 -text -noout
```

### Test Connectivity

```bash
# Test HTTPS connection from agent pod
kubectl exec deployment/agent-<name> -n <namespace> -- \
  curl -v https://<server>:<port>/health

# Test with custom CA certificate
kubectl exec deployment/agent-<name> -n <namespace> -- \
  curl --cacert /etc/ssl/certs/custom/ca.crt \
  https://<server>:<port>/health

# Test ignoring certificate verification (debugging only)
kubectl exec deployment/agent-<name> -n <namespace> -- \
  curl -k https://<server>:<port>/health
```

### Check Agent Configuration

```bash
# View ModelConfig TLS configuration
kubectl get modelconfig <name> -n <namespace> -o yaml | grep -A10 "tls:"

# Check if Secret is referenced correctly
kubectl get modelconfig <name> -n <namespace> \
  -o jsonpath='{.spec.tls.caCertSecretRef}'

# View agent deployment volumes
kubectl get deployment agent-<name> -n <namespace> \
  -o jsonpath='{.spec.template.spec.volumes}' | jq

# Check volume mounts
kubectl get deployment agent-<name> -n <namespace> \
  -o jsonpath='{.spec.template.spec.containers[0].volumeMounts}' | jq
```

## Step-by-Step Debugging

Use this checklist when debugging SSL/TLS issues:

### Step 1: Verify Basic Configuration

- [ ] ModelConfig exists and has `spec.tls` configuration
- [ ] Secret exists in same namespace as ModelConfig
- [ ] `spec.tls.caCertSecretRef` matches Secret name exactly
- [ ] `spec.tls.caCertSecretKey` matches key in Secret (e.g., `ca.crt`)
- [ ] Agent pod is running (not in CrashLoopBackOff)

### Step 2: Verify Certificate Accessibility

- [ ] Secret is mounted in agent pod at `/etc/ssl/certs/custom/`
- [ ] Certificate file exists: `kubectl exec ... -- cat /etc/ssl/certs/custom/ca.crt`
- [ ] Certificate is in PEM format (starts with `-----BEGIN CERTIFICATE-----`)
- [ ] Environment variables are set: `TLS_CA_CERT_PATH`, `TLS_VERIFY_DISABLED`, `TLS_USE_SYSTEM_CAS`

### Step 3: Verify Certificate Validity

- [ ] Certificate is not expired: `openssl x509 ... -noout -dates`
- [ ] Certificate is currently valid (not "not yet valid")
- [ ] Certificate can be parsed by openssl without errors
- [ ] Certificate subject and issuer look correct

### Step 4: Verify Certificate Chain

- [ ] Server certificate is signed by the CA in your Secret
- [ ] Intermediate certificates are included if needed (certificate bundle)
- [ ] Certificate chain can be verified: `openssl verify -CAfile ca-cert.pem server-cert.pem`

### Step 5: Verify Server Connectivity

- [ ] Server is reachable from agent pod: `kubectl exec ... -- curl -k https://server/health`
- [ ] Server is using expected certificate
- [ ] Certificate CN/SAN matches hostname in baseUrl
- [ ] Port is correct in baseUrl

### Step 6: Verify RBAC Permissions

- [ ] Agent ServiceAccount can read the Secret
- [ ] RBAC permissions are correctly configured
- [ ] Test with: `kubectl auth can-i get secrets/<name> --as=system:serviceaccount:...`

### Step 7: Check Application Logs

- [ ] Agent logs show TLS configuration at startup
- [ ] No error messages about missing certificates
- [ ] TLS mode is correct (Custom CA, System CAs, etc.)
- [ ] No SSL handshake errors

## Environment-Specific Issues

### Development/Local Kubernetes

**Issue**: Certificate verification fails in local dev clusters (kind, minikube, etc.)

**Solution**: Use verification disabled mode for local development only:
```yaml
spec:
  tls:
    verifyDisabled: true  # Development only!
```

### AWS EKS

**Issue**: EKS nodes have limited system CAs, some internal CAs not trusted

**Solution**: Use `useSystemCAs: false` and provide complete certificate chain:
```yaml
spec:
  tls:
    caCertSecretRef: complete-ca-chain
    caCertSecretKey: ca-bundle.crt
    useSystemCAs: false
```

### GKE

**Issue**: GKE uses custom kernel that may handle certificates differently

**Solution**: Ensure certificate is in correct PEM format with Unix line endings (LF, not CRLF):
```bash
dos2unix ca-cert.pem
```

### Azure AKS

**Issue**: AKS clusters may have time synchronization issues affecting certificate validity

**Solution**: Check system time in pods and on nodes:
```bash
# Check pod time
kubectl exec deployment/agent-<name> -- date

# Compare with actual time
date
```

## Certificate Chain Issues

### Understanding Certificate Chains

A complete certificate chain includes:
1. **Root CA**: Self-signed, trusted by client
2. **Intermediate CA(s)**: Signed by root or another intermediate
3. **Server Certificate**: Signed by intermediate or root

### Viewing Certificate Chain

```bash
# Get all certificates from server
openssl s_client -connect <server>:<port> -showcerts 2>/dev/null

# Count certificates in chain
openssl s_client -connect <server>:<port> -showcerts 2>/dev/null | \
  grep -c "BEGIN CERTIFICATE"
```

### Creating Certificate Bundle

If you need multiple CA certificates:

```bash
# Combine into bundle (order: intermediate(s) first, then root)
cat intermediate-ca.crt root-ca.crt > ca-bundle.crt

# Verify bundle format
openssl crl2pkcs7 -nocrl -certfile ca-bundle.crt | \
  openssl pkcs7 -print_certs -text -noout
```

## Getting Help

If you're still stuck after following this guide:

1. **Collect diagnostic information:**
   ```bash
   # Save all diagnostics to a file
   kubectl get modelconfig <name> -n <namespace> -o yaml > modelconfig.yaml
   kubectl get secret <secret-name> -n <namespace> -o yaml > secret.yaml
   kubectl logs deployment/agent-<name> -n <namespace> --tail=200 > agent-logs.txt
   kubectl describe deployment agent-<name> -n <namespace> > agent-describe.txt
   ```

2. **Gather certificate information:**
   ```bash
   # Get server certificate
   openssl s_client -connect <server>:<port> -showcerts 2>/dev/null > server-certs.txt

   # Get CA certificate from Secret
   kubectl get secret <secret-name> -n <namespace> \
     -o jsonpath='{.data.ca\.crt}' | base64 -d > ca-from-secret.crt
   ```

3. **Test certificate verification manually:**
   ```bash
   # Extract server cert
   openssl s_client -connect <server>:<port> 2>/dev/null | \
     openssl x509 -outform PEM > server-cert.pem

   # Verify with CA
   openssl verify -CAfile ca-from-secret.crt server-cert.pem
   ```

4. **Open an issue** with the collected information:
   - ModelConfig YAML (redact sensitive data)
   - Error messages from agent logs
   - Certificate verification output
   - Kubernetes version and distribution (EKS, GKE, etc.)

## Additional Resources

- [User Configuration Guide](../user-guide/modelconfig-tls.md) - Complete TLS configuration guide
- [ModelConfig API Reference](../api-reference/modelconfig.md) - Field documentation
- [OpenSSL Cookbook](https://www.feistyduck.com/library/openssl-cookbook/) - Certificate management
- [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/) - Secret management
- [TLS Debugging Guide](https://wiki.openssl.org/index.php/Debugging) - Advanced TLS debugging
