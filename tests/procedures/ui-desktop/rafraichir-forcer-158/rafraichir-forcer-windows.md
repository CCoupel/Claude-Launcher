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

## Criteres de Validation

- [ ] Rafraichir relit sans attendre les caches ; l'indicateur disparait toujours, y compris sur erreur
- [ ] Un projet dont le pod a disparu apparait ferme apres Rafraichir ; son onglet est retire
- [ ] Forcer la fermeture : proposee seulement si pertinente, refusee si un autre launcher pilote, projet ouvrable ensuite
- [ ] Aucun fichier du projet supprime ; `projects.json` intact

## Notes QA

Risque connu : le bandeau decale les cartes a l'ouverture/fermeture (hauteur fixe 1-2 lignes, sans animation).
