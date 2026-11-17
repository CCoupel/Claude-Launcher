# Contrat — backends distants et autres clusters depuis le pod du launcher (v6.4.0, #193, #194)

> Révision GATE 2 du 2026-11-17 (rev1) : les clés ssh sont fournies **par l'interface**, par backend ssh.
> Complète `contracts/helm-values.md`, `contracts/web-auth.md`. Hors cluster (hub, exe), rien ne change sans les
> nouveaux champs.

## Image **[CHANGED]** (#194)

L'image `ghcr.io/ccoupel/claude-launcher` ajoute `openssh-client`, le client docker (binaire statique `docker` de
download.docker.com, sha256 figée : `docker-cli` n'existe pas dans ubuntu:26.04, et `docker.io` embarque le démon)
et le client podman distant (paquet `podman-remote`, appelé comme `podman` : lien ou adaptation de
`core/backend.go`, à noter dans le CHANGELOG). Ni démon docker, ni service podman, ni `sshd`.

## Coffre des secrets du launcher **[NEW]**

Dossier `CL_SECRETS_DIR`, `0700`, un sous-dossier par backend (`<dossier>/<backend>/`), fichiers en `0600`.

| Launcher | Emplacement par défaut |
|----------|------------------------|
| pod (chart) | volume dédié **non partagé** (PVC `ReadWriteOnce` `<release>-secrets`, 10 Mi, `keep`) monté en `/var/lib/claude-launcher/secrets` |
| hub Linux | `~/.config/claude-launcher/secrets` (jamais le dossier de config s'il est sous la racine partagée) |
| exe Windows | `%APPDATA%\claude-launcher\secrets` (ACL : utilisateur seul) |

- Refus (erreur claire, rien n'est écrit) si le dossier est sous `CL_ROOT_BASE`, sous un volume de `CL_K8S_VOLUMES`,
  sous une racine `shared` d'un backend, ou s'il n'est pas `0700` (Linux).
- Aucun Secret Kubernetes n'est écrit par le launcher (aucun droit RBAC `secrets` ajouté).
- Le contenu n'est jamais journalisé, jamais rendu par une API (sauf la clé publique), jamais copié dans
  `backends.yaml` ni dans le cache.

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

## Règle des secrets **[NEW]** — à trancher au GATE 2

Qui peut **écrire** un secret (clé ssh, `known_hosts`, kubeconfig) ? La lecture de la clé publique et des empreintes
est permise à tout client qui voit les Réglages.

| Client | Règle R1 (**recommandée**) | Règle R2 |
|--------|----------------------------|----------|
| fenêtre native (hub, exe) | oui | oui |
| client web du hub / de l'exe (mode `token`) | non (comme les réglages d'exposition en v6.3.0) | oui |
| client web de l'**ingress externe**, requête de confiance (`contracts/web-auth.md`) | oui | oui |
| client web de l'**ingress interne** (sans authentification) | **non** : refus « secret modifiable seulement par une session authentifiée » | oui |

- **Pourquoi R1** : sur l'interne, toute machine du LAN pourrait remplacer `known_hosts` et la clé d'un backend, donc
  envoyer les projets vers une machine qu'elle contrôle (interception de code et de données) **sans laisser de
  trace dans un terminal**. Elle peut déjà piloter les terminaux, mais c'est visible. R1 aligne les secrets sur la
  règle de #193 (kubeconfig non modifiable depuis le web), tout en restant faisable en cluster par l'hôte externe.
- **Conséquence de R1** : dans le cluster, saisir une clé exige de passer **une fois** par l'adresse externe
  (connexion GitHub). Sans ingress externe, la saisie passe par `kubectl exec` (procédure documentée) : le coffre
  est propre à chaque launcher et n'est pas partagé avec un hub.
- R2 : plus simple, cohérent avec « le LAN est de confiance » ; risque ci-dessus accepté.

## kubeconfig d'un backend k8s **[CHANGED]** (#193, cohérence avec la règle des secrets)

```yaml
backends:
  autre-cluster:
    type: k8s
    kubeconfig: true            # NEW — fichier du coffre : <CL_SECRETS_DIR>/autre-cluster/kubeconfig
    # kubeconfig: /chemin/abs   # ou chemin d'un fichier existant (Secret monté), hors partage
    context: prod               # facultatif : contexte de CE fichier
    namespace: claude-projets
    volumes: { /share/MFS/ROOT/CLAUDE_PROJETS: claude-projets }
```

- Le kubeconfig peut être **déposé par l'interface** dans le coffre (`backendSecretSet` avec `{backend, kubeconfig}`),
  avec la même règle d'écriture que les clés ssh (R1 ou R2). Il n'est jamais relu ni affiché (contextes listés seulement).
- `kubeconfig:` comme chemin reste possible (Secret monté par le chart, `backends.kubeconfigs`), modifiable seulement
  par le fichier `backends.yaml` ou un client autorisé par la règle (la clé entre dans l'empreinte d'exposition :
  refus aux autres clients web).
- **Priorité** : `kubeconfig` du backend > ServiceAccount du pod (en cluster) > `KUBECONFIG` / `~/.kube/config`.
  Avec `kubeconfig`, `context` est honoré même en cluster ; kubectl reçoit `--kubeconfig` (jamais de `KUBECONFIG`
  hérité).
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
