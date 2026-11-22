package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// banc de test : base, projet avec fichiers, affectation, couleur, historique claude, entrée d'approbation.
type mvFix struct {
	t      *testing.T
	base   string
	proj   string
	claude string
	env    MoveEnv
	ctrRm  []string
}

func newMvFix(t *testing.T) *mvFix {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	base := filepath.Join(t.TempDir(), "PROJETS")
	proj := filepath.Join(base, "INFRA", "alpha")
	if err := os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(base, "AUTRE"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, "a.txt"), []byte("aaa"), 0o644)
	_ = os.WriteFile(filepath.Join(proj, ".claude-launcher", "env.yaml"), []byte("image: debian:12\n"), 0o644)
	_ = os.WriteFile(ConfigPath(), []byte(defaultConfigFor()+"\ndeclare -A PROJECT_COLORS=([alpha]=\"52\")\n"), 0o600)
	claude := t.TempDir()
	_ = os.MkdirAll(filepath.Join(claude, "projects", ProjectDirName(proj)), 0o755)
	_ = os.WriteFile(filepath.Join(claude, "projects", ProjectDirName(proj), "s.jsonl"), []byte("{}"), 0o644)
	if err := WriteAssignment(proj, "host"); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkEnvPending(proj, "https://github.com/a/b.git", "debian:12"); err != nil {
		t.Fatal(err)
	}
	info, _ := EnvTrustInfoFor(proj)
	if _, err := AnswerEnvTrust(proj, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	f := &mvFix{t: t, base: base, proj: proj, claude: claude}
	f.env = MoveEnv{Roots: []string{base}, ClaudeDir: claude,
		Foreign: func(string) string { return "" }, Running: func(string) string { return "" },
		RemoveCtr: func(p, b string) error { f.ctrRm = append(f.ctrRm, p+"|"+b); return nil }}
	return f
}

func (f *mvFix) state(path string) (dirOK, assign, trust, hist bool, color string) {
	_, err := os.Stat(filepath.Join(path, "a.txt"))
	dirOK = err == nil
	assign = Assigned(path)
	_, trust = EnvTrustEntry(path)
	_, herr := os.Stat(filepath.Join(f.claude, "projects", ProjectDirName(path), "s.jsonl"))
	hist = herr == nil
	color = LoadConfig().ProjectColors[filepath.Base(path)]
	return
}

func TestMoveSucces(t *testing.T) {
	f := newMvFix(t)
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if !r.OK {
		t.Fatalf("échec : %+v", r)
	}
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if r.Path != dst || r.Key != CtrName(dst) {
		t.Errorf("résultat = %+v", r)
	}
	dirOK, assign, trust, hist, color := f.state(dst)
	if !dirOK || !assign || !trust || !hist || color != "52" {
		t.Errorf("destination : dir=%v assign=%v trust=%v hist=%v color=%q", dirOK, assign, trust, hist, color)
	}
	if _, err := os.Stat(f.proj); err == nil {
		t.Error("source encore là")
	}
	if _, a, tr, h, c := f.state(f.proj); a || tr || h || c != "" {
		t.Errorf("ancienne clé non nettoyée : assign=%v trust=%v hist=%v color=%q", a, tr, h, c)
	}
	if len(f.ctrRm) != 1 || !strings.HasPrefix(f.ctrRm[0], f.proj+"|") {
		t.Errorf("suppression du conteneur : %v", f.ctrRm)
	}
	// l'approbation suit SANS nouvelle question : état accepted, empreinte inchangée (les fichiers sont les mêmes)
	if EnvTrust(dst) != TrustAccepted {
		t.Errorf("approbation après déplacement : %s", EnvTrust(dst))
	}
	if len(readMovesCached()) != 0 {
		t.Error("journal non vidé")
	}
}

