# Procedure de Test — Menu contextuel et reglages d'un projet (#148, LOT-3.2)

**Version** : 6.1.0
**Date** : 2026-11-14
**Testeur** : QA

Reference : `contracts/desktop-dispatch.md` (projectSettings, projectApply), maquette `docs/mockup/v6.1.0/ui/config_ui__interface_structuree.html` (A — menu contextuel, reglages du projet).

## Prerequis

- [ ] Environnement : QUALIF — exe Windows natif
- [ ] Donnees : au moins 2 projets **factices** (jamais de vrais projets) : un ferme, un ouvert (faux `claude`) ; `<projet>/.claude-launcher/env.yaml` avec commentaires (en tete, entre blocs, en fin de ligne) ; `env-template.yaml` du template present (apports du template) ; `env.yaml` du launcher avec au moins un paquet commun ; palette `PROJECT_COLORS` connue
- [ ] Sauvegardes avant : `env.yaml` du projet, `config.conf`, `projects.json` (copies `avant.*`)

## Scenarios

### Scenario 1 — Menu contextuel : chaque entree

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Clic droit sur un projet de l'arbre | Menu : Ouvrir (Entree), Fermer (Suppr), « ⚙ Reglages du projet… », « Changer de backend… » (F2), « Environnement du conteneur… », « Couleur… », « Editer env.yaml en YAML » ; le projet clique est selectionne | | |
| 2 | « Ouvrir » | Le projet s'ouvre comme par Entree | | |
| 3 | « Fermer » sur un projet ouvert | Question de fermeture habituelle comme par Suppr ; « Fermer » absent ou grise sur un projet ferme | | |
| 4 | « ⚙ Reglages du projet… » | Fenetre Reglages ouverte sur la rubrique du projet | | |
| 5 | « Changer de backend… » | Meme fenetre sur la section Execution (backend) | | |
| 6 | « Environnement du conteneur… » | Section Environnement (env.yaml du projet, apercu fusionne) | | |
| 7 | « Couleur… » | Section Couleur (palette) | | |
| 8 | « Editer env.yaml en YAML » | Editeur YAML sur `env.yaml` du projet ; « ← Reglages » ramene a la rubrique | | |
| 9 | Echap ou clic hors du menu | Menu ferme, aucune action | | |
| 10 | Clic droit sur une categorie ou sur du vide | Pas de menu projet (ou menu sans entrees projet) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Ouverture rapide et ecran Reglages du projet

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir les reglages d'un projet (menu) | Fenetre visible en moins d'une demi-seconde, « Chargement… » puis contenu ; interface non figee | | |
| 2 | Contenu | Sections Execution (backend, image de base, backend prefere), Environnement du conteneur, Couleur ; fichier concerne en pied (`env.yaml du projet`) | | |
| 3 | Plusieurs projets a la suite | Chaque ecran montre les valeurs du bon projet (pas celles du precedent) | | |
| 4 | Projet sans `env.yaml` | Section Environnement vide, creation a l'enregistrement, pas d'erreur | | |
| 5 | Projet avec `env.yaml` en erreur de syntaxe | Probleme affiche en pied, pas de plantage, « Editer en YAML » propose | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Backend et image : projet ferme seulement

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet **ferme** : changer le backend (liste des backends de `backends.yaml`) et l'image de base, Enregistrer | Enregistre ; `projects.json` mis a jour ; relire l'ecran : valeurs conservees | | |
| 2 | Ouvrir ce projet | Il s'ouvre sur le nouveau backend | | |
| 3 | Projet **ouvert** : ouvrir ses reglages | Backend et image affiches en lecture seule ou refus a l'enregistrement avec message « fermer d'abord » | | |
| 4 | Projet ouvert : tenter de changer le backend / l'image | Refus avec message ; `projects.json` inchange | | |
| 5 | Projet ouvert : changer la couleur ou l'env.yaml | Permis (seuls backend et image sont bloques) | | |
| 6 | F2 sur un projet (comportement existant) | Inchange : meme selection de backend que depuis le menu, memes refus sur projet ouvert | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — env.yaml du projet : enregistrement et apercu fusionne

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ajouter un paquet, modifier un `setup`, ajouter un dossier `local` et un montage ; Enregistrer | Enregistre ; copie `.bak` ; `diff avant.env.yaml env.yaml` : seules les valeurs modifiees changent, commentaires et ordre des cles conserves (bruit de mise en forme tolere : lignes vides, alignement) | | |
| 2 | Apercu fusionne | Lecture seule ; chaque ligne marquee par son origine : template, launcher (env.yaml commun), projet | | |
| 3 | Un paquet present dans le template ET dans le projet | Affiche une seule fois (fusion sans doublon), origine coherente | | |
| 4 | Retirer un paquet du projet | Disparait de l'apercu sauf s'il vient du template / du launcher (reste marque ainsi) | | |
| 5 | Valeur invalide (ex. `image` vide, montage mal forme) | Probleme en pied, Enregistrer bloque, fichier inchange | | |
| 6 | Modifier `env.yaml` a la main pendant que l'ecran est ouvert, puis Enregistrer | Conflit signale (recharger / ecraser) | | |
| 7 | Backend prefere (`prefer`) | Enregistre dans `env.yaml`, propose aux autres postes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Couleur du projet

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Choisir une couleur de la palette, Enregistrer | Pastille/onglet du projet a la nouvelle couleur sans relance ; `config.conf` → `PROJECT_COLORS` mis a jour, commentaires du fichier conserves | | |
| 2 | Le coque Linux (hub) du meme poste | Meme couleur apres rechargement de la config | | |
| 3 | Couleur hors palette (edition manuelle de `config.conf`) | Refusee ou ramenee a la palette avec message | | |
| 4 | Retirer la couleur (valeur par defaut) | Cle retiree de `PROJECT_COLORS`, couleur automatique de retour | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 6 — Retour « ← Reglages » et « Editer en YAML » depuis un projet

**Validation** : HUMAIN — **Issues** : #148

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Menu → « Editer env.yaml en YAML », texte non modifie, « ← Reglages » | Retour direct a l'ecran Reglages du projet | | |
| 2 | Texte modifie, « ← Reglages », accepter l'enregistrement | Enregistre puis retour avec le changement visible dans l'apercu fusionne | | |
| 3 | Texte modifie, « ← Reglages », abandon | Fichier inchange, retour | | |
| 4 | Texte invalide, « ← Reglages », accepter | Reste dans l'editeur avec le probleme ; fichier inchange | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

## Criteres de Validation

- [ ] Scenarios 1 a 6 passent sur Windows natif
- [ ] Backend/image jamais modifies sur un projet ouvert ; aucun commentaire perdu dans `env.yaml` ni `config.conf`
- [ ] F2, Entree et Suppr inchanges ; aucune regression sur l'arbre

## Notes QA

Restaurer les sauvegardes `avant.*` en fin de session. Masquer tout `ghp_…` dans une capture d'ecran. Jamais de vrai projet.
