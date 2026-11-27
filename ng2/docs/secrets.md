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

## Coffre hors des pods projets (v7.0.0)

Un pod projet Kubernetes ne monte plus que son dossier et `CLAUDE_CONFIG_DIR` (voir [kubernetes.md](kubernetes.md), §8b).
Le dossier `<racine>/.config/claude-launcher-ng2/secrets/` lui est donc absent : une session claude ne peut pas lire les
clés ssh, les `known_hosts` ni les kubeconfigs du coffre. Le coffre reste accessible au hub Linux et à l'exe Windows, comme
avant.

**« Pas de coffre dans les pods » ne veut pas dire « pas de jeton GitHub ».** `.claude/settings.json` (dans
`CLAUDE_CONFIG_DIR`, monté dans les pods projets) contient les variables `GH_TOKEN` et `GITHUB_PERSONAL_ACCESS_TOKEN`,
injectées volontairement depuis la v5.7.0 (hors périmètre de #253). Ces jetons sont donc lisibles par les sessions du pod.

## Qui écrit le coffre (v7.0.0)

Avec l'authentification activée (`auth.enabled: true`), **seul un administrateur** peut écrire dans le coffre : clé ssh,
`known_hosts`, kubeconfig (`backendSecretSet`, `backendSecretDelete`, `backendKnownHostsSet`). Un `user` peut utiliser les
secrets existants (déclarer ou tester un backend, sans les écrire) ; `backendSecretGet` ne renvoie jamais le contenu.
Contrat : `contracts/auth.md` §4.

Chaque écriture porte son auteur dans `<CacheDir>/secrets-journal.jsonl` : le champ `user` (login GitHub, ou `admin@local`
pour le compte de secours), à côté de l'instance, de l'action et de l'empreinte. Le journal ne contient jamais le contenu.
Les mêmes écritures apparaissent dans le journal d'audit (`audit.jsonl`, voir [kubernetes.md](kubernetes.md) §4b).

## Coffre de l'utilisateur (v7.1.0, LOT-2)

Un utilisateur a son coffre dans `users/<uid>/vault/` (dossier 0700, fichiers 0600, **en clair**), séparé de celui de l'admin.
Le coffre de la racine n'est pas modifié. Un nom de backend commun ne peut donc pas lire le coffre de l'admin.
Contrat : `contracts/user-backends.md` §7.

## Coffre par espace : backends de l'utilisateur (v7.1.0, LOT-3.1, #257)

- **Emplacement** : `users/<uid>/vault/<backend>/` (`<backend>` = nom DNS-1123). Dossiers `0700`, fichiers `0600`, **en clair**.
  Dette assumée : chiffrement au repos (clé hors racine) suivi par **#279**.
- **Contenu** : kubeconfig (k8s), ou CA, certificat client et clé (docker / podman en `tcp://` avec TLS). Jamais de ServiceAccount
  de remplacement : un backend k8s sans kubeconfig du coffre ne lance aucune commande.
- **Lecture** : un lien symbolique, un fichier non régulier ou des droits au-delà de `0600` (dossier `0700`) sont **refusés** ;
  le backend est signalé « coffre non conforme » et rien n'est lancé. Un certificat expiré ou qui expire dans moins de 30 jours
  produit un avertissement, sans refus.
- **Écriture** : `userBackendAdd` (création : déclaration et coffre ensemble, tout ou rien), `backendSecretSet` /
  `backendSecretDelete` (remplacement ou suppression des secrets), `userBackendRemove` (retrait des deux). Hors R1 pour l'utilisateur
  sur ses propres backends. Rien n'est écrit si un contrôle échoue (PEM : un seul bloc, clé non chiffrée, appariement
  certificat/clé vérifié). Le journal de dépôt ne contient pas de contenu ; le contenu n'est jamais rendu ni transmis au navigateur.
- **Coffre orphelin** : un dossier `vault/<nom>` sans déclaration (retrait fait à la main dans l'éditeur YAML, ou arrêt entre
  deux étapes) est **repris** à la création d'un backend de même nom (supprimé puis recréé), ou **purgé** à `userBackendRemove`.
  Si la suppression échoue, l'erreur demande de réessayer.
- **Liens symboliques** : jamais suivis, ni à la création, ni au retrait, ni à la lecture ; un lien au nom du coffre est refusé.
- **Passage à `kubectl` / `docker`** : les **chemins** des fichiers du coffre sont passés directement (`--tlscacert=…`, `--tlscert=…`,
  `--tlskey=…`), sans copie temporaire. Les chemins apparaissent dans la liste des processus (`ps`) ; ils ne révèlent ni valeur
  ni secret (identifiant GitHub numérique et nom de backend).
- **Environnement** : `DOCKER_CONFIG` pointe vers un dossier vide privé (`<cache>/docker-empty`, 0700) ; podman utilise un
  `REGISTRY_AUTH_FILE` isolé ; les autres variables héritées sont retirées.
