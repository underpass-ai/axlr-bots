{{- define "axlr-bots-control-plane.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 52 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 52 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "axlr-bots-control-plane.labels" -}}
app.kubernetes.io/name: axlr-bots-control-plane
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "axlr-bots-control-plane.tag" . | quote }}
app.kubernetes.io/part-of: axlr-bots
app.kubernetes.io/component: control-plane
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{- define "axlr-bots-control-plane.selectorLabels" -}}
app.kubernetes.io/name: axlr-bots-control-plane
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "axlr-bots-control-plane.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "axlr-bots-control-plane.image" -}}
{{- $ref := printf "%s:%s" .Values.image.repository (include "axlr-bots-control-plane.tag" .) -}}
{{- if .Values.image.digest -}}
{{- $ref = printf "%s@%s" $ref .Values.image.digest -}}
{{- end -}}
{{- $ref -}}
{{- end -}}

{{- define "axlr-bots-control-plane.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "axlr-bots-control-plane.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "axlr-bots-control-plane.claimName" -}}
{{- default (printf "%s-state" (include "axlr-bots-control-plane.fullname" .)) .Values.persistence.existingClaim -}}
{{- end -}}

{{/* The certificate's extra names: the operator's, then the Service's cluster names. */}}
{{- define "axlr-bots-control-plane.serverNames" -}}
{{- $svc := include "axlr-bots-control-plane.fullname" . -}}
{{- $names := concat (default (list) .Values.serverNames) (list $svc (printf "%s.%s" $svc .Release.Namespace) (printf "%s.%s.svc" $svc .Release.Namespace) (printf "%s.%s.svc.cluster.local" $svc .Release.Namespace)) -}}
{{- join "," (uniq $names) -}}
{{- end -}}

{{/* The Tempo of this chart. */}}
{{- define "axlr-bots-control-plane.tempoName" -}}
{{- printf "%s-tempo" (include "axlr-bots-control-plane.fullname" .) -}}
{{- end -}}

{{/* Its own labels: the control plane's Service selects name and instance, which a Tempo pod must not carry. */}}
{{- define "axlr-bots-control-plane.tempoSelectorLabels" -}}
app.kubernetes.io/name: axlr-bots-tempo
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "axlr-bots-control-plane.tempoLabels" -}}
{{ include "axlr-bots-control-plane.tempoSelectorLabels" . }}
app.kubernetes.io/version: {{ .Values.tempo.image.tag | quote }}
app.kubernetes.io/part-of: axlr-bots
app.kubernetes.io/component: tracing
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{/* Where the control plane sends traces: the endpoint given, else the Tempo of this chart, else nowhere. */}}
{{- define "axlr-bots-control-plane.otlpEndpoint" -}}
{{- if .Values.tracing.otlpEndpoint -}}
{{- .Values.tracing.otlpEndpoint -}}
{{- else if .Values.tempo.enabled -}}
{{- printf "http://%s:4318" (include "axlr-bots-control-plane.tempoName" .) -}}
{{- end -}}
{{- end -}}
