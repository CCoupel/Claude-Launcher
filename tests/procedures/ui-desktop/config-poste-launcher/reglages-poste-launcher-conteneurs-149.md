# Procedure de Test — Reglages : Ce poste, Launcher, Conteneurs (#149, LOT-3.3)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA (scenarios 7 et 9 : utilisateur)

Reference : `contracts/desktop-dispatch.md` (LOT-3.3), maquette `docs/mockup/v6.1.0/ui/config_ui__interface_structuree.html` (A — rubriques Launcher, Conteneurs, Ce poste).

## Prerequis

- [ ] Environnement : QUALIF — exe Windows natif (et hub Linux du meme poste pour le scenario 8)
- [ ] Donnees : `config.conf`, `backends.yaml` et `env.yaml` du launcher **sauvegardes avant** (`avant.*`) ; `config.conf` avec commentaires, autres cles variees (tableaux, `declare -A`), et un jeton GitHub **factice** (`ghp_FACTICE…`, jamais un vrai) ; `backends.yaml` avec une section `hubs.<machine>` (nom de ce poste) ; un `config.conf` d'une forme bash non reconnue pour le scenario 4 ; projets factices seulement
- [ ] Acces : ecriture sur le dossier de config du launcher ; masquer tout `ghp_…` dans une capture d'ecran

## Scenarios

### Scenario 1 — Ouverture rapide et lecture des trois rubriques

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir Reglages (⚙ config, Ctrl+,) et chronometrer | Fenetre en moins d'une demi-seconde, « Chargement… » puis contenu | | |
| 2 | Rubrique Launcher | Racines des projets (introuvable signale ⚠), options de claude, dossier de config, jeton, variables ajoutees, teams experimentales, souris ; reprise des sessions ; mise en page des teams ; palette ; consommation ; arret du conteneur | | |
| 3 | Rubrique Conteneurs | Paquets, par gestionnaire, etapes `setup`, `on_start` du `env.yaml` du launcher | | |
| 4 | Rubrique Ce poste | Nom de la machine et systeme ; correspondance de chemins ; utilisateur distant (nom, uid:gid, dossier personnel) ; taille des caracteres ; navigateur | | |
| 5 | Fichier absent (`config.conf` renomme) | Rubrique Launcher ouverte sur les valeurs par defaut, sans erreur | | |
| 6 | Pied de fenetre | Nom du fichier de la rubrique, « ✓ valide » ou problemes listes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Launcher : enregistrement, commentaires et autres cles conserves

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Copier `config.conf` en `avant.conf` | | | |
| 2 | Modifier « Reprendre » (demander → jamais) et « Age maximal », Enregistrer | Message d'enregistrement ; note de prise en compte (a chaud ou redemarrage) | | |
| 3 | `diff avant.conf config.conf` | Seules les lignes des cles modifiees different ; commentaires, tableaux, autres cles et ordre intacts | | |
| 4 | Ajouter une racine de projets, en retirer une | Tableau `GITHUB_DIRS` mis a jour sans toucher le reste ; racine introuvable signalee mais non bloquante | | |
| 5 | Variables ajoutees (ajouter / modifier / supprimer) | Ecrites dans `EXTRA_ENVS` ; relues identiques apres fermeture/reouverture | | |
| 6 | Valeur invalide (entier non numerique, teinte hors 1 a 5, palette hors 0–255) | Probleme en pied, Enregistrer bloque, fichier inchange | | |
| 7 | Fichier modifie a la main pendant que l'ecran est ouvert, puis Enregistrer | Conflit signale (recharger / ecraser) | | |
| 8 | `config.conf.bak` | Copie de l'etat precedent | | |
| 9 | Fichier `config.conf` aux fins de ligne CRLF (Windows) | Enregistrement OK, lignes d'origine conservees, fichier relu par le launcher | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — TEAM_HIDE_IDLE depuis Launcher et prise en compte a chaud

