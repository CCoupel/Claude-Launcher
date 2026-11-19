# Claude-Launcher-NG2 (v6)

Binaire Go qui pilote plusieurs projets Claude Code en parallèle. Il a trois façons de tourner, qui partagent le même cœur
(`core`) : une **coque tmux** sous Linux et macOS, une **application native** sous Windows, et un **launcher dans un cluster
Kubernetes**, piloté par une interface web derrière un Ingress interne. Un projet peut tourner sur l'hôte, ou dans un
conteneur local ou distant (podman, docker, ssh, Kubernetes).

NG2 a ses propres noms et peut tourner à côté d'un launcher v4 : binaire `claude-launcher-ng2`, coque `claude-ui-ng2`,
serveur tmux intérieur `claude-ng2` (session `claude-hub-ng2`), config `~/.config/claude-launcher-ng2/`, cache
`~/.cache/claude-launcher-ng2/`. **Passer de v4 à v5/v6 (ou revenir) uniquement avec tous les projets fermés.** De v5 à v6, la
mise à jour se fait seule.

Vue d'ensemble, site et installation rapide : [README du repo](../README.md).

## Trois façons de le faire tourner

| Où | Interface | Guide |
|----|-----------|-------|
| Linux, macOS | coque tmux : navigateur de projets à gauche, tmux à droite | [Installation et lancement](docs/installation.md) |
| Windows | application native `claude-launcher-ng2.exe` (Wails + WebView2) | [Launcher Windows natif](docs/windows.md) |
| Cluster Kubernetes (Helm) | interface web servie par un pod, derrière un Ingress interne | [Déploiement dans un cluster](docs/kubernetes.md) |

## Quel guide pour quoi

| Je veux | Guide |
|---------|-------|
| installer, lancer, mettre à jour le binaire ou l'exe | [installation.md](docs/installation.md) |
| configurer : `config.conf`, `backends.yaml`, projets en conteneur, backends distants, kubeconfig, navigateur | [configuration.md](docs/configuration.md) |
| utiliser l'application Windows (fenêtre native, correspondance des chemins, proxy) | [windows.md](docs/windows.md) |
| déployer le launcher dans un cluster (prérequis, valeurs, sécurité, mise à jour, purge, schéma) | [kubernetes.md](docs/kubernetes.md) |
| comprendre le coffre des secrets (emplacement partagé, `PRIVATE/` des projets, migration, risque du stockage en clair) | [secrets.md](docs/secrets.md) |
| utiliser l'interface (onglets, cartes, raccourcis, interface web) | [interface.md](docs/interface.md) |
| comprendre les versions, les mises à jour et le template | [versions.md](docs/versions.md) |
| comprendre le code (structure, détection des changements) | [architecture.md](docs/architecture.md) |
| résoudre un problème | [depannage.md](docs/depannage.md) |
| lancer les tests et mesurer | [tests.md](docs/tests.md) |

## Versions et migration

- **Version courante et historique** : [CHANGELOG.md](../CHANGELOG.md). Les changements de contrats sont dans
  [`contracts/CHANGELOG.md`](../contracts/CHANGELOG.md).
- **De v5 à v6** : la mise à jour se fait seule, voir [Changement de version majeure](docs/configuration.md#changement-de-version-majeure).
- **Passer un hub à un launcher de cluster, ou en ajouter un à côté, sans perte** : [MIGRATION.md §14](MIGRATION.md#14-passer-dun-hub-à-un-launcher-de-cluster-ou-en-ajouter-un-à-côté).
- **Projets sur un stockage partagé** : [MIGRATION.md](MIGRATION.md).
