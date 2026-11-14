# Changelog des contrats

> Changements de contrats (API, schemas) : `BREAKING` | `CHANGED` | `ADDED`. Lu par le CDP au GATE 2.

## [20261113] — v6.1.0 : panes cachés (#136) et configuration structurée (#118)

- **[CHANGED]** `cards.team` (interne à l'exe : frontend embarqué, aucun consommateur externe) — une ligne par teammate, champ `count` retiré, champs `pane`, `started`, `active`, `hidden`, `pinned`, `idleLeft` ajoutés (`contracts/desktop-dispatch.md`)
- **[ADDED]** `cards.team[]` (cycle 4) — `tplState` (`ok`/`late`/vide), `tplTarget`, `companionFile` : état du template par teammate (`contracts/desktop-dispatch.md`)
- **[NEW]** `teamToggle` — bascule caché / visible d'un teammate inactif
- **[NEW]** options tmux `@cl_panevis` (fenêtre), `@cl_idle_at`, `@cl_home` (pane) et session de réserve `cl-hidden` — `contracts/tmux-options.md` ; un launcher antérieur les ignore (panes cachés laissés dans la réserve)
- **[NEW]** `configModel`, `configApply`, `backendTest`, `backendStates` — configuration structurée
- **[NEW]** `projectSettings`, `projectApply` — réglages d'un projet
- **[NEW]** clé `TEAM_HIDE_IDLE` de `config.conf` (défaut 30, 0 = jamais) ; préférence par poste `termFontSize`
