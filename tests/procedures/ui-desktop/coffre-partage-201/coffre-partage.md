# Procedure de Test — Coffre des secrets partage (#201)

**Version** : 6.5.0
**Date** : 2026-11-19
**Testeur** : QA + utilisateur (scenarios HUMAIN)

Reference : `contracts/backends-remote.md` « Coffre des secrets », `.claude/workflow/plan-v6.5.0.md` (LOT-2).
Automatise (jouer en premier) : `cd ng2 && go test -race -count=1 -run '201' ./core/` ; migration : ajouter `-tags secretsmigration` tant que le tag n'est pas retire.

## Prerequis

- [ ] Environnement : exe Windows reel (partage `\\wsl.localhost\…`), hub Linux, pod QUALIF `claude-qualif`
- [ ] Donnees : un backend ssh declare ; `CL_ROOT_BASE` identique sur les trois ; PVC `<release>-secrets` v6.4.0 contenant au moins un backend
- [ ] Acces : cle de test (jamais une cle de production)

## Scenarios

### Scenario 1 — Garde automatisee des controles de chemin

**Validation** : AUTO — **Issues** : #201

Commande ci-dessus. Attendu : 0 echec (lien pendant relatif/absolu, boucle, coffre sous un projet, non 0700, `kubeconfig.path`, `/files`, migration).

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer les tests `201` | PASS | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Cle saisie dans l'exe Windows, utilisee par le hub et le pod

**Validation** : HUMAIN — **Issues** : #201 (jugement : saisie dans la fenetre, connexion reelle sur 3 environnements)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Exe Windows : Reglages > Backends > ssh, saisir cle privee + publique | « cle privee enregistree · SHA256:… », cle privee jamais reaffichee | | |
| 2 | Linux : `ls -l <CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/<backend>/` | dossier `0700` (`drwx------`), fichiers `0600`, meme uid que le proprietaire du partage | | |
| 3 | Hub Linux : « Tester » le backend | connexion ssh OK avec cette cle | | |
| 4 | Pod QUALIF : « Tester » le backend depuis l'interface web du pod | connexion OK, sans nouvelle saisie | | |
| 5 | Journal : `<CacheDir>/secrets-journal.jsonl` | une entree par ecriture, instance indiquee, aucun contenu de cle | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Droits vus depuis Linux apres ecriture Windows

**Validation** : HUMAIN — **Issues** : #201 (verifie sur partage 9P reel, non simulable)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Exe Windows : supprimer puis recreer la cle du backend | pas d'erreur de droits | | |
| 2 | Linux : `stat -c '%a %U' <coffre> <coffre>/<backend> <coffre>/<backend>/id` | `700`, `700`, `600`, uid du partage | | |
| 3 | Hub Linux : relancer, ouvrir Reglages | coffre accepte, aucun refus « droits trop larges » | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — Migration du PVC sur claude-qualif

**Validation** : HUMAIN — **Issues** : #201 (cluster reel)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Deployer 6.5.0 sur `claude-qualif` avec `secrets.legacy.enabled` | pod demarre, `CL_SECRETS_LEGACY_DIR` pose, `CL_SECRETS_DIR` absent | | |
| 2 | Verifier le coffre commun sur le partage | backends de l'ancien PVC copies (`0600`), journal `action: migrate` | | |
| 3 | Ancien PVC | contenu conserve (non supprime) | | |
| 4 | Backend present des deux cotes avec contenu different | global conserve, ecart signale UNE fois dans l'interface | | |
| 5 | Redemarrer le pod | aucun nouveau signalement, aucun doublon au journal | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Scenarios 1 a 4 PASS
- [ ] `/files` du pod ne sert ni le coffre ni `PRIVATE/` (essai par URL directe)
- [ ] Aucun secret dans `backends.yaml`, le cache ni l'API

## Notes QA

Ne jamais utiliser de cle de production. Scenarios 2 a 4 : validation humaine (VALID-HUMAINE).