// Échec injecté à chaque étape : état initial exact (retour arrière complet), sauf conteneur (dernière, non réversible).
func TestMoveEchecARetourArriere(t *testing.T) {
	for _, step := range moveStepOrder {
		t.Run(step, func(t *testing.T) {
			f := newMvFix(t)
			f.env.Fail = func(s string, undo bool) error {
				if s == step && !undo {
					return errors.New("échec injecté")
				}
				return nil
			}
			r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "NEUVE/SOUS", NewCategory: true, Name: "beta"})
			if r.OK || r.Step != step || !r.RolledBack || r.State != "" {
				t.Fatalf("résultat = %+v", r)
			}
			dirOK, assign, trust, hist, color := f.state(f.proj)
			if !dirOK || !assign || !trust || !hist || color != "52" {
				t.Errorf("état initial non restauré : dir=%v assign=%v trust=%v hist=%v color=%q", dirOK, assign, trust, hist, color)
			}
			if _, err := os.Stat(filepath.Join(f.base, "NEUVE")); err == nil {
				t.Error("catégorie créée par le déplacement non retirée")
			}
			if len(readMovesCached()) != 0 {
				t.Error("journal non vidé après retour arrière")
			}
			if step == MoveStepCtr && len(f.ctrRm) != 0 {
				t.Error("conteneur supprimé malgré l'échec")
			}
		})
	}
}

