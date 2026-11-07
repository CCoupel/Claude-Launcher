# Passer ses projets et `~/.claude` sur un stockage partagé

Ce guide déplace une installation **locale** (projets sur le disque de la machine, `~/.claude` dans
le HOME) vers un **stockage partagé** (NFS, MooseFS…), pour pouvoir lancer les projets sur une autre
machine ou dans Kubernetes avec Claude-Launcher-NG2.

> Pour garder les projets sur cette machine (hôte ou conteneur **local** podman/docker), **rien de
> tout cela n'est nécessaire** : ce guide ne sert qu'aux backends distants (docker ou podman en
> direct sur une autre machine, ssh, k8s).

La procédure **copie** sans rien supprimer : les projets d'origine, `~/.claude` et `~/.claude.json`
restent intacts jusqu'à ce que vous ayez tout validé. Le retour arrière est décrit à la fin.

---

## 1. Pourquoi il ne suffit pas de copier les projets

Le **chemin absolu** d'un projet sert de clé à plusieurs endroits. Le changer sans les mettre à jour
fait perdre :

| Donnée | Où | Clé | Perdu si rien n'est fait |
|---|---|---|---|
| Conversations (reprise `↺`, `claude --resume`) et **mémoire automatique** | `~/.claude/projects/<chemin encodé>/` | chemin, chaque caractère non alphanumérique remplacé par `-` | historique et mémoire du projet |
| Réglages par projet (confiance du dossier, MCP du projet, outils autorisés) | `~/.claude.json`, table `projects` | chemin absolu | confiance redemandée, MCP du projet |
| Historique des prompts (flèche ↑) | `~/.claude/history.jsonl`, champ `project` | chemin absolu | rappel des anciens prompts du projet |
| Racines du navigateur | `GITHUB_DIRS` de la config du launcher | chemin | les projets n'apparaissent plus |
| Exécution choisie (hôte, backend) | `~/.local/state/claude-launcher-ng2/projects.json` | chemin absolu | question redemandée à la 1re ouverture |

