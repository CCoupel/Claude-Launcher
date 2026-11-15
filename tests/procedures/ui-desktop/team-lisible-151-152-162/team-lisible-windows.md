# Procedure de Test — Team lisible : carte TEAM, en-tetes de panes, onglet Teamleaders, Windows (#151, #152, #162)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, interface native (Wails), comparaison possible avec l'exe v6.1.0
- [ ] Donnees : projets factices avec registre d'agents (`agents.registry` de `project-config.json`) dont **un teammate au template en retard** ; faux `claude` ; team de 4 teammates dont un `cdp`, un actif, un inactif ; un pane ouvert a la main (sans agent) ; **plusieurs projets ouverts** (onglets) pour #162
- [ ] Maquettes : `docs/mockup/v6.2.0/ui/team_card__versions_entete.html` (A), `pane_header__etat_activite.html` (A), `tab_bar__teamleaders_separe.html` (B)
- [ ] Equivalents automatises : `TestCardsTeamHeadAndOdd`, `TestLayoutPanesStateAndBorderOptions`, `TestCycleTabOrderTeamleadersFirst`, `TestTeamleadersTabOutsideScrollAndNoOscillation` (`ng2/ui/desktop/team151_152_test.go`)

## Scenarios

### Scenario 1 — Carte TEAM : versions dans le titre, carte plus etroite qu'en v6.1.0

**Validation** : HUMAIN — **Issues** : #151

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet ouvert avec team, ouvrir la carte TEAM | Titre « Team · N · tpl vX · claude Y » (version cible du template, version de claude du leader) | | |
| 2 | Observer les lignes | Marque ●/○, nom, « modele · effort », ⚠ en derniere colonne, etat ; **plus de colonne de version** | | |
| 3 | Mesurer la largeur utile de la carte (comparer a l'exe v6.1.0 : environ 345 px) | Environ 300 px, nettement plus etroite qu'en v6.1.0 | | |
| 4 | Verifier le teammate au template en retard | ⚠ orange en bout de ligne ; infobulle : « template vA au lieu de vB » | | |
| 5 | Verifier un teammate dont la version de claude differe de celle du leader | ⚠ orange ; infobulle donne la version | | |
| 6 | Teammates conformes | Pas de ⚠ ; ● vert | | |
| 7 | Projet ferme (cartes sans team lancee) | Titre sans version de claude ; ○ pour les non lances ; ⚠ seulement pour les tags differents de la cible | | |
| 8 | Projet sans registre d'agents (version cible inconnue) | Pas de « tpl » dans le titre, aucun ⚠ de template | | |
| 9 | Non-regression v6.1.0 | Surbrillance du pane visible, double-clic cache ↔ visible, « + » du compagnon, ○ non demarre : inchanges | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — En-tetes de panes : bandeau par etat d'activite

**Validation** : HUMAIN — **Issues** : #152

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet ouvert, team avec un actif, un inactif, le leader, un `cdp`, un pane sans agent | En-tetes en bandeau plein : **vert** (texte sombre) pour l'actif, **gris sombre** pour l'inactif, clair neutre pour le leader, le `cdp` et le pane sans agent | | |
| 2 | Lire le texte des en-tetes | « nom · actif », « nom · inactif », « nom · leader » ; le pane sans agent garde sa commande seule | | |
| 3 | Faire changer l'etat d'un teammate (actif ↔ inactif) | L'en-tete change de couleur et de texte en moins de 2 s, sans relance | | |
| 4 | Cliquer dans un pane | Le pane qui a le clavier garde son soulignement orange | | |
| 5 | Comparer avec la maquette `pane_header__etat_activite.html` (A) | Couleurs et libelles conformes | | |
| 6 | Verifier l'alignement | L'en-tete dessine se superpose a la ligne de titre tmux : pas de ligne vide, pas de decalage, aucun pane coupe | | |
| 7 | Redimensionner la fenetre, ajouter puis retirer un teammate | Mise en page stable ; titres toujours alignes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Onglet Teamleaders isole

**Validation** : HUMAIN — **Issues** : #162

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir plusieurs projets (onglets) | Teamleaders en pastille a contour a gauche, **hors de la zone qui defile**, suivie d'un filet ; les onglets de projets defilent a part | | |
| 2 | Faire defiler les onglets de projets (molette, beaucoup d'onglets) | La pastille Teamleaders ne bouge pas | | |
| 3 | Cliquer sur Teamleaders | Vue Teamleaders affichee, pastille « allumee » | | |
| 4 | Ctrl+Tab et Ctrl+Maj+Tab | Ordre : Teamleaders, puis les projets dans l'ordre des onglets, circulaire | | |
| 5 | Comparer avec la maquette `tab_bar__teamleaders_separe.html` (B) | Conforme | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Reduction a l'icone, sans oscillation

**Validation** : HUMAIN — **Issues** : #162

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fenetre large, quelques onglets de projets | Pastille « ⌗ Teamleaders » complete | | |
| 2 | **Retrecir** la fenetre lentement jusqu'au debordement des onglets de projets | La pastille se reduit a l'icone « ⌗ » (infobulle « Teamleaders ») au moment du debordement | | |
| 3 | Continuer a retrecir, puis a la limite, **elargir** lentement la fenetre | La pastille reprend sa forme complete **seulement quand les onglets tiennent avec la pastille a sa largeur complete** | | |
| 4 | Rester immobile sur la largeur juste avant puis juste apres chaque seuil ; faire varier la largeur de 1 a 2 px autour du seuil | **Aucune oscillation** reduit/etendu (pas de clignotement) | | |
| 5 | Ouvrir ou fermer un onglet pres du seuil | Recalcul immediat, etat stable | | |
| 6 | Onglet actif cache par le defilement | L'onglet actif est ramene dans la zone visible | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Deux launchers sur un meme projet en conteneur (hub Linux et exe Windows)

**Validation** : HUMAIN — **Issues** : #152 — **Prerequis** : QUALIF avec un projet en conteneur ouvert par le hub Linux (qui a la main) et regarde depuis l'exe Windows

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub Linux a la main : changer l'etat d'un teammate | Les en-tetes de l'exe Windows (lecture seule) suivent le meme etat | | |
| 2 | L'exe Windows prend la main | Les options des panes (`@cl_state`, `@cl_name`) et de la fenetre (`pane-border-*`) sont reposees ; les deux interfaces affichent la meme chose | | |
| 3 | Reprendre la main depuis le hub Linux | Idem ; aucun pane decale ni ligne de titre en double | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Carte TEAM : versions dans le titre, ⚠ pour les ecarts, carte plus etroite qu'en v6.1.0, comportements v6.1.0 inchanges
- [ ] En-tetes de panes : vert actif, gris inactif, neutre leader/cdp/sans agent, mise a jour en moins de 2 s
- [ ] Teamleaders isole, ordre de Ctrl+Tab, reduction a l'icone sans oscillation
- [ ] Les deux launchers affichent le meme etat sur un projet en conteneur

## Notes QA

Risque connu : l'onglet Teamleaders pourrait osciller (reduit → les onglets tiennent → etendu → ils debordent) ; evite par la mesure a largeur complete — a verifier en redimensionnant (scenario 4).