// Retour arrière en échec : « partial », entrée gardée, blocker « déplacement inachevé » ; Reprendre ou Annuler.
func TestMovePartialReprendre(t *testing.T) {
	f := newMvFix(t)
	n := 0
	f.env.Fail = func(s string, undo bool) error {
		switch {
		case s == MoveStepHistory && !undo && n == 0:
			n++
			return errors.New("échec history")
		case s == MoveStepAssign && undo:
			return errors.New("échec du retour arrière assign")
		}
		return nil
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.OK || r.State != "partial" || r.RolledBack || len(r.Done) == 0 {
		t.Fatalf("résultat = %+v", r)
	}
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if bl := MoveBlockers(f.env, dst); !anyContains(bl, "déplacement inachevé") {
		t.Errorf("blockers = %v", bl)
	}
	if bl := MoveBlockers(f.env, f.proj); !anyContains(bl, "déplacement inachevé") {
		t.Errorf("blockers (source) = %v", bl)
	}
	f.env.Fail = nil
	r2 := ResumeMove(f.env, dst)
	if !r2.OK || r2.Path != dst {
		t.Fatalf("reprise = %+v", r2)
	}
	if d, a, tr, h, c := f.state(dst); !d || !a || !tr || !h || c != "52" {
		t.Errorf("après reprise : %v %v %v %v %q", d, a, tr, h, c)
	}
	if len(readMovesCached()) != 0 {
		t.Error("journal non vidé après reprise")
	}
}

func TestMovePartialAnnuler(t *testing.T) {
	f := newMvFix(t)
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepTrust && !undo {
			return errors.New("échec trust")
		}
		if s == MoveStepConf && undo {
			return errors.New("échec du retour arrière conf")
		}
		return nil
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.State != "partial" {
		t.Fatalf("résultat = %+v", r)
	}
	f.env.Fail = nil
	dst := filepath.Join(f.base, "AUTRE", "beta")
	r2 := CancelMove(f.env, dst)
	if !r2.RolledBack {
		t.Fatalf("annulation = %+v", r2)
	}
	if d, a, tr, h, c := f.state(f.proj); !d || !a || !tr || !h || c != "52" {
		t.Errorf("état initial non restauré : %v %v %v %v %q", d, a, tr, h, c)
	}
}

func anyContains(l []string, sub string) bool {
	for _, s := range l {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// EXDEV simulé : copie complète + contrôle + suppression de la source ; retour arrière par copie inverse.
func TestMoveExdev(t *testing.T) {
	f := newMvFix(t)
	linked := os.Symlink("a.txt", filepath.Join(f.proj, "lien")) == nil // Windows : liens réservés aux comptes privilégiés
	_ = os.MkdirAll(filepath.Join(f.proj, "sub", "deep"), 0o755)
	_ = os.WriteFile(filepath.Join(f.proj, "sub", "deep", "b.bin"), []byte("0123456789"), 0o600)
	exdev := &os.LinkError{Op: "rename", Err: xdevErr()}
	f.env.Rename = func(a, b string) error {
		if a == f.proj { // seul le rename de la source échoue ; la bascule <dest>.moving → <dest> (même volume) réussit
			return exdev
		}
		return os.Rename(a, b)
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if !r.OK {
		t.Fatalf("échec : %+v", r)
	}
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "deep", "b.bin")); string(b) != "0123456789" {
		t.Errorf("fichier copié = %q", b)
	}
	if linked {
		if l, err := os.Readlink(filepath.Join(dst, "lien")); err != nil || l != "a.txt" {
			t.Errorf("lien symbolique = %q %v", l, err)
		}
	}
	if st, _ := os.Stat(filepath.Join(dst, "sub", "deep", "b.bin")); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("droits = %v", st.Mode().Perm())
	}
	if _, err := os.Stat(f.proj); err == nil {
		t.Error("source non supprimée après la copie")
	}
	if m, _ := filepath.Glob(dst + ".moving-*"); len(m) != 0 {
		t.Errorf("copie temporaire restée : %v", m)
	}
}

func TestMoveExdevEchecApresLaCopieRestaure(t *testing.T) {
	f := newMvFix(t)
	f.env.Rename = func(a, b string) error {
		if a == f.proj {
			return &os.LinkError{Op: "rename", Err: xdevErr()}
		}
		return os.Rename(a, b)
	}
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepTrust && !undo {
			return errors.New("échec trust")
		}
		return nil
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.OK || !r.RolledBack {
		t.Fatalf("résultat = %+v", r)
	}
	if d, a, tr, h, c := f.state(f.proj); !d || !a || !tr || !h || c != "52" {
		t.Errorf("état initial non restauré : %v %v %v %v %q", d, a, tr, h, c)
	}
	if _, err := os.Stat(filepath.Join(f.base, "AUTRE", "beta")); err == nil {
		t.Error("destination restée")
	}
}

func TestMoveRefus(t *testing.T) {
	f := newMvFix(t)
	_ = os.MkdirAll(filepath.Join(f.base, "AUTRE", "existe"), 0o755)
	for name, tgt := range map[string]MoveTarget{
		"identique":         {Base: f.base, Category: "INFRA", Name: "alpha"},
		"existante":         {Base: f.base, Category: "AUTRE", Name: "existe"},
		"hors base":         {Base: "/etc", Category: "", Name: "x"},
		"traversée":         {Base: f.base, Category: "../x", Name: "x"},
		"nom invalide":      {Base: f.base, Category: "AUTRE", Name: "a/b"},
		"catégorie absente": {Base: f.base, Category: "ABSENTE", Name: "x"},
		"dans le projet":    {Base: f.base, Category: "INFRA/alpha", NewCategory: true, Name: "x"},
	} {
		if r := MoveProject(f.env, f.proj, tgt); r.OK || r.Msg == "" {
			t.Errorf("%s : %+v", name, r)
		}
	}
	if d, _, _, _, _ := f.state(f.proj); !d {
		t.Error("projet touché par un refus")
	}
	if len(readMovesCached()) != 0 {
		t.Error("journal écrit pour un refus")
	}
}

func TestMoveBlockers(t *testing.T) {
	f := newMvFix(t)
	f.env.OpenHere = func(string) bool { return true }
	f.env.Foreign = func(string) string { return "autre-hub" }
	f.env.Running = func(string) string { return "ctr sur srv1" }
	bl := MoveBlockers(f.env, f.proj)
	for _, want := range []string{"ouvert dans ce launcher", "autre launcher", "en cours d'exécution"} {
		if !anyContains(bl, want) {
			t.Errorf("blocker %q absent : %v", want, bl)
		}
	}
	if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}); r.OK {
		t.Error("déplacement malgré les blockers")
	}
	info, err := MoveInfoFor(f.env, f.proj)
	if err != nil || info.Base != f.base || info.Category != "INFRA" || info.Name != "alpha" || !info.Open || info.Key != CtrName(f.proj) {
		t.Errorf("info = %+v %v", info, err)
	}
}