Et pour un backend distant, **`~/.claude` lui-même** doit être sur le stockage partagé, au même
chemin partout : conversations, teams, connexion et réglages sont lus par le launcher **et** par le
claude du conteneur distant. Or `~/.claude` contient des chemins absolus vers lui-même (barre
d'état dans `settings.json`, plugins dans `plugins/*.json`) qu'il faut réécrire.

---

## 2. Exemple utilisé dans ce guide

| | Avant | Après |
|---|---|---|
| Projets | `/mnt/c/Users/cyril/Documents/PROJETS` | `/share/MFS/ROOT/home/cyril/PROJETS` |
| Config de claude | `/home/cyril/.claude` et `/home/cyril/.claude.json` | `/share/MFS/ROOT/home/cyril/.claude` (`.claude.json` dedans) |

Toutes les commandes partent de ces variables — **adaptez-les**, puis copiez-collez les blocs dans
l'ordre, dans un même terminal :

```bash
OLD=/mnt/c/Users/cyril/Documents/PROJETS          # racine actuelle des projets
NEW=/share/MFS/ROOT/home/cyril/PROJETS            # racine sur le stockage partagé
CFG=/share/MFS/ROOT/home/cyril/.claude            # futur CLAUDE_CONFIG_DIR
BAK=~/claude-migration-$(date +%Y%m%d)            # sauvegarde
```

Si vos projets sont sous plusieurs racines (`GITHUB_DIRS` en liste), refaites les étapes 5, 7, 8 et
9 pour chaque couple `OLD` / `NEW`.

---

## 3. Prérequis

1. **Le stockage partagé est monté au même chemin** sur cette machine et sur chaque machine
   distante (ici `/share/MFS/ROOT`), et **au démarrage** : si le montage manque, plus aucun claude
   ne démarre (sa config est dessus).
   ```bash
   ls /share/MFS/ROOT                      # ici, et :
   ssh srv1 ls /share/MFS/ROOT             # sur chaque machine distante
   ```
   Sous WSL avec un client MooseFS en conteneur, la propagation doit être partagée dès le démarrage
   (`/etc/wsl.conf`, section `[boot]` : `command="mount --make-rshared /"`).
2. **Le même uid partout** (`id -u` identique ici et sur les machines distantes) : les fichiers
   écrits d'un côté doivent appartenir au même utilisateur de l'autre.
3. **Les dossiers cibles existent et sont à vous** :
   ```bash
   mkdir -p "$NEW" "$CFG" && ls -ld "$NEW" "$CFG"
   ```
4. **`python3`** sur cette machine (les réécritures de fichiers JSON des étapes 6 à 9 l'utilisent).
5. **Accès depuis Windows** (WSL) : des projets sur le stockage partagé ne sont plus visibles dans
   l'explorateur ni les éditeurs Windows comme sous `/mnt/c` ; prévoyez un autre accès (partage
   SMB du stockage, ou édition depuis WSL).

---

## 4. Tout fermer et sauvegarder

Aucun claude ne doit tourner pendant la migration (il réécrit `~/.claude.json` et les conversations
en permanence).

```bash
# dans le launcher v4 et dans NG2 : Esc, t, puis o (ferme tous les projets)
pgrep -a claude || echo "aucun claude en cours"     # doit afficher « aucun claude en cours »

mkdir -p "$BAK"
tar -C ~ -czf "$BAK/claude.tgz" .claude .claude.json
cp ~/.local/state/claude-launcher-ng2/projects.json "$BAK/" 2>/dev/null
ls -lh "$BAK"
```

---

## 5. Copier les projets

```bash
rsync -a --no-o --no-g --info=progress2 "$OLD/" "$NEW/"
```

- `rsync -a` garde l'arborescence, `.git`, les liens et les dates ; `--no-o --no-g` laisse les
  fichiers à votre utilisateur sur la cible.
- Vérifier un projet : `git -C "$NEW/PRO/Ansible_SecAgent" status` doit montrer le même état
  qu'avant.
- Les originaux sous `$OLD` ne sont **pas** supprimés.

---

## 6. Copier la config de claude et réécrire ses chemins internes

```bash
rsync -a --no-o --no-g ~/.claude/ "$CFG/"
cp -p ~/.claude.json "$CFG/.claude.json"
```

`~/.claude` contient des chemins absolus vers lui-même. Les lister :

```bash
grep -rl --include='*.json' "$HOME/.claude" "$CFG" | grep -v "/projects/"
# typiquement : settings.json (barre d'état), plugins/known_marketplaces.json,
# plugins/installed_plugins.json
```

Les réécrire vers `$CFG` **dans la copie** (les originaux ne changent pas) :

```bash
for f in "$CFG/settings.json" "$CFG/plugins/known_marketplaces.json" "$CFG/plugins/installed_plugins.json"; do
  [ -f "$f" ] && sed -i "s#$HOME/.claude#$CFG#g" "$f" && echo "réécrit : $f"
done
grep -c "$HOME/.claude" "$CFG/settings.json" "$CFG/plugins/"*.json || true   # doit afficher 0 partout
```

Si votre barre d'état ou un hook appelle un script de `~/.claude` qui utilise des outils absents des
images (`jq`…), ajoutez-les aux `packages` de l'`env.yaml` des projets concernés.

---

## 7. Renommer les dossiers de conversation (historique et mémoire)

Claude Code range les conversations d'un projet sous `projects/` à un nom dérivé de son chemin :
chaque caractère qui n'est ni une lettre ASCII ni un chiffre devient `-`
(`/mnt/c/Users/cyril/Documents/PROJETS/PRO/Ansible_SecAgent` →
`-mnt-c-Users-cyril-Documents-PROJETS-PRO-Ansible-SecAgent`).

Le script suivant **affiche d'abord** ce qu'il ferait (`DRY=1`), puis renomme (`DRY=0`) :

```bash
cat > /tmp/rename-conv.py <<'PY'
import os, re, sys
cfg, old, new, dry = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "1"
enc = lambda p: re.sub(r"[^A-Za-z0-9]", "-", p)       # encodage de Claude Code
src, dst = enc(old.rstrip("/")), enc(new.rstrip("/"))
pdir = os.path.join(cfg, "projects")
for d in sorted(os.listdir(pdir)):
    if d == src or d.startswith(src + "-"):
        t = dst + d[len(src):]
        if os.path.exists(os.path.join(pdir, t)):
            print("⚠ existe déjà, ignoré :", t); continue
        print(("à renommer : " if dry else "renommé : ") + d + "  →  " + t)
        if not dry:
            os.rename(os.path.join(pdir, d), os.path.join(pdir, t))
PY
python3 /tmp/rename-conv.py "$CFG" "$OLD" "$NEW" 1      # essai : lire la liste
python3 /tmp/rename-conv.py "$CFG" "$OLD" "$NEW" 0      # renommage
```

Le préfixe est comparé en entier : `PROJETS-PRO-…` est renommé, pas un projet voisin
`PROJETS2-…`. Le dossier `memory/` de chaque projet suit son dossier.

---

## 8. Reporter les réglages par projet et l'historique des prompts

Les réglages de chaque projet sont **dupliqués** sous le nouveau chemin (l'ancienne entrée est
gardée) ; l'historique des prompts reçoit le nouveau chemin :

```bash
cat > /tmp/rekey.py <<'PY'
import json, sys
cfg, old, new = sys.argv[1], sys.argv[2].rstrip("/"), sys.argv[3].rstrip("/")
mv = lambda p: new + p[len(old):] if p == old or p.startswith(old + "/") else None

# .claude.json : table « projects », clé = chemin absolu du projet
f = cfg + "/.claude.json"
d = json.load(open(f))
n = 0
for k in list(d.get("projects", {})):
    if mv(k) and mv(k) not in d["projects"]:
        d["projects"][mv(k)] = d["projects"][k]; n += 1
json.dump(d, open(f + ".tmp", "w"), indent=2)
import os; os.replace(f + ".tmp", f)
print(f".claude.json : {n} projet(s) reporté(s)")

# history.jsonl : champ « project » de chaque ligne
h = cfg + "/history.jsonl"
lines, n = [], 0
for l in open(h, encoding="utf-8"):
    try:
        e = json.loads(l)
        if isinstance(e.get("project"), str) and mv(e["project"]):
            e["project"] = mv(e["project"]); n += 1
        lines.append(json.dumps(e, ensure_ascii=False) + "\n")
    except ValueError:
        lines.append(l)
open(h + ".tmp", "w", encoding="utf-8").writelines(lines); os.replace(h + ".tmp", h)
print(f"history.jsonl : {n} ligne(s) mise(s) à jour")
PY
python3 /tmp/rekey.py "$CFG" "$OLD" "$NEW"
```

---

## 9. Mettre à jour le launcher

**Racines** — dans `~/.config/claude-launcher-ng2/config.conf` (et dans `~/.config/claude-launcher.conf`
si vous gardez le launcher v4), remplacer l'ancienne racine par la nouvelle :

```bash
sed -i "s#$OLD#$NEW#g" ~/.config/claude-launcher-ng2/config.conf
grep -n GITHUB_DIR ~/.config/claude-launcher-ng2/config.conf
```

(Si `GITHUB_DIRS` utilise une variable comme `$PROJECT_BASE`, c'est sa définition qu'il faut changer.)

**Exécution choisie par projet** (NG2) :

```bash
python3 - "$OLD" "$NEW" <<'PY'
import json, os, sys
old, new = sys.argv[1].rstrip("/"), sys.argv[2].rstrip("/")
f = os.path.expanduser("~/.local/state/claude-launcher-ng2/projects.json")
if os.path.exists(f):
    d = json.load(open(f))
    d = {(new + k[len(old):] if k == old or k.startswith(old + "/") else k): v for k, v in d.items()}
    json.dump(d, open(f, "w"), indent=2); print(len(d), "affectation(s)")
else:
    print("pas d'affectations NG2 (", f, ") : rien à faire")
PY
```

**Backends** — dans `~/.config/claude-launcher-ng2/backends.yaml`, déclarer la racine partagée :

```yaml
backends:
  srv1-docker: { type: docker, host: tcp://192.168.1.211:2375, shared: [/share/MFS/ROOT] }
  srv1:        { type: ssh, host: srv1, engine: podman,          shared: [/share/MFS/ROOT] }
  lab:         { type: k8s, context: k8s, namespace: claude-ng2,
                 volumes: { /share/MFS/ROOT: claude-shared } }   # PVC ReadWriteMany sur ce partage
```

---

## 10. Pointer claude vers la nouvelle config

**Pour les sessions du launcher seulement** (le plus simple, et sans toucher aux autres claude) :
le mettre dans `config.conf` — relatif au dossier de la config si le launcher est portable :

```bash
echo 'CLAUDE_CONFIG_DIR="/share/MFS/ROOT/home/cyril/.claude"' >> ~/.config/claude-launcher-ng2/config.conf
```

NG2 l'applique à toutes ses sessions, sur l'hôte et dans les conteneurs. Les claude lancés hors du
launcher (et le launcher v4) continuent sur `~/.claude` : n'ouvrez alors les projets migrés que dans
NG2.

**Pour tous les claude de la machine**, `CLAUDE_CONFIG_DIR` doit être défini **pour tous les claude** (hôte, launcher v4, NG2, qui le
transmet aux conteneurs) et **avant** le démarrage des serveurs tmux (ils transmettent leur
environnement aux sessions) :

```bash
echo "export CLAUDE_CONFIG_DIR=$CFG" >> ~/.profile
echo "export CLAUDE_CONFIG_DIR=$CFG" >> ~/.bashrc
```

Puis **ouvrir un nouveau terminal** (sous WSL, le plus sûr est `wsl --shutdown` depuis Windows, puis
rouvrir) et vérifier :

```bash
echo "$CLAUDE_CONFIG_DIR"          # → /share/MFS/ROOT/home/cyril/.claude
claude --version                   # démarre, sans demander de connexion
```

Avec cette variable, claude lit sa config dans `$CFG/.claude.json` (et non plus `~/.claude.json`).

---

## 11. Vérifier

```bash
claude-launcher-ng2 backends       # aucun ⚠ pour les backends que vous utilisez
```

Dans le launcher :

1. Les projets apparaissent sous la nouvelle racine.
2. Ouvrir un projet **sur l'hôte** : pas de question de confiance du dossier ; `/resume` dans claude
   liste les anciennes conversations ; la mémoire du projet est là
   (`ls "$CFG/projects/$(python3 -c "import re,sys;print(re.sub(r'[^A-Za-z0-9]','-',sys.argv[1]))" "$NEW/PRO/Ansible_SecAgent")/memory"`).
3. `↑` dans claude rappelle les anciens prompts du projet.
4. Configurer un projet de test sur un backend distant (F2 → socle → `←→` jusqu'au backend), l'ouvrir :
   le conteneur démarre là-bas, claude y est connecté, la team y est mise en page.

---

## 12. Revenir en arrière

Tant que rien n'a été supprimé, il suffit de rendre la main à l'ancienne config :

```bash
sed -i '/CLAUDE_CONFIG_DIR=/d' ~/.profile ~/.bashrc       # retirer la variable
sed -i "s#$NEW#$OLD#g" ~/.config/claude-launcher-ng2/config.conf
cp "$BAK/projects.json" ~/.local/state/claude-launcher-ng2/ 2>/dev/null
# nouveau terminal (ou wsl --shutdown) : ~/.claude et ~/.claude.json d'origine reprennent
```

Les conversations et changements faits **après** la migration ne sont que sur le stockage partagé
(`$CFG`, `$NEW`) : les recopier vers l'ancien emplacement avant de revenir s'ils comptent.

En dernier recours, la sauvegarde de l'étape 4 restaure l'état exact d'avant :
`tar -C ~ -xzf "$BAK/claude.tgz"`.

---

## 13. Après validation

Une fois tout vérifié (plusieurs jours d'usage), les originaux peuvent être archivés puis supprimés
(`$OLD`, `~/.claude`, `~/.claude.json`) — ou gardés comme sauvegarde. Les projets d'origine n'étant
plus utilisés, mieux vaut les retirer des racines de tout launcher encore installé, pour ne pas en
ouvrir un par erreur.
