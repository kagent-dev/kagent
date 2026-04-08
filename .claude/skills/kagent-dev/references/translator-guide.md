# Translator Guide - When and How to Update Translators

This guide explains the translator pattern in kagent and when you need to update translators after CRD changes.

## What is a Translator?

Translators convert Agent CRDs into Kubernetes resources (Deployment, Service, Secret, ConfigMap, etc.). The main translator for agents is `adk_api_translator.go`.

**Path:** `go/core/internal/controller/translator/agent/adk_api_translator.go`

**Key method:** `translateInlineAgent()` - converts an Agent CR to:
- ADK config (JSON)
- Deployment manifest
- Service manifest
- Secret (containing config)
- PVC (if needed for skills)

## When to Update the Translator

Update the translator when your CRD field needs to:

1. **Affect Kubernetes resources** - Field changes how Deployment, Service, ConfigMap, etc. are created
2. **Affect agent runtime config** - Field goes into the ADK config.json
3. **Be validated at translation time** - Field requires special processing/validation

**Don't update translator if:**
- Field is only for status/metadata
- Field is processed elsewhere (e.g., by another controller)
- Field doesn't affect generated resources

## Common Translation Patterns

### Pattern 1: Add Environment Variable

```go
// Field affects container environment
func (a *adkApiTranslator) translateInlineAgent(...) {
    env := []corev1.EnvVar{
        // existing env vars...
    }

    // Add from new field
    if agent.Spec.Declarative.NewEnvField != nil {
        env = append(env, corev1.EnvVar{
            Name:  "NEW_ENV_VAR",
            Value: *agent.Spec.Declarative.NewEnvField,
        })
    }

    // ... use env in container spec
}
```

### Pattern 2: Modify Deployment Spec

```go
// Field affects pod/container configuration
func (a *adkApiTranslator) translateInlineAgent(...) {
    deployment := &appsv1.Deployment{
        // ... existing spec
    }

    // Apply resource limits from new field
    if agent.Spec.Declarative.Resources != nil {
        deployment.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
            Requests: corev1.ResourceList{
                corev1.ResourceCPU: resource.MustParse(agent.Spec.Declarative.Resources.CPU),
            },
        }
    }

    return deployment
}
```

### Pattern 3: Update ADK Config

```go
// Field goes into agent runtime config
func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    config := &adk.AgentConfig{
        Model: modelConfig,
        // ... existing fields
    }

    // Add from CRD field
    if agent.Spec.Declarative.Timeout != nil {
        config.Timeout = *agent.Spec.Declarative.Timeout
    }

    return config, ...
}
```

### Pattern 4: Create/Modify Service

```go
// Field affects service configuration
func (a *adkApiTranslator) translateInlineAgent(...) {
    service := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      agent.Name,
            Namespace: agent.Namespace,
        },
        Spec: corev1.ServiceSpec{
            Selector: labels,
            Ports: []corev1.ServicePort{
                {
                    Name: "http",
                    Port: 8080,
                },
            },
        },
    }

    // Custom service type from CRD
    if agent.Spec.Declarative.ServiceType != nil {
        service.Spec.Type = corev1.ServiceType(*agent.Spec.Declarative.ServiceType)
    }

    return service
}
```

### Pattern 5: Conditional Resource Creation

```go
// Field controls whether resources are created
func (a *adkApiTranslator) translateInlineAgent(...) {
    var pvc *corev1.PersistentVolumeClaim

    // Only create PVC if storage is requested
    if agent.Spec.Declarative.Storage != nil {
        pvc = &corev1.PersistentVolumeClaim{
            ObjectMeta: metav1.ObjectMeta{
                Name:      agent.Name + "-storage",
                Namespace: agent.Namespace,
            },
            Spec: corev1.PersistentVolumeClaimSpec{
                AccessModes: []corev1.PersistentVolumeAccessMode{
                    corev1.ReadWriteOnce,
                },
                Resources: corev1.VolumeResourceRequirements{
                    Requests: corev1.ResourceList{
                        corev1.ResourceStorage: resource.MustParse(*agent.Spec.Declarative.Storage),
                    },
                },
            },
        }
    }

    return pvc
}
```

