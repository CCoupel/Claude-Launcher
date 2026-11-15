# Procedure de Test — Organisations de la fenetre d'un projet (leader en haut / a gauche), Windows (#153)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, interface native (Wails)
- [ ] Donnees : projets factices (au moins deux : A et B) avec faux `claude` ; une team de 6 teammates dont un `cdp` ; jamais de vrai projet
- [ ] Maquette : `docs/mockup/v6.2.0/ui/project_layout__organisations.html`
- [ ] Equivalents automatises : `TestProjectSettingsLayoutFields`, `TestProjectApplyLayout`, `TestNativeLayoutLeftAndSwitchWithoutRestart` (`ng2/ui/desktop/layout153_test.go`) ; `ng2/core/layout153_test.go`

## Scenarios

### Scenario 1 — Reglage global « Leader a gauche »

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages › **Launcher** : boite **« Mise en page des panes »** en **tete** de la page (pas dans « Teams et panes ») | Champs « Mise en page des panes » = « Leader en haut » (`LAYOUT_MODE=top`) et « Largeur du leader (leader a gauche, % ) » = 46 ; renvoi vers l'exception par projet | | |
| 2 | Choisir « Leader a gauche », enregistrer | Valeur enregistree (`LAYOUT_MODE=left`) ; un projet ouvert se remet en page sans relance (au tour suivant, quelques secondes) | | |
| 3 | Projet A ouvert avec 1 teammate | Leader a gauche sur toute la hauteur (≈ 46 % de la largeur) ; le teammate a droite sur toute la hauteur | | |
| 4 | Ajouter des teammates jusqu'a 4 | **Une colonne** a droite, teammates empiles de haut en bas | | |
| 5 | Ajouter un 5e puis un 6e teammate | **Deux colonnes egales** a droite ; la premiere recoit la moitie arrondie en haut (⌈n/2⌉ : 3 puis 3) | | |
| 6 | Verifier l'agent `cdp` | Empile sous le leader dans la colonne de gauche | | |
| 7 | Compter les lignes de titre (#152) | Chaque pane garde son en-tete ; rien ne deborde ni ne se chevauche | | |
| 8 | Changer « Largeur du leader (leader a gauche, %) » (20 a 80) | La largeur du leader suit ; hors bornes : probleme de validation a l'enregistrement | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Exception par projet et retour « Comme le reglage global »

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Global « Leader a gauche ». Clic droit sur le projet B › Reglages du projet : boite **« Mise en page des panes »** placee **avant** « Execution » (plus en bas de « Execution », sous les pastilles de couleur) | Champ « Organisation » avec la valeur « Comme le reglage global (Leader a gauche) » | | |
| 2 | Choisir « Leader en haut » pour B, enregistrer | B (ouvert) repasse en leader en haut sans relance ; A reste a gauche | | |
| 3 | Rouvrir les reglages de B | « Leader en haut » explicite ; `config.conf` contient `PROJECT_LAYOUTS` pour B ; **le reste du fichier est inchange** (commentaires, autres cles) | | |
| 4 | Choisir « Comme le reglage global » pour B | B suit de nouveau le global (leader a gauche) ; l'entree de B est retiree de `PROJECT_LAYOUTS` | | |
| 5 | Changer le global en « Leader en haut » | A et B (sans exception) suivent ; un projet avec exception garde la sienne | | |
| 6 | Changer l'organisation de B en meme temps que sa couleur, un seul enregistrement | Les deux sont enregistres ; un seul ecrit de `config.conf` ; message « enregistre » | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — 1 a 6 teammates, panes caches, changement a chaud

**Validation** : HUMAIN — **Issues** : #153

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Organisation « Leader a gauche », team de 6 teammates dont 4 inactifs (TEAM_HIDE_IDLE court) | Les inactifs se cachent ; seule la disposition des panes **visibles** est calculee (2 actifs : une colonne) | | |
| 2 | Double-clic sur un teammate cache dans la carte TEAM | Le pane revient ; la disposition suit (3 puis 4 teammates : toujours une colonne) | | |
| 3 | Atteindre 5 teammates visibles | Passage a deux colonnes | | |
| 4 | Redimensionner la fenetre | Disposition recalculee, leader ≈ 46 % de la largeur | | |
| 5 | Changer l'organisation pendant que des teammates travaillent | Remise en page sans perdre les terminaux ni le focus clavier | | |
| 6 | Projet avec un seul pane (pas de team) | Le leader occupe toute la fenetre dans les deux organisations | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Deux launchers : l'organisation est celle du launcher qui a la main

**Validation** : HUMAIN — **Issues** : #153 — **Prerequis** : hub Linux et exe Windows sur un meme projet en conteneur

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Hub Linux a la main avec LAYOUT_MODE=left ; exe Windows en lecture seule avec LAYOUT_MODE=top | L'exe Windows affiche la disposition recue (gauche) sans la modifier | | |
| 2 | L'exe Windows prend la main | La disposition devient celle de **sa** configuration (haut) | | |
| 3 | Le hub reprend la main | Disposition de sa configuration (gauche) ; aucune option tmux nouvelle | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Bouton de bascule dans la barre d'onglets

**Validation** : HUMAIN — **Issues** : #153 (source : `_work/reports/dev-frontend-lot5-fix-v6.2.0.md`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Plusieurs projets ouverts (onglets). Observer la barre d'onglets | Un bouton **tout a droite** de la barre, **hors de la zone qui defile** (ne bouge pas quand on fait defiler les onglets) | | |
| 2 | Onglet d'un projet en « Leader en haut » : lire l'icone et l'info-bulle | Icone = organisation courante (rectangle a bandeau haut) ; info-bulle « Mise en page : leader en haut — cliquer pour leader a gauche » | | |
| 3 | Cliquer le bouton | Le projet **actif** passe en « Leader a gauche » **sans relance** (quelques secondes) ; l'icone devient colonne gauche et l'info-bulle s'inverse immediatement | | |
| 4 | Cliquer a nouveau | Retour a « Leader en haut » ; icone et info-bulle suivent | | |
| 5 | Verifier que les autres projets ne sont pas touches | Seul le projet de l'onglet actif change | | |
| 6 | Apres une bascule, ouvrir la page du projet (clic droit › Reglages du projet) | « Mise en page des panes » indique l'organisation choisie comme **exception** (plus « Comme le reglage global ») ; `config.conf` contient l'entree `PROJECT_LAYOUTS` du projet | | |
| 7 | Fermer puis relancer la fenetre native, reouvrir le projet | L'exception est conservee ; l'icone du bouton reflete l'organisation (relue au plus toutes les 5 s) | | |
| 8 | Modifier l'organisation dans la page du projet (ou « Comme le reglage global ») | L'icone du bouton suit sans clic (au plus 5 s) | | |
| 9 | Selectionner l'onglet **Teamleaders** | Le bouton est **grise** (aucun projet actif) ; aucun effet au clic | | |
| 10 | Simuler un refus (fichier `config.conf` en lecture seule) puis cliquer | L'icone est restauree et un message d'erreur s'affiche | | |
| 11 | Dans la page web (meme poste) | Le bouton fonctionne de la meme facon (aucune garde dans un client web) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Le bouton ne provoque pas d'oscillation de l'onglet Teamleaders

**Validation** : HUMAIN — **Issues** : #153, #162 (equivalent automatise du principe : `TestTeamleadersTabOutsideScrollAndNoOscillation`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Fenetre large, quelques onglets de projets | Pastille « Teamleaders » complete ; bouton de bascule visible a droite | | |
| 2 | **Retrecir** lentement la fenetre jusqu'au debordement des onglets | La pastille se reduit a l'icone « ⌗ » ; **la largeur du bouton de bascule est comptee** dans la mesure (les onglets ne sont jamais caches derriere lui) | | |
| 3 | **Elargir** lentement, puis rester immobile autour de chaque seuil (1 a 2 px) | **Aucune oscillation** reduit/etendu, aucun clignotement | | |
| 4 | Ouvrir / fermer un onglet pres du seuil | Recalcul immediat, etat stable ; le bouton reste a droite | | |
| 5 | Basculer l'organisation puis redimensionner | Pas de changement de comportement | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] « Leader a gauche » : leader ≈ 46 % a gauche sur toute la hauteur, teammates a droite (1 colonne jusqu'a 4, 2 colonnes des 5)
- [ ] Reglage global + exception par projet + « Comme le reglage global » ; seule la cle `PROJECT_LAYOUTS` est ecrite
- [ ] Pris en compte sans relance ; seuls les panes visibles sont disposes ; lignes de titre (#152) respectees
- [ ] Option trouvable : boite « Mise en page des panes » en tete de Reglages › Launcher et avant « Execution » dans la page du projet
- [ ] Bouton de bascule dans la barre d'onglets : bascule le projet actif sans relance, icone = organisation courante, exception enregistree et conservee apres relance, grise sur Teamleaders, sans oscillation de l'onglet Teamleaders
- [ ] Le comportement « Leader en haut » (defaut) est inchange par rapport a la v6.1.0

## Notes QA

Si tmux refuse la chaine de mise en page « left », le launcher retombe sur « top » avec un message : a verifier si reproductible (non provoquable de facon fiable en test automatique).
