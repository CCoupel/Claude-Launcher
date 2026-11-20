# Procedure de Test — Nouveau projet depuis un depot Git ou le template (#215)

**Version** : 6.6.0
**Date** : 2026-11-20
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 (`create` source `git` / `template`, `newJob`, `createCancel`, `initProposal`). Equivalents automatises (faux `git`, jamais de reseau) : `ng2/core/clone215_test.go`, `ng2/core/clone_test.go`, `ng2/ui/desktop/create215_test.go`. Automatise, donc a ne pas rejouer a la main : URL refusees, jeton absent de argv / `.git/config` / messages, nettoyage complet sur echec ou annulation, une seule creation a la fois.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x, puis **pod du launcher** du cluster QUALIF (interface web)
- [ ] Donnees : un **depot Git de test** que tu possedes (petit, public ou prive) ; pour le prive : `GITHUB_TOKEN` renseigne dans `config.conf` (jamais colle dans l'URL) ; base factice (`C:\cl-test\PROJETS`, puis base 9P `\\wsl.localhost\<distro>\…`)
- [ ] Acces : ecriture sur la base ; aucun vrai projet de travail

## Scenarios

### Scenario 1 — Clone reel depuis l'exe Windows (base locale)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet > source « Depot Git », URL HTTPS du depot de test, categorie neuve `CLONES`, nom `essai-215` | Ligne d'authentification : jeton present / absent, sans jamais afficher sa valeur | | |
| 2 | Valider | Progression visible (etape + pourcentage) et fenetre reactive pendant le clonage | | |
| 3 | Attendre la fin | Projet ouvert ; dossier `…\CLONES\essai-215` contient le depot ; categorie creee | | |
| 4 | Ouvrir `.git\config` du projet dans un editeur | Une seule origine = l'URL, **aucun jeton**, aucun credential helper | | |
| 5 | Chercher le jeton dans les messages de la fenetre (bandeau, journal) | Introuvable | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Echec d'authentification et annulation (exe Windows)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Retirer le jeton de `config.conf`, creer depuis un depot **prive**, categorie neuve `ECHEC` | Message lisible « authentification refusee (HTTPS, jeton GITHUB_TOKEN de config.conf) » + dossier supprime | | |
| 2 | Verifier le disque | Ni le dossier du projet ni la categorie `ECHEC` ; rien de nouveau dans l'arbre | | |
| 3 | Lancer un gros clonage puis cliquer « Annuler » | Etat « annulee — dossier supprime », processus git absent du Gestionnaire des taches, dossier et categorie neuve retires | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Clone sur base 9P (`\\wsl.localhost\…`)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cloner le depot de test dans la base 9P | Clonage termine sans erreur de chemin ou de permission ; duree acceptable | | |
| 2 | Ouvrir le projet | Ouverture normale, arbre git lisible | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Clone depuis le pod QUALIF (cle ssh du coffre)

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Interface web du pod, source Git en HTTPS (jeton) | Clonage reussi sur le volume partage ; projet ouvert | | |
| 2 | Source Git en `git@github.com:…` avec cle et `known_hosts` du coffre pour le backend | Clonage reussi ; hote inconnu → message « known_hosts du coffre a completer » | | |
| 3 | Dans le pod : `ps` pendant un clonage | Aucun jeton dans les arguments des processus | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Source Template : proposition de /init-project

**Validation** : HUMAIN — **Issues** : #215

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Nouveau projet > source « Template », nom `tpl-215` | Projet cree et ouvert ; `/init-project` n'est **pas** lance d'office | | |
| 2 | Observer la fenetre | Bandeau « Lancer /init-project ? » apparait une seule fois | | |
| 3 | Cliquer « Plus tard » | Bandeau retire ; ne revient pas tant que le launcher tourne | | |
| 4 | Relancer le launcher, rouvrir le projet | Bandeau propose de nouveau (marqueur `.claude-launcher/init-proposed` conserve) | | |
| 5 | Cliquer « Lancer » | `/init-project` tape dans le leader et execute ; marqueur supprime ; bandeau ne revient plus | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Le jeton n'apparait nulle part (argv, `.git/config`, messages, journaux)
- [ ] Aucun dossier ni categorie residuels apres un echec ou une annulation
- [ ] `/init-project` jamais lance sans reponse de l'utilisateur

## Notes QA

Supprimer les dossiers de test a la fin. Ne jamais utiliser un jeton ayant des droits d'ecriture sur un depot de production.
