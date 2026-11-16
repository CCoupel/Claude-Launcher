# Contrat — variables d'environnement et options du launcher (v6.3.0)

> Interface entre le launcher (`ng2`), l'image de conteneur (`deploy/image/`), le chart Helm (`deploy/helm/`) et les
> procédures de déploiement. Ce fichier ne décrit que ce qui est ajouté ou modifié à partir de v6.3.0.
> Le backend peut ajuster un détail pour une contrainte technique, en le notant dans `contracts/CHANGELOG.md`.
> Règle générale : **sans aucune de ces variables, le comportement est celui de la v6.2.0** (hub Linux et exe Windows).

## Racine de base (#165)

| Élément | Valeur |
|---------|--------|
| Variable | `CL_ROOT_BASE=<dossier absolu>` |
| Option | `--root-base <dossier>` ou `--root-base=<dossier>`, acceptée à n'importe quelle position des arguments |
| Priorité entre les deux | l'option l'emporte sur la variable |

- L'option est retirée des arguments avant l'analyse de la sous-commande, puis reportée dans `CL_ROOT_BASE` du
  processus : toute relance (mise à jour, `relaunch`) la conserve.
- Les sous-commandes lancées **par tmux** (`nav`, `state`, `ctr-attach`, `layout-watch`, `mirror`, `relayout`)
  reçoivent `CL_ROOT_BASE` dans leur préfixe de commande, comme `CL_HUB` aujourd'hui (elles héritent de
  l'environnement du serveur tmux, pas de celui du launcher).

**Dossier de configuration — ordre de priorité**

1. `CL_CONFIG_DIR` (inchangé) ;
2. `<root_base>/.config/claude-launcher-ng2` — retenu **même si `config.conf` n'y existe pas encore** ;
3. `<dossier du binaire>/.config/claude-launcher-ng2` s'il contient un `config.conf` (mode portable, inchangé) ;
4. dossier personnel (inchangé).

**`config.conf` créé sous `root_base`** (première exécution) : la racine de projets et `CLAUDE_CONFIG_DIR` y sont
proposés relativement à `root_base` (`<root_base>/PROJETS`, `<root_base>/.claude`), jamais en `$HOME/GITHUB`.

**`CLAUDE_CONFIG_DIR` sous la racine** : sans valeur dans `config.conf` (fichier neuf **ou existant** qui ne le pose
pas), `CLAUDE_CONFIG_DIR` vaut `<root_base>/.claude` (choix assumé : la configuration de claude suit la racine).
Une valeur posée dans `config.conf`, ou un `CLAUDE_CONFIG_DIR` déjà défini au lancement, l'emporte. La racine est
nettoyée (barre oblique finale, `.` et `..` retirés) avant tout usage.

**Option sans valeur** : `--root-base` sans argument, `--root-base=` ou `--root-base ""` est une erreur explicite
(« --root-base : valeur manquante »), code de sortie non nul, rien créé ; la variable n'est pas effacée en silence.

**Racine invalide** (absente, pas un dossier, illisible) : message d'erreur nommant le chemin et sa source
(`CL_ROOT_BASE` ou `--root-base`), code de sortie non nul. Aucune configuration n'est créée ailleurs.

**`claude-launcher-ng2 backends`** — nouvelle section en tête :

```
Racine de base : /data/cl (CL_ROOT_BASE)        # ou « (--root-base) », ou « aucune »
  configuration   : /data/cl/.config/claude-launcher-ng2
  cache           : /data/cl/.cache/claude-launcher-ng2/cluster     (CL_CACHE_DIR)
  config de claude: /data/cl/.claude
  projets         : /data/cl/PROJETS
```

## Identité et état (#169)

| Variable | Rôle | Défaut |
|----------|------|--------|
| `CL_HUB_NAME` | Nom du launcher, à la place du nom de la machine : clé `hubs.<nom>` de `backends.yaml`, identité de propriétaire des sessions (`LauncherID`), hôte admis des serveurs web | nom de la machine |
| `CL_CACHE_DIR` | Dossier de cache du launcher, utilisé tel quel (jetons, préférences, navigateur apparié, état par projet, cache des milestones) | `$XDG_CACHE_HOME` ou `~/.cache`, suivi de `claude-launcher-ng2` (inchangé) |

