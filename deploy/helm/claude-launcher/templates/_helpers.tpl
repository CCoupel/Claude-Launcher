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
{{- default .Values.rootBase .Values.volume.mountPath -}}
{{- end -}}

{{/* Contrôles que le schéma ne sait pas exprimer ; messages nommant la valeur */}}
{{- define "cl.validate" -}}
{{- $root := required "rootBase est obligatoire (CL_ROOT_BASE, chemin absolu)" .Values.rootBase -}}
{{- $claim := required "volume.existingClaim est obligatoire (PVC RWX existant)" .Values.volume.existingClaim -}}
{{- include "cl.checkPath" (dict "name" "rootBase" "path" $root) -}}
{{- if .Values.volume.mountPath -}}
{{- include "cl.checkPath" (dict "name" "volume.mountPath" "path" .Values.volume.mountPath) -}}
{{- end -}}
{{- $mount := include "cl.mountPath" . -}}
{{- $vc := .Values.volume.create -}}
{{- if and (not $vc.enabled) (or $vc.persistentVolume.enabled $vc.persistentVolume.name $vc.persistentVolume.source $vc.storageClassName) -}}
{{- fail "volume.create.persistentVolume / storageClassName posés sans volume.create.enabled : ils seraient ignorés — poser volume.create.enabled: true, ou les retirer (PVC existant)" -}}
{{- end -}}
{{- if not (or (eq $root $mount) (hasPrefix (printf "%s/" $mount) $root)) -}}
{{- fail (printf "rootBase (%s) doit être volume.mountPath (%s) ou un de ses sous-dossiers" $root $mount) -}}
{{- end -}}
{{- /* variables posées par le chart : extraEnv ne peut pas les remplacer (le dernier doublon l'emporterait) */ -}}
{{- $reservedEnv := list "CL_ROOT_BASE" "CL_HUB_NAME" "CL_CACHE_DIR" "CL_K8S_VOLUMES" "CL_IN_CLUSTER" "CL_UPDATE_MODE" "CL_WEB_URL" "CL_ALLOW_HOST_BACKEND" -}}
{{- range .Values.extraEnv -}}
{{- if has .name $reservedEnv -}}
{{- fail (printf "extraEnv : %s est posée par le chart (valeur dédiée : rootBase, hubName, allowHostBackend, ingress.host…), refusée" .name) -}}
{{- end -}}
{{- end -}}
{{- if .Values.ingress.enabled -}}
{{- $_ := required "ingress.controller est obligatoire quand ingress.enabled (traefik ou nginx)" .Values.ingress.controller -}}
{{- $_ := required "ingress.className est obligatoire quand ingress.enabled" .Values.ingress.className -}}
{{- $_ := required "ingress.host est obligatoire quand ingress.enabled" .Values.ingress.host -}}
{{- if not .Values.ingress.allowedSourceRanges -}}
{{- fail "ingress.allowedSourceRanges est obligatoire et non vide quand ingress.enabled" -}}
{{- end -}}
{{- range .Values.ingress.allowedSourceRanges -}}
{{- include "cl.checkCIDR" . -}}
{{- end -}}
{{- /* annotations qui porteraient ou supprimeraient la liste d'adresses, la redirection HTTPS ou le TLS */ -}}
{{- range $k, $_ := .Values.ingress.annotations -}}
{{- if or (hasPrefix "traefik.ingress.kubernetes.io/router." $k) (has $k (list "nginx.ingress.kubernetes.io/whitelist-source-range" "nginx.ingress.kubernetes.io/allowlist-source-range" "nginx.ingress.kubernetes.io/denylist-source-range" "nginx.ingress.kubernetes.io/ssl-redirect" "nginx.ingress.kubernetes.io/force-ssl-redirect" "nginx.ingress.kubernetes.io/configuration-snippet" "nginx.ingress.kubernetes.io/server-snippet" "nginx.ingress.kubernetes.io/use-forwarded-headers")) -}}
{{- fail (printf "ingress.annotations : %s est réservée au chart (liste d'adresses, redirection HTTPS, TLS), refusée" $k) -}}
{{- end -}}
{{- end -}}
{{- if and .Values.ingress.tls.secretName .Values.ingress.tls.clusterIssuer -}}
{{- fail "ingress.tls : secretName et clusterIssuer sont exclusifs, en donner un seul" -}}
{{- end -}}
{{- if not (or .Values.ingress.tls.secretName .Values.ingress.tls.clusterIssuer) -}}
{{- fail "ingress.tls : TLS obligatoire — donner ingress.tls.secretName ou ingress.tls.clusterIssuer" -}}
{{- end -}}
{{- if eq .Values.ingress.tls.clusterIssuer "a-renseigner" -}}
{{- fail "ingress.tls.clusterIssuer vaut « a-renseigner » : donner le nom réel de l'émetteur interne (kubectl get clusterissuer)" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* chemin absolu normalisé : pas de « / » final, de « // », ni de segment « . » ou « .. » ; « / » seul refusé */}}
{{- define "cl.checkPath" -}}
{{- if not (regexMatch "^(/[^/]+)+$" .path) -}}
{{- fail (printf "%s (%s) : chemin absolu normalisé attendu (sans « / » final ni « // », « / » seul refusé)" .name .path) -}}
{{- end -}}
{{- if regexMatch "(^|/)\\.\\.?(/|$)" .path -}}
{{- fail (printf "%s (%s) : segments « . » et « .. » refusés" .name .path) -}}
{{- end -}}
{{- end -}}

{{/* une entrée de ingress.allowedSourceRanges : CIDR IPv4 (octets 0–255, masque /8–/32) ou IPv6 (masque /16–/128).
     Tout préfixe trop large est refusé, /0 compris quelle que soit son écriture (0.0.0.0/00, 10.0.0.0/0, 0::/0…). */}}
{{- define "cl.checkCIDR" -}}
{{- $c := . -}}
{{- $parts := splitList "/" $c -}}
{{- if ne (len $parts) 2 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » n'est pas un CIDR (adresse/masque)" $c) -}}
{{- end -}}
{{- $addr := index $parts 0 -}}
{{- $m := index $parts 1 -}}
{{- if not (regexMatch "^[0-9]{1,3}$" $m) -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : masque invalide" $c) -}}
{{- end -}}
{{- $mask := atoi $m -}}
{{- if regexMatch "^[0-9]{1,3}(\\.[0-9]{1,3}){3}$" $addr -}}
{{- range splitList "." $addr -}}
{{- if gt (atoi .) 255 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : octet %s hors de 0–255" $c .) -}}
{{- end -}}
{{- end -}}
{{- if gt $mask 32 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : masque IPv4 > /32" $c) -}}
{{- end -}}
{{- if lt $mask 8 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : préfixe trop large (IPv4 : /8 au minimum ; /0 = toutes les adresses)" $c) -}}
{{- end -}}
{{- else if regexMatch "^([0-9a-fA-F]{1,4}(:[0-9a-fA-F]{1,4}){7}|([0-9a-fA-F]{1,4}(:[0-9a-fA-F]{1,4}){0,6})?::([0-9a-fA-F]{1,4}(:[0-9a-fA-F]{1,4}){0,6})?)$" $addr -}}
{{- if gt $mask 128 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : masque IPv6 > /128" $c) -}}
{{- end -}}
{{- if lt $mask 16 -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : préfixe trop large (IPv6 : /16 au minimum ; /0 = toutes les adresses)" $c) -}}
{{- end -}}
{{- else -}}
{{- fail (printf "ingress.allowedSourceRanges : « %s » : adresse IPv4 ou IPv6 invalide" $c) -}}
{{- end -}}
{{- end -}}

{{- define "cl.tlsSecret" -}}
{{- if .Values.ingress.tls.secretName -}}{{ .Values.ingress.tls.secretName }}{{- else -}}{{ .Release.Name }}-tls{{- end -}}
{{- end -}}
