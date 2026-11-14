# Procedure de Test — Taille des caracteres des terminaux (Ctrl+molette) (#140)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA

## Prerequis

- [ ] Environnement : LOCAL — interface native Windows (Wails) ET mode `--web` (navigateur)
- [ ] Donnees : projets factices (`CL_ROOTS`), faux `claude` (`CL_CLAUDE_BIN`), tmux isole — jamais de vrais projets ni de vrai `claude`
- [ ] Un projet factice ouvert avec une team de 2 teammates minimum (plusieurs terminaux visibles)
- [ ] Acces : cache du launcher (`desktop-ui.json`) accessible en lecture ; une session tmux (hub Linux) attachee au meme projet

## Scenarios

### Scenario 1 — Ctrl+molette change la taille sur la zone des terminaux

**Validation** : HUMAIN — **Issues** : #140

**Objectif** : la taille des caracteres de tous les terminaux suit Ctrl+molette, sans zoom de la page.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Noter la taille initiale (defaut 13) | Texte lisible, taille par defaut | | |
| 2 | Ctrl+molette vers le haut sur la zone des terminaux | Tous les terminaux grossissent d'un pas ; message bref « Taille des caracteres : N » | | |
| 3 | Ctrl+molette vers le bas | Tous les terminaux reduisent ; message mis a jour | | |
| 4 | Verifier la page (menus, arbre, barre) | Aucun zoom de la page ; seul le texte des terminaux change | | |
| 5 | Repeter vers le haut jusqu'a la butee | Arret a 28 ; plus de changement au-dela | | |
| 6 | Repeter vers le bas jusqu'a la butee | Arret a 8 ; plus de changement au-dela | | |
| 7 | Chaque terminal : colonnes/lignes coherentes, pas de texte coupe ni de zone vide | Zone recalculee, rendu net sur tous les terminaux | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Arbre et cartes inchanges

**Validation** : HUMAIN — **Issues** : #140

**Objectif** : seule la zone des terminaux est concernee.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ctrl+molette sur la zone des terminaux (plusieurs pas) | Texte des terminaux change | | |
| 2 | Observer l'arbre des projets et les cartes (TEAM, etc.) | Police, hauteur de ligne et mise en page identiques a avant | | |
| 3 | Ctrl+molette sur l'arbre ou une carte | Pas de changement de taille des terminaux (hors zone des vues) | | |
| 4 | Molette seule (sans Ctrl) sur un terminal | Defilement habituel du terminal, taille inchangee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Memorisation apres relance

**Validation** : AUTO — **Issues** : #140

**Objectif** : la taille choisie est conservee dans `termFontSize` (desktop-ui.json) et reappliquee au lancement. Equivalent automatise : `ng2/ui/desktop/prefs_fontsize_test.go` (aller-retour, defaut, null) ; la relecture visuelle est couverte par le scenario 1.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Regler la taille a 18 par Ctrl+molette, attendre ~1 s | Ecriture differee effectuee | | |
| 2 | Lire `desktop-ui.json` du cache du launcher | Contient `"termFontSize": 18` | | |
| 3 | Fermer puis relancer l'interface | Terminaux ouverts directement a 18 | | |
| 4 | Regler a 8, relancer | Terminaux a 8 | | |
| 5 | Supprimer la cle `termFontSize` du fichier, relancer | Retour a 13 | | |
| 6 | Mettre `"termFontSize": 99` puis `2` dans le fichier, relancer | Valeur ramenee dans 8–28 (28 puis 8) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Resynchronisation tmux, vue depuis le hub Linux

**Validation** : HUMAIN — **Issues** : #140

**Objectif** : la taille de zone recalculee est renvoyee a tmux (`refresh-client -C`), la mise en page reste coherente cote hub.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Depuis le hub Linux, attacher la session tmux du projet (en lecture) | Panes visibles avec leur largeur/hauteur initiales | | |
| 2 | Dans l'interface, augmenter la taille (Ctrl+molette haut) | Cote tmux : nombre de colonnes/lignes du client diminue (`tmux list-clients -F '#{client_width}x#{client_height}'`), panes redimensionnes | | |
| 3 | Reduire la taille | Colonnes/lignes augmentent, panes redimensionnes | | |
| 4 | Verifier le contenu des panes cote hub | Aucun texte tronque, pas de pane vide ni de decalage | | |
| 5 | Basculer sur le hub Linux puis revenir | Mise en page identique, taille conservee | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Aucune touche retiree

**Validation** : HUMAIN — **Issues** : #140

**Objectif** : l'ecouteur de molette n'a pas pris de raccourci existant.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Dans un terminal : Ctrl+C, Ctrl+V, Ctrl+L, fleches, Tab | Comportement identique a avant | | |
| 2 | Raccourcis de l'interface (navigation, onglets, touches relayees vers tmux) | Tous fonctionnent comme avant | | |
| 3 | Ctrl+ « + » / « - » / « 0 » du navigateur (mode `--web`) | Comportement inchange (non detournes par la fonctionnalite) | | |
| 4 | Ctrl+molette sur Windows (WebView2) : pas de zoom de page parasite | Seule la taille des terminaux change | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 5 passent sous Windows natif et en `--web`
- [ ] Taille bornee a 8–28, defaut 13
- [ ] Aucune regression sur l'arbre, les cartes et les raccourcis

## Notes QA

Masquer tout `ghp_…` dans une capture d'ecran. Jamais de vrai projet ni de vrai `claude`.
