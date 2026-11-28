# Configuration et projets en conteneur

> Autres guides : [Installation et lancement](installation.md) · [Launcher Windows natif](windows.md) · [Déploiement dans un cluster Kubernetes](kubernetes.md) · [Interface](interface.md) · [Versions, mises à jour et template](versions.md) · [Architecture](architecture.md) · [Dépannage](depannage.md) · [Tests](tests.md). Vue d'ensemble : [README](../README.md).

**Sommaire**

- [Projets en conteneur (v5)](#projets-en-conteneur-v5)
  - [Déclarer l'environnement : `<projet>/.claude-launcher/env.yaml`](#déclarer-lenvironnement--projetclaude-launcherenvyaml)
    - [Environnement fourni par le template : `env-template.yaml` (v5.5.0)](#environnement-fourni-par-le-template--env-templateyaml-v550)
    - [Bootstrap du launcher : `env.yaml` à côté de `backends.yaml`](#bootstrap-du-launcher--envyaml-à-côté-de-backendsyaml)
    - [Ce qui est construit reste dans le conteneur](#ce-qui-est-construit-reste-dans-le-conteneur)
  - [Configurer l'exécution d'un projet](#configurer-lexécution-dun-projet)
    - [Organisation des panes : `top` ou `left` (v6.2.0, #153)](#organisation-des-panes--top-ou-left-v620-153)
  - [Backends : `backends.yaml`](#backends--backendsyaml)
    - [Réglages : fenêtre `⚙ config` (v6.1.0, #118)](#réglages--fenêtre--config-v610-118)
    - [Backends distants](#backends-distants)
    - [Groupe de serveurs (docker, podman, ssh)](#groupe-de-serveurs-docker-podman-ssh)
    - [Pools de machines nommés](#pools-de-machines-nommés)
    - [Kubernetes](#kubernetes)
  - [À l'ouverture](#à-louverture)
  - [Fichiers ouverts dans le navigateur : serveur web du launcher (v5.10.0)](#fichiers-ouverts-dans-le-navigateur--serveur-web-du-launcher-v5100)
    - [Le bon navigateur : appariement (v5.10.0)](#le-bon-navigateur--appariement-v5100)
    - [Option avancée : `browser_paths` (v5.9.0)](#option-avancée--browser_paths-v590)
  - [Clavier dans un projet en conteneur](#clavier-dans-un-projet-en-conteneur)
  - [Launcher arrêté, hub fermé](#launcher-arrêté-hub-fermé)
  - [Fermer un projet](#fermer-un-projet)
  - [Rafraîchir et forcer la fermeture (v6.2.0, #158)](#rafraîchir-et-forcer-la-fermeture-v620-158)
  - [Projets d'un utilisateur en mode serveur (v7.1.0, #275)](#projets-dun-utilisateur-en-mode-serveur-v710-275)
  - [Plusieurs launchers sur un même projet](#plusieurs-launchers-sur-un-même-projet)
  - [Config de claude dans les conteneurs](#config-de-claude-dans-les-conteneurs)
  - [Consommation du compte : un seul cache pour tous](#consommation-du-compte--un-seul-cache-pour-tous)
  - [Changement de version majeure](#changement-de-version-majeure)

---

## Projets en conteneur (v5)

Un projet peut tourner dans **son propre conteneur** au lieu de l'OS hôte : les
outils, bibliothèques et paquets qu'il installe restent dans le conteneur, et
chaque projet choisit sa distribution (Debian, Rocky, Alpine…). Sans
déclaration, rien ne change : le projet tourne sur l'hôte comme en v4.

### Déclarer l'environnement : `<projet>/.claude-launcher/env.yaml`

Versionné avec le projet :

```yaml
image: docker.io/library/debian:12   # obligatoire ; image « vanilla », rien à préinstaller
packages: [python3-venv, libpq-dev]  # en plus de tmux git bash sudo curl (implicites)
packages_by_manager:                 # noms qui diffèrent selon la distribution
  dnf: [libpq-devel]
setup:                               # exécuté une fois, en utilisateur, dans le projet
  - "[ -d .venv/bin ] || python3 -m venv .venv"
  - .venv/bin/pip install -r requirements.txt
watch: [requirements.txt]            # contenu modifié → setup rejoué (seul) à l'ouverture
on_start:                            # à chaque démarrage du projet (pas au rattachement)
  - .venv/bin/python manage.py migrate --check
local: [.venv]                       # dossiers privés au conteneur (voir ci-dessous)
mounts:                              # dossiers en plus (lecture seule par défaut)
  - path: ../claude_project_template
prefer: podman                       # backend proposé par défaut (facultatif)
```

Le conteneur ne monte que **le dossier du projet** (au même chemin que sur
l'hôte) et `~/.claude` (conversations, teams, réglages, connexion : partagés).

**Éditer le projet** (v6.1.0, #148) : clic droit sur un projet de l'arbre, puis **Ouvrir**, **Fermer** (grisé si
fermé), **⚙ Réglages du projet…**, **Changer de backend…** (F2), **Environnement du conteneur…**, **Couleur…**,
**Éditer env.yaml en YAML**. Les réglages ouvrent la rubrique **Projet** de la fenêtre Réglages
(`⚙ config`) ; « Changer de backend… », « Environnement du conteneur… » et « Couleur… » y placent le curseur sur
leur champ. « Éditer env.yaml en YAML » ouvre l'éditeur de texte sur le fichier du projet.

La rubrique **Projet** contient :

- **Exécution** : le backend (`host` ou backend conteneur) et l'image de base (liste : `images:` de `backends.yaml` ou socles par défaut, plus l'image courante ; « Autre… »
  pour une saisie libre), modifiables **projet fermé
  seulement** (ouvert ici, ou par l'autre launcher qui le tient, ils sont désactivés avec un message) ; le backend
  préféré ; la couleur du projet (pastilles de la palette `COLOR_PALETTE`, ou automatique).
- **Environnement du conteneur** : image, paquets (par gestionnaire, `apt: git jq`), `setup`, `on_start`, `watch`,
  `local`, montages (`chemin[:rw]`). Un montage sans `:rw` est **en lecture seule**. Un backend `host` n'a pas
  d'image : le champ est ignoré.
- **Aperçu fusionné** : l'environnement tel qu'il sera lancé, ligne par ligne, avec la provenance de chaque élément
  (`template` en violet, `launcher` en bleu, sinon le projet) ; il se rafraîchit pendant la saisie.

**Enregistrer** contrôle d'abord le fichier `env.yaml` (validation, conflit, copie `.bak`) : une erreur ou un conflit
(**recharger** ou **écraser**) arrête tout, rien n'est écrit. Le backend, l'image et la couleur sont écrits ensuite.
Si une étape échoue après l'écriture de `env.yaml`, la fenêtre indique ce qui a été enregistré et ce qui ne l'a pas
été. Seule la couleur du projet est écrite dans `config.conf` (les autres lignes, commentaires compris, sont
conservées). Le pied de la rubrique propose « Éditer env.yaml en YAML », Annuler et Enregistrer.

Limites : la couleur d'un projet déjà ouvert n'est prise qu'à sa prochaine ouverture ; la liste des montages est
réécrite en bloc (ses commentaires internes sont perdus) ; `env.yaml` est reformaté à l'enregistrement, sans diff
minimal.

#### Environnement fourni par le template : `env-template.yaml` (v5.5.0)

Deux fichiers versionnés, chacun avec **un seul propriétaire** :

| Fichier | Propriétaire | Écriture |
|---|---|---|
| `.claude-launcher/env-template.yaml` | le template (`/init-project`), selon la stack | réécrit entièrement à chaque passage : ne pas le modifier |
| `.claude-launcher/env.yaml` | le projet | à la main ; le launcher n'y touche que pour `image:` (F2) |

L'un des deux suffit. NG2 les fusionne (après le bootstrap du launcher, ci-dessous) :

- `image`, `prefer` : ceux d'`env.yaml` l'emportent ; `prefer` est refusé dans
  `env-template.yaml` (choix du backend, propre au projet) ;
- `packages`, `packages_by_manager`, `watch`, `local` : réunis ;
- `setup`, `on_start` : étapes du template, puis celles du projet ;
- `mounts` : réunis par `path`, le `ro` d'`env.yaml` l'emporte.

Un projet n'enlève rien au template. Toute modification de l'un ou l'autre fichier
rejoue le bootstrap : les étapes doivent pouvoir être rejouées. Le bootstrap est
**additif** : un paquet retiré d'un fichier reste installé dans le conteneur
existant (rien n'est désinstallé, le conteneur n'est pas recréé pour cela) ; il
disparaît à la prochaine recréation (changement d'`image` ou des dossiers `local`). Un `env.yaml` seul
se lit exactement comme avant (empreinte inchangée, conteneur non recréé).

#### Bootstrap du launcher : `env.yaml` à côté de `backends.yaml`

Ce qui sert à **vous** plutôt qu'à un projet (statusline, hooks, outils de
votre `settings.json`) se déclare une fois pour tous vos conteneurs dans
`<config>/env.yaml`, à côté de `backends.yaml` (ex.
`/share/MFS/ROOT/CLAUDE_PROJETS/.config/claude-launcher-ng2/env.yaml`) :

```yaml
# bootstrap du launcher : ajouté à celui de chaque projet
packages: [python3, jq]          # ex. statusline en python
setup:
  - pip install --user --break-system-packages rich
```

Clés admises : `packages`, `packages_by_manager`, `setup`, `on_start` (les
autres, comme `image` ou `local`, regardent un projet et sont refusées avec un
message clair). Les paquets sont réunis à ceux du projet, les étapes du launcher
passent **avant** celles du projet. Modifier ce fichier rejoue le bootstrap de
chaque conteneur à sa prochaine ouverture ; sans lui, rien ne change.

Exemple vécu : une statusline `bash … statusline-command.sh` qui lit son JSON
avec `python3` reste vide dans un conteneur `ubuntu:24.04`, qui n'a pas python :
`packages: [python3]` ici la répare partout.

#### Ce qui est construit reste dans le conteneur

Le dossier du projet est partagé (hôte, autres conteneurs, autres machines) : il
ne devrait contenir que les sources, les verrous de dépendances et `env.yaml`.
Ce que `setup` construit dépend de l'image (glibc ou musl, chemins des
interpréteurs) :

- **`local:`** — chemins relatifs au projet (`node_modules`,
  `web/node_modules`, `.venv`…) **privés au conteneur** : un volume du conteneur
  (volume anonyme podman/docker, `emptyDir` en k8s) est monté par-dessus et
  **masque** le dossier du projet. Les outils les trouvent à leur place
  habituelle ; l'hôte garde son propre `node_modules` sans conflit. Ils
  appartiennent à l'utilisateur et vivent autant que le conteneur : recréé
  (image changée, liste `local` modifiée, pod perdu), il repart de volumes
  neufs, reconstruits par `setup`. Le moteur crée au besoin, dans le projet, le
  dossier vide servant de point de montage.
- **Caches** (pip, npm, go, uv…) : rien à faire, ils sont sous `$HOME`
  (`~/.cache`, `~/.npm`, `~/go`), qui est déjà propre au conteneur (seul
  `~/.claude` est partagé).
- **`watch:`** — fichiers dont le **contenu** est comparé à chaque ouverture :
  s'il a changé depuis le dernier `setup` (verrou de dépendances modifié), les
  étapes `setup` sont rejouées, seules, sans réinstaller les paquets système.
- **`on_start:`** — étapes rejouées à chaque démarrage d'une session du projet
  (jamais au rattachement à une session vivante) ; à l'auteur de les garder
  rapides (`[ -d node_modules/.bin ] || npm ci`).

Les étapes `setup` et `on_start` tournent en utilisateur, dans le projet, avec
`bash -e` : une erreur arrête l'ouverture (et `setup` sera retenté à la suivante).

### Approbation d'un dépôt cloné (v6.6.0, #215)

Un dépôt cloné n'est pas de confiance. S'il apporte `.claude-launcher/env.yaml` ou `env-template.yaml`
(image, paquets, `setup`, `on_start`, `watch`, `mounts`, `local`), ce fichier n'est **jamais appliqué sans accord** :
à la première ouverture du projet en conteneur, le launcher demande **Accepter**, **Ouvrir sans** (le projet
s'ouvre avec la seule image choisie, rien d'autre du dépôt) ou **Annuler**.

- **Quand c'est redemandé** : dès que `env.yaml` ou `env-template.yaml` change (par exemple après un `git pull`),
  même si l'approbation précédente était « Accepter ». La question présente l'environnement courant ; elle n'affiche
  pas de différence.
- **Revoir** : dans les réglages du projet, « Revoir » remet l'approbation en attente ; la question revient à la
  prochaine ouverture.
- **Limite** : les scripts appelés par `setup` / `on_start` ne sont pas couverts par l'approbation. Accepter un
  environnement, c'est accepter que ces scripts s'exécutent dans le conteneur.
- **Registre `trust.json`** : il est partagé sous `<CL_ROOT_BASE>/.config/claude-launcher-ng2/` (exe Windows, hub et pod
  s'accordent), jamais dans le projet. Une copie `trust.json.bak` est faite avant chaque écriture.
- **Registre illisible** : le launcher refuse les projets concernés et ne réécrit rien. Message affiché :
  « registre d'approbation illisible (<chemin>) : restaurez trust.json.bak ou supprimez trust.json pour repartir de zéro ».
  Pour récupérer, restaurez `trust.json.bak`. Ou supprimez `trust.json` : **supprimer `trust.json` lève toutes les
  approbations** ; les projets clonés devront être approuvés à nouveau.

### Déplacer ou renommer un projet (v6.6.0, #216)

Un projet se déplace vers une autre catégorie, une autre base, ou se renomme : clic droit sur le projet dans
l'arbre ou sur sa carte, « Déplacer / renommer… ». Dans la coque tmux, la touche `m` dans F2.

- **Avant de valider**, le dialogue montre ce qui change : nouveau chemin, nouvelle clé (nom de la fenêtre et du
  conteneur), affectation de backend, réglages (couleur, organisation, masquage), historique de conversations,
  conteneur, et l'approbation d'un dépôt cloné (« conservée »).
- **Bloqueurs** : projet ouvert dans ce launcher (proposer « Fermer le projet »), ouvert par un autre launcher,
  conteneur ou pod en cours d'exécution, déplacement inachevé. Destination identique, existante ou hors des bases
  refusée. Rien n'est déplacé dans ces cas.
- **Sur un autre système de fichiers** (ex. disque local vers base sur partage 9P), le dossier est copié, son contenu
  est contrôlé fichier par fichier (SHA-256) puis la source est supprimée. Le déplacement peut être long.
- **Ce qui suit le projet** : son historique de conversations, ses réglages et son approbation d'environnement.
  Le conteneur ou le pod est **supprimé** et recréé au nouveau nom à la prochaine ouverture.
- **Interruption** : si le launcher s'arrête en plein déplacement, il propose au relancement « Reprendre » ou
  « Annuler ». Un projet à moitié copié n'apparaît jamais comme projet ; l'annulation remet `config.conf` à
  l'équivalent de l'état d'origine (mêmes clés et valeurs, pas à l'octet près).

### Modifier la configuration à chaud (v6.6.0, #225)

Un changement de `config.conf` est pris en compte sans relancer le launcher. Le fichier est relu dès qu'il change
(par date et contenu), avant toute action qui en dépend : ouverture d'un projet, création, lecture des milestones.
Le `settings.json` des projets est recopié à l'enregistrement de la fenêtre `⚙ config` et à l'ouverture d'un projet ;
il ne reste jamais plus ancien que `config.conf`.

- **Fichier invalide** : le launcher garde la dernière copie valide et l'indique en pied de fenêtre. Corrigez le
  fichier, la relecture se fait toute seule.
- **Fichier absent** (ex. supprimé puis recréé dans un éditeur) : les réglages en mémoire sont gardés pendant 3 s ;
  s'il est toujours absent après, les valeurs par défaut s'appliquent. Il est relu dès qu'il revient.
- **Ce qui reste « relance du launcher »** : les racines (`Roots`) et `CLAUDE_CONFIG_DIR`, lus une seule fois au
  démarrage. Après l'enregistrement, l'interface propose « Relancer le launcher ».
- **Env. d'un projet (`env.yaml`)** : il n'est pas modifiable pendant que le projet est ouvert ici, ouvert par un
  autre launcher, ou que son conteneur tourne : l'interface le met en lecture seule et le serveur refuse l'écriture
  avec la raison. Si le backend ne répond pas, l'écriture reste permise avec un avertissement « état du conteneur
  inconnu » ; un changement de backend ou d'image reste refusé dans ce cas.

### Délai d'écriture sur le partage (v6.7.0, #220)

Un clic qui modifie `config.conf` (bascule d'organisation, œil de masquage, couleur d'un projet) écrit sur le partage
du launcher. Si le partage ne répond plus, l'interface ne reste pas bloquée : la réponse revient au bout de
`SHARE_WRITE_TIMEOUT` secondes.

```ini
SHARE_WRITE_TIMEOUT=10
```

- **Défaut** `10`, bornes `2` à `120` (valeur hors bornes ramenée dans l'intervalle). Rubrique « Projets » de la fenêtre
  `⚙ config`, effet immédiat.
- **Délai dépassé** : la bascule est annulée (l'organisation précédente revient) et le message indique le délai, par
  exemple « partage injoignable : config.conf non écrit après 10 s — retour à l'organisation précédente ».
- **L'écriture abandonnée continue** en arrière-plan. Si elle aboutit plus tard, le fichier fait foi : la relecture
  applique ce qui a été écrit.
- **Une seule écriture abandonnée à la fois** par fichier : tant qu'elle est bloquée, les clics suivants échouent
  tout de suite avec le même message.
- **Reprise automatique** : dès que le partage répond, les clics suivants écrivent normalement, sans relance.
- L'écran `⚙ config` (enregistrement explicite) n'est pas borné par ce délai.

Pour le diagnostic, voir [Dépannage](depannage.md) (puce « Partage injoignable »).

### Notifications d'attente du teamleader (v6.7.0, #223)

Quand le teamleader d'un projet attend une réponse (question `AskUserQuestion`, demande de permission de claude), le
projet est signalé par un badge « ❓ en attente » ; voir [Interface](interface.md). L'option suivante ne concerne que
les notifications système et du navigateur, pas le badge :

```ini
ATTENTION_NOTIFY=1
```

- **Défaut** `1` (activées), rubrique « Teams et panes » de la fenêtre `⚙ config`, effet immédiat.
- `0` : plus aucune notification système ni navigateur ; le badge, la bordure et le titre restent.
- **Fenêtre native** : une notification par attente, retirée dès que l'attente se termine. Si les notifications
  système sont indisponibles, seul le badge est affiché.
- **Navigateur (`--web`)** : l'autorisation est demandée par un bandeau « Autoriser les notifications d'attente »,
  affiché tant que la permission n'est pas donnée. Un refus retire le bandeau. Hors `127.0.0.1` / `localhost` (contexte
  non sécurisé), il n'y a pas de notification.
- **Hooks** : posés dans `<CLAUDE_CONFIG_DIR>/settings.json` par le launcher. Une session ouverte avant leur pose
  ne signale rien tant qu'elle n'est pas relancée.

### Effet d'une option et éléments cachés (v6.6.0, #226, #227)

**Pastille d'effet.** Chaque option de la fenêtre `⚙ config` porte une pastille qui dit quand elle prend effet.
Survolez-la, donnez-lui le focus au clavier ou cliquez dessus pour ouvrir la bulle (elle se ferme en quittant la
pastille, par Échap ou par un second appui).

- **immédiat** : appliqué dès l'enregistrement (ex. couleurs, masquage des teammates inactifs).
- **prochaine ouverture** : pris en compte à l'ouverture suivante d'un projet (ex. options de lancement).
- **relance des sessions claude** : les sessions claude déjà ouvertes gardent l'ancienne valeur ; les nouvelles
  l'ont (ex. désactivation de la souris de claude). Dans la coque tmux, la bulle indique la marche à suivre.
- **relance du launcher** : les racines et `CLAUDE_CONFIG_DIR`. Voir [Modifier la configuration à chaud](#modifier-la-configuration-à-chaud-v660-225).

**Rappel après enregistrement.** Un bandeau ne liste que les options modifiées, avec leur effet. Il propose, sans rien
faire seul, « Rafraîchir » les projets ouverts concernés, ou « Relancer le launcher ». Rien n'est relancé ni
rafraîchi automatiquement.

Pour `backends.yaml`, le hub et `env.yaml`, la note de relecture reste affichée après enregistrement, tant que ces
types n'ont pas leur pastille.

**Éléments cachés.** Un mot de passe ou un jeton (ex. `GITHUB_TOKEN`) est masqué. L'œil dans le champ affiche la valeur :

- la valeur reste en mémoire le temps de l'affichage seulement : elle est re-masquée au clic, après 30 s, au
  changement de rubrique et à la fermeture de la fenêtre ;
- elle n'est jamais écrite dans un champ éditable ni dans le stockage du navigateur ;
- au plus 10 révélations par minute et par session ; un refus n'en consomme pas ;
- le journal des écritures de secrets consigne « révélé : clé, interface, heure », jamais la valeur ;
- **accès local seulement** : la révélation est refusée depuis un accès distant (adresse publique du serveur web).
  Le message est « indisponible à distance — afficher depuis la fenêtre native ou l'accès local ». Un
  `kubectl port-forward` vers le pod est un accès local : l'œil y est actif (l'accès kube donne déjà accès au pod).

### Configurer l'exécution d'un projet

Le formulaire **Exécution** propose `0 hôte` puis les socles (`↑↓` ou un
chiffre), et le backend conteneur (`←→`). Choisir un socle écrit un
`env.yaml` minimal et commenté (ou change sa ligne `image:` s'il existe ; rien
n'est écrit si `env-template.yaml` fournit déjà ce socle) et
affecte le projet à ce backend ; `0 hôte` le remet sur l'OS (l'`env.yaml` est
gardé). Il s'ouvre :

- à la **création** (« ✦ Nouveau projet »), après le type de projet ;
- à la **première ouverture** d'un projet jamais configuré : le choix est fait
  une fois, puis ↵ ouvre le projet (s'il n'existe aucun backend conteneur, rien
  n'est demandé) ;
- par **F2** sur un projet fermé, **prérempli avec son état** (socle et backend
  actuels, ou « hôte ») : c'est la reconfiguration.

Un projet configuré se lance **uniquement** sur son backend : pour le lancer
ailleurs, le reconfigurer par F2. Si sa configuration n'est plus valable
(backend retiré de `backends.yaml`, `env.yaml` et `env-template.yaml` absents,
ou l'un d'eux illisible), il est
marqué `⚠` et son ouverture est refusée — jamais de repli sur l'hôte.

Le mode et le socle **présélectionnés** se règlent dans `backends.yaml` :

```yaml
defaults:
  backend: podman                         # host : « hôte » présélectionné
  image: docker.io/library/debian:12
images:                                   # socles proposés (facultatif)
  - docker.io/library/debian:12
  - docker.io/library/ubuntu:24.04
  - docker.io/library/rockylinux:9
  - docker.io/library/alpine:3.20
```

Sans `images`, ce sont ces quatre-là (bootstrap vérifié sur chacun). Un
`defaults.image` hors de la liste y est ajouté en tête.

#### Organisation des panes : `top` ou `left` (v6.2.0, #153)

La fenêtre d'un projet (coque tmux et interface native) a deux organisations :

- **`top`** (défaut, inchangé) : leader et `cdp` en haut ; les teammates sur une ligne jusqu'à 5, le haut sur 50 % ;
  au-delà, 3 lignes 3/7, 2/7 et le reste.
- **`left`** : leader et `cdp` empilés à gauche, teammates à droite ; une colonne jusqu'à 4 teammates, deux colonnes à
  partir de 5 (la première reçoit ⌈n/2⌉).

Réglages :

- **Global** : `LAYOUT_MODE` (`top` | `left`, défaut `top`) et la largeur du leader `LAYOUT_LEFT_LEADER_PCT` (20 à 80,
  défaut 46). Dans `config.conf`, ou dans Réglages › Launcher, boîte « Mise en page des panes » en tête de la rubrique.
- **Par projet** : `PROJECT_LAYOUTS`, champ « Mise en page » de **⚙ Réglages du projet…**, placé avant « Exécution »
  (« Comme le réglage global » retire l'exception).
- **Bouton de bascule** (interface native) : tout à droite de la barre d'onglets, il fait passer le projet affiché de
  « leader en haut » à « leader à gauche », et inversement. Le choix est enregistré comme exception du projet. Il est
  grisé sur l'onglet Teamleaders. La clé est le **nom du dossier** du projet, comme `PROJECT_COLORS` : deux projets de même nom
  partagent l'exception. Priorité : `PROJECT_LAYOUTS` > `LAYOUT_MODE` > `top`.

```bash
# ~/.config/claude-launcher-ng2/config.conf
LAYOUT_MODE=left
LAYOUT_LEFT_LEADER_PCT=45
declare -A PROJECT_LAYOUTS=(
  ["mon-projet"]="top"
)
```

Le changement s'applique aux projets ouverts sans relance : la coque et l'interface native relisent `config.conf`, et
la fenêtre est remise en page quand son organisation effective change. Dans l'interface native, ce changement (bouton de
bascule, Réglages, ou `config.conf` modifié à la main) est appliqué **immédiatement** : pour la bascule, la disposition
change dès le clic, avant l'enregistrement de `config.conf` qui suit en arrière-plan. Si l'écriture échoue, le projet
revient à l'organisation précédente avec un message. Plusieurs clics rapides : le dernier l'emporte. Dans la coque Linux,
la réaction prend 1 à 2 s.
Seuls les panes visibles sont disposés. Si tmux
refuse la disposition `left` (fenêtre trop petite), le projet revient à `top` avec un message ; `Ctrl+B R` remet en
ordre dans les deux cas.

### Backends : `backends.yaml`

Sans `~/.config/claude-launcher-ng2/backends.yaml`, les backends conteneur sont podman et docker
**s'ils répondent** (un `docker` de Docker Desktop sans intégration WSL est
ignoré). Pour les nommer ou viser un contexte docker :

```yaml
backends:
  podman: { type: podman }
  docker: { type: docker, context: default }
```

#### Réglages : fenêtre `⚙ config` (v6.1.0, #118)

`⚙ config` dans l'en-tête, ou `Ctrl+,`, ouvre la fenêtre **Réglages** : `backends.yaml` s'édite par formulaires,
sans passer par le texte. Rubriques :

- **Backends** : la liste avec son état (joignable, ou diagnostic en attente) ; un formulaire par type (`host`,
  `podman`, `docker`, `ssh`, `k8s`) avec ses serveurs (adresse, groupe, pool) ; ajouter, dupliquer, supprimer,
  renommer, choisir le défaut. **Tester** lance les étapes du backend sans rien enregistrer : outil requis, moteur
  joignable, stockage partagé ; pour `k8s`, namespace, droits, PVC. Délai global de 45 s ; une étape en échec
  propose son correctif.
- **Pools** : les machines d'un pool, le drain, la charge commune (conteneurs en marche par machine). Renommer un
  pool le renomme aussi dans les backends qui le citent.
- **Défauts** : backend et image par défaut, images proposées. Le serveur web et les chemins navigateur s'affichent
  en lecture seule.
- **Launcher, Conteneurs, Projet** : renvoi à « Éditer en YAML » pour l'instant. **Ce poste** : tailles de texte
  de l'arbre, des cartes et des terminaux (rubrique Affichage, voir ci-dessous), le reste renvoie à « Éditer en YAML ».

**Enregistrer** : le brouillon est validé comme au chargement réel. Une erreur **bloque** l'enregistrement (les
problèmes s'affichent en pied, rien n'est écrit). Si le fichier a changé sur le disque depuis son ouverture, la
fenêtre propose de **recharger** ou d'**écraser**. La version précédente est gardée en `.bak`. Un backend par défaut
supprimé ou renommé est mis à jour dans le même enregistrement ; une référence restante à un backend ou un pool
inconnu est refusée. Une adresse ou un contexte commençant par `-` est refusé. L'application se fait à chaud quand
c'est possible, et une note le précise.

Les commentaires et les clés inconnues sont conservés, mais **le fichier est reformaté** à l'enregistrement : lignes
vides entre entrées retirées, alignement et guillemets normalisés. Ne pas attendre un diff minimal.

Limites : le `host` du backend implicite Windows n'est pas éditable ; les volumes `k8s` s'écrivent une ligne
`racine=PVC` par volume.

#### Backends distants

Quatre façons d'atteindre un moteur de conteneurs, plus Kubernetes :

```yaml
backends:
  srv1-docker: { type: docker, host: tcp://192.0.2.21:2375,  shared: [/share/MFS/ROOT] }
  srv2-podman: { type: podman, host: tcp://192.0.2.22:8888,  shared: [/share/MFS/ROOT] }
  srv1-sshd:   { type: ssh, host: srv1, engine: docker,          shared: [/share/MFS/ROOT] }
  srv2-sshp:   { type: ssh, host: srv2, engine: podman,          shared: [/share/MFS/ROOT] }
```

| Type | Accès | Côté machine distante |
|---|---|---|
| `docker` + `host` | **direct** au démon (`docker -H`, ou `context`) | démon exposé (`daemon.json` → `"hosts": [… "tcp://0.0.0.0:2375"]`, TLS : 2376) |
| `podman` + `host` | **direct** au service podman (`podman --url`) | `podman system service --time=0 tcp://0.0.0.0:8888` (API non authentifiée ni chiffrée : réseau de confiance) |
| `ssh`, `engine: docker` | `ssh <host> docker …` | docker dans le PATH d'une commande ssh, utilisateur dans le groupe `docker` |
| `ssh`, `engine: podman` (défaut) | `ssh <host> podman …` | podman dans le PATH d'une commande ssh |

#### Groupe de serveurs (docker, podman, ssh)

Un backend docker, podman ou ssh peut viser **plusieurs serveurs** (`hosts:`
à la place de `host:`). Le projet est affecté au groupe (F2 propose son nom),
et **le launcher choisit le serveur** :

```yaml
backends:
  docker-pool: { type: docker, shared: [/share/MFS/ROOT],
                 hosts: [tcp://192.0.2.21:2375, tcp://192.0.2.22:2375, tcp://192.0.2.23:2375] }
  ssh-pool:    { type: ssh, engine: podman, shared: [/share/MFS/ROOT], hosts: [srv1, srv2] }
```

- **Aucun registre** : les conteneurs eux-mêmes font foi (labels
  `cl.launcher`, `cl.project`, `cl.backend`) ; le serveur d'un projet est celui
  où l'on trouve son conteneur. Rien à perdre ni à resynchroniser.
- **À l'ouverture**, tous les serveurs du groupe sont interrogés (en
  parallèle) : le projet garde le serveur de son conteneur, même arrêté (son
  bootstrap, son `$HOME` et ses dossiers `local:` y sont) ; sans conteneur, il
  va sur le serveur qui a **le moins de conteneurs NG2 en marche**. Un verrou
  (`.claude-launcher/.lock`, le temps de la création) empêche deux launchers de
  le créer sur deux serveurs.
- **Entre deux launchers**, le scan régulier du navigateur (10 s) parcourt
  tous les serveurs : un projet ouvert par l'autre apparaît en 10 s au plus,
  avec son point (`■` orange piloté ailleurs…).
- **Serveur injoignable** : le groupe reste utilisable tant qu'un serveur
  répond ; à l'ouverture d'un projet introuvable, le launcher demande s'il faut
  démarrer ailleurs (son conteneur est peut-être sur le serveur muet). Chaque
  commande de listing est bornée à 8 s.
- **Doublon** (le même projet sur deux serveurs) : point `✕` ; Entrée demande
  lequel garder, les autres sont supprimés.
- **Rééquilibrer** : supprimer le conteneur d'un projet ; il est replacé à la
  prochaine ouverture.
- `claude-launcher-ng2 backends` détaille chaque serveur : état, conteneurs,
  problèmes. En ssh, une connexion persistante (`ControlMaster`) sert toutes les
  commandes vers un même serveur.

#### Pools de machines nommés

Quand plusieurs backends visent les mêmes machines (docker **et** ssh, par
exemple), la liste se déclare **une fois** dans `pools:` ; chaque backend la
cite par `pool:` (à la place de `hosts:`) :

```yaml
pools:
  dev:
    hosts: [192.0.2.21, 192.0.2.22, 192.0.2.23]
  gros:
    hosts: [srv6, srv7, srv8]
    drain: [srv7]                # maintenance : plus de nouveaux projets

backends:
  docker-dev:  { type: docker, pool: dev,  host: "tcp://{}:2375", shared: [/share/MFS/ROOT] }
  ssh-dev:     { type: ssh,    pool: dev,  host: "cyril@{}", engine: podman, shared: [/share/MFS/ROOT] }
  docker-gros: { type: docker, pool: gros, host: "tcp://{}:2375", shared: [/share/MFS/ROOT] }
```

- Un pool est **toujours un objet** : `hosts:` (obligatoire) et `drain:`
  (facultatif). Mettre une machine de côté, c'est ajouter une ligne `drain:`,
  sans réécrire la liste ; une liste simple (`dev: [a, b]`) est refusée.
- Avec `pool:`, `host:` est un **modèle d'adresse** : `{}` y est remplacé par la
  machine (`tcp://{}:2375`, `cyril@{}`). Sans `host:`, chaque entrée du pool est
  prise telle quelle (adresses complètes).
- **`drain:`** : la machine ne reçoit plus de **nouveaux** projets ; elle reste
  interrogée, ses projets restent visibles et s'ouvrent normalement (pour la
  vider : supprimer leurs conteneurs, ils sont replacés ailleurs à la
  prochaine ouverture). Si toutes les machines sont en drain, un nouveau projet
  est refusé (« aucun serveur disponible (tous en drain) »).
- **Charge commune** : la charge d'une machine additionne les conteneurs NG2 en
  marche de **tous les backends du pool** (ici docker-dev et ssh-dev) ; le
  choix du serveur interroge donc aussi les autres backends du pool.
- Refusés au chargement : pool inconnu ou vide, machine en double, entrée de
  `drain:` absente de `hosts:`, `pool:` avec `hosts:`, `host:` sans `{}`.
- Retirer une machine du pool alors que des projets y tournent les rend
  invisibles : la passer d'abord en `drain:`, puis la vider.
- `claude-launcher-ng2 backends` affiche le pool, les machines en drain et la
  charge venue des autres backends du pool.

#### Kubernetes

```yaml
backends:
  lab: { type: k8s, context: k8s, namespace: claude-ng2,
         volumes: { /share/MFS/ROOT: claude-shared } }   # racine partagée → PVC ReadWriteMany
```

Un projet = un **StatefulSet** (1 réplique) `<catégorie>-<projet>` ; son pod
`<catégorie>-<projet>-0` (aussi son hostname, imposé par le StatefulSet) monte chaque PVC de `volumes` **au chemin de sa racine**, donc aux mêmes
chemins que sur la machine du launcher (les racines de `volumes` tiennent lieu
de `shared`). Le pod tourne en root pour le bootstrap ; les commandes
utilisateur passent par `su` (`kubectl exec` n'a pas de `-u`). Le chemin du
projet est en annotation (`cl.project`), le listing par labels
(`cl.launcher=ng2`, `cl.backend`).

Prérequis : `kubectl` et son contexte, un namespace où l'on peut créer des
StatefulSets, des PVC ReadWriteMany liés (ex. MooseFS : PV `csi.moosefs.com`
avec `volumeAttributes.mfsSubDir`), et un namespace qui autorise les pods root.
`claude-launcher-ng2 backends` signale ce qui manque, commande à l'appui.
Droits minimaux dans le namespace : StatefulSets (et `statefulsets/scale`),
pods, `pods/exec`, events et PVC en lecture, plus `get` sur le namespace
lui-même (exemple complet : `.claude-launcher/k8s-acces-pod.sh` de ce dépôt).

**Placement des pods** (#60) : par défaut, les pods NG2 d'un backend sont
**répartis sur les nodes**. NG2 ajoute une contrainte `topologySpreadConstraints`
par node (`maxSkew: 1`, `whenUnsatisfiable: ScheduleAnyway`) : un projet de plus
que de nodes ne reste jamais bloqué en `Pending`, et la répartition reste
équilibrée (2-2-1 plutôt que 3-1-1). Le reste se règle sous `pod:`, ajouté au
spec du pod :

```yaml
backends:
  lab:
    type: k8s
    context: k8s
    namespace: claude-ng2
    volumes: { /share/MFS/ROOT: claude-shared }
    spread: true                 # défaut ; false : placement laissé au scheduler
    pod:                         # facultatif
      nodeSelector: { node-role/claude: "true" }
      tolerations: [{ key: dedie, operator: Exists }]
      resources: { limits: { memory: 4Gi } }       # appliqué au conteneur
      # affinity: …  anti-affinité stricte, si on la veut vraiment
```

Clés admises sous `pod` : `nodeSelector`, `affinity`, `tolerations`,
`topologySpreadConstraints` (ajoutées à celle de `spread`), `resources`,
`priorityClassName`, `serviceAccountName`, `automountServiceAccountToken`,
`runtimeClassName`, `schedulerName`, `nodeName`, `imagePullSecrets`, `dnsPolicy`,
`dnsConfig`, `hostAliases`, `terminationGracePeriodSeconds`, `securityContext`.
Conteneurs et volumes restent gérés par NG2 : `backends.yaml` est refusé s'il
les mentionne. `claude-launcher-ng2 backends` affiche le placement et le node
de chaque pod.

Un placement modifié **n'est jamais appliqué à un pod en marche** (le recréer
couperait sa session) : son empreinte (annotation `cl.sched`) est comparée, et
il prend effet au **prochain démarrage du pod**. Pour rééquilibrer un projet,
le fermer (Ctrl+clic) puis le rouvrir.

Pod perdu (nœud, éviction) : le StatefulSet le recrée, la fenêtre se ferme ;
à la réouverture, le bootstrap est refait et la reprise de la conversation est
proposée (le launcher constate que le pod n'a plus de session : le leader est
mort, même si son PID n'est pas vérifiable d'ici).

Le client du moteur doit être installé sur la machine du launcher pour les
accès directs (le client podman ne pilote pas un démon docker). En ssh,
`engine_host` donne l'adresse du démon côté machine distante (docker rootless :
`unix:///run/user/<uid>/docker.sock`, service podman…). Accès ssh par clé, sans
mot de passe.

Pour passer une installation locale sur le stockage partagé (projets, `~/.claude`,
conversations, mémoire, réglages par projet), suivre le guide pas à pas
[MIGRATION.md](../MIGRATION.md), avec exemples et retour arrière : copier les projets ne
suffit pas.

**`shared`** : racines du stockage partagé (NFS, MooseFS…), montées **aux mêmes
chemins** sur la machine du launcher et sur la machine distante. `~/.claude`
(ou `CLAUDE_CONFIG_DIR`) et le projet doivent s'y trouver : le launcher lit
lui-même conversations, teams et marqueurs, et trouve les projets dans son
navigateur. Les dossiers sont montés avec `--mount` : s'ils manquent de l'autre
côté, la création échoue (au lieu d'y créer un dossier vide).

Un backend inutilisable est signalé `⚠` dans le navigateur, et
`claude-launcher-ng2 backends` détaille chaque problème avec la façon d'y
remédier : client ou moteur absent, accès au socket docker refusé, démon
distant non exposé, accès ssh, podman absent de la machine distante, stockage
partagé non monté ou non déclaré, `~/.claude` hors du partage.

L'affectation (y compris « hôte ») est mémorisée dans `projects.json`, à côté de `backends.yaml`
(partagée en mode portable), pas dans le projet.

| Icône | Signification |
|---|---|
| `⬡` gris | `env.yaml` ou `env-template.yaml` présent, projet sur l'hôte (ou conteneur pas encore créé) |
| `⬢` cyan | conteneur en marche |
| `⬢` gris | conteneur arrêté (redémarré à l'ouverture) |

Le point devant le nom dit, lui, où en est **la session claude** (voir
[Fermer un projet](#fermer-un-projet)).

### À l'ouverture

1. Le conteneur `<catégorie>-<projet>` (hostname : `<projet>`) est créé (ou démarré), avec les
   labels `cl.launcher=ng2`, `cl.project`, `cl.backend`, `cl.image`, `cl.local` (empreinte des dossiers `local`).

   **Noms (v5.6.0)** : catégorie et projet réduits à des minuscules, chiffres et
   `-` (20 et 30 caractères au plus), par exemple `CLAUDE_LAUNCHER/Claude-Launcher`
   → conteneur ou StatefulSet `claude-launcher-claude-launcher`. Le chemin complet,
   posé à la création (label `cl.project`, annotation `cl.project` sous k8s),
   départage deux projets de même catégorie et même nom sous des racines
   différentes : le second est refusé avec un message clair (renommer la
   catégorie ou le projet). **Migration** des noms d'avant v5.6.0
   (`cl-<projet>-<empreinte>`, `clx-…`), à l'ouverture du projet : un conteneur
   en marche est renommé sans coupure, et son hostname est corrigé à sa prochaine
   ouverture une fois arrêté (recréé, les dossiers `local` repartent à vide) ; un
   StatefulSet en marche est conservé tel quel, puis recréé sous le nouveau nom à
   la première ouverture après sa fermeture.
2. **Bootstrap** (idempotent, rejoué seulement si `env.yaml` ou `env-template.yaml` change) : paquets
   système (`apt`, `dnf`, `apk`, `zypper`), utilisateur aux uid/gid de l'hôte
   avec sudo, **installation de claude**, étapes `setup`. Compter ~20 s la
   première fois (image en cache), une fraction de seconde ensuite. Si seul un
   fichier `watch` a changé : étapes `setup` seulement. Puis, pour une nouvelle
   session, les étapes `on_start`.
3. Le démarrage habituel (questions `/init-project` et reprise, claude, marqueur
   de sortie propre) s'exécute **dans le tmux du conteneur**, attaché dans la
   fenêtre du projet. Les teammates y sont créés et mis en page comme en local.

Prérequis de l'image : un gestionnaire de paquets connu et **tmux ≥ 3.2** dans
ses dépôts (Debian 12, Rocky/Alma 9, Alpine 3.16+ ; pas UBI sans abonnement
RHEL, ni RHEL 8 et ses dérivés).

### Fichiers ouverts dans le navigateur : serveur web du launcher (v5.10.0)

Le template ouvre dans Chrome (`claude-in-chrome`) des fichiers du projet : maquettes, procédures de
test, aperçus. L'extension refuse les URL `file://`. Le hub sert donc ces fichiers en HTTP, en lecture
seule, et **le chemin de l'URL est le chemin interne** :

```
Claude (session ou conteneur) : /share/MFS/ROOT/x/maquette.html
URL donnée à Chrome           : http://localhost:8765/share/MFS/ROOT/x/maquette.html
```

- Chrome tourne sur la machine du launcher. Sous WSL2, le `localhost` de Windows est redirigé vers
  WSL : il n'y a pas de `\\wsl.localhost\…` à configurer. Les projets en conteneur (podman, docker,
  k8s) sont couverts aussi, puisque le stockage partagé y est monté au même chemin que sur le hub.
- **Ne sont servies que les racines connues** : racines de projets, `shared` des backends, et
  `allow`. Le reste renvoie 404, y compris les `..` et les liens symboliques qui sortent de ces
  racines. L'en-tête `Host` est contrôlé (une page piégée qui ferait pointer son nom vers
  `127.0.0.1` est refusée), il n'y a pas de CORS, et les réponses portent `Cache-Control: no-store`
  pour qu'une maquette modifiée se recharge. Un dossier affiche son `index.html` ou, à défaut, la
  liste de ses fichiers ; `.md` et `.yaml` sont affichés en texte.
- Le serveur tourne dans le pane des cartes du hub, qui est relancé s'il s'arrête (sous Windows :
  dans l'application, sur son propre port, voir [Windows](windows.md#windows-v6)). Le réglage est
  relu toutes les 10 s, et un port occupé est réessayé.
  `claude-launcher-ng2 backends` affiche l'adresse, l'état (`actif : Claude-Launcher-NG2 v…`) et les
  racines servies.

```yaml
# ~/.config/claude-launcher-ng2/backends.yaml — tout est facultatif
defaults:
  browser_server:
    listen: 127.0.0.1:8765        # défaut (exe Windows : 127.0.0.1:8766) ; « 9000 » = 127.0.0.1:9000 ; off = désactivé
    allow: [/data/maquettes]      # dossiers servis en plus des racines connues
hubs:
  serveur:                        # par machine du launcher : l'emporte champ par champ
    browser_server: { listen: "0.0.0.0:8765", url: "http://serveur.maison.lan:8765" }
    windows:                      # par système de cette machine (windows, linux, darwin) :
      browser_server: { listen: "8081" }   # l'emporte champ par champ sur hubs.serveur
```

Ordre de priorité, champ par champ (`listen`, `url`, `allow`) :
`hubs.<machine>.<système>.browser_server` > `hubs.<machine>.browser_server` >
`defaults.browser_server` > défaut (`127.0.0.1:8765`, ou `127.0.0.1:8766` pour l'exe Windows,
voir la section Windows).

Dans Réglages › Ce poste, la rubrique « Serveur de maquettes » affiche la **valeur effective** pour ce poste et ce
système, avec sa provenance : `defaults`, `hubs.<machine>`, le nom du système (`windows`, `linux`, `darwin`) quand une
surcharge le pose, ou rien si aucune couche ne le pose. Modifier un champ l'écrit sur la couche qui gagne ; « retour au
défaut » le retire de toutes les couches qui le posent, sans laisser réapparaître une valeur masquée.

`url` est la base annoncée au template. Elle est déduite de `listen` par défaut : `localhost` en
boucle locale, le nom de la machine si le serveur écoute sur toutes les interfaces. Attention :
écouter ailleurs que sur `127.0.0.1` expose les racines à tout le réseau.

La correspondance est donnée à chaque session claude dans la variable **`CL_BROWSER_PATH_MAP`**,
que le template lit (`/init-project` ne pose plus la question). Avec le serveur, elle vaut
`[{"container":"/","host":"http://localhost:8765","os":"url"}]`. Pour une entrée `os: "url"`, l'URL
est `host` + `/` + le chemin relatif au `container`, chaque segment encodé.

#### Le bon navigateur : appariement (v5.10.0)

L'extension peut être connectée à plusieurs Chrome (autres PC du même compte) ; les sessions
doivent piloter celui de la machine du launcher. Les noms affichés (« Browser 1 », « Browser 2 »)
sont renumérotés selon les connexions ; le **`deviceId`** est stable d'un lancement de Chrome à
l'autre (une 2e fenêtre du même profil garde le même ; un autre profil ou une réinstallation de
l'extension en donne un autre).

- **Détection** : seul le Chrome de la machine du launcher atteint le serveur web (`127.0.0.1`).
  La première session claude qui a besoin de Chrome essaie chaque navigateur connecté sur
  `http://localhost:8765/__claude-launcher__/pair?device=<deviceId>&token=<jeton du hub>` ; la page
  `✓ navigateur du hub <machine>` désigne le bon, et le serveur enregistre son `deviceId`
  (`~/.cache/claude-launcher-ng2/browser-device`). Le jeton, tiré une fois par hub, écarte un autre
  launcher qui écouterait sur le même port d'un autre PC. Plusieurs `✓` (plusieurs profils Chrome) :
  claude vous demande lequel, une fois.
- **Ensuite** : chaque session sélectionne directement ce `deviceId`. S'il n'est pas connecté
  (Chrome fermé), claude le signale au lieu de prendre un autre navigateur.
- **Imposer un navigateur** : `hubs.<machine>.browser_device: <deviceId>` dans `backends.yaml`.
  Refaire la détection : supprimer `~/.cache/claude-launcher-ng2/browser-device`.
- **Rien à faire côté template** : la consigne est donnée par le launcher dans `CL_BROWSER_HINT`
  (posée avec `CL_BROWSER_PATH_MAP`, dans le tmux du hub et celui des conteneurs), qu'un hook
  `SessionStart` ajouté au `settings.json` utilisateur affiche au démarrage de chaque session, et
  après `/clear` ou une compaction. Le hook est en `sh` pur (aucun binaire requis dans les
  conteneurs), n'affiche rien hors launcher, et vos autres hooks sont conservés.
- `claude-launcher-ng2 backends` affiche le `deviceId` retenu et sa source.

#### Option avancée : `browser_paths` (v5.9.0)

Pour ouvrir les fichiers par un autre chemin (partage réseau en `file://`, autre serveur web), on
déclare la correspondance ; elle l'emporte alors sur le serveur. Chrome voit ce dossier ailleurs
que Claude (`\\nas\MFS\ROOT\…`, `C:\…`, `http://…`) :

```yaml
# ~/.config/claude-launcher-ng2/backends.yaml
defaults:
  browser_paths:
    - { container: /share/MFS/ROOT, host: '\\nas\MFS\ROOT' }   # NAS vu depuis Windows
hubs:                                     # facultatif : par machine du launcher (hostname)
  portable:
    browser_paths:
      - { container: /share/MFS/ROOT, host: /mnt/mfs/ROOT }     # portable Linux
```

- `container` : préfixe absolu vu par claude. `host` : le même dossier vu par le navigateur,
  sous la forme `C:\…`, `\\serveur\partage\…` (dont `\\wsl.localhost\<distro>\…`), `/…` ou une base
  `http(s)://…`. `os` (`windows`, `linux` — macOS = `linux` — ou `url`) est facultatif : il est
  déduit de la forme de `host`.
- **Global, surchargeable par poste, pas par backend** : NG2 monte le stockage partagé au même chemin
  dans tous les backends, seul le poste où tourne Chrome change le côté `host`. `backends.yaml` étant
  commun à tous vos hubs, `hubs.<machine>` (nom de la machine du launcher, nom court accepté)
  l'emporte sur `defaults`. `browser_paths: []` : aucun navigateur ne peut ouvrir ces fichiers,
  le template affiche seulement leur chemin.
- **Sans `browser_paths`** : le serveur web ci-dessus. Avec `browser_server: { listen: off }` :
  même chemin des deux côtés pour chaque racine de projets et chaque racine partagée des backends ;
  sous WSL, `host` = `wslpath -w <racine>` et `os: windows`.
- Entrée invalide (`container` relatif, `host` d'une autre forme, `os` inconnu) : ignorée et signalée
  par `claude-launcher-ng2 backends`, qui affiche aussi la correspondance effective et sa source.
- Valeur : tableau JSON compact, préfixe `container` le plus long d'abord, sans séparateur final :
  `CL_BROWSER_PATH_MAP=[{"container":"/share/MFS/ROOT","host":"\\\\nas\\MFS\\ROOT","os":"windows"}]`.
  Elle est toujours posée par un launcher v5.9.0 ou plus récent ; son absence signale un launcher
  plus ancien.
- **Où elle est posée** : dans l'environnement du tmux du hub pour les projets sur l'hôte, et dans
  celui de la session tmux du conteneur pour les autres, dès sa création puis à chaque ouverture ou
  rattachement. Le teamleader et les teammates (panes créés dans ce tmux) en héritent. Elle ne passe
  ni par le `settings.json` commun à tous les hubs, ni par le spec du conteneur ou du pod : une
  config modifiée s'applique à la prochaine ouverture du projet, sans recréer le conteneur. Un claude
  déjà en marche garde l'ancienne valeur jusqu'à sa relance.

### Clavier dans un projet en conteneur

| Touche | Effet |
|---|---|
| `Ctrl+B` + touche | agit dans le conteneur (`z` zoom, flèches, `[` copie, `R` remise en ordre…) |
| `Ctrl+B Ctrl+B` + touche | préfixe du hub (`n` / `p` : fenêtre suivante / précédente…) |
| double-clic | zoom du pane, comme en local |

### Launcher arrêté, hub fermé

La session du conteneur (claude **et** ses teammates) continue de tourner. Au
prochain lancement, NG2 retrouve les conteneurs par leurs labels et recrée la
fenêtre de chaque projet dont la session est vivante : on retrouve la session
telle quelle. Fermer la fenêtre d'un projet en conteneur ne l'arrête pas non
plus : le point passe à `●` orange et Entrée dans le navigateur s'y rattache. Pour
l'arrêter : **Ctrl+clic** sur le projet (voir ci-dessous).

### Fermer un projet

Le point devant le nom du projet dit où en sont son conteneur et sa session
claude, et qui la pilote ; l'icône `⬡`/`⬢` reste l'état du conteneur.

| Point | État | Entrée | Ctrl+clic / Suppr |
|---|---|---|---|
| `○` gris | rien ne tourne : projet sur l'hôte non lancé, pas de conteneur, ou conteneur arrêté | démarre le projet | rien à fermer |
| `●` bleu | conteneur en marche **sans claude** : pas de tmux, ou tmux resté seul après un `/exit` | claude démarre aussitôt, sans attendre de pod (un tmux resté seul est recréé) | arrête le conteneur, sans question |
| `▲` vert | ouvert dans ce hub, **ce launcher a la main** | va à sa fenêtre | ferme, après confirmation |
| `●` vert | ouvert dans ce hub **en lecture seule** (un autre launcher a pris la main) | rejoindre en lecture seule ou prendre la main | refusé : prendre la main d'abord |
| `■` orange | claude tourne, **piloté par un autre hub** | rejoindre en lecture seule ou prendre la main | refusé : prendre la main d'abord |
| `●` orange | claude tourne, **aucun launcher ne le pilote** (hub fermé, launcher arrêté, propriétaire silencieux depuis 20 s) | s'y rattache et en prend le contrôle | ferme, après confirmation |
| `✕` rouge | problème : backend `⚠`, configuration invalide, pod en échec, session interrompue `↺` | comme avant (reprise proposée, erreur expliquée) | abandonne la reprise et arrête le conteneur, après confirmation |

Un projet sur l'hôte ne connaît que `○`, `▲` et `✕` (sa session vit dans le
hub). Le navigateur relit toutes les 10 s chaque conteneur en marche, en une
seule commande : propriétaire de la session et commande du leader (claude ou
son shell).

La question tient dans le pied du navigateur (`Fermer web-shop ? claude + 2
teammate(s) · pod k8s arrêté (rien n'est perdu) · o/N`) ; toute autre touche
qu'`o` annule. La fermeture, ensuite :

1. `/exit` est envoyé à claude (après un Échap qui interrompt une réponse en
   cours) : sortie propre, marqueur posé, pas de `↺` ensuite ;
2. attente de la fin de claude (30 s), puis fermeture de la session tmux,
   teammates compris, cachés compris (voir [Interface](interface.md#interface)) — forcée si claude ne répond pas ;
3. arrêt du conteneur : `podman`/`docker stop` ; k8s : StatefulSet à **0
   réplique** (le pod est libéré, tout est sur le stockage partagé). L'ouverture
   suivante le redémarre (k8s : remise à 1 réplique, ~20 s).

Un projet sur l'hôte s'arrête aux étapes 1 et 2. Un projet piloté par un autre
launcher n'est jamais fermé d'ici, ce qui couperait le travail de quelqu'un
d'autre : Entrée pour prendre la main, puis Ctrl+clic.

`CTR_STOP_ON_EXIT=1` dans `config.conf` (désactivé par défaut) arrête aussi le
conteneur quand claude se termine proprement **de lui-même** (`/exit` tapé dans
le projet) : la session du conteneur se termine et la fenêtre se ferme. Un
simple détachement, lui, n'arrête rien. Sans cette option, le projet passe au
`●` bleu.

### Rafraîchir et forcer la fermeture (v6.2.0, #158)

Le menu du projet (fenêtre native et navigateur) propose deux actions. « Fermer » est disponible dès que la ligne
est ouverte ou que sa pastille n'est pas `○`.

- **⟳ Rafraîchir** (`F5` sur le projet sélectionné dans l'interface native ; `F5` dans la coque Linux) : relit tout le projet
  sans les caches — conteneur ou pod, session, git, GitHub, template, consommation (le délai minimal de l'API après un
  échec est conservé). Un bandeau d'étapes en tête des cartes montre l'avancement (`✓` fait, `⟳` en cours, `✕` erreur,
  `·` en attente). Une erreur reste affichée jusqu'à `×` ou au rafraîchissement suivant. L'étape GitHub relit le
  milestone **sans attendre** la relecture de masse des projets. Si `gh` est absent, non connecté, ou si le dépôt n'a
  pas de remote GitHub, l'étape est **verte** avec la raison affichée (cas sans objet, pas une panne) ; une vraie panne
  est **rouge** et affiche la cause renvoyée par `gh` (authentification, réseau, dépôt introuvable, limite d'API…). Rafraîchir ne retire pas
  l'onglet d'un projet ouvert.
- **Forcer la fermeture…** : proposée quand le projet est affiché ouvert ou en erreur **et** qu'il n'a plus de conteneur,
  de pod ni de session (cas typique : un pod k8s disparu). Une boîte de confirmation indique la raison et liste ce qui
  sera nettoyé. Le nettoyage est local : marque de sortie, verrou périmé, fichier d'ordre, panes cachés orphelins.
  `projects.json` n'est jamais modifié. La fermeture est refusée si un autre launcher pilote le projet, ou si le projet
  revit entre la question et la confirmation (la question expire au bout de 2 min).
  Un pod k8s en marche à l'ancien modèle de montage (avant v7.0.0) est arrêté par « Forcer la fermeture… » comme par « Fermer ».
- **Backend injoignable** : la fermeture forcée est proposée avec l'avertissement « le projet tourne peut-être encore ».
  Seul le nettoyage local est fait : aucun arrêt du conteneur n'est demandé au backend.
- **« dubious ownership » (Windows, dépôt sur un partage)** : git refuse les dépôts dont le propriétaire est un autre
  compte (typiquement le compte Linux qui a créé le dépôt sur le partage). Le launcher n'autorise alors ce dépôt que pour
  ses propres appels `git` et `gh`, et seulement pour le dossier du projet : `safe.directory` est passé à chaque appel,
  la configuration globale n'est jamais modifiée. **Risque accepté** : lever cette protection laisse git appliquer la
  configuration du dépôt (`core.fsmonitor`, `core.sshCommand`…). À réserver à vos propres projets. Si git refuse encore
  (git < 2.31, lecteur mappé), l'étape affiche la commande `git config --global --add safe.directory '<projet>'` : ne
  l'exécutez que pour vos propres dépôts.

Dans la coque Linux, `Suppr` sur un projet propose la fermeture forcée (`o/N`) quand elle est possible ; `F1` la décrit.

### Projets d'un utilisateur en mode serveur (v7.1.0, #275)

Sur le launcher de cluster, chaque utilisateur gère ses projets sur ses propres backends (voir `contracts/user-backends.md` §5 à §6 bis).

- **Découverte** : les projets sont les objets qui existent sur le backend (labels `cl.user`). Rien n'est installé chez vous.
  Le launcher garde seulement une trace des projets vus (nom, catégorie, backend, date).
- **États** : `arrêté` (objet présent, conteneur à l'arrêt, dernier état connu affiché avec sa date) ; `introuvable` (objet
  absent, trace présente) : seul **Purger** est proposé, il efface la trace et ne supprime rien sur le backend ;
  `injoignable` (backend hors ligne) : les projets restent visibles, les actions sont désactivées avec la raison.
- **F5** sur un projet arrêté démarre le conteneur sans session claude et met à jour le dernier état. Le launcher ne démarre
  jamais un projet par lui-même : seuls **Ouvrir** et **F5** le relancent.
- **Options du conteneur** (réglages du projet) : CPU, mémoire, PIDs, mémoire partagée et GPU pour docker/podman ; CPU et
  mémoire demandés ou limités, éphémère et zone JSON avancée pour k8s. Les valeurs hors liste blanche sont refusées avec le
  chemin du champ fautif. Modifier une option qui exige une recréation demande une confirmation : les données du projet sont
  conservées, les sessions ouvertes sont coupées.
- **Approbation de `env.yaml`** : à la première ouverture d'un projet cloné, le launcher demande d'approuver le fichier
  (image et paquets affichés). L'approbation est scellée par une clé propre à votre compte ; elle est redemandée si le fichier change.
- **Plafonds** : nombre de projets (`users.maxProjects`, défaut 30) et de terminaux ou lectures simultanés (40 par utilisateur).

### Plusieurs launchers sur un même projet

Un projet en conteneur (surtout distant) peut être atteint par plusieurs
launchers, sur plusieurs machines ; **un seul le pilote** : son propriétaire,
noté dans la session tmux du conteneur et entretenu par son launcher (battement
de cœur toutes les 5 s, parti au-delà de 20 s). Un autre launcher qui l'ouvre
pendant ce temps demande :

| Choix | Effet |
|---|---|
| `l` | **rejoindre en lecture seule** : on voit la session (partage d'écran), sans pouvoir taper ni la réarranger |
| `p` | **prendre la main** : l'autre launcher passe en **lecture seule** (sa fenêtre reste ouverte et l'indique) et ce launcher devient propriétaire |
| `a` | annuler |

Le launcher qui a perdu la main reprend la sienne par **Entrée** sur le
projet : sa fenêtre en lecture seule est rouverte et la même question est
posée. Au démarrage, un launcher ne rattache que les sessions dont il est
propriétaire. Et un projet qui tourne dans un conteneur piloté par un autre
launcher ne peut pas être lancé sur l'hôte (deux claude dans le même dossier
partagé).

### Config de claude dans les conteneurs

Les sessions en conteneur utilisent `CLAUDE_CONFIG_DIR=~/.claude`, donc
`~/.claude/.claude.json` (un fichier monté seul se détacherait : claude le
réécrit par remplacement). Au premier conteneur, ce fichier est **copié** depuis
`~/.claude.json`, jamais déplacé : les sessions de l'hôte gardent le leur.

### Consommation du compte : un seul cache pour tous

Le cache de la consommation du compte est dans `<config claude>/claude-launcher-cache/` (avec `usage-levels.json`, les niveaux d'épuisement calculés par le launcher pour le panneau milestone des projets, réécrit seulement quand `usage.json` change)
(la config claude, `CLAUDE_CONFIG_DIR`, est montée au même chemin dans tous les
conteneurs, locaux ou distants, et l'endpoint est propre au compte) :

- le **hub** y lit et écrit directement ;
- le **panneau milestone** des projets lit `~/.cache/claude-launcher/usage.json` :
  - sur la machine du hub, `usage.json` et `usage.json.err` de ce dossier sont des
    **liens** vers le cache commun (le reste du dossier n'est pas touché) ; ils sont
    posés au démarrage et rétablis à chaque relevé ;
  - dans un conteneur, le dossier entier est un lien, posé à chaque ouverture du projet.

Hubs et conteneurs partagent ainsi le cache **et** les échecs : l'endpoint n'est appelé
qu'une fois par intervalle (`USAGE_REFRESH_SEC`, 120 s par défaut) pour tout le monde.
Un projet en conteneur ouvert avant la v5.4 garde son propre cache jusqu'à sa
réouverture (détacher puis Entrée).

Après un refus pour excès d'appels (HTTP 429), plus personne n'appelle avant la fin
de l'attente : `Retry-After` s'il est donné, sinon 5 min, puis 10, 20 et 30 au plus
tant que les refus se suivent (retour à la normale au premier succès). L'heure du
prochain essai est affichée (`⚠ … trop d'appels à l'endpoint, nouvel essai vers 10:52`).
Un renouvellement du jeton par claude ne lève pas cette attente ; `F5` force un appel.
Au lancement du hub, l'échec noté et l'attente sont effacés (les dernières données restent
affichées) : le premier relevé interroge l'endpoint sans attendre, même si l'échec venait d'un
autre launcher arrêté depuis.

Le jeton est lu dans la config claude (`$CLAUDE_CONFIG_DIR/.credentials.json`), le même que
celui des sessions claude ; jusqu'à la v5.4.2, `~/.claude/.credentials.json`, qui pouvait garder
un ancien jeton refusé en 429. **Un launcher v4 encore actif** lit toujours cet ancien fichier
et écrit ses échecs dans le cache commun : l'arrêter.


- Le copier-coller OSC 52 à travers les deux niveaux de tmux n'est pas vérifié.
- La carte Team lit la version de claude du leader dans le conteneur (une fois
  par minute) ; celles des teammates viennent du nom de leur processus.

### Changement de version majeure

v4 ne connaît pas les conteneurs : son nettoyage des teams et sa reprise
peuvent prendre la session d'un conteneur pour morte. **Passer de v4 à v5/v6 (ou
revenir) uniquement avec tous les projets fermés.**

**v5 → v6** : la coque Linux, ses sessions tmux, ses fichiers et sa
configuration sont les mêmes. Un hub v5 se met à jour en v6 comme pour une
version mineure (au lancement ou par l'annonce verte), projets conservés. Les
nouvelles clés de `backends.yaml` (`hubs.<machine>.paths`, `windows:`,
`hub_cache`…) sont ignorées par un hub v5. Revenir à la v5 : installer la
v5.10.0 et la lancer avec `--no-update` (sinon elle se remet à jour en v6). Un
template v6 n'est proposé qu'à un launcher v6
([Versions](versions.md#versions-mises-à-jour-et-template)). Pas à pas :
[README du repo — Passage à la v6](../../README.md#passage-à-la-v6).
