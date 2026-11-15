# Procedure de Test — Rafraichir un projet et Forcer la fermeture, Windows (#158)

**Version** : 6.2.0
**Date** : 2026-11-15
**Testeur** : QA

## Prerequis

- [ ] Environnement : QUALIF — poste Windows, interface native (Wails)
- [ ] Donnees : projets factices ; **un projet en conteneur k8s jetable** (pod supprimable a la main) ; faux `claude` — jamais de vrai projet
- [ ] Un projet factice ouvert dans un onglet ; un second projet ferme ; cartes ◈ Etat affichees
- [ ] Acces : `kubectl` (ou equivalent) pour supprimer le pod ; acces a `projects.json` et au dossier du projet (lecture)

## Scenarios

### Scenario 1 — Menu du projet : entrees « Rafraichir » (F5) et « Forcer la fermeture… »

**Validation** : HUMAIN — **Issues** : #158

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Clic droit sur un projet ouvert sain | Entrees « Rafraichir » (F5) presente et active ; « Forcer la fermeture… » absente ou inactive ; « Fermer » active | | |
| 2 | Clic droit sur un projet ferme sain | « Rafraichir » active ; « Forcer la fermeture… » inactive | | |
| 3 | Comparer avec la maquette `docs/mockup/v6.2.0/ui/project_menu__rafraichir_forcer.html` | Libelles, ordre et raccourci conformes | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Rafraichir : bandeau d'etapes dans les cartes

**Validation** : HUMAIN — **Issues** : #158

