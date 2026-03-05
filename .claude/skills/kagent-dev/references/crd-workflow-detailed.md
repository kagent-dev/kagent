# CRD Workflow - Detailed Examples

This guide provides detailed examples of adding different types of fields to kagent CRDs.

## Table of Contents

- [Simple String Field](#simple-string-field)
- [Enum Field](#enum-field)
- [Nested Struct](#nested-struct)
- [Array Field](#array-field)
- [Map Field](#map-field)
- [Pointer vs Value Types](#pointer-vs-value-types)
- [Validation Markers](#validation-markers)

---

## Simple String Field

**Use case:** Adding a simple optional string field.

```go
// go/api/v1alpha2/agent_types.go

type DeclarativeAgentSpec struct {
    // Existing fields...

    // Description provides a human-readable description of the agent
    // +optional
    // +kubebuilder:validation:MaxLength=500
    Description *string `json:"description,omitempty"`
}
```

**Translator usage:**
```go
// go/core/internal/controller/translator/agent/adk_api_translator.go

func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    config := &adk.AgentConfig{
        // ... existing fields
    }

    // Use the field if set
    if agent.Spec.Declarative.Description != nil {
        config.Description = *agent.Spec.Declarative.Description
    }

    return config, ...
}
```

**E2E test:**
```go
// go/core/test/e2e/invoke_api_test.go

func TestE2EAgentWithDescription(t *testing.T) {
    agent := &v1alpha2.Agent{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-agent-" + randString(),
            Namespace: "kagent",
        },
        Spec: v1alpha2.AgentSpec{
            Description: "Test agent for validating description field",
            Declarative: &v1alpha2.DeclarativeAgentSpec{
                Description: pointer.String("An agent that tells jokes"),
                ModelConfig: v1alpha2.TypedLocalReference{
                    Name: "test-model-config",
                },
                // ... other required fields
            },
        },
    }

    // Create agent
    require.NoError(t, client.Create(ctx, agent))
    defer client.Delete(ctx, agent)

    // Wait for ready
    require.Eventually(t, func() bool {
        err := client.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, agent)
        return err == nil && agent.Status.Ready
    }, 60*time.Second, 1*time.Second)

    // Verify description is in generated config
    secret := &corev1.Secret{}
    err := client.Get(ctx, types.NamespacedName{
        Name:      agent.Name,
        Namespace: agent.Namespace,
    }, secret)
    require.NoError(t, err)

    config := &adk.AgentConfig{}
    require.NoError(t, json.Unmarshal(secret.Data["config.json"], config))
    assert.Equal(t, "An agent that tells jokes", config.Description)
}
```

---

## Enum Field

**Use case:** Adding a field with a fixed set of allowed values.

```go
// go/api/v1alpha2/common_types.go

// LogLevel represents the logging verbosity level
// +kubebuilder:validation:Enum=debug;info;warn;error
type LogLevel string

const (
    LogLevelDebug LogLevel = "debug"
    LogLevelInfo  LogLevel = "info"
    LogLevelWarn  LogLevel = "warn"
    LogLevelError LogLevel = "error"
)

// go/api/v1alpha2/agent_types.go

type DeclarativeAgentSpec struct {
    // Existing fields...

    // LogLevel sets the agent's logging verbosity
    // +optional
    // +kubebuilder:default="info"
    LogLevel *LogLevel `json:"logLevel,omitempty"`
}
```

**Translator usage:**
```go
func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    // Set environment variable for log level
    env := []corev1.EnvVar{
        // ... existing env vars
    }

    logLevel := "info" // default
    if agent.Spec.Declarative.LogLevel != nil {
        logLevel = string(*agent.Spec.Declarative.LogLevel)
    }
    env = append(env, corev1.EnvVar{
        Name:  "LOG_LEVEL",
        Value: logLevel,
    })

    // ... use in deployment
}
```

**Example usage in Agent YAML:**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  declarative:
    logLevel: debug  # Must be one of: debug, info, warn, error
    # ... other fields
```

---

## Nested Struct

**Use case:** Adding a complex field with multiple sub-fields.

```go
// go/api/v1alpha2/agent_types.go

// ResourceRequirements specifies CPU and memory requests/limits
type ResourceRequirements struct {
    // CPU request (e.g., "100m", "1")
    // +optional
    // +kubebuilder:validation:Pattern=^[0-9]+m?$
    CPURequest *string `json:"cpuRequest,omitempty"`

    // Memory request (e.g., "128Mi", "1Gi")
    // +optional
    // +kubebuilder:validation:Pattern=^[0-9]+(Mi|Gi)$
    MemoryRequest *string `json:"memoryRequest,omitempty"`

    // CPU limit
    // +optional
    CPULimit *string `json:"cpuLimit,omitempty"`

    // Memory limit
    // +optional
    MemoryLimit *string `json:"memoryLimit,omitempty"`
}

type DeclarativeAgentSpec struct {
    // Existing fields...

    // Resources specifies resource requests and limits for the agent pod
    // +optional
    Resources *ResourceRequirements `json:"resources,omitempty"`
}
```

**Translator usage:**
```go
func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    container := corev1.Container{
        Name:  "agent",
        Image: agentImage,
    }

    // Apply resource requirements if specified
    if agent.Spec.Declarative.Resources != nil {
        res := agent.Spec.Declarative.Resources
        container.Resources = corev1.ResourceRequirements{
            Requests: corev1.ResourceList{},
            Limits:   corev1.ResourceList{},
        }

        if res.CPURequest != nil {
            q, err := resource.ParseQuantity(*res.CPURequest)
            if err != nil {
                return nil, fmt.Errorf("invalid CPU request: %w", err)
            }
            container.Resources.Requests[corev1.ResourceCPU] = q
        }
        if res.MemoryRequest != nil {
            q, err := resource.ParseQuantity(*res.MemoryRequest)
            if err != nil {
                return nil, fmt.Errorf("invalid memory request: %w", err)
            }
            container.Resources.Requests[corev1.ResourceMemory] = q
        }
        if res.CPULimit != nil {
            q, err := resource.ParseQuantity(*res.CPULimit)
            if err != nil {
                return nil, fmt.Errorf("invalid CPU limit: %w", err)
            }
            container.Resources.Limits[corev1.ResourceCPU] = q
        }
        if res.MemoryLimit != nil {
            q, err := resource.ParseQuantity(*res.MemoryLimit)
            if err != nil {
                return nil, fmt.Errorf("invalid memory limit: %w", err)
            }
            container.Resources.Limits[corev1.ResourceMemory] = q
        }
    }

    // ... use container in deployment
}
```

**Example usage:**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  declarative:
    resources:
      cpuRequest: "500m"
      memoryRequest: "512Mi"
      cpuLimit: "1"
      memoryLimit: "1Gi"
```

---

## Array Field

**Use case:** Adding a field that holds multiple values.

```go
// go/api/v1alpha2/agent_types.go

type DeclarativeAgentSpec struct {
    // Existing fields...

    // Tags are arbitrary key-value pairs for categorizing the agent
    // +optional
    // +kubebuilder:validation:MaxItems=10
    Tags []string `json:"tags,omitempty"`
}
```

**Important:** For arrays, don't use pointer types. Use `[]Type` not `*[]Type`. The `omitempty` tag handles the optional case.

**Translator usage:**
```go
func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    labels := map[string]string{
        "app": "kagent",
        "agent": agent.Name,
    }

    // Add tags as labels
    for i, tag := range agent.Spec.Declarative.Tags {
        // Sanitize tag for use as label
        sanitized := sanitizeLabelValue(tag)
        labels[fmt.Sprintf("tag-%d", i)] = sanitized
    }

    // ... use labels in deployment
}
```

**With validation for items:**
```go
// Tags are lowercase alphanumeric strings
// +optional
// +kubebuilder:validation:MaxItems=10
// +kubebuilder:validation:items:Pattern=^[a-z0-9-]+$
Tags []string `json:"tags,omitempty"`
```

---

## Map Field

**Use case:** Adding a field with arbitrary key-value pairs.

```go
// go/api/v1alpha2/agent_types.go

type DeclarativeAgentSpec struct {
    // Existing fields...

    // Annotations are arbitrary metadata for the agent
    // +optional
    Annotations map[string]string `json:"annotations,omitempty"`
}
```

**Important:** For maps, don't use pointer types. Use `map[K]V` not `*map[K]V`.

**Translator usage:**
```go
func (a *adkApiTranslator) translateInlineAgent(...) (*adk.AgentConfig, ...) {
    deployment := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      agent.Name,
            Namespace: agent.Namespace,
            Annotations: agent.Spec.Declarative.Annotations, // Pass through directly
        },
        // ... rest of deployment
    }

    return deployment
}
```

**Example usage:**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  declarative:
    annotations:
      team: "platform"
      environment: "production"
      version: "1.0.0"
```

---

## Pointer vs Value Types

### When to Use Pointers

Use pointer types (`*Type`) for:

1. **Optional primitive fields** (string, int, bool)
   - Distinguishes between "not set" and "set to zero/empty value"
   - Example: `*string` can be nil (not set) or `""` (set to empty)

2. **Optional struct fields**
   - Entire nested structure is optional
   - Example: `*ResourceRequirements`

### When to Use Value Types

Use value types (`Type`) for:

1. **Required fields**
   ```go
   // Name is required
   // +kubebuilder:validation:Required
   Name string `json:"name"`
   ```

2. **Arrays and maps** (even if optional)
   ```go
   // +optional
   Tags []string `json:"tags,omitempty"`

   // +optional
   Annotations map[string]string `json:"annotations,omitempty"`
   ```

3. **Fields with meaningful zero values**
   ```go
   // Replicas defaults to 1 if not set
   // +optional
   // +kubebuilder:default=1
   Replicas int32 `json:"replicas,omitempty"`
   ```

### Example Comparison

```go
// BAD: Can't distinguish "not set" from "set to false"
Enabled bool `json:"enabled,omitempty"`

// GOOD: nil = not set, false = explicitly disabled
Enabled *bool `json:"enabled,omitempty"`

// BAD: Unnecessary pointer for array
Tags *[]string `json:"tags,omitempty"`

// GOOD: Use value type, omitempty handles nil case
Tags []string `json:"tags,omitempty"`
```

---

## Validation Markers

### Common Validation Markers

```go
// String validation
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=255
// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$

// Numeric validation
// +kubebuilder:validation:Minimum=0
// +kubebuilder:validation:Maximum=100
// +kubebuilder:validation:ExclusiveMinimum=false
// +kubebuilder:validation:ExclusiveMaximum=true

// Array validation
// +kubebuilder:validation:MinItems=1
// +kubebuilder:validation:MaxItems=10
// +kubebuilder:validation:UniqueItems=true

// Enum validation
// +kubebuilder:validation:Enum=value1;value2;value3

// Format validation
// +kubebuilder:validation:Format=uri
// +kubebuilder:validation:Format=email
// +kubebuilder:validation:Format=date-time
```

### Advanced Patterns

**Conditional validation (OneOf):**
```go
// Either URL or ConfigMapRef must be set, not both
// +kubebuilder:validation:OneOf
type ToolSource struct {
    // +optional
    URL *string `json:"url,omitempty"`

    // +optional
    ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`
}
```

**Cross-field validation:**
```go
// Validate that startTime < endTime
// +kubebuilder:validation:XValidation:rule="self.startTime < self.endTime",message="startTime must be before endTime"
type TimeRange struct {
    StartTime metav1.Time `json:"startTime"`
    EndTime   metav1.Time `json:"endTime"`
}
```

**Default values:**
```go
// +optional
// +kubebuilder:default="info"
LogLevel *LogLevel `json:"logLevel,omitempty"`

// +optional
// +kubebuilder:default=1
Replicas *int32 `json:"replicas,omitempty"`
```

### Validation Best Practices

1. **Be specific:** Use pattern validation for structured strings (URLs, emails, etc.)
2. **Set reasonable limits:** MaxLength, MaxItems prevent resource exhaustion
3. **Use enums:** When values are from a fixed set, use enum validation
4. **Test validation:** E2E tests should verify both valid and invalid inputs

**Example test for validation:**
```go
func TestInvalidAgentSpec(t *testing.T) {
    invalidLevel := v1alpha2.LogLevel("invalid")

    agent := &v1alpha2.Agent{
        Spec: v1alpha2.AgentSpec{
            Declarative: &v1alpha2.DeclarativeAgentSpec{
                LogLevel: &invalidLevel, // Not in enum
            },
        },
    }

    err := client.Create(ctx, agent)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "Unsupported value: \"invalid\"")
}
```

---

## Complete Example: Adding a Complex Field

Here's a complete example of adding support for custom environment variables.

**1. Define types:**

```go
// go/api/v1alpha2/agent_types.go

// EnvVar represents an environment variable for the agent
type EnvVar struct {
    // Name of the environment variable
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Pattern=^[A-Z_][A-Z0-9_]*$
    Name string `json:"name"`

    // Value of the environment variable
    // +optional
    Value *string `json:"value,omitempty"`

    // ValueFrom allows setting value from a Secret or ConfigMap
    // +optional
    ValueFrom *corev1.EnvVarSource `json:"valueFrom,omitempty"`
}

type DeclarativeAgentSpec struct {
    // Existing fields...

    // Env allows setting custom environment variables for the agent
    // +optional
    // +kubebuilder:validation:MaxItems=50
    Env []EnvVar `json:"env,omitempty"`
}
```

**2. Run codegen:**

```bash
make -C go generate
cp go/api/config/crd/bases/kagent.dev_agents.yaml helm/kagent-crds/templates/
```

**3. Update translator:**

```go
// go/core/internal/controller/translator/agent/adk_api_translator.go

func (a *adkApiTranslator) translateInlineAgent(...) {
    // Build environment variables
    env := []corev1.EnvVar{
        // ... default env vars
    }

    // Add custom env vars from spec
    for _, e := range agent.Spec.Declarative.Env {
        envVar := corev1.EnvVar{
            Name: e.Name,
        }

        if e.Value != nil {
            envVar.Value = *e.Value
        }
        if e.ValueFrom != nil {
            envVar.ValueFrom = e.ValueFrom
        }

        env = append(env, envVar)
    }

    // Use in container spec
    container.Env = env
}
```

**4. Add E2E test:**

```go
func TestE2EAgentWithCustomEnv(t *testing.T) {
    agent := &v1alpha2.Agent{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-env-agent-" + randString(),
            Namespace: "kagent",
        },
        Spec: v1alpha2.AgentSpec{
            Declarative: &v1alpha2.DeclarativeAgentSpec{
                Env: []v1alpha2.EnvVar{
                    {
                        Name:  "CUSTOM_VAR",
                        Value: pointer.String("custom-value"),
                    },
                    {
                        Name: "SECRET_VAR",
                        ValueFrom: &corev1.EnvVarSource{
                            SecretKeyRef: &corev1.SecretKeySelector{
                                LocalObjectReference: corev1.LocalObjectReference{
                                    Name: "my-secret",
                                },
                                Key: "password",
                            },
                        },
                    },
                },
                // ... other required fields
            },
        },
    }

    // Create and verify
    require.NoError(t, client.Create(ctx, agent))
    defer client.Delete(ctx, agent)

    // Wait for deployment
    require.Eventually(t, func() bool {
        deployment := &appsv1.Deployment{}
        err := client.Get(ctx, types.NamespacedName{
            Name:      agent.Name,
            Namespace: agent.Namespace,
        }, deployment)
        if err != nil {
            return false
        }

        // Verify env vars are in container
        container := deployment.Spec.Template.Spec.Containers[0]
        hasCustomVar := false
        hasSecretVar := false

        for _, env := range container.Env {
            if env.Name == "CUSTOM_VAR" && env.Value == "custom-value" {
                hasCustomVar = true
            }
            if env.Name == "SECRET_VAR" && env.ValueFrom != nil {
                hasSecretVar = true
            }
        }

        return hasCustomVar && hasSecretVar
    }, 30*time.Second, 1*time.Second)
}
```

**5. Update documentation:**

```bash
# Create example
cat > examples/agent-with-env.yaml <<EOF
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: agent-with-custom-env
  namespace: kagent
spec:
  declarative:
    env:
      - name: LOG_FORMAT
        value: json
      - name: API_KEY
        valueFrom:
          secretKeyRef:
            name: api-credentials
            key: key
    # ... other fields
EOF
```

**6. Run tests:**

```bash
# Unit tests
make -C go test

# E2E tests
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
make -C go e2e
```

**7. Commit:**

```bash
git add go/api/ go/core/ helm/ examples/
git commit -s -m "feat: add support for custom environment variables in agent spec"
```
