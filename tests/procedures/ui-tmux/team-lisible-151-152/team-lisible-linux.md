# Procedure de Test — Team lisible : carte TEAM et en-tetes de panes, coque Linux (#151, #152)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — hub Linux reel (navigateur et panes tmux), jamais la session `claude-hub` de production
- [ ] Donnees : projets factices avec registre d'agents dont un teammate au template en retard ; faux `claude` ; team avec un actif et un inactif
- [ ] Pour le scenario 4 : deux launchers (hub Linux et exe Windows) sur un meme projet en conteneur
- [ ] Equivalents automatises : `TestTeamCardTitleCarriesVersions`, `TestTeamCardRowsHaveNoVersionColumn`, `TestTeamCardNarrowRowsHaveNoVersionColumn`, `TestTeamCardTemplateState`, `TestLayoutWatchSetsStateOptions` (`ng2/ui/tmux/`)

## Scenarios

### Scenario 1 — Carte TEAM

**Validation** : HUMAIN — **Issues** : #151

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Volet d'etat du projet : carte TEAM | Titre « TEAM · N · TPL vX · CLAUDE Y » | | |
| 2 | Lire les lignes | Marque, nom, modele · effort, ⚠, etat ; **aucune colonne de version** | | |
| 3 | Teammate au template en retard / claude different du leader | ⚠ orange en bout de ligne (la ligne dit lequel en pane large) | | |
| 4 | Pane etroit (reduire le volet) | Meme regle ; titre tronque en dernier sur la version de claude | | |
| 5 | Double-clic sur un teammate inactif, surbrillance du pane visible, « + » du compagnon | Inchanges (v6.1.0) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — En-tetes de panes (ligne de titre par pane)

**Validation** : HUMAIN — **Issues** : #152

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir un projet avec team (hub) | Chaque pane a une ligne de titre : « nom · actif » (fond vert), « nom · inactif » (gris sombre), « nom · leader » (neutre) ; pane sans agent : sa commande seule | | |
| 2 | Changer l'etat d'un teammate | Titre mis a jour en environ 1 s, sans relance | | |
| 3 | Fermer le launcher, le relancer, rattacher le projet | Lignes de titre toujours presentes et correctes (options de fenetre reposees au rattachement) | | |
| 4 | Verifier la mise en page | Chaque pane perd une ligne ; aucun pane coupe, mise en page stable | | |
| 5 | Projet en conteneur (tmux du conteneur) | Memes titres | | |
| 6 | `tmux show-options -w pane-border-status` sur la fenetre ; `tmux show-options -pv @cl_state` sur un pane | `top` ; etat conforme a l'affichage | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Launcher anterieur ou en lecture seule

**Validation** : HUMAIN — **Issues** : #152

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Un launcher sans la main (lecture seule) regarde la fenetre | Il n'ecrit aucune option ; il affiche l'etat pose par celui qui a la main | | |
| 2 | Un launcher anterieur a la v6.2.0 prend la main (si disponible) | Les titres restent a leur derniere valeur (limite connue) ; corriges quand un launcher v6.2.0 reprend la main | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — QUALIF a deux launchers sur un meme projet en conteneur

**Validation** : HUMAIN — **Issues** : #152

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub Linux a la main, exe Windows en lecture seule : changer l'etat d'un teammate | Memes etats sur les deux ; l'exe Windows suit | | |
| 2 | L'exe Windows prend la main (Entree) | Il repose les options de pane et de fenetre ; le hub Linux affiche la meme chose | | |
| 3 | Le hub reprend la main | Idem ; aucune ligne de titre en double, aucun decalage de geometrie cote Windows | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Carte TEAM : versions dans le titre, ⚠ pour les ecarts, plus de colonne de version
- [ ] Une ligne de titre par pane, colorée selon l'etat, mise a jour en environ 1 s, reposee au rattachement
- [ ] Deux launchers coherents sur un meme projet en conteneur

## Notes QA

Aucun test automatique des options de fenetre a l'ouverture et au rattachement cote coque (elles sont couvertes par le test de l'interface native et par ce scenario).
