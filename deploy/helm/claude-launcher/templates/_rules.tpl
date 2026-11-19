{{/* droits du launcher dans un namespace (Role de la release et des rbac.extraNamespaces) */}}
{{- define "cl.rules" -}}
  # apply (get/create/patch), liste des projets, delete --wait (watch)
  - apiGroups: [apps]
    resources: [statefulsets]
    verbs: [get, list, watch, create, patch, delete]
  # fermeture d'un projet : kubectl scale --replicas=0 (core/close.go)
  - apiGroups: [apps]
    resources: [statefulsets/scale]
    verbs: [get, patch, update]
  # état, liste, wait --for=condition=Ready (watch), suppression
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch, delete]
  # kubectl exec (terminaux) : create seul
  - apiGroups: [""]
    resources: [pods/exec]
    verbs: [create]
  # diagnostic d'échec de démarrage (get events --field-selector)
  - apiGroups: [""]
    resources: [events]
    verbs: [list]
  # diagnostic : PVC lié et ReadWriteMany
  - apiGroups: [""]
    resources: [persistentvolumeclaims]
    verbs: [get]
{{- end -}}
