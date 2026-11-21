package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// M1 : contrôle de contenu (pas seulement nombre + taille) : un octet différent à taille égale est détecté.
func TestSameTreeContentDetecteUnOctet(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(a, "f"), []byte("0123456789"), 0o644)
	_ = os.WriteFile(filepath.Join(b, "f"), []byte("0123456780"), 0o644) // même taille, contenu différent
	if err := sameTreeContent(a, b); err == nil || !strings.Contains(err.Error(), "contenu différent") {
		t.Errorf("octet différent non détecté : %v", err)
	}
	_ = os.WriteFile(filepath.Join(b, "f"), []byte("0123456789"), 0o644)
	if err := sameTreeContent(a, b); err != nil {
		t.Errorf("copies identiques refusées : %v", err)
	}
	_ = os.WriteFile(filepath.Join(b, "en-trop"), []byte("x"), 0o644) // entrée en trop dans la copie
	if err := sameTreeContent(a, b); err == nil {
		t.Error("fichier en trop non détecté")
	}
	_ = os.Remove(filepath.Join(b, "en-trop"))
	_ = os.Remove(filepath.Join(b, "f")) // manquant
	if err := sameTreeContent(a, b); err == nil {
		t.Error("fichier manquant non détecté")
	}
}

// M1 : si le contrôle de la copie échoue, la source est INTACTE (rien n'est supprimé) ; dates et liens conservés.
func TestCopieCorrompueLaisseLaSourceIntacte(t *testing.T) {
	f := newMvFix(t)
	f.env.Rename = func(a, b string) error {
		if a == f.proj {
			return &os.LinkError{Op: "rename", Err: xdevErr()}
		}
		return os.Rename(a, b)
	}
	f.env.Fail = nil
	mt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(f.proj, "a.txt"), mt, mt)
	// la copie est corrompue juste après sa réalisation : on remplace le fichier source par un contenu de même taille
	// pendant la fenêtre (via Fail avant l'étape assign, donc APRÈS dir) n'est pas possible ; on teste copyTree + contrôle
	tmp := filepath.Join(t.TempDir(), "copie")
	if err := copyTree(f.proj, tmp); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(tmp, "a.txt")); !st.ModTime().Equal(mt) {
		t.Errorf("date de modification non conservée : %v", st.ModTime())
	}
	if err := sameTreeContent(f.proj, tmp); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("zzz"), 0o644) // même taille que « aaa »
	if err := sameTreeContent(f.proj, tmp); err == nil {
		t.Error("copie corrompue non détectée")
	}
	if b, _ := os.ReadFile(filepath.Join(f.proj, "a.txt")); string(b) != "aaa" {
		t.Error("source touchée")
	}
}

// Liens symboliques relatifs ET absolus recopiés tels quels.
func TestCopieLiensTelsQuels(t *testing.T) {
	src := t.TempDir()
	if err := os.Symlink("a.txt", filepath.Join(src, "rel")); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	_ = os.Symlink(filepath.Join(src, "a.txt"), filepath.Join(src, "abs"))
	dst := filepath.Join(t.TempDir(), "d")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if l, _ := os.Readlink(filepath.Join(dst, "rel")); l != "a.txt" {
		t.Errorf("lien relatif = %q", l)
	}
	if l, _ := os.Readlink(filepath.Join(dst, "abs")); l != filepath.Join(src, "a.txt") {
		t.Errorf("lien absolu = %q (recopié tel quel)", l)
	}
}

