# Procedure de Test — Teammates inactifs caches, carte TEAM (#136)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA (scenario 7 : utilisateur)

Reference : `contracts/tmux-options.md`, `contracts/desktop-dispatch.md`, maquette `docs/mockup/v6.1.0/ui/team_card__panes_caches.html`.

## Prerequis

- [ ] Environnement : QUALIF — poste Windows (exe natif) et hub Linux ; projet en conteneur ouvert des deux cotes pour le scenario 6
- [ ] Donnees : projet factice avec une team d'au moins 4 teammates (faux `claude`) ; `TEAM_HIDE_IDLE=30` (defaut) puis `0` ; `TEAM_HIDE_IDLE=10` conseille pour accelerer
- [ ] Acces : le launcher teste a la main sur le projet (`@cl_owner`) ; jamais de vrais projets ni de vrai `claude`, sauf scenario 7 (vrai Claude Code, par l'utilisateur)

## Scenarios

### Scenario 1 — Masquage automatique d'un inactif et retour a l'activite

**Validation** : AUTO — **Issues** : #136

**Objectif** : un teammate inactif est cache apres `TEAM_HIDE_IDLE` s ; un actif est toujours visible. Equivalent automatise : tests `core` (regle) et `ui-tmux` (tmux isole) du LOT-2.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le projet, laisser un teammate inactif | Ligne de la carte TEAM en surbrillance (visible) | | |
| 2 | Attendre `TEAM_HIDE_IDLE` s (+ 1 tour) | Son pane quitte la fenetre ; ligne sans surbrillance ; les visibles se repartagent toute la fenetre | | |
| 3 | `tmux list-panes -a -F '#{pane_id} #{session_name}'` | Le `%id` existe, dans la session `cl-hidden` | | |
| 4 | Rendre le teammate actif | Pane ramene dans la fenetre, meme `%id` | | |
| 5 | Mettre `TEAM_HIDE_IDLE=0`, attendre | Aucun masquage automatique | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Double-clic : cacher / fixer visible

**Validation** : HUMAIN — **Issues** : #136

**Objectif** : bascule manuelle d'un inactif depuis la carte TEAM (Windows et Linux).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Double-clic sur un teammate inactif visible | Pane cache immediatement ; surbrillance retiree ; mise en page des visibles recalculee | | |
| 2 | Double-clic sur ce teammate (cache) | Pane ramene ; surbrillance ; reste visible meme apres `TEAM_HIDE_IDLE` s (fixe) | | |
| 3 | Second double-clic sur le visible fixe | Pane cache a nouveau | | |
| 4 | Rendre un visible fixe actif puis inactif | Reprend la regle normale : cache `TEAM_HIDE_IDLE` s apres la fin d'activite | | |
| 5 | Mise en page : leader en haut, ≤ 5 teammates sur une ligne, au-dela sur deux | Conforme a la regle existante, sans trou | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Refus de la bascule

**Validation** : HUMAIN — **Issues** : #136

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Double-clic sur un teammate actif | Refus, message en pied de volet ; rien ne bouge | | |
| 2 | Double-clic sur le leader, puis sur un agent `cdp` | Refus avec message ; jamais caches | | |
| 3 | Double-clic sur un pane ouvert a la main (sans agent) | Refus avec message | | |
| 4 | Projet en lecture seule (un autre launcher a la main) : double-clic | Refus avec message ; `@cl_panevis` non ecrit | | |
| 5 | Double-clic sur un teammate non demarre (ligne ○) | Rien ne se passe (ou refus), aucune erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Focus clavier

**Validation** : HUMAIN — **Issues** : #136

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Donner le focus clavier a un teammate inactif, attendre `TEAM_HIDE_IDLE` s | Pas masque automatiquement | | |
| 2 | Deplacer le focus ailleurs | Masque au tour suivant | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Fermeture du projet : aucun processus cache ne survit

**Validation** : AUTO — **Issues** : #136

**Objectif** : equivalent automatise `critical` (tests `ui-tmux`) ; a rejouer en reel sur QUALIF.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cacher 2 teammates, fermer le projet (question de fermeture) | La question compte aussi les panes caches | | |
| 2 | `tmux list-sessions` / `pgrep -af claude` apres fermeture | Plus de session `cl-hidden`, aucun processus des teammates | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Deux launchers attaches (hub Linux + exe Windows)

**Validation** : HUMAIN — **Issues** : #136

**Objectif** : etat partage via `@cl_panevis`, `@cl_idle_at`, `@cl_home`, sans perte de pane.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le meme projet en conteneur des deux cotes | Memes teammates caches/visibles des deux cotes | | |
| 2 | Cacher un teammate depuis le launcher qui a la main | L'autre launcher le voit cache (carte, fenetre) | | |
| 3 | Faire prendre la main par l'autre launcher | Aucun pane perdu, `%id` identiques, cache toujours caches | | |
| 4 | Depuis le launcher sans la main : double-clic | Refus avec message | | |
| 5 | Reouvrir le projet apres arret d'un launcher | Panes caches recenses (carte TEAM complete) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Un vrai Claude Code cache repond a un message (joue par l'utilisateur)

**Validation** : HUMAIN — **Issues** : #136

**Objectif** : un teammate cache continue de travailler et de recevoir ses messages (jamais en test automatique).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet reel de test avec un vrai teammate inactif ; le laisser se cacher | Pane dans la reserve, ligne de la carte non surlignee | | |
| 2 | Envoyer un message au teammate depuis le leader | Le teammate traite le message (reponse recue) | | |
| 3 | Il passe actif pendant le travail | Ramene dans la fenetre, sortie coherente | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 7 passent (Windows et Linux)
- [ ] Aucun pane tue, `%id` conserves ; aucun processus cache apres fermeture
- [ ] Aucune regression sur la mise en page de la fenetre du projet

## Notes QA

Limite connue : un launcher anterieur a v6.1.0 qui ferme le projet laisse les panes caches en vie.
