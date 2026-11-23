# Release v6.8.1 - Des couleurs de quotas mieux calibrées

**Date** : 2026-11-23

## Nouveautés

### Un seuil d'alerte adapté à chaque quota
L'orange (quota épuisé peu après son renouvellement) dépend désormais de la fenêtre : moins d'1 h pour la session de 5 h, moins de 5 h pour les quotas hebdomadaires (y compris par modèle, par exemple Fable). Auparavant, le seuil était de 6 h pour tous.

Le launcher publie aussi le niveau d'épuisement de chaque quota dans un fichier partagé, que le panneau milestone des projets peut réutiliser.

## Corrections

- Nouvelles versions : la vérification des mises à jour GitHub est plus fréquente (2 min avec jeton, 5 min sans) et une nouvelle version apparaît au plus un cycle après sa publication, sans `F5`. Sans jeton, plusieurs instances derrière la même adresse IP peuvent atteindre le quota anonyme de GitHub : renseignez `GITHUB_TOKEN`.

## Comment mettre à jour

Remplacez le binaire puis utilisez le bandeau « binaire changé → Relancer » (`ctrl+r` dans la coque).

## Liens

- [GitHub Release](https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.8.1)
