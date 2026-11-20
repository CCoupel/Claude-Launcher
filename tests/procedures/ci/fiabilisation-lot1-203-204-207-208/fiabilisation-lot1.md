# Procedure de Test — Fiabilisation LOT-1 : CI et tests fiables (#203 #204 #207 #208)

**Version** : 6.5.0
**Date** : 2026-11-19
**Testeur** : QA

Reference : `.claude/workflow/plan-v6.5.0.md` (LOT-1). Aucun contrat. Tous les scenarios sont `AUTO` (aucun jugement humain).

## Prerequis

- [ ] Environnement : conteneur de developpement, branche `milestone/v6.5.0` au SHA du lot, poste au repos
- [ ] Donnees : aucune ; `go`, `gcc` (`-race`), `node` disponibles ; aucun vrai projet ni vrai `claude`
- [ ] Acces : aucun

## Scenarios

### Scenario 1 — #203 : SSE gelee, 50 passages sous -race

**Validation** : AUTO — **Issues** : #203

**Objectif** : le test n'attend plus un `Sleep(200ms)` mais la condition (2 gestionnaires comptes) ; 0 echec sur 50 passages.

```bash
cd ng2 && go test -race -count=50 -run TestSSEFrozenConnectionHasWriteDeadlineAndCountsInCap ./ui/desktop/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer la commande | `ok` puis `OK`, 0 echec | | |
| 2 | `grep -n 'Sleep(200' ng2/ui/desktop/websec_sse_test.go` | Aucun `Sleep(200ms)` dans ce test ; l'assertion 503 est conservee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — #208 : jeton obsolete retire, 20 passages sous charge

**Validation** : AUTO — **Issues** : #208

```bash
cd ng2 && time go test -count=20 -p 8 -run TestWebAccessNoneRemovesStaleTokenFile ./ui/desktop/ && echo OK
# duree par passage (local) :
go test -count=1 -v -run 'TestWebAccessNoneRemovesStaleTokenFile$' ./ui/desktop/ | grep -E '^--- (PASS|FAIL)'
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | 20 passages `-p 8` | 0 echec | | |
| 2 | Duree d'un passage seul (`--- PASS: … (N.NNs)`) | < 10 s | | |
| 3 | Lire le handoff dev-backend (`_work/handoff/dev-backend-*.md`) | La cause de la lenteur est nommee ; les attentes de 45 s ne servent que de garde-fou | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — #207 : assertion « init lent » de sse_flow.js

**Validation** : AUTO — **Issues** : #207

```bash
node ng2/ui/desktop/testdata/sse_flow.js ng2/ui/desktop/frontend/app.js   # attendu : OK
cd ng2 && go test -count=1 -run 'SSEFlow|Flow' ./ui/desktop/                # le test Go qui l'execute
```

Mutations a jouer sur une COPIE de `app.js` (`_work/tmp/`, jamais l'original) :

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Script sur le `app.js` du lot | `OK`, code 0 | | |
| 2 | Copie dont le backoff est 8000, 16000, 30000, 30000 (suite croissante plafonnee) | OK, quel que soit le nombre d'essais | | |
| 3 | Copie dont la suite n'est pas croissante (ex. 8000, 4000, 16000) | `KO …`, code 1 | | |
| 4 | Copie dont le plafond est depasse (ex. 8000, 16000, 45000) | `KO …`, code 1 | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — #204 : version du chart verifiee par scripts/ci.sh

**Validation** : AUTO — **Issues** : #204

```bash
tests/integration/ci/chart-version-204/chart-version-guard.sh      # attendu : GARDE chart-version : OK
sed -nE 's/^(version|appVersion): *"?([^"]+)"?/\1=\2/p' deploy/helm/claude-launcher/Chart.yaml   # 6.5.0 / v6.5.0
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Garde d'integration | `GARDE chart-version : OK` : chart juste → pas de refus ; chart faux → KO avec le message de `release.yml` ; hors milestone → ignore avec message | | |
| 2 | Chart.yaml du depot | `version=6.5.0`, `appVersion=v6.5.0` | | |
| 3 | `scripts/ci.sh` complet sur la branche (~1 min) | `CI OK` | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenarios 1 a 4 PASS
- [ ] Aucun test existant modifie hors ceux du lot (`git diff --stat` ne touche que `websec_sse_test.go`, `web184_test.go`, `sse_flow.js`, `ci.sh`, `Chart.yaml`)
- [ ] CI verte (Linux et Windows)

## Notes QA

Les boucles sont longues : lot tagge `slow`. Aucune validation humaine requise pour ce lot.
