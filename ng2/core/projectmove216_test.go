package core

// #216 (critical) : déplacer / renommer un projet — compléments boîte noire à projectmove_test.go. On compare des
// INSTANTANÉS COMPLETS du monde (arbre du projet, projects.json, config.conf, historique claude, trust.json, journal,
// dossiers de la base) : après un échec à CHAQUE étape, l'état initial est EXACT ; après un retour arrière en échec,
// « Reprendre » rejoint l'état d'un déplacement réussi et « Annuler » l'état initial. Projets factices sous t.TempDir(),
// faux conteneurs (RemoveCtr / Running injectés), EXDEV simulé par Rename injecté.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
)

type world216 map[string]string

var confKeyQuote216 = regexp.MustCompile(`\["([^"\]]+)"\]`)

// snap216 : chemin relatif → empreinte (contenu + type + droits sur Unix) de tout ce qui compte. Les copies .bak (trust,
// config) et le journal vide sont écartés : leur présence n'est pas un état du projet.
func snap216(t *testing.T, f *mvFix) world216 {
	t.Helper()
	w := world216{}
	add := func(prefix, root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			key := prefix + "/" + filepath.ToSlash(rel)
			info, _ := d.Info()
			mode := ""
			if runtime.GOOS != "windows" && info != nil {
				mode = fmt.Sprint(info.Mode().Perm())
			}
			switch {
			case d.Type()&os.ModeSymlink != 0:
				l, _ := os.Readlink(p)
				w[key] = "lien->" + l
			case d.IsDir():
				w[key] = "dir " + mode
			default:
				b, _ := os.ReadFile(p)
				h := sha256.Sum256(b)
				w[key] = "fichier " + mode + " " + hex.EncodeToString(h[:6])
			}
			return nil
		})
	}
	add("base", f.base)
	add("claude", f.claude)
	cfg := Ng2ConfigDir()
	ents, _ := os.ReadDir(cfg)
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, ".bak") || strings.Contains(n, ".tmp-") || n == "moves.json" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(cfg, n))
		if n == "trust.json" { // clés = chemins : normalisé (mise en forme libre)
			var m map[string]TrustEntry
			_ = json.Unmarshal(b, &m)
			b, _ = json.Marshal(m)
		}
		if n == "config.conf" { // la réécriture normalise les clés (`[alpha]` ↔ `["alpha"]`) : même contenu sémantique
			b = []byte(confKeyQuote216.ReplaceAllString(string(b), "[$1]"))
		}
		w["cfg/"+n] = string(b)
	}
	return w
}

func diff216(a, b world216) []string {
	var out []string
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var ks []string
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		if a[k] != b[k] {
			out = append(out, fmt.Sprintf("%s : %q → %q", k, a[k], b[k]))
		}
	}
	return out
}

