# Procedure de Test — Deplacer / renommer un projet depuis l'interface (#216)

**Version** : 6.6.0
**Date** : 2026-11-21
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 (`projectMoveInfo`, `projectMove`, etapes `dir` → `assign` → `conf` → `history` → `trust` → `ctr`, journal `moves.json`) ; maquette retenue B `docs/mockup/v6.6.0/ui/project_move__deplacer_renommer.html`. Equivalents automatises (projets factices, faux podman/k8s, echec injecte a chaque etape, EXDEV simule) : `ng2/core/projectmove216_test.go`, `ng2/ui/desktop/move216_test.go`, `ng2/ui/tmux/move216_test.go`. Automatise, donc a ne pas rejouer a la main : retour arriere exact a chaque etape, reprise apres echec du retour arriere, destination existante ou hors base refusee, copie EXDEV, blockers.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x, **hub** (coque tmux), et si possible une base sur partage 9P `\\wsl.localhost\<distro>\…`
- [ ] Donnees : bases factices uniquement (jamais de vrais projets) ; au moins deux categories (`INFRA`, `PRO`) ; un projet factice `alpha` (avec `.git`, un fichier `config.conf` par projet : couleur du projet, organisation) ; un **projet clone approuve** (depot de test avec `env.yaml`, approbation « Accepter » donnee — voir la procedure #215, scenario 6) ; les noms de test sont supprimes a la fin
- [ ] Acces : ecriture sur les bases ; sauvegarde de `projects.json`, `config.conf`, `trust.json` du dossier de config avant de commencer

## Scenarios

### Scenario 1 — Renommer et deplacer dans la meme base (exe Windows)

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Clic droit sur `alpha` dans l'arbre (et sur sa carte) | Menu contextuel avec « Deplacer / renommer… » | | |
| 2 | Ouvrir l'action sur un projet **ouvert** | Dialogue (maquette B) : bloqueur « projet ouvert dans ce launcher », bouton « Fermer le projet » propose ; validation grisee | | |
| 3 | « Fermer le projet » puis rouvrir le dialogue | Plus de bloqueur ; effets listes (dossier, affectation, reglages, historique, conteneur) ; chemin final affiche | | |
| 4 | Base inchangee, categorie `PRO`, nom `alpha2`, valider | Projet deplace ; **arbre relu** et selection suivie ; ancien chemin disparu | | |
| 5 | Verifier le disque | Dossier `…\PRO\alpha2` avec son `.git` et ses fichiers intacts ; l'ancien dossier n'existe plus | | |
| 6 | Reglages du projet deplace | Couleur, organisation, masquage, backend / image : **identiques** a avant | | |
| 7 | Rouvrir le projet | S'ouvre normalement ; l'historique des conversations (reprise `↺`) est **retrouve** sous le nouveau nom | | |
| 8 | Renommer seulement (meme categorie, autre nom), puis revenir au nom d'origine | Fonctionne dans les deux sens | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Refus : destination existante, hors base, nom invalide

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Destination identique a la source | Refus lisible (« destination identique ») | | |
| 2 | Destination existante (un autre projet, ou un dossier du meme nom) | Refus lisible sous le champ ; rien deplace | | |
| 3 | Nom `..\x` ou `a/b`, categorie `..` | Erreur sous le champ concerne ; validation bloquee | | |
| 4 | Verifier les dossiers | Aucun dossier cree hors des bases ; `projects.json` et `config.conf` inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Deplacement sur base 9P (`\\wsl.localhost\…`)

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Deplacer un projet **dans** la base 9P (rename sur le meme montage) | Termine sans erreur, duree acceptable, dossier au nouvel emplacement | | |
| 2 | Deplacer un projet de la base locale `C:\…` vers la base 9P (**autre systeme de fichiers**) | Copie complete + controle + suppression de la source : progression ou attente claire, **aucun fichier manquant** (comparer le nombre de fichiers et le `git status` du projet : propre) | | |
| 3 | Interrompre une copie volumineuse (fermer la fenetre pendant la copie, si faisable) puis relancer le launcher | Etat coherent : soit le projet est a la source (tout intact), soit « deplacement inacheve » est propose avec **Reprendre / Annuler** ; jamais un projet a moitie copie visible comme projet | | |
| 4 | Pas de dossier `*.moving-*` residuel apres reussite ou annulation | Aucun reste | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Projet clone approuve : l'approbation suit, aucune nouvelle question

**Validation** : HUMAIN — **Issues** : #216, #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Prendre le projet clone dont l'environnement est **accepte** (affecte a un backend conteneur), le fermer | Etat `accepted` dans `trust.json` (cle = chemin du projet) | | |
| 2 | Le deplacer / renommer (ex. categorie `PRO`, nom `depot-renomme`) | Deplacement reussi | | |
| 3 | `trust.json` (copie avant/apres) | **Aucune entree orpheline** : l'ancienne cle a disparu, la nouvelle porte le **meme etat et le meme sha** ; `trust.json.bak` cree | | |
| 4 | Rouvrir le projet deplace en conteneur | **Aucune** question d'approbation ; l'environnement approuve est applique | | |
| 5 | Meme test avec un projet en etat « ouvrir sans » (`ignored`) | L'etat suit, le socle seul reste applique, aucune question | | |
| 6 | Meme test avec un projet `pending` (jamais repondu) | La question reste posee apres le deplacement (rien n'est approuve par le deplacement) | | |
| 7 | Deplacer vers une destination dont la cle existe deja dans `trust.json` (reste d'un ancien projet de meme nom) | Refus ou ecrasement explicite signale ; jamais une approbation heritee par erreur | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Hub : entree « Deplacer / renommer » dans F2 (coque tmux)

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Sur le hub, projet ferme sous le curseur : F2 | Entree « Deplacer / renommer » presente (aucune nouvelle touche) | | |
| 2 | Projet **ouvert** : F2 > Deplacer / renommer | Refus : « fermer le projet d'abord » (bloqueur) | | |
| 3 | Fermer, F2 > Deplacer / renommer : base, categorie (liste), nom | Memes etapes et controles que la creation (#214) ; chemin final affiche | | |
| 4 | Valider | Projet deplace, arbre relu, curseur sur le projet ; fenetre tmux recree au **nouveau nom** a la prochaine ouverture ; ancien nom de fenetre absent | | |
| 5 | Projet ouvert depuis un **autre launcher** (exe) | Blocage « ouvert par un autre launcher » (presence) ; apres fermeture, deplacement possible | | |
| 6 | Navigateur du projet / URLs `/files/<cle>/…` | Fonctionnent avec la **nouvelle** cle ; l'ancienne ne repond plus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Conteneur / pod : ancien environnement supprime, nouveau cree a l'ouverture (QUALIF)

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet en conteneur (podman ou pod QUALIF) **en cours d'execution** | Bloqueur « conteneur en cours d'execution » ; deplacement impossible | | |
| 2 | Arreter le conteneur / pod, puis deplacer | Deplacement reussi | | |
| 3 | `podman ps -a` ou `kubectl get pods,statefulsets` | L'ancien conteneur / StatefulSet + pod `<ancienne cle>` sont **supprimes** ; aucun orphelin | | |
| 4 | Rouvrir le projet deplace | Nouveau conteneur / pod au **nouveau nom** (`<categorie>-<nom>`) ; session claude demarree ; fichiers du projet visibles | | |
| 5 | Pods des **autres** projets | Inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Echec et reprise (si reproductible)

**Validation** : HUMAIN — **Issues** : #216

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Provoquer un echec reel a l'etape `ctr` (ex. daemon podman arrete) | Message « echec a l'etape ctr » ; les etapes precedentes sont **annulees** : projet a son emplacement et son nom d'origine, reglages et historique d'origine | | |
| 2 | Verifier `moves.json` (dossier de config) | Pas d'entree restante apres un retour arriere reussi | | |
| 3 | Rendre un fichier du dossier de destination verrouille pour faire echouer aussi le retour arriere (cas extreme, optionnel) | Etat « deplacement inacheve » : bloqueur affiche, actions **Reprendre** / **Annuler** ; apres correction, Reprendre termine le deplacement, Annuler retablit l'etat initial | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Un deplacement reussi ne perd rien : fichiers, `.git`, reglages (couleur, organisation, masquage), affectation, historique des conversations
- [ ] Un echec laisse l'etat initial exact (ou un etat « inacheve » repris explicitement) — jamais un projet a moitie deplace
- [ ] L'approbation d'environnement d'un projet clone suit le projet (meme etat, meme sha) ; aucune entree orpheline, aucune approbation heritee par erreur
- [ ] Jamais de deplacement d'un projet ouvert (ici ou par un autre launcher) ni d'un conteneur en cours d'execution
- [ ] Aucun dossier cree hors des bases configurees

## Notes QA

Les verifications de logique (etape en echec → etat initial, reprise, EXDEV, trust.json) sont automatisees. Supprimer les projets et dossiers de test a la fin et restaurer les sauvegardes de `projects.json`, `config.conf`, `trust.json`.
