# Procedure de Test — Organisations de la fenetre d'un projet, coque Linux (#153)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — hub Linux reel (navigateur et panes tmux), jamais la session `claude-hub` de production
- [ ] Donnees : projets factices (A et B), faux `claude`, team de 6 teammates dont un `cdp`
- [ ] Equivalents automatises : `TestRelayoutLeftZeroToTenTeammates`, `TestRelayoutLeftLeaderWidth`, `TestRelayoutTopUnchangedByDefault`, `TestRelayoutEffectiveLayoutPerProject`, `TestRelayoutLeftOnlyVisiblePanes`, `TestRelayoutLeftWithPaneTitleLine`, `TestLayoutWatchFollowsConfigChange` (`ng2/ui/tmux/layout153_test.go`)

## Scenarios

### Scenario 1 — « Leader a gauche » global, Ctrl+B R et prise en compte automatique

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `~/.config/claude-launcher-ng2/config.conf` : `LAYOUT_MODE=left` ; projet A ouvert avec team | Au tour suivant du surveillant (≈ 1 s a quelques secondes) le projet se met en page : leader a gauche (≈ 46 %), teammates a droite | | |
| 2 | Ctrl+B R dans la fenetre | Disposition immediate, identique | | |
| 3 | 1 a 4 teammates | Une colonne a droite | | |
| 4 | 5 a 10 teammates | Deux colonnes egales ; la premiere a ⌈n/2⌉ teammates | | |
| 5 | Avec un agent `cdp` | Empile sous le leader dans la colonne de gauche | | |
| 6 | `LAYOUT_LEFT_LEADER_PCT=30` puis 70 | La largeur du leader suit ; 10 ou 95 : probleme de validation dans les reglages | | |
| 7 | Remettre `LAYOUT_MODE=top` (ou retirer la ligne) | Retour a la disposition v6.1.0, sans relancer | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Exception par projet (PROJECT_LAYOUTS)

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Global `top` ; `declare -A PROJECT_LAYOUTS=(["A"]="left")` | A en leader a gauche, B en haut | | |
| 2 | Global `left` ; entree `["A"]="top"` | A en haut, B a gauche | | |
| 3 | Retirer l'entree de A | A suit de nouveau le global | | |
| 4 | Valeur inconnue (`["A"]="diagonal"`) | Retombe sur le global ; probleme de validation | | |
| 5 | Verifier le mode de reglage dans l'interface native de la meme machine | Les deux interfaces affichent la meme organisation effective | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Panes caches, lignes de titre

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | « Leader a gauche », 6 teammates dont 4 inactifs (TEAM_HIDE_IDLE court) | Seuls les panes visibles sont disposes (2 : une colonne) | | |
| 2 | Double-clic sur un teammate cache (carte TEAM) | Le pane revient, la disposition suit | | |
| 3 | Lignes de titre par pane (#152) | Chaque pane a son titre ; aucun pane coupe ni chevauche | | |
| 4 | Projet en conteneur (tmux du conteneur) | Meme disposition | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Deux launchers sur un meme projet

**Validation** : HUMAIN — **Issues** : #153 — **Prerequis** : hub Linux et exe Windows sur un meme projet en conteneur

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Le hub a la main avec `left`, l'exe Windows (lecture seule) avec `top` | L'exe affiche la disposition recue | | |
| 2 | L'exe prend la main | La disposition devient celle de sa configuration | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Leader a gauche conforme au contrat (46 %, 1 colonne jusqu'a 4, 2 colonnes des 5, cdp empiles a gauche)
- [ ] Priorite PROJECT_LAYOUTS > LAYOUT_MODE > top ; prise en compte sans relance et par Ctrl+B R
- [ ] « Leader en haut » inchange ; panes caches et lignes de titre respectes

## Notes QA

Repli sur `top` avec message si tmux refuse la chaine : non provoquable de facon fiable en automatique.
