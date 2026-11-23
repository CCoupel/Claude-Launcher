# Release v6.8.0 - Vos quotas, en couleur

**Date** : 2026-11-23

## Nouveautés

### La date de renouvellement des quotas prend une couleur
Dans la coque Linux, la fenêtre Windows et l'interface web, la date de renouvellement de chaque quota (session de 5 h, semaine) est colorée selon votre rythme d'utilisation :

- **rouge**, avec « ⚠ épuisé ~HH:MM » : le quota sera épuisé avant son renouvellement ;
- **orange** : épuisé peu après le renouvellement ;
- **vert** : le quota tient largement.

Quand la projection n'est pas fiable (consommation nulle, fenêtre entamée depuis moins de 10 min ou 5 %), aucune couleur n'est affichée.

## Corrections

- Réglages du projet : la liste « Image de base » propose toutes les images disponibles (backends.yaml, valeurs par défaut, image courante) au lieu de la seule image déjà configurée. « Autre… » permet toujours une saisie libre.

## Comment mettre à jour

Remplacez le binaire puis utilisez le bandeau « binaire changé → Relancer » (`ctrl+r` dans la coque).

## Liens

- [GitHub Release](https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.8.0)
