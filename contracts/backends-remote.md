# Contrat — backends distants et autres clusters depuis le pod du launcher (v6.4.0, #193, #194)

> Révisions GATE 2 du 2026-11-17 : rev1 (clés ssh fournies **par l'interface**, par backend ssh) ; rev2 (écriture
> permise depuis l'ingress interne, règle provisoire jusqu'à v7.0.0).
> Complète `contracts/helm-values.md`, `contracts/web-server.md`. Hors cluster (hub, exe), rien ne change sans les
> nouveaux champs.

## Image **[CHANGED]** (#194)

L'image `ghcr.io/ccoupel/claude-launcher` ajoute `openssh-client`, le client docker (binaire statique `docker` de
download.docker.com, sha256 figée : `docker-cli` n'existe pas dans ubuntu:26.04, et `docker.io` embarque le démon)
et le client podman distant (paquet `podman-remote`, appelé comme `podman` : lien ou adaptation de
`core/backend.go`, à noter dans le CHANGELOG). Ni démon docker, ni service podman, ni `sshd`.

## Coffre des secrets du launcher **[BREAKING v6.5.0]** (#201 : partagé entre les instances)

> v6.4.0 : coffre local à chaque instance, refusé sous la racine partagée. v6.5.0 : **un coffre commun** au hub Linux,
> à l'exe Windows et au pod, **en clair** sur la racine partagée, protégé par les droits (risque accepté par l'utilisateur,
> GATE 2 du 2026-11-19 : instantanés et administrateurs du stockage partagé peuvent lire les secrets).

### Deux portées

| Portée | Contenu | Emplacement | Écrit par |
|--------|---------|-------------|-----------|
| **Globale** (backends, launcher) | par backend : `id`, `id.pub`, `known_hosts`, `kubeconfig` | `<CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/<backend>/` (`0700` / `0600`) | le launcher (interface : `backendSecretSet`…) |
| **Projet** | accès propres à un projet : environnements (kubeconfig, clés de déploiement, comptes), `.env` désignant des fichiers d'accès | `<projet>/PRIVATE/GLOBAL/` ou `<projet>/PRIVATE/<ENV>/` (convention `PRIVATE/` du template) | l'utilisateur / l'agent `infra` du projet — **jamais le launcher** |

- Le launcher ne range **aucun** secret de projet dans le coffre global, et n'écrit pas dans `PRIVATE/`.
- `CL_SECRETS_DIR` reste une surcharge (chemin absolu). Sans `CL_ROOT_BASE` et sans `CL_SECRETS_DIR` : emplacement local
  v6.4.0 (`~/.config/claude-launcher/secrets`, `%APPDATA%\claude-launcher\secrets`).
- Exe Windows : la racine est atteinte en UNC (`\\wsl.localhost\…`, 9P) ; pas d'ACL Windows sur ce chemin (droits
  POSIX de l'uid WSL) ; les jonctions Windows ne sont pas concernées.
- Pod : même uid que le propriétaire du partage (`runAsUser`) ; le PVC `<release>-secrets` n'est plus le coffre.

### Contrôles

- Refus (erreur claire, rien n'est écrit) : chemin non absolu ; dossier existant non `0700` (Linux, et partage vu depuis
  Linux) ; chemin résolu (liens symboliques, **liens pendants compris**) hors de l'emplacement attendu ; coffre sous un
  dossier de projet (`<racine>/<projet>/…`).
- **Supprimé [BREAKING]** : le refus « sous `CL_ROOT_BASE` / `CL_K8S_VOLUMES` / `shared` d'un backend ».
- `kubeconfig.path` d'un backend : admis dans le coffre global ou dans un `PRIVATE/` de projet ; refusé ailleurs sous la
  racine partagée.
- Écritures concurrentes (plusieurs instances) : verrou fichier (`core/lockfile.go`) + écriture atomique ; la dernière
  écriture l'emporte ; chaque écriture est journalisée (`<CacheDir>/secrets-journal.jsonl`, avec l'instance).
- `/files` ne sert jamais le coffre ni `PRIVATE/` (déjà dans `sensitiveNames` / dossiers exclus) — test obligatoire.
- Le contenu n'est jamais journalisé, jamais rendu par une API (sauf la clé publique), jamais copié dans
  `backends.yaml` ni dans le cache. Aucun Secret Kubernetes n'est écrit par le launcher.

### Migration depuis v6.4.0

Au démarrage de chaque instance : pour chaque `<backend>/` de l'ancien coffre local (emplacement v6.4.0, ou
`CL_SECRETS_LEGACY_DIR` posé par le chart sur l'ancien PVC) absent du coffre global, copie des fichiers (`0600`) sous
verrou, journalisée (`action: migrate`). Ancien coffre **conservé** (secours, suppression manuelle). Backend présent
des deux côtés avec un contenu différent : coffre global conservé, écart journalisé et signalé une fois dans l'interface.
Tout l'ancien contenu est de portée globale (sous-dossiers = backends) : rien n'est migré vers `PRIVATE/`.

