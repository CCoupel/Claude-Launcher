# Procedure de Test — Image du launcher du cluster (#167, #168)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : QA

Reference : `contracts/launcher-env.md` (« Image de conteneur », « Launcher dans un cluster »), `deploy/image/` (Dockerfile, entrypoint.sh, build.sh). Equivalents automatises sans image : `ng2/core/cluster_fakekubectl_test.go` (faux kubectl), `ng2/cluster_server_test.go` (serveur en mode cluster), `ng2/serve_sigterm_test.go` (SIGTERM, jeton conserve).

## Prerequis

- [ ] Environnement : conteneur de developpement avec `buildah` (stockage `vfs`, sudo) — ou QUALIF (StatefulSet de test dans `claude-ng2`)
- [ ] Donnees : image locale `localhost/claude-launcher:<version>` construite par `deploy/image/build.sh <version>` ; un dossier de racine **factice** (jamais la racine reelle) contenant `PROJETS/TEST/alpha` (depot git vide)
- [ ] Acces : `buildah run` ou `kubectl -n claude-ng2` ; aucun vrai projet ni vrai `claude`
- [ ] Conteneur de travail : `ctr=$(sudo buildah from localhost/claude-launcher:<version>)` ; **dans le conteneur de developpement, `buildah run` exige `--isolation chroot`** (cgroups en lecture seule) : `sudo buildah run --isolation chroot "$ctr" -- <commande>` (`<ctr>` ci-dessous). Supprimer ensuite : `sudo buildah rm "$ctr"`

## Scenarios

### Scenario 1 — Image : utilisateur, version, outils

**Validation** : AUTO — **Issues** : #167

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `buildah inspect --format '{{.OCIv1.Config.User}}' <image>` | `1000:1000` (non root) | | |
| 2 | `buildah run --isolation chroot <ctr> -- id -u` | `1000` | | |
| 3 | `buildah run --isolation chroot <ctr> -- claude-launcher-ng2 version` | `Claude-Launcher-NG2 <tag de l'image>` ; label `org.opencontainers.image.version` identique | | |
| 4 | `buildah run --isolation chroot <ctr> -- sh -c 'for c in git gh kubectl bash; do command -v "$c" \|\| echo "ABSENT $c"; done'` (**un nom a la fois** : `command -v` de `sh`/dash n'en accepte qu'un) | quatre chemins, aucune ligne `ABSENT` | | |
| 4b | `buildah run --isolation chroot <ctr> -- sh -c 'for c in tmux claude; do command -v "$c" && echo "PRESENT (anormal) $c"; done; true'` | aucune sortie : `tmux` et `claude` absents | | |
| 5 | `CL_UPDATE_MODE` dans l'environnement de l'image | `notify` | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Point d'entree : refus sans racine, interface servie avec un dossier monte

**Validation** : AUTO — **Issues** : #167

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer l'image sans `CL_ROOT_BASE` | Refus, message clair, code 64 | | |
| 2 | `CL_ROOT_BASE=/nulle-part` | Refus, code 66 | | |
| 3 | Racine factice montee (`-v <racine>:/racine`, `CL_ROOT_BASE=/racine`, `CL_IN_CLUSTER=0`), attendre 10 s | `/healthz` et `/readyz` : 200 `ok`, sans cookie ni information | | |
| 4 | `ls <racine>/.config/claude-launcher-ng2/config.conf` apres 10 s | **Present** (constat infra : non cree en 7 s — voir test `TestClusterServerModeServesAndCreatesConfigUnderRoot`) | | |
| 5 | Sortie du conteneur (`buildah logs` / `kubectl logs`) | Aucun jeton d'acces en clair (`?t=<jeton>` seulement en espace reserve) ; message d'arret par SIGTERM adapte au mode serveur | | |
| 6 | `~/.config` du conteneur | Absent (tout est sous la racine de base) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Arret propre et jeton conserve

**Validation** : AUTO — **Issues** : #167

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `kill -TERM` du processus du conteneur (ou `kubectl delete pod`) | Arret en < 10 s, code 0, serveur ferme | | |
| 2 | Relancer sur la meme racine | Meme jeton (fichier `desktop-web-token` conserve), sessions precedentes non reprises | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 4 — QUALIF : pod de test dans `claude-ng2`

**Validation** : HUMAIN — **Issues** : #167, #168 — **Poste** : cluster (QUALIF LOT-4)

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Deployer l'image dans un StatefulSet de test (racine de QUALIF montee) | Pod `Ready` (sonde `/readyz`) | | |
| 2 | Reglages > Backends > Tester le backend k8s implicite | Etapes : cluster joignable (ServiceAccount du pod), droits un par un, PVC `Bound` `ReadWriteMany`, PVC monte dans le pod du launcher | | |
| 3 | Retirer un droit au Role puis retester | « droit manquant : <verbe> <ressource> » + conseil `helm upgrade` | | |
| 4 | Annonce de version (mode notify) | « ↑ NG vX disponible · mise a jour par l'image », sans bouton d'installation | | |
| 5 | Bloquer la sortie reseau du pod (NetworkPolicy de test) puis ouvrir la connexion / la consommation | Message clair evoquant les sorties reseau (claude.ai, GitHub), pas une erreur opaque | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Image non root, version du tag, outils attendus, ni `tmux` ni `claude`
- [ ] Interface servie avec un dossier monte, sondes sans information, `config.conf` sous la racine
- [ ] Arret propre, jeton conserve, aucun jeton dans les journaux
- [ ] Diagnostic k8s depuis le pod : chaque droit et chaque montage nommes

## Notes QA

Construire l'image d'essai : `deploy/image/build.sh <vX.Y.Z.a>` (a chaud ~15 s, a froid ~40 s) ; supprimer l'image de travail ensuite (stockage vfs).
