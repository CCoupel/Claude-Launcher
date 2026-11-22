# Release v6.7.0 - Ne ratez plus une question de teamleader

**Date** : 2026-11-22

## Nouveautés

### Savoir quel teamleader vous attend
Un badge, un onglet et une vignette signalent le teamleader en attente de votre réponse (question ou permission), avec une notification du navigateur. La fin de l'attente retire le signalement.

### Session proposée à l'ouverture
À l'ouverture d'un projet géré dont la session n'est pas démarrée, le launcher propose de lancer `/start-session` avant claude. Une reprise ou une team existante ne pose pas la question.

### Partage injoignable : l'interface ne se fige plus
L'écriture de `config.conf` est bornée par l'option `SHARE_WRITE_TIMEOUT` (10 s par défaut, 2 à 120). Au bout du délai, l'organisation précédente est rétablie ; l'écriture reprend seule au retour du partage.

### Arrêt propre de l'interface
Les terminaux sont détachés puis attendus (échéance de 25 s) ; plus aucune écriture n'est acceptée après l'arrêt.

## Corrections

- Message de repli « leader à gauche refusé par tmux » affiché une seule fois au lieu d'à chaque remise en page

## Comment mettre à jour

Remplacez le binaire puis utilisez le bandeau « binaire changé → Relancer » (`ctrl+r` dans la coque).

## Liens

- [GitHub Release](https://github.com/CCoupel/Claude-Launcher/releases/tag/v6.7.0)
