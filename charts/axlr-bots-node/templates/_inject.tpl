{{/*
Templates another chart includes to put the node agent in its own pod.
Each takes (dict "values" <this chart's values> "root" $); from a chart
that depends on this one, the values are (index .Values "axlr-bots-node"):

  {{- $bots := dict "values" (index .Values "axlr-bots-node") "root" $ }}
  spec:
    containers:
      - name: my-service
        …
      {{- include "axlr-bots-node.inject.container" $bots | nindent 6 }}
    volumes:
      {{- include "axlr-bots-node.inject.volumes" $bots | nindent 6 }}
  volumeClaimTemplates:
    {{- include "axlr-bots-node.inject.volumeClaimTemplate" $bots | nindent 4 }}

The workload should be a StatefulSet: the node id is <idPrefix><pod name>,
and the pod's name and its state volume are stable only there. The pod
needs the label node.appLabelKey (app). sidecar.workspaces names the pod's
volumes the bots read, mounted read-only. A NetworkPolicy is per pod, so
the sidecar's egress (axlr-bots-node.inject.egress) goes into the
including chart's own policy, beside its service's rules.
*/}}

{{- define "axlr-bots-node.image" -}}
{{- $digest := required "image.digest is required (sha256:<64 hex>, from the image push)" .image.digest -}}
{{- if not (regexMatch "^sha256:[0-9a-f]{64}$" $digest) }}{{ fail "image.digest must be sha256:<64 hex>" }}{{ end -}}
{{- required "image.repository is required" .image.repository }}@{{ $digest }}
{{- end -}}

{{- define "axlr-bots-node.controlURL" -}}
{{- $url := required "controlPlane.url is required (https://host:port, the control plane's node address)" .controlPlane.url -}}
{{- if not (regexMatch "^https://[^/ ]+$" $url) }}{{ fail "controlPlane.url must be https://host:port" }}{{ end -}}
{{- $url -}}
{{- end -}}