**Validation** : HUMAIN — **Issues** : #149 (#136)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Launcher → Mise en page des teams → « Cacher les inactifs apres » : 15, Enregistrer | `TEAM_HIDE_IDLE=15` ecrit ; note de prise en compte | | |
| 2 | Projet factice ouvert avec une team factice (teammate inactif) | Pane cache apres ~15 s, sans relancer le launcher (prise en compte a chaud) | | |
| 3 | Mettre 0 | Plus aucun masquage automatique | | |
| 4 | Valeur negative ou non numerique | Refus en pied, fichier inchange | | |
| 5 | Teinte actif/inactif, leader (%) | Pris en compte a la prochaine mise en page ; la mise en page des teams reste valide | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Formes bash non reconnues : lecture seule, rien d'ecrit

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `config.conf` contenant une forme que l'interface ne sait pas lire (ex. `OPTS="$(commande)"`, valeur calculee, `source autre.conf`, tableau construit par boucle) | La rubrique affiche la valeur en **lecture seule** avec une indication (« modifier en YAML/texte ») ; aucune erreur | | |
| 2 | Modifier une autre cle et Enregistrer | La ligne non reconnue est **conservee a l'identique** (diff) | | |
| 3 | « Editer en YAML » (texte) sur `config.conf` | Editeur sur le fichier pour modifier la forme a la main | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Jeton GitHub masque

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir Launcher avec un jeton factice present | Champ affiche « ghp_•••••• » (ou « defini ») ; jamais la valeur en clair a l'ecran, ni dans la barre d'etat, ni dans les messages | | |
| 2 | Enregistrer sans toucher au jeton | `GITHUB_TOKEN` inchange dans le fichier (diff vide sur cette ligne) | | |
| 3 | Saisir un nouveau jeton factice, Enregistrer | Ecrit ; le champ reste masque apres reouverture | | |
| 4 | Vider le jeton (action explicite) | Cle retiree ou videe selon la regle ; jamais ecrasee sans action | | |
| 5 | « afficher » (si propose) | Affichage volontaire seulement ; refermer la fenetre le re-masque | | |
| 6 | Droits du fichier `config.conf` apres ecriture (`ls -l` ; sous Windows : ACL de l'utilisateur seul) | 0600 (lecture/ecriture proprietaire uniquement) | | |
| 7 | Mode `--web` : reponse de `configModel` dans l'onglet reseau du navigateur | Aucune trace du jeton en clair (`{ "set": true }` seulement) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Conteneurs : env.yaml du launcher

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter un paquet, un paquet par gestionnaire (apt), une etape `setup` et une `on_start`, Enregistrer | Ecrits dans `env.yaml` du launcher ; commentaires conserves (diff) | | |
| 2 | Cle reservee au projet (ex. `image`) ajoutee par « Editer en YAML » | Refus a l'enregistrement (cles autorisees : packages, packages_by_manager, setup, on_start) | | |
| 3 | Ouvrir les reglages d'un projet : apercu fusionne | Les ajouts apparaissent marques « launcher » | | |
| 4 | Fichier absent | Cree a l'enregistrement, avec seulement les cles saisies | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Ce poste : chemins, utilisateur distant, surcharges par systeme (joue par l'utilisateur)

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter une correspondance `\\nas\MFS` ↔ `/share/MFS` (forme Windows), Enregistrer | Ecrite sous `hubs.<ce poste>.paths` ; commentaires de `backends.yaml` conserves | | |
| 2 | Verifier le meme fichier cote Linux (hub du meme poste) | Entree de forme Windows ignoree sous Linux avec indication « ignoree sur ce systeme » ; aucune erreur | | |
| 3 | Ajouter une correspondance de forme Linux | Visible comme « autre systeme » depuis Windows, non appliquee, non supprimee | | |
| 4 | Utilisateur distant : nom, uid:gid, dossier personnel | Enregistres ; valeurs invalides (uid non numerique) refusees | | |
| 5 | Serveur web du navigateur (defaut de tous les postes) | Valeur du defaut modifiable ; « Ce poste » vide = le defaut s'applique ; surcharge par poste affichee | | |
| 6 | Autre poste (section `hubs` d'une autre machine) | Jamais modifiee, jamais affichee comme celle de ce poste | | |
| 7 | Nom de machine non resolu / absent de `hubs` | Section creee a l'enregistrement sous le bon nom, sans toucher aux autres | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 8 — Taille des caracteres depuis Ce poste

**Validation** : HUMAIN — **Issues** : #149 (#140)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ce poste → Taille des caracteres : + puis − | Valeur 8 a 28 ; les terminaux ouverts changent en direct | | |
| 2 | Ctrl+molette sur un terminal puis rouvrir Ce poste | La valeur affichee suit la molette (meme preference `termFontSize`) | | |
| 3 | Fermer / relancer l'application | Taille conservee (par poste, `desktop-ui.json`) | | |
| 4 | Valeur hors 8–28 saisie | Ramenee aux bornes ou refusee | | |
| 5 | Autre poste (hub Linux) | Sa propre taille, non modifiee par ce reglage | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 9 — Navigation, « ← Reglages » et « Editer en YAML »

**Validation** : HUMAIN — **Issues** : #149

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Pour chaque rubrique, « Editer en YAML » (pied) | Editeur sur le bon fichier : `config.conf`, `env.yaml` du launcher, `backends.yaml` | | |
| 2 | Texte non modifie, « ← Reglages » | Retour direct a la rubrique d'origine | | |
| 3 | Texte modifie, « ← Reglages », accepter | Enregistre puis retour avec le changement visible | | |
| 4 | Texte modifie, « ← Reglages », abandon | Fichier inchange, retour | | |
| 5 | Texte invalide / conflit, « ← Reglages », accepter | Reste dans l'editeur avec le probleme | | |
| 6 | Changement non enregistre dans une rubrique, changer de rubrique | Avertissement ou conservation du brouillon, jamais de perte silencieuse | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 9 passent sur Windows natif
- [ ] Le jeton GitHub n'apparait jamais en clair ; `config.conf` en 0600
- [ ] Aucune ligne, commentaire ni forme non reconnue perdu dans `config.conf`
- [ ] Les entrees `hubs.<autre machine>` et d'un autre systeme ne sont jamais alterees

## Notes QA

Restaurer les sauvegardes `avant.*` en fin de session. Jeton factice uniquement.
