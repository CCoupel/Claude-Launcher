package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #158 (LOT-2) — « Rafraîchir » et « Forcer la fermeture » d'un projet.
// Contrat : contracts/desktop-dispatch.md, section « v6.2.0 — LOT-3 : rafraîchissement d'un projet
// et fermeture forcée (#158) » (intitulée ainsi dans le contrat ; c'est le LOT-2 du plan).
// Tests isolés : projets factices, faux claude, tmux isolé (TMUX_TMPDIR dédié), jamais de vrai projet.

var refreshStepIDs = []string{"ctr", "session", "git", "github", "tpl", "usage"}

func rawPath(p string) json.RawMessage { return json.RawMessage(`{"path":` + strconv.Quote(p) + `}`) }

// refreshEvents : tous les évènements « refresh » du projet, dans l'ordre.
func (r *recorder) refreshEvents(path string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, e := range r.evs {
		if e["_name"] == "refresh" && e["path"] == path {
			out = append(out, e)
		}
	}
	return out
}

func waitRefreshDone(t *testing.T, r *recorder, path string, d time.Duration) map[string]any {
	t.Helper()
	return r.wait(t, d, "refresh done:true", func(e map[string]any) bool {
		return e["_name"] == "refresh" && e["path"] == path && e["done"] == true
	})
}

func stepStates(t *testing.T, e map[string]any) map[string]string {
	t.Helper()
	m := map[string]string{}
	var order []string
	raw, _ := e["steps"].([]any)
	for _, s := range raw {
		sm := s.(map[string]any)
		id, _ := sm["id"].(string)
		st, _ := sm["state"].(string)
		switch st {
		case "wait", "run", "ok", "err":
		default:
			t.Errorf("étape %s : état %q hors wait|run|ok|err", id, st)
		}
		m[id] = st
		order = append(order, id)
	}
	if len(order) > 0 && strings.Join(order, ",") != strings.Join(refreshStepIDs, ",") {
		t.Errorf("étapes %v, attendu %v (dans cet ordre)", order, refreshStepIDs)
	}
	return m
}

