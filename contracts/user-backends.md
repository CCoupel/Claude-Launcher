# Contrat — espace par utilisateur et backends de l'utilisateur, sans agent (v7.1.0)

> Issues : #255 (réécrite : espace par utilisateur), #257 (réécrite : backends k8s et docker/podman de l'utilisateur),
> #259 (réécrite : backend injoignable), #275 (configuration par utilisateur), parapluie #249. #256 et #258 : fermées
> sans objet. Décisions utilisateur au GATE 2 (2026-11-26) :
> - **modèle sans agent** : rien n'est installé chez l'utilisateur. Le launcher découvre les projets qui existent sur les
>   backends, les démarre si besoin, et y accède comme aujourd'hui (exec, terminaux).
> - Le launcher **fournit une interface, ni stockage ni calcul** (P15).
> - Backends en mode serveur : **k8s, docker, podman** ; **ssh refusé pour tous** (P13).
>
> - **Les données projet restent chez l'utilisateur** (P18, principe) : le launcher ne garde par utilisateur que ses
>   backends, ses identifiants, ses réglages et une **trace** minimale des projets vus (§2) ; projets découverts sur le
>   backend, métadonnées lues dans le projet ou sur l'objet du backend.
> - Pas d'arrêt automatique (P17) ; relance seulement sur Ouvrir ou F5 (P20) ; compte k8s trop puissant = avertissement
>   (P19) ; approbation `env.yaml` dans le volume, scellée par le launcher (P21). Aucun point ouvert. **Mode poste inchangé** (hub tmux Linux, exe Windows, fenêtre native : ssh compris).

## 1. Vocabulaire et règles

| Terme | Sens |
|---|---|
| **Mode serveur** | Le launcher de cluster (`desktop --web`, `CL_IN_CLUSTER=1`, `CL_WEB_ACCESS=proxy`). Les règles de ce contrat ne s'appliquent qu'à lui. |
| **Espace d'un utilisateur** | Ce que le launcher garde pour lui **dans son propre état** : ses backends, son coffre, ses réglages, la trace des projets vus (§2). **Aucune donnée projet** (ni fichier, ni métadonnée git) ; la liste des projets vient de ses backends (§5). |
| **Backend de l'utilisateur** | Un k8s (kubeconfig) ou un docker/podman (`tcp://`, TLS activable) **déclaré par l'utilisateur**, joint par le launcher en connexion **entrante** vers l'API du backend, avec les accès fournis par l'utilisateur. |
| **Stockage d'un backend** | Le PVC (k8s) ou le volume (docker/podman) **nommé par l'utilisateur** dans la déclaration du backend (§4). Le launcher ne crée ni ne nomme aucun stockage. |
| **Conteneur du projet** | Le pod/conteneur qui monte le dossier du projet ; **seul** processus qui lit ou exécute le contenu de l'utilisateur. |
| **Espace de l'admin** | La racine actuelle (MooseFS) et ses backends existants, inchangés : le pod launcher monte toujours la racine (H1). |

- **Règle n°1 (F2) maintenue pour les tiers** : le processus du launcher ne lit **jamais** le stockage d'un utilisateur
  autre que l'admin et n'y exécute rien (`git status`, `.git/config`, hooks). Tout passe par le conteneur du projet, **sur
  le backend de l'utilisateur**.
- **Règle n°2 (P15)** : la plateforme ne fournit ni stockage ni calcul. Un utilisateur qui veut utiliser le k8s de la
  plateforme le déclare comme n'importe quel backend, avec ses propres accès (namespace, ServiceAccount, PVC).
- **Règle n°3** : aucun contenu d'utilisateur tiers n'est écrit sur la racine de l'admin (seul l'état du launcher, §2).

## 2. État par utilisateur (pas de base de données)

```
<racine>/.config/claude-launcher-ng2/users/<uid>/     (0700 ; <uid> = "github-<id numérique>" — P2)
├── backends.yaml      backends de l'utilisateur (k8s, docker, podman), images et défauts — §3
├── vault/             identifiants des backends, PAT GitHub, clé d'approbation (§6 bis), EN CLAIR 0600 (§7)
├── config.json        réglages **globaux** de l'utilisateur : affichage, CLAUDE_OPTIONS, choix du jeton GitHub (§8)
└── traces.json        trace des projets vus (§5) — seul résidu lié aux projets
```

- **Règle de rangement** (décision utilisateur, GATE 2) : ce qui concerne **un projet** vit dans le stockage du projet ou
  sur son objet, **sur le backend** (état §6, approbation `env.yaml` scellée §6 bis) ; ce qui concerne **l'utilisateur
  globalement** vit dans le launcher (backends, identifiants, réglages). Seule exception : la trace ci-dessous.
- **Trace** (`traces.json`), une entrée par projet vu à la dernière découverte réussie :
  `{backend, kind (k8s|docker|podman), object (nom du StatefulSet ou du conteneur), project (cl.project),
  category (cl.category), first_seen, last_seen}`. **Rien d'autre** : ni branche, ni état git, ni chemin de fichier, ni
  dépôt. Rôle : afficher en erreur un projet dont l'objet ou le backend a disparu (§5). Durée : jusqu'à « Purger », ou
  jusqu'au retrait du backend par son propriétaire (traces purgées avec lui, après confirmation listant les projets).
  Pas d'expiration automatique.
- Écriture atomique + verrou (`core/lockfile.go`) ; aucune lecture croisée entre `<uid>`.
- **Admin** : son `<uid>` désigne l'espace actuel (`backends.yaml`, `projects.json`, coffre de la racine), sans migration ni
  changement de comportement. Compte local `admin@local` = l'admin.
- **Utilisateur sans backend** : espace vide → interface « Ajoute ton premier backend » (M1).
- Identifiant numérique : résolu depuis le login (`X-Forwarded-User`) par `GET https://api.github.com/users/<login>`,
  mis en cache (`users/index.json` : login → id, date ; nouvelle résolution après 24 h). Un login dont l'id change est
  refusé (`401` + audit) jusqu'à action de l'admin (`ForgetUserLogin`). API GitHub injoignable : dernier id connu ;
  sans id connu → `503`.

## 3. Backends en mode serveur **[NEW / CHANGED]** (#257, P12, P13)

