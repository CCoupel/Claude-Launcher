# Contrat — niveaux d'épuisement des quotas : `usage-levels.json` (v6.8.1, #237)

> Producteur : Claude-Launcher ≥ 6.8.1 (`core`), **seule source** du calcul.
> Consommateur : panneau milestone du template (CCoupel/claude_project_template#160, `hooks/lib.ts`
> `parseUsageLevels` / `matchLevel` / `levelDisplay`). Contenu convenu dans l'issue #237 : ce contrat le fige sans
> le modifier ; les points « Précisions » ne font que lever des cas non écrits.

## Emplacement

`$CLAUDE_CONFIG_DIR/claude-launcher-cache/usage-levels.json` — voisin de `usage.json` (réponse brute de l'endpoint
`/api/oauth/usage`, **jamais modifiée** par ce changement). Même dossier partagé (`core.sharedUsageDir()`), donc
visible de tous les conteneurs et pods du compte.

## Schéma 1

```json
{
  "schema": 1,
  "computed_at": "2026-11-23T13:51:32.226Z",
  "source_mtime": "2026-11-23T13:48:49.000Z",
  "windows": [
    { "kind": "session", "scope": null, "resets_at": "2026-11-23T18:00:00.487638+00:00", "percent": 6,
      "exhausts_at": "2026-11-23T14:33:32.226Z", "level": "red", "alert": "⚠ épuisé ~14:33" },
    { "kind": "weekly_all", "scope": null, "resets_at": "2026-11-25T10:00:00.487661+00:00", "percent": 73,
      "exhausts_at": "2026-11-25T10:30:00.487Z", "level": "yellow", "alert": null },
    { "kind": "weekly_scoped", "scope": { "model": { "id": null, "display_name": "Fable" }, "surface": null },
      "resets_at": "2026-11-25T10:00:00+00:00", "percent": 0,
      "exhausts_at": null, "level": null, "alert": null }
  ]
}
```

| Champ | Type | Règle |
|-------|------|-------|
| `schema` | entier | `1`. Tout autre valeur → le consommateur ignore le fichier. |
| `computed_at` | RFC3339, UTC, millisecondes (`…T13:51:32.226Z`) | instant du calcul (`now` de la projection). |
| `source_mtime` | RFC3339, UTC, millisecondes | mtime de `usage.json` **lu pour ce calcul** (stat du fichier, pas l'heure courante), tronquée à la milliseconde. Le consommateur compare à l'mtime de `usage.json` avec une tolérance de ±1 s (stockage partagé à la seconde : MooseFS, 9P) ; écart → fichier périmé, ignoré. |
| `windows[]` | tableau | une entrée par élément de `limits[]` de `usage.json` dont `kind` ∈ {`session`, `weekly_all`, `weekly_scoped`}, **dans l'ordre de `limits[]`** ; `weekly_scoped` peut apparaître plusieurs fois (une par modèle). Aucune limite reconnue → `[]`. |
| `kind` | chaîne | valeur de l'endpoint telle quelle. |
| `scope` | objet ou `null` | objet `scope` de l'endpoint **recopié octet pour octet** (JSON brut), `null` si absent ou `null` (cas de `session` / `weekly_all`). Le consommateur joint sur `kind` + `scope` (clé : `model.id`, sinon `model.display_name`, sinon `surface`), repli `kind` + `resets_at`. |
| `resets_at` | chaîne RFC3339 ou `null` | valeur de l'endpoint telle quelle (microsecondes et décalage `+00:00` conservés) ; `null` si absente. |
| `percent` | nombre | `percent` de l'endpoint tel quel (nombre JSON, entier ou décimal). |
| `exhausts_at` | RFC3339 UTC ms, ou `null` | instant d'épuisement projeté ; `null` si projection non fiable. Déjà épuisé (`percent` ≥ 100) → `computed_at`. |
| `level` | `"red"` \| `"yellow"` \| `"green"` \| `null` | voir « Calcul » ; `null` = projection non fiable (pas de couleur). `yellow` est affiché **orange**. |
| `alert` | chaîne ou `null` | `"⚠ épuisé ~<heure>"` (ou `"⚠ épuisé"` si déjà épuisé) seulement si `level` = `red`, sinon `null`. Texte de `core.Exhaustion.Alert` (heure locale du producteur, format `FormatReset`) : **indicatif** ; le consommateur réaffiche l'heure depuis `exhausts_at`. |

## Calcul (`core.ProjectExhaustion`, commun coque tmux / fenêtre native / `--web` / fichier)

Projection linéaire inchangée (rythme moyen depuis le début de la fenêtre, extrapolé à 100 % ; non fiable si fenêtre
inconnue, `resets_at` invalide ou passé, consommation nulle, fenêtre entamée depuis moins de max(10 min, 5 %)).
Niveau par rapport au renouvellement `R` et à l'épuisement projeté `E` :

| Niveau | Condition | Fenêtre |
|--------|-----------|---------|
| `red` | `E` < `R` | toutes |
| `yellow` (orange) | `R` ≤ `E` < `R` + **1 h** | session 5 h (`session`) |
| `yellow` (orange) | `R` ≤ `E` < `R` + **5 h** | hebdomadaires : `weekly_all`, `weekly_scoped` |
| `green` | au-delà | toutes |
| `null` | projection non fiable | toutes |

La marge dépend de la **durée de la fenêtre** (`UsageWindow` : 5 h → 1 h, 7 j → 5 h) ; elle remplace la marge unique
de 6 h de la v6.8.0 (#235). Bornes : `E` = `R` + marge exactement → `green` (comparaison stricte, comme avant).

## Écriture

- **Atomique** : fichier temporaire unique dans le même dossier puis renommage (`core.WriteFileAtomic`) — jamais de
  fichier partiel visible ; plusieurs launchers peuvent écrire en même temps (dernier renommage gagnant, contenus
  équivalents).
- **Quand** :
  1. après chaque relevé réussi de `usage.json` par le launcher (`FetchUsage`) ;
  2. à chaque lecture de la consommation par un launcher (`ReadUsage` : coque, fenêtre native, `--web`) si le fichier
     est absent, illisible ou si son `source_mtime` diffère de l'mtime courante de `usage.json` (cas d'un `usage.json`
     écrit par le panneau, recopié par `LinkUsageCache`, ou d'un premier démarrage). Sinon, aucune écriture (pas
     d'écriture périodique sur le stockage partagé).
- `usage.json` absent ou sans `limits` exploitable → fichier non écrit (l'ancien, périmé par `source_mtime`, reste).
- Le fichier n'est pas recalculé tant que `usage.json` ne change pas : `computed_at` en donne l'âge.
- Sous test : écriture refusée hors du dossier temporaire (`guardWrite`) — `CLAUDE_CONFIG_DIR` temporaire obligatoire.

## Hors contrat

- `usage.json`, `usage.json.err`, `usage.json.wait` : inchangés.
- Panneau sans launcher (pas de fichier valide) : pas de couleur — décidé côté template (#160).
