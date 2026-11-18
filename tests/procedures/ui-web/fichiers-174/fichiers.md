# Procedure de Test — Fichiers des projets par l'interface, /files (#174)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/web-server.md` « Fichiers des projets par l'interface » — tests auto : `ng2/ui/desktop/files174_test.go`, `ng2/core/browserfiles174_test.go`
**Environnement** : QUALIF, pod `claude-launcher-0`, adresse interne (ingress interne, LAN) ; Chrome (claude-in-chrome) sur un poste du LAN

## Prerequis

- [ ] Image QUALIF 6.4.0.x déployée, un projet de QUALIF avec une maquette HTML dans `docs/mockup/`
- [ ] `CL_WEB_URL` de l'ingress interne connu (ex. `https://launcher-qualif.local`)

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./ui/desktop ./core -run 174` dans `ng2` | PASS : racines, `..`, liens, CSP sandbox, liste de dossier, Host / Sec-Fetch-Site, mode none sans session, appariement ; correspondance des sessions en cluster / hub inchangé |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Maquette ouverte dans Chrome par l'adresse interne

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Dans une session claude du pod : `echo $CL_BROWSER_PATH_MAP; echo $CL_BROWSER_HINT` | `[{"container":"/","host":"<CL_WEB_URL>/files","os":"url"}]` ; consigne visant `<CL_WEB_URL>/files` |
| 2 | Chrome, nouvel onglet : `<CL_WEB_URL>/files/<chemin interne du projet>/docs/mockup/<maquette>.html` | maquette affichée, scripts de la page bloqués (CSP sandbox) |
| 3 | Ouvrir un dossier (`…/docs/mockup/`) | liste des fichiers |
| 4 | Demander à la session d'ouvrir la maquette dans Chrome | la session ouvre l'adresse `/files/…` (jamais `file://`) |
| 5 | Appariement : `…/files/__claude-launcher__/pair?device=<id>&token=<jeton>` | page « ✓ navigateur du hub » avec le bon jeton ; refus avec un mauvais jeton |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Rien hors des racines

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `curl -k -i '<CL_WEB_URL>/files/etc/passwd'` | 404, jamais le contenu |
| 2 | `curl -k -i --path-as-is '<CL_WEB_URL>/files<racine>/../../etc/passwd'` | 404 |
| 3 | Dans le pod, créer un lien symbolique du projet vers `/etc` ; `curl …/files<projet>/lien/passwd` | 404 |
| 4 | `curl -k -i -X POST …/files<projet>/docs/mockup/<maquette>.html` | refusé (405), fichier intact |
| 5 | Dossier du coffre `/var/lib/claude-launcher/secrets` via `/files` | 404 |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Hub Linux et exe Windows inchangés

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Hub : une session ouvre une maquette dans Chrome | adresse `http://localhost:8765/…` comme en v6.3.0 |
| 2 | Hub en mode `token`, `/files` sans session | refusé ; avec session : servi |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Une maquette du projet s'ouvre dans Chrome par l'adresse interne via `/files`
- [ ] Aucun fichier hors racine servi ; CSP sandbox présente

## Notes QA

Risque connu : `/files` est de même origine que l'interface ; la CSP `sandbox` empêche une maquette d'appeler l'API (relecture security prévue).