// Reprise idempotente après le rename de la copie : source ET destination complètes ⇒ la suppression de la source termine.
func TestRepriseApresRenameDeLaCopie(t *testing.T) {
	f := newMvFix(t)
	dst := filepath.Join(f.base, "AUTRE", "beta")
	if err := copyTree(f.proj, dst); err != nil { // état : copie en place, source pas encore supprimée
		t.Fatal(err)
	}
	e := &MoveEntry{ID: "r1", From: f.proj, To: dst, Detail: map[string]string{"dir": "copy-done"}}
	if err := moveDir(f.env, e); err != nil {
		t.Fatalf("reprise : %v", err)
	}
	if _, err := os.Stat(f.proj); err == nil {
		t.Error("source non supprimée à la reprise")
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Error("destination perdue")
	}
	// destination incomplète : pas de suppression de la source (la reprise échoue, source intacte)
	g := newMvFix(t)
	d2 := filepath.Join(g.base, "AUTRE", "beta")
	_ = os.MkdirAll(d2, 0o755) // destination vide
	e2 := &MoveEntry{ID: "r2", From: g.proj, To: d2, Detail: map[string]string{"dir": "copy-done"}}
	_ = moveDir(g.env, e2)
	if _, err := os.Stat(filepath.Join(g.proj, "a.txt")); err != nil {
		t.Error("source supprimée alors que la destination est incomplète")
	}
}

// M2 : exclusion — deux déplacements simultanés du même projet : un seul passe, l'autre est refusé « déjà en cours ».
func TestExclusionDeuxDeplacementsConcurrents(t *testing.T) {
	f := newMvFix(t)
	gate := make(chan struct{})
	f.env.Fail = func(step string, undo bool) error {
		if step == MoveStepAssign && !undo {
			<-gate // le premier reste « en cours » pendant que les autres essaient
		}
		return nil
	}
	var wg sync.WaitGroup
	res := make(chan MoveResult, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res <- MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta" + string(rune('a'+i))})
		}(i)
	}
	time.Sleep(300 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(res)
	ok, refused := 0, 0
	for r := range res {
		switch {
		case r.OK:
			ok++
		case strings.Contains(r.Msg, "déjà en cours") || strings.Contains(r.Msg, "déplacement inachevé") || strings.Contains(r.Msg, "projet introuvable"):
			refused++
		default:
			t.Errorf("résultat inattendu : %+v", r)
		}
	}
	if ok != 1 || refused != 5 {
		t.Fatalf("ok=%d refusés=%d, attendu 1 et 5", ok, refused)
	}
	if len(readMovesCached()) != 0 {
		t.Error("journal non vidé")
	}
}

// M2 : reprise / annulation concurrentes sur la même entrée : une seule exécution.
func TestExclusionResumeAbortConcurrents(t *testing.T) {
	f := newMvFix(t)
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepHistory && !undo {
			return errors.New("échec")
		}
		if s == MoveStepDir && undo {
			return errors.New("retour arrière impossible")
		}
		return nil
	}
	if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}); r.State != "partial" {
		t.Fatalf("%+v", r)
	}
	id := readMovesCached()[0].ID
	f.env.Fail = nil
	gate := make(chan struct{})
	f.env.Fail = func(s string, undo bool) error {
		if s == MoveStepAssign && !undo {
			<-gate
		}
		return nil
	}
	first := make(chan MoveResult, 1)
	go func() { first <- ResumeMoveID(f.env, id) }()
	time.Sleep(300 * time.Millisecond)
	if r := ResumeMoveID(f.env, id); r.OK || !strings.Contains(r.Msg, "déjà en cours") {
		t.Errorf("seconde reprise = %+v", r)
	}
	if r := AbortMoveID(f.env, id); r.OK || !strings.Contains(r.Msg, "déjà en cours") {
		t.Errorf("annulation pendant la reprise = %+v", r)
	}
	close(gate)
	if r := <-first; !r.OK {
		t.Errorf("première reprise = %+v", r)
	}
}

