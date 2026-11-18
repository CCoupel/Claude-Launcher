# Procedure de Test — Kubeconfig par backend k8s et autres namespaces (#193)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/backends-remote.md` (kubeconfig, règle provisoire), `contracts/helm-values.md` — tests auto : `ng2/core/kubeconfig193_test.go`, `ng2/ui/desktop/kubeconfig193_test.go`, `ng2/chart_kubeconfig193_test.go`
**Environnement** : QUALIF, pod `claude-launcher-0` (ns `claude-qualif`), interface par l'ingress interne ; cible : namespace jetable `claude-qualif-extra` créé par infra

## Prerequis

- [ ] Image et chart QUALIF 6.4.0.x déployés
- [ ] Namespace jetable `claude-qualif-extra` + ServiceAccount **à droits limités** (Role identique à celui du launcher) créés par infra ; kubeconfig de ce compte (`PRIVATE/QUALIF/`, jamais dans le dépôt)
- [ ] Un PVC ReadWriteMany pour les volumes du backend, dans `claude-qualif-extra`

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./core ./ui/desktop -run 193` et `go test . -run 193` dans `ng2` | tous PASS |
| 2 | `helm template` avec `rbac.extraNamespaces=[claude-qualif-extra]` | un Role et un RoleBinding dans chaque namespace, aucun ClusterRole |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Droits du launcher dans le namespace supplémentaire

**Validation** : HUMAIN | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Déployer avec `rbac.extraNamespaces: [claude-qualif-extra]` | déploiement OK |
| 2 | `kubectl -n claude-qualif-extra get role,rolebinding` | Role et RoleBinding vers le ServiceAccount du pod |
| 3 | `kubectl auth can-i create statefulsets -n claude-qualif-extra --as=system:serviceaccount:claude-qualif:<sa>` | `yes` ; dans un namespace non listé : `no` |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Déposer le kubeconfig par l'interface (ingress interne)

**Validation** : HUMAIN | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Réglages › Backends › ajouter un backend k8s `autre`, namespace `claude-qualif-extra`, kubeconfig du coffre | formulaire accepté |
| 2 | Coller un texte quelconque comme kubeconfig | refus motivé, rien d'enregistré |
| 3 | Coller le vrai kubeconfig du compte limité | enregistré ; **contextes listés** ; contenu jamais affiché |
| 4 | Recharger | seuls les noms de contextes et « modifié le … » apparaissent, jamais jeton ni certificat |
| 5 | Observer les réponses (onglet Réseau) et `kubectl logs` du pod | aucun jeton ni certificat ; une ligne de journal (backend, type) |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — « Tester » et pilotage de la cible

**Validation** : HUMAIN | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | « Tester » le backend `autre` | « kubeconfig lisible » OK, puis `auth can-i` de chaque droit avec ce kubeconfig (namespace `claude-qualif-extra`) |
| 2 | Ouvrir un projet sur `autre` | StatefulSet créé dans `claude-qualif-extra`, terminal ouvert |
| 3 | `kubectl -n claude-qualif get sts` | rien créé dans le namespace du launcher |
| 4 | Backend avec `context:` d'un kubeconfig à deux contextes | le contexte choisi est utilisé, même dans le pod |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Fichier illisible : erreur sans repli

**Validation** : HUMAIN | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Dans le pod, supprimer le fichier du coffre `<coffre>/autre/kubeconfig` | — |
| 2 | « Tester » `autre`, puis ouvrir un projet | erreur « kubeconfig illisible sur ce poste » ; **aucun** appel avec le ServiceAccount du pod (pas de StatefulSet créé dans `claude-qualif`) |
| 3 | Redéposer le kubeconfig | le backend refonctionne |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 6 — Kubeconfig monté par le chart (alternative au coffre)

**Validation** : HUMAIN | **Issues** : #193

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Créer un Secret `kc-autre` (clé `config`) et déployer avec `backends.kubeconfigs: [{name: autre, secretName: kc-autre, key: config}]` | fichier en `/var/run/claude-launcher/kube/autre/config`, lecture seule, lisible par l'uid 1000 |
| 2 | Backend `kubeconfig: /var/run/claude-launcher/kube/autre/config` ; « Tester » | « kubeconfig lisible » OK |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Le launcher pilote une cible d'un autre namespace par le kubeconfig déposé
- [ ] Fichier illisible : erreur, jamais de repli sur le ServiceAccount
- [ ] Contenu du kubeconfig jamais renvoyé ni journalisé
- [ ] `PRIVATE/QUALIF/` : le kubeconfig de test n'est jamais commité

## Notes QA

Règle provisoire v6.4.0 : tout poste du LAN admis peut déposer un kubeconfig (R1 en v7.0.0). Nettoyer `claude-qualif-extra` en fin de recette.
