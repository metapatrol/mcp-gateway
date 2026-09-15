{{/*
Expand the name of the chart.
*/}}
{{- define "mcp-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "mcp-gateway.fullname" -}}
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
Create chart label.
*/}}
{{- define "mcp-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "mcp-gateway.labels" -}}
helm.sh/chart: {{ include "mcp-gateway.chart" . }}
{{ include "mcp-gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels, shared by both components. The component label is what tells
them apart, so every selector that means one of them adds it.
*/}}
{{- define "mcp-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Component names. The gateway is what the release is named after, so it takes
the plain name, as its binary does: mcp-gateway and mcp-gateway-controller.
*/}}
{{- define "mcp-gateway.controller.fullname" -}}
{{- printf "%s-controller" (include "mcp-gateway.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mcp-gateway.gateway.fullname" -}}
{{- include "mcp-gateway.fullname" . }}
{{- end }}

{{/*
ServiceAccount names.
*/}}
{{- define "mcp-gateway.controller.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.create }}
{{- default (include "mcp-gateway.controller.fullname" .) .Values.controller.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.controller.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "mcp-gateway.gateway.serviceAccountName" -}}
{{- if .Values.gateway.serviceAccount.create }}
{{- default (include "mcp-gateway.gateway.fullname" .) .Values.gateway.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.gateway.serviceAccount.name }}
{{- end }}
{{- end }}

{{- /*
  The image tag, in three steps.

  An explicit tag always wins. Otherwise the chart version decides, and it
  decides differently depending on where the chart came from:

    0.3.0                 a released chart -> appVersion, the released image
    0.3.0+ab12cd34ef56    a Git source     -> that commit's image

  Flux appends the commit to the chart version when a HelmRelease reads a chart
  from a GitRepository with `reconcileStrategy: Revision`. Without this branch
  such a release tracks the chart on every commit while pinning the images to
  the last cut release. CI pushes images under that same twelve-character tag so
  this resolves.

  Called with a dict: the root context as "root", the component's image values
  as "image".
*/ -}}
{{- define "mcp-gateway.image" -}}
{{- $tag := .image.tag -}}
{{- if not $tag -}}
  {{- $parts := splitList "+" .root.Chart.Version -}}
  {{- if gt (len $parts) 1 -}}
    {{- $tag = last $parts -}}
  {{- else -}}
    {{- $tag = .root.Chart.AppVersion -}}
  {{- end -}}
{{- end -}}
{{- printf "%s:%s" .image.repository $tag }}
{{- end }}

{{/*
A port taken from a bind address, so the Service, the container port and the
flag cannot drift apart. ":8080" and "0.0.0.0:8080" both yield 8080.
*/}}
{{- define "mcp-gateway.port" -}}
{{- last (splitList ":" (toString .)) -}}
{{- end }}
