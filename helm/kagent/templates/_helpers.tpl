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
Watch namespaces - transforms list of namespaces cached by the controller into comma-separated string.
Precedence: controller.watchNamespaces (explicit override) > rbac.namespaces > empty (watch all).
*/}}
{{- define "kagent.watchNamespaces" -}}
{{- if .Values.controller.watchNamespaces -}}
  {{- .Values.controller.watchNamespaces | uniq | join "," -}}
{{- else if and .Values.rbac .Values.rbac.namespaces -}}
  {{- .Values.rbac.namespaces | uniq | join "," -}}
{{- end -}}
{{- end -}}

{{/*
Guards on the rbac block
*/}}
{{- define "kagent.rbac.validate" -}}
{{- if and .Values.rbac (hasKey .Values.rbac "clusterScoped") -}}
{{- fail "rbac.clusterScoped has been removed. Leave rbac.namespaces empty for cluster-scoped RBAC, or set rbac.namespaces=[<ns>, ...] for namespaced RBAC." -}}
{{- end -}}
{{- if and .Values.rbac .Values.rbac.namespaces -}}
{{- $installNs := include "kagent.namespace" . -}}
{{- if not (has $installNs .Values.rbac.namespaces) -}}
{{- fail (printf "rbac.namespaces is set but does not include the install namespace %q" $installNs) -}}
{{- end -}}
{{- end -}}
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

{{/*
Check if leader election should be enabled (more than 1 replica)
*/}}
{{- define "kagent.leaderElectionEnabled" -}}
{{- gt (.Values.controller.replicas | int) 1 -}}
{{- end -}}

{{/*
PostgreSQL service name for the bundled postgres instance
*/}}
{{- define "kagent.postgresqlServiceName" -}}
{{- printf "%s-postgresql" (include "kagent.fullname" .) -}}
{{- end -}}

{{/*
Bundled PostgreSQL image - constructs the full image reference from registry/repository/name/tag
*/}}
{{- define "kagent.postgresql.image" -}}
{{- $pg := .Values.database.postgres.bundled -}}
{{- printf "%s/%s/%s:%s" $pg.image.registry $pg.image.repository $pg.image.name $pg.image.tag -}}
{{- end -}}

{{/*
Password secret name - returns the chart-managed Secret name for POSTGRES_PASSWORD.
*/}}
{{- define "kagent.passwordSecretName" -}}
{{- printf "%s-postgresql" (include "kagent.fullname" .) -}}
{{- end -}}

{{/*
A2A Base URL - computes the default URL based on the controller service name if not explicitly set
*/}}
{{- define "kagent.a2aBaseUrl" -}}
{{- if .Values.controller.a2aBaseUrl -}}
{{- .Values.controller.a2aBaseUrl -}}
{{- else -}}
{{- printf "http://%s-controller.%s.svc.cluster.local:%d" (include "kagent.fullname" .) (include "kagent.namespace" .) (.Values.controller.service.ports.port | int) -}}
{{- end -}}
{{- end -}}

{{/*
imagePullSecrets from global values (for subchart usage).
Reads .Values.global.imagePullSecrets set by the parent chart.
*/}}
{{- define "kagent.imagePullSecrets" -}}
{{- $global := ((.Values.global).imagePullSecrets) | default list -}}
{{- if $global -}}
imagePullSecrets:
{{- toYaml $global | nindent 2 }}
{{- end -}}
{{- end -}}

