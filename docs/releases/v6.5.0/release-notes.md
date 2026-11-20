# Release v6.5.0 - Fiabilisation : un coffre commun et un œil pour les teammates

**Date** : 2026-11-20

## Nouveautés

### Masquer les teammates inactifs, projet par projet
Un œil dans le titre de la carte TEAM : « default » suit le réglage global, « auto hide » masque les teammates inactifs, « no hide » les garde tous visibles. Un clic passe de l'un à l'autre, un clic droit ouvre le choix ; la carte se met à jour aussitôt.

### Un coffre de secrets commun
Le hub, l'exe Windows et le pod partagent le même coffre : une clé ssh ou un kubeconfig saisi une fois sert partout. Les coffres de la v6.4.0 sont migrés au démarrage et conservés. Les secrets propres à un projet vivent dans son `PRIVATE/`. Voir le guide `ng2/docs/secrets.md`.

### Cartes toujours à jour
Un rafraîchissement demandé pendant une relecture lente n'est plus perdu : il est rejoué à la fin.

## Corrections

- Arrêt de l'exe Windows : plus de lecteur bloqué ni de handles gardés
- Écritures d'état interrompues (plus de 24 h) nettoyées au démarrage
- Version du chart contrôlée dès la CI de branche
- Tests plus rapides et plus stables

## Comment mettre à jour

L'exe et la coque s'annoncent en orange, jaune puis vert : un clic installe la nouvelle version. **Cluster Helm** : le PVC `<release>-secrets` n'est plus créé ; un ancien volume est monté en lecture seule pour la migration — lire la note avant `helm upgrade`.

## Liens

- [Release GitHub](https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.5.0)
- [Guide des secrets](https://github.com/CCoupel/Claude-Launcher/blob/main/ng2/docs/secrets.md)
