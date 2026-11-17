#!/bin/bash
# Comptes de déploiement « ng2-deploy » : installent et mettent à jour le chart Helm du launcher du cluster
# (deploy/helm/claude-launcher, contracts/helm-values.md). Un compte PAR NAMESPACE (décision H1, LOT-4.2) :
#   QUALIF → namespace claude-qualif      PROD → namespace claude-ng2
#
# À lancer UNE FOIS par l'administrateur, depuis une machine qui a déjà un accès administrateur au
# cluster (WSL), à la racine du projet :   bash .claude-launcher/k8s-acces-deploy.sh
# Prérequis : les deux namespaces existent (fiche administrateur : création, PVC, labels Pod Security).
#
# Crée dans chaque namespace :
#   - le compte de service ng2-deploy et son Role, limité à ce namespace (aucun droit sur l'autre :
#     le compte de QUALIF ne peut rien faire à la PROD) ;
#   - son jeton (Secret ng2-deploy-<env>-token) ;
#   - la lecture de ce seul namespace (ClusterRole ng2-deploy-namespace-<env>, resourceNames).
# puis écrit un kubeconfig par environnement dans le dossier privé du projet (jamais commité) :
#   PRIVATE/QUALIF/ng2-deploy.kubeconfig   PRIVATE/PROD/ng2-deploy.kubeconfig
#
# Droits : ceux des objets du chart (ServiceAccount, Role, RoleBinding, StatefulSet, Service, Ingress,
# Middleware Traefik, Certificate cert-manager), les Secrets (Helm y stocke l'état des releases, et le
# Secret TLS d'un certificat fourni), plus — règle d'escalade de Kubernetes — tous les droits que le Role
# du chart accorde au launcher (un compte ne peut accorder que ce qu'il possède : pas de « escalate »/« bind »).
# Ni PVC (le chart utilise le PVC existant), ni NetworkPolicy, ni ClusterRole en écriture.
#
# Variables : CTX (contexte admin, défaut k8s), SERVER (URL de l'API écrite dans le kubeconfig, défaut
# l'adresse interne au cluster, celle des pods de développement), ENVS (défaut « QUALIF PROD »),
# NS_QUALIF (défaut claude-qualif), NS_PROD (défaut claude-ng2).
#
# Révoquer un environnement :  kubectl --context k8s -n claude-ng2 delete secret ng2-deploy-prod-token
# Tout retirer (par environnement, ici PROD) :
#   kubectl --context k8s -n claude-ng2 delete sa/ng2-deploy role/ng2-deploy rolebinding/ng2-deploy secret/ng2-deploy-prod-token
#   kubectl --context k8s delete clusterrole/ng2-deploy-namespace-prod clusterrolebinding/ng2-deploy-namespace-prod
set -euo pipefail
CTX=${CTX:-k8s}
NS_QUALIF=${NS_QUALIF:-claude-qualif}
NS_PROD=${NS_PROD:-claude-ng2}
ns_of() { case "$1" in QUALIF) echo "$NS_QUALIF" ;; PROD) echo "$NS_PROD" ;; *) echo "⚠ environnement inconnu : $1 (QUALIF ou PROD)" >&2; exit 1 ;; esac; }
SERVER=${SERVER:-https://kubernetes.default.svc}
ENVS=${ENVS:-QUALIF PROD}
PRIVATE_DIR="$(cd "$(dirname "$0")/.." && pwd)/PRIVATE"
K="kubectl --context $CTX"

[ -d "$PRIVATE_DIR" ] || { echo "⚠ $PRIVATE_DIR introuvable : lancer depuis le checkout principal du projet"; exit 1; }


for ENV in $ENVS; do
  env=$(echo "$ENV" | tr '[:upper:]' '[:lower:]')
  NS=$(ns_of "$ENV")
  $K get namespace "$NS" >/dev/null || { echo "⚠ namespace $NS absent : le créer d'abord (fiche administrateur)"; exit 1; }
  $K apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata: {name: ng2-deploy, namespace: $NS}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: ng2-deploy, namespace: $NS}
rules:
  # objets du chart
  - apiGroups: [""]
    resources: [serviceaccounts, services, secrets]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [roles, rolebindings]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [apps]
    resources: [statefulsets, statefulsets/scale]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [networking.k8s.io]
    resources: [ingresses]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [traefik.io]
    resources: [middlewares]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [cert-manager.io]
    resources: [certificates]
    verbs: [get, list, watch, create, update, patch, delete]
  # droits accordés au launcher par le Role du chart (escalade interdite sans les posséder) ;
  # pods/events/PVC en lecture servent aussi à « helm --wait » et au diagnostic
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch, delete]
  - apiGroups: [""]
    resources: [pods/exec]
    verbs: [create, get]
  - apiGroups: [""]
    resources: [pods/log, events]
    verbs: [get, list]
  # PVC de la racine créé par le chart (volume.create) ; jamais supprimé (resource-policy keep) : pas de delete
  - apiGroups: [""]
    resources: [persistentvolumeclaims]
    verbs: [get, list, watch, create, update, patch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: ng2-deploy, namespace: $NS}
subjects: [{kind: ServiceAccount, name: ng2-deploy, namespace: $NS}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: ng2-deploy}
---
# lecture de ce seul namespace (diagnostic, sondes d'accès)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: ng2-deploy-namespace-$env}
rules:
  - apiGroups: [""]
    resources: [namespaces]
    resourceNames: [$NS]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: ng2-deploy-namespace-$env}
subjects: [{kind: ServiceAccount, name: ng2-deploy, namespace: $NS}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: ng2-deploy-namespace-$env}
EOF
  SECRET=ng2-deploy-$env-token
  OUT=$PRIVATE_DIR/$ENV/ng2-deploy.kubeconfig
  $K apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: $SECRET
  namespace: $NS
  annotations: {kubernetes.io/service-account.name: ng2-deploy}
type: kubernetes.io/service-account-token
EOF
  echo "attente du jeton $ENV…"
  TOKEN=
  for i in $(seq 1 30); do
    TOKEN=$($K -n $NS get secret $SECRET -o jsonpath='{.data.token}' 2>/dev/null | base64 -d || true)
    [ -n "$TOKEN" ] && break; sleep 1
  done
  [ -n "$TOKEN" ] || { echo "⚠ jeton $ENV non créé"; exit 1; }
  CA=$($K -n $NS get secret $SECRET -o jsonpath='{.data.ca\.crt}')

  mkdir -p "$(dirname "$OUT")"; chmod 700 "$(dirname "$OUT")"
  ( umask 077
    cat > "$OUT" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: k8s
    cluster:
      server: $SERVER
      certificate-authority-data: $CA
users:
  - name: ng2-deploy-$env
    user:
      token: $TOKEN
contexts:
  - name: k8s-deploy
    context: {cluster: k8s, user: ng2-deploy-$env, namespace: $NS}
current-context: k8s-deploy
EOF
  )
  ls -l "$OUT"
done

echo "✓ comptes ng2-deploy prêts ($(for e in $ENVS; do printf '%s → %s ' "$e" "$(ns_of "$e")"; done), serveur $SERVER) — kubeconfig : $(for e in $ENVS; do printf 'PRIVATE/%s/ng2-deploy.kubeconfig ' "$e"; done)"
echo "  Vérification (depuis le pod du projet) :"
echo "    KUBECONFIG=PRIVATE/QUALIF/ng2-deploy.kubeconfig kubectl auth can-i create ingresses -n $NS_QUALIF"
