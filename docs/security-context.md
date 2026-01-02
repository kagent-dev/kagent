# Security Context Configuration

This document describes how kagent handles security contexts for agent deployments, including compatibility with OpenShift Security Context Constraints (SCC) and Kubernetes Pod Security Standards (PSS).

## Overview

kagent allows users to configure security contexts at both the pod and container levels through the Agent CRD. These settings are passed through to the generated Kubernetes Deployment, enabling compliance with various security policies.

## Configuration Options

### Pod Security Context

Configure pod-level security settings via `spec.declarative.deployment.podSecurityContext`:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: secure-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "A secure agent"
    modelConfig: my-model
    deployment:
      podSecurityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
        seccompProfile:
          type: RuntimeDefault
```

### Container Security Context

Configure container-level security settings via `spec.declarative.deployment.securityContext`:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: secure-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "A secure agent"
    modelConfig: my-model
    deployment:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
        seccompProfile:
          type: RuntimeDefault
```

## OpenShift SCC Compatibility

kagent supports deployment under various OpenShift Security Context Constraints. Below is a compatibility matrix showing which SCCs can be used with different kagent configurations.

### SCC Compatibility Matrix

| SCC | Basic Agent | Agent with Skills | Notes |
|-----|-------------|-------------------|-------|
| `restricted` | Yes | No | Most secure, requires UID range allocation |
| `restricted-v2` | Yes | No | Updated restricted with seccomp requirements |
| `anyuid` | Yes | No | Allows specific UID without range restrictions |
| `nonroot` | Yes | No | Must run as non-root user |
| `nonroot-v2` | Yes | No | Updated nonroot with seccomp requirements |
| `privileged` | Yes | Yes | Required for agents with skills/sandbox |

### Restricted SCC Configuration

For OpenShift's `restricted` or `restricted-v2` SCC:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: restricted-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "Agent running under restricted SCC"
    modelConfig: my-model
    deployment:
      podSecurityContext:
        runAsNonRoot: true
        # OpenShift will assign UID from namespace range
        fsGroup: 1000680000  # Use your namespace's allocated range
        seccompProfile:
          type: RuntimeDefault
      securityContext:
        runAsNonRoot: true
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
        seccompProfile:
          type: RuntimeDefault
```

### Anyuid SCC Configuration

For OpenShift's `anyuid` SCC (when you need a specific UID):

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: anyuid-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "Agent running under anyuid SCC"
    modelConfig: my-model
    deployment:
      podSecurityContext:
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
      securityContext:
        runAsUser: 1000
        runAsGroup: 1000
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
```

### Privileged SCC Configuration

For agents that require skills or sandboxed code execution:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: privileged-agent
spec:
  type: Declarative
  skills:
    refs:
      - my-skill:latest
  declarative:
    systemMessage: "Agent with skills requiring privileged SCC"
    modelConfig: my-model
    deployment:
      podSecurityContext:
        runAsUser: 0
        runAsGroup: 0
```

**Important**: When an agent uses skills, kagent automatically sets `privileged: true` on the container security context to enable sandbox functionality.

## Pod Security Standards (PSS) Compliance

kagent supports Kubernetes Pod Security Standards at all levels. The table below shows compatibility:

### PSS Compatibility Matrix

| PSS Level | Basic Agent | Agent with Skills | Notes |
|-----------|-------------|-------------------|-------|
| `privileged` | Yes | Yes | No restrictions |
| `baseline` | Yes | No | Prevents known privilege escalations |
| `restricted` | Yes | No | Highly restrictive, current best practices |

### PSS Restricted Profile Configuration

For namespaces enforcing the `restricted` PSS profile:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: pss-restricted-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "PSS restricted compliant agent"
    modelConfig: my-model
    deployment:
      podSecurityContext:
        runAsNonRoot: true
        runAsUser: 65534  # nobody user
        runAsGroup: 65534
        fsGroup: 65534
        seccompProfile:
          type: RuntimeDefault
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        runAsGroup: 65534
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
        seccompProfile:
          type: RuntimeDefault
      # Add volumes for writable paths when using readOnlyRootFilesystem
      volumes:
        - name: tmp
          emptyDir: {}
        - name: cache
          emptyDir: {}
      volumeMounts:
        - name: tmp
          mountPath: /tmp
        - name: cache
          mountPath: /cache
```

### PSS Baseline Profile Configuration

For namespaces enforcing the `baseline` PSS profile:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: pss-baseline-agent
spec:
  type: Declarative
  declarative:
    systemMessage: "PSS baseline compliant agent"
    modelConfig: my-model
    deployment:
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
          add:
            - NET_BIND_SERVICE  # Allowed in baseline
```

## Sandbox and Privileged Mode

When an agent uses skills (via `spec.skills.refs`), kagent automatically enables privileged mode for the container. This is required for:

- Container-based sandbox execution
- Isolated code execution environments
- Skill package installation and execution

The privileged mode is applied regardless of user-provided security context settings, though other security context fields (like `runAsUser`) are preserved.

### Implications

1. **OpenShift**: Requires the `privileged` SCC to be assigned to the service account
2. **Kubernetes PSS**: Cannot run in namespaces with `restricted` or `baseline` enforcement
3. **Security**: Carefully review which agents require skills and limit their use in production

## Best Practices

1. **Start Restricted**: Begin with the most restrictive settings and relax only as needed
2. **Use Non-Root**: Always prefer running as non-root (`runAsNonRoot: true`)
3. **Drop Capabilities**: Drop all capabilities and add back only what's needed
4. **Enable Seccomp**: Use `RuntimeDefault` seccomp profile for defense in depth
5. **Read-Only Filesystem**: Use `readOnlyRootFilesystem: true` with explicit writable volume mounts
6. **Limit Skills Usage**: Only use skills when necessary, as they require privileged mode
7. **Namespace Isolation**: Run privileged agents in dedicated namespaces with appropriate PSS/SCC policies

## Troubleshooting

### Common Issues

**Pod fails to start with "container has runAsNonRoot and image will run as root"**
- Ensure both `runAsNonRoot: true` and a non-zero `runAsUser` are set

**Pod fails with SCC/PSS policy violations**
- Check the namespace's PSS enforcement level: `kubectl get ns <namespace> -o yaml`
- For OpenShift, verify SCC assignments: `oc get scc` and `oc adm policy who-can use scc/<scc-name>`

**Agent with skills fails to start**
- Verify the privileged SCC is available (OpenShift)
- Ensure the namespace allows privileged pods (Kubernetes PSS)

### Verification Commands

```bash
# Check which SCC a pod is using (OpenShift)
oc get pod <pod-name> -o yaml | grep scc

# Check namespace PSS enforcement (Kubernetes)
kubectl get ns <namespace> -o yaml | grep pod-security

# Describe pod for security context issues
kubectl describe pod <pod-name>
```

## References

- [Kubernetes Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [OpenShift Security Context Constraints](https://docs.openshift.com/container-platform/latest/authentication/managing-security-context-constraints.html)
- [Kubernetes Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
