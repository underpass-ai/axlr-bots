{{- define "axlr-bots-node.demoName" -}}
{{- printf "%s-%s" .Release.Name .Values.demo.name | trunc 52 | trimSuffix "-" -}}
{{- end -}}

{{- define "axlr-bots-node.labels" -}}
app.kubernetes.io/name: axlr-bots-node
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/part-of: axlr-bots
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{- define "axlr-bots-node.selectorLabels" -}}
app.kubernetes.io/name: axlr-bots-node
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* The demo's values with the service's log volume as the bot's workspace. */}}
{{- define "axlr-bots-node.demoValues" -}}
{{- $values := deepCopy .Values -}}
{{- $workspaces := append (default (list) $values.sidecar.workspaces) (dict "bot" .Values.demo.bot "volume" "service-logs") -}}
{{- $_ := set $values.sidecar "workspaces" $workspaces -}}
{{- toYaml $values -}}
{{- end -}}

{{/* The pod security context the demo and the test use. */}}
{{- define "axlr-bots-node.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "axlr-bots-node.scheduling" -}}
{{- with .Values.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/* The Secrets the HTTPS demo reads: given, or the ones cert-manager makes. */}}
{{- define "axlr-bots-node.https.serverSecret" -}}
{{- if .Values.demo.https.certManager.enabled -}}
{{- printf "%s-tls" (include "axlr-bots-node.demoName" .) -}}
{{- else -}}
{{- required "demo.https.serverTLS.existingSecret is required (or demo.https.certManager.enabled)" .Values.demo.https.serverTLS.existingSecret -}}
{{- end -}}
{{- end -}}

{{- define "axlr-bots-node.https.clientCASecret" -}}
{{- if .Values.demo.https.certManager.enabled -}}
{{- printf "%s-client-ca" (include "axlr-bots-node.demoName" .) -}}
{{- else -}}
{{- required "demo.https.clientCA.existingSecret is required (or demo.https.certManager.enabled)" .Values.demo.https.clientCA.existingSecret -}}
{{- end -}}
{{- end -}}
