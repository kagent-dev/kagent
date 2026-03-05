# Fix for kagent-tools Helm Chart Helper Name Collision

## Problem Summary

The kagent-tools Helm chart has a **helper template naming collision** with its parent kagent chart. Both charts define helpers with the same names (`kagent.name`, `kagent.fullname`, `kagent.selectorLabels`, etc.). Since Helm's template system is global, the parent chart's definitions override the subchart's, causing incorrect label generation.

### Current Impact

With the current published `kagent-tools:0.1.0` chart:

- Using `nameOverride: tools` results in:
  - Service name: `kagent-tools` (correct, uses fullname logic)
  - Selector label: `app.kubernetes.io/name: tools` (incorrect, uses parent's helper)
  - Expected selector: `app.kubernetes.io/name: kagent-tools`

This mismatch causes:
1. **Helm upgrade failures** due to immutable selector fields
2. **Inability to use `nameOverride`** - forces using `fullnameOverride` as a workaround

### Root Cause

**Parent chart** (`kagent/templates/_helpers.tpl`):
```yaml
{{- define "kagent.selectorLabels" -}}
app.kubernetes.io/name: {{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

**Subchart** (`kagent-tools/templates/_helpers.tpl`):
```yaml
{{- define "kagent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kagent.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
```

When the subchart tries to use `{{ include "kagent.selectorLabels" . }}`, it gets the **parent's definition** instead of its own.

---

## Solution: Rename Subchart Helpers

Rename all helper templates in the kagent-tools chart to use a unique prefix (`kagent-tools.*` instead of `kagent.*`).

---

## Implementation Steps

### 1. Locate the kagent-tools Chart Source

The kagent-tools chart is published to `oci://ghcr.io/kagent-dev/tools/helm/kagent-tools`. Find the source repository (likely `kagent-dev/tools` or similar).

### 2. Update `templates/_helpers.tpl`

**Original helpers to rename:**
- `kagent.name` → `kagent-tools.name`
- `kagent.fullname` → `kagent-tools.fullname`
- `kagent.chart` → `kagent-tools.chart`
- `kagent.labels` → `kagent-tools.labels`
- `kagent.selectorLabels` → `kagent-tools.selectorLabels`
- `kagent.namespace` → `kagent-tools.namespace`
- `kagent.serviceAccountName` → `kagent-tools.serviceAccountName`
- Any other `kagent.*` helpers

**Example before:**
```yaml
{{- define "kagent.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- if not .Values.nameOverride }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}
```

**Example after:**
```yaml
{{- define "kagent-tools.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- if not .Values.nameOverride }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}
```

### 3. Update All Template References

Search and replace all `{{ include "kagent.*" }}` references in template files to use the new names.

**Files to update (typical structure):**
- `templates/deployment.yaml`
- `templates/service.yaml`
- `templates/serviceaccount.yaml`
- `templates/rbac.yaml`
- `templates/clusterrole.yaml`
- `templates/clusterrolebinding.yaml`
- `templates/secret.yaml`
- Any other template files

**Example changes:**

**Before:**
```yaml
metadata:
  name: {{ include "kagent.fullname" . }}
  namespace: {{ include "kagent.namespace" . }}
  labels:
    {{- include "kagent.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "kagent.selectorLabels" . | nindent 6 }}
```

**After:**
```yaml
metadata:
  name: {{ include "kagent-tools.fullname" . }}
  namespace: {{ include "kagent-tools.namespace" . }}
  labels:
    {{- include "kagent-tools.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "kagent-tools.selectorLabels" . | nindent 6 }}
```

### 4. Search for All References

Use this command to find all references:

```bash
cd <kagent-tools-chart-directory>
grep -r "include \"kagent\." templates/
grep -r "define \"kagent\." templates/
```

Ensure ALL references are updated to the new `kagent-tools.*` prefix.

### 5. Update Chart Version

In `Chart.yaml`, bump the version:
- If considered a bug fix: `0.1.0` → `0.1.1`
- If considered breaking change: `0.1.0` → `0.2.0`

**Recommendation:** Use `0.1.1` since the behavior should be the same for users already using `fullnameOverride`.

---

## Testing

### 1. Lint the Chart

```bash
helm lint .
```

### 2. Test Template Rendering

```bash
# Test with nameOverride
helm template test-release . \
  --set nameOverride=tools

# Verify selector labels:
helm template test-release . \
  --set nameOverride=tools \
  --show-only templates/deployment.yaml | grep -A 5 "selector:"

# Expected output:
# selector:
#   matchLabels:
#     app.kubernetes.io/name: test-release-tools
#     app.kubernetes.io/instance: test-release
```

### 3. Test Service Name

```bash
helm template test-release . \
  --set nameOverride=tools \
  --show-only templates/service.yaml | grep "name:"

# Expected: name: test-release-tools
```

### 4. Test with fullnameOverride

```bash
helm template test-release . \
  --set fullnameOverride=custom-tools \
  --show-only templates/deployment.yaml | grep -A 5 "selector:"

# Expected:
# selector:
#   matchLabels:
#     app.kubernetes.io/name: custom-tools
#     app.kubernetes.io/instance: test-release
```

### 5. Test as Subchart

Create a test parent chart:

```yaml
# test-parent/Chart.yaml
apiVersion: v2
name: test-parent
version: 1.0.0
dependencies:
  - name: kagent-tools
    version: <new-version>
    repository: file://../kagent-tools
```

```bash
cd test-parent
helm dependency update
helm template test . --set kagent-tools.nameOverride=tools
```

Verify the selector uses `test-tools` (not just `tools`).

---

## Publishing

### 1. Package the Chart

```bash
helm package .
```

### 2. Push to OCI Registry

```bash
helm push kagent-tools-<version>.tgz oci://ghcr.io/kagent-dev/tools/helm
```

### 3. Update kagent Chart

In the main `kagent` repository, update `helm/kagent/Chart-template.yaml`:

```yaml
  - name: kagent-tools
    version: <new-version>  # e.g., 0.1.1
    repository: oci://ghcr.io/kagent-dev/tools/helm
    condition: kagent-tools.enabled
```

### 4. Update kagent values.yaml

In `helm/kagent/values.yaml`, change from:

```yaml
kagent-tools:
  enabled: true
  fullnameOverride: kagent-tools  # Workaround for helper collision
```

To:

```yaml
kagent-tools:
  enabled: true
  nameOverride: tools  # Now works correctly with renamed helpers
```

### 5. Regenerate kagent Chart.yaml

```bash
cd kagent
make helm-version
```

### 6. Update Helm Dependencies

```bash
cd helm/kagent
helm dependency update
```

---

## Verification Checklist

- [ ] All `kagent.*` helpers renamed to `kagent-tools.*` in `_helpers.tpl`
- [ ] All template references updated to use new helper names
- [ ] Chart version bumped in `Chart.yaml`
- [ ] `helm lint` passes
- [ ] Template rendering produces correct selector labels with `nameOverride`
- [ ] Template rendering produces correct service names
- [ ] Chart packaged and pushed to OCI registry
- [ ] kagent Chart.yaml updated to use new version
- [ ] kagent values.yaml updated to use `nameOverride: tools`
- [ ] Helm unit tests pass: `cd helm/kagent && helm unittest .`
- [ ] E2E tests pass with the new chart version

---

## Backward Compatibility Note

Users currently using `fullnameOverride: kagent-tools` will continue to work without changes. Users can optionally migrate to `nameOverride: tools` for better flexibility with custom release names.

---

## Additional Context

### Why This Fix Matters

1. **Enables proper use of nameOverride** - allows release-name-based service names (`my-release-tools` instead of hardcoded `kagent-tools`)
2. **Fixes helm upgrade issues** - removes selector mismatch that causes immutable field errors
3. **Follows Helm best practices** - subcharts should not have naming conflicts with parent charts
4. **Improves maintainability** - clear separation between parent and subchart helpers

### Related Files in kagent Repo

- `helm/kagent/values.yaml` - Currently uses `fullnameOverride` workaround
- `helm/kagent/templates/toolserver-kagent.yaml` - Dynamically computes tools service name
- `helm/kagent/tests/toolserver_test.yaml` - Tests for RemoteMCPServer URL generation

### Original Issue

See: https://github.com/kagent-dev/kagent/issues/1190

The issue reported that `fullnameOverride` prevented using custom release names. The proper fix requires renaming helpers in the kagent-tools chart to avoid template collisions.
