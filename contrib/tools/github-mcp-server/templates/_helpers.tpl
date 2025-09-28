{{/*
Expand the name of the chart.
*/}}
{{- define "github-mcp-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "github-mcp-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "github-mcp-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "github-mcp-server.labels" -}}
helm.sh/chart: {{ include "github-mcp-server.chart" . }}
{{ include "github-mcp-server.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "github-mcp-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "github-mcp-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Generate the URL for a given toolset
*/}}
{{- define "github-mcp-server.toolUrl" -}}
{{- $toolset := .toolset -}}
{{- $readonly := .readonly -}}
{{- $baseUrl := .context.Values.baseUrl -}}
{{- if eq $toolset "all" -}}
{{- $baseUrl }}/
{{- else -}}
{{- $baseUrl }}/x/{{ include "github-mcp-server.toolsetPath" $toolset }}
{{- end -}}
{{- if $readonly }}/readonly{{- end -}}
{{- end }}

{{/*
Generate the resource name for a given toolset
*/}}
{{- define "github-mcp-server.resourceName" -}}
{{- $baseName := include "github-mcp-server.fullname" .context -}}
{{- $suffix := include "github-mcp-server.toolsetKebab" .toolset -}}
{{- if .readonly -}}
{{- $suffix = printf "%s-readonly" $suffix -}}
{{- end -}}
{{- printf "%s-%s" $baseName $suffix | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Generate the description for a given toolset
*/}}
{{- define "github-mcp-server.description" -}}
{{- $config := .config -}}
{{- $mode := ternary "read-only" "read-write" .readonly -}}
{{- printf "%s - %s (%s)" .context.Values.descriptionPrefix $config.description $mode -}}
{{- end }}

{{/*
Get the token secret name for a toolset

Algorithm (precedence order):
1. If toolset has tokenSecretRef:
   - Use toolset.tokenSecretRef.name
   - If not provided - throw an error
2. Else if toolset has tokenSecret:
   - Use toolset.tokenSecret.name if provided
   - Else if toolset has tokenSecret.value: auto-generate "{resourceName}-token"
   - Else throw an error (empty tokenSecret block)
3. Else if global tokenSecretRef.name exists:
   - Use global.tokenSecretRef.name
4. Else if global.tokenSecret.name exists:
   - Use global.tokenSecret.name
5. Else (final):
   - There is no name specified for the secret to be used - throw an error!
*/}}
{{- define "github-mcp-server.tokenSecretName" -}}
{{- $config := .config -}}
{{- $global := .context.Values.tokenSecret -}}
{{- $globalRef := .context.Values.tokenSecretRef -}}
{{- $toolset := .toolset -}}
{{- if $config.tokenSecretRef -}}
{{- if $config.tokenSecretRef.name -}}
{{- $config.tokenSecretRef.name -}}
{{- else -}}
{{- fail (printf "toolset '%s' has tokenSecretRef but no name specified. Please provide tokenSecretRef.name" $toolset) -}}
{{- end -}}
{{- else if $config.tokenSecret -}}
{{- if $config.tokenSecret.name -}}
{{- $config.tokenSecret.name -}}
{{- else if $config.tokenSecret.value -}}
{{- printf "%s-token" (include "github-mcp-server.resourceName" (dict "toolset" $toolset "readonly" false "context" .context)) -}}
{{- else -}}
{{- fail (printf "toolset '%s' has empty tokenSecret block. Please provide tokenSecret.name or tokenSecret.value" $toolset) -}}
{{- end -}}
{{- else if $globalRef.name -}}
{{- $globalRef.name -}}
{{- else if $global.name -}}
{{- $global.name -}}
{{- else -}}
{{- fail "No secret name specified. Please provide either global tokenSecret.name or tokenSecretRef.name" -}}
{{- end -}}
{{- end }}

{{/*
Validate that a token is available for a toolset
*/}}
{{- define "github-mcp-server.validateToken" -}}
{{- $config := .config -}}
{{- $global := .context.Values.tokenSecret -}}
{{- $globalRef := .context.Values.tokenSecretRef -}}
{{- $hasToken := or (and $config.tokenSecretRef.name $config.tokenSecretRef.key) $config.tokenSecret.value (and $globalRef.name $globalRef.key) $global.value -}}
{{- if not $hasToken -}}
{{- fail (printf "No token configured for toolset '%s'. Please provide tokenSecret.value or tokenSecretRef.name+key either globally or per toolset." .toolset) -}}
{{- end -}}
{{- end }}

{{/*
Get the token secret key for a toolset
*/}}
{{- define "github-mcp-server.tokenSecretKey" -}}
{{- $config := .config -}}
{{- $global := .context.Values.tokenSecret -}}
{{- $globalRef := .context.Values.tokenSecretRef -}}
{{- coalesce $config.tokenSecretRef.key $config.tokenSecret.key $globalRef.key $global.key "token" -}}
{{- end }}

{{/*
Get the timeout for a toolset
*/}}
{{- define "github-mcp-server.timeout" -}}
{{- $config := .config -}}
{{- $global := .context.Values.timeout -}}
{{- if $config.timeout -}}
{{- $config.timeout -}}
{{- else -}}
{{- $global -}}
{{- end -}}
{{- end }}

{{/*
Convert camelCase toolset names to URL path format
*/}}
{{- define "github-mcp-server.toolsetPath" -}}
{{- $toolset := . -}}
{{- if eq $toolset "codeSecurity" -}}
code_security
{{- else if eq $toolset "organizations" -}}
orgs
{{- else if eq $toolset "pullRequests" -}}
pull_requests
{{- else if eq $toolset "repositories" -}}
repos
{{- else if eq $toolset "secretProtection" -}}
secret_protection
{{- else -}}
{{ $toolset }}
{{- end -}}
{{- end }}

{{/*
Convert camelCase toolset names to kebab-case for resource names
*/}}
{{- define "github-mcp-server.toolsetKebab" -}}
{{- $toolset := . -}}
{{- if eq $toolset "codeSecurity" -}}
code-security
{{- else if eq $toolset "pullRequests" -}}
pull-requests
{{- else if eq $toolset "secretProtection" -}}
secret-protection
{{- else -}}
{{ $toolset | lower }}
{{- end -}}
{{- end }}
