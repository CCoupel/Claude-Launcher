# Procedure de Test — Chart du launcher en QUALIF : exposition et liste d'adresses (#170, #173)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : QA (avec l'administrateur du cluster pour les prerequis)

Reference : `contracts/helm-values.md`, `deploy/helm/values-qualif.yaml`, audit securite du chart (`_work/reports/security-lot4.2-*.md`, H1, H2, H3). Tests de rendu sans cluster : `ng2/chart_helm_test.go` (`go test -run TestChart .`).

## Prerequis

- [ ] Environnement : QUALIF — **namespace `claude-ng2-qualif`** (separe de la PROD `claude-ng2`, decision H1), label Pod Security `pod-security.kubernetes.io/enforce: baseline` sur les deux namespaces (verifier que les pods de projets le respectent)
- [ ] Donnees : release `claude-launcher-qualif` installee avec `values-qualif.yaml` (ClusterIssuer interne **renseigne** : la valeur `a-renseigner` est refusee), image candidate `image.tag=vX.Y.Z.a` (tag sans digest : risque accepte H2, reseau interne de confiance)
- [ ] Acces : un poste dans `192.168.1.0/24`, un poste **hors** de cette plage (ou une IP source simulee), un pod du cluster, `kubectl -n claude-ng2-qualif`
- [ ] Racine reelle de QUALIF : projets factices dedies seulement, sauvegarde avant, controle apres (decision C1)

## Scenarios

### Scenario 1 — Rendu et installation

**Validation** : AUTO — **Issues** : #170

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `helm lint --strict` + `helm template … -n claude-ng2-qualif -f values-qualif.yaml --set ingress.tls.clusterIssuer=<reel>` | Verts ; tous les objets dans `claude-ng2-qualif` | | |
| 2 | `helm upgrade --install … -n claude-ng2-qualif` | Pod `claude-launcher-qualif-0` `Ready` (sonde `/readyz`) ; un seul pod | | |
| 3 | `kubectl -n claude-ng2-qualif get role,rolebinding,svc,ingress,certificate` | Role limite au namespace (aucun ClusterRole), Service `ClusterIP`, Ingress TLS, Certificate `Ready` | | |

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
| 1 | Depuis un pod, `curl http://claude-launcher-qualif.claude-ng2-qualif:8080/` (Service interne, hors Ingress) | Reponse HTTP sans cookie de session exploitable (cookie `Secure`, `__Host-`) ; **risque accepte** : pas de NetworkPolicy en v6.3.0 (#172 en v6.4.0) | | |
| 2 | `kubectl auth can-i --list --as=system:serviceaccount:claude-ng2-qualif:claude-launcher-qualif -n claude-ng2-qualif` | Seulement les droits du Role ; **rien** dans `claude-ng2` (PROD) ni au niveau du cluster | | |
| 3 | `kubectl -n claude-ng2-qualif get pvc` | PVC RWX vers le meme stockage que `claude-projets` ; le chart n'en cree aucun | | |
| 4 | Pods de projets de QUALIF sous le label PSA `baseline` | Demarrent sans refus de politique (sinon : le signaler) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Interface et desinstallation

**Validation** : HUMAIN — **Issues** : #170, #171

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis le poste de `192.168.1.0/24` : jeton lu avec `kubectl exec … cat <CL_CACHE_DIR>/desktop-web-token`, ouvrir `https://launcher-qualif.coupel.lan/?t=<jeton>` | Redirection vers `/` (jeton retire de l'URL), interface affichee, terminaux et evenements fonctionnent | | |
| 2 | `helm uninstall … -n claude-ng2-qualif` | Objets du chart retires ; PVC, StatefulSets des projets et fichiers de la racine **intacts** | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Hors de la plage : toujours `403`, y compris depuis un pod et avec un `X-Forwarded-For` forge
- [ ] Namespace QUALIF separe, droits du ServiceAccount limites a ce namespace
- [ ] Desinstallation sans effet sur le PVC, les projets et la racine

## Notes QA

Risques acceptes a rappeler dans le guide : image par tag sans digest (H2), pas de NetworkPolicy, jeton dans le dossier d'etat. Desinstaller la QUALIF des la validation (racine reelle).
