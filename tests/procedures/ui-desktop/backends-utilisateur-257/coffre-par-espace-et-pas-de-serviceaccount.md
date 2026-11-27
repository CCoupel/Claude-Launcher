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

## Base du plan de test HUMAIN de LOT-3.1

Voir `tests/procedures/ui-desktop/espace-utilisateur-255/` section « Reporte en LOT-3.1 » (parcours admin complet, refus par `CCoupel`) ;
a completer au GATE LOT-3.1.

## Notes QA

Les tests LOT-2 `TestUserSpace255*` (core) et `TestEditor255EnregistrementDunBackendValide` (ui/desktop), qui declaraient un k8s SANS kubeconfig
comme valide, devront etre mis a jour avec le code (CHANGELOG : kubeconfig obligatoire pour un utilisateur).
