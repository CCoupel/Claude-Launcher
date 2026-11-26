{{/* Nom des objets : nom de la release, tronqué à 63 caractères */}}
{{- define "cl.fullname" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cl.labels" -}}
app.kubernetes.io/name: claude-launcher
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "cl.versionLabel" . | quote }}
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

{{/* référence de l'image : dépôt:tag, ou dépôt:tag@empreinte si image.digest (l'image installée est exactement celle publiée) */}}
{{- define "cl.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s:%s@%s" .Values.image.repository (include "cl.tag" .) .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (include "cl.tag" .) -}}
{{- end -}}
{{- end -}}

{{/* valeur de label : le tag seul (ni « @ » ni plus de 63 caractères) */}}
{{- define "cl.versionLabel" -}}
{{- include "cl.tag" . | trunc 63 | trimSuffix "-" | trimSuffix "." -}}
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
{{- include "cl.validateAuth" . -}}
{{- if and .Values.networkPolicy.enabled (not .Values.networkPolicy.ingressNamespaces) (not .Values.networkPolicy.ingressCIDRs) -}}
{{- fail "networkPolicy.enabled sans ingressNamespaces ni ingressCIDRs : plus aucune entrée admise (même l'Ingress) — donner le namespace du contrôleur, ou networkPolicy.enabled: false" -}}
{{- end -}}
{{- /* montages du chart : la racine partagée ne chevauche ni le coffre ni le Secret docker */ -}}
{{- range $p := list "/var/lib/claude-launcher/secrets-legacy" "/var/run/claude-launcher/docker" -}}
{{- if or (eq $mount $p) (hasPrefix (printf "%s/" $mount) $p) (hasPrefix (printf "%s/" $p) $mount) -}}
{{- fail (printf "volume.mountPath (%s) chevauche le montage réservé %s" $mount $p) -}}
{{- end -}}
{{- end -}}
{{- /* backends.kubeconfigs : un nom par entrée (volume kube-<name> et dossier de montage) */ -}}
{{- $seen := dict -}}
{{- range .Values.backends.kubeconfigs -}}
{{- if hasKey $seen .name -}}
{{- fail (printf "backends.kubeconfigs : le nom %q est donné deux fois (un volume et un dossier de montage par nom)" .name) -}}
{{- end -}}
{{- $_ := set $seen .name true -}}
{{- end -}}
{{- /* variables posées par le chart : extraEnv ne peut pas les remplacer (le dernier doublon l'emporterait) */ -}}
{{- $reservedEnv := list "CL_ROOT_BASE" "CL_HUB_NAME" "CL_CACHE_DIR" "CL_K8S_VOLUMES" "CL_IN_CLUSTER" "CL_UPDATE_MODE" "CL_WEB_URL" "CL_ALLOW_HOST_BACKEND" "CL_WEB_ACCESS" "CL_WEB_ADDR" "CL_HEALTH_ADDR" "CL_WEB_ALT_URLS" "CL_AUTH_ADMINS" "CL_AUTH_USERS" "CL_AUTH_LOCAL_ADMIN_HOSTS" "CL_AUTH_SESSION_DURATION" "CL_AUTH_SESSION_REFRESH" "CL_SECRETS_DIR" "CL_SECRETS_LEGACY_DIR" "DOCKER_CERT_PATH" "DOCKER_TLS_VERIFY" -}}
{{- range .Values.extraEnv -}}
{{- if has .name $reservedEnv -}}
{{- fail (printf "extraEnv : %s est posée par le chart (valeur dédiée : rootBase, hubName, allowHostBackend, web.access, ingress.host, auth.*…), refusée" .name) -}}
{{- end -}}
{{- end -}}
{{- if and (not .Values.auth.enabled) (not .Values.ingress.enabled) (eq (.Values.web.access | default "none") "none") -}}
{{- fail "web.access: none sans Ingress refusé (accès sans jeton sans Ingress : poser web.access: token) — le launcher exige une adresse publique https pour l'accès sans jeton" -}}
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