// Clé trust.json : l'approbation d'un projet cloné suit le déplacement (état et empreinte conservés).
func TestMoveTrustEntry(t *testing.T) {
	f := newMvFix(t)
	before, _ := EnvTrustEntry(f.proj)
	to := filepath.Join(f.base, "AUTRE", "beta")
	if moved, err := MoveTrustEntry(f.proj, to); !moved || err != nil {
		t.Fatalf("%v %v", moved, err)
	}
	if after, ok := EnvTrustEntry(to); !ok || after != before {
		t.Errorf("entrée = %+v, attendu %+v", after, before)
	}
	if _, ok := EnvTrustEntry(f.proj); ok {
		t.Error("ancienne clé restée")
	}
	if _, err := os.Stat(trustPath() + ".bak"); err != nil {
		t.Error(".bak absent")
	}
	if moved, _ := MoveTrustEntry(filepath.Join(f.base, "rien"), to); moved {
		t.Error("déplacement d'une entrée inexistante")
	}
	// registre illisible : refus, jamais réécrit
	_ = os.WriteFile(trustPath(), []byte("{casse"), 0o600)
	if _, err := MoveTrustEntry(to, f.proj); !errors.Is(err, ErrTrustBroken) {
		t.Errorf("registre illisible : %v", err)
	}
}