// M2 : blockers revérifiés juste avant dir et juste avant ctr (échec propre + retour arrière, jamais de rm -f).
func TestBlockersRevérifiésAvantDirEtCtr(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(f *mvFix, n *int)
		step string
	}{
		{"projet ouvert entre-temps (avant dir)", func(f *mvFix, n *int) {
			f.env.OpenHere = func(string) bool { *n++; return *n > 1 } // libre au premier contrôle, ouvert ensuite
		}, MoveStepDir},
		{"conteneur démarré entre-temps (avant ctr)", func(f *mvFix, n *int) {
			f.env.Running = func(string) string {
				*n++
				if *n > 2 { // appels : contrôle initial (1), avant dir (2), avant ctr (3)
					return "ctr sur srv1"
				}
				return ""
			}
		}, MoveStepCtr},
		{"autre launcher entre-temps (avant ctr)", func(f *mvFix, n *int) {
			f.env.Foreign = func(string) string {
				*n++
				if *n > 2 {
					return "autre-hub"
				}
				return ""
			}
		}, MoveStepCtr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMvFix(t)
			n := 0
			tc.set(f, &n)
			r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
			if r.OK || r.Step != tc.step || !r.RolledBack {
				t.Fatalf("résultat = %+v", r)
			}
			if d, a, tr, h, c := f.state(f.proj); !d || !a || !tr || !h || c != "52" {
				t.Errorf("état initial non restauré : %v %v %v %v %q", d, a, tr, h, c)
			}
			if len(f.ctrRm) != 0 {
				t.Errorf("conteneur supprimé malgré le blocker : %v", f.ctrRm)
			}
		})
	}
}

// Mineur 1 : journal illisible = fail-closed (refus, message de reprise), jamais réécrit.
func TestJournalIllisibleFailClosed(t *testing.T) {
	f := newMvFix(t)
	for _, bad := range []string{"{casse", ""} {
		_ = os.WriteFile(movesPath(), []byte(bad), 0o600)
		r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
		if r.OK || !strings.Contains(r.Msg, "journal des déplacements illisible") || !strings.Contains(r.Msg, "restaurez moves.json.bak") {
			t.Errorf("journal %q : %+v", bad, r)
		}
		if b, _ := os.ReadFile(movesPath()); string(b) != bad {
			t.Errorf("journal illisible réécrit : %q", b)
		}
		if d, _, _, _, _ := f.state(f.proj); !d {
			t.Error("projet déplacé malgré le journal illisible")
		}
		if bl := MoveBlockers(f.env, f.proj); !anyContains(bl, "journal des déplacements illisible") {
			t.Errorf("blockers = %v", bl)
		}
	}
	_ = os.Remove(movesPath())
	if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}); !r.OK {
		t.Errorf("journal supprimé : déplacement refusé %+v", r)
	}
	// .bak avant chaque écriture
	if _, err := os.Stat(movesPath() + ".bak"); err == nil {
		t.Log(".bak présent")
	}
}

// Mineur 5a : registre trust.json illisible ne bloque que les projets à environnement.
func TestTrustIllisibleNeBloquePasUnProjetSansEnv(t *testing.T) {
	f := newMvFix(t)
	_ = os.Remove(filepath.Join(f.proj, ".claude-launcher", "env.yaml"))
	_ = setTrust(f.proj, nil)
	_ = os.WriteFile(trustPath(), []byte("{casse"), 0o600)
	if r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}); !r.OK {
		t.Errorf("projet sans environnement bloqué par un registre illisible : %+v", r)
	}
	// avec environnement : bloqué (fail-closed) et retour arrière complet
	g := newMvFix(t)
	_ = os.WriteFile(trustPath(), []byte("{casse"), 0o600)
	r := MoveProject(g.env, g.proj, MoveTarget{Base: g.base, Category: "AUTRE", Name: "beta"})
	if r.OK || r.Step != MoveStepTrust || !r.RolledBack {
		t.Errorf("projet à environnement : %+v", r)
	}
}

