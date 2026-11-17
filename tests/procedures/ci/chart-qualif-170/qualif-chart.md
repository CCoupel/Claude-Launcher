# Procedure de Test — Chart du launcher en QUALIF : exposition et liste d'adresses (#170, #173)

**Version** : 6.3.0
**Date** : 2026-11-17
**Testeur** : QA (avec l'administrateur du cluster pour les prerequis)

Reference : `contracts/helm-values.md`, `deploy/helm/values-qualif.yaml`, audit securite du chart (`_work/reports/security-lot4.2-*.md`, H1, H2, H3). Tests de rendu sans cluster : `ng2/chart_helm_test.go` (`go test -run TestChart .`).

## Prerequis

- [ ] Environnement : QUALIF — **namespace `claude-qualif`** (separe de la PROD `claude-ng2`, decision du 2026-11-17), label Pod Security `pod-security.kubernetes.io/enforce: baseline` sur **les deux** namespaces (`kubectl get ns claude-ng2 claude-qualif --show-labels`) ; verifier que les pods de projets le respectent
- [ ] **Racine de QUALIF separee** : `/share/MFS/ROOT/CLAUDE-QUALIF` (dossier cree sur MooseFS, **different** de la racine de PROD `/share/MFS/ROOT/CLAUDE_PROJETS`), PV/PVC RWX **propre a la QUALIF** `claude-projects-qualif` dans `claude-qualif` ; le PVC `claude-projets` de la PROD n'est jamais monte par la QUALIF
- [ ] **Squelette de la racine de QUALIF** : `<racine QUALIF>/.config/claude-launcher-ng2/backends.yaml` avec **les memes types de backends que la PROD** (**podman, docker `srv1-docker` — dont `shared` pointe la racine de QUALIF — et k8s** ; pas de backend ssh), le backend `k8s` pointant `namespace: claude-qualif` et `volumes` vers le PVC de QUALIF (la ligne `context:` d'un backend k8s est ignoree en cluster : ServiceAccount du pod) ; `config.conf` cree au demarrage du pod ; **projets factices** dans la categorie `<racine QUALIF>/PROJETS/QUALIF/` (`factice-1`, `factice-2`) (depot git vide, faux `claude`) — jamais un vrai projet
- [ ] Donnees : release `claude-launcher-qualif` installee avec `values-qualif.yaml` (**ClusterIssuer interne reel renseigne** : la valeur `a-renseigner` est refusee par le chart), image candidate `image.tag=vX.Y.Z.a` (tag sans digest : risque accepte H2, reseau interne de confiance)
- [ ] Acces : un poste dans `192.168.1.0/24`, un poste **hors** de cette plage (ou une IP source simulee), un pod du cluster, `kubectl -n claude-qualif`
- [ ] **La PROD n'est pas touchee** : `config.conf`, `backends.yaml`, `projects.json`, reglages et jeton de la racine reelle restent inchanges pendant toute la QUALIF (sauvegarde horodatee avant, comparaison apres)

## Scenarios

### Scenario 1 — Rendu et installation

**Validation** : AUTO — **Issues** : #170

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm lint --strict` + `helm template … -n claude-qualif -f values-qualif.yaml --set ingress.tls.clusterIssuer=<reel>` | Verts ; tous les objets dans `claude-qualif` | | |
| 2 | `helm upgrade --install … -n claude-qualif` | Pod `claude-launcher-qualif-0` `Ready` (sonde `/readyz`) ; un seul pod | | |
| 3 | `kubectl -n claude-qualif get role,rolebinding,svc,ingress,certificate` | Role limite au namespace (aucun ClusterRole), Service `ClusterIP`, Ingress TLS, Certificate `Ready` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Test negatif de la liste d'adresses (H3)

**Validation** : AUTO — **Issues** : #170

**Objectif** : l'Ingress n'admet que `192.168.1.0/24` ; l'adresse vue par Traefik est bien celle du client (pas celle d'un noeud ni d'un proxy), et un pod du cluster ne contourne pas la liste.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis un poste de `192.168.1.0/24` : `curl -sk -o /dev/null -w '%{http_code}' https://launcher-qualif.coupel.lan/healthz` | `200` | | |
| 2 | Depuis une IP **hors** de `192.168.1.0/24` (autre sous-reseau / VPN) : meme requete | **`403`** (refus par le Middleware `ipAllowList`), jamais `200` | | |
| 3 | Depuis un **pod** du cluster, en passant par l'Ingress (`kubectl run -it --rm curl --image=<curl> -- curl -sk https://launcher-qualif.coupel.lan/healthz`) | **`403`** : l'IP source vue par Traefik est celle du pod (hors liste), pas une adresse de noeud admise | | |
| 4 | `kubectl -n <ns-traefik> get svc <service traefik> -o jsonpath='{.spec.externalTrafficPolicy}'` | `Local` (l'IP source du client est conservee) ; si `Cluster` : le test 2 est a refaire en lisant les journaux Traefik (l'IP vue est celle du noeud) et a signaler | | |
| 5 | Journaux Traefik pendant les etapes 2 et 3 | L'IP source journalisee est celle du client de test, pas celle d'un noeud | | |
| 6 | `ipStrategy` du Middleware (`kubectl get middleware -o yaml`) | Explicite si posee (profondeur / exclusions documentees) ; aucun en-tete `X-Forwarded-For` falsifiable n'elargit la liste (requete avec `X-Forwarded-For: 192.168.1.10` depuis une IP hors liste → `403`) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Acces direct, namespaces, droits

**Validation** : AUTO — **Issues** : #170

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis un pod, `curl http://claude-launcher-qualif.claude-qualif:8080/` (Service interne, hors Ingress) | Reponse HTTP sans cookie de session exploitable (cookie `Secure`, `__Host-`) ; **risque accepte** : pas de NetworkPolicy en v6.3.0 (#172 en v6.4.0) | | |
| 2 | `kubectl auth can-i --list --as=system:serviceaccount:claude-qualif:claude-launcher-qualif -n claude-qualif` | Seulement les droits du Role ; **rien** dans `claude-ng2` (PROD) ni au niveau du cluster | | |
| 3 | `kubectl -n claude-qualif get pvc` ; `kubectl -n claude-qualif get statefulset claude-launcher-qualif -o jsonpath='{.spec.template.spec.volumes}'` | Seul le PVC **de QUALIF** `claude-projects-qualif` est monte (jamais `claude-projets`) ; il est cree par le chart (RWX, lie a `claude-qualif-pv`, annotation `helm.sh/resource-policy: keep`) | | |
| 4 | Pods de projets de QUALIF sous le label PSA `baseline` | Demarrent sans refus de politique (sinon : le signaler) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Interface et desinstallation

**Validation** : HUMAIN — **Issues** : #170, #171

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis le poste de `192.168.1.0/24` : jeton lu avec `kubectl exec … cat <CL_CACHE_DIR>/desktop-web-token`, ouvrir `https://launcher-qualif.coupel.lan/?t=<jeton>` | Redirection vers `/` (jeton retire de l'URL), interface affichee, terminaux et evenements fonctionnent | | |
| 2 | `helm uninstall … -n claude-qualif` | Objets du chart retires ; PVC, StatefulSets des projets et fichiers de la racine **intacts** | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Isolation QUALIF / PROD : la QUALIF ne voit ni ne touche la PROD

**Validation** : AUTO — **Issues** : #170, #171

**Objectif** : QUALIF et PROD ont des racines et des PVC distincts ; le pod de QUALIF ne peut ni lire le jeton, ni modifier les fichiers du launcher de PROD. (Remplace l'ancien scenario « regenerer le jeton PROD » : sans objet des que les racines sont distinctes.)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `kubectl -n claude-qualif exec claude-launcher-qualif-0 -- ls /share/MFS/ROOT` | Seule `CLAUDE-QUALIF` est visible (ou le dossier parent n'est pas monte) ; `CLAUDE_PROJETS` absent | | |
| 2 | `kubectl -n claude-qualif exec … -- cat /share/MFS/ROOT/CLAUDE_PROJETS/.cache/claude-launcher-ng2/cluster/desktop-web-token` | Echec : fichier introuvable (le jeton PROD n'est pas lisible depuis la QUALIF) | | |
| 3 | Variables du pod QUALIF : `kubectl exec … -- env \| grep '^CL_'` | `CL_ROOT_BASE=/share/MFS/ROOT/CLAUDE-QUALIF`, `CL_HUB_NAME=cluster-qualif` (jamais `cluster`), `CL_CACHE_DIR` sous la racine de QUALIF | | |
| 4 | Dans l'interface de QUALIF : Reglages > Backends > Tester le backend `k8s` | Etapes vertes dans le namespace `claude-qualif` ; ligne d'information « contexte « k8s » ignore : ServiceAccount du pod » si le fichier le declare | | |
| 5 | Ouvrir un projet factice depuis la QUALIF (StatefulSet dans `claude-qualif`) | Le pod du projet est cree dans `claude-qualif`, jamais dans `claude-ng2` ; `kubectl -n claude-ng2 get sts` inchange | | |
| 6 | En fin de QUALIF : comparer les fichiers de la racine de PROD a la sauvegarde d'avant (`config.conf`, `backends.yaml`, `projects.json`, reglages, `desktop-web-token`) | **Identiques** (aucune ecriture de la QUALIF) ; le launcher de PROD n'a pas ete interrompu | | |
| 7 | Verifier que les deux launchers coexistent : noms de hub distincts (`cluster` et `cluster-qualif`), pas de refus « launcher deja actif » | Deux launchers actifs, un par racine | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Hors de la plage : toujours `403`, y compris depuis un pod et avec un `X-Forwarded-For` forge
- [ ] Namespace QUALIF separe, droits du ServiceAccount limites a ce namespace
- [ ] Desinstallation sans effet sur le PVC, les projets et la racine
- [ ] Isolation : racine, PVC, namespace et nom de hub de QUALIF distincts de ceux de la PROD ; aucun fichier de la PROD modifie ni jeton PROD lisible depuis la QUALIF

## Notes QA

Risques acceptes a rappeler dans le guide : image par tag sans digest (H2), pas de NetworkPolicy, jeton dans le dossier d'etat. Desinstaller la QUALIF apres validation si elle n'est plus utile (la racine de QUALIF est separee : aucune urgence pour la PROD).