## Template Resolution

If your field uses Go templates (referencing ConfigMaps), you need to update `template.go`:

**Path:** `go/core/internal/controller/translator/agent/template.go`

```go
// If field supports template resolution
func (a *adkApiTranslator) resolveTemplates(ctx context.Context, agent *v1alpha2.Agent) error {
    // Resolve systemMessage with templates
    if agent.Spec.Declarative.PromptTemplate != nil {
        resolved, err := a.templateResolver.Resolve(ctx, agent)
        if err != nil {
            return err
        }
        agent.Spec.Declarative.SystemMessage = resolved
    }

    return nil
}
```

## Testing Translator Changes

### 1. Update Golden Files

Translator tests use golden files to verify output.

**Location:** `go/core/internal/controller/translator/agent/testdata/`

**Input fixtures:** `testdata/inputs/*.yaml` - sample Agent CRDs
**Expected outputs:** `testdata/outputs/*.json` - expected translated configs

**Regenerate after changes:**
```bash
UPDATE_GOLDEN=true make -C go test
```

**Review changes:**
```bash
git diff go/core/internal/controller/translator/agent/testdata/outputs/
```

### 2. Add Test Case (if needed)

If your field requires a new test scenario:

```go
// go/core/internal/controller/translator/agent/adk_api_translator_test.go

func TestTranslateAgentWithNewField(t *testing.T) {
    agent := &v1alpha2.Agent{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-agent",
            Namespace: "default",
        },
        Spec: v1alpha2.AgentSpec{
            Declarative: &v1alpha2.DeclarativeAgentSpec{
                NewField: pointer.String("test-value"),
                // ... other required fields
            },
        },
    }

    translator := newAdkApiTranslator(...)
    config, deployment, err := translator.translateInlineAgent(context.Background(), agent)

    require.NoError(t, err)
    assert.Equal(t, "test-value", config.NewField)
    // ... verify deployment has expected changes
}
```

### 3. Add E2E Test

E2E tests verify end-to-end behavior with real cluster:

```go
// go/core/test/e2e/invoke_api_test.go

func TestE2EAgentWithNewField(t *testing.T) {
    agent := &v1alpha2.Agent{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-agent-" + randString(),
            Namespace: "kagent",
        },
        Spec: v1alpha2.AgentSpec{
            Declarative: &v1alpha2.DeclarativeAgentSpec{
                NewField: pointer.String("test-value"),
                ModelConfig: v1alpha2.TypedLocalReference{
                    Name: "test-model-config",
                },
            },
        },
    }

    // Create agent
    require.NoError(t, k8sClient.Create(ctx, agent))
    defer k8sClient.Delete(ctx, agent)

    // Wait for deployment
    require.Eventually(t, func() bool {
        deployment := &appsv1.Deployment{}
        err := k8sClient.Get(ctx, types.NamespacedName{
            Name:      agent.Name,
            Namespace: agent.Namespace,
        }, deployment)
        if err != nil {
            return false
        }

        // Verify new field affected deployment
        return deployment.Spec.Template.Spec.Containers[0].Env[0].Value == "test-value"
    }, 30*time.Second, 1*time.Second)
}
```

## Common Issues

### Golden Files Mismatch

**Symptom:** Translator tests fail with diff output

**Cause:** Generated manifests don't match golden files

**Fix:**
1. Regenerate: `UPDATE_GOLDEN=true make -C go test`
2. Review diff: `git diff go/core/internal/controller/translator/agent/testdata/outputs/`
3. If expected, commit the new golden files
4. If unexpected, fix translator logic

### Field Not Appearing in Deployment

**Symptom:** Field is set in Agent CR but doesn't appear in created Deployment

**Debugging:**
```bash
# 1. Check translator is using the field
grep -n "NewField" go/core/internal/controller/translator/agent/adk_api_translator.go

# 2. Check generated secret contains field
kubectl get secret -n kagent test-agent -o jsonpath='{.data.config\.json}' | base64 -d | jq

# 3. Check deployment has expected changes
kubectl get deployment -n kagent test-agent -o yaml | grep -A 5 env
```