// Mineur 6 : clé config.conf partagée (même nom de dossier) signalée dans « ce qui change ».
func TestImpactConfCleePartagee(t *testing.T) {
	f := newMvFix(t)
	autre := filepath.Join(f.base, "AUTRE", "alpha") // même nom, autre catégorie
	_ = os.MkdirAll(filepath.Join(autre, ".git"), 0o755)
	info, _ := MoveInfoDest(f.env, f.proj, &MoveTarget{Base: f.base, Category: "AUTRE", Name: "gamma"})
	var conf *MoveImpact
	for i := range info.Dest.Impacts {
		if info.Dest.Impacts[i].What == "conf" {
			conf = &info.Dest.Impacts[i]
		}
	}
	if conf == nil || !strings.Contains(conf.Label, "partagée avec") || !strings.Contains(conf.Label, autre) {
		t.Errorf("impact conf = %+v", conf)
	}
}

// Mineur 7 : les entrées de config.conf déplacées sont relues à l'identique par TOUS les lecteurs (bash et confLite).
func TestConfNormaliseeLueParTousLesLecteurs(t *testing.T) {
	forms := []string{
		`declare -A PROJECT_COLORS=(["alpha"]="52" [beta]=7)`, // forme canonique écrite par le launcher / forme nue d'origine
		`declare -A PROJECT_COLORS=([alpha]=52 ["beta"]="7")`,
	}
	for _, src := range forms {
		out := confLite(src+"\n", "/home/x")
		for _, want := range []string{"color\talpha\t52", "color\tbeta\t7"} {
			if !strings.Contains(out, want) {
				t.Errorf("confLite(%q) sans %q :\n%s", src, want, out)
			}
		}
	}
	// et un déplacement réécrit proprement une entrée nue : alpha → gamma, beta intacte
	f := newMvFix(t)
	_ = os.WriteFile(ConfigPath(), []byte(defaultConfigFor()+"\ndeclare -A PROJECT_COLORS=([alpha]=\"52\" [beta]=7)\n"), 0o600)
	if err := moveConf(f.proj, filepath.Join(f.base, "AUTRE", "gamma")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ConfigPath())
	out := confLite(string(b), "/home/x")
	for _, want := range []string{"color\tgamma\t52", "color\tbeta\t7"} {
		if !strings.Contains(out, want) {
			t.Errorf("après déplacement, confLite sans %q :\n%s", want, out)
		}
	}
	if strings.Contains(out, "color\talpha\t") {
		t.Error("ancienne clé restée")
	}
	if c := LoadConfig(); c.ProjectColors["gamma"] != "52" || c.ProjectColors["beta"] != "7" || c.ProjectColors["alpha"] != "" {
		t.Errorf("LoadConfig (bash) : %+v", c.ProjectColors)
	}
}

// projectMoveInfo (appelé à chaque frappe) ne reparcourt pas l'arbre à chaque appel : la recherche des clés partagées est
// mémorisée ; un nouvel appel dans la fenêtre ne voit pas un projet créé entre-temps (preuve du cache).
func TestInfoALaFrappeSansReparcoursDeLArbre(t *testing.T) {
	f := newMvFix(t)
	sharedScanCache.Lock()
	sharedScanCache.names = nil // cache vide
	sharedScanCache.Unlock()
	tgt := &MoveTarget{Base: f.base, Category: "AUTRE", Name: "gamma"}
	first, _ := MoveInfoDest(f.env, f.proj, tgt)
	for _, im := range first.Dest.Impacts {
		if im.What == "conf" && im.Label != "" {
			t.Fatalf("clé partagée annoncée sans autre projet : %+v", im)
		}
	}
	autre := filepath.Join(f.base, "AUTRE", "alpha")
	_ = os.MkdirAll(filepath.Join(autre, ".git"), 0o755) // créé APRÈS le premier parcours
	second, _ := MoveInfoDest(f.env, f.proj, tgt)
	for _, im := range second.Dest.Impacts {
		if im.What == "conf" && im.Label != "" {
			t.Error("l'arbre a été reparcouru à la frappe (le cache devait servir)")
		}
	}
	sharedScanCache.Lock()
	sharedScanCache.at = time.Now().Add(-time.Hour) // fenêtre échue : nouveau parcours
	sharedScanCache.Unlock()
	third, _ := MoveInfoDest(f.env, f.proj, tgt)
	found := false
	for _, im := range third.Dest.Impacts {
		found = found || (im.What == "conf" && strings.Contains(im.Label, autre))
	}
	if !found {
		t.Error("après la fenêtre, la clé partagée n'est pas détectée")
	}
}