## Clés ssh d'un backend ssh **[NEW]** (#194)

| Fichier | Contenu | Affichage |
|---------|---------|-----------|
| `id` | clé privée OpenSSH ou PEM, **sans phrase de passe** (le launcher appelle ssh en `BatchMode`) | **jamais relue ni affichée** : « clé privée enregistrée · SHA256:… » |
| `id.pub` | clé publique | affichée en entier (copiable, pour `authorized_keys`) |
| `known_hosts` | empreinte(s) de l'hôte | empreintes affichées (`SHA256:…`, type) |

- Saisie : coller la clé privée **et** la clé publique ; le launcher vérifie qu'elles vont ensemble (même clé
  publique dérivée, `golang.org/x/crypto/ssh`, module déjà présent) ; refus si une phrase de passe la protège.
  Option « Générer une paire » : ed25519 générée par le launcher, seule la clé publique est montrée.
- `known_hosts` : bouton « Relever l'empreinte » (`ssh-keyscan` de l'hôte du backend), puis affichage des empreintes
  et **confirmation explicite** avant l'enregistrement ; ou collage de lignes `known_hosts`. Pas d'acceptation
  silencieuse à la première connexion (`StrictHostKeyChecking=yes`, `UserKnownHostsFile=<dossier>/known_hosts`).
- Utilisation : le backend ssh reçoit `-i <dossier>/<backend>/id -o IdentitiesOnly=yes -o UserKnownHostsFile=…`.
  Sans clé enregistrée : comportement de v6.3.0 (agent ssh, `~/.ssh` du poste).
- Remplacer / supprimer : remplacement entier (clé privée + publique) ; « Supprimer » efface les trois fichiers.

### Appels (`/api/call` et liaison Wails)