// projectMoveInfo avec destination : clé recalculée, système de fichiers, existence, impacts (dont l'approbation).
func TestMoveInfoDestination(t *testing.T) {
	f := newMvFix(t)
	_ = os.MkdirAll(filepath.Join(f.base, "AUTRE", "existe"), 0o755)
	info, err := MoveInfoDest(f.env, f.proj, &MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if err != nil || info.Dest == nil {
		t.Fatalf("%+v %v", info, err)
	}
	d := info.Dest
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if !d.OK || d.Path != dst || d.Key != CtrName(dst) || !d.KeyChanged || d.CrossDevice || d.Exists || d.Field != "" {
		t.Errorf("dest = %+v", d)
	}
	whats := map[string]MoveImpact{}
	for _, im := range d.Impacts {
		whats[im.What] = im
	}
	for _, w := range []string{"dir", "assign", "conf", "history", "trust"} {
		if _, ok := whats[w]; !ok {
			t.Errorf("impact %q absent : %+v", w, d.Impacts)
		}
	}
	if whats["dir"].From != f.proj || whats["dir"].To != dst || whats["assign"].Value != "host" || whats["conf"].Keys[0] != "PROJECT_COLORS" {
		t.Errorf("impacts = %+v", d.Impacts)
	}
	if whats["trust"].Label != "Approbation de l'environnement : conservée" {
		t.Errorf("ligne d'approbation = %q", whats["trust"].Label)
	}
	if _, has := whats["ctr"]; has {
		t.Error("impact ctr pour un projet sans conteneur")
	}
	// même nom (déplacement de catégorie seulement) : pas de ligne conf ; sans entrée d'approbation : pas de ligne trust
	g := newMvFix(t)
	_ = setTrust(g.proj, nil)
	i2, _ := MoveInfoDest(g.env, g.proj, &MoveTarget{Base: g.base, Category: "AUTRE", Name: "alpha"})
	for _, im := range i2.Dest.Impacts {
		if im.What == "conf" || im.What == "trust" {
			t.Errorf("impact %q inattendu : %+v", im.What, i2.Dest.Impacts)
		}
	}
	// refus : existante, identique, traversée
	for name, tc := range map[string]struct {
		t     MoveTarget
		field string
	}{
		"existante": {MoveTarget{Base: f.base, Category: "AUTRE", Name: "existe"}, "name"},
		"identique": {MoveTarget{Base: f.base, Category: "INFRA", Name: "alpha"}, "name"},
		"traversée": {MoveTarget{Base: f.base, Category: "../x", Name: "z"}, "category"},
		"base":      {MoveTarget{Base: "/etc", Name: "z"}, "base"},
	} {
		i, _ := MoveInfoDest(f.env, f.proj, &tc.t)
		if i.Dest.OK || i.Dest.Field != tc.field || i.Dest.Msg == "" {
			t.Errorf("%s : %+v", name, i.Dest)
		}
		if name == "existante" && !i.Dest.Exists {
			t.Error("exists attendu")
		}
	}
	// sans destination : pas de dest
	if i, _ := MoveInfoFor(f.env, f.proj); i.Dest != nil {
		t.Error("dest présent sans destination")
	}
}

// pending au format du contrat ; reprise / abandon par id.
func TestMovePendingEtResumeAbortParID(t *testing.T) {
	f := newMvFix(t)
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepHistory && !undo {
			return errors.New("échec history")
		}
		if s == MoveStepDir && undo {
			return errors.New("retour arrière dir impossible")
		}
		return nil
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.State != "partial" {
		t.Fatalf("%+v", r)
	}
	dst := filepath.Join(f.base, "AUTRE", "beta")
	info, _ := MoveInfoFor(f.env, dst)
	p := info.Pending
	if p == nil || p.ID == "" || p.From != f.proj || p.To != dst || p.FromKey != CtrName(f.proj) || p.ToKey != CtrName(dst) ||
		p.Step != MoveStepAssign || len(p.Done) == 0 || p.At == "" || !strings.Contains(p.Msg, "retour arrière") {
		t.Fatalf("pending = %+v", p)
	}
	// aussi visible depuis la source (le contrat : from OU to)
	if i2, _ := MoveInfoFor(f.env, f.proj); i2.Pending == nil || i2.Pending.ID != p.ID {
		t.Error("pending absent côté source")
	}
	// id inconnu
	if r := ResumeMoveID(f.env, "inconnu"); r.OK || r.Msg != "aucun déplacement inachevé" {
		t.Errorf("id inconnu : %+v", r)
	}
	// autre blocker (conteneur actif) : refusé
	f.env.Running = func(string) string { return "ctr sur srv1" }
	if r := ResumeMoveID(f.env, p.ID); r.OK || !strings.Contains(r.Msg, "en cours d'exécution") {
		t.Errorf("reprise malgré un conteneur actif : %+v", r)
	}
	f.env.Running = func(string) string { return "" }
	// abandon : retour arrière (le dir était la cause du « partial » : il passe maintenant)
	f.env.Fail = nil
	ab := AbortMoveID(f.env, p.ID)
	if !ab.OK || !ab.RolledBack || ab.Path != f.proj || ab.Key != CtrName(f.proj) {
		t.Fatalf("abandon = %+v", ab)
	}
	if d, a, tr, h, c := f.state(f.proj); !d || !a || !tr || !h || c != "52" {
		t.Errorf("état d'origine non rétabli : %v %v %v %v %q", d, a, tr, h, c)
	}
	if len(readMovesCached()) != 0 {
		t.Error("journal non vidé")
	}
	// reprise par id sur une nouvelle entrée partielle
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepHistory && !undo {
			return errors.New("échec history")
		}
		if s == MoveStepDir && undo {
			return errors.New("retour arrière dir impossible")
		}
		return nil
	}
	_ = MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	f.env.Fail = nil
	id := readMovesCached()[0].ID
	rs := ResumeMoveID(f.env, id)
	if !rs.OK || rs.Path != dst {
		t.Fatalf("reprise = %+v", rs)
	}
	if d, a, tr, h, c := f.state(dst); !d || !a || !tr || !h || c != "52" {
		t.Errorf("après reprise : %v %v %v %v %q", d, a, tr, h, c)
	}
}

// Étapes idempotentes : un dossier déjà à destination est un succès.
func TestMoveDirIdempotent(t *testing.T) {
	f := newMvFix(t)
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if err := os.Rename(f.proj, dst); err != nil { // le rename a eu lieu, le journal ne le sait pas
		t.Fatal(err)
	}
	e := &MoveEntry{ID: "x", From: f.proj, To: dst, Detail: map[string]string{}}
	if err := moveDir(f.env, e); err != nil {
		t.Errorf("dir déjà à destination : %v", err)
	}
}

// isCrossDevice reconnaît l'erreur de rename entre volumes DE LA PLATEFORME (Windows : ERROR_NOT_SAME_DEVICE).
func TestIsCrossDevice(t *testing.T) {
	for _, err := range []error{xdevErr(), &os.LinkError{Op: "rename", Old: "a", New: "b", Err: xdevErr()}} {
		if !isCrossDevice(err) {
			t.Errorf("%v non reconnue comme EXDEV de la plateforme", err)
		}
	}
	for _, err := range []error{nil, os.ErrPermission, os.ErrNotExist, errors.New("autre")} {
		if isCrossDevice(err) {
			t.Errorf("%v reconnue à tort", err)
		}
	}
}
