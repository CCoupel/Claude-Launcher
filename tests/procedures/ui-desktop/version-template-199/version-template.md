# Procedure de Test — Version du template juste et verification GitHub (#199)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/template-version.md` — tests auto : `ng2/core/tplversion199_test.go`

## Prerequis

- [ ] Hub Linux (coque tmux) et exe Windows (interface native) en 6.4.0.x, meme projet ouvert dans les deux
- [ ] Projet de test a la version de template **v5.11.1** (`.claude/.template-source.json`), cache du poste a **v5.11.0** (`~/.template-version` + `~/.template-commit`)
- [ ] Poste de test : sauvegarder puis restaurer `~/.template-version` / `~/.template-commit` en fin de recette

## Scenarios

### Scenario 1 — Projet plus recent que le cache : rien d'anormal

**Validation** : AUTO | **Issues** : #199

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Lancer `go test ./core -run '199'` dans `ng2` | `TestReadTemplate199`, `TestCarteTeamVersionDuProjet199` passent |
| 2 | Verifier que `~/.template-version` n'a pas change | contenu inchange |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Titre de la carte TEAM, projet v5.11.1 / cache v5.11.0

**Validation** : HUMAIN | **Issues** : #199

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Hub Linux : ouvrir la carte TEAM du projet | titre « tpl v5.11.1 » (version du projet), **sans ⚠** |
| 2 | Idem dans l'exe Windows | meme titre, sans ⚠ |
| 3 | Aucun agent a jour n'affiche « late » ni indicateur d'ecart | OK |
| 4 | Badge ⚠ du projet dans l'arbre, lancement du projet | pas de badge, **aucune question `/init-project`** |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — ⚠ orange et infobulle quand une version plus recente existe

**Validation** : HUMAIN | **Issues** : #199

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Poser `~/.template-version` a `v5.12.0` (cache plus recent que le projet) | — |
| 2 | Rafraichir la carte (F5) sur Linux et Windows | « tpl v5.11.1 » suivi d'un **⚠ orange** |
| 3 | Survoler le ⚠ (Windows) | infobulle « template du projet v5.11.1 — v5.12.0 disponible (/init-project au lancement) » |
| 4 | Lancer le projet | la question `/init-project` est proposee, cible v5.12.0 (jamais une cible egale ou plus ancienne) |
| 5 | Restaurer `v5.11.0` | le ⚠ disparait |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Alerte de quota GitHub (hub Linux et exe Windows)

**Validation** : HUMAIN | **Issues** : #199 (le comportement logique est teste en AUTO : `TestQuota403…199`)

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Simuler le quota : lancer le hub avec `CL_API_BASE` vers un faux serveur renvoyant 403 + `X-RateLimit-Remaining: 0` + `X-RateLimit-Reset` dans ~5 min | — |
| 2 | Sans appuyer sur F5, attendre la premiere verification | zone des annonces : « ⚠ vérification GitHub impossible (quota, reprise HH:MM) », orange, **non cliquable**, visible en continu |
| 3 | Appuyer sur F5 | meme message immediatement ; le faux serveur ne recoit **aucun** nouvel appel |
| 4 | Arreter le faux serveur (injoignable), supprimer `<CacheDir>/update-check.json`, F5 | « ⚠ GitHub injoignable » visible aussi hors F5 |
| 5 | Retablir un serveur sain, F5 apres l'heure de reprise | l'alerte disparait |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 5 — Annonce verte conservee, 304 et resultat partage

**Validation** : AUTO | **Issues** : #199

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./core -run 'Test304|TestResultatPartage|TestJeton|TestAnnonce' -v` | tous PASS |
| 2 | Verifier `<CacheDir>/update-check.json` apres une vraie verification | champs `checked_at`, `launcher`, `template` (tag, sha, etag) |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Aucun test automatise n'ecrit dans le vrai `~` (`TestGardeSansVraiHome199`)
- [ ] Linux et Windows affichent le meme titre TEAM et la meme alerte

## Notes QA