{{/* ancien coffre v6.4.0 : monté (lecture seule) seulement si l'ancien PVC <release>-secrets existe (migration, #201) ; rendu vide hors cluster (helm template) */}}
{{- define "cl.legacySecrets" -}}
{{- if .Values.secrets.legacy.enabled -}}
{{- if lookup "v1" "PersistentVolumeClaim" .Release.Namespace (printf "%s-secrets" (include "cl.fullname" .)) -}}true{{- end -}}
{{- end -}}
{{- end -}}

{{/* ── v7.0.0 : accès authentifié (contracts/helm-values.md « Valeurs auth.* ») ── */}}

{{/* logins GitHub : forme d'un login (1 à 39 caractères alphanumériques ou « - », ni début ni fin par « - »), en minuscules, sans doublon */}}
{{- define "cl.checkLogins" -}}
{{- $name := .name -}}
{{- $seen := dict -}}
{{- range .list -}}
{{- $l := lower (toString .) -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{0,37}[a-z0-9])?$" $l) -}}
{{- fail (printf "%s : « %v » n'est pas un login GitHub valide" $name .) -}}
{{- end -}}
{{- if hasKey $seen $l -}}
{{- fail (printf "%s : le login %q est donné deux fois" $name $l) -}}
{{- end -}}
{{- $_ := set $seen $l true -}}
{{- end -}}
{{- end -}}

{{/* durée « 15m » / « 8h » en minutes */}}
{{- define "cl.minutes" -}}
{{- $d := toString .value -}}
{{- if not (regexMatch "^[1-9][0-9]*[hm]$" $d) -}}
{{- fail (printf "%s : durée « %s » invalide (entier suivi de h ou m, ex. 8h, 30m)" .name $d) -}}
{{- end -}}
{{- if hasSuffix "h" $d -}}{{ mul (atoi (trimSuffix "h" $d)) 60 }}{{- else -}}{{ atoi (trimSuffix "m" $d) }}{{- end -}}
{{- end -}}

{{/* hôtes où le compte local est admis : auth.localAdmin.hosts, défaut ingress.host ; vide si le compte local est désactivé */}}
{{- define "cl.localHosts" -}}
{{- if and .Values.auth.enabled .Values.auth.localAdmin.enabled -}}
{{- if .Values.auth.localAdmin.hosts -}}{{ join "," .Values.auth.localAdmin.hosts }}{{- else if .Values.ingress.enabled -}}{{ .Values.ingress.host }}{{- end -}}
{{- end -}}
{{- end -}}

{{/* adresse publique principale (CL_WEB_URL) : l'hôte externe s'il est activé (P7), sinon l'hôte interne */}}
{{- define "cl.primaryURL" -}}
{{- if .Values.externalIngress.enabled -}}https://{{ .Values.externalIngress.host }}{{ .Values.externalIngress.pathPrefix | default "" }}{{- else if .Values.ingress.enabled -}}https://{{ .Values.ingress.host }}{{- end -}}
{{- end -}}

{{/* Secret de la GitHub App : celui fourni (existingSecret), sinon <release>-github créé par le chart (clientId / clientSecret) */}}
{{- define "cl.githubSecretName" -}}
{{- if .Values.auth.existingSecret -}}{{ .Values.auth.existingSecret }}{{- else -}}{{ include "cl.fullname" . }}-github{{- end -}}
{{- end -}}
{{- define "cl.cookieSecretName" -}}{{ include "cl.fullname" . }}-auth-cookie{{- end -}}
{{- define "cl.cookieSecretNameExt" -}}{{ include "cl.fullname" . }}-auth-cookie-ext{{- end -}}
{{/* hôte de l'instance interne : l'ingress interne, sinon l'externe (seule adresse) */}}
{{- define "cl.internalHost" -}}{{ if .Values.ingress.enabled }}{{ .Values.ingress.host }}{{ else }}{{ .Values.externalIngress.host }}{{ end }}{{- end -}}
{{- define "cl.localAdminSecretName" -}}
{{- default (printf "%s-local-admin" (include "cl.fullname" .)) .Values.auth.localAdmin.existingSecret -}}
{{- end -}}

{{- define "cl.validateAuth" -}}
{{- $a := .Values.auth -}}
{{- if .Values.externalIngress.enabled -}}
{{- if not $a.enabled -}}
{{- fail "ingress externe refusé : authentification désactivée (auth.enabled: false)" -}}
{{- end -}}
{{- $_ := required "externalIngress.className est obligatoire quand externalIngress.enabled" .Values.externalIngress.className -}}
{{- $_ := required "externalIngress.host est obligatoire quand externalIngress.enabled" .Values.externalIngress.host -}}
{{- include "cl.checkHostname" (dict "name" "externalIngress.host" "host" .Values.externalIngress.host) -}}
{{- if and .Values.externalIngress.pathPrefix (not (regexMatch "^/[a-z0-9-]+(/[a-z0-9-]+)*$" .Values.externalIngress.pathPrefix)) -}}
{{- fail (printf "externalIngress.pathPrefix : « %s » invalide (segments en minuscules a-z 0-9 -, ex. /claude-qualif ; sans « / » final, sans « // », sans « .. »)" .Values.externalIngress.pathPrefix) -}}
{{- end -}}
{{- if and .Values.externalIngress.tls.secretName .Values.externalIngress.tls.clusterIssuer -}}
{{- fail "externalIngress.tls : secretName et clusterIssuer sont exclusifs, en donner un seul" -}}
{{- end -}}
{{- if not (or .Values.externalIngress.tls.secretName .Values.externalIngress.tls.clusterIssuer) -}}
{{- fail "externalIngress.tls : TLS obligatoire — donner externalIngress.tls.secretName ou externalIngress.tls.clusterIssuer" -}}
{{- end -}}
{{- if and .Values.ingress.enabled .Values.externalIngress.tls.clusterIssuer (eq .Values.externalIngress.tls.clusterIssuer .Values.ingress.tls.clusterIssuer) -}}
{{- fail (printf "externalIngress.tls.clusterIssuer (%s) est celui de l'ingress interne : un émetteur public est exigé (l'émetteur interne a des contraintes de nom)" .Values.externalIngress.tls.clusterIssuer) -}}
{{- end -}}
{{- if eq .Values.externalIngress.host .Values.ingress.host -}}
{{- fail "externalIngress.host est celui de ingress.host : deux noms distincts exigés" -}}
{{- end -}}
{{- if not $a.github.enabled -}}
{{- fail "ingress externe refusé : auth.github.enabled: false (le compte local n'est jamais admis depuis Internet)" -}}
{{- end -}}
{{- range $k, $_ := .Values.externalIngress.annotations -}}
{{- if or (hasPrefix "traefik.ingress.kubernetes.io/router." $k) (has $k (list "nginx.ingress.kubernetes.io/whitelist-source-range" "nginx.ingress.kubernetes.io/allowlist-source-range" "nginx.ingress.kubernetes.io/denylist-source-range" "nginx.ingress.kubernetes.io/ssl-redirect" "nginx.ingress.kubernetes.io/force-ssl-redirect" "nginx.ingress.kubernetes.io/configuration-snippet" "nginx.ingress.kubernetes.io/server-snippet" "nginx.ingress.kubernetes.io/use-forwarded-headers")) -}}
{{- fail (printf "externalIngress.annotations : %s est réservée au chart (redirection HTTPS, TLS), refusée" $k) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $a.enabled -}}
{{- if .Values.allowHostBackend -}}
{{- fail "backend host interdit derrière le proxy d'authentification (allowHostBackend: true avec auth.enabled) : une session du conteneur du launcher forgerait l'identité" -}}
{{- end -}}
{{- if and $a.localAdmin.enabled (eq (default "traefik" .Values.ingress.controller) "nginx") -}}
{{- fail "ingress.controller: nginx refusé avec auth.localAdmin.enabled : le retrait de l'en-tête Authorization et la limite d'essais passent par un snippet que ingress-nginx désactive par défaut (chemin Basic-auth ouvert) — utiliser Traefik, ou désactiver le compte local" -}}
{{- end -}}
{{- if .Values.web.access -}}
{{- fail "web.access est imposé à proxy par auth.enabled ; le retirer (ou auth.enabled: false)" -}}
{{- end -}}
{{- if not (or .Values.ingress.enabled .Values.externalIngress.enabled) -}}
{{- fail "auth.enabled sans ingress.enabled ni externalIngress.enabled : aucune adresse https pour le retour OAuth et CL_WEB_URL" -}}
{{- end -}}
{{- if and (not $a.github.enabled) (not $a.localAdmin.enabled) -}}
{{- fail "auth.github.enabled: false exige auth.localAdmin.enabled: true (aucune connexion possible sinon)" -}}
{{- end -}}
{{- if or (and $a.github.clientId (not (trim $a.github.clientId))) (and $a.github.clientSecret (not (trim $a.github.clientSecret))) -}}
{{- fail "auth.github.clientId / clientSecret : valeur vide (espaces seuls) refusée — la paire complète de la GitHub App est exigée" -}}
{{- end -}}
{{- if and $a.existingSecret (or $a.github.clientId $a.github.clientSecret) -}}
{{- fail "auth.existingSecret et auth.github.clientId/clientSecret sont exclusifs : en donner un seul" -}}
{{- end -}}
{{- if ne (not (not $a.github.clientId)) (not (not $a.github.clientSecret)) -}}
{{- fail "auth.github.clientId et auth.github.clientSecret vont ensemble : la paire complète est exigée" -}}
{{- end -}}
{{- if $a.github.enabled -}}
{{- if not (or $a.existingSecret $a.github.clientId) -}}
{{- fail "auth.github.enabled : donner auth.existingSecret (recommandé : Secret du namespace, clés client-id et client-secret de la GitHub App) ou la paire auth.github.clientId / clientSecret" -}}
{{- end -}}
{{- if not $a.admins -}}
{{- fail "auth.admins est obligatoire et non vide quand auth.github.enabled (logins GitHub administrateurs)" -}}
{{- end -}}
{{- end -}}
{{- include "cl.checkLogins" (dict "name" "auth.admins" "list" $a.admins) -}}
{{- include "cl.checkLogins" (dict "name" "auth.users" "list" $a.users) -}}
{{- range $a.users -}}
{{- $u := lower (toString .) -}}
{{- range $a.admins -}}
{{- if eq (lower (toString .)) $u -}}
{{- fail (printf "auth.users : %q est aussi dans auth.admins (un seul rôle par login)" $u) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $dur := include "cl.minutes" (dict "name" "auth.sessionDuration" "value" $a.sessionDuration) | atoi -}}
{{- $ref := include "cl.minutes" (dict "name" "auth.sessionRefresh" "value" $a.sessionRefresh) | atoi -}}
{{- if or (lt $dur 15) (gt $dur 10080) -}}
{{- fail (printf "auth.sessionDuration (%s) : 15m au minimum, 168h au maximum" $a.sessionDuration) -}}
{{- end -}}
{{- if ge $ref $dur -}}
{{- fail (printf "auth.sessionRefresh (%s) doit être inférieur à auth.sessionDuration (%s)" $a.sessionRefresh $a.sessionDuration) -}}
{{- end -}}
{{- if .Values.externalIngress.enabled -}}
{{- $edur := include "cl.minutes" (dict "name" "externalIngress.sessionDuration" "value" .Values.externalIngress.sessionDuration) | atoi -}}
{{- if or (lt $edur 15) (gt $edur 10080) -}}
{{- fail (printf "externalIngress.sessionDuration (%s) : 15m au minimum, 168h au maximum" .Values.externalIngress.sessionDuration) -}}
{{- end -}}
{{- if ge $ref $edur -}}
{{- fail (printf "externalIngress.sessionDuration (%s) doit rester supérieure à auth.sessionRefresh (%s)" .Values.externalIngress.sessionDuration $a.sessionRefresh) -}}
{{- end -}}
{{- end -}}
{{- $pi := $a.proxy.image -}}
{{- if or (not $pi.tag) (eq (lower $pi.tag) "latest") -}}
{{- fail "auth.proxy.image.tag : une version précise est exigée (jamais latest ni vide)" -}}
{{- end -}}
{{- if $a.localAdmin.enabled -}}
{{- range $a.localAdmin.hosts -}}
{{- if and $.Values.externalIngress.enabled (eq . $.Values.externalIngress.host) -}}
{{- fail (printf "auth.localAdmin.hosts : %s est l'hôte externe — le compte local n'est jamais admis depuis Internet" .) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$" (lower .)) -}}
{{- fail (printf "auth.localAdmin.hosts : « %s » n'est pas un nom d'hôte" .) -}}
{{- end -}}
{{- end -}}
{{- if $a.localAdmin.existingSecret -}}
{{- $ls := lookup "v1" "Secret" .Release.Namespace $a.localAdmin.existingSecret -}}
{{- if $ls -}}
{{- $h := (index $ls.data "htpasswd" | default "") | b64dec -}}
{{- if not (regexMatch "^admin@local:\\$2[aby]\\$(1[0-9]|[2-3][0-9])\\$.{53}\\s*$" $h) -}}
{{- fail (printf "auth.localAdmin.existingSecret (%s) : la clé htpasswd doit être « admin@local:$2y$<coût ≥ 10>$… » (bcrypt)" $a.localAdmin.existingSecret) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- else -}}
{{- if or $a.localAdmin.hosts $a.localAdmin.existingSecret -}}
{{- /* auth.localAdmin.* sans auth.enabled : ignorés, sans erreur (valeurs par défaut du chart) */ -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* image du sidecar oauth2-proxy : dépôt:tag, ou dépôt:tag@empreinte */}}
{{- define "cl.proxyImage" -}}
{{- $i := .Values.auth.proxy.image -}}
{{- if $i.digest -}}{{ printf "%s:%s@%s" $i.repository $i.tag $i.digest }}{{- else -}}{{ printf "%s:%s" $i.repository $i.tag }}{{- end -}}
{{- end -}}

{{/* conteneur oauth2-proxy (contracts/helm-values.md « Pod du launcher avec auth.enabled ») : dict root, name, port, portName,
     local (compte local : htpasswd + formulaire ; jamais sur l'instance externe), host (hôte de l'instance : URL de retour fixe),
     duration (--cookie-expire), cookie (nom du cookie), secret (Secret du cookie), tpl (volume des gabarits), base (sous-chemin public de l'instance, "" = racine) */}}
{{- define "cl.authProxy" -}}
{{- $root := .root -}}
{{- $a := $root.Values.auth -}}
{{- $logins := concat ($a.admins | default list) ($a.users | default list) | uniq -}}
- name: {{ .name }}
  image: {{ include "cl.proxyImage" $root | quote }}
  imagePullPolicy: {{ $root.Values.image.pullPolicy }}
  securityContext:
    runAsNonRoot: true
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    capabilities:
      drop: [ALL]
  args:
    - --provider=github
    {{- if $a.github.enabled }}
    - --github-user={{ join "," $logins | lower }}
    {{- else }}
    # sans connexion GitHub : client factice (le bouton GitHub n'est pas rendu), aucun Secret exigé
    - --client-id=disabled
    - --client-secret=disabled
    {{- end }}
    - --email-domain=*
    - --upstream=http://127.0.0.1:8081/
    - --http-address=0.0.0.0:{{ .port }}
    - --reverse-proxy=true
    - --pass-user-headers=true
    - --pass-access-token=false
    - --skip-provider-button={{ if .local }}false{{ else }}true{{ end }}
    - --api-route=^{{ .base }}/api/
    {{- if .base }}
    - --skip-auth-route=GET=^{{ .base }}/(signed-out|too-many)$
    {{- else }}
    - --skip-auth-route=GET=^/signed-out$
    - --skip-auth-route=GET=^/too-many$
    {{- end }}
    {{- if .base }}
    # sous-chemin public : le préfixe n'est jamais retiré par Traefik ; oauth2-proxy vit sous <base>/oauth2 et son cookie sous <base>
    - --proxy-prefix={{ .base }}/oauth2
    - --cookie-path={{ .base }}
    {{- end }}
    - --cookie-name={{ .cookie }}
    - --cookie-secure=true
    - --cookie-samesite=lax
    - --cookie-expire={{ .duration }}
    - --cookie-refresh={{ $a.sessionRefresh }}
    - --custom-templates-dir=/etc/cl-auth/templates
    - --request-logging=false
    - --auth-logging=true
    - --ping-path=/ping
    # chemins d'authentification annexes fermés (contracts/auth.md §8, m4) : ni Basic ni Authorization transmis, retour OAuth fixe
    - --pass-basic-auth=false
    - --pass-authorization-header=false
    - --set-xauthrequest=false
    - --skip-auth-strip-headers=true
    - --redirect-url=https://{{ .host }}{{ .base }}/oauth2/callback
    {{- if .local }}
    - --htpasswd-file=/etc/cl-auth/local/htpasswd
    - --display-htpasswd-form=true
    {{- end }}
  env:
    {{- if $a.github.enabled }}
    - name: OAUTH2_PROXY_CLIENT_ID
      valueFrom:
        secretKeyRef: {name: {{ include "cl.githubSecretName" $root | quote }}, key: client-id}
    - name: OAUTH2_PROXY_CLIENT_SECRET
      valueFrom:
        secretKeyRef: {name: {{ include "cl.githubSecretName" $root | quote }}, key: client-secret}
    {{- end }}
    - name: OAUTH2_PROXY_COOKIE_SECRET
      valueFrom:
        secretKeyRef: {name: {{ .secret | quote }}, key: cookie-secret}
  ports:
    - name: {{ .portName }}
      containerPort: {{ .port }}
      protocol: TCP
  volumeMounts:
    - name: {{ .tpl }}
      mountPath: /etc/cl-auth/templates
      readOnly: true
    {{- if .local }}
    - name: auth-local
      mountPath: /etc/cl-auth/local
      readOnly: true
    {{- end }}
  livenessProbe:
    httpGet: {path: /ping, port: {{ .portName }}}
    periodSeconds: 20
    timeoutSeconds: 5
    failureThreshold: 3
  readinessProbe:
    httpGet: {path: /ping, port: {{ .portName }}}
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 3
  resources:
    {{- toYaml $a.proxy.resources | nindent 4 }}
{{- end -}}

{{/* contrôleur de l'ingress externe : celui de l'interne, sinon Traefik (contrôleur du cluster) */}}
{{- define "cl.extController" -}}
{{- default "traefik" .Values.ingress.controller -}}
{{- end -}}

{{/* nom d'hôte DNS en minuscules (RFC 1123) : étiquettes de 1 à 63 caractères [a-z0-9-] sans « - » en tête ni en queue, 253 au total ;
     ni schéma, ni chemin, ni espace, ni joker, ni majuscule */}}
{{- define "cl.checkHostname" -}}
{{- $h := toString .host -}}
{{- if or (ne $h (lower $h)) (gt (len $h) 253) -}}
{{- fail (printf "%s : « %s » : nom d'hôte en minuscules RFC 1123 attendu (au plus 253 caractères)" .name $h) -}}
{{- end -}}
{{- range (splitList "." $h) -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$" .) -}}
{{- fail (printf "%s : « %s » : nom d'hôte en minuscules RFC 1123 attendu (étiquettes de 1 à 63 caractères a-z 0-9 -, sans tiret en tête ou en queue, sans schéma, chemin, espace ni joker)" $.name $h) -}}
{{- end -}}
{{- end -}}
{{- end -}}