// riche216 : un projet avec de quoi trahir une perte (sous-dossiers, vide, exécutable, lien, gros fichier, .git).
func riche216(t *testing.T, f *mvFix) {
	t.Helper()
	p := f.proj
	_ = os.MkdirAll(filepath.Join(p, ".git", "objects"), 0o755)
	_ = os.WriteFile(filepath.Join(p, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(p, "src", "vide"), 0o755)
	_ = os.WriteFile(filepath.Join(p, "src", "main.go"), []byte("package main\n"), 0o644)
	_ = os.WriteFile(filepath.Join(p, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	big := make([]byte, 300*1024)
	for i := range big {
		big[i] = byte(i * 7)
	}
	_ = os.WriteFile(filepath.Join(p, "data.bin"), big, 0o600)
	if runtime.GOOS != "windows" {
		_ = os.Symlink("src/main.go", filepath.Join(p, "lien"))
	}
}

func exdev216(f *mvFix) {
	f.env.Rename = func(a, b string) error {
		if a == f.proj { // seule la source change de volume ; la bascule <dest>.moving-<id> → <dest> reste sur le même
			return &os.LinkError{Op: "rename", Old: a, New: b, Err: syscall.EXDEV}
		}
		return os.Rename(a, b)
	}
}

func journal216(t *testing.T) []MoveEntry {
	t.Helper()
	var m []MoveEntry
	if b, err := os.ReadFile(movesPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// Échec à CHAQUE étape (dir, assign, conf, history, trust, ctr), déplacement simple, renommage, nouvelle catégorie,
// copie EXDEV : retour arrière complet — l'instantané du monde est identique, le journal vide, aucune catégorie ni
// copie résiduelle.
func TestEtatInitialExactApresEchecAChaqueEtape216(t *testing.T) {
	steps := []string{MoveStepDir, MoveStepAssign, MoveStepConf, MoveStepHistory, MoveStepTrust, MoveStepCtr}
	cases := []struct {
		name   string
		target MoveTarget
		exdev  bool
	}{
		{"autre catégorie", MoveTarget{Category: "AUTRE", Name: "alpha"}, false},
		{"renommage", MoveTarget{Category: "INFRA", Name: "gamma"}, false},
		{"nouvelle catégorie imbriquée", MoveTarget{Category: "NEUVE/SOUS", NewCategory: true, Name: "delta"}, false},
		{"autre système de fichiers", MoveTarget{Category: "AUTRE", Name: "epsilon"}, true},
		{"EXDEV + nouvelle catégorie", MoveTarget{Category: "NEUVE", NewCategory: true, Name: "zeta"}, true},
	}
	for _, c := range cases {
		for _, step := range steps {
			t.Run(c.name+"/"+step, func(t *testing.T) {
				f := newMvFix(t)
				riche216(t, f)
				if c.exdev {
					exdev216(f)
				}
				before := snap216(t, f)
				injected := errors.New("échec injecté à " + step)
				var seen []string
				f.env.Fail = func(s string, undo bool) error {
					if !undo {
						seen = append(seen, s)
						// le journal consigne les étapes déjà faites, dans l'ordre
						if got := journal216(t); len(got) != 1 || got[0].State != "running" {
							t.Errorf("avant %s : journal %+v, attendu une entrée « running »", s, got)
						} else if want := strings.Join(seen[:len(seen)-1], ","); strings.Join(got[0].Done, ",") != want {
							t.Errorf("avant %s : étapes consignées %v, attendu %s", s, got[0].Done, want)
						}
					}
					if s == step && !undo {
						return injected
					}
					return nil
				}
				tg := c.target
				tg.Base = f.base
				r := MoveProject(f.env, f.proj, tg)
				if r.OK || r.Step != step || !r.RolledBack || r.State == "partial" {
					t.Fatalf("résultat = %+v, attendu un échec à %s avec retour arrière", r, step)
				}
				if !strings.Contains(r.Msg, "échec injecté") {
					t.Errorf("message = %q : la cause doit être dite", r.Msg)
				}
				if d := diff216(before, snap216(t, f)); len(d) != 0 {
					t.Errorf("état initial NON restauré (%d écart(s)) :\n  %s", len(d), strings.Join(d, "\n  "))
				}
				if j := journal216(t); len(j) != 0 || len(PendingMoves(f.proj)) != 0 {
					t.Errorf("journal non vidé après un retour arrière réussi : %+v", j)
				}
				if m, _ := filepath.Glob(filepath.Join(f.base, "*", "*.moving-*")); len(m) != 0 {
					t.Errorf("copies temporaires restées : %v", m)
				}
				if len(f.ctrRm) != 0 {
					t.Errorf("conteneur supprimé alors que le déplacement a échoué : %v", f.ctrRm) // l'étape ctr est la dernière
				}
				if bl := MoveBlockers(f.env, f.proj); len(bl) != 0 {
					t.Errorf("blockers après retour arrière : %v", bl)
				}
			})
		}
	}
}

// Succès : le monde final est celui d'un déplacement parfait (aucune perte), identique pour rename et EXDEV, et le
// journal est vide.
func TestSuccesSansPerteRenameEtExdevIdentiques216(t *testing.T) {
	final := map[bool]world216{}
	for _, exdev := range []bool{false, true} {
		f := newMvFix(t)
		riche216(t, f)
		if exdev {
			exdev216(f)
		}
		src := snap216(t, f)
		r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "NEUVE", NewCategory: true, Name: "beta"})
		if !r.OK {
			t.Fatalf("exdev=%v : %+v", exdev, r)
		}
		dst := filepath.Join(f.base, "NEUVE", "beta")
		if r.Path != dst || r.Key != CtrName(dst) {
			t.Errorf("exdev=%v : résultat %+v, attendu %s / %s", exdev, r, dst, CtrName(dst))
		}
		// l'arbre du projet est identique octet pour octet à celui d'origine (hors chemin)
		for k, v := range src {
			if !strings.HasPrefix(k, "base/INFRA/alpha/") {
				continue
			}
			k2 := "base/NEUVE/beta/" + strings.TrimPrefix(k, "base/INFRA/alpha/")
			if got := snap216(t, f)[k2]; got != v {
				t.Errorf("exdev=%v : %s : %q, attendu %q", exdev, k2, got, v)
			}
		}
		if _, err := os.Lstat(f.proj); err == nil {
			t.Errorf("exdev=%v : source restée", exdev)
		}
		if m, _ := filepath.Glob(filepath.Join(f.base, "*", "*.moving-*")); len(m) != 0 {
			t.Errorf("exdev=%v : copie temporaire restée : %v", exdev, m)
		}
		if j := journal216(t); len(j) != 0 {
			t.Errorf("exdev=%v : journal non vidé : %+v", exdev, j)
		}
		if len(f.ctrRm) != 1 || !strings.HasPrefix(f.ctrRm[0], f.proj+"|") {
			t.Errorf("exdev=%v : conteneur de l'ancienne clé : %v, attendu une suppression pour %s", exdev, f.ctrRm, f.proj)
		}
		// effets annexes : affectation, historique, approbation, couleur (renommage de clé)
		if !Assigned(dst) || Assigned(f.proj) {
			t.Errorf("exdev=%v : affectation non reportée", exdev)
		}
		if _, ok := EnvTrustEntry(dst); !ok {
			t.Errorf("exdev=%v : approbation non reportée", exdev)
		}
		if _, ok := EnvTrustEntry(f.proj); ok {
			t.Errorf("exdev=%v : entrée orpheline de l'ancien chemin", exdev)
		}
		if _, err := os.Stat(filepath.Join(f.claude, "projects", ProjectDirName(dst), "s.jsonl")); err != nil {
			t.Errorf("exdev=%v : historique non retrouvé : %v", exdev, err)
		}
		if c := LoadConfig().ProjectColors; c["beta"] != "52" || c["alpha"] != "" {
			t.Errorf("exdev=%v : couleurs %v, attendu beta=52 et plus d'alpha", exdev, c)
		}
		w := snap216(t, f)
		final[exdev] = w
	}
	// rename et copie EXDEV donnent le même monde (hors chemins temporaires de test, comparés par suffixe relatif)
	norm := func(w world216) world216 {
		out := world216{}
		for k, v := range w {
			out[k] = v
		}
		return out
	}
	a, b := norm(final[false]), norm(final[true])
	for _, w := range []world216{a, b} {
		for k := range w {
			if strings.HasPrefix(k, "cfg/") || strings.HasPrefix(k, "claude/") { // chemins propres à chaque test
				delete(w, k)
			}
		}
	}
	if d := diff216(a, b); len(d) != 0 {
		t.Errorf("rename et EXDEV divergent :\n  %s", strings.Join(d, "\n  "))
	}
}

// Retour arrière en échec à chaque étape défaisable : état « partial », blocker « déplacement inachevé », projet
// inutilisable par un autre déplacement ; Reprendre → monde d'un déplacement réussi ; Annuler → état initial.
func TestRetourArriereEnEchecReprendreOuAnnuler216(t *testing.T) {
	undoSteps := []string{MoveStepTrust, MoveStepHistory, MoveStepConf, MoveStepAssign, MoveStepDir}
	for _, undoStep := range undoSteps {
		for _, exdev := range []bool{false, true} {
			name := undoStep
			if exdev {
				name += "/EXDEV"
			}
			t.Run(name, func(t *testing.T) {
				run := func(t *testing.T) (*mvFix, MoveResult, world216) {
					f := newMvFix(t)
					riche216(t, f)
					if exdev {
						exdev216(f)
					}
					before := snap216(t, f)
					broken := true
					f.env.Fail = func(s string, undo bool) error {
						switch {
						case s == MoveStepCtr && !undo:
							return errors.New("conteneur : moteur injoignable")
						case s == undoStep && undo && broken:
							return errors.New("retour arrière bloqué")
						}
						return nil
					}
					r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
					// réparation : le blocage du retour arrière disparaît, la panne du conteneur aussi
					f.env.Fail = func(string, bool) error { return nil }
					_ = broken
					return f, r, before
				}
				f, r, before := run(t)
				if r.OK || r.RolledBack || r.State != "partial" || r.Step != MoveStepCtr {
					t.Fatalf("résultat = %+v, attendu partial (échec à ctr, retour arrière bloqué à %s)", r, undoStep)
				}
				if len(r.Done) == 0 || !strings.Contains(r.Msg, "retour arrière impossible") {
					t.Errorf("partial sans étapes faites ou sans message : %+v", r)
				}
				j := journal216(t)
				if len(j) != 1 || j[0].State != "partial" {
					t.Fatalf("journal = %+v, attendu une entrée partial", j)
				}
				if bl := MoveBlockers(f.env, f.proj); !contains216(bl, "déplacement inachevé") {
					t.Errorf("blockers = %v, attendu « déplacement inachevé »", bl)
				}
				// le projet (source ET destination) est verrouillé pour tout autre déplacement
				if r2 := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "gamma"}); r2.OK {
					t.Errorf("nouveau déplacement accepté pendant un déplacement inachevé : %+v", r2)
				}
				if got := PendingMoves(f.proj); len(got) != 1 {
					t.Errorf("PendingMoves(source) = %v", got)
				}
				if got := PendingMoves(filepath.Join(f.base, "AUTRE", "beta")); len(got) != 1 {
					t.Errorf("PendingMoves(destination) = %v", got)
				}
				mid := snap216(t, f)

				// Annuler : état initial exact
				rc := CancelMove(f.env, f.proj)
				if !rc.OK && !rc.RolledBack {
					t.Fatalf("CancelMove = %+v", rc)
				}
				if d := diff216(before, snap216(t, f)); len(d) != 0 {
					t.Errorf("après Annuler : état initial NON restauré :\n  %s", strings.Join(d, "\n  "))
				}
				if j := journal216(t); len(j) != 0 {
					t.Errorf("journal après Annuler : %+v", j)
				}
				_ = mid

				// Reprendre (scénario frais) : monde d'un déplacement réussi
				f2, r2, _ := run(t)
				if r2.State != "partial" {
					t.Fatalf("2e scénario : %+v", r2)
				}
				rr := ResumeMove(f2.env, f2.proj)
				if !rr.OK {
					t.Fatalf("ResumeMove = %+v", rr)
				}
				dst := filepath.Join(f2.base, "AUTRE", "beta")
				if _, err := os.Stat(filepath.Join(dst, "run.sh")); err != nil {
					t.Errorf("projet absent de la destination après Reprendre : %v", err)
				}
				if _, err := os.Lstat(f2.proj); err == nil {
					t.Error("source restée après Reprendre")
				}
				if !Assigned(dst) || Assigned(f2.proj) {
					t.Error("affectation non reportée après Reprendre")
				}
				if _, ok := EnvTrustEntry(dst); !ok {
					t.Error("approbation non reportée après Reprendre")
				}
				if _, ok := EnvTrustEntry(f2.proj); ok {
					t.Error("entrée d'approbation orpheline après Reprendre")
				}
				if _, err := os.Stat(filepath.Join(f2.claude, "projects", ProjectDirName(dst), "s.jsonl")); err != nil {
					t.Errorf("historique non reporté après Reprendre : %v", err)
				}
				if c := LoadConfig().ProjectColors; c["beta"] != "52" || c["alpha"] != "" {
					t.Errorf("couleurs après Reprendre : %v", c)
				}
				if len(f2.ctrRm) != 1 {
					t.Errorf("étape ctr rejouée %d fois, attendu 1", len(f2.ctrRm))
				}
				if j := journal216(t); len(j) != 0 {
					t.Errorf("journal après Reprendre : %+v", j)
				}
			})
		}
	}
}

func contains216(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// La clé RemotePath du projet dans trust.json suit le projet, quel que soit l'état ; rien ne se perd, aucune entrée
// orpheline ; un projet sans entrée n'en gagne pas ; une entrée existante pour la destination n'est jamais écrasée.
func TestApprobationSuitLeProjet216(t *testing.T) {
	for _, st := range []TrustState{TrustAccepted, TrustIgnored, TrustPending, TrustNone} {
		t.Run(string(st), func(t *testing.T) {
			f := newMvFix(t)
			reg := trustPath()
			dst := filepath.Join(f.base, "AUTRE", "beta")
			ent := TrustEntry{State: st, SHA: EnvFilesSHA(f.proj), Source: "https://github.com/a/b.git", Image: "debian:12", At: "2026-11-20T10:00:00Z"}
			writeReg216(t, reg, map[string]TrustEntry{RemotePath(f.proj): ent, "/autre/projet": {State: TrustAccepted, SHA: "z"}})
			before, _ := os.ReadFile(reg)
			r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
			if !r.OK {
				t.Fatalf("%+v", r)
			}
			m := readReg216(t, reg)
			if got, ok := m[RemotePath(dst)]; !ok || got != ent {
				t.Errorf("entrée déplacée = %+v (présente=%v), attendu %+v (état ET empreinte conservés)", got, ok, ent)
			}
			if _, ok := m[RemotePath(f.proj)]; ok {
				t.Error("entrée orpheline : l'ancienne clé est restée")
			}
			if m["/autre/projet"].SHA != "z" || len(m) != 2 {
				t.Errorf("registre = %v : les autres entrées doivent rester intactes", m)
			}
			if bak, err := os.ReadFile(reg + ".bak"); err != nil || string(bak) != string(before) {
				t.Errorf("trust.json.bak = %q, %v ; attendu la version d'avant", bak, err)
			}
			// la question ne revient pas pour un projet approuvé (EnvTrust sur le nouveau chemin = le même état)
			if got := EnvTrust(dst); st != TrustNone && got != st && st != TrustPending {
				t.Errorf("EnvTrust(destination) = %s, attendu %s", got, st)
			}
		})
	}
	t.Run("sans entrée", func(t *testing.T) {
		f := newMvFix(t)
		_ = os.Remove(trustPath())
		if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}); !r.OK {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Stat(trustPath()); err == nil {
			t.Error("trust.json créé pour un projet de confiance")
		}
	})
	t.Run("entrée existante pour la destination", func(t *testing.T) {
		f := newMvFix(t)
		dst := filepath.Join(f.base, "AUTRE", "beta")
		m := readReg216(t, trustPath())
		m[RemotePath(dst)] = TrustEntry{State: TrustAccepted, SHA: "reste-d-un-ancien-projet"}
		writeReg216(t, trustPath(), m)
		before := snap216(t, f)
		r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
		if r.OK {
			t.Fatalf("déplacement accepté vers un chemin dont la clé d'approbation existe : %+v", r)
		}
		if d := diff216(before, snap216(t, f)); len(d) != 0 {
			t.Errorf("état modifié par le refus :\n  %s", strings.Join(d, "\n  "))
		}
		if got := readReg216(t, trustPath())[RemotePath(dst)]; got.SHA != "reste-d-un-ancien-projet" {
			t.Errorf("approbation de la destination écrasée : %+v", got)
		}
	})
	t.Run("registre illisible", func(t *testing.T) {
		f := newMvFix(t)
		const broken = `{"coupé":`
		_ = os.WriteFile(trustPath(), []byte(broken), 0o600)
		before := snap216(t, f)
		r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
		if r.OK {
			t.Fatalf("déplacement accepté avec un registre d'approbation illisible : %+v", r)
		}
		if d := diff216(before, snap216(t, f)); len(d) != 0 {
			t.Errorf("état initial non restauré :\n  %s", strings.Join(d, "\n  "))
		}
		if got, _ := os.ReadFile(trustPath()); string(got) != broken {
			t.Errorf("trust.json illisible réécrit : %q", got)
		}
	})
}

func writeReg216(t *testing.T, path string, m map[string]TrustEntry) {
	t.Helper()
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readReg216(t *testing.T, path string) map[string]TrustEntry {
	t.Helper()
	m := map[string]TrustEntry{}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Refus : rien n'est écrit (monde identique), journal vide, message lisible — destination existante (dossier, fichier,
// lien), hors des bases, traversée, lien sortant, destination = projet / dans le projet, catégorie absente sans
// newCategory, nom invalide.
func TestRefusLeMondeResteIdentique216(t *testing.T) {
	f := newMvFix(t)
	riche216(t, f)
	_ = os.WriteFile(filepath.Join(f.base, "AUTRE", "fichier"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(f.base, "AUTRE", "existe"), 0o755)
	dehors := t.TempDir()
	if runtime.GOOS != "windows" {
		_ = os.Symlink(dehors, filepath.Join(f.base, "SORTIE"))
		_ = os.Symlink(filepath.Join(f.base, "AUTRE", "existe"), filepath.Join(f.base, "AUTRE", "lien-dest"))
	}
	before := snap216(t, f)
	bad := []struct {
		name string
		t    MoveTarget
	}{
		{"destination existante (dossier)", MoveTarget{Base: f.base, Category: "AUTRE", Name: "existe"}},
		{"destination existante (fichier)", MoveTarget{Base: f.base, Category: "AUTRE", Name: "fichier"}},
		{"destination = le projet", MoveTarget{Base: f.base, Category: "INFRA", Name: "alpha"}},
		{"base inconnue", MoveTarget{Base: "/tmp", Category: "AUTRE", Name: "x"}},
		{"sous-dossier pris pour base", MoveTarget{Base: filepath.Join(f.base, "AUTRE"), Name: "x"}},
		{"catégorie ..", MoveTarget{Base: f.base, Category: "../..", Name: "x"}},
		{"catégorie absolue", MoveTarget{Base: f.base, Category: "/etc", Name: "x"}},
		{"catégorie \\", MoveTarget{Base: f.base, Category: `a\b`, Name: "x"}},
		{"nom ..", MoveTarget{Base: f.base, Category: "AUTRE", Name: ".."}},
		{"nom avec /", MoveTarget{Base: f.base, Category: "AUTRE", Name: "a/b"}},
		{"nom vide", MoveTarget{Base: f.base, Category: "AUTRE", Name: ""}},
		{"nom caché", MoveTarget{Base: f.base, Category: "AUTRE", Name: ".h"}},
		{"catégorie absente sans newCategory", MoveTarget{Base: f.base, Category: "ABSENTE", Name: "x"}},
		{"dans le projet lui-même", MoveTarget{Base: f.base, Category: "INFRA/alpha", NewCategory: true, Name: "x"}},
	}
	if runtime.GOOS != "windows" {
		bad = append(bad,
			struct {
				name string
				t    MoveTarget
			}{"lien sortant", MoveTarget{Base: f.base, Category: "SORTIE", NewCategory: true, Name: "x"}},
			struct {
				name string
				t    MoveTarget
			}{"destination = lien", MoveTarget{Base: f.base, Category: "AUTRE", Name: "lien-dest"}})
	}
	for _, c := range bad {
		r := MoveProject(f.env, f.proj, c.t)
		if r.OK || strings.TrimSpace(r.Msg) == "" {
			t.Errorf("%s : %+v, attendu un refus avec message", c.name, r)
		}
		if d := diff216(before, snap216(t, f)); len(d) != 0 {
			t.Errorf("%s : le monde a changé :\n  %s", c.name, strings.Join(d, "\n  "))
		}
		if j := journal216(t); len(j) != 0 {
			t.Errorf("%s : journal non vide : %+v", c.name, j)
		}
	}
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors des bases : %v", es)
	}
	// projet inconnu / hors bases
	if r := MoveProject(f.env, filepath.Join(t.TempDir(), "etranger"), MoveTarget{Base: f.base, Name: "x"}); r.OK {
		t.Errorf("projet hors des bases déplacé : %+v", r)
	}
	if r := MoveProject(f.env, filepath.Join(f.base, "INFRA", "inexistant"), MoveTarget{Base: f.base, Name: "x"}); r.OK {
		t.Errorf("projet inexistant déplacé : %+v", r)
	}
}

// Chaque blocker, seul, refuse le déplacement sans rien écrire ; chacun est dit dans MoveInfo.
func TestChaqueBlockerRefuse216(t *testing.T) {
	for name, set := range map[string]func(f *mvFix){
		"ouvert ici":         func(f *mvFix) { f.env.OpenHere = func(string) bool { return true } },
		"autre launcher":     func(f *mvFix) { f.env.Foreign = func(string) string { return "poste-b" } },
		"conteneur en cours": func(f *mvFix) { f.env.Running = func(string) string { return "podman sur pod" } },
		"déplacement inachevé": func(f *mvFix) {
			_ = saveEntry(&MoveEntry{ID: "x", From: f.proj, To: filepath.Join(f.base, "AUTRE", "z"), State: "partial", Done: []string{}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMvFix(t)
			set(f)
			before := snap216(t, f)
			journalBefore := journal216(t)
			r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
			if r.OK || strings.TrimSpace(r.Msg) == "" {
				t.Fatalf("%+v", r)
			}
			if d := diff216(before, snap216(t, f)); len(d) != 0 {
				t.Errorf("le monde a changé :\n  %s", strings.Join(d, "\n  "))
			}
			if len(journal216(t)) != len(journalBefore) {
				t.Error("journal modifié par un refus")
			}
			info, err := MoveInfoFor(f.env, f.proj)
			if err != nil || len(info.Blockers) != 1 {
				t.Errorf("MoveInfoFor = %+v %v, attendu exactement un blocker", info, err)
			}
			if name == "ouvert ici" && !info.Open {
				t.Error("MoveInfo.Open faux alors que le projet est ouvert ici")
			}
		})
	}
	// plusieurs blockers : tous dits
	f := newMvFix(t)
	f.env.OpenHere = func(string) bool { return true }
	f.env.Foreign = func(string) string { return "poste-b" }
	f.env.Running = func(string) string { return "podman sur pod" }
	if bl := MoveBlockers(f.env, f.proj); len(bl) != 3 {
		t.Errorf("blockers = %v, attendu 3", bl)
	}
	// aucun blocker : déplaçable
	g := newMvFix(t)
	if bl := MoveBlockers(g.env, g.proj); len(bl) != 0 {
		t.Errorf("blockers = %v, attendu aucun", bl)
	}
}

// Configuration : même nom dans une autre catégorie → config.conf intact octet pour octet (« si la clé change ») ;
// renommage → UNE écriture, entrées des autres projets intactes.
func TestConfReporteSeulementSiLaCleChange216(t *testing.T) {
	f := newMvFix(t)
	raw, _ := os.ReadFile(ConfigPath())
	_ = os.WriteFile(ConfigPath(), append(raw, []byte("declare -A PROJECT_HIDE_IDLE=([alpha]=\"auto\" [autre]=\"none\")\n")...), 0o600)
	b0, _ := os.ReadFile(ConfigPath())
	if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "alpha"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if b1, _ := os.ReadFile(ConfigPath()); string(b1) != string(b0) {
		t.Errorf("config.conf modifié alors que le nom du dossier ne change pas :\n%s", b1)
	}
	if _, err := os.Stat(ConfigPath() + ".bak"); err == nil {
		t.Error(".bak de config.conf écrit sans écriture")
	}
	// renommage
	g := newMvFix(t)
	raw, _ = os.ReadFile(ConfigPath())
	_ = os.WriteFile(ConfigPath(), append(raw, []byte("declare -A PROJECT_HIDE_IDLE=([alpha]=\"auto\" [autre]=\"none\")\n")...), 0o600)
	if r := MoveProject(g.env, g.proj, MoveTarget{Base: g.base, Category: "INFRA", Name: "omega"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	cfg := LoadConfig()
	if cfg.ProjectColors["omega"] != "52" || cfg.ProjectColors["alpha"] != "" {
		t.Errorf("couleurs = %v", cfg.ProjectColors)
	}
	if cfg.ProjectHideIdle["omega"] != "auto" || cfg.ProjectHideIdle["alpha"] != "" || cfg.ProjectHideIdle["autre"] != "none" {
		t.Errorf("masquage = %v (autre projet intact, entrée suivie)", cfg.ProjectHideIdle)
	}
}

// Historique : le dossier de conversations suit, jamais écrasé ; une destination dont l'historique existe déjà est
// refusée (retour arrière exact) ; sans historique : rien à faire.
func TestHistoriqueSuitSansEcraser216(t *testing.T) {
	f := newMvFix(t)
	dst := filepath.Join(f.base, "AUTRE", "beta")
	stale := filepath.Join(f.claude, "projects", ProjectDirName(dst))
	_ = os.MkdirAll(stale, 0o755)
	_ = os.WriteFile(filepath.Join(stale, "ancien.jsonl"), []byte("autre projet"), 0o644)
	before := snap216(t, f)
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.OK || r.Step != MoveStepHistory || !r.RolledBack {
		t.Fatalf("%+v, attendu un échec à history avec retour arrière", r)
	}
	if d := diff216(before, snap216(t, f)); len(d) != 0 {
		t.Errorf("état initial non restauré :\n  %s", strings.Join(d, "\n  "))
	}
	if b, _ := os.ReadFile(filepath.Join(stale, "ancien.jsonl")); string(b) != "autre projet" {
		t.Error("historique de la destination écrasé")
	}
	// sans historique du tout
	g := newMvFix(t)
	_ = os.RemoveAll(filepath.Join(g.claude, "projects"))
	if r := MoveProject(g.env, g.proj, MoveTarget{Base: g.base, Category: "AUTRE", Name: "beta"}); !r.OK {
		t.Errorf("sans historique : %+v", r)
	}
}

// Deux déplacements simultanés du MÊME projet : au plus un réussit, jamais de perte ni de projet en double — le projet
// existe à un seul endroit, complet.
func TestDeplacementsSimultanesDuMemeProjet216(t *testing.T) {
	f := newMvFix(t)
	riche216(t, f)
	src := snap216(t, f)
	var wg sync.WaitGroup
	results := make([]MoveResult, 6)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: fmt.Sprintf("copie%d", i)})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, r := range results {
		if r.OK {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d déplacements réussis, attendu exactement 1 : %+v", ok, results)
	}
	// un seul endroit, complet
	var where []string
	for _, p := range []string{f.proj} {
		if _, err := os.Stat(p); err == nil {
			where = append(where, p)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(f.base, "AUTRE"))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "copie") {
			where = append(where, e.Name())
		}
	}
	if len(where) != 1 {
		t.Fatalf("projet présent à %d endroits : %v", len(where), where)
	}
	cur := snap216(t, f)
	for k, v := range src {
		if !strings.HasPrefix(k, "base/INFRA/alpha/") {
			continue
		}
		rel := strings.TrimPrefix(k, "base/INFRA/alpha/")
		found := false
		for _, w := range where {
			prefix := "base/INFRA/alpha/"
			if w != f.proj {
				prefix = "base/AUTRE/" + w + "/"
			}
			if cur[prefix+rel] == v {
				found = true
			}
		}
		if !found {
			t.Errorf("fichier perdu ou altéré : %s", rel)
		}
	}
	if j := journal216(t); len(j) != 0 {
		t.Errorf("journal non vide : %+v", j)
	}
}
