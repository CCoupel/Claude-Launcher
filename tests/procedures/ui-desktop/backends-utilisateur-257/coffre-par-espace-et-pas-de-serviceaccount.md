# Procedure de Test — Coffre par espace et pas de repli ServiceAccount (#257, S15 / S16)

**Version** : 7.1.0
**Date** : 2026-11-27
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL (faux kubectl, coffre et config temporaires, canaris `CANARY-…`, aucun secret reel)
- [ ] Tests ecrits AVANT le code (TDD) : rouges sur le code de LOT-2, verts avec le code du LOT-3.1

## Scenarios

### Scenario 1 — S16 : un coffre par espace d'utilisateur

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run TestS16` | PASS : deux utilisateurs, meme nom de backend → deux coffres `users/<uid>/vault/<backend>/` ; un backend nomme comme celui de l'admin ne lit jamais le coffre commun ; noms invalides refuses ; coffre commun intact | | |
| 2 | `cd ng2 && go test ./ui/desktop -run TestS16` | PASS : `backendSecretSet` d'un utilisateur ecrit dans son coffre seulement ; noms traversants (`../`, `a/b`, majuscules, 64 car.) refuses sans ecriture ; aucun canari dans les reponses ni le journal | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — S15 : jamais le ServiceAccount du launcher

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run TestS15` | PASS : declaration k8s sans `kubeconfig: true` refusee (message nommant le kubeconfig) ; `kubectl` toujours lance avec `--kubeconfig` du coffre de l'utilisateur, jamais `~/.kube/config`, `KUBECONFIG` herite ni namespace du pod ; kubeconfig absent → kubectl non lance ; admin inchange | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — S1–S9, S13, S14, D1 : bornage des hotes d'un utilisateur

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run Hosts257` | PASS : >= 25 formes interdites (loopback, LAN, link-local/metadonnees, CGNAT, IPv4-mappee, NAT64, 6to4, decimal/octal/hexa, noms internes, userinfo, port, controle, longueur) refusees a l'enregistrement et AVANT l'appel avec le meme message « adresse non autorisee pour un utilisateur » ; hotes publics acceptes ; admin sans contrainte ; ssh refuse en mode serveur, inchange en mode poste | | |
| 2 | `cd ng2 && go test -tags s257seams ./core -run D1257` | PASS (une fois les points d'injection `userHostResolver` / `userHostCacheTTL` exposes) : public puis prive → second appel refuse, cache, une IP interdite suffit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — S10, S17–S21, S12 : kubeconfig durci et certificats TLS

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run 'Kubeconfig257|TLS257'` | PASS : kubeconfig > 64 Kio, alias/ancres/doublons, profondeur, exec, auth-provider, `*-file`, proxy-url (toutes casses) refuses ; `server:` de tous les clusters borne ; `insecure-skip-tls-verify` accepte avec avertissement ; cle ≠ certificat, cle chiffree, PEM invalides refuses ; aucun contenu reaffiche | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — S10, S29, S32 : environnement et certificats temporaires

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run Env257` | PASS : variables HTTP(S)_PROXY, ALL_PROXY, NO_PROXY, DOCKER_*, CONTAINER_HOST, KUBECONFIG heritees non transmises ; certificats temporaires 0600 supprimes apres l'appel, noms distincts ; aucun shell | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — S8, S25, S26, S31, D5 : fuites, limites, audit, backendTest

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./ui/desktop -run 'Fuites257|Limites257|Audit257|BackendTest257|SSH257'` | PASS : canari absent des reponses, audit et journaux ; erreurs YAML sans citation ; 11e backend refuse ; 7e test/min et tests simultanes → 429 ; audit `backend.call` sans secret ; avertissements (root sur l'hote, TLS non verifie, compte trop puissant + Role), volume/PVC absent refuses ; ssh refuse | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Chart : limites

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test . -run TestChartUsers257` (helm requis) | PASS : `users.maxBackends` 10 (1–100) et `users.backendTestsPerMinute` 6 (1–60) rendus ; hors bornes refuse | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Cycle 2 : hotes publics reels, creation atomique, editeur apres refus

**Validation** : AUTO — **Issues** : #257

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && go test ./core -run Public258` ; `go test -tags net ./core -run Net258` (vrai DNS, non bloquant en CI) | PASS : adresses publiques reelles (yahoo.fr, github.com, example.com ; v4 ET v6, AAAA) acceptees ; plages limites (juste en dessous / au-dessus de chaque plage interdite) ; DNS en echec → refus non mis en cache ; resolution lente acceptee, bloquee bornee ; validation en parallele | | |
| 2 | `cd ng2 && go test ./ui/desktop -run 'Create258|Secret258|Editeur258|Diagnostic258'` | PASS : `userBackendAdd` k8s/docker (tout ou rien, coffre 0700/0600, `kubeconfig: true` pose par le serveur), refus nommes `{ok:false, field, code, msg}`, nom pris, 11e backend, remplacement/suppression de secret, debit, editeur utilisable apres un refus (fichier et mtime intacts), raison interne du refus dans l'audit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Base du plan de test HUMAIN de LOT-3.1

Voir `tests/procedures/ui-desktop/espace-utilisateur-255/` section « Reporte en LOT-3.1 » (parcours admin complet, refus par `CCoupel`) ;
a completer au GATE LOT-3.1.

## Notes QA

Les tests LOT-2 `TestUserSpace255*` (core) et `TestEditor255EnregistrementDunBackendValide` (ui/desktop), qui declaraient un k8s SANS kubeconfig
comme valide, devront etre mis a jour avec le code (CHANGELOG : kubeconfig obligatoire pour un utilisateur).
