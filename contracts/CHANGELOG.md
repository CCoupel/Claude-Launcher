# Changelog des contrats

> Changements de contrats (API, schemas) : `BREAKING` | `CHANGED` | `ADDED`. Lu par le CDP au GATE 2.

## [20261116] — v6.3.0 : launcher dans Kubernetes (#165 à #171, #173, #175 à #177), réglages des serveurs web (#183)

- **[NEW]** `CL_ROOT_BASE` et `--root-base` : racine de base ; ordre `CL_CONFIG_DIR` > racine de base > dossier du binaire > dossier personnel (#165) — `contracts/launcher-env.md`
- **[NEW]** `CL_HUB_NAME`, `CL_CACHE_DIR` : nom et dossier d'état du launcher (#169)
- **[NEW]** `CL_IN_CLUSTER`, `CL_ALLOW_HOST_BACKEND`, `CL_K8S_VOLUMES` : launcher dans un cluster — backend k8s par le ServiceAccount du pod, namespace du pod, backend k8s implicite, backend `host` refusé (#168, #169)
- **[CHANGED]** diagnostic et « Tester » d'un backend k8s **en cluster** : contrôle de chaque droit, contrôle du montage du PVC, plus d'étape `get namespace` (#168) — hors cluster, inchangé
- **[NEW]** `CL_UPDATE_MODE` (`auto` | `notify`) : annonce de version sans installation (#167)
- **[CHANGED]** `desktop --web` **en cluster** : jeton conservé dans le cache au lieu d'un jeton neuf à chaque démarrage (#167) — hors cluster, inchangé
- **[CHANGED]** `desktop --web` : arrêt propre sur `SIGTERM` / `SIGINT` (#167)
- **[NEW]** image `ghcr.io/ccoupel/claude-launcher` (tags `vX.Y.Z`, `vX.Y.Z.a`) (#167)
- **[NEW]** `desktop_web.enabled` (commutateur) et `desktop_web.url` (adresse publique) ; option `--url`, variable `CL_WEB_URL` (#183, #171) — `contracts/desktop-web.md` ; sans `enabled`, comportement de la v6.2.0
- **[NEW]** routes `GET /healthz` et `GET /readyz`, sans session ni contrôle de `Host`, sans information (#167)
- **[CHANGED]** `GET /api/events` : en-têtes `X-Accel-Buffering: no` et `no-transform` ; reconnexion de l'interface à délai croissant ; session perdue signalée (#171)
- **[CHANGED]** cookie de session : attribut `Secure` quand l'adresse publique est en `https` (#171) — sans adresse publique, inchangé
- **[NEW]** instantané `webUI` : champs `enabled`, `url`, `server` ; `configModel` / `configApply` `kind: "hub"` : clés `desktop_web.enabled`, `desktop_web.url` (#183, #171)
- **[NEW]** chart Helm `deploy/helm/claude-launcher` et ses valeurs (#170, #173) — `contracts/helm-values.md`
- **[CHANGED]** chart (revue et audit LOT-4.2) : Role réduit aux appels réels (`pods/exec` create seul, `statefulsets` sans update, `events` list, PVC get) ; liste d'adresses validée entrée par entrée (/8 IPv4 et /16 IPv6 au minimum) ; `rootBase` normalisé ; `extraEnv` et `ingress.annotations` ne peuvent pas remplacer les variables et annotations posées par le chart ; `clusterIssuer: a-renseigner` refusé ; racine du conteneur en lecture seule ; QUALIF dans le namespace `claude-qualif` sur la racine dédiée `/share/MFS/ROOT/CLAUDE-PROJECTS-QUALIF` (#170, #173)
- Aucun `BREAKING` : sans les nouvelles variables ni les nouveaux réglages, hub Linux et exe Windows se comportent comme en v6.2.0.

## [20261115] — v6.2.0 : tailles par zone (#150), carte TEAM (#151), en-têtes (#152), rafraîchir (#158), interface web (#163)

- **[NEW]** préférences par poste `treeFontSize`, `cardsFontSize` (#150) — rendues aussi par `configModel` `kind: "hub"`
- **[NEW]** `cards.teamHead` (`tpl`, `claude`, `odd`) et `cards.team[].odd` (#151) — champs existants conservés
- **[NEW]** `layout.panes[].state` (`lead` / `active` / `idle` / vide) (#152) — `role` et `active` inchangés
- **[NEW]** options tmux de pane `@cl_state`, `@cl_name` et options de fenêtre `pane-border-*` posées par le launcher qui a la main (#152) — `contracts/tmux-options.md` ; un launcher antérieur les ignore
- **[NEW]** `refreshProject` + évènement `refresh`, `forceClose`, `forceCloseConfirm` ; lignes de l'instantané : `refreshing`, `canForce` (#158)
- **[NEW]** réglage `desktop_web` de `backends.yaml` (`defaults` / `hubs.<machine>[.<système>]`), champ `webUI` de l'instantané, méthode `webUIOpen` (#163)
- **[CHANGED]** `clipboardRead` / `clipboardWrite` : `unavailable` décidé **par appel** (client web) et non plus par processus ; `quit`, `setTheme` refusés à un client web (#163) — rétrocompatible pour `desktop --web` seul
- **[CHANGED]** `desktop --web` sans `--addr` : utilise `desktop_web.listen` s'il est fixé (sinon port libre, inchangé) (#163)
- **[NEW]** clés `LAYOUT_MODE` (`top` | `left`, défaut `top`), `LAYOUT_LEFT_LEADER_PCT`, `PROJECT_LAYOUTS` de `config.conf` (#153) — sans réglage, la mise en page est inchangée
- **[NEW]** `projectSettings` : `layout`, `layoutAuto`, `layouts` ; `projectApply` : argument `layout` (#153)
- **[CHANGED]** `projectApply` : `colorSaved` vrai dès que `config.conf` est écrite, pour la couleur ou pour l'organisation (#153) — rétrocompatible
- Coque Linux (#152) : forme arrêtée — ligne de titre par pane (`pane-border-status top`), maquette A retenue
- Aucun `BREAKING`.

## [20261113] — v6.1.0 : panes cachés (#136) et configuration structurée (#118)

- **[CHANGED]** `cards.team` (interne à l'exe : frontend embarqué, aucun consommateur externe) — une ligne par teammate, champ `count` retiré, champs `pane`, `started`, `active`, `hidden`, `pinned`, `idleLeft` ajoutés (`contracts/desktop-dispatch.md`)
- **[NEW]** `clipboardWrite`, `clipboardRead` (LOT-4, #155) — presse-papiers du poste par le runtime Wails, « indisponible » en `--web` ; **[CHANGED]** `termInput` rend un refus (`readonly`, `toobig`) au lieu d'ignorer la saisie
- **[NEW]** `projectSettings`, `projectApply` (LOT-3.2, #148) — réglages d'un projet : backend, image, couleur, `env.yaml` structuré, aperçu fusionné avec provenance (`contracts/desktop-dispatch.md`)
- **[ADDED]** `cards.team[]` (cycle 4) — `tplState` (`ok`/`late`/vide), `tplTarget`, `companionFile` : état du template par teammate (`contracts/desktop-dispatch.md`)
- **[NEW]** `teamToggle` — bascule caché / visible d'un teammate inactif
- **[NEW]** options tmux `@cl_panevis` (fenêtre), `@cl_idle_at`, `@cl_home` (pane) et session de réserve `cl-hidden` — `contracts/tmux-options.md` ; un launcher antérieur les ignore (panes cachés laissés dans la réserve)
- **[NEW]** `configModel`, `configApply`, `backendTest`, `backendStates` — configuration structurée
- **[NEW]** `projectSettings`, `projectApply` — réglages d'un projet
- **[NEW]** clé `TEAM_HIDE_IDLE` de `config.conf` (défaut 30, 0 = jamais) ; préférence par poste `termFontSize`
