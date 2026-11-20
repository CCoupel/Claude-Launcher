# Procedure de Test — Rafraichissement des cartes jamais perdu (#210)

**Version** : 6.5.0
**Date** : 2026-11-20
**Testeur** : QA

Reference : `.claude/workflow/plan-v6.5.0-lot5.md`. Aucun scenario HUMAIN.

## Prerequis

- [ ] Environnement : conteneur de dev, branche `milestone/v6.5.0` ; `go`, `gcc` (`-race`)

## Scenarios

### Scenario 1 — Signal pendant une relecture lente, 50 passages sous -race

**Validation** : AUTO — **Issues** : #210

```bash
cd ng2 && go test -race -count=50 -run 'Cartes.*210' ./ui/desktop/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | 0 echec : 1 puis 5 signaux = exactement 2 relectures, derniere carte a jour ; jamais 2 relectures simultanees ; selection changee → carte de l'ancien projet jamais emise ; `quit` ne bloque pas et aucune relecture apres ; `state` (#130) emis pendant une relecture bloquee | | |
| 2 | `go test -race -count=1 ./ui/desktop/` | suite complete verte (aucun etat global `cardsMu` residuel) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenario 1 PASS ; CI verte (Linux et Windows)
