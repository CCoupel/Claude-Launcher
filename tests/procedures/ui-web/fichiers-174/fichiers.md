# Procedure de Test — Fichiers des projets par l'interface, /files/<projet>/… (#174)

**Version** : 6.4.0
**Testeur** : QA
**Contrat** : `contracts/web-server.md` « Fichiers des projets par l'interface » — tests auto : `ng2/ui/desktop/files174_test.go`, `ng2/core/browserfiles174_test.go`
**Environnement** : QUALIF, pod `claude-launcher-0`, adresse interne (ingress interne, LAN) ; Chrome (claude-in-chrome) sur un poste du LAN

## Prerequis

- [ ] Image QUALIF 6.4.0.x déployée, le projet factice `qualif-factice-1` (nom du launcher `<catégorie>-<projet>`) avec la maquette `_work/mockup/essai-174/index.html`
- [ ] `CL_WEB_URL` de l'ingress interne connu (ex. `https://launcher-qualif.local`)

## Scenarios

### Scenario 1 — Contrôles automatisés

**Validation** : AUTO | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `go test ./ui/desktop ./core -run 174` dans `ng2` | PASS : forme projet, projet inconnu / ancienne forme / racine en 404, noms `__` réservés, sortie de la racine du projet (`..`, liens), CSP sandbox, liste de dossier, Host / Sec-Fetch, exclusions, modes token et none, appariement ; correspondance par session en cluster, hub inchangé |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 2 — Maquette ouverte dans Chrome par l'adresse interne

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Dans une session claude du projet `qualif-factice-1` : `echo $CL_BROWSER_PATH_MAP; echo $CL_BROWSER_HINT` | `[{"container":"<racine du projet vue par la session>","host":"<CL_WEB_URL>/files/qualif-factice-1","os":"url"}]` ; consigne visant `<CL_WEB_URL>/files/qualif-factice-1/<chemin relatif>` |
| 2 | Chrome, nouvel onglet : `<CL_WEB_URL>/files/qualif-factice-1/_work/mockup/essai-174/index.html` | maquette affichée (ses scripts s'exécutent en origine opaque, sans accès à l'interface) |
| 3 | Ouvrir un dossier : `<CL_WEB_URL>/files/qualif-factice-1/_work/mockup/essai-174/` | liste des fichiers **sans chemin interne** (ni dans la page, ni dans l'adresse) |
| 4 | Demander à la session d'ouvrir la maquette dans Chrome | la session ouvre `/files/qualif-factice-1/…` (jamais `file://`, jamais un chemin interne) |
| 5 | Dans une session d'un AUTRE projet : un fichier de `qualif-factice-1` | aucune URL proposée (la correspondance est celle du projet de la session) |
| 6 | Appariement : `…/files/__claude-launcher__/pair?device=<id>&token=<jeton>` | page « ✓ navigateur du hub » avec le bon jeton ; refus avec un mauvais jeton |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 3 — Ce qui n'est jamais servi

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | `curl -k -i '<CL_WEB_URL>/files/'` et `…/files` | **404**, sans liste ni nom de projet |
| 2 | `curl -k -i '<CL_WEB_URL>/files/projet-inconnu/x'` | 404 |
| 3 | Ancienne forme : `curl -k -i '<CL_WEB_URL>/files/share/MFS/ROOT/CLAUDE-QUALIF/PROJETS/QUALIF/factice-1/_work/mockup/essai-174/index.html'` | **404** |
| 4 | `curl -k -i --path-as-is '<CL_WEB_URL>/files/qualif-factice-1/../qualif-factice-2/…'` et `…/qualif-factice-1/../../etc/passwd` | 404 |
| 5 | Dans le pod, lien symbolique du projet vers `/etc` puis vers un AUTRE projet ; `curl …/files/qualif-factice-1/lien/passwd` | 404 dans les deux cas |
| 6 | `curl -k -i -X POST …/files/qualif-factice-1/_work/mockup/essai-174/index.html` | refusé (405), fichier intact |
| 7 | `.env`, `.git/config`, `PRIVATE/`, `_work/rapports…` (hors `_work/mockup`) du projet | 404 |
| 8 | Dossier du coffre / cache du launcher via `/files/qualif-factice-1/…` (même sous une racine partagée) | 404 |
| 9 | `curl -k -i '<CL_WEB_URL>/files/__claude-launcher__/x'` | 404 (nom réservé) |

**Verdict** : [ ] PASS  [ ] FAIL

### Scenario 4 — Hub Linux et exe Windows inchangés

**Validation** : HUMAIN | **Issues** : #174

| Etape | Action | Resultat attendu |
|-------|--------|------------------|
| 1 | Hub : une session ouvre une maquette dans Chrome | adresse `http://localhost:8765/…` comme en v6.3.0 |
| 2 | Hub en mode `token`, `/files` sans session | refusé ; avec session : servi |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Une maquette du projet s'ouvre dans Chrome par `/files/<projet>/<chemin relatif>` ; aucun chemin interne dans une URL ni une liste
- [ ] Aucun fichier hors racine servi ; CSP sandbox présente

## Notes QA

Forme unique `/files/<projet>/…` (décision utilisateur 2026-11-18) ; la CSP `sandbox` (origine opaque) empêche une maquette d'appeler l'API ou de lire un cookie de l'interface. Risque accepté : une maquette peut envoyer ce qu'elle lit vers Internet (pas de `connect-src`).
