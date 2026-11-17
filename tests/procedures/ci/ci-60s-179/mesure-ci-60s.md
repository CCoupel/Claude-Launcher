# Procedure de Test — CI sous 60 s (#179)

**Version** : 6.3.0
**Date** : 2026-11-16
**Testeur** : QA

Reference : `.claude/workflow/plan-v6.3.0.md` (LOT-1), `scripts/ci.sh`, garde dans `tests/METRICS.md` (section « Garde LOT-1 »).

## Prerequis

- [ ] Environnement : conteneur de developpement (celui de la session), poste au repos (aucun autre build ni test en cours)
- [ ] Donnees : branche `milestone/v6.3.0` au SHA du lot, arbre propre (`git status --short` vide)
- [ ] Acces : `go`, `gcc`, `node`, `tmux` disponibles ; aucun vrai projet ni vrai `claude` utilise

## Critere (decision D1)

Le critere de #179 est : **`scripts/ci.sh` hors etape image <= 60 s**, moyenne de trois passes. La procedure releve la duree
de **chaque etape** (pas seulement le total) : controle JS (`node --check`), `go vet`, `go test -race` (par paquet), garde
des sockets, compilation croisee Windows, `govulncheck`. L'etape image (construction/controle du chart, LOT-4) est
**exclue** du total mesure ici ; son budget propre (a chaud, <= 30 s vise) sera ajoute en LOT-4.

## Scenarios

### Scenario 1 — Trois passes de `scripts/ci.sh`, moyenne <= 60 s

**Validation** : AUTO — **Issues** : #179

**Objectif** : la moyenne de trois passes consecutives de `scripts/ci.sh` (hors etape image) est <= 60 s, chaque passe est verte, et la duree de chaque etape est relevee. Equivalent automatise : la boucle ci-dessous (rejouable par QA, sortie `OK`/`ECHEC`).

```bash
tot=0; for i in 1 2 3; do
  s=$(date +%s.%N); PS4='+[${EPOCHREALTIME}] ' bash -x scripts/ci.sh >/tmp/ci-pass-$i.log 2>&1 || { echo "ECHEC passe $i (voir /tmp/ci-pass-$i.log)"; exit 1; }
  d=$(echo "$(date +%s.%N) - $s" | bc); echo "passe $i : ${d} s"; tot=$(echo "$tot + $d" | bc)
done
moy=$(echo "scale=1; $tot / 3" | bc); echo "moyenne : ${moy} s"
[ "$(echo "$moy <= 60" | bc)" = 1 ] && echo OK || echo "ECHEC : moyenne > 60 s"
```

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Premiere passe (cache Go possiblement tiede) | Verte ; duree relevee (`d1`) | | |
| 2 | Deuxieme passe | Verte ; `d2` relevee | | |
| 3 | Troisieme passe | Verte ; `d3` relevee | | |
| 3b | Par passe, relever la duree de chaque etape : ecarts entre horodatages `+[<epoch>]` du journal `/tmp/ci-pass-N.log` aux lignes `node --check`, `go vet`, `go test -race`, `govulncheck`, `go build`/compilation Windows (et toute etape image, a exclure du total) | Tableau des etapes rempli ci-dessous ; aucune etape ne domine sans explication | | |
| 4 | Calculer `(d1+d2+d3)/3` | <= 60 s | | |
| 5 | `ls /tmp/tmux-$(id -u)` apres les passes | Aucun socket `cl-test-*`/`cl-desk-*` laisse | | |

Duree par etape (s) :

| Etape | Passe 1 | Passe 2 | Passe 3 | Moyenne |
|-------|---------|---------|---------|---------|
| node --check JS | | | | |
| go vet | | | | |
| go test -race (total) | | | | |
| garde sockets / nettoyage | | | | |
| compilation croisee Windows | | | | |
| govulncheck | | | | |
| etape image (exclue du total, LOT-4) | | | | |

Releves : d1 = ___ s ; d2 = ___ s ; d3 = ___ s ; moyenne = ___ s ; pire passe = ___ s.

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 2 — Aucun test retire, couverture maintenue

**Validation** : AUTO — **Issues** : #179

**Objectif** : l'acceleration n'a retire aucun test ni affaibli la couverture par rapport a la reference de `dev-backend`. Commandes et valeurs de reference : `tests/METRICS.md`, section « Garde LOT-1 ».

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | Lancer le releve du nombre de tests (METRICS.md) | Nombre par paquet >= reference | | |
| 2 | Lancer le releve de couverture (METRICS.md) | Couverture par paquet >= reference − 0,5 point | | |
| 3 | `git diff milestone/v6.3.0~ -- 'ng2/**/*_test.go' \| grep '^-.*\(Fatal\|Error\|assert\)'` | Aucune assertion supprimee (relecture des lignes retirees) | | |

**Verdict** : [ ] PASS  [ ] FAIL

---

### Scenario 3 — Duree par test de `ui/desktop` (diagnostic, si scenario 1 FAIL)

**Validation** : AUTO — **Issues** : #179

**Objectif** : identifier les tests lents restants.

| Etape | Action | Resultat Attendu | Resultat Obtenu | OK ? |
|-------|--------|-----------------|----------------|------|
| 1 | `cd ng2 && TMUX_TMPDIR=$(mktemp -d /tmp/clci.XXXXXX) CGO_ENABLED=1 go test -race -v ./ui/desktop 2>&1 \| grep -E '^--- (PASS\|FAIL)' \| sort -t'(' -k2 -rn \| head -15` | Aucun test > 5 s hors attente justifiee (rapport) | | |

**Verdict** : [ ] PASS  [ ] FAIL

## Criteres de Validation

- [ ] Moyenne de trois passes <= 60 s hors etape image, les trois vertes, duree de chaque etape relevee
- [ ] Nombre de tests et couverture par paquet >= reference
- [ ] Aucun socket tmux de test laisse

## Notes QA

Les durees de l'issue (62 s) et de la memoire (111 s) se contredisent : seule la reference de `dev-backend` fait foi.
