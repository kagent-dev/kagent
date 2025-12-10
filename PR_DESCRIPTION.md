## Fix: Apply Security Context Fields from Agent Spec to Generated Pods

Fixes #1083

### Problem

The kagent controller was not applying `runAsUser` and other security context fields from the Agent's `deployment.securityContext` and `deployment.podSecurityContext` to the generated pod specifications. This caused pods to fail with `CreateContainerConfigError` when container images have non-numeric users.

**Current Behavior**: Only `runAsNonRoot` and `allowPrivilegeEscalation` were being applied to pods, while `runAsUser`, `runAsGroup`, `fsGroup`, and `capabilities` were ignored.

**Expected Behavior**: All security context fields from the Agent spec should be properly propagated to the pod template.

### Solution

This PR adds support for both pod-level and container-level security contexts in the Agent API and ensures they are properly propagated to generated pods and containers.

### Changes Made

#### 1. API Changes (`go/api/v1alpha2/agent_types.go`)
- Added `SecurityContext *corev1.SecurityContext` to `SharedDeploymentSpec` for container-level security context
- Added `PodSecurityContext *corev1.PodSecurityContext` to `SharedDeploymentSpec` for pod-level security context

#### 2. Internal Struct Updates (`go/internal/controller/translator/agent/adk_api_translator.go`)
- Added `SecurityContext` and `PodSecurityContext` fields to the `resolvedDeployment` struct

#### 3. Resolver Functions
- **`resolveInlineDeployment`**: Now copies `SecurityContext` and `PodSecurityContext` from the Agent spec
- **`resolveByoDeployment`**: Now copies `SecurityContext` and `PodSecurityContext` from the Agent spec

#### 4. Manifest Building (`buildManifest` function)
- **Pod-level security context**: `PodSecurityContext` from the Agent spec is applied to `PodSpec.securityContext`, which includes fields like:
  - `runAsUser`, `runAsGroup`, `runAsNonRoot`
  - `fsGroup`, `supplementalGroups`
  - `seLinuxOptions`, `seccompProfile`
  
- **Container-level security context**: `SecurityContext` from the Agent spec is applied to container `SecurityContext`, which includes fields like:
  - `runAsUser`, `runAsGroup`, `runAsNonRoot`
  - `capabilities`, `allowPrivilegeEscalation`
  - `readOnlyRootFilesystem`, `privileged`
  
- **Sandbox compatibility**: When `needSandbox` is `true` (for skills or code execution), the `Privileged` flag is set appropriately while preserving user-provided security context settings

- **Init containers**: Security context is also applied to init containers (e.g., skills-init container)

#### 5. Code Generation
- Ran `make generate` to update the generated deepcopy methods for the new fields

### How It Works

1. **Pod-level security context**: The `podSecurityContext` field from the Agent spec is directly applied to `PodSpec.securityContext`, affecting all containers in the pod.

2. **Container-level security context**: The `securityContext` field from the Agent spec is applied to each container's `SecurityContext`. When sandbox mode is required (for skills or code execution), the `Privileged` flag is merged with user-provided settings.

3. **Priority**: User-provided security context settings take precedence, with sandbox requirements merged in when necessary.

### Testing

**Unit Tests**:
- Verified that security context fields are properly copied in resolver functions
- Confirmed that security context is correctly applied to pod and container specs in manifest building

**Manual Testing**:
- Verified that pods are created successfully with `runAsUser` specified (e.g., `runAsUser: 1000`)
- Confirmed that security context fields (`runAsUser`, `runAsGroup`, `fsGroup`, `capabilities`) are properly applied to both main containers and init containers
- Tested sandbox mode compatibility (skills and code execution) with custom security contexts
- Validated that `CreateContainerConfigError` is resolved when container images have non-numeric users
- Verified that both `podSecurityContext` and `securityContext` from Agent spec are correctly propagated to pod template

**Code Quality**:
- Ran `make lint` to ensure code style compliance
- All existing tests pass

### Documentation

- API changes are self-documenting through the CRD schema
- No additional documentation updates required as this fixes existing functionality

### Checklist

- [x] Code follows project style guidelines (Go Code Review Comments)
- [x] Ran `make lint` and fixed any issues
- [x] Ran `make generate` to update generated code
- [x] Changes are tested and verified
- [x] All commits are signed off (DCO)

### Related Issues

- Fixes #1083 - Controller not applying runAsUser from Agent securityContext to pod containers
- Resolves `CreateContainerConfigError` when container images have non-numeric users or require specific security context configurations