| Type | Connexion (entrante, depuis le pod launcher) | Identifiants (coffre) | Refus |
|---|---|---|---|
| `k8s` | API du cluster (URL du kubeconfig), `namespace` obligatoire | kubeconfig à jeton (ServiceAccount) ou certificat client | `exec:` / `auth-provider:` dans le kubeconfig (commande exécutée dans le launcher) ; namespace absent |
| `docker` | `host: tcp://hôte:port` | si `tls.enabled` : `ca.pem` (si `tls.verify`), `cert.pem`, `key.pem` | `unix://` et moteur local pour un utilisateur (calcul sur la plateforme, règle n°2) ; `ssh://` |
| `podman` | `host: tcp://hôte:port` (API compatible docker ; TLS via un proxy, podman n'en ayant pas) | idem docker | idem docker |
| `ssh` ; tout `host`/`engine_host` en `ssh://` ; pools d'hôtes ssh | — | — | **refusés pour tous en mode serveur** (P13) |
| `host` | — | — | refusé en mode serveur (déjà le cas derrière le proxy, v7.0.0) |

### Validation du `backends.yaml` d'un utilisateur : liste blanche, échec fermé (#255, revue LOT-2)

Tout ce qui n'est pas listé ici est **refusé** (« réservé à l'administrateur » ou « non autorisé en mode serveur ») ;
rien n'est écrit. YAML illisible, ancres / alias / fusion `<<`, clés en double (casse comprise) → refus.

