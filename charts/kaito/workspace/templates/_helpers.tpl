{{/*
Expand the name of the chart.
*/}}
{{- define "kaito.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kaito.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "kaito.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kaito.labels" -}}
{{- if .Values.commonLabels }}
{{- toYaml .Values.commonLabels }}
{{- else }}
helm.sh/chart: {{ include "kaito.chart" . }}
app.kubernetes.io/created-by: {{ include "kaito.chart" . }}
{{ include "kaito.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kaito.selectorLabels" -}}
{{- if .Values.selectorLabels }}
{{- toYaml .Values.selectorLabels }}
{{- else }}
app.kubernetes.io/name: {{ include "kaito.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
{{- end }}

{{/*
ServiceAccount name
*/}}
{{- define "kaito.serviceAccountName" -}}
{{- if .Values.serviceAccountName -}}
{{- .Values.serviceAccountName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-sa
{{- end -}}
{{- end -}}

{{/*
Service name
*/}}
{{- define "kaito.serviceName" -}}
{{- if .Values.serviceName -}}
{{- .Values.serviceName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-svc
{{- end -}}
{{- end -}}

{{/*
Logging ConfigMap name
*/}}
{{- define "kaito.loggingConfigMapName" -}}
{{- if .Values.loggingConfigMapName -}}
{{- .Values.loggingConfigMapName -}}
{{- else -}}
kaito-logging-config
{{- end -}}
{{- end -}}

{{/*
ClusterRole name
*/}}
{{- define "kaito.clusterRoleName" -}}
{{- if .Values.clusterRoleName -}}
{{- .Values.clusterRoleName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-clusterrole
{{- end -}}
{{- end -}}

{{/*
ClusterRoleBinding name
*/}}
{{- define "kaito.clusterRoleBindingName" -}}
{{- if .Values.clusterRoleBindingName -}}
{{- .Values.clusterRoleBindingName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-rolebinding
{{- end -}}
{{- end -}}

{{/*
Role name
*/}}
{{- define "kaito.roleName" -}}
{{- if .Values.roleName -}}
{{- .Values.roleName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-role
{{- end -}}
{{- end -}}

{{/*
RoleBinding name
*/}}
{{- define "kaito.roleBindingName" -}}
{{- if .Values.roleBindingName -}}
{{- .Values.roleBindingName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-rolebinding
{{- end -}}
{{- end -}}

{{/*
Deployment name
*/}}
{{- define "kaito.deploymentName" -}}
{{- if .Values.deploymentName -}}
{{- .Values.deploymentName -}}
{{- else -}}
{{- include "kaito.fullname" . }}
{{- end -}}
{{- end -}}

{{/*
InferenceSet ClusterRole name
*/}}
{{- define "kaito.inferenceSetClusterRoleName" -}}
{{- if .Values.inferenceSetClusterRoleName -}}
{{- .Values.inferenceSetClusterRoleName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-inferenceset-clusterrole
{{- end -}}
{{- end -}}

{{/*
InferenceSet ClusterRoleBinding name
*/}}
{{- define "kaito.inferenceSetClusterRoleBindingName" -}}
{{- if .Values.inferenceSetClusterRoleBindingName -}}
{{- .Values.inferenceSetClusterRoleBindingName -}}
{{- else -}}
{{- include "kaito.fullname" . }}-inferenceset-rolebinding
{{- end -}}
{{- end -}}

{{/*
Utils function
*/}}
{{- define "utils.joinKeyValuePairs" -}}
{{- $pairs := list -}}
{{- range $key, $value := . -}}
{{- $pairs = append $pairs (printf "%s=%t" $key $value) -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end -}}
