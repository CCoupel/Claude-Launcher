# Procedure de Test — Modele et effort des teammates depuis la carte TEAM (#250)

**Version** : 6.9.0
**Date** : 2026-11-25
**Testeur** : QA (scenario AUTO) + utilisateur (scenarios HUMAIN)

Reference : `contracts/desktop-dispatch.md` « v6.9.0 » (`teamAgentSet`, `clientInfo`, `TeamRow`, coque `team-set`), maquettes retenues `docs/mockup/v6.9.0/ui/team_card__modele_effort.html` (M1 A, fenetre native et navigateur) et `team_card__modele_effort_coque.html` (M2 A, coque tmux), `docs/mockup/DECISIONS.md` (team_card), regle du template `MODEL_EFFORT` §7.

## Prerequis

- [ ] Environnement : conteneur de dev (AUTO) ; QUALIF (exe Windows, coque tmux du hub, launcher du cluster derriere l'Ingress) pour les scenarios HUMAIN
- [ ] Donnees : **projet factice uniquement** (jamais un vrai projet), cree sur la QUALIF et initialise avec le template (registre d'agents, `teamleader.template.md`, `settings.json` du projet) ; le projet factice est supprime en fin de procedure
- [ ] Acces : `go`, `node` (AUTO) ; fenetre native Windows, navigateur en boucle locale, navigateur via l'Ingress QUALIF, coque tmux du hub (HUMAIN)
- [ ] Git : le projet factice est un depot ; relever `git status` / `git diff` avant et apres chaque scenario

## Scenarios

### Scenario 1 — Ecriture, regles, refus distant (automatise)

**Validation** : AUTO — **Issues** : #250

```bash
cd ng2 && CL_TEST_ISOLATION=1 go test -race -count=1 -run '250' ./core/ ./ui/desktop/ ./ui/tmux/ && echo OK
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Commande ci-dessus | compagnon : corps conserve octet pour octet (CRLF, BOM), cle retiree si egale au template, bloc retire puis fichier supprime, jamais d'ecriture dans `.template.md` ; refus (hors registre, `../`, valeur hors liste, lien symbolique, `cdp`) sans ecriture ; table `ModelEfforts` et `effortAdjusted` ; `settings.json` conserve hors `model`/`effortLevel`, JSON invalide = rien d'ecrit, `partial` ; `teamAgentSet` refuse en acces distant (hote, source, `X-Forwarded-For`, `Forwarded`) ; `clientInfo` ; `team-set` codes 0 et 2 ; `display-menu` (entrees defaut `d` et `0`, efforts grises, position) ; zone de clic ; simulation node du menu | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Changer le planner dans la fenetre native : diff du compagnon

**Validation** : HUMAIN — **Issues** : #250 (rendu du menu, conformite a la maquette M1 A, lisibilite du diff)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Exe Windows, ouvrir le projet factice, carte TEAM : lire la ligne du planner | « modele · effort » = valeurs du template, sans « + » devant le nom (pas de compagnon) | | |
| 2 | Cliquer sur « modele · effort » du planner | menu conforme a la maquette M1 A : rubriques Modele puis Effort, **« Defaut (template) — <valeur> » en tete de chacune, cochee**, valeur egale au template marquee « template = defaut », « Tout par defaut » et note de pied (fichier ecrit, prochain spawn) | | |
| 3 | Choisir le modele « opus » puis l'effort « max » | message de confirmation (fichier `implementation-planner.md`), ligne « opus · max », un « + » devant le nom ; carte relue tout de suite (sans F5) | | |
| 4 | Dans le projet : `git diff` / ouvrir `.claude/agents/implementation-planner.md` | fichier **cree avec ce seul frontmatter** `---` / `model: opus` / `effort: max` / `---` ; `implementation-planner.template.md` inchange | | |
| 5 | Ajouter a la main un corps (texte) dans ce compagnon, puis re-choisir l'effort « high » | seule la ligne `effort:` change, le corps et les autres lignes sont intacts (`git diff`) | | |
| 6 | Ouvrir le menu avec un modele sans effort possible (poser `model: claude-haiku-4-5` a la main, rafraichir) | entrees d'effort **grisees avec infobulle « non pris en charge »** (jamais masquees), « Defaut » **jamais grisee** | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — « Defaut » puis « Tout par defaut » : la cle puis le fichier disparaissent

**Validation** : HUMAIN — **Issues** : #250 (comprehension de l'entree « Defaut », conformite a la maquette)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Partir du compagnon du scenario 2 (`model: opus`, `effort: max` + un corps) ; menu → Modele → « Defaut (template) » | `model:` retire du frontmatter, `effort:` conserve ; « Defaut » coche dans la rubrique Modele ; ligne = modele du template | | |
| 2 | Menu → « Tout par defaut » | frontmatter retire, **le corps reste** (le compagnon a encore du contenu) ; « Defaut » coche dans les deux rubriques | | |
| 3 | Supprimer le corps a la main, re-choisir « opus » puis « Tout par defaut » | le compagnon est **supprime** ; message « compagnon supprime (template) » ; le « + » disparait de la ligne | | |
| 4 | Choisir explicitement la valeur egale au template (ex. « sonnet » si le template dit sonnet) | meme resultat que « Defaut » : aucune cle ecrite, jamais de valeur figee egale au template | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Changer un teammate qui tourne : ⟳, puis effet au respawn

**Validation** : HUMAIN — **Issues** : #250 (rendu du ⟳ et de l'infobulle, effet reel au spawn)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet factice avec une team lancee (un teammate qui tourne en modele A) ; changer son modele pour B | la ligne affiche **la nouvelle valeur B suivie de ⟳** ; infobulle « en cours : A — B au prochain spawn » | | |
| 2 | Verifier que le teammate garde A dans son pane (en-tete du pane ou `/status` dans le teammate) | il tourne toujours en A jusqu'au prochain spawn | | |
| 3 | Arreter ce teammate puis le relancer (nouveau spawn) | il demarre en B, le ⟳ disparait | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Teamleader : `teamleader.md` + `settings.json`, puis nouvelle session (R7)

**Validation** : HUMAIN — **Issues** : #250 (effet reel sur une session Claude Code ; `xhigh` / `max` acceptes ou non par la version installee)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Relever `.claude/settings.json` du projet factice (copie) puis, sur la ligne du teamleader, choisir modele « opus » et effort « xhigh » | message : `teamleader.md` **et** `settings.json` ecrits, rappel « la session en cours garde son modele (/model, /effort) » | | |
| 2 | `git diff` du projet | `teamleader.md` : frontmatter `model: opus` / `effort: xhigh` ; `settings.json` : **seules** les valeurs de `model` et `effortLevel` ont change (ou ont ete ajoutees en tete), tout le reste identique (ordre, espaces) ; `settings.local.json` intact | | |
| 3 | Fermer puis relancer la session du teamleader (nouvelle session) | la session demarre en opus ; `/model` et `/effort` (ou l'en-tete) montrent opus et xhigh | | |
| 4 | Recommencer avec l'effort « max » (nouvelle session) | idem : max accepte. **Si la version de Claude Code refuse `xhigh` ou `max`** : le noter ici (R7) — `settings.json` doit alors recevoir l'effort accepte le plus proche | | |
| 5 | Retour : « Tout par defaut » | `teamleader.md` perd les cles ; `settings.json` recoit les **valeurs du template** (cles non retirees) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Navigateur distant (Ingress QUALIF) : grise et refuse

**Validation** : HUMAIN — **Issues** : #250 (etat grise, infobulle, refus constate depuis un vrai acces distant)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le launcher du cluster QUALIF par l'adresse de l'Ingress, carte TEAM d'un projet factice | « modele · effort » **non cliquable**, grise ; infobulle « reglage reserve a la fenetre native » | | |
| 2 | Cliquer dessus, Entree et Espace au clavier | aucun menu ne s'ouvre | | |
| 3 | Dans le projet : `git status` | aucun fichier modifie | | |
| 4 | Ouvrir le meme launcher en boucle locale (navigateur du hub, `127.0.0.1`) | menu disponible comme dans la fenetre native ; un reglage est ecrit | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Coque tmux : menu a la position du clic

**Validation** : HUMAIN — **Issues** : #250 (positionnement du menu, lisibilite, conformite a la maquette M2 A)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Coque tmux du hub, projet factice ouvert, cliquer sur la colonne modele d'une ligne de la carte TEAM | menu `display-menu` **ouvert a l'endroit du clic** (pas decale, pas hors ecran), entrees « defaut (template) » (touche `d` / `0`), `o s h f`, `1`-`5`, `t` | | |
| 2 | Choisir un effort incompatible avec le modele (ligne en `claude-opus-4-5`, effort `max`) | entree **grisee**, sans raccourci | | |
| 3 | Choisir une valeur (touche ou clic) | message 6 s sous la liste, ligne mise a jour, ⟳ si le teammate tourne ; double-clic sur le **nom** inchange (masque / affiche) | | |
| 4 | Cliquer sur une ligne non reglable (`cdp`) | pas de menu, raison dite sous la liste | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenario 1 (AUTO) vert sous `-race`
- [ ] Les compagnons modifies conservent leur corps octet pour octet ; aucun `.template.md` modifie
- [ ] « Defaut (template) » present, coche selon la source, jamais grise, dans les trois interfaces
- [ ] Acces distant refuse et grise ; fenetre native et boucle locale autorises
- [ ] Aucune regression sur la carte TEAM (double-clic de masquage, œil, versions en en-tete)

## Notes QA

[Espace pour observations — noter notamment le comportement de `xhigh` / `max` (R7)]