**Objectif** : un bandeau d'etapes est visible dans les cartes pendant l'operation ; il disparait a la fin, ou reste avec le message en cas d'erreur.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Selectionner le projet (cartes affichees), choisir « Rafraichir » (ou F5 sur le projet) | Bandeau en tete des cartes : une coche par etape (conteneur/pod, session, git, GitHub, template, consommation) ; la ligne de l'arbre ne change pas | | |
| 2 | Observer la fin sans erreur | Les etapes passent a ✓, le bandeau disparait ; cartes a jour ; le cadre des cartes ne « saute » pas (hauteur fixe 1-2 lignes) | | |
| 3 | Selectionner un autre projet pendant l'operation | Pas de bandeau sur ses cartes ; la fin est signalee en pied de volet | | |
| 4 | Relancer « Rafraichir » pendant qu'un rafraichissement du meme projet tourne | Refus avec message (un seul a la fois) | | |
| 5 | Couper le reseau (GitHub injoignable), « Rafraichir » | Bandeau en rouge avec l'etape en echec et son message, reste jusqu'a × ou au rafraichissement suivant ; l'operation se termine en 30 s au plus | | |
| 6 | Rafraichir plusieurs fois de suite (< delai minimal de l'API) | Consommation relue depuis le cache sans erreur 429 | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Pod supprime a la main : Rafraichir puis fermeture forcee

**Validation** : HUMAIN — **Issues** : #158 — **Prerequis** : projet k8s jetable

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Ouvrir le projet k8s (onglet), puis supprimer son pod a la main | Onglet encore affiche (pas de relecture automatique immediate) | | |
| 2 | « Rafraichir » | Etape conteneur/pod en alerte ; message « pod absent — projet ferme » ; onglet retire, projet affiche ferme | | |
| 3 | Rouvrir le projet puis recommencer la suppression du pod ; cette fois choisir « Forcer la fermeture… » sans rafraichir | Boite de confirmation : titre, raison (« son pod k8s n'existe plus »), liste de ce qui sera nettoye (onglet, session…), boutons Annuler / Forcer la fermeture | | |
| 4 | Echap, puis rouvrir la boite et cliquer Annuler | Rien n'est change | | |
| 5 | Pendant la boite : taper des touches | Aucune touche n'atteint les terminaux | | |
| 6 | Confirmer « Forcer la fermeture » | Onglet retire ; message de fin ; projet ouvrable de nouveau (double-clic) | | |
| 7 | Verifier le dossier du projet et `projects.json` | Aucun fichier supprime ; `projects.json` inchange ; conversation toujours dans `/resume` ; pas de proposition de reprise ↺ | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Forcer la fermeture : cas refuses

**Validation** : HUMAIN — **Issues** : #158

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet sain ouvert (pod present) : menu | « Forcer la fermeture… » non proposee | | |
| 2 | Projet ouvert depuis un second launcher (hub Linux) : menu sur Windows | Refus « prendre la main d'abord » ; rien n'est nettoye | | |
| 3 | « Fermer » (normale) sur un projet sain | Inchange par rapport a la v6.1.0 | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Projet factice local : session tuee, rafraichi = ferme, fermeture forcee = ouvrable

**Validation** : AUTO (equivalents : `TestRefreshProjectRemovesTabOfKilledSession`, `TestForceCloseGoneSessionThenReopen`, `TestForceCloseRefusedWhenOtherLauncherAlive` dans `ng2/ui/desktop/refresh158_test.go`) — **Issues** : #158

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer les trois tests | PASS | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 6 — Backend injoignable : fermeture forcee avec avertissement, nettoyage local seul

**Validation** : HUMAIN — **Issues** : #158 — **Prerequis** : projet k8s jetable ouvert (onglet), pod en marche
(equivalents automatises : `TestForceCloseWarnsWhenBackendReadFailed`, `TestRefreshProjectKeepsTabWhenBackendReadFails`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Couper l'acces au backend du projet (reseau du cluster, VPN ou contexte kubectl invalide) ; le pod, lui, continue de tourner | Le projet reste affiche ouvert | | |
| 2 | « Rafraichir » | L'etape conteneur/pod passe en erreur (bandeau rouge) ; **l'onglet est conserve** (jamais detache sur une lecture echouee) | | |
| 3 | Menu › « Forcer la fermeture… » | Proposee ; la boite affiche l'avertissement « backend injoignable : le projet tourne peut-etre encore » | | |
| 4 | Lire la liste de ce qui sera nettoye | Etat local seulement (onglet, session locale, marque de sortie propre) ; **aucun arret de conteneur ou de pod** annonce | | |
| 5 | Annuler | Rien n'est change ; onglet toujours present | | |
| 6 | Rouvrir la boite et confirmer | Onglet retire, nettoyage local fait ; message de fin sans arret du conteneur | | |
| 7 | Retablir l'acces au backend, Rafraichir | Le pod est toujours en marche (aucun arret demande par la fermeture forcee) ; le projet est affiche ouvert/ouvrable selon l'etat reel | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 7 — Le projet revit entre la question et la confirmation

**Validation** : HUMAIN — **Issues** : #158 — (equivalents automatises : `TestForceCloseConfirmRefusedWhenHostSessionRevives`, `TestForceCloseConfirmRefusedWhenLiveSessionAppears`)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Projet dont le pod a disparu : « Forcer la fermeture… », boite de confirmation ouverte (ne pas confirmer) | Question affichee | | |
| 2 | Pendant que la boite est ouverte, faire revivre le projet (le pod est recree, ou le projet est rouvert depuis un autre moyen) | | | |
| 3 | Confirmer | **Refus** avec un message (le projet est de nouveau vivant) ; ni session tuee, ni conteneur arrete, ni marque de sortie propre ; la reprise ↺ reste proposee | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 8 — Etape GitHub du rafraichissement

**Validation** : HUMAIN — **Issues** : #158 — **Prerequis** : projet k8s (ou conteneur) ouvert ; `gh` installe et connecte sur le poste Windows pour l'etape 1 (`gh auth status` OK) ; acces pour desinstaller/renommer `gh` et se deconnecter (`gh auth logout`) pour les cas legitimes

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Cas nominal : `gh` connecte, projet avec remote GitHub. « Rafraichir » | Etape GitHub (milestone et issues) **verte** ; message final sans erreur GitHub | | |
| 2 | `gh` absent (le renommer ou le sortir du PATH), « Rafraichir » | Etape GitHub **verte** ; la raison est dite dans le message final (ex. « gh absent ») ; aucun bandeau rouge | | |
| 3 | `gh` present mais non connecte (`gh auth logout`), « Rafraichir » | Etape GitHub **verte** ; message final : non connecte | | |
| 4 | Projet sans remote GitHub (depot local ou remote non GitHub), « Rafraichir » | Etape GitHub **verte** ; message final : pas de depot GitHub | | |
| 5 | Retablir `gh` et la connexion, « Rafraichir » | Retour au cas nominal (etape verte, milestone relu) | | |
| 6 | Si l'etape est **rouge** dans l'un des cas ci-dessus (ou en nominal) : relever le message exact du bandeau et du message final (cause renvoyee par `gh`), puis lancer `gh auth status` dans un terminal du poste Windows et noter sa sortie complete | Message et sortie de `gh auth status` consignes dans « Resultat Obtenu » et les Notes QA ; anomalie a ouvrir | | |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 9 — « dubious ownership » : projet du partage ouvert depuis Windows

**Validation** : HUMAIN — **Issues** : #158 — **Prerequis** : projet git du partage (hote Linux/WSL) possede par un autre utilisateur que le compte Windows ; `git` installe sur le poste Windows ; un lecteur reseau mappe (ex. `Z:`) sur le meme partage, si l'utilisateur en a un

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Noter, avant tout : `git config --global --get-all safe.directory` et `git --version` | Sorties consignees (reference) | | |
| 2 | Ouvrir le projet par son chemin `\\wsl.localhost\<distribution>\…` (racine des projets du launcher), selectionner le projet, « Rafraichir » | Etape GitHub **verte** ; la carte milestone est affichee ; aucun message « dubious ownership » ; **aucune commande `git config` lancee** par l'utilisateur ni par le launcher | | |
| 3 | Si l'utilisateur a un lecteur mappe : configurer la racine des projets sur `Z:\…`, relancer le launcher, « Rafraichir » le meme projet | Meme resultat qu'a l'etape 2 : etape GitHub verte, carte milestone affichee | | |
| 4 | Relancer `git config --global --get-all safe.directory` | Sortie **identique** a l'etape 1 (le launcher n'a ajoute aucune entree) | | |
| 5 | Si l'etape GitHub est **rouge** avec « dubious ownership » : relever le message exact du bandeau et du message final (il doit contenir la commande d'aide pour declarer le depot sur), et noter `git --version` | Message et version consignes dans « Resultat Obtenu » et les Notes QA ; anomalie a ouvrir | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Rafraichir relit sans attendre les caches ; l'indicateur disparait toujours, y compris sur erreur
- [ ] Un projet dont le pod a disparu apparait ferme apres Rafraichir ; son onglet est retire
- [ ] Forcer la fermeture : proposee seulement si pertinente, refusee si un autre launcher pilote, projet ouvrable ensuite
- [ ] Aucun fichier du projet supprime ; `projects.json` intact
- [ ] Backend injoignable : Rafraichir conserve l'onglet ; fermeture forcee proposee avec avertissement, nettoyage local seul, aucun arret du conteneur
- [ ] Un projet qui revit entre la question et la confirmation n'est jamais ferme de force
- [ ] Etape GitHub : verte en nominal et dans les cas legitimes (gh absent, non connecte, pas de remote GitHub), avec la raison dans le message final ; rouge seulement sur une vraie erreur, dont le message exact est releve
- [ ] Projet du partage ouvert depuis Windows (`\\\\wsl.localhost\\…`, lecteur mappe) : etape GitHub verte, milestone affiche, `safe.directory` global inchange ; si « dubious ownership », le message contient la commande d'aide

## Notes QA

Risque connu : le bandeau decale les cartes a l'ouverture/fermeture (hauteur fixe 1-2 lignes, sans animation).
