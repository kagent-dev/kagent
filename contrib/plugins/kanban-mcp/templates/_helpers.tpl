{{/*
Expand the name of the chart.
*/}}
{{- define "kanban-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "kanban-mcp.fullname" -}}
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
{{- define "kanban-mcp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kanban-mcp.labels" -}}
helm.sh/chart: {{ include "kanban-mcp.chart" . }}
{{ include "kanban-mcp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kanban-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kanban-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Name of the Secret that holds the database URL.
Uses database.existingSecret when set, otherwise a generated "<fullname>-db" Secret.
*/}}
{{- define "kanban-mcp.dbSecretName" -}}
{{- if .Values.database.existingSecret -}}
{{- .Values.database.existingSecret -}}
{{- else -}}
{{- printf "%s-db" (include "kanban-mcp.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
In-cluster URL of the kanban MCP endpoint, used as RemoteMCPServer spec.url.
The kanban server serves MCP over Streamable HTTP at /mcp, with the web UI at /.
*/}}
{{- define "kanban-mcp.serverUrl" -}}
{{- printf "http://%s.%s:%d/mcp" (include "kanban-mcp.fullname" .) .Release.Namespace (.Values.service.port | int) }}
{{- end }}

{{/*
Key within the database Secret that holds the URL.
*/}}
{{- define "kanban-mcp.dbSecretKey" -}}
{{- if .Values.database.existingSecret -}}
{{- default "url" .Values.database.existingSecretKey -}}
{{- else -}}
url
{{- end -}}
{{- end }}
