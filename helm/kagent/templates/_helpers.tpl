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
Common labels
*/}}
{{- define "kagent.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "kagent.selectorLabels" . }}
{{- if .Chart.Version }}
app.kubernetes.io/version: {{ .Chart.Version | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kagent
{{- with .Values.labels }}
{{ toYaml . | nindent 0 }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kagent.selectorLabels" -}}
app.kubernetes.io/name: {{ default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
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

{{/*
UI selector labels
*/}}
{{- define "kagent.ui.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: ui
{{- end }}

{{/*
Controller selector labels
*/}}
{{- define "kagent.controller.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Engine selector labels
*/}}
{{- define "kagent.engine.selectorLabels" -}}
{{ include "kagent.selectorLabels" . }}
app.kubernetes.io/component: engine
{{- end }}

{{/*
Controller labels
*/}}
{{- define "kagent.controller.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
UI labels
*/}}
{{- define "kagent.ui.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: ui
{{- end }}

{{/*
Engine labels
*/}}
{{- define "kagent.engine.labels" -}}
{{ include "kagent.labels" . }}
app.kubernetes.io/component: engine
{{- end }}
