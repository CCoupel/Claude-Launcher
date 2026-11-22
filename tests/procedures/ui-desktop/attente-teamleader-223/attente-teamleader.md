# Procedure de Test — Teamleader en attente : badge et notifications (#223)

**Version** : 6.7.0
**Date** : 2026-11-22
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — exe Windows natif (Wails) de la version candidate ; interface web `https://launcher.local` dans Chrome ; hub Linux
- [ ] Donnees : projets factices, **un projet en pod (k8s jetable)** ; **faux claude** qui joue les hooks (ou claude reel sur un projet factice) — jamais de vrai projet
- [ ] Hooks : relancer les sessions apres la mise a jour (les hooks `# cl-attention` ne sont lus qu'au demarrage de claude) ; option `ATTENTION_NOTIFY` a sa valeur par defaut (1)
- [ ] **Simuler l'attente** (en QUALIF claude est un faux claude : ses hooks ne se declenchent pas) avec le script `simulate-attention.sh`, depuis la racine du depot, sur l'**hote** ou l'on lance le launcher :
  `sh tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh <chemin du projet> question|permission|teammate|fin`
  (`question` / `permission` : attente du teamleader ; `teammate` : attente d'un autre pane, ne doit rien signaler ; `fin` : equivaut a la reponse de l'utilisateur).
  Le script trouve le pane leader comme le launcher (plus petit `%id` des panes de la session tmux du projet) et affiche ce qu'il ecrit.
  **Projet en pod** : `sh .../simulate-attention.sh <chemin du projet DANS le pod> question --pod <pod> [<namespace>]` (rejoue le script par `kubectl exec -i`).
  Serveur tmux different : variable `TMUX_SOCKET` (defaut `claude-desk-ng2`, fenetre native) ou `TMUX_SESSION`. Avec un vrai claude, le script est inutile : poser une vraie question.
  Notation ci-dessous : **[sim X]** = lancer le script avec l'action X.
- [ ] Maquette de reference : `docs/mockup/v6.7.0/ui/attente_teamleader__badge_notifications.html` (version A)

## Scenarios

### Scenario 1 — Badge : arbre, onglet, vignette Teamleaders (exe Windows)

**Validation** : HUMAIN — **Issues** : #223

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir un projet, **[sim question]** | En 5 s au plus : pastille orange « ❓ en attente » sur la ligne du projet (infobulle = la question), ❓ et soulignement orange sur l'onglet | | |
| 2 | Ouvrir l'onglet Teamleaders | Vignette du projet : cadre orange, pastille dans le titre, bandeau « ❓ question : « … » » ; contour de la pastille Teamleaders sans compteur ni icone seule | | |
| 3 | Comparer a la maquette A | Libelles, couleurs et placement conformes | | |
| 4 | **[sim fin]** (reponse) | Pastille, soulignement, cadre et bandeau disparaissent en 5 s au plus | | |
| 5 | **[sim permission]** | Meme signalement (« permission : … ») ; retire des la reponse | | |
| 6 | **[sim teammate]** (attente ecrite par un teammate) | **Aucun** signalement (seul le teamleader est signale) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Notification Windows (exe) : une seule, clic vers l'onglet, retrait a la reponse

**Validation** : HUMAIN — **Issues** : #223

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fenetre de l'exe en arriere-plan (autre application au premier plan), **[sim question]** | **Une** notification Windows : titre avec le nom du projet, corps = la question ; pas de doublon apres 10 s | | |
| 2 | Cliquer la notification | Fenetre au premier plan, onglet du projet selectionne | | |
| 3 | **[sim fin]** puis observer le centre de notifications | Notification retiree ; aucune nouvelle notification apres la reponse | | |
| 4 | **[sim question]** (nouvelle attente) | Une nouvelle notification (une par attente) | | |
| 5 | Si Windows refuse les notifications (exe non installe) | Badge seul, aucune erreur visible ; ligne dans le journal | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Notification navigateur Chrome sur `https://launcher.local`

**Validation** : HUMAIN — **Issues** : #223

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir l'interface web, option active, autorisation Chrome jamais accordee | Bandeau discret « Autoriser les notifications d'attente » | | |
| 2 | Cliquer le bandeau, autoriser dans Chrome | Bandeau disparu ; la demande de Chrome n'apparait que sur ce clic | | |
| 3 | Onglet Chrome en arriere-plan, **[sim question]** | Notification Chrome (projet, question) ; titre de l'onglet prefixe « ❓ » ; badge dans la page | | |
| 4 | Cliquer la notification | Onglet Chrome au premier plan, onglet du projet selectionne | | |
| 5 | **[sim fin]** | Notification fermee, prefixe « ❓ » retire | | |
| 6 | Recommencer avec l'autorisation **refusee** dans Chrome | Badge seul, plus de bandeau | | |
| 7 | Ouvrir l'interface en `http://<ip>:<port>` (contexte non securise) | Pas de notification ; mention « contexte non securise » sous l'option dans les Reglages ; badge et titre « ❓ » toujours la | | |
| 8 | Deux onglets Chrome sur le meme hub | Une seule notification par attente (pas de doublon) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Projet en pod

**Validation** : HUMAIN — **Issues** : #223

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le projet en pod (session lancee apres la mise a jour), **[sim question]** en variante pod (`--pod`) | Badge en 5 s au plus sur l'exe, l'interface web et la coque Linux (aucun `kubectl exec` supplementaire) | | |
| 2 | Ouvrir le meme projet depuis un autre launcher (session vivante ailleurs) | Le badge est visible aussi (projet non ouvert ici mais session vivante) | | |
| 3 | **[sim fin]** (variante pod) | Badge retire partout en 5 s au plus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Desactivation de l'option

**Validation** : HUMAIN — **Issues** : #223

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages > Teams et panes : decocher « Notifications d'attente » (pastille d'effet « immediat ») | Enregistre sans relance | | |
| 2 | **[sim question]** | **Badge toujours affiche**, aucune notification (Windows ni Chrome) | | |
| 3 | Recocher l'option, nouvelle attente | La notification revient | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Equivalent automatise

**Validation** : AUTO — **Issues** : #223

Joue par `qa` : `cd ng2 && CL_TEST_ISOLATION=1 go test -race ./core ./ui/desktop ./ui/tmux -run '223' -count=1` (`core/attention223_test.go`,
`core/attentionnotify223_test.go`, `ui/desktop/attention223_test.go`, `ui/desktop/attention_notify223_test.go`, `ui/tmux/attention223_test.go`) :
hooks reels executes, signal en 5 s au plus, retrait a la reponse, une notification par attente, aucune avec l'option a 0, coque (pastille, `@cl_attn`, vignette).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer la commande | Tous les tests passent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 5 : signalement en 5 s au plus, notification unique, retrait a la reponse
- [ ] Le badge est toujours affiche, l'option ne desactive que les notifications
- [ ] Aucune regression sur la vignette Teamleaders et la barre d'onglets (`DECISIONS.md` : tab_bar)

## Notes QA

[Espace pour observations]
