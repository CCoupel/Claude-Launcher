# #235 — Date de renouvellement colorée + alerte d'épuisement

Projets/comptes factices uniquement : ne jamais écraser le vrai cache d'usage. Isoler :
`export XDG_CACHE_HOME=$(mktemp -d)` et `CLAUDE_CONFIG_DIR=$(mktemp -d)` avant de lancer le launcher
(cache lu : `$CLAUDE_CONFIG_DIR/claude-launcher-cache/usage.json`, format `limits[]` : kind, percent, resets_at).

## Préparation — usage.json factice (3 fenêtres hebdo `seven_day`, `resets_at` = maintenant + durée restante)
| Cas | percent | Écoulé de la fenêtre | Attendu |
|-----|---------|----------------------|---------|
| Rouge | 80 | ~3 j sur 7 (épuisé avant le renouvellement) | date rouge + « ⚠ épuisé ~HH:MM » |
| Orange | 50 | rythme épuisant < 6 h après le renouvellement | date orange, pas d'alerte |
| Vert | 10 | ~3 j sur 7 | date verte, pas d'alerte |
| Sans couleur | 0 ou fenêtre entamée < 10 min / < 5 % | — | date neutre, pas d'alerte |
Rejouer avec `kind` `five_hour` (session 5 h) : mêmes règles.

## Scénarios
A. Coque tmux (nav) : lignes de quotas = couleurs ci-dessus, alerte rouge après la date.
B. Fenêtre native Windows : panneau Usage, mêmes couleurs (orange = niveau « yellow »), alerte rouge en gras.
C. `--web` (Chrome) : idem B.
D. Fenêtre dont `resets_at` est passé : aucune couleur ni alerte.
E. Cache périmé (`stale`) : date neutre, pas d'alerte (coque).
