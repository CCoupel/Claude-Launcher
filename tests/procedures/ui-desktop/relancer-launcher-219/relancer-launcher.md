# Procedure de Test — Relancer le launcher depuis l'interface (#219)

**Version** : 6.6.0
**Date** : 2026-11-20
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` (`launcherInfo`, `relaunch`). Equivalents automatises : `ng2/core/relaunch219_test.go`, `ng2/ui/tmux/relaunch219_test.go`, `ng2/ui/desktop/relaunch219_test.go`.

## Prerequis

- [ ] Une version N installee et lancee, puis une version N+1 posee sur le binaire (mise a jour ou copie) sans relancer
- [ ] Projets factices ouverts (jamais de vrais projets)

## Scenarios

### Scenario 1 — Exe Windows : bandeau, confirmation, relance

**Validation** : HUMAIN — **Issues** : #219

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Remplacer `claude-launcher-ng2.exe` par la version N+1 pendant que la fenetre tourne (ou laisser la mise a jour le faire) | Bandeau « binaire change → Relancer » | | |
| 2 | Cliquer « Relancer » | Dialogue de confirmation (relance / garder) | | |
| 3 | Choisir « Garder » | Rien ne bouge, bandeau conserve | | |
| 4 | Cliquer « Relancer » puis confirmer | Fenetre fermee puis rouverte, nouvelle version affichee, un seul processus `claude-launcher-ng2.exe` (Gestionnaire des taches), port libre | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Hub reel : vignettes et layout-watch sans Echap / `l`

**Validation** : HUMAIN — **Issues** : #219 — **Tags** : critical

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub N+1 deploye sur le poste, session `claude-hub` avec 2 projets ouverts (panes projets notes : `tmux -L <socket> list-panes -a -F '#{pane_pid}'`) | Bandeau d'alerte dans la coque | | |
| 2 | Declencher la relance depuis la coque (touche selon maquette) et confirmer | Nav, cartes et vignettes redemarrent | | |
| 3 | `pgrep -af 'claude-launcher-ng2 (mirror|layout-watch)'` | Aucun processus de l'ancien binaire ; vignettes et surveillants de la nouvelle version | | |
| 4 | Comparer les pids des panes projets | Identiques : conversations Claude non interrompues | | |
| 5 | Redimensionner / masquer un teammate | Mise en page suivie sans Echap ni `l` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — QUALIF cluster : pod du launcher redemarre

**Validation** : HUMAIN — **Issues** : #219

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Interface web du launcher du cluster QUALIF, bandeau « Relancer » apres mise a jour de l'image | Bandeau visible | | |
| 2 | Relancer et confirmer | Interface deconnectee puis reconnectee ; `kubectl get pods` : pod du launcher redemarre (RESTARTS +1) | | |
| 3 | `kubectl get pods` : pods des projets | Inchanges (meme age, pas de redemarrage) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Aucune session ni conversation perdue sur le hub
- [ ] Aucun ancien processus apres relance

## Notes QA

Les verifications de processus (anciens arretes, autres serveurs intacts, refus sans confirmation) sont automatisees.