| Méthode | Arguments | Résultat |
|---------|-----------|----------|
| `backendSecretGet` | `{backend}` | `{ hasKey, publicKey, fingerprint, knownHosts: [{type, fingerprint}], dir }` — jamais la clé privée |
| `backendSecretSet` | `{backend, privateKey, publicKey}` ou `{backend, generate: true}` | `{ ok, fingerprint, publicKey, msg }` |
| `backendHostScan` | `{backend}` | `{ ok, keys: [{type, fingerprint, line}] }` (rien n'est enregistré) |
| `backendKnownHostsSet` | `{backend, lines}` | `{ ok, msg }` |
| `backendSecretDelete` | `{backend, what: "key" \| "known_hosts" \| "all"}` | `{ ok }` |

## Règle d'écriture des secrets **[NEW]** — provisoire en v6.4.0

Qui peut **écrire** un secret (clé ssh, `known_hosts`, kubeconfig) en v6.4.0 :

| Client | v6.4.0 (provisoire, décision utilisateur 2026-11-17) | v7.0.0 (règle R1, avec l'authentification) |
|--------|-------------------------------------------------------|---------------------------------------------|
| fenêtre native (hub, exe) | oui | oui |
| client web du hub / de l'exe (mode `token`) | oui | à rediscuter |
| client web de l'ingress interne (cluster, `none`, LAN) | **oui** | non : session authentifiée exigée |

- Le journal des écritures est `<CacheDir>/secrets-journal.jsonl` (hors du coffre) ; il est écrit **avant** le secret : un journal inaccessible, comme une ACL Windows impossible à poser, refuse l'écriture. Écriture et relevé d'empreinte (`backendHostScan`, adresse du backend déclaré, jamais loopback ni adresse du pod) exigent un backend ssh déclaré ; le dossier du coffre n'est montré qu'à la fenêtre native.
- Ce qui reste exigé dans tous les cas : clé privée jamais relue ni affichée, coffre aux droits restreints (v6.5.0 : sur la racine partagée, voir « Coffre des secrets »), empreinte de
  l'hôte confirmée explicitement, aucune trace des contenus dans les journaux.
- **Risque accepté (v6.4.0)** : toute machine du LAN admise par la liste d'adresses peut déposer ou remplacer la clé
  et l'empreinte d'un backend, et donc envoyer les projets vers une machine qu'elle contrôle sans laisser de trace dans
  un terminal. Elle pouvait déjà piloter les terminaux. Chaque écriture est journalisée (backend, type de secret,
  empreinte, adresse source vue par le serveur, sans contenu) et affichée dans les Réglages (« modifié le … »).
- **Chaîne d'attaque acceptée jusqu'à v7.0.0** (audit security LOT-2, M2) : une écriture non authentifiée depuis le LAN
  (ingress interne, `none`) peut **redéclarer** un backend ssh (`backends.yaml`, Réglages), y déposer une clé et un
  `known_hosts` de son choix, puis lancer un projet : le launcher exécute alors des commandes sur l'hôte choisi par
  l'attaquant (exécution à distance sur une machine qu'il contrôle, ou sur un hôte qui autorise cette clé). Mêmes
  moyens que « piloter les terminaux », mais sans trace dans un terminal. Atténuations v6.4.0 : liste d'adresses de
  l'ingress, NetworkPolicy, journal des écritures (`<CacheDir>/secrets-journal.jsonl`), relevé d'empreinte limité
  aux backends déclarés (ni loopback, ni link-local, ni adresse du pod, adresse résolue une seule fois). Levée :
  règle R1 de v7.0.0 (session authentifiée exigée pour toute écriture).
- Le kubeconfig de #193 suit la même règle provisoire. Sa clé de backend (`kubeconfig`) reste hors de l'empreinte
  d'exposition `WebExposureChanged` en v6.4.0, par cohérence ; elle y entrera avec R1 en v7.0.0.

## kubeconfig d'un backend k8s **[CHANGED]** (#193, cohérence avec la règle des secrets)

```yaml
backends:
  autre-cluster:
    type: k8s
    kubeconfig: true            # NEW — fichier du coffre : <CL_SECRETS_DIR>/autre-cluster/kubeconfig
    # kubeconfig: /chemin/abs   # ou chemin d'un fichier existant (Secret monté), hors partage, ou dans le coffre global ou un PRIVATE/ de projet (v6.5.0)
    context: prod               # facultatif : contexte de CE fichier
    namespace: claude-projets
    volumes: { /share/MFS/ROOT/CLAUDE_PROJETS: claude-projets }
```

- Le kubeconfig peut être **déposé par l'interface** dans le coffre (`backendSecretSet` avec `{backend, kubeconfig}`),
  avec la même règle d'écriture que les clés ssh (provisoire en v6.4.0). Il n'est jamais relu ni affiché (contextes listés seulement).
- `kubeconfig:` comme chemin reste possible (Secret monté par le chart, `backends.kubeconfigs`), modifiable seulement
  par le fichier `backends.yaml` ou les Réglages (même règle provisoire).
- **Priorité** : `kubeconfig` du backend > ServiceAccount du pod (en cluster) > `KUBECONFIG` / `~/.kube/config`.
  Avec `kubeconfig`, `context` est honoré même en cluster ; kubectl reçoit `--kubeconfig` (jamais de `KUBECONFIG`
  hérité).
- Contrôle **à chaque appel de kubectl** (dépôt, chemin, coffre) : un kubeconfig qui exécute une commande (`exec`, `auth-provider`) ou lit un fichier du poste (`client-certificate`, `client-key`, `tokenFile`, `certificate-authority`), quelle que soit la casse des clés, est refusé — kubectl n'est pas lancé (erreur claire). `backendSecretSet {kubeconfig}` renvoie `{ ok, fingerprint, contexts, warnings }` ; `warnings` liste `insecure-skip-tls-verify` et `proxy-url` (accepté, signalé).
- Fichier illisible : backend en erreur « kubeconfig illisible sur ce poste », **sans repli** sur le ServiceAccount.
- Diagnostic / « Tester » : « kubeconfig lisible », puis `auth can-i` de v6.3.0 avec ce kubeconfig.

## Autre namespace du même cluster (#193, chart)

`rbac.extraNamespaces: [ns, …]` : même Role et un RoleBinding vers le ServiceAccount du pod dans chacun ; aucun
ClusterRole.

## docker / podman en TLS (#194)

Inchangé par la révision : Secret `backends.docker.secretName` (`ca.pem`, `cert.pem`, `key.pem`) monté en
`/var/run/claude-launcher/docker`, `DOCKER_CERT_PATH` et `DOCKER_TLS_VERIFY=1` seulement s'il est donné.
`tcp://…:2375` sans TLS reste accepté (décision utilisateur en prérequis).

## « Tester » d'un backend distant depuis le pod (#194)

Étapes ajoutées : « client <ssh|docker|podman> présent », « clé ssh enregistrée » (ssh), « empreinte de l'hôte
connue » (ssh), puis les étapes actuelles.

## v7.0.0 — règle R1 définitive **[CHANGED]** (#252)

La règle provisoire de v6.4.0 (« Règle d'écriture des secrets ») est **remplacée** par la règle R1 de
`contracts/auth.md` §4 :

| Client | Écriture d'un secret, de la configuration, approbation d'environnement |
|--------|--------------------------------------------------------------------------|
| fenêtre native (hub, exe) | oui |
| client web du hub / de l'exe (mode `token`) | oui (décision du plan v7.0.0 : le jeton désigne le propriétaire du poste) |
| client web du cluster, `proxy`, rôle `admin` | oui |
| client web du cluster, `proxy`, rôle `user` | non |
| client web du cluster, `none` (`auth.enabled: false`) | **non** — la chaîne d'attaque M2 (redéclaration d'un backend ssh depuis le LAN) est levée |

- `secrets-journal.jsonl` : champ `user` ajouté (login GitHub, `fenêtre native` ou `web local`).
- La clé `kubeconfig` d'un backend **entre** dans l'empreinte `WebExposureChanged` : non modifiable depuis un client
  web par `configSave` / `configApply` ; dépôt dans le coffre par `backendSecretSet {kubeconfig}` : administrateur.
- Le risque accepté « toute machine du LAN peut déposer une clé » est **retiré** de ce contrat.
