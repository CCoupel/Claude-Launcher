# Tests

> Autres guides : [Installation et lancement](installation.md) · [Configuration et projets en conteneur](configuration.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Dépannage](depannage.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Tests](#tests)

---

## Tests

`go test ./...` : comparaison de versions, resynchronisation de `init-project.md`, mise en page
(géométrie, somme de contrôle, ordre MRU, teintes comparées au bash v3), reprise après crash
(encodage des chemins, dernière conversation, team, teams épargnées), et une sonde
`inotify` qui s'ignore sur `9p`. Variables de test : `CL_ROOTS` (racines factices),
`CL_CLAUDE_BIN` (faux `claude`), `CL_HUB`, `CL_INNER_SOCKET`, `CL_OUTER_SOCKET`, `CL_SCRIPT`,
`CL_RAW_BASE` (serveur de fichiers bruts), `CL_API_BASE` (API GitHub), `CL_RELEASE_BASE`
(fichiers de release), `CL_FAKE_LATEST_TPL`, `CL_FAKE_LATEST_LAUNCHER`, `CL_FAKE_TPL_RELEASED`,
`XDG_CACHE_HOME`.

`scripts/ci.sh` (la CI) lance `go test -race ./...` sous Linux : il exige un compilateur C (`gcc`, cgo) et s'arrête avec un
message explicite s'il manque (`apt install gcc` / `dnf install gcc`). Il contrôle aussi la syntaxe du JS du frontend
(`node --check` sur `ng2/ui/desktop/frontend/*.js`, node requis). Il isole tmux : il crée un dossier dédié et court sous `/tmp` (en dur, `mktemp -d /tmp/clci.XXXXXX`,
indépendamment de `$TMPDIR` ; limite `sun_path` de 108 octets : le dossier du projet est trop long), et les tests
reçoivent ce dossier comme `TMUX_TMPDIR` et `TMPDIR`. Le script échoue si un socket de test `cl-*` reste dans ce dossier,
ou si un socket de test tombe dans `/tmp/tmux-<uid>` (garde limitée aux préfixes produits par les tests seulement :
`cl-test-*`, `cl-e2e-*`, `cl-ansible-secagent-*`, `cl-desk-{test,paste,seed,size,vis,r158}-*` ; les sessions du launcher
réel `cl`, `cl-hidden` et le serveur `claude-desk-ng2` sont ignorés), ou si un `index.lock` subsiste dans le dépôt après les tests (un git concurrent bref est toléré ; le verrou est celui de
`git rev-parse --git-path index.lock`, valable en worktree). Les lectures git du launcher ne prennent plus de verrou (`GIT_OPTIONAL_LOCKS=0`) et n'exécutent plus
`git describe --dirty` : le suffixe `-dirty` vient de `git status --porcelain`.

Durée de la CI : `scripts/ci.sh` tient en moins de 60 s hors étape image (moyenne de trois passes : 56 s
sur le conteneur de développement, contre 119 s avant les attentes sur évènement de `ui/desktop`). Le chemin critique est
`go test -race` de `ui/desktop` (~48 s) ; `go vet`, la compilation croisée Windows et `govulncheck` prennent quelques secondes
chacun. L'étape image (LOT-4) aura son propre budget. Procédure de mesure par étape :
`tests/procedures/ci/ci-60s-179/mesure-ci-60s.md`. Marge faible (~4 s) : une machine plus chargée peut dépasser 60 s.

Interface native (`ui/desktop`) : décodeur du mode contrôle de tmux (séquence réelle de tmux 3.6,
`%output` en octal, layouts, UTF-8 coupé), état et légende des cartes, appels, et un projet sur
l'hôte dans un tmux de test (ouverture, saisie, team de 7 rangée en 3 lignes, teintes,
détachement) ; `CL_DESK_SOCKET` choisit le tmux local de test, `CL_DESKTOP_TOKEN` fixe le jeton du
mode web. Bout en bout sur un vrai backend avec les variables des e2e de `core` :
`CL_E2E_POOL=<racine>,tcp://…` (docker, premier serveur) et `CL_E2E_K8S=…` (`TestE2EDesktopContainer` :
ouverture en conteneur, attache en mode contrôle, saisie dans le leader, fermeture).

`docs/maquette.html` est la maquette interactive d'origine de l'interface.
