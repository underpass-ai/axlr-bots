{{- define "orchestra.labels" -}}
app.kubernetes.io/part-of: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "orchestra.cp" -}}{{ .Release.Name }}-cp{{- end -}}
{{- define "orchestra.nats" -}}{{ .Release.Name }}-nats{{- end -}}
{{- define "orchestra.score" -}}{{ .Release.Name }}-score{{- end -}}
{{- define "orchestra.rave" -}}{{ .Release.Name }}-rave{{- end -}}

{{/* A service's names inside the cluster: name, name.ns.svc, name.ns.svc.cluster.local. */}}
{{- define "orchestra.svcNames" -}}
{{- $name := index . 0 }}{{- $ns := index . 1 -}}
{{ $name }},{{ $name }}.{{ $ns }}.svc,{{ $name }}.{{ $ns }}.svc.cluster.local
{{- end -}}

{{- define "orchestra.podSpecCommon" -}}
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

{{/* An image of images.<name>, by digest: (list $ "<name>"). */}}
{{- define "orchestra.image" -}}
{{- $root := index . 0 }}{{- $name := index . 1 -}}
{{- $ref := index $root.Values.images $name | toString -}}
{{- if not (regexMatch "^[^@ ]+@sha256:[0-9a-f]{64}$" $ref) }}{{ fail (printf "images.%s is required: <repository>@sha256:<64 hex>" $name) }}{{ end -}}
{{ $ref }}
{{- end -}}

{{- define "orchestra.restricted" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities: {drop: ["ALL"]}
{{- end -}}

{{/* Every chair's node id, in the score's order. */}}
{{- define "orchestra.nodeIDs" -}}
{{- $out := list }}
{{- range $s := .Values.chairs.sections }}
{{- if not (regexMatch "^[a-z0-9]{1,8}$" (toString $s.id)) }}{{ fail (printf "chairs.sections: %q is not a section id" $s.id) }}{{ end }}
{{- range $n := until (int $s.chairs) }}
{{- $out = append $out (printf "%s%s-%d" $.Values.chairs.nodePrefix $s.id (add1 $n)) }}
{{- end }}
{{- end }}
{{- toJson $out }}
{{- end -}}

{{/* The ingress for one public service: (dict "root" $ "name" <service> "host" <host> "port" <port>). */}}
{{- define "orchestra.ingress" -}}
{{- $root := .root }}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$" (toString .host)) }}{{ fail (printf "%s: the public host is required (a DNS name)" .name) }}{{ end }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .name }}
  labels:
    {{- include "orchestra.labels" $root | nindent 4 }}
  annotations:
    {{- with $root.Values.ingress.clusterIssuer }}
    cert-manager.io/cluster-issuer: {{ . }}
    {{- end }}
    nginx.ingress.kubernetes.io/force-ssl-redirect: "true"
    # /audio.wav is an endless stream and /ws/hands a WebSocket held for
    # the whole concert: no buffering, long reads and writes.
    nginx.ingress.kubernetes.io/proxy-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-request-buffering: "off"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: {{ $root.Values.ingress.className }}
  rules:
    - host: {{ .host }}
      http:
        paths:
          - {path: /, pathType: Prefix, backend: {service: {name: {{ .name }}, port: {name: http}}}}
  tls:
    - hosts: [{{ .host }}]
      secretName: {{ .name }}-tls
{{- end -}}
