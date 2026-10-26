{{- define "fleet.labels" -}}
app.kubernetes.io/part-of: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "fleet.aisLabels" -}}
app.kubernetes.io/name: ais
app.kubernetes.io/component: ais-receiver
fleet.underpass.ai/source: aisstream.io
{{- end -}}

{{- define "fleet.cp" -}}{{ .Release.Name }}-cp{{- end -}}
{{- define "fleet.base" -}}{{ .Release.Name }}-base{{- end -}}

{{- define "fleet.podSpecCommon" -}}
automountServiceAccountToken: false
enableServiceLinks: false
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile: {type: RuntimeDefault}
{{- with .Values.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{- define "fleet.restricted" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities: {drop: ["ALL"]}
{{- end -}}