**Common causes:**
- Forgot to use field in translator
- Used wrong field path (e.g., `agent.Spec.NewField` instead of `agent.Spec.Declarative.NewField`)
- Forgot to run `make -C go generate` after adding field to types

### Template Resolution Errors

**Symptom:** Agent status shows `Accepted=False` with template error

**Cause:** Template syntax error or missing ConfigMap

**Debugging:**
```bash
# Check Accepted condition
kubectl get agent my-agent -n kagent -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'

# Check if ConfigMap exists
kubectl get configmap -n kagent kagent-builtin-prompts

# Check controller logs
kubectl logs -n kagent deployment/kagent-controller | grep template
```

**Common causes:**
- ConfigMap doesn't exist in namespace
- Template syntax error (e.g., `{{include "missing/path"}}`)
- Helm template syntax (`{{ }}`) in CRD doc comments (don't do this!)

## Translator Architecture

### Key Components

1. **adkApiTranslator** - Main translator struct
   - Translates Agent CR → Kubernetes resources
   - Handles template resolution
   - Generates ADK config

2. **templateResolver** - Resolves Go templates in systemMessage
   - Fetches ConfigMaps
   - Substitutes variables (AgentName, ToolNames, etc.)
   - Returns resolved string

3. **modelTranslator** - Translates ModelConfig CR → ADK model config
   - Resolves provider credentials
   - Handles provider-specific config

### Data Flow

```
Agent CR (with new field)
    ↓
adkApiTranslator.translateInlineAgent()
    ↓
┌─────────────────────────────────────┐
│ 1. Resolve templates                │
│ 2. Translate model config           │
│ 3. Build ADK config (with new field)│
│ 4. Create Deployment (with new field│
│ 5. Create Service                   │
│ 6. Create Secret (config.json)      │
└─────────────────────────────────────┘
    ↓
Kubernetes resources applied by controller
    ↓
Agent pod starts with new configuration
```

## Best Practices

1. **Handle nil values** - Always check if pointer fields are nil before dereferencing
   ```go
   if agent.Spec.Declarative.NewField != nil {
       use(*agent.Spec.Declarative.NewField)
   }
   ```

2. **Preserve existing behavior** - New fields should be additive, not change existing logic
   ```go
   // Good: defaults preserve existing behavior
   timeout := 30 // default
   if agent.Spec.Declarative.Timeout != nil {
       timeout = *agent.Spec.Declarative.Timeout
   }

   // Bad: changes existing behavior
   timeout := *agent.Spec.Declarative.Timeout // panics if nil!
   ```

3. **Use constants for magic strings**
   ```go
   const (
       DefaultTimeout = 30
       EnvVarTimeout  = "AGENT_TIMEOUT"
   )

   if agent.Spec.Declarative.Timeout != nil {
       env = append(env, corev1.EnvVar{
           Name:  EnvVarTimeout,
           Value: strconv.Itoa(*agent.Spec.Declarative.Timeout),
       })
   }
   ```

4. **Error handling** - Return errors, don't panic
   ```go
   // Good
   if agent.Spec.Declarative.Resources != nil {
       cpu, err := resource.ParseQuantity(*agent.Spec.Declarative.Resources.CPU)
       if err != nil {
           return nil, nil, nil, fmt.Errorf("invalid CPU quantity: %w", err)
       }
       // ... use cpu
   }

   // Bad
   cpu := resource.MustParse(*agent.Spec.Declarative.Resources.CPU) // panics on error!
   ```

5. **Update golden files** - Always regenerate and review before committing
   ```bash
   UPDATE_GOLDEN=true make -C go test
   git diff go/core/internal/controller/translator/agent/testdata/outputs/
   ```

## Complete Example

For a full worked example (adding a field end-to-end through types, codegen, translator, golden files, and E2E test), see `references/crd-workflow-detailed.md`.