{{- define "axlr-bots-node.inject.container" -}}
{{- $v := .values -}}
{{- if not (regexMatch "^[a-z0-9._-]{0,40}$" (toString $v.node.idPrefix)) }}{{ fail "node.idPrefix may only use lowercase letters, digits, '.', '_' and '-'" }}{{ end -}}
- name: {{ $v.sidecar.name }}
  image: {{ include "axlr-bots-node.image" $v }}
  imagePullPolicy: IfNotPresent
  {{- if $v.sidecar.native }}
  restartPolicy: Always
  {{- end }}
  env:
    - name: POD_NAME
      valueFrom: {fieldRef: {fieldPath: metadata.name}}
    - name: POD_NAMESPACE
      valueFrom: {fieldRef: {fieldPath: metadata.namespace}}
    - name: POD_APP
      valueFrom: {fieldRef: {fieldPath: "metadata.labels['{{ $v.node.appLabelKey }}']"}}
    - name: AXLR_NODE_ID
      value: "{{ $v.node.idPrefix }}$(POD_NAME)"
    - name: AXLR_NODE_LABELS
      value: "namespace=$(POD_NAMESPACE),app=$(POD_APP),pod=$(POD_NAME){{ range $key, $value := $v.node.labels }}{{ if not (and (regexMatch "^[a-z0-9._-]{1,64}$" $key) (regexMatch "^[a-z0-9._-]{1,64}$" (toString $value))) }}{{ fail "node.labels keys and values may only use lowercase letters, digits, '.', '_' and '-'" }}{{ end }},{{ $key }}={{ $value }}{{ end }}"
    - name: AXLR_NODE_DEDICATED
      value: {{ ternary "true" "false" (eq (toString $v.node.dedicated) "true") | quote }}
    - name: AXLR_NODE_CONTROL
      value: {{ include "axlr-bots-node.controlURL" $v | quote }}
    - name: AXLR_NODE_TOKEN_FILE
      value: /etc/axlr-node/enroll/$(AXLR_NODE_ID)
    {{- if $v.controlPlane.ca.existingConfigMap }}
    - name: AXLR_NODE_CA_FILE
      value: /etc/axlr-node/ca/ca.crt
    {{- end }}
    {{- if ne (toString $v.model.enabled) "false" }}
    - name: OPENROUTER_API_KEY
      valueFrom:
        secretKeyRef:
          name: {{ required "model.apiKey.existingSecret is required (or model.enabled false for a node of Jev bots only)" $v.model.apiKey.existingSecret }}
          key: {{ $v.model.apiKey.key }}
    {{- end }}
    {{- if eq (toString $v.bus.kind) "nats" }}
    # The pushes come from the broker; with a tls:// URL the node shows it
    # its own certificate from enrollment (mutual TLS).
    - name: AXLR_NODE_BUS
      value: nats
    - name: AXLR_NODE_NATS_URL
      value: {{ required "bus.nats.url is required with bus.kind nats (tls://host:4222)" $v.bus.nats.url | quote }}
    {{- if or $v.bus.nats.ca.existingConfigMap $v.bus.nats.ca.existingSecret }}
    - name: AXLR_NODE_NATS_CA
      value: /etc/axlr-node/nats-ca/ca.crt
    {{- end }}
    {{- else if ne (toString $v.bus.kind) "direct" }}{{ fail "bus.kind is direct or nats" }}
    {{- end }}
    {{- if $v.sidecar.mcp.existingConfigMap }}
    # The mcp.json a Jev bot's program finds its MCP server in.
    - name: AXLR_NODE_MCP_CONFIG
      value: /etc/axlr-node/mcp/mcp.json
    {{- end }}
    {{- with $v.sidecar.env }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: ["ALL"]
    seccompProfile:
      type: RuntimeDefault
  resources:
    {{- toYaml $v.sidecar.resources | nindent 4 }}
  volumeMounts:
    - {name: axlr-node-state, mountPath: /var/lib/axlr-node}
    # Its own volume, before the bots' read-only mounts inside it: the
    # runtime creates those mount points as root, which would leave
    # workspaces/ on the state volume unwritable for every other bot.
    - {name: axlr-node-workspaces, mountPath: /var/lib/axlr-node/workspaces}
    - {name: axlr-node-tmp, mountPath: /tmp}
    - {name: axlr-node-enroll, mountPath: /etc/axlr-node/enroll, readOnly: true}
    {{- if $v.controlPlane.ca.existingConfigMap }}
    - {name: axlr-node-ca, mountPath: /etc/axlr-node/ca, readOnly: true}
    {{- end }}
    {{- if and (eq (toString $v.bus.kind) "nats") (or $v.bus.nats.ca.existingConfigMap $v.bus.nats.ca.existingSecret) }}
    - {name: axlr-node-nats-ca, mountPath: /etc/axlr-node/nats-ca, readOnly: true}
    {{- end }}
    {{- if $v.sidecar.mcp.existingConfigMap }}
    - {name: axlr-node-mcp, mountPath: /etc/axlr-node/mcp, readOnly: true}
    {{- end }}
    {{- range $v.sidecar.workspaces }}
    {{- if not (regexMatch "^[a-z0-9._-]{1,64}$" (toString .bot)) }}{{ fail "sidecar.workspaces[].bot must be a bot id" }}{{ end }}
    - name: {{ required "sidecar.workspaces[].volume is required" .volume }}
      mountPath: /var/lib/axlr-node/workspaces/{{ .bot }}
      readOnly: true
      {{- with .subPath }}
      subPath: {{ . | quote }}
      {{- end }}
    {{- end }}
{{- end -}}

{{- define "axlr-bots-node.inject.volumes" -}}
{{- $v := .values -}}
{{- if not $v.sidecar.state.persistent }}
- name: axlr-node-state
  emptyDir:
    sizeLimit: {{ $v.sidecar.state.size }}
{{- end }}
- name: axlr-node-workspaces
  emptyDir:
    sizeLimit: {{ $v.sidecar.workspacesSize }}
- name: axlr-node-tmp
  emptyDir:
    medium: Memory
    sizeLimit: {{ $v.sidecar.tmpSize }}
- name: axlr-node-enroll
  secret:
    secretName: {{ required "enrollment.existingSecret is required (one key per node id, each a token)" $v.enrollment.existingSecret }}
    optional: true
    defaultMode: 292 # 0444: the sidecar runs as 65532 whatever the pod's fsGroup
{{- if $v.controlPlane.ca.existingConfigMap }}
- name: axlr-node-ca
  configMap:
    name: {{ $v.controlPlane.ca.existingConfigMap }}
    items: [{key: ca.crt, path: ca.crt}]
{{- end }}
{{- if eq (toString $v.bus.kind) "nats" }}
{{- if $v.bus.nats.ca.existingConfigMap }}
- name: axlr-node-nats-ca
  configMap:
    name: {{ $v.bus.nats.ca.existingConfigMap }}
    items: [{key: ca.crt, path: ca.crt}]
{{- else if $v.bus.nats.ca.existingSecret }}
# Only the CA's certificate of the Secret (cert-manager's, say): never its key.
- name: axlr-node-nats-ca
  secret:
    secretName: {{ $v.bus.nats.ca.existingSecret }}
    items: [{key: ca.crt, path: ca.crt}]
    defaultMode: 292
{{- end }}
{{- end }}
{{- if $v.sidecar.mcp.existingConfigMap }}
- name: axlr-node-mcp
  configMap:
    name: {{ $v.sidecar.mcp.existingConfigMap }}
    items: [{key: {{ required "sidecar.mcp.key is required with sidecar.mcp.existingConfigMap" $v.sidecar.mcp.key | quote }}, path: mcp.json}]
    defaultMode: 292 # 0444: the sidecar runs as 65532 whatever the pod's fsGroup
{{- end }}
{{- end -}}

{{- define "axlr-bots-node.inject.volumeClaimTemplate" -}}
{{- $v := .values -}}
{{- if $v.sidecar.state.persistent }}
- metadata:
    name: axlr-node-state
  spec:
    accessModes: [ReadWriteOnce]
    {{- with $v.sidecar.state.storageClassName }}
    storageClassName: {{ . | quote }}
    {{- end }}
    resources:
      requests:
        storage: {{ required "sidecar.state.size is required" $v.sidecar.state.size | quote }}
{{- end }}
{{- end -}}

{{- define "axlr-bots-node.inject.egress" -}}
{{- $v := .values -}}
{{- $url := include "axlr-bots-node.controlURL" $v -}}
{{- $hostPort := (urlParse $url).host -}}
{{- $parts := splitList ":" $hostPort -}}
{{- $host := first $parts -}}
{{- $port := ternary (last $parts) "443" (gt (len $parts) 1) -}}
{{- $cidr := $v.networkPolicy.controlPlaneCIDR -}}
{{- if not $cidr -}}
{{- if regexMatch "^[0-9]{1,3}(\\.[0-9]{1,3}){3}$" $host -}}
{{- $cidr = printf "%s/32" $host -}}
{{- else -}}
{{- fail "networkPolicy.controlPlaneCIDR is required when controlPlane.url names a host, not an IP" -}}
{{- end -}}
{{- end -}}
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: {{ $v.networkPolicy.dns.namespace }}
      podSelector:
        matchLabels:
          {{- toYaml $v.networkPolicy.dns.podLabels | nindent 10 }}
  ports:
    - {protocol: UDP, port: 53}
    - {protocol: TCP, port: 53}
- to:
    - ipBlock:
        cidr: {{ $cidr }}
  ports:
    - {protocol: TCP, port: {{ $port }}}
{{- if eq (toString $v.bus.kind) "nats" }}
- to:
    - ipBlock:
        cidr: {{ required "bus.nats.cidr is required with bus.kind nats (the broker's address as a CIDR)" $v.bus.nats.cidr }}
  ports:
    - {protocol: TCP, port: {{ $v.bus.nats.port }}}
{{- end }}
- to:
    {{- range $v.networkPolicy.modelProvider.cidrs }}
    - ipBlock:
        cidr: {{ . }}
        {{- with $v.networkPolicy.modelProvider.except }}
        except:
          {{- toYaml . | nindent 10 }}
        {{- end }}
    {{- end }}
  ports:
    - {protocol: TCP, port: {{ $v.networkPolicy.modelProvider.port }}}
{{- end -}}