| Niveau | Autorisé | Refusé (exemples, liste non limitative) |
|---|---|---|
| Rubriques | `backends:` seulement (LOT-3.2 ajoutera `images:` et `defaults: {backend, image}` pour #275) | `pools`, `hubs`, `defaults`, `images` (avant LOT-3.2), `desktop_web`, `browser_server`, `browser_paths`, `browser_device`, toute autre |
| Tout backend | `type` (obligatoire) ∈ {`k8s`, `docker`, `podman`} | `type: host`, `type: ssh`, type absent ou autre |
| `docker` / `podman` | `host` **obligatoire**, de la forme `tcp://hôte:port` (`https://` **non admis** : docker et podman utilisent `tcp://` avec TLS) ; `volume` (LOT-3.2) ; `tls: {enabled, verify}` (LOT-3.1) ; `container:` = options **par défaut** des conteneurs de projet (LOT-3.2, §5 bis) | `host` absent ou vide (moteur local du launcher), `unix://`, `npipe://`, `fd://`, `ssh://`, chemin de socket ; `hosts`, `pool`, `engine`, `engine_host`, `shared`, `volumes`, `context`, `kubeconfig`, `pod`, `spread` |
| `k8s` | `namespace` (obligatoire, libre) ; `pvc` (LOT-3.2) ; `kubeconfig: true` (**obligatoire** dès LOT-3.1, posé par l'envoi du kubeconfig collé ; jamais un chemin) ; `spread` ; `pod:` = options **par défaut** des pods de projet (§5 bis : `resources`, `nodeSelector`, `affinity`, `tolerations`) | `host`, `hosts`, `pool`, `context`, `volumes`, `kubeconfig` autre que `true`, toute autre clé de `pod` (dont les clés R1 de `auth.md` §4) |

- Le champ refusé est nommé dans le message (« `pool` : non autorisé pour un utilisateur »), sans recopier de valeur.
- Le `backends.yaml` de l'admin n'est pas concerné (règle R1 de `auth.md` §4, inchangée).

### Enregistrement : conflit de version

- L'éditeur envoie le `mtime` du fichier lu à l'ouverture. Sous verrou, le launcher refuse l'écriture si le fichier a
  changé depuis (`mtime` différent), **ou si le `mtime` est absent alors que le fichier existe**. Message : « Le fichier
  a changé depuis l'ouverture de l'éditeur : rouvre l'éditeur pour repartir de la dernière version. »
- Fichier absent et `mtime` absent : création. Écriture atomique `0600`, copie `.bak` de la version précédente.

### TLS docker/podman (P12)

```yaml
backends:
  nas:
    type: docker
    host: tcp://nas.lan:2376
    volume: projets-alice          # §4
    tls:
      enabled: true                # false : connexion en clair
      verify: true                 # false : certificat du serveur non vérifié (LAN, certificat auto-signé)
```

- Mise en place côté hôte : procédure officielle <https://docs.docker.com/engine/security/protect-access/> (TLS,
  `dockerd --tlsverify`), à reprendre dans `ng2/docs/backends-utilisateur.md`.
- **Avertissement affiché à l'enregistrement, et en permanence sur la carte du backend** quand TLS est désactivé ou non
  vérifié : « Accès docker sans TLS vérifié : quiconque atteint ce port est root sur l'hôte. À réserver à un réseau de
  confiance. » Le même texte vaut, sous une forme courte, pour tout accès docker distant : « un accès docker donne
  l'équivalent de root sur l'hôte ».
- Le backend `srv1-docker` existant de l'admin (`tcp://192.168.1.211:2375`, sans TLS) reste accepté, avec l'avertissement.

### Saisie des identifiants : par collage, jamais par fichier (éditeur de backends actuel)

- L'éditeur de backends actuel (Réglages › Backends, `settings.js`) est conservé. Pour un utilisateur en mode serveur :
  type limité à docker / podman / k8s ; champ `volume` (docker/podman) ou `pvc` (k8s) ; le bouton « Fichier » de
  l'encadré kubeconfig est **retiré** (collage seulement) ; nouvel encadré **TLS** pour docker/podman.
- **Encadré TLS** : `tls.enabled`, `tls.verify` ; **CA du serveur à coller** (`ca.pem`, exigée si `tls.verify`),
  certificat client (`cert.pem`) et clé client (`key.pem`) collés. Envoi par `backendSecretSet` (`ca`, `cert`, `key`),
  comme le kubeconfig et les clés ssh aujourd'hui.
- Aucun chemin local lu par le launcher pour un utilisateur : `kubeconfig.path`, `DOCKER_CERT_PATH`, `context` docker
  (fichier local du poste) refusés.
- Contrôles à la réception (refus avec message, rien n'est écrit) : taille ≤ 64 Kio par champ ; kubeconfig = YAML valide,
  formes `*-data` seulement, sans `exec` / `auth-provider` ; `ca` et `cert` = exactement un bloc `CERTIFICATE` valide
  (dates affichées, `ca` avec `CA:TRUE` ou auto-signé) ; `key` = un bloc de clé privée **non chiffrée**
  (`PRIVATE KEY`, `RSA PRIVATE KEY`, `EC PRIVATE KEY`), qui correspond au certificat client.
- Après l'envoi : champ vidé, contenu **jamais réaffiché** ; état affiché = « enregistré · empreinte SHA256 · sujet ·
  expiration » (clé : « enregistrée », sans détail). Remplacer / Supprimer comme l'encadré ssh actuel.

### Création d'un backend d'utilisateur : une seule action atomique **[NEW]** (GATE LOT-3.1 refusé, B9/B12/B13)

Un backend d'utilisateur se crée **avec ses identifiants, dans le même envoi**. Il n'y a plus de « crée d'abord, dépose
ensuite » : un backend k8s sans kubeconfig est refusé (S15), et il est impossible d'y joindre un kubeconfig avant qu'il
existe.

**Interface** : « + Ajouter » ouvre un brouillon de fiche (non enregistré) qui contient, selon le type :
- k8s : la zone de **kubeconfig collé** ;
- docker/podman : l'**encadré TLS** (activer, vérifier, CA, certificat, clé).

« Enregistrer » envoie **une** requête `userBackendAdd`.

**Requête** `userBackendAdd` :

```json
{ "backend": { "name": "k8s-maison", "type": "k8s", "namespace": "claude-alice", "pvc": "projets-alice",
               "pod": { "resources": {…}, "nodeSelector": {…}, "affinity": {…} } },
  "secrets": { "kubeconfig": "apiVersion: v1\n…" },
  "mtime": "<mtime du backends.yaml lu à l'ouverture de l'éditeur, ou vide si absent>" }
```

docker/podman : `"backend": {"name", "type", "host": "tcp://…", "volume", "tls": {"enabled", "verify"}}` et
`"secrets": {"tls": {"ca", "cert", "key"}}`. Les champs `kubeconfig: true` et la présence des fichiers TLS sont posés
par le serveur, jamais par le client.

**Validation complète avant toute écriture** (rien n'est écrit si une règle échoue) :
1. liste blanche de la déclaration (§3), nom DNS-1123, nom pas déjà pris (`conflict`) ;
2. bornage de l'hôte (`host`, et `server:` de tous les clusters du kubeconfig), DNS compris ;
3. secrets obligatoires selon le type :
   - k8s → `secrets.kubeconfig` **obligatoire** ;
   - docker/podman avec `tls.enabled` → `cert` et `key` obligatoires, plus `ca` si `tls.verify` ;
4. contrôles des secrets : kubeconfig durci, PEM, tailles (§3) ;
5. limites : 11ᵉ backend (`users.maxBackends`), débit des dépôts (60/min), conflit de version (`mtime`).

**Écriture tout ou rien** (sous le verrou de l'espace) :
1. le dossier `users/<uid>/vault/<nom>/` est écrit (fichiers `0600`, dossier `0700`). Un coffre **non déclaré**
   (orphelin : déclaration retirée par l'éditeur YAML, arrêt entre les deux étapes) est **repris** : supprimé puis
   recréé. Un nom **déclaré** dont le coffre existe déjà est refusé (`conflict`). Un lien symbolique au nom du coffre
   n'est jamais suivi (refus) ;
2. puis `backends.yaml` (écriture atomique, contrôle `mtime`, `.bak`) ;
3. si l'étape 2 échoue, le dossier du coffre créé à l'étape 1 est supprimé. Aucun backend sans ses secrets, aucun secret
   sans son backend. Tout se fait sous le verrou de `backends.yaml`.

**Retrait** (`userBackendRemove`, sous le même verrou) : la déclaration et le coffre sont supprimés ensemble.
- Nom non déclaré mais coffre présent : le coffre orphelin est purgé.
- Ni déclaration ni coffre : « backend inconnu ».
- Échec de la suppression du coffre : erreur claire, « le backend est retiré mais son coffre n'a pas pu être supprimé :
  réessaie » (un nouvel appel purge le coffre orphelin).
- Un lien symbolique au nom du coffre n'est jamais suivi.

Aucun test n'est lancé automatiquement : « Tester » reste une action séparée (plafond de 6 par minute).

**Réponses** :

| Cas | Réponse |
|---|---|
| Succès | `{ "ok": true, "backend": {"name", "type"}, "warnings": ["…"] }` (avertissements : compte trop puissant, TLS non vérifié, root sur l'hôte, certificat proche de l'expiration) |
| Refus de validation | `{ "ok": false, "field": "<champ>", "code": "<code>", "msg": "<raison en clair>" }` |
| Débit dépassé | HTTP `429`, même forme, `code: "rate"` |
| Règle R1 (admin) | forme R1 inchangée (`auth.md` §4) |

- `field` désigne l'élément du formulaire à corriger : `name`, `type`, `host`, `namespace`, `pvc`, `volume`, `pod`,
  `kubeconfig`, `tls.ca`, `tls.cert`, `tls.key`, ou absent pour une erreur globale (`limit`, `conflict`).
- Codes : `required`, `invalid`, `forbidden_address`, `forbidden_field`, `too_large`, `conflict`, `limit`, `rate`.
- Messages, exemples (jamais de contenu de secret ni d'IP résolue) :
  - « kubeconfig obligatoire pour un backend k8s » ;
  - « adresse non autorisée pour un utilisateur » ;
  - « kubeconfig refusé : commande exec interdite » ;
  - « clé privée chiffrée : colle la clé déchiffrée » ;
  - « nombre maximal de backends atteint (10) » ;
  - « un backend porte déjà ce nom » ;
  - « le fichier a changé depuis l'ouverture de l'éditeur : rouvre l'éditeur ».

**Comportement de l'éditeur** :
- Après un refus, la fiche reste ouverte et **modifiable**. Le brouillon est conservé, y compris le contenu collé, gardé
  en mémoire de la page seulement (jamais stocké ni journalisé). Le champ nommé par `field` est mis en évidence avec le
  message : on corrige et on réenvoie, sans passer par le YAML.
- **Jamais de bouton grisé muet** : un bouton désactivé affiche à côté la raison (« colle le kubeconfig pour enregistrer
  un backend k8s »).
- **Modification d'un backend existant** : les champs non secrets passent par `configApply` / `configSave` (comme en
  LOT-2). Le remplacement ou la suppression d'un secret reste possible séparément (`backendSecretSet` /
  `backendSecretDelete`, `auth.md` §4). Passer un backend existant en `type: k8s` sans kubeconfig déjà présent est
  refusé (`field: "kubeconfig"`, `code: "required"`).
- `configApply` / `configSave` qui **ajoutent** un backend k8s, ou docker/podman avec TLS, sans ses secrets sont refusés
  avec le même message, et renvoient vers « + Ajouter ».

### Droits vérifiés à l'enregistrement (`backendTest` étendu)

- k8s : `SelfSubjectAccessReview` sur les verbes requis dans le namespace (statefulsets CRUD + scale, pods
  get/list/watch/delete, `pods/exec` create, pvc get, events list). S'il en manque, refus avec la Role à appliquer
  (documentée). Des droits **au-delà** du namespace (`*` sur `*`, ressources de cluster) donnent un **avertissement**
  (P19 tranché), sans refus : « Compte plus puissant que nécessaire. Le coffre est en clair : préfère un ServiceAccount
  limité au namespace » + la Role minimale suggérée. Risque documenté (`backends-utilisateur.md`). `insecure-skip-tls-verify` est accepté avec
  l'avertissement « certificat de l'API non vérifié » (même logique que `tls.verify: false`).
- docker/podman : `version`, `info` ; le volume déclaré doit exister (`volume inspect`), sinon refus.
- **Joignabilité et bornage des hôtes (décision 2026-11-27)** :
  - backend d'un **utilisateur** : **Internet public seulement**, sauf si l'admin pose **`users.allowPrivateNetworks: true`**
    (Helm, défaut `false`, décision P25) : les plages privées `10/8`, `172.16/12`, `192.168/16` et les adresses IPv6 ULA
    `fc00::/7` sont alors admises, ainsi que les noms internes qui y mènent (`*.lan`, `*.local`, `*.internal`, `*.home.arpa`,
    `*.svc`, `*.cluster.local`, noms sans point). **Toujours refusés**, même avec le paramètre : loopback et `localhost*`,
    `0.0.0.0` / `::`, link-local et métadonnées, multicast, `100.64/10`, `192.0.0/24`, `198.18/15`, documentation, `240/4`,
    diffusion. Au démarrage, le launcher journalise « réseaux privés autorisés aux utilisateurs : un utilisateur peut
    viser le LAN de la plateforme ». Usage prévu : la QUALIF ;
  - un **admin** n'a aucune contrainte : LAN permis, `backends.yaml` inchangé.
  - Pour un utilisateur, refus (échec fermé, message unique « adresse non autorisée pour un utilisateur ») de :
    - loopback, `0.0.0.0`, link-local et métadonnées (`169.254/16`, `fe80::/10`, `100.100.100.200`, `fd00:ec2::254`) ;
    - plages privées et réservées (`10/8`, `172.16/12`, `192.168/16`, `100.64/10`, `fc00::/7`, `192.0.0/24`, `198.18/15`, documentation, multicast, `240/4`, diffusion) ;
    - noms internes (sans point, `*.local`, `*.lan`, `*.internal`, `*.home.arpa`, `*.svc`, `*.cluster.local`, `localhost*`, `kubernetes.default`).
  - Hôte analysé strictement (`url.Parse` / `net/netip`) : formes numériques non canoniques, zone IPv6, userinfo, chemin, requête refusés ; IPv4-mappée, NAT64 et 6to4 déballées puis contrôlées ; port obligatoire 1–65535.
  - Appliqué au `host` docker/podman **et** au `server:` (en `https://`) de tous les clusters du kubeconfig.
  - **Contrôle à l'enregistrement et avant chaque appel** : résolution DNS par le launcher, cache 30 s, une seule IP interdite parmi celles rendues suffit à refuser. **Résolution en erreur** (NXDOMAIN, délai, serveur injoignable) → même refus uniforme, **jamais mis en cache** (échec fermé).
  - Risque résiduel accepté : le CLI résout à nouveau (fenêtre de rebinding de quelques ms).
  - Un backend derrière un NAT sans redirection de port ni tunnel reste impossible (« injoignable depuis la plateforme »).
- **Backend k8s d'un utilisateur** : `kubeconfig: true` (kubeconfig collé dans son coffre) **obligatoire**. Sans lui, refus à la déclaration et à l'exécution : jamais le ServiceAccount du launcher, jamais `~/.kube/config`. Coffre : `users/<uid>/vault/<backend>/` uniquement.
- **Appels d'un utilisateur** : environnement nettoyé (`HTTP(S)_PROXY`, `ALL_PROXY`, `NO_PROXY`, `DOCKER_*`, `CONTAINER_HOST`, `KUBECONFIG` retirés ; `DOCKER_CONFIG` pointé vers un dossier vide privé, pour que la configuration docker du launcher ne s'applique jamais) ; certificats TLS passés à `docker` par les **chemins du coffre** (`--tlscacert`, `--tlscert`, `--tlskey` → `users/<uid>/vault/<backend>/…`), sans copie : seuls des chemins figurent dans `argv`, fichiers `0600` et dossiers `0700` vérifiés à l'usage (`Lstat`, pas de lien symbolique) ; erreurs filtrées (joignable / non joignable, sans corps de réponse ni secret) ; `proxy-url` refusé ; kubeconfig ≤ 64 Kio, sans alias ni ancre.
- **Limites (D5)** :
  - `users.maxBackends` backends par utilisateur (Helm, défaut 10) ;
  - `users.maxProjects` projets par utilisateur, tous backends confondus (Helm, défaut 30 ; objets `cl.user` existants comptés à la création) ; au-delà, refus nommé « nombre maximal de projets atteint (30) » (P26) ;
  - **aucun plafond de CPU ni de mémoire** imposé par le launcher : l'utilisateur est responsable de sa consommation sur ses backends (P26, risque accepté) ;
  - `backendTest` : `users.backendTestsPerMinute` par utilisateur (défaut 6) et un seul test à la fois (`429` au-delà) ;
  - `backendSecretSet`, `backendSecretDelete`, `userBackendAdd` et `userBackendRemove` : plafond commun de 10 × `users.backendTestsPerMinute` par minute et par utilisateur (60 par défaut), `429` au-delà ;
  - `configOpen`, `configModel`, `configValidate`, `configSave`, `configApply` (espace d'un utilisateur) : 60 par minute, `429` avec `code: "rate"` ;
  - sondage : un appel en vol par backend, attente croissante (10 s → 5 min) quand il est injoignable ;
  - audit `backend.call` de chaque appel sortant (utilisateur, backend, opération, résultat, durée ; sans secret ; agrégé par minute pour le sondage).
- Pools nommés, `hosts` multiples, `spread` : non proposés aux utilisateurs en v7.1.0.

### ssh en mode serveur : refus explicite, jamais silencieux (P13)

- **Chargement** de `backends.yaml` (admin compris) en mode serveur : un backend ssh (liste ci-dessus) est **écarté avec
  un diagnostic visible** sur sa carte et au démarrage : « backend `<nom>` ignoré : ssh est interdit en mode serveur ;
  utilise docker (TLS) ou k8s ». Ses projets apparaissent « backend refusé », sans aucune suppression. Le fichier reste
  lisible : le hub Linux et l'exe Windows, qui partagent la racine, continuent d'utiliser ces backends.
- **Enregistrement / validation** (`configValidate`, `configSave`, `userBackendAdd`) depuis le mode serveur : un backend
  ssh est **refusé** avec le même message.
- **Mode poste** (hub, exe Windows) : ssh **conservé**, sans changement.
- État constaté le 2026-11-26 : **aucun backend ssh** dans les `backends.yaml` de QUALIF ni de PROD (seulement `podman`
  local, `srv1-docker`, `k8s`) : migration nulle. Les backends ssh éventuels du hub restent en mode poste.

## 4. Stockage d'un backend **[NEW]** (P10, P11)

| Backend | Paramètre (nommé par l'utilisateur) | Montage dans le conteneur du projet |
|---|---|---|
| `k8s` | `pvc: <nom>` : PVC **existant** du namespace (RWX, pour que plusieurs projets ouverts le montent), créé par l'utilisateur | `subPath` `PROJETS/<catégorie>/<projet>` + `subPath` `.claude` sur `CLAUDE_CONFIG_DIR` (règle #253) |
| `docker` / `podman` | `volume: <nom>` : volume **existant** de l'hôte, créé par l'utilisateur | volume monté entier sur `/space` ; travail dans `/space/PROJETS/<catégorie>/<projet>` ; `CLAUDE_CONFIG_DIR=/space/.claude` |

- Le launcher ne crée ni ne supprime aucun PVC ni volume (règle n°2). Un PVC ou volume absent donne un refus à
  l'enregistrement et un diagnostic à l'usage.
- `.claude` = un par backend : la connexion claude.ai se fait **une fois par backend** (`claude /login` dans un terminal
  du premier projet), puis sert à tous les projets de ce backend.
- Le backend `k8s` de l'admin garde sa déclaration actuelle (`volumes:`), sans changement.

## 5. Source de vérité et cycle de vie d'un projet

- **Source de vérité = le backend** : les projets d'un utilisateur sont les objets qui **existent** sur ses backends,
  découverts par le launcher en s'y connectant (`ReadCtrView` / `K8sList` actuels) :
  - k8s : StatefulSets `cl.launcher=ng2, cl.user=<uid>` du namespace ;
  - docker/podman : conteneurs (arrêtés compris, `ps -a`) aux mêmes labels.
  Sondage toutes les 10 s (cadence actuelle de `readCtr`). Chaque découverte réussie met à jour la trace (§2).
- Labels posés à la création (immuables) : `cl.launcher`, `cl.user`, `cl.project`, `cl.category`, `cl.backend`,
  `cl.repo` (URL du dépôt, si clone), `cl.created`.

### Disparition (P18)

| Situation | Affichage | Action |
|---|---|---|
| Objet absent du backend joignable, trace présente | projet **visible en erreur** : « introuvable sur `<backend>` (vu le …) » | **Purger** : efface la trace seulement ; message « les fichiers éventuels restent dans `<PVC/volume>` » ; **aucune** suppression chez l'utilisateur |
| Backend retiré de la déclaration par l'utilisateur | — | confirmation listant les projets, puis traces purgées ; rien n'est supprimé sur le backend |
| Backend injoignable | projets de la trace visibles « backend injoignable » (§10) | aucune (attente du retour) ; Purger disponible |
| Dossier dans le stockage sans objet **et** sans trace | invisible (orphelin) | aucune en v7.1.0 ; documenté : recréer un projet du même nom réutilise les fichiers |

### Actions

| Action | Où | Détail |
|---|---|---|
| **Créer** | launcher → backend choisi | l'utilisateur choisit backend, catégorie et nom ; le launcher crée l'objet labellisé, monte le stockage déclaré (§4), le dossier est créé au premier démarrage |
| Cloner | conteneur du projet | création puis `git clone` lancé **dans** le conteneur (uid de session), URL validée (`ValidateCloneURL`) ; progression en job |
| Ouvrir | API + exec | démarre le conteneur s'il est arrêté, puis session et terminaux comme aujourd'hui (`Engine`, exec, `tmux -C`) |
| **F5** | API + exec | démarre le conteneur s'il est arrêté (sans session claude), relit l'état, met à jour `cl.meta` (k8s) |
| Fermer / Forcer fermer | API + exec | comme aujourd'hui (#269) : fin de session puis arrêt du conteneur. **Pas d'arrêt automatique** (P17). **Relance** : seulement sur action explicite (Ouvrir, F5), jamais par le sondage de 10 s **(P20 tranché)** |
| Déplacer / renommer | conteneur (démarré au besoin) | dans le **même** backend : déplacement du dossier + recréation de l'objet aux nouveaux labels ; vers un autre backend : refusé (hors v7.1.0) |
| Supprimer | conteneur puis API | suppression du dossier (confirmation détaillée existante) puis de l'objet et de la trace |
| Purger | launcher | trace seulement (§2) |
| Approbation `env.yaml` (#215) | conteneur à la première ouverture | condensés lus dans le conteneur, approbation scellée (HMAC) écrite dans le volume du projet (§6 bis) |

## 5 bis. Options du pod ou du conteneur, par projet **[NEW]** (P26)

- Les options vivent **sur l'objet du projet** (règle de rangement) : spec du StatefulSet (k8s) ou configuration du
  conteneur (docker/podman, lue par `inspect`). Le `pod:` / `container:` du backend fournit les **valeurs par défaut** d'un
  nouveau projet.
- Édition dans l'interface (réglages du projet, maquette M4). Le launcher construit le manifeste ou l'`argv` sur une
  **liste blanche fermée** : aucun champ libre, aucun texte recopié tel quel.

| Backend | Champs autorisés (et rien d'autre) | Défauts proposés |
|---|---|---|
| k8s (`pod:`) | `resources.requests` / `resources.limits` : `cpu`, `memory`, `ephemeral-storage`, ressources étendues au format `<domaine>/<nom>` (ex. `nvidia.com/gpu`) ; `nodeSelector` (≤ 16 paires, format des labels) ; `affinity` (`nodeAffinity`, `podAffinity`, `podAntiAffinity`, schéma k8s validé) ; `tolerations` (≤ 16) | requests `cpu: 100m`, `memory: 256Mi` ; limits `cpu: "2"`, `memory: 4Gi` ; rien d'autre |
| docker/podman (`container:`) | `cpus` (décimal > 0), `memory` (quantité, ≥ 64m), `pids_limit` (entier), `shm_size` (quantité), `gpus` (`all` ou entier) | `cpus: 2`, `memory: 4g`, `pids_limit: 4096`, `shm_size: 256m` |

- **Jamais** (k8s et docker/podman), quelle que soit la saisie : `privileged`, capacités (`cap_add`), `hostPath`, `hostNetwork`,
  `hostPID`, `hostIPC`, montages libres, `--volumes-from`, `--device`, `--network`, `-p` / ports, `securityContext`,
  `serviceAccountName`, `runtimeClassName`, `--security-opt`, `--userns`, `--pid`. Tout champ hors de la table est refusé
  avec un message nommé (`field: "pod.<chemin>"` / `"container.<champ>"`).
- **Modification après création** :
  - k8s : patch du StatefulSet, appliqué au prochain démarrage du pod ;
  - docker/podman : `cpus`, `memory`, `pids_limit` changés **à chaud** par `docker update`, sans recréer. `shm_size` et
    `gpus` ne se changent pas sur un conteneur existant : le changement **recrée le conteneur après confirmation**
    (P27 tranché).
- **Recréation d'un conteneur (P27)** :
  - **Confirmation explicite** : la boîte de dialogue liste ce qui est **perdu** (tout ce qui a été installé ou modifié dans
    le conteneur hors du volume : paquets, fichiers hors `/space`) et ce qui est **gardé** (dossier du projet, `.claude`,
    état et approbation sous `.claude-launcher/`, tous dans le volume).
  - **Session claude en cours ou terminal ouvert** : la confirmation annonce que le projet sera d'abord fermé (comme
    « Fermer ») ; sans confirmation, rien ne change.
  - **Déroulé, avec retour arrière** :
    1. arrêt du conteneur ;
    2. renommage en `<nom>-old-<horodatage>` ;
    3. création du nouveau conteneur (mêmes labels, nouvelles options, même volume) puis démarrage ;
    4. succès → suppression de l'ancien ;
    5. échec à l'étape 3 → suppression du nouveau, ancien renommé et redémarré s'il tournait, erreur nommée.
  - Le volume n'est jamais touché. Audit `project.recreate` (options changées, résultat, sans contenu).
  - k8s : rien de tel. Un changement d'options patche le StatefulSet, et le pod est recréé à son prochain démarrage (un pod k8s ne garde
    jamais ses installations hors volume).

## 6. Projet arrêté **[NEW]** (P9, principe P18)

**Seul l'état est conservé, et il est conservé sur le backend**, jamais dans le launcher.

| Information | Projet arrêté | Projet en marche (exec, comme aujourd'hui) |
|---|---|---|
| Nom, catégorie, backend, état, image, dépôt, création | labels de l'objet | idem |
| Git (branche, modifié, en avance/retard, dernier commit), template, team | **dernier état connu + date**, lu **sans démarrer** : k8s → annotation `cl.meta` du StatefulSet (lisible à 0 réplique) ; docker/podman → fichier `.claude-launcher/state.json` du projet, dans le volume, lu dans le conteneur arrêté (`docker cp` / `podman cp`, API archive) | en direct, toutes les 30 s ; état réécrit à chaque lecture et avant l'arrêt |
| Milestones GitHub | API GitHub à partir de `cl.repo` | idem |
| Attention, sessions en attente | aucune | en direct |
| Consommation claude.ai du backend | inconnue | lue dans un conteneur en marche de ce backend |
| `/files/<projet>/…` | **`404 projet arrêté`** (aucune lecture, aucun démarrage) | flux `exec cat` / `tar`, règles `sensitiveRel` appliquées par le launcher, liens résolus dans le conteneur (`realpath`) |

- **Où est écrit le dernier état** (par le launcher, jamais par le projet) :
  - k8s : annotation `cl.meta` du StatefulSet (patch par l'API ; le pod n'a pas de jeton de ServiceAccount, il ne peut
    pas l'écrire) ;
  - docker/podman : fichier `<projet>/.claude-launcher/state.json` **dans le volume** (survit à la recréation du
    conteneur), écrit par exec à chaque lecture et avant l'arrêt, lu à l'arrêt par l'API archive (`docker cp`).
    Vérifié le 2026-11-26 sur docker 29.2.1 (srv1) : `inspect` et `cp` d'un conteneur arrêté lisent les labels, le
    système de fichiers et les volumes sans le démarrer. podman : `podman cp` et l'API archive compatible lisent aussi un
    conteneur arrêté (documentation ; à confirmer en QUALIF).
  - Pourquoi k8s diffère : un PVC ne se lit pas sans pod en marche ; l'annotation de l'objet, elle, reste lisible.
  - Le contenu est **affiché seulement**, jamais exécuté : JSON ≤ 4 Kio, schéma fixe, valeurs échappées. Le fichier
    étant dans le volume, le contenu du projet peut le modifier : au pire, un état affiché faux, sans autre effet.
- **Objet supprimé** : plus d'objet, donc plus d'état ; c'est le rôle de la **trace** (§2, §5) : projet visible en
  erreur avec « Purger ». **Backend injoignable** : ni inspect ni cp possibles, donc trace seulement (§10).
- **F5** sur un projet arrêté : démarrage (docker/podman `start` 1 à 3 s ; k8s `scale 0→1` 5 à 30 s, jusqu'à 2 min si
  l'image n'est pas en cache du nœud), relecture, le conteneur **reste en marche** (pas d'arrêt automatique).
- **Ressources d'un conteneur sans session** (image du projet au repos : `sh` + serveur tmux) : environ 5 à 20 Mio de
  mémoire, CPU nul ; en k8s, un emplacement de pod du nœud (110 par défaut) et les requêtes de `pod.resources` si
  l'utilisateur en déclare (réservées même au repos). C'est le calcul de l'utilisateur, pas de la plateforme (règle n°2).
- `cl.meta` : JSON ≤ 4 Kio (`branch`, `dirty`, `ahead`, `behind`, `head`, `tpl`, `team`, `seen`) ; jamais de contenu de
  fichier ni de secret. Exige le verbe `patch` sur `statefulsets` (déjà dans la Role requise).

## 6 bis. Approbation de l'environnement d'un projet cloné (#215) **[CHANGED]** (P21)

- **Existant** : registre `trust.json` dans la config partagée du launcher (`core/envtrust.go:71`), hors projet. (L'annotation
  `cl.trust` de la révision 5 n'était qu'une proposition.) L'espace de l'admin et le mode poste **gardent** ce registre.
- **Utilisateurs en mode serveur** — règle : par projet, donc **dans le volume du projet**, pour k8s comme pour
  docker/podman (survit à la recréation du conteneur) : fichier `<projet>/.claude-launcher/trust.json`.
- **Sécurité : approbation scellée** — le fichier contient, pour chaque fichier d'environnement approuvé, son condensé
  SHA-256, la date, et `mac = HMAC-SHA256(clé d'approbation de l'utilisateur, uid ‖ liste triée des condensés)`.
  - La clé d'approbation (32 octets aléatoires, une par utilisateur, créée au premier besoin) est rangée dans le coffre de
    l'utilisateur **côté launcher** (`users/<uid>/vault/approval.key`) : réglage global de l'utilisateur, jamais
    envoyée au backend.
  - À l'ouverture, le launcher lit les fichiers d'environnement et `trust.json` dans le conteneur (exec), recalcule les
    condensés et le HMAC : approbation valide seulement si tout correspond ; sinon il redemande (#215 : redemandée si ces
    fichiers changent).
  - Un dépôt cloné qui embarque un `trust.json` ne peut pas le forger (pas de clé) ; du code déjà exécuté dans le conteneur
    non plus. Copier l'approbation d'un autre projet de l'utilisateur ne vaut que pour un contenu identique, donc déjà approuvé.
- **Limites** :
  - perte ou rotation de la clé → toutes les approbations sont redemandées (échec sûr) ;
  - le coffre étant en clair (P14), qui lit la racine peut forger une approbation. Mais il a déjà les identifiants des
    backends ;
  - le fichier est versionnable par erreur : exclu de git (`.git/info/exclude`, comme le dossier d'attention).

## 7. Coffre de l'utilisateur **[NEW]** (P14 : en clair)

- `users/<uid>/vault/<backend>/` **en clair**, `0700` / `0600`, comme le coffre actuel de l'admin (risque accepté
  v6.5.0) : lisible par les administrateurs du stockage partagé et dans les instantanés MooseFS.
- **Dette assumée** : chiffrement au repos (clé hors racine) dans une prochaine version (issue de suivi proposée).
- **Création** : le coffre d'un nouveau backend est écrit **dans la même action** que sa déclaration (`userBackendAdd`, §3),
  tout ou rien ; ensuite, remplacement et suppression par `backendSecretSet` / `backendSecretDelete`. Un coffre orphelin
  (sans déclaration) est repris à la création ou purgé au retrait (§3) ; le retrait supprime déclaration et coffre
  ensemble.
- **Lecture du coffre** : un fichier du coffre qui est un lien symbolique, ou dont les droits dépassent `0600` (dossier
  `0700`), est **refusé** (le backend est signalé « coffre non conforme », rien n'est lancé).
- **Certificats** (CA, certificat client, certificat du kubeconfig) : avertissement s'ils sont expirés ou s'ils expirent dans
  moins de 30 jours (carte du backend et `backendTest`), sans refus.
- Écrit par l'interface (`backendSecretSet` / `backendSecretDelete`, **hors R1 pour l'utilisateur sur ses propres
  backends**, `auth.md` §4), aucune relecture sauf état et empreinte ; jamais journalisé, jamais rendu, jamais transmis au
  navigateur ; passé à `kubectl` (`--kubeconfig`) ou `docker` (`--tls*`) par **le chemin du fichier dans le coffre**, sans
  copie temporaire (décision security et revue LOT-3.1 : jamais de contenu dans `argv` ni dans l'environnement ; droits et
  absence de lien vérifiés à chaque usage).

## 8. Jeton GitHub de l'utilisateur **[NEW]** (P5 « les 2 »)

| Choix (`config.json` `github.token`) | Source | Usage |
|---|---|---|
| `pat` | PAT saisi par l'utilisateur, coffre §7 | milestones (launcher), `GH_TOKEN` des sessions |
| `oauth` | jeton de la connexion : oauth2-proxy `--pass-access-token` → en-tête `X-Forwarded-Access-Token` (sidecar → launcher en boucle locale seulement) | idem ; gardé **en mémoire** (par session web), jamais écrit, jamais journalisé ; injecté dans l'environnement du conteneur à l'ouverture |

- `oauth` impose une GitHub App avec permissions dépôts (contenu, issues, métadonnées) et organisation en lecture. Le jeton
  d'utilisateur a une durée limitée (8 h) : une session claude ouverte plus longtemps perd l'accès git. L'interface
  l'indique, et la documentation conseille un PAT pour les longues sessions.
- L'en-tête `X-Forwarded-Access-Token` reçu d'une autre source que le sidecar est ignoré (même règle que
  `X-Forwarded-User`, `auth.md` §2).

## 9. Routage par utilisateur (P1) — formes livrées en LOT-2

- Une identité → son espace : une instance d'état d'interface par utilisateur (créée à la première connexion, libérée
  10 min après le dernier client). SSE, terminaux et `/files` sont filtrés par identité : chaque client ne reçoit que
  les événements de son espace (`hello` et `ping` compris), plus de diffusion commune. Admin et modes sans identité :
  inchangés.
- **Instance d'un utilisateur** (LOT-2) : méthodes servies `clientInfo`, `init`, `setPrefs`, `setTheme`, `configFiles`,
  `configOpen`, `configValidate`, `configSave` (fichier `backends.yaml` de l'espace seulement). Méthodes R1 → refus R1 ;
  autres → `error` « … indisponible dans votre espace » (projets, coffre et tests de backend arrivent en LOT-3).
- `init` (utilisateur) : `{state: {rows: []}, cards: null, help, prefs (config.json › affichage), server, access, user, space}`.
- `clientInfo` : `{remote, access, user, space}`.
- `space` = `{admin: bool, login, backends: [{name, type}], empty: bool, error?}`. `empty` vrai → « Ajoute ton premier
  backend ». Jamais d'identifiant numérique ni de chemin serveur.
- `/files/…` pour un utilisateur : `404` en LOT-2 (aucun projet) ; servi par exec à partir de LOT-3.2 (§6).
- Un utilisateur ne voit que ses backends et projets. L'admin voit son espace (la racine) ; il ne voit pas les espaces
  des autres (méthode `userList` seulement, §11).

## 10. Backend injoignable **[NEW]** (#259)

| État d'un backend | Condition | Interface |
|---|---|---|
| `ok` | diagnostic vert | normal |
| `unreachable` | API injoignable (3 échecs, sondage 30 s) | projets de la **trace** affichés « injoignables » (nom et catégorie seulement : l'état vit sur le backend), actions désactivées avec la raison, Purger disponible |
| `forbidden` | identifiants refusés (jeton révoqué, certificat expiré) | « identifiants à renouveler » + lien vers le backend |
| `degraded` | droits insuffisants (verbe manquant), PVC/volume absent | message précis (verbe, ressource) |
| `refused` | ssh en mode serveur (§3) | « ssh interdit en mode serveur » |

- Terminaux : exec coupé → message dans le terminal, **rattachement automatique** au retour (session tmux intacte sur le
  backend). Écritures refusées hors ligne, jamais mises en file.
- Alerte après 5 min (pastille persistante) ; audit `backend.unreachable` / `backend.ok` (avec durée).

## 11. Méthodes web **[NEW / CHANGED]**

| Méthode | Rôle | Effet |
|---|---|---|
| (existantes) `configOpen/Save/Validate` sur `backends.yaml`, `secretSet/Delete/Status`, `backendTest`, `create`, `open`, `refresh`… | user | **portée = espace de l'identité** ; fichiers de l'admin réservés à l'admin (R1 inchangée) ; `refresh` démarre un projet arrêté (§5) |
| `userBackendAdd` | user | **création atomique** d'un backend avec ses secrets (§3 « Création ») ; validation complète, puis écriture tout ou rien ; réponse `{ok, backend, warnings}` ou `{ok:false, field, code, msg}` ; aucun test automatique |
| `userBackendRemove` | user | retire la déclaration **et** le coffre sous le même verrou (coffre orphelin purgé ; « backend inconnu » sans l'un ni l'autre ; échec de suppression du coffre → erreur « réessaie ») ; plafond des dépôts (60/min) ; ses projets disparaissent de l'arbre (traces purgées après confirmation), rien n'est supprimé chez l'utilisateur |
| `githubTokenMode` | user | `pat` / `oauth` (§8) |
| `projectPurge` | user | efface la trace d'un projet introuvable (§5) ; jamais de suppression sur le backend |
| `userList` | admin (R1, `auth.md` §4) | `{users: [{login, backends, state}]}` : login, nombre de backends, état ; jamais d'identifiant numérique |

- Audit : `user.backend.add/remove`, `user.vault.set`, `backend.unreachable/ok`, `backend.refused`, `project.refreshstart`,
  `project.purge`.

## 12. Sécurité — règles vérifiables

1. Le processus du launcher n'exécute **aucun** `git` ni lecture de fichier sur le stockage d'un utilisateur tiers (test :
   espace tiers simulé, traçage des appels `exec.Command` / `os.Open`).
2. Kubeconfig avec `exec` ou `auth-provider` → refus à l'enregistrement (test).
3. Compte k8s trop puissant → avertissement avec la Role minimale, enregistrement accepté (P19, faux `SelfSubjectAccessReview`). ssh (type, `ssh://`, pool ssh) en mode serveur : écarté au chargement avec diagnostic, refusé à l'enregistrement ;
   conservé en mode poste (tests des deux modes).
4. docker/podman sans TLS ou sans vérification : accepté avec l'avertissement « root sur l'hôte » (test du texte) ;
   `unix://` pour un utilisateur → refus.
5. Coffre : `0700`/`0600`, jamais journalisé ni rendu (test) ; chiffrement = dette.
6. Aucune donnée d'un utilisateur visible par un autre (SSE, terminaux, `/files`, état) — test d'isolement critique.
7. Jeton OAuth : jamais écrit sur disque ni dans un journal (recherche dans `audit.jsonl`, journaux, `users/`).
8. `/files` d'un projet arrêté → `404` sans démarrage ; F5 → démarrage sans session claude ; aucun arrêt automatique ;
   le sondage ne relance jamais un projet arrêté (P20 tranché).
10. **Aucune donnée projet dans le launcher** : `users/<uid>/` ne contient que `backends.yaml`, `vault/`, `config.json`,
   `traces.json` (champs de §2 seulement) — test de schéma. Purger n'émet aucune commande vers le backend.
11. Identifiants **collés** seulement : aucune API ne lit un chemin local ; contrôles PEM/YAML/taille (test).
12. Approbation : un `trust.json` sans HMAC valide (forgé, copié pour un autre contenu, clé changée) est ignoré et
   l'approbation redemandée (tests) ; la clé d'approbation n'est jamais envoyée au backend.
9. Aucun PVC ni volume créé ou supprimé par le launcher (test : kubectl/docker factices, aucun `create pvc` / `volume create`).