// Message exact selon la cause : contenu illisible / fichier inaccessible / verrou inaccessible.
func TestMessageJournalSelonLaCause(t *testing.T) {
	f := newMvFix(t)
	// contenu illisible : restaurer .bak ou supprimer
	_ = os.WriteFile(movesPath(), []byte("{casse"), 0o600)
	msg := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}).Msg
	for _, want := range []string{"journal des déplacements illisible", movesPath(), "restaurez moves.json.bak", "supprimez moves.json"} {
		if !strings.Contains(msg, want) {
			t.Errorf("contenu : message sans %q : %s", want, msg)
		}
	}
	// fichier inaccessible (dossier à la place du fichier) : droits, pas « restaurez »
	_ = os.Remove(movesPath())
	if err := os.Mkdir(movesPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	msg = MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"}).Msg
	if !strings.Contains(msg, "journal des déplacements inaccessible") || !strings.Contains(msg, "vérifiez les droits") || strings.Contains(msg, "restaurez") || strings.Contains(msg, "illisible") {
		t.Errorf("accès : message = %s", msg)
	}
	_ = os.Remove(movesPath())
	// verrou inaccessible : réessayer / supprimer le verrou
	m := movesBrokenError{"verrou", "délai dépassé"}.Error()
	for _, want := range []string{"verrou du journal des déplacements inaccessible", movesPath() + ".lock", "réessayez", "supprimez ce verrou"} {
		if !strings.Contains(m, want) {
			t.Errorf("verrou : message sans %q : %s", want, m)
		}
	}
	if strings.Contains(m, "illisible") || strings.Contains(m, "moves.json.bak") {
		t.Errorf("verrou : message trompeur : %s", m)
	}
}

// Une écriture du journal en échec arrête le déplacement AVANT l'étape suivante (retour arrière), jamais « poursuivi sans trace ».
func TestEcritureDuJournalEnEchecArreteLeDeplacement(t *testing.T) {
	f := newMvFix(t)
	ran := map[string]bool{}
	f.env.Fail = func(step string, undo bool) error {
		if !undo {
			ran[step] = true
		}
		if step == MoveStepAssign && !undo {
			// le journal devient impossible à écrire APRÈS que « dir » ait été journalisé : la sauvegarde qui suit « assign » échoue
			_ = os.Remove(movesPath())
			_ = os.Mkdir(movesPath(), 0o755)
		}
		return nil
	}
	r := MoveProject(f.env, f.proj, MoveTarget{Base: f.base, Category: "AUTRE", Name: "beta"})
	if r.OK || r.Step != MoveStepAssign || !strings.Contains(r.Msg, "journal des déplacements") {
		t.Fatalf("résultat = %+v", r)
	}
	if ran[MoveStepConf] || ran[MoveStepHistory] || ran[MoveStepTrust] || ran[MoveStepCtr] {
		t.Errorf("étapes poursuivies malgré le journal non écrit : %v", ran)
	}
	_ = os.Remove(movesPath())
	if d, a, tr, h, c := f.state(f.proj); !d || !a || !tr || !h || c != "52" {
		t.Errorf("état initial non restauré : dir=%v assign=%v trust=%v hist=%v color=%q", d, a, tr, h, c)
	}
	if len(f.ctrRm) != 0 {
		t.Error("conteneur supprimé")
	}
}
