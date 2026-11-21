# Procedure de Test — Effet de chaque option (#226) et element cache avec bouton oeil (#227)

**Version** : 6.6.0
**Date** : 2026-11-21
**Testeur** : utilisateur (validation humaine, label `VALID-HUMAINE`)

Reference : `contracts/desktop-dispatch.md` § v6.6.0 — LOT-4 (`effect` / `effectHow`, `apply`, `secretReveal`) ; maquettes retenues `docs/mockup/v6.6.0/ui/config_ui__effet_des_options.html` (A revisee : bulle) et `config_ui__element_cache.html` (A : oeil dans le champ) ; decisions : revelation **refusee a distance**, re-masquage a **30 s**, au clic, au changement de rubrique et a la fermeture.

## Prerequis

- [ ] Environnement : QUALIF — exe natif **Windows** 6.6.0.x ; pour le scenario 5 : acces **web distant** (adresse publique `desktop_web.url`, QUALIF cluster) et acces web local (`127.0.0.1`)
- [ ] Donnees : un jeton GitHub **factice** (`ghp_FAKE_TEST_…`) dans `config.conf` (`GITHUB_TOKEN`) ; jamais un vrai jeton ; au moins un projet factice `alpha` **ouvert**
- [ ] Acces : sauvegarde de `config.conf` avant de commencer

## Scenarios

### Scenario 1 — Pastille d'effet et bulle (exe Windows)

**Validation** : HUMAIN — **Issues** : #226

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages › Configuration | Chaque option a une **pastille d'effet** a droite du champ (immediat / projet / claude / launcher) | | |
| 2 | Survoler la pastille de « Desactiver la souris de claude » | **Bulle** : pourquoi (recopie dans settings.json, lue au demarrage de claude), quoi faire, procedure de la coque, lien « Rafraichir » | | |
| 3 | Naviguer au **clavier** (Tab) jusqu'a la pastille | Meme bulle au focus ; le lien d'action est atteignable au clavier | | |
| 4 | Appuyer sur la pastille (tactile / clic) puis **Echap** (ou second appui) | La bulle s'ouvre, puis se ferme | | |
| 5 | Comparer une option « immediat » (`TEAM_HIDE_IDLE`) et une option « launcher » (`GITHUB_DIR`) | Immediat : aucun lien d'action ; launcher : lien « Relancer le launcher » | | |
| 6 | Textes | Francais, clairs, sans balisage affiche ni coupure | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Rappel apres enregistrement (exe Windows)

**Validation** : HUMAIN — **Issues** : #226

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `alpha` ouvert ; changer la **souris** puis Enregistrer | Rappel limite a l'option modifiee, avec « Rafraichir » (projet ouvert concerne : `alpha`) ; **rien** n'est rafraichi automatiquement | | |
| 2 | Cliquer « Rafraichir » | Le projet est rafraichi (#158) ; la souris suit | | |
| 3 | Changer `TEAM_HIDE_IDLE` seulement | Pas de rappel de rafraichissement (effet immediat) | | |
| 4 | Changer `GITHUB_DIR` | Rappel « Relancer le launcher » (#219) | | |
| 5 | N'ajouter qu'un commentaire dans l'editeur texte | Aucun rappel (aucune option n'a change) | | |
| 6 | Re-enregistrer une valeur identique | Aucun rappel | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Oeil du jeton (exe Windows)

**Validation** : HUMAIN — **Issues** : #227

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Reglages › GitHub, jeton defini | Valeur **masquee** (points) ; bouton oeil dans le champ ; « (defini) » | | |
| 2 | Clic sur l'oeil | Jeton factice affiche en **texte** avec compte a rebours « masque dans N s » | | |
| 3 | Selectionner / copier la valeur, la coller dans un editeur | Copiable ; ce n'est pas un champ de saisie | | |
| 4 | Clic de nouveau sur l'oeil | Re-masque immediatement | | |
| 5 | Reveler, attendre 30 s sans rien faire | Re-masque tout seul | | |
| 6 | Reveler puis changer de rubrique, revenir | Re-masque (aucune valeur gardee) | | |
| 7 | Reveler puis fermer les Reglages, rouvrir | Re-masque | | |
| 8 | Reveler 11 fois en moins d'une minute | La 11e est refusee avec un message (limite), sans valeur | | |
| 9 | Jeton non defini | Oeil grise ; « (non defini) » | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Le jeton ne fuit nulle part (exe Windows)

**Validation** : HUMAIN — **Issues** : #227

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Apres plusieurs revelations, rechercher le jeton factice dans le journal des secrets (`secrets-journal.jsonl`, dossier de cache) | Lignes « revele : GITHUB_TOKEN, fenetre native, heure » ; **jamais** la valeur, ni longueur, ni empreinte | | |
| 2 | Rechercher le jeton dans les journaux / la console du launcher | Absent | | |
| 3 | Aucun autre ecran ne montre le jeton (cartes, instantane, aide) | Absent | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 5 — Acces web : local admis, distant refuse

**Validation** : HUMAIN — **Issues** : #227

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Navigateur sur l'acces web **local** (`http://127.0.0.1:<port>`, session valide), Reglages › GitHub, oeil | Jeton revele comme en natif | | |
| 2 | Navigateur sur l'**adresse publique** (QUALIF cluster, https), memes Reglages | Oeil **grise** avec l'indication « indisponible a distance — afficher depuis la fenetre native ou l'acces local » | | |
| 3 | Depuis l'adresse publique, forcer l'appel (outils du navigateur / script) | Refus « indisponible depuis un acces distant », jamais la valeur | | |
| 4 | Acces par une adresse du reseau local (ecoute LAN, hote non boucle locale) | Meme refus | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Chaque option affiche son effet ; la bulle est lisible, accessible au clavier et se ferme
- [ ] Le rappel apres enregistrement est limite aux options modifiees, sans action automatique
- [ ] Le jeton n'est visible que sur demande, 30 s au plus, jamais copie en champ ni journalise
- [ ] A distance, la revelation est impossible

## Notes QA

Restaurer `config.conf` sauvegarde ; supprimer le jeton factice.