// SMOKE — refreshProject rend {ok:true} aussitôt, émet des évènements « refresh » d'étapes
// ctr, session, git, github, tpl, usage et se termine TOUJOURS par done:true, toutes étapes
// résolues (ok|err), sans « refreshing » laissé dans l'instantané.
func TestRefreshProjectAlwaysEndsWithDone(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	res, err := a.Dispatch("refreshProject", rawPath(ps[0]))
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := res.(map[string]any); m == nil || m["ok"] != true {
		t.Fatalf("refreshProject : %v, attendu {ok:true}", res)
	}
	last := waitRefreshDone(t, &r, ps[0], 35*time.Second)
	for id, st := range stepStates(t, last) {
		if st == "wait" || st == "run" {
			t.Errorf("étape %s laissée à %q dans le dernier évènement", id, st)
		}
	}
	if len(last["steps"].([]any)) != len(refreshStepIDs) {
		t.Errorf("dernier évènement : %v, attendu les %d étapes", last["steps"], len(refreshStepIDs))
	}
	if _, ok := last["msg"]; !ok {
		t.Error("champ msg absent du dernier évènement")
	}
	// done:true n'apparaît qu'une fois, en dernier
	evs := r.refreshEvents(ps[0])
	for i, e := range evs {
		if e["done"] == true && i != len(evs)-1 {
			t.Errorf("évènement refresh après done:true (%d/%d)", i+1, len(evs))
		}
	}
	// l'état « en cours » ne reste pas dans l'instantané
	b, _ := json.Marshal(a.Snapshot())
	var snap struct {
		Rows []struct {
			Path       string `json:"path"`
			Refreshing bool   `json:"refreshing"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(b, &snap)
	for _, row := range snap.Rows {
		if row.Path == ps[0] && row.Refreshing {
			t.Error("refreshing:true encore présent après done:true")
		}
	}
}

// Projet inconnu : même règle que les autres appels (jamais d'écriture ni d'évènement).
func TestRefreshProjectUnknownPath(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	outside := t.TempDir()
	if _, err := a.Dispatch("refreshProject", rawPath(outside)); err == nil || !strings.Contains(err.Error(), "projet inconnu") {
		t.Errorf("projet hors arbre : %v, attendu l'erreur « projet inconnu »", err)
	}
	for _, name := range []string{"forceClose", "forceCloseConfirm"} {
		if _, err := a.Dispatch(name, rawPath(outside)); err == nil || !strings.Contains(err.Error(), "projet inconnu") {
			t.Errorf("%s hors arbre : %v, attendu l'erreur « projet inconnu »", name, err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(r.refreshEvents(outside)); n != 0 {
		t.Errorf("%d évènement(s) refresh pour un projet inconnu", n)
	}
}

// SMOKE — un git qui ne répond pas (stockage lent) ne bloque jamais : done:true quand même
// (délai global 30 s), et un second refreshProject pendant l'opération est refusé
// ({ok:false, msg}) — un seul rafraîchissement par projet à la fois.
func TestRefreshProjectSlowGitStillDoneAndSingleFlight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux git en shell : hors Windows")
	}
	if testing.Short() {
		t.Skip("≈ 5 à 30 s")
	}
	ps := sandbox(t, "", "alpha")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 120\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var r recorder
	a := newApp(t, r.emit)
	t0 := time.Now()
	first, err := a.Dispatch("refreshProject", rawPath(ps[0]))
	if err != nil || first.(map[string]any)["ok"] != true {
		t.Fatalf("premier refreshProject : %v %v", first, err)
	}
	second, err := a.Dispatch("refreshProject", rawPath(ps[0]))
	if err != nil {
		t.Fatal(err)
	}
	if m := second.(map[string]any); m["ok"] != false || m["msg"] == "" || m["msg"] == nil {
		t.Errorf("second refreshProject pendant l'opération : %v, attendu {ok:false, msg}", second)
	}
	last := waitRefreshDone(t, &r, ps[0], 40*time.Second)
	if d := time.Since(t0); d > 35*time.Second {
		t.Errorf("done après %v : le délai global est de 30 s", d.Round(time.Second))
	}
	for id, st := range stepStates(t, last) {
		if st == "wait" || st == "run" {
			t.Errorf("étape %s laissée à %q après done", id, st)
		}
	}
	// l'opération terminée, un nouveau rafraîchissement est accepté
	if res, _ := a.Dispatch("refreshProject", rawPath(ps[0])); res.(map[string]any)["ok"] != true {
		t.Errorf("refreshProject refusé après la fin du précédent : %v", res)
	}
	waitRefreshDone(t, &r, ps[0], 40*time.Second)
}

// ── Intégration : projet factice, faux claude, tmux isolé ────────────────

type fakeProj struct {
	a    *App
	r    *recorder
	path string
	sock string
}

// tmux : commande tmux sur le serveur isolé du test.
func (f fakeProj) tmux(args ...string) string {
	out, _ := exec.Command("tmux", append([]string{"-L", f.sock}, args...)...).Output()
	return strings.TrimSpace(string(out))
}

// openFakeProject : projet factice ouvert dans un onglet (session tmux hôte `p-<nom>`).
func openFakeProject(t *testing.T) fakeProj {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil { // court : limite des sockets unix ; jamais /tmp/tmux-<uid>
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Setenv("TMUX", "")
	sock := "cl-desk-r158-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() }) // exécuté avant la suppression du dossier
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	f := fakeProj{r: &recorder{}, path: ps[0], sock: sock}
	f.a = newApp(t, f.r.emit)
	if res, err := f.a.open(ps[0], core.LaunchNormal); err != nil || fmt.Sprint(res) != fmt.Sprint(map[string]any{"tab": ps[0]}) {
		t.Fatalf("open : %v %v", res, err)
	}
	f.a.term(ps[0]).resize(120, 40)
	f.r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	if f.tmux("has-session", "-t", "="+hostSession(ps[0])) != "" || !f.sessionAlive() {
		t.Fatalf("session %s absente après l'ouverture", hostSession(ps[0]))
	}
	return f
}

func (f fakeProj) sessionAlive() bool {
	return exec.Command("tmux", "-L", f.sock, "has-session", "-t", "="+hostSession(f.path)).Run() == nil
}

func (f fakeProj) hasTab() bool {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	_, ok := f.a.terms[f.path]
	return ok
}

func (f fakeProj) waitTabGone(t *testing.T, d time.Duration) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if !f.hasTab() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("onglet de %s encore présent après %v", f.path, d)
}

// Projet dont la session tmux a été tuée de l'extérieur : « Rafraîchir » le relit
// et retire son onglet ; il apparaît fermé (plus Open), ouvrable à nouveau.
func TestRefreshProjectRemovesTabOfKilledSession(t *testing.T) {
	f := openFakeProject(t)
	f.tmux("kill-server") // session disparue : onglet encore affiché
	res, err := f.a.Dispatch("refreshProject", rawPath(f.path))
	if err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("refreshProject : %v %v", res, err)
	}
	waitRefreshDone(t, f.r, f.path, 35*time.Second)
	f.waitTabGone(t, 5*time.Second)
	for _, row := range f.a.Snapshot().Rows {
		if row.Path == f.path && row.Open {
			t.Error("la ligne du projet reste « ouverte » après le rafraîchissement")
		}
	}
	// rouvrable
	if res, err := f.a.open(f.path, core.LaunchNormal); err != nil || fmt.Sprint(res) != fmt.Sprint(map[string]any{"tab": f.path}) {
		t.Errorf("réouverture : %v %v", res, err)
	}
}

// snapDir : hash de chaque fichier du projet (chemin relatif), hors fichiers exclus.
func snapDir(t *testing.T, dir string, skip ...string) map[string]string {
	t.Helper()
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[filepath.ToSlash(s)] = true
	}
	m := map[string]string{}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if skipSet[rel] || strings.HasPrefix(rel, ".git/") {
			return nil
		}
		b, _ := os.ReadFile(p)
		s := sha256.Sum256(b)
		m[rel] = hex.EncodeToString(s[:])
		return nil
	})
	return m
}

func fileHash(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "absent"
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func assignFile() string { return filepath.Join(core.Ng2ConfigDir(), "projects.json") }

// CRITICAL — fermeture forcée d'un projet dont la session a disparu : question avec
// `why` et `items` (onglet…) sans rien changer avant confirmation ; la confirmation retire
// l'onglet, abandonne la reprise (marque de sortie propre), supprime le verrou de création
// périmé ; elle ne supprime AUCUN fichier du projet et ne touche pas à projects.json ;
// le projet est ensuite ouvrable.
func TestForceCloseGoneSessionThenReopen(t *testing.T) {
	f := openFakeProject(t)
	lock := filepath.Join(f.path, ".claude-launcher", ".lock")
	_ = os.MkdirAll(filepath.Dir(lock), 0o755)
	_ = os.WriteFile(lock, nil, 0o644)
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(lock, old, old) // verrou périmé
	before := snapDir(t, f.path, ".claude-launcher/.lock")
	assignBefore := fileHash(assignFile())
	if assignBefore == "absent" {
		t.Fatal("projects.json absent (affectation du projet factice)")
	}

	f.tmux("kill-server") // la session disparaît ; l'onglet reste affiché
	res, err := f.a.Dispatch("forceClose", rawPath(f.path))
	if err != nil {
		t.Fatal(err)
	}
	q, _ := res.(map[string]any)
	if q == nil || q["question"] == nil || q["question"] == "" {
		t.Fatalf("forceClose : %v, attendu {question, why, items} (projet affiché ouvert, session disparue)", res)
	}
	if why, _ := q["why"].(string); why == "" {
		t.Errorf("why absent : %v", q)
	}
	items, _ := q["items"].([]any)
	if len(items) == 0 || !strings.Contains(strings.ToLower(fmt.Sprint(items)), "onglet") {
		t.Errorf("items = %v, attendu la liste de ce qui sera nettoyé (dont l'onglet)", q["items"])
	}
	// la question ne change rien
	if !f.hasTab() {
		t.Error("onglet retiré dès la question, avant la confirmation")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("verrou supprimé avant la confirmation")
	}

	res, err = f.a.Dispatch("forceCloseConfirm", rawPath(f.path))
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := res.(map[string]any); m == nil || m["ok"] != true {
		t.Fatalf("forceCloseConfirm : %v, attendu {ok:true, msg}", res)
	}
	f.waitTabGone(t, 10*time.Second)
	if _, err := os.Stat(lock); err == nil {
		t.Error("verrou de création périmé non supprimé")
	}
	if _, err := os.Stat(core.CleanExitMarker(f.path)); err != nil {
		t.Errorf("marque de sortie propre non posée (la reprise ↺ doit être abandonnée) : %v", err)
	}
	if f.sessionAlive() {
		t.Error("session tmux du projet encore vivante")
	}
	// CRITICAL : rien n'est supprimé ni modifié dans le projet, projects.json intact
	after := snapDir(t, f.path, ".claude-launcher/.lock")
	for p, h := range before {
		if a, ok := after[p]; !ok {
			t.Errorf("fichier du projet supprimé par la fermeture forcée : %s", p)
		} else if a != h {
			t.Errorf("fichier du projet modifié par la fermeture forcée : %s", p)
		}
	}
	if h := fileHash(assignFile()); h != assignBefore {
		t.Error("projects.json modifié par la fermeture forcée (il ne porte aucun état d'ouverture)")
	}
	// le projet est de nouveau ouvrable
	if res, err := f.a.open(f.path, core.LaunchNormal); err != nil || fmt.Sprint(res) != fmt.Sprint(map[string]any{"tab": f.path}) {
		t.Fatalf("réouverture après fermeture forcée : %v %v", res, err)
	}
	f.r.wait(t, 20*time.Second, "ready (réouverture)", func(e map[string]any) bool {
		return e["_name"] == "term" && e["path"] == f.path && e["kind"] == "status" && e["state"] == "ready" && f.sessionAlive()
	})
}

// CRITICAL — jamais sur un projet piloté par un autre launcher vivant : refus (« prendre la main
// d'abord »), aucune question, la confirmation (même forcée à la main) n'efface rien : ni verrou,
// ni marque de sortie, ni fichier du projet, ni projects.json.
func TestForceCloseRefusedWhenOtherLauncherAlive(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	_ = core.WriteAssignment(proj, "host")
	var r recorder
	a := newApp(t, r.emit)
	lock := filepath.Join(proj, ".claude-launcher", ".lock")
	_ = os.MkdirAll(filepath.Dir(lock), 0o755)
	_ = os.WriteFile(lock, nil, 0o644)
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(lock, old, old)
	before := snapDir(t, proj)
	assignBefore := fileHash(assignFile())

	// un autre launcher, vivant, pilote la session du projet
	a.mu.Lock()
	if a.ctr.Live == nil {
		a.ctr.Live = map[string]core.LiveSess{}
	}
	a.ctr.Live[proj] = core.LiveSess{Owner: core.OwnerInfo{Id: "autre-hub:1:x", At: time.Now()}, Panes: 2, Claude: true}
	a.mu.Unlock()

	res, err := a.Dispatch("forceClose", rawPath(proj))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m == nil || m["question"] != nil {
		t.Fatalf("forceClose sur un projet piloté ailleurs : %v, attendu un refus sans question", res)
	}
	if msg, _ := m["msg"].(string); !strings.Contains(msg, "prendre la main") {
		t.Errorf("msg = %q, attendu « … prendre la main d'abord »", msg)
	}
	res, _ = a.Dispatch("forceCloseConfirm", rawPath(proj))
	if cm, _ := res.(map[string]any); cm != nil && cm["ok"] == true {
		t.Errorf("forceCloseConfirm accepté pour un projet piloté ailleurs : %v", res)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("verrou supprimé alors qu'un autre launcher pilote le projet")
	}
	if _, err := os.Stat(core.CleanExitMarker(proj)); err == nil {
		t.Error("marque de sortie propre posée alors qu'un autre launcher pilote le projet")
	}
	if got := snapDir(t, proj); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Errorf("projet modifié par un refus : avant %v, après %v", before, got)
	}
	if h := fileHash(assignFile()); h != assignBefore {
		t.Error("projects.json modifié")
	}
}

// Rien à forcer : projet fermé et sain → {msg} (sans objet), aucune question ; la confirmation sans
// question en attente est refusée.
func TestForceCloseNothingToDo(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	res, err := a.Dispatch("forceClose", rawPath(ps[0]))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m == nil || m["question"] != nil || m["msg"] == nil || m["msg"] == "" {
		t.Errorf("forceClose sur un projet fermé : %v, attendu {msg} sans question", res)
	}
	res, err = a.Dispatch("forceCloseConfirm", rawPath(ps[0]))
	if cm, _ := res.(map[string]any); err == nil && (cm == nil || cm["ok"] != false) {
		t.Errorf("forceCloseConfirm sans question en attente : %v, attendu {ok:false} ou une erreur", res)
	}
}
