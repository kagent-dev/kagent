{{/*
Expand the name of the chart.
*/}}
{{- define "grafana-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "grafana-mcp.fullname" -}}
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
{{- define "grafana-mcp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "grafana-mcp.labels" -}}
helm.sh/chart: {{ include "grafana-mcp.chart" . }}
{{ include "grafana-mcp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "grafana-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "grafana-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "grafana-mcp.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "grafana-mcp.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the grafana server URL
*/}}
{{- define "grafana-mcp.serverUrl" -}}
{{- printf "http://%s.%s:%d/mcp" (include "grafana-mcp.fullname" .) .Release.Namespace (.Values.service.port | int) }}
{{- end }}

{{/*
Host header values the MCP server will answer to.

The server rejects any Host it was not told about — protection against DNS rebinding,
which is aimed at browsers and applies to every client. Its default allow-list is the
loopback forms of --address, so a server reached over the cluster network answers
"forbidden: host not allowed" to the handshake and reports no tools at all.

Derived from the same fullname, namespace and port as grafana-mcp.serverUrl, because that
URL is precisely what the controller dials: the two cannot be allowed to disagree. The
shorter in-cluster forms are included for anything addressing the service directly, and
loopback for a port-forward. Set allowedHosts to override, including "*" to switch the
check off behind a proxy that rewrites Host.
*/}}
{{- define "grafana-mcp.allowedHosts" -}}
{{- if .Values.allowedHosts -}}
{{- .Values.allowedHosts -}}
{{- else -}}
{{- $name := include "grafana-mcp.fullname" . -}}
{{- $ns := .Release.Namespace -}}
{{- $port := .Values.service.port | int -}}
{{- $hosts := list
  (printf "%s:%d" $name $port)
  (printf "%s.%s:%d" $name $ns $port)
  (printf "%s.%s.svc:%d" $name $ns $port)
  (printf "%s.%s.svc.cluster.local:%d" $name $ns $port)
  (printf "localhost:%d" $port)
  (printf "127.0.0.1:%d" $port)
-}}
{{- join "," $hosts -}}
{{- end -}}
{{- end }}

{{/*
Join registry/repository/name/tag for grafana-mcp image, skipping empty segments, then append tag
*/}}
{{- define "grafana-mcp.image" -}}
{{- $img := .Values.image -}}
{{- $parts := compact (list $img.registry $img.repository $img.name) -}}
{{- printf "%s:%s" (join "/" $parts) $img.tag -}}
{{- end -}}