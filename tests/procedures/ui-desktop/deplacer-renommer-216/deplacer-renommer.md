# Procedure de Test — Deplacer / renommer un projet depuis l'interface (#216)

**Version** : 6.6.0
**Date** : 2026-11-21 (+ scenario 8, env.yaml projet ouvert)
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

### Scenario 8 — env.yaml : modification refusee tant que le projet ou son conteneur tourne (complement #216)

**Validation** : HUMAIN — **Issues** : #216

Meme regle que « Deplacer / renommer » et que le choix backend / image : l'environnement d'un projet (paquets, setup, on_start, watch, mounts, prefer, local, image de l'env.yaml) ne se modifie pas tant que le projet est ouvert ou que son conteneur tourne, ici **ou** depuis un autre launcher. Couleur, organisation et masquage des inactifs restent modifiables a chaud.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet **ferme** (aucun onglet, conteneur arrete) : Reglages > Projet, ajouter un paquet puis Enregistrer ; puis « Editer en YAML » et modifier `setup` | Les deux ecritures reussissent ; `env.yaml` du projet modifie | | |
| 2 | Ouvrir le projet, rouvrir Reglages > Projet | Sections d'environnement (paquets, setup, on_start, mounts…) **grisees / en lecture seule** avec la mention « fermez le projet pour modifier son environnement » ; couleur, organisation, masquage restent actifs | | |
| 3 | Meme projet ouvert : « Editer en YAML » de l'env.yaml | Editeur en lecture seule avec la meme mention ; Enregistrer impossible | | |
| 4 | Projet ouvert : changer la **couleur** puis le masquage des inactifs | Effet **a chaud**, sans refus | | |
| 5 | Provoquer l'ecriture malgre l'interface (appel `/api/call` forge ou second onglet de Reglages ouvert **avant** l'ouverture du projet) : enregistrer l'environnement | Refus lisible du serveur (`{ok:false}` + raison « projet ouvert dans ce launcher ») ; `env.yaml` inchange (comparer avant/apres) | | |
| 6 | Projet ouvert **par un autre launcher** (l'exe ouvre le projet, le hub tente de modifier l'environnement) | Refus « projet ouvert par un autre launcher » ; apres fermeture dans l'exe, l'ecriture est possible | | |
| 7 | Projet en conteneur dont le **conteneur tourne** mais sans onglet ouvert ici (session laissee en arriere-plan) | Refus « conteneur ou pod en cours d'execution » ; apres arret du conteneur, ecriture possible | | |
| 8 | Hub : F2 sur un projet ouvert > entrees d'environnement | Entrees **grisees** avec la mention « fermez le projet pour modifier son environnement » ; couleur / masquage inchanges | | |
| 9 | **Projet clone** a l'approbation en attente : depuis la fenetre ou la coque, Accepter / Ouvrir sans / Revoir | L'approbation fonctionne normalement **meme si** elle ecrit ou scelle l'empreinte de `env.yaml` : le refus ne la bloque pas | | |
| 10 | Creer un nouveau projet avec un socle conteneur (ecriture de l'env.yaml a la creation) et cloner un depot avec socle | Creation et clonage reussissent (le projet n'est pas encore ouvert) | | |
| 11 | Projet **non ouvert ici**, backend conteneur **injoignable** (arreter le daemon podman / couper l'acces au serveur docker ou kubernetes) : modifier l'environnement (formulaire puis YAML) | Ecriture **autorisee** avec l'avertissement « etat du conteneur inconnu (backend injoignable) : s'appliquera a la prochaine ouverture » ; reponse en quelques secondes (jamais d'attente sans fin) | | |
| 12 | Meme situation (backend injoignable) : « Deplacer / renommer » le projet | **Refuse** (« non verifiable ») : le deplacement reste prudent ; apres retablissement du backend, il est possible | | |
| 13 | Backend injoignable **et** projet ouvert ici (ou conteneur confirme en cours sur un autre backend) | Modification de l'environnement refusee (le blocker certain l'emporte sur l'etat inconnu) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Un deplacement reussi ne perd rien : fichiers, `.git`, reglages (couleur, organisation, masquage), affectation, historique des conversations
- [ ] Un echec laisse l'etat initial exact (ou un etat « inacheve » repris explicitement) — jamais un projet a moitie deplace
- [ ] L'approbation d'environnement d'un projet clone suit le projet (meme etat, meme sha) ; aucune entree orpheline, aucune approbation heritee par erreur
- [ ] Jamais de deplacement d'un projet ouvert (ici ou par un autre launcher) ni d'un conteneur en cours d'execution
- [ ] Aucun dossier cree hors des bases configurees
- [ ] L'environnement (env.yaml) d'un projet ouvert, ou dont le conteneur tourne, n'est jamais modifie ; l'approbation d'un depot clone et la creation ne sont pas bloquees

## Notes QA

Les verifications de logique (etape en echec → etat initial, reprise, EXDEV, trust.json) sont automatisees. Supprimer les projets et dossiers de test a la fin et restaurer les sauvegardes de `projects.json`, `config.conf`, `trust.json`.
