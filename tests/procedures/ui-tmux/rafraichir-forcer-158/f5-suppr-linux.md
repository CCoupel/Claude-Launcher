# Procedure de Test — F5 et Suppr du navigateur, coque Linux (#158)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — hub Linux reel (navigateur tmux), jamais la session `claude-hub` de production
- [ ] Donnees : projets factices ; un projet en conteneur jetable dont on peut supprimer le conteneur ou pod
- [ ] Acces : moteur (podman/docker/kubectl) pour supprimer le conteneur a la main

## Scenarios

### Scenario 1 — F5 relit le projet selectionne

**Validation** : HUMAIN — **Issues** : #158 (equivalent automatise du message : `TestF5RereadsSelectedProject`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Selectionner un projet, appuyer sur F5 | Pied de navigateur « ⟳ … » avec le nom du projet, puis le resultat de la relecture (conteneur, session, fenetres, git, diagnostic) | | |
| 2 | Verifier l'effet historique | Recherche de mises a jour lancee, annonces masquees reapparues | | |
| 3 | Supprimer a la main le conteneur d'un projet affiche ouvert, puis F5 sur ce projet | La pastille et l'etat refletent la disparition sans attendre le cycle normal | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Suppr propose la fermeture forcee

**Validation** : HUMAIN — **Issues** : #158 (equivalent automatise : `TestDeleteProposesForceCloseWhenContainerGone`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet ouvert dont le conteneur a ete supprime : Suppr | Question de fermeture forcee (o/N) en pied de navigateur, sans attente de /exit | | |
| 2 | Repondre N | Rien n'est change | | |
| 3 | Suppr puis o | Fenetre retiree, session nettoyee, projet ouvrable de nouveau (Entree) ; aucun fichier du projet supprime | | |
| 4 | Projet sain ouvert : Suppr | Fermeture normale inchangee | | |
| 5 | Projet pilote par un autre launcher : Suppr | Refus « prendre la main d'abord » (equivalent : `TestDeleteNeverForcesWhenOtherLauncherAlive`) | | |
| 6 | Verifier l'aide (`?`) | F5 et Suppr decrits ; aucune nouvelle touche | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Backend injoignable : Suppr propose la fermeture forcee avec avertissement

**Validation** : HUMAIN — **Issues** : #158 — **Prerequis** : projet en conteneur ouvert (fenetre du hub)
(equivalent automatise : `TestForceCloseWarnsWhenBackendReadFailed` dans `ng2/ui/tmux/refresh158_major_test.go`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Couper l'acces au backend du projet (moteur distant injoignable) ; le conteneur continue de tourner | Projet toujours affiche ouvert | | |
| 2 | F5 sur le projet | Pied : « ⟳ … » puis resultat mentionnant « backend injoignable » ; la fenetre du projet est conservee | | |
| 3 | Suppr | Question de fermeture forcee avec l'avertissement « backend injoignable : le projet tourne peut-etre encore » (o/N) | | |
| 4 | Repondre o | Fenetre et etat local nettoyes ; **aucun arret du conteneur** demande (verifier cote moteur apres retablissement de l'acces) | | |
| 5 | Retablir l'acces, F5 | Le conteneur tourne toujours ; etat relu coherent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Le projet revit entre la question et la reponse

**Validation** : HUMAIN — **Issues** : #158 — (equivalent automatise : `TestForceCloseConfirmRefusedWhenProjectRevives`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet dont le conteneur a disparu : Suppr (question affichee, ne pas repondre) | Question de fermeture forcee | | |
| 2 | Faire revivre le projet (conteneur recree, session de nouveau vivante) puis repondre o | Refus avec message ; aucune fenetre fermee, aucune session tuee, aucune marque de sortie propre | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] F5 relit le projet selectionne, avec « ⟳ … » puis le resultat
- [ ] Suppr pose la question de fermeture forcee seulement quand elle est proposable
- [ ] Backend injoignable : proposee avec avertissement, nettoyage local seul, aucun arret du conteneur
- [ ] Un projet qui revit entre la question et la reponse n'est jamais ferme de force

## Notes QA

La coque n'a ni bandeau ni boite de dialogue : c'est l'equivalent, pas la meme forme.
