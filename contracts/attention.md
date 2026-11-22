# Contrat — teamleader en attente de réponse (#223, v6.7.0)

> Signal « le teamleader d'un projet attend une réponse » : produit par des hooks Claude Code posés par le launcher,
> écrit dans le dossier du projet (partagé : hub, exe Windows, pod), lu par chaque launcher et affiché sur toutes les
> interfaces (fenêtre native, `--web`, coque tmux). Décisions du GATE 2 (2026-11-22) : signaux question + permission,
> notifications activées par défaut, maquette A (pastille « ❓ en attente »).

## 1. Signaux retenus (GATE 2)

| Évènement Claude Code | Filtre (`matcher`) | Effet |
|---|---|---|
| `PreToolUse` | `AskUserQuestion` | attente `question` (dont les GATE du template) |
| `Notification` | `permission_prompt` (et `elicitation_dialog` si la version de claude le connaît) | attente `permission` |
| `PostToolUse` | tous (sans `matcher`) | fin d'attente |
| `UserPromptSubmit`, `Stop`, `SessionEnd` | — | fin d'attente |

- Jamais `idle_prompt` (inactivité de 60 s) ni `Stop` comme signal d'attente : un teamleader qui attend ses
  teammates finit son tour sans attendre l'utilisateur.
- Les noms de `matcher` de `Notification` dépendent de la version de claude : le dev vérifie sur la version installée ;
  si le filtre n'existe pas, hook sans `matcher` et le launcher filtre sur le contenu (`notification_type` / `message`).

## 2. Hooks dans `settings.json` **[NEW]**

- Fichier : `<CLAUDE_CONFIG_DIR>/settings.json` (le même que `SyncSettingsEnv` / `SyncBrowserHook`, même verrou #176),
  posés par `core.SyncAttentionHook`, appelé depuis `SyncLauncherEnv`. Autres hooks conservés ; groupes du launcher
  reconnus par la marque `# cl-attention` (remplacés si la commande a changé, comme `browserHookMark`).
  JSON illisible : laissé tel quel.
- Commandes **sh POSIX**, sans `jq` ni binaire du launcher (elles tournent aussi dans les conteneurs et les pods) ;
  sortie toujours 0, jamais de sortie sur stdout (`PreToolUse` : aucune décision rendue, l'outil n'est pas bloqué) :

```sh
# attente (PreToolUse AskUserQuestion, Notification)
d="$CL_ATTENTION_DIR"; p="${TMUX_PANE#%}"; case "$p" in ''|*[!0-9]*) p=;; esac
[ -n "$d" ] && [ -n "$p" ] && mkdir -p "$d" && head -c 65536 > "$d/.$p.tmp" && mv -f "$d/.$p.tmp" "$d/$p.json"; true # cl-attention
# fin d'attente (PostToolUse, UserPromptSubmit, Stop, SessionEnd)
d="$CL_ATTENTION_DIR"; p="${TMUX_PANE#%}"; case "$p" in ''|*[!0-9]*) p=;; esac
[ -n "$d" ] && [ -n "$p" ] && [ -e "$d/$p.json" ] && rm -f "$d/$p.json"; true # cl-attention
```

- Hooks lus par claude **au démarrage de la session** : une session ouverte avant leur pose ne signale rien tant
  qu'elle n'est pas relancée (documenté).

## 3. Variable `CL_ATTENTION_DIR` **[NEW]**

- Valeur : `<projet tel que vu par la session>/.claude-launcher/attention` (`RemotePath` en conteneur / pod).
- Exportée **dans le script de démarrage de claude du projet** (`core.StartCommands` → `HostStartKeys` /
  `CtrStartScript`), juste avant `claude`, précédée de `rm -rf "$CL_ATTENTION_DIR"` (état d'une session précédente
  effacé à chaque lancement neuf). Pas dans l'environnement tmux de la session : les panes des teammates (créés par
  claude dans tmux) ne l'héritent pas → **seul le teamleader écrit**. Garde-fou si une version de claude transmettait
  l'environnement aux teammates : pour un projet ouvert ici, seul le fichier du pane leader (dernière mise en page) compte.
