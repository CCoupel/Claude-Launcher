# Release v7.0.0 - Le mode SaaS : un launcher de cluster protégé

**Date** : 2026-11-26

La v7.0.0 est une version **majeure** : le launcher qui tourne dans votre cluster Kubernetes n'accepte plus rien sans identité GitHub autorisée. Les postes (Linux, macOS, Windows) ne changent pas.

## Nouveautés

### Connexion avec GitHub
Un sidecar oauth2-proxy se place devant le launcher : connexion GitHub, comptes déclarés dans `auth.admins` et `auth.users`. Un compte de secours `admin@local` existe, accessible depuis l'hôte interne seulement (mot de passe généré). Un limiteur d'essais protège `/oauth2/` et renvoie une page 429.

### Deux rôles et la règle R1
`admin` gère le serveur, les options globales, le coffre des secrets et les champs de `backends.yaml` qui touchent au pod ; `user` gère ses projets et ses backends. Une requête JSON à clés en double est refusée.

### Accès depuis Internet, sous un sous-chemin
L'accès externe peut se publier sous un chemin d'un hôte partagé (`externalIngress.pathPrefix`, par exemple `coupel.net/claude`), toujours derrière l'authentification.

### Pods projets cloisonnés
Un pod projet ne monte que son dossier et `CLAUDE_CONFIG_DIR` : le coffre des secrets, `backends.yaml`, `.kube` et `.cache` ne lui sont plus visibles, et le jeton de ServiceAccount n'est plus monté par défaut.

### Journal d'audit et interface
Chaque appel est consigné (`audit.jsonl`), avec une ligne d'intention avant chaque action d'administration. L'interface affiche votre identité, grise les actions réservées à l'admin et signale l'expiration de la session. Quotas : le niveau estimé sur peu de données s'affiche en gris.

## Corrections

- Les logins GitHub de `auth.admins` / `auth.users` gardent leur casse côté oauth2-proxy : une casse différente de celle de GitHub provoquait une erreur de connexion.

## Comment mettre à jour

- **Poste** : rien à faire.
- **Cluster** : avant `helm upgrade`, créez la GitHub App (deux URL de retour, interne et externe), renseignez `auth.existingSecret` (ou `auth.github.clientId` / `clientSecret`) et `auth.admins`, sinon le pod ne démarre pas. `auth.enabled: false` rétablit le comportement de la v6.9.0. Les pods projets en marche à l'ancien modèle sont conservés jusqu'à leur fermeture puis leur réouverture.

## Liens

- [Guide Kubernetes](../../../ng2/docs/kubernetes.md) · [Vers un launcher multi-utilisateur](../../../ng2/docs/saas.md)
- [GitHub Release](https://github.com/CCoupel/Claude-Launcher/releases/tag/v7.0.0)
