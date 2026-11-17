# Contrat — backends distants et autres clusters depuis le pod du launcher (v6.4.0, #193, #194)

> Complète `contracts/helm-values.md` et `contracts/launcher-env.md`. Hors cluster (hub, exe), rien ne change
> sans les nouveaux champs.

## Image **[CHANGED]** (#194)

L'image `ghcr.io/ccoupel/claude-launcher` ajoute `openssh-client`, le client docker (binaire statique `docker` de
download.docker.com, sha256 figée : `docker-cli` n'existe pas dans ubuntu:26.04, et `docker.io` embarque le démon)
et le client podman distant (paquet `podman-remote`, appelé comme `podman` : lien ou adaptation de
`core/backend.go`, à noter dans le CHANGELOG). Versions figées par la distribution de base (ubuntu:26.04) ; tout binaire téléchargé hors paquet a sa somme
sha256 figée dans le `Dockerfile` (règle de kubectl). Ni démon docker, ni service podman, ni `sshd` dans l'image.

## Identifiants par Secret **[NEW]** (#194, #193)

- Règle commune : un fichier d'identifiant est **toujours** un Secret monté en lecture seule, jamais sous
  `rootBase`, sous `volume.mountPath` ni sous `/home/launcher` directement monté ; le launcher refuse un chemin
  d'identifiant situé sous `CL_ROOT_BASE` ou sous un volume de `CL_K8S_VOLUMES` (message clair, backend en erreur).
- **ssh** (`backends.ssh.secretName` du chart) : Secret avec `id_ed25519` (ou `id_rsa`) et `known_hosts`
  (obligatoire). Monté en `/var/run/claude-launcher/ssh` (`0440`). L'entrypoint copie ces fichiers dans
  `~/.ssh` (emptyDir) en `0600` avant de lancer le launcher ; `StrictHostKeyChecking=yes` (pas d'acceptation à la
  volée). Clé absente ou illisible : le launcher démarre, les backends ssh échouent avec « identifiant ssh absent ».
- **docker / podman en TLS** (`backends.docker.secretName`) : `ca.pem`, `cert.pem`, `key.pem`, montés en
  `/var/run/claude-launcher/docker` ; `DOCKER_CERT_PATH` et `DOCKER_TLS_VERIFY=1` posés seulement si le Secret est
  donné. Un `host: tcp://…:2375` (sans TLS) reste accepté comme aujourd'hui (risque existant, rappelé dans le guide).
- **kubeconfig** (#193) : liste `backends.kubeconfigs[] = {name, secretName, key}` du chart ; chaque Secret est monté
  en `/var/run/claude-launcher/kube/<name>/config` (`0440`).

## Champ `kubeconfig` d'un backend k8s **[NEW]** (#193)

```yaml
backends:
  autre-cluster:
    type: k8s
    kubeconfig: /var/run/claude-launcher/kube/autre/config   # NEW — chemin d'un fichier, jamais son contenu
    context: prod                                            # facultatif : contexte de CE fichier
    namespace: claude-projets
    volumes: { /share/MFS/ROOT/CLAUDE_PROJETS: claude-projets }
```

- **Priorité** : `kubeconfig` du backend > ServiceAccount du pod (en cluster) > `KUBECONFIG` / `~/.kube/config`
  (hors cluster). Avec `kubeconfig`, `context` est honoré même en cluster. Sans `kubeconfig` : v6.3.0 (contexte
  ignoré en cluster).
- kubectl reçoit `--kubeconfig <chemin>` (jamais de `KUBECONFIG` hérité dans ce cas).
- Fichier illisible sur ce launcher (`backends.yaml` partagé entre postes) : backend en erreur « kubeconfig <chemin>
  illisible sur ce poste », **sans repli** silencieux sur le ServiceAccount.
- Le contenu n'est jamais lu par le launcher (kubectl seul le lit), ni affiché, ni journalisé : seul le chemin
  apparaît.
- Client web : `backends.<nom>.kubeconfig` entre dans l'empreinte `WebExposureChanged` — **non modifiable depuis le
  web** (création, modification, suppression du champ refusées), quel que soit le mode d'accès.
- Diagnostic / « Tester » : étape « kubeconfig lisible », puis les contrôles de droits (`auth can-i`) de v6.3.0 avec
  ce kubeconfig.

## Autre namespace du même cluster **[NEW]** (#193, chart)

`rbac.extraNamespaces: [ns, …]` : le chart crée, dans chacun, le même Role que dans son namespace et un RoleBinding
vers le ServiceAccount du pod. Aucun ClusterRole. Le backend k8s vise ce namespace par son champ `namespace`
(code inchangé : le ServiceAccount a désormais les droits).

## « Tester » d'un backend distant depuis le pod (#194)

Étapes ajoutées en cluster : « client <ssh|docker|podman> présent », « identifiant trouvé » (ssh : clé et
`known_hosts`), puis les étapes actuelles (connexion, moteur, stockage partagé).
