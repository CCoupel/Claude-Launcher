{{/* Nom des objets : nom de la release, tronqué à 63 caractères */}}
{{- define "cl.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cl.labels" -}}
app.kubernetes.io/name: claude-launcher
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "cl.tag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "cl.selector" -}}
app.kubernetes.io/name: claude-launcher
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cl.tag" -}}
{{- default .Chart.AppVersion .Values.image.tag -}}
{{- end -}}

{{- define "cl.hubName" -}}
{{- default .Release.Name .Values.hubName -}}
{{- end -}}

{{- define "cl.mountPath" -}}
{{- default .Values.rootBase .Values.volume.mountPath | trimSuffix "/" -}}
{{- end -}}

{{/* Contrôles que le schéma ne sait pas exprimer ; messages nommant la valeur */}}
{{- define "cl.validate" -}}
{{- $root := required "rootBase est obligatoire (CL_ROOT_BASE, chemin absolu)" .Values.rootBase | trimSuffix "/" -}}
{{- $claim := required "volume.existingClaim est obligatoire (PVC RWX existant)" .Values.volume.existingClaim -}}
{{- $mount := include "cl.mountPath" . -}}
{{- if not (or (eq $root $mount) (hasPrefix (printf "%s/" $mount) $root)) -}}
{{- fail (printf "rootBase (%s) doit être volume.mountPath (%s) ou un de ses sous-dossiers" $root $mount) -}}
{{- end -}}
{{- if .Values.ingress.enabled -}}
{{- $_ := required "ingress.controller est obligatoire quand ingress.enabled (traefik ou nginx)" .Values.ingress.controller -}}
{{- $_ := required "ingress.className est obligatoire quand ingress.enabled" .Values.ingress.className -}}
{{- $_ := required "ingress.host est obligatoire quand ingress.enabled" .Values.ingress.host -}}
{{- if not .Values.ingress.allowedSourceRanges -}}
{{- fail "ingress.allowedSourceRanges est obligatoire et non vide quand ingress.enabled" -}}
{{- end -}}
{{- range .Values.ingress.allowedSourceRanges -}}
{{- if or (eq . "0.0.0.0/0") (eq . "::/0") -}}
{{- fail (printf "ingress.allowedSourceRanges : %s refusé (toutes les adresses)" .) -}}
{{- end -}}
{{- end -}}
{{- if and .Values.ingress.tls.secretName .Values.ingress.tls.clusterIssuer -}}
{{- fail "ingress.tls : secretName et clusterIssuer sont exclusifs, en donner un seul" -}}
{{- end -}}
{{- if not (or .Values.ingress.tls.secretName .Values.ingress.tls.clusterIssuer) -}}
{{- fail "ingress.tls : TLS obligatoire — donner ingress.tls.secretName ou ingress.tls.clusterIssuer" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "cl.tlsSecret" -}}
{{- if .Values.ingress.tls.secretName -}}{{ .Values.ingress.tls.secretName }}{{- else -}}{{ .Release.Name }}-tls{{- end -}}
{{- end -}}
