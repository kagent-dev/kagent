{{- define "agent.deploymentSpec" -}}
{{- with coalesce (empty .Values.imagePullSecrets | ternary nil .Values.imagePullSecrets) .Values.global.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.podSecurityContext }}
podSecurityContext:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.securityContext }}
securityContext:
  {{- toYaml . | nindent 2 }}
{{- end }}
resources:
  {{- toYaml .Values.resources | nindent 2 }}
{{- end }}
