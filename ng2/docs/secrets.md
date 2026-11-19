# Coffre des secrets (v6.5.0)

Le launcher range les accès de ses **backends** (clés ssh, `known_hosts`, kubeconfig) dans un coffre. Depuis la v6.5.0, ce
coffre est **commun** au hub Linux, à l'exe Windows et au pod : la clé saisie dans une instance sert aux deux autres.

## Emplacement

| Situation | Coffre |
|-----------|--------|
| `CL_ROOT_BASE` posée (ou `--root-base`) | `<racine>/.config/claude-launcher-ng2/secrets/` |
| `CL_SECRETS_DIR` posée (chemin absolu) | ce dossier, prioritaire |
| ni l'une ni l'autre | local au poste, comme en v6.4.0 (`~/.config/claude-launcher/secrets`, `%APPDATA%\claude-launcher\secrets`) |

Un sous-dossier par backend (`<coffre>/<backend>/`) : `id`, `id.pub`, `known_hosts`, `kubeconfig`. Dossiers en `0700`,
fichiers en `0600`. Les écritures de plusieurs instances sont sérialisées par un verrou fichier (`.vault.lock`) et atomiques ;
la dernière l'emporte. Chaque écriture est journalisée dans `<CacheDir>/secrets-journal.jsonl` (backend, action,
empreinte, instance `hub` / `pod` / `windows`, jamais le contenu).

**Pour que l'exe Windows partage le coffre**, il lui faut la même racine : `CL_ROOT_BASE` (adresse UNC
`\\wsl.localhost\…`) ou `--root-base`. Sans elle, il garde son coffre local. Sur un chemin UNC il n'y a pas d'ACL Windows :
les droits sont ceux de l'uid WSL (les jonctions Windows ne sont pas concernées).

## Secrets d'un projet : `<projet>/PRIVATE/`

Le coffre ne contient que des secrets de **backend**. Les accès propres à un projet (kubeconfig d'un environnement, clés de
déploiement, comptes) vont dans `<projet>/PRIVATE/GLOBAL/` ou `<projet>/PRIVATE/<ENV>/`, convention du template. Le launcher
n'y écrit **jamais** ; il peut seulement les lire quand un backend les désigne : `kubeconfig: /chemin/…/PRIVATE/…/kubeconfig`
est admis, comme un fichier du coffre. Un kubeconfig désigné ailleurs sous la racine partagée est refusé.

## Contrôles

Le coffre est refusé (rien n'est écrit, erreur claire) si : le chemin n'est pas absolu ; il est sous un dossier de projet ;
son chemin résolu sort de l'emplacement attendu (lien symbolique, **lien pendant compris**, boucle de liens) ; il existe avec
des droits plus larges que `0700` (Linux, y compris un partage vu depuis Linux). `/files` ne sert jamais le coffre ni `PRIVATE/`.

## Migration depuis la v6.4.0

Au démarrage de chaque instance, les backends de l'ancien coffre (emplacement local de la v6.4.0, et `CL_SECRETS_LEGACY_DIR`
posée par le chart sur l'ancien PVC) **absents** du coffre commun y sont copiés (`0600`, action `migrate` au journal). L'ancien
coffre est **conservé** : supprimez-le à la main quand vous êtes sûr. Un backend présent des deux côtés avec un contenu
différent garde la version du coffre commun ; l'écart est écrit une fois dans le journal et dans la sortie du launcher.

## Risque accepté : stockage en clair

Le coffre est **en clair** sur la racine partagée, protégé par les seuls droits du système de fichiers. Les instantanés du
stockage partagé et ses administrateurs peuvent lire les clés. Choix de l'utilisateur au GATE 2 de la v6.5.0 ; l'audit de
sécurité est reporté à la v7.0.0 (authentification). Pour un secret à ne jamais partager, utilisez un coffre local
(`CL_SECRETS_DIR` hors racine) sur le poste concerné.
