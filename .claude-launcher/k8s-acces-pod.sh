#!/bin/bash
# Accès au cluster pour les pods NG2 qui développent/testent le launcher (tests k8s
# lancés depuis le pod du projet Claude-Launcher).
#
# À lancer UNE FOIS depuis une machine qui a déjà un accès administrateur au
# cluster (WSL) :   bash .claude-launcher/k8s-acces-pod.sh
#
# Crée dans le namespace claude-ng2 un compte de service « ng2-tests » limité à
# ce namespace (StatefulSets, pods, exec, events, PVC ; lecture du namespace),
# puis écrit un kubeconfig (contexte « k8s », comme dans backends.yaml) sur le
# stockage partagé, lu par le bootstrap du projet (.claude-launcher/env.yaml).
#
# Révoquer :  kubectl --context k8s -n claude-ng2 delete secret ng2-tests-token
set -euo pipefail
CTX=${CTX:-k8s}
NS=claude-ng2
OUT=/share/MFS/ROOT/CLAUDE_PROJETS/.kube/ng2-tests.kubeconfig
K="kubectl --context $CTX"

$K apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata: {name: ng2-tests, namespace: $NS}
---
apiVersion: v1
kind: Secret
metadata:
  name: ng2-tests-token
  namespace: $NS
  annotations: {kubernetes.io/service-account.name: ng2-tests}
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: ng2-tests, namespace: $NS}
rules:
  - apiGroups: [apps]
    resources: [statefulsets, statefulsets/scale]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch, delete]
  - apiGroups: [""]
    resources: [pods/exec]
    verbs: [create, get]
  - apiGroups: [""]
    resources: [events, persistentvolumeclaims]
    verbs: [get, list]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: ng2-tests, namespace: $NS}
subjects: [{kind: ServiceAccount, name: ng2-tests, namespace: $NS}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: ng2-tests}
---
# « kubectl get namespace claude-ng2 » (diagnostic de NG2) : ce namespace seulement
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: ng2-tests-namespace}
rules:
  - apiGroups: [""]
    resources: [namespaces]
    resourceNames: [$NS]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: ng2-tests-namespace}
subjects: [{kind: ServiceAccount, name: ng2-tests, namespace: $NS}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: ng2-tests-namespace}
EOF

echo "attente du jeton…"
for i in $(seq 1 30); do
  TOKEN=$($K -n $NS get secret ng2-tests-token -o jsonpath='{.data.token}' 2>/dev/null | base64 -d || true)
  [ -n "$TOKEN" ] && break; sleep 1
done
[ -n "$TOKEN" ] || { echo "⚠ jeton non créé"; exit 1; }
CA=$($K -n $NS get secret ng2-tests-token -o jsonpath='{.data.ca\.crt}')
VER=$($K version -o json 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin)["serverVersion"]["gitVersion"].split("+")[0])' || echo "")

mkdir -p "$(dirname "$OUT")"; chmod 700 "$(dirname "$OUT")"
umask 077
cat > "$OUT" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: k8s
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority-data: $CA
users:
  - name: ng2-tests
    user:
      token: $TOKEN
contexts:
  - name: k8s
    context: {cluster: k8s, user: ng2-tests, namespace: $NS}
current-context: k8s
EOF
[ -n "$VER" ] && echo "$VER" > "$(dirname "$OUT")/kubectl-version"
ls -l "$OUT"
echo "✓ kubeconfig écrit (contexte k8s, namespace $NS, serveur interne au cluster ; version $VER)"
echo "  Rouvrez le projet Claude-Launcher dans NG2 (détacher puis Entrée) : le bootstrap l'installe."
