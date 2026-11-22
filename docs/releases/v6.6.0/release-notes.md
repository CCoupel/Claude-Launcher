# Release v6.6.0 - Gestion des projets : créer, cloner, déplacer depuis l'interface

**Date** : 2026-11-22

## Nouveautés

### Nouveau projet : des listes, pas un chemin
La base et la catégorie se choisissent dans des listes (une nouvelle catégorie peut être créée) et le chemin final s'affiche avant de valider. Trois sources : « Projet brut », « Template projet » ou « Dépôt Git » (https ou ssh, avec progression et annulation).

### Un dépôt cloné doit être approuvé
Un projet cloné qui apporte son propre `env.yaml` demande votre accord à la première ouverture en conteneur (Accepter, Ouvrir sans, Annuler). La question revient si ces fichiers changent.

### Déplacer ou renommer un projet
Clic droit sur l'arbre ou la carte (`m` dans la coque) : le dialogue montre ce qui change avant validation. Entre deux systèmes de fichiers, le contenu est copié et contrôlé avant de supprimer la source ; un déplacement inachevé se reprend ou s'annule.

### Réglages pris en compte à chaud
`config.conf` est relu dès qu'il change ; un fichier invalide garde la dernière version valide et le signale. Chaque option porte une pastille d'effet (immédiat, prochaine ouverture, relance des sessions, relance du launcher).

### Voir un jeton le temps de le vérifier
Un œil dans le champ affiche un élément caché (mot de passe, jeton GitHub) ; il se remasque au clic, après 30 s ou au changement de rubrique. Refusé depuis un accès distant, jamais journalisé.

### Mise à jour réversible, relance depuis l'interface
Le binaire remplacé est gardé en `<exe>.bak-v<version précédente>` (retour arrière documenté dans `ng2/docs/versions.md`). Après une mise à jour, un bandeau « Relancer » redémarre le launcher sans perdre vos sessions.

## Corrections

- `config.conf` écrit de façon atomique (plus de fichier tronqué, même sous Windows)
- Tests instables fiabilisés (verrou de présence, flux SSE)
- Course de données et panic d'arrangement des panes corrigées

## Comment mettre à jour

L'exe et la coque s'annoncent en orange, jaune puis vert : un clic installe la nouvelle version, puis « Relancer ».

**Attention API** : `create` n'accepte plus qu'une `base` configurée (l'ancien appel avec un chemin absolu est refusé) ; l'interface et le backend sont livrés ensemble.

## Liens

- [Release GitHub](https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.6.0)
- [Versions et retour arrière](https://github.com/CCoupel/Claude-Launcher/blob/main/ng2/docs/versions.md)
