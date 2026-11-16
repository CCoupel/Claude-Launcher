# Contrat — interface web : réglage, commutateur, proxy inverse (v6.3.0)

> Complète `contracts/desktop-dispatch.md` (« v6.2.0 — LOT-4 : interface web servie par la fenêtre native »). Ce
> fichier décrit ce que #183 et #171 ajoutent. Les protections de #163 restent toutes actives : session en mémoire,
> cookie `HttpOnly` et `SameSite=Strict`, contrôle de `Host`, contrôle d'origine, corps JSON, en-têtes de sécurité.

## Réglage `desktop_web` de `backends.yaml` **[CHANGED]** (#183, #171)

Emplacements inchangés : `defaults.desktop_web`, `hubs.<machine>.desktop_web`, `hubs.<machine>.<système>.desktop_web`.

```yaml
desktop_web:
  enabled: true                  # NEW (#183) — absent : voir la règle ci-dessous
  listen: 127.0.0.1:8787         # inchangé
  allow_lan: false               # inchangé
  url: https://launcher.exemple  # NEW (#171) — adresse publique derrière un proxy inverse
```

**`enabled`** (commutateur)

| `enabled` | `listen` | Interface web |
|-----------|----------|---------------|
| absent | vide ou `off` | éteinte (v6.2.0) |
| absent | renseigné | allumée (v6.2.0) |
| `false` | quelconque | **éteinte**, `listen` conservé |
| `true` | renseigné | allumée |
| `true` | vide ou `off` | erreur de réglage : « interface web activée sans adresse d'écoute » |

- L'interface enregistre toujours `enabled` explicitement quand l'utilisateur touche au commutateur.
- Allumer ou éteindre s'applique à l'enregistrement, sans relancer le launcher (`applyWebUI`). Le serveur des
  maquettes (`browser_server`) n'est pas touché, et inversement.
- Quand l'utilisateur allume le commutateur sans adresse saisie, l'interface propose `127.0.0.1:8787`.
- Mode serveur (`desktop --web`) : `enabled` est ignoré, l'interface est le processus lui-même.

**`url`** (adresse publique)

- Forme : `http://hôte[:port]` ou `https://hôte[:port]`, sans chemin, sans paramètres, sans identifiants. Autre
  forme : erreur de réglage, interface éteinte, raison dans `webUI.msg`.
- Sources, par priorité : option `--url` > variable `CL_WEB_URL` > `desktop_web.url`.
- L'interface est toujours servie à la racine du nom (pas de préfixe de chemin).

**Modification depuis un client web** : refusée pour tout `desktop_web` (`enabled` et `url` compris) et pour
`browser_server`, comme en v6.2.0.

## Effets de l'adresse publique (#171)

| Contrôle | Sans `url` (v6.2.0) | Avec `url` |
|----------|---------------------|------------|
| `Host` admis | boucle locale, hôte d'écoute, nom de la machine — sur le port d'écoute | les mêmes, **plus** l'hôte de `url` sur le port de `url` (ou sans port si `url` n'en a pas) |
| Origine admise | `http://<Host>` | `http://<Host>` pour un hôte local ; **l'origine de `url` exactement** (schéma, hôte, port) pour l'hôte public |
| Cookie de session | `HttpOnly`, `SameSite=Strict` | les mêmes, **plus `Secure` sur toute réponse** quand `url` est en `https` |
| Lien d'amorçage affiché | `http://<écoute>/?t=…` | `<url>/?t=…` |

- Conséquence du cookie `Secure` : un accès direct en HTTP au port du pod ne garde pas de session, et ne donne
  aucun cookie réutilisable en HTTPS.
- **Aucun en-tête `X-Forwarded-*`, `Forwarded` ou `X-Real-IP` n'est lu.** Le schéma et l'hôte publics viennent
  seulement de `url`.
- Un autre nom d'hôte, ou une autre origine, reçoit `403` comme aujourd'hui.

## Routes HTTP **[CHANGED]**

