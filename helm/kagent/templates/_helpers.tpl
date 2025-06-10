{{/*
Expand the name of the chart.
*/}}
{{- define "kagent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kagent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kagent.labels" -}}
helm.sh/chart: {{ include "kagent.chart" . }}
{{ include "kagent.selectorLabels" . }}
{{- if .Chart.Version }}
app.kubernetes.io/version: {{ .Chart.Version | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kagent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kagent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*Default provider name*/}}
{{- define "kagent.defaultProviderName" -}}
{{ .Values.providers.default | default "openAI" | lower}}
{{- end }}

{{/*Default model name*/}}
{{- define "kagent.defaultModelConfigName" -}}
default-model-config
{{- end }}

{{/*
Expand the namespace of the release.
Allows overriding it for multi-namespace deployments in combined charts.
*/}}
{{- define "kagent.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Watch namespaces - transforms list of namespaces cached by the controller into comma-separated string
Removes duplicates
*/}}
{{- define "kagent.watchNamespaces" -}}
{{- $nsSet := dict }}
{{- .Values.controller.watchNamespaces | default list | uniq | join "," }}
{{- end -}}