- `CL_HUB_NAME` : `^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`. Valeur invalide : erreur au démarrage.
- Deux launchers qui partagent une racine doivent porter des noms différents (#176). **Garde** : avec `CL_HUB_NAME`
  posé, un témoin `hub-<nom>.presence` est tenu dans le dossier d'état (`CL_CACHE_DIR`, renouvelé toutes les 3 s,
  repris après 10 s ou si son processus est mort sur la même machine) ; un second launcher du même nom refuse de
  démarrer (`desktop`), message nommant le launcher déjà actif, code 1. Sans `CL_HUB_NAME`, aucun contrôle.
- Le nom réel de la machine reste celui des espaces de PID des teams (écrits par Claude Code) ; `CL_HUB_NAME` ne
  change que l'identité du launcher.
- **Cache par défaut sous une racine de base** : il reste sous le dossier personnel (`$XDG_CACHE_HOME` ou
  `~/.cache`) — c'est un état par launcher, jamais partagé. Seul `CL_CACHE_DIR` le déplace. La section « Racine de
  base » de `backends` affiche la ligne « cache » avec sa source (`CL_CACHE_DIR` ou « défaut »).
- **Écritures partagées (#176)** : `projects.json`, le `settings.json` de `CLAUDE_CONFIG_DIR`, l'`env.yaml` d'un
  projet sont modifiés sous un verrou court (`<fichier>.lock`, création exclusive, repris après 20 s) puis écrits
  atomiquement (temporaire à nom unique puis renommage) ; `config.conf` et `env.yaml` neufs sont créés en
  exclusivité.
  Précisions (revue cycle 1) : le verrou porte un identifiant unique (pid + aléa) et sa libération ne supprime que
  le verrou qui le contient ; un verrou périmé (60 s) est repris sous un petit verrou de reprise
  (`<fichier>.reclaim`) après revérification, jamais un verrou vivant ; la péremption se mesure sur la date du
  fichier vue par le serveur de fichiers : des écarts d'horloge entre launchers doivent rester très inférieurs à
  60 s. Même schéma pour le témoin de présence (battement qui recrée un témoin supprimé et signale une place
  prise). Sous Windows, le renommage atomique est réessayé (violation de partage) et `ProcessAlive` rend toujours
  vrai : après un plantage, le témoin n'est repris qu'une fois périmé (10 s).
- **`settings.json`** : la liste des clés posées par le launcher est tenue **par launcher**
  (`claude-launcher/settings-env-<nom>.json` avec `CL_HUB_NAME`, `settings-env.json` sans, comme en v6.2.0) : un
  launcher ne retire que les clés qu'il a posées. Même clé posée par deux launchers avec des valeurs différentes :
  le dernier écrivain gagne.
- `CL_CACHE_DIR` : créé s'il manque. Tous les chemins de cache passent par lui, `milestones-v2.json` compris.
- Convention du chart : `CL_CACHE_DIR=<rootBase>/.cache/claude-launcher-ng2/<hubName>` (état sur le volume partagé,
  un dossier par nom de hub).

## Launcher dans un cluster (#168, #169)

| Variable | Rôle |
|----------|------|
| `CL_IN_CLUSTER` | `1` ou `0` : force la détection. Absente : détection automatique |
| `CL_ALLOW_HOST_BACKEND` | `1` : autorise le backend `host` dans un launcher en cluster (refusé par défaut) |
| `CL_K8S_VOLUMES` | `<racine>=<pvc>[,<racine>=<pvc>…]` : volumes du backend k8s **implicite** (sans `backends.yaml`) |

**Détection automatique** : `KUBERNETES_SERVICE_HOST` non vide **et**
`/var/run/secrets/kubernetes.io/serviceaccount/token` lisible. Les tests fixent `CL_IN_CLUSTER=0` dans leur bac à
sable : le conteneur de développement est lui-même un pod.

**Effets quand le launcher est en cluster**

- Backend k8s **sans `context`** : `kubectl` est appelé sans `--context` et sans `KUBECONFIG` ; il utilise le
  ServiceAccount du pod.
- Backend k8s **sans `namespace`** : namespace du pod (fichier
  `/var/run/secrets/kubernetes.io/serviceaccount/namespace`), et non plus `default`.
- **Sans `backends.yaml`** : un backend implicite `k8s` (type `k8s`, namespace du pod, volumes de `CL_K8S_VOLUMES`)
  est proposé, comme podman et docker détectés aujourd'hui.
- Backend `host` : refusé, message « backend host refusé dans le pod du launcher (claude n'y est pas installé) »,
  sauf `CL_ALLOW_HOST_BACKEND=1`. Aucun outil n'est réclamé pour un backend indisponible.
- Jeton de l'interface en `desktop --web` : `CL_DESKTOP_TOKEN` s'il est fourni, sinon le jeton **conservé** dans
  `<cache>/desktop-web-token` (créé au premier démarrage, 0600). Il ne change plus à chaque redémarrage du pod.
  Hors cluster, `desktop --web` tire toujours un nouveau jeton (inchangé).

**Diagnostic et « Tester » d'un backend k8s en cluster** — étapes, dans l'ordre :

| # | Étape | Échec affiché |
|---|-------|---------------|
| 1 | `kubectl` présent | inchangé |
| 2 | API joignable avec le ServiceAccount du pod | « cluster injoignable (ServiceAccount du pod) : … » — plus de « contexte « » » |
| 3 | Droits, un par un (`kubectl auth can-i`) : `statefulsets` (get, list, create, patch, delete), `statefulsets/scale` (patch), `pods` (get, list, delete), `pods/exec` (create), `events` (list), `persistentvolumeclaims` (get) | « droit manquant : <verbe> <ressource> » + conseil « mettre à jour le Role du chart (`helm upgrade`) » |
| 4 | Volumes déclarés | inchangé |
| 5 | Chaque PVC : présent, `Bound`, `ReadWriteMany` | inchangé |
| 6 | Chaque PVC est monté **dans le pod du launcher** au chemin déclaré dans `volumes` | « volume <pvc> non monté dans le pod du launcher à <racine> » |

L'étape `get namespace` n'est pas jouée en cluster : elle exigerait un ClusterRole.

## Mise à jour (#167)

| Variable | Valeur | Effet |
|----------|--------|-------|
| `CL_UPDATE_MODE` | `auto` (défaut) | comportement de la v6.2.0 |
| | `notify` | la nouvelle version est annoncée, sans téléchargement ni bouton d'installation ; texte « ↑ NG vX disponible · mise à jour par l'image » |

`CL_NO_UPDATE=1` et `--no-update` restent inchangés (pas d'installation au lancement). L'image fixe
`CL_UPDATE_MODE=notify`.

## Mode serveur : options de `desktop --web` (#167, #171)

| Option | Variable | Rôle |
|--------|----------|------|
| `--addr HÔTE:PORT` | — | inchangé |
| `--no-open` | — | inchangé |
| `--url <adresse publique>` | `CL_WEB_URL` | adresse publique derrière un proxy inverse — voir `contracts/desktop-web.md` |

**Sortie de démarrage en cluster** (`CL_IN_CLUSTER` ou détection automatique) : la sortie standard part dans les
journaux du pod (`pods/log`, collecte) : **le jeton n'y est jamais imprimé**. Le lancement affiche l'adresse sans
jeton, l'endroit où lire le jeton (`<cache>/desktop-web-token`, ou « fourni par `CL_DESKTOP_TOKEN` ») et le message
d'arrêt du mode serveur (« Arrêt par SIGTERM… ») à la place de « Ctrl+C pour arrêter ». L'avertissement
« écoute hors de la boucle locale sans allow_lan » (`--addr 0.0.0.0` derrière l'Ingress) n'est pas affiché. Hors
cluster : sortie inchangée (lien d'amorçage avec jeton, Ctrl+C, avertissement).

Arrêt : `SIGTERM` et `SIGINT` déclenchent un arrêt propre (serveur fermé, terminaux détachés, code 0). Les projets
restent ouverts dans leurs pods, comme à la fermeture de la fenêtre.

## Image de conteneur (#167)

| Élément | Valeur |
|---------|--------|
| Nom | `ghcr.io/ccoupel/claude-launcher` (paquet public) |
| Tags | `vX.Y.Z` (release, construit par la CI sur le tag) ; `vX.Y.Z.a` (version candidate, QUALIF) |
| Fichiers | `deploy/image/Dockerfile`, `deploy/image/entrypoint.sh`, `.dockerignore` |
| Contenu | binaire statique `claude-launcher-ng2`, `bash`, `git`, `gh`, `kubectl`, certificats racine. Pas de `claude`, pas de `tmux` |
| Utilisateur | non root, uid:gid `1000:1000` par défaut, remplaçables par le chart (`runAsUser`, `runAsGroup`) |
| Point d'entrée | `entrypoint.sh` : refuse de démarrer sans `CL_ROOT_BASE`, puis `exec claude-launcher-ng2 desktop --web --addr 0.0.0.0:8080 --no-open` (+ `--url` si `CL_WEB_URL`) |
| Variables fixées | `CL_UPDATE_MODE=notify`, `HOME=/home/launcher` (dossier inscriptible, non persistant) |
| Port | `8080` |
| Version | `claude-launcher-ng2 version` rend le tag de l'image |