| Route | Session | Changement |
|-------|---------|------------|
| `GET /healthz` | aucune, **sans contrôle de `Host`** | **NEW** — `200`, corps `ok`. Vivant : le serveur répond |
| `GET /readyz` | aucune, sans contrôle de `Host` | **NEW** — `200` `ok` quand l'application a fini son chargement initial, `503` avant |
| `GET /api/events` | cookie | en-têtes ajoutés : `X-Accel-Buffering: no`, `Cache-Control: no-store, no-transform` |
| autres | inchangé | — |

- `/healthz` et `/readyz` ne rendent aucune information (ni version, ni nom, ni état des projets). Elles sont
  exemptées du contrôle de `Host` parce que les sondes du cluster appellent l'adresse du pod.
- Flux d'évènements : battement `: ping` au plus toutes les 20 s (inchangé, compatible avec un délai de proxy de
  60 s).

## Reconnexion du flux côté interface **[CHANGED]** (#171)

- Coupure du flux : nouvelle tentative après 1,5 s, puis délai doublé à chaque échec jusqu'à 15 s ; remis à 1,5 s
  dès qu'une connexion aboutit. Après reconnexion : `init` puis remise à jour de l'affichage (inchangé).
- Réponse `401` ou `403` à `/api/events` ou `/api/call` : la session est perdue. L'interface arrête de réessayer et
  affiche « session expirée — rouvrir le lien d'accès ».

## Instantané — champ `webUI` **[CHANGED]** (#183, #171)

```json
"webUI": { "state": "off" | "on" | "error", "listen": "127.0.0.1:8787", "lan": false, "warn": "", "msg": "",
           "enabled": true, "url": "https://launcher.exemple", "server": false }
```

| Champ | Sens |
|-------|------|
| `enabled` | **NEW** — valeur effective du commutateur |
| `url` | **NEW** — adresse publique effective, vide sinon |
| `server` | **NEW** — vrai en mode serveur (`desktop --web`) : `state` vaut alors toujours `on`, `listen` est l'adresse du lancement |

Jamais le jeton, comme aujourd'hui.

## `configModel` / `configApply`, `kind: "hub"` **[CHANGED]** (#183)

`settings.desktop_web` gagne `enabled` (booléen ou absent) et `url` (chaîne). Les clés de changement
`desktop_web.enabled` et `desktop_web.url` suivent les règles des clés existantes `desktop_web.listen` et
`desktop_web.allow_lan` (validation, refus à un client web, surcharge par système en lecture seule).

## Valeurs effectives par système **[CHANGED]** (#183, exigence de cohérence des Réglages)

`browser_server` et `desktop_web` se résolvent champ par champ : `hubs.<machine>.<système>` > `hubs.<machine>` >
`defaults`. Les rubriques « Serveur de maquettes » et « Interface web » affichent et modifient la valeur
**effective** pour ce poste et ce système, jamais une valeur masquée.

- `configModel`, `kind: "hub"` : nouveau champ `model.resolved` =
  `{ "browser_server": {listen,url,allow}, "desktop_web": {listen,allow_lan,enabled,url}, "sources": {...} }`.
  `sources` donne, pour chaque champ (`browser_server.listen`, `desktop_web.enabled`…), la couche qui gagne :
  `"defaults"`, `"hub"` (`hubs.<machine>`), le nom du système (`"windows"`, `"linux"`, `"darwin"` : surcharge) ou
  `""` (posé nulle part). `settings` et `overrides` restent inchangés (lecture des surcharges conservée).
- `configApply`, `kind: "hub"` : les changements `set`/`delete` d'`at` = `["browser_server"]`, `["desktop_web"]`
  (objet effectif entier) ou `["<réglage>", "<champ>"]` sont portés **sur la couche qui gagne** pour le système du
  launcher : si `hubs.<machine>.<système>` définit le champ, c'est lui qui est écrit, sinon `hubs.<machine>`. Un
  champ absent de l'objet remplaçant est retiré de chaque couche qui le pose. Un `at` qui commence par un système
  (`["windows", …]`) est appliqué tel quel.