- Absente (session lancée hors launcher) : les hooks ne font rien.
- `.git/info/exclude` du projet reçoit `/.claude-launcher/attention/` (même mécanisme que `init-proposed`, #215) :
  jamais commité.

## 4. Fichier d'état

`<projet>/.claude-launcher/attention/<n° de pane>.json` = charge utile JSON brute du hook (≤ 64 Kio). Interprétation
(`core.ReadAttention(projet) (Attention, bool)`) :

| Champ lu | Résultat |
|---|---|
| `hook_event_name` = `PreToolUse` | `kind: "question"`, `text` = `tool_input.questions[0].question` |
| `hook_event_name` = `Notification` | `kind: "permission"`, `text` = `message` |
| autre / JSON illisible | ignoré |

- `text` tronqué à 200 caractères, caractères de contrôle retirés. `since` = date du fichier. `id` = `<pane>-<since en
  ms>` (une attente = un `id`). Plusieurs fichiers : le plus récent.
- Ignoré (et supprimé par le launcher qui porte la session du projet) : fichier de plus de 24 h, ou session du projet
  absente (projet fermé).

## 5. Lecture par le launcher

- Boucle `attention` (fenêtre native / `--web` et coque) toutes les **2 s**, sur les projets **ouverts ici** et ceux
  dont la session vit ailleurs (`ctr.Live`, autre launcher, pod) — jamais tout l'arbre. Une lecture de dossier par
  projet, par le partage (aucun `wsl.exe` / `kubectl exec` lancé). Seuil : attente affichée **≤ 5 s** après le hook.
- Transition repérée par `id` : nouvelle attente → évènement `attention` ; disparition → `attentionEnd`.

## 6. Interface native et `--web` **[NEW]**

- Instantané : `Row.attention` (ligne de projet) et `TabInfo.attention` (onglet ouvert), absents hors attente :
  `{ "kind": "question"|"permission", "text": "<question ou permission>", "since": "<RFC 3339>", "id": "<id>" }`.
- Évènement `attention` : `{ "path", "name", "kind", "text", "id" }` ; évènement `attentionEnd` : `{ "path", "id" }`.
- Rendu : maquette retenue **A** (`attente_teamleader__badge_notifications.html`, version retenue produite après le
  GATE 2) — arbre : pastille orange « ❓ en attente » (infobulle : `text`) ; onglet du projet : ❓ et soulignement
  orange ; pastille Teamleaders : contour orange **sans compteur ni icône seule** (contrainte tab_bar) ; vignette
  Teamleaders : cadre orange, pastille dans le titre, bandeau « ❓ question : « … » » (ou « permission : … ») au-dessus
  du terminal.

## 7. Notifications **[NEW]**

- Option `config.conf` `ATTENTION_NOTIFY` (`bool`, rubrique « Teams et panes », libellé « Notifications d'attente »,
  `effect: immediate`). Défaut **`1` (activées)**, décision GATE 2. Le badge, lui, est toujours affiché.
- **Fenêtre native** (build `wails`) : notification système par `runtime.SendNotification` (Wails v2.17), une par `id`,
  retirée à `attentionEnd` (`RemoveNotification`) ; clic (`OnNotificationResponse`) : fenêtre au premier plan et
  onglet du projet sélectionné (ouvert si besoin) : le Go met la fenêtre au premier plan et émet l'évènement
  **`attentionFocus`** `{ "path", "id" }` ; le frontend sélectionne alors l'onglet du projet (`path`). Émis par la
  fenêtre native seulement (jamais vers le navigateur miroir #163) ; `id` inconnu ou attente déjà terminée :
  l'onglet est quand même sélectionné. Notifications indisponibles (`IsNotificationAvailable` faux) :
  badge seul, ligne dans le journal.
- **Navigateur `--web`** : API `Notification` du navigateur ; option active par défaut, l'autorisation du navigateur est
  demandée par un geste de l'utilisateur (les navigateurs refusent une demande sans clic) : bandeau discret « Autoriser
  les notifications d'attente » affiché une fois tant que `Notification.permission` vaut `default`, ou à la
  première attente ; refus (`denied`) : badge seul, plus de bandeau ;
  `tag` = `id` (pas de doublon entre onglets d'un même navigateur), fermée à `attentionEnd` ; clic : `window.focus()`
  et onglet du projet. Contexte non sécurisé (http hors `127.0.0.1` / `localhost`) : pas de notification, mention
  sous l'option. Titre de la page préfixé « ❓ » pendant une attente.
- Une notification au plus par `id` et par interface ; aucune après la fin de l'attente.
- Instantané : champ **`attentionNotify`** (`bool`) = valeur courante de `ATTENTION_NOTIFY`, **toujours envoyé**
  (`true|false`, jamais `omitempty` : un `false` omis se lirait « activé ») ; côté client, **absent = activé** (serveur
  plus ancien ; option non renseignée = défaut `1`). Suit le rechargement à chaud de `config.conf`
  (#225) : un changement d'option renvoie un instantané. Le client web le lit pour couper (ou rétablir) ses
  notifications navigateur sans recharger la page ; le badge n'en dépend pas.

## 8. Coque tmux

- Navigateur de projets : pastille ` ❓ en attente ` (fond orange) sur la ligne du projet ; vignette du projet dans
  « ⌗ Teamleaders » : bordure orange, « ❓ en attente » dans le titre et ligne de la question (maquette A). Pas de
  notification système.
