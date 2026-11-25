package desktop

// #214 (critical) : projectBases, projectCategories, categoryCreate, newInfo étendu et `create` contraint
// (base + catégorie + nom) — fenêtre native (Dispatch) et --web (/api/call), appels forgés compris.
// Bases factices : sandbox() pose CL_ROOTS = <tmp>/PROJETS (projets dans PROJETS/TEST/<nom>).

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func base214(t *testing.T) string {
	t.Helper()
	return filepath.Dir(filepath.Dir(sandbox(t, "", "alpha")[0])) // <tmp>/PROJETS
}

func snapDir214(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error { out = append(out, p); return nil })
	return out
}

// voisins214 : entrées à côté de la base qui ne sont pas des dossiers du bac à sable (écrits en tâche de fond par
// l'application : config, cache d'état… — jamais comptés). Tout le reste serait une écriture hors de la base.
// stableSnap214 : instantané de la base une fois l'arbre STABLE (#270). L'ouverture d'un projet écrit en arrière-plan, APRÈS l'état
// « ready », des fichiers (.claude/commands/init-project.md…) ; sous charge ils arrivent pendant le sous-test suivant et seraient pris
// pour des créations hors du projet attendu. Stable = quatre lectures identiques espacées de 200 ms, au plus 10 s. Le contrôle de
// confinement (rien créé hors du projet attendu et de ses catégories) est inchangé.
func stableSnap214(t *testing.T, root string) []string {
	t.Helper()
	prev := snapDir214(t, root)
	stable := 0
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		cur := snapDir214(t, root)
		if strings.Join(cur, "\n") == strings.Join(prev, "\n") {
			if stable++; stable >= 4 {
				return cur
			}
		} else {
			stable = 0
		}
		prev = cur
	}
	t.Logf("arbre toujours en mouvement après 10 s : instantané pris tel quel")
	return prev
}

func voisins214(t *testing.T, base string) []string {
	t.Helper()
	app := map[string]bool{"home": true, "xdg_config_home": true, "xdg_cache_home": true, "cl_config_dir": true,
		"claude_config_dir": true, "appdata": true, "localappdata": true, "userprofile": true, filepath.Base(base): true}
	var out []string
	ents, _ := os.ReadDir(filepath.Dir(base))
	for _, e := range ents {
		if !app[e.Name()] {
			out = append(out, e.Name())
		}
	}
	return out
}

func refus214(t *testing.T, v map[string]any, field string) {
	t.Helper()
	if v["ok"] != false {
		t.Fatalf("attendu un refus {ok:false, field:%q}, reçu %v", field, v)
	}
	if v["field"] != field {
		t.Errorf("champ = %v, attendu %q (%v)", v["field"], field, v["msg"])
	}
	if m, _ := v["msg"].(string); strings.TrimSpace(m) == "" {
		t.Errorf("message vide : %v", v)
	}
}

func TestProjectBases214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "projectBases", map[string]any{})
	bs, _ := v["bases"].([]any)
	if len(bs) != 1 {
		t.Fatalf("bases = %v", v)
	}
	b0 := bs[0].(map[string]any)
	if b0["path"] != base || b0["missing"] != false {
		t.Errorf("base = %v, attendu {path:%s, missing:false}", b0, base)
	}
	if v["default"] != base {
		t.Errorf("default = %v, attendu %s (seule base accessible)", v["default"], base)
	}
}

func TestProjectBasesPlusieursEtManquante214(t *testing.T) {
	base := base214(t)
	autre := filepath.Join(t.TempDir(), "AUTRE")
	_ = os.MkdirAll(autre, 0o755)
	perdue := filepath.Join(t.TempDir(), "perdue")
	t.Setenv("CL_ROOTS", strings.Join([]string{base, autre, perdue}, string(os.PathListSeparator)))
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "projectBases", map[string]any{})
	bs, _ := v["bases"].([]any)
	if len(bs) != 3 {
		t.Fatalf("bases = %v", v)
	}
	for i, want := range []struct {
		p       string
		missing bool
	}{{base, false}, {autre, false}, {perdue, true}} { // ordre de config.conf conservé
		b := bs[i].(map[string]any)
		if b["path"] != want.p || b["missing"] != want.missing {
			t.Errorf("base %d = %v, attendu %v", i, b, want)
		}
	}
	// plusieurs bases accessibles : la base du projet sélectionné (alpha, sous la première)
	if v["default"] != base {
		t.Errorf("default = %v, attendu %s (base du projet sélectionné)", v["default"], base)
	}
}

func TestProjectCategoriesEtCategoryCreate214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	// « TEST » existe (contient le projet alpha) : c'est une catégorie à 1 projet
	v := dispatchJSON(t, a, "projectCategories", map[string]any{"base": base})
	cats, _ := v["categories"].([]any)
	found := false
	for _, c := range cats {
		m := c.(map[string]any)
		if m["path"] == "TEST" {
			found = true
			if n, _ := m["projects"].(float64); n != 1 {
				t.Errorf("TEST : %v projets, attendu 1", m["projects"])
			}
		}
		if m["path"] == "TEST/alpha" {
			t.Error("un projet est listé comme catégorie")
		}
	}
	if !found {
		t.Errorf("TEST absent : %v", v)
	}
	// création : nouvelle, puis doublon = fusion
	c := dispatchJSON(t, a, "categoryCreate", map[string]any{"base": base, "category": "INFRA/K8S"})
	if c["ok"] != true || c["path"] != "INFRA/K8S" || c["existed"] != false {
		t.Errorf("categoryCreate = %v", c)
	}
	if st, err := os.Stat(filepath.Join(base, "INFRA", "K8S")); err != nil || !st.IsDir() {
		t.Errorf("dossier non créé : %v", err)
	}
	c = dispatchJSON(t, a, "categoryCreate", map[string]any{"base": base, "category": "INFRA/K8S"})
	if c["ok"] != true || c["existed"] != true {
		t.Errorf("doublon = %v, attendu ok et existed:true (fusion)", c)
	}
	// une catégorie vide est listée avec 0 projet
	v = dispatchJSON(t, a, "projectCategories", map[string]any{"base": base})
	vide := false
	for _, x := range v["categories"].([]any) {
		if m := x.(map[string]any); m["path"] == "INFRA/K8S" && m["projects"] == float64(0) {
			vide = true
		}
	}
	if !vide {
		t.Errorf("catégorie vide non listée : %v", v)
	}
}

func TestRefusDeChemin214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	avant := snapDir214(t, base)
	dehors := t.TempDir()
	_ = os.Symlink(dehors, filepath.Join(base, "SORTIE"))
	avant = snapDir214(t, base)

	for _, c := range []struct {
		method string
		args   map[string]any
		field  string
	}{
		{"projectCategories", map[string]any{"base": "/etc"}, "base"},
		{"projectCategories", map[string]any{"base": ""}, "base"},
		{"projectCategories", map[string]any{"base": filepath.Join(base, "TEST")}, "base"}, // ancien appel : une catégorie n'est pas une base
		{"categoryCreate", map[string]any{"base": "/etc", "category": "x"}, "base"},
		{"categoryCreate", map[string]any{"base": base, "category": "../evil"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": "/abs"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": `a\b`}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": "SORTIE/x"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": ""}, "category"},
		// create forgé : base hors des bases, ancienne forme, traversées, absolu, lien sortant
		{"create", map[string]any{"base": "/tmp", "name": "x"}, "base"},
		{"create", map[string]any{"base": filepath.Join(base, "TEST"), "name": "x"}, "base"},
		{"create", map[string]any{"base": base, "category": "../..", "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "/etc", "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": `..\x`, "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "SORTIE", "newCategory": true, "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "../x"}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "a/b"}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": ""}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "alpha"}, "name"},        // existe déjà
		{"create", map[string]any{"base": base, "category": "INEXISTANTE", "name": "x"}, "category"}, // sans newCategory
	} {
		refus214(t, dispatchJSON(t, a, c.method, c.args), c.field)
	}
	if apres := snapDir214(t, base); len(apres) != len(avant) {
		t.Errorf("écriture sur le disque malgré les refus :\navant=%v\napres=%v", avant, apres)
	}
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors des bases : %v", es)
	}
	if extra := voisins214(t, base); len(extra) != 0 {
		t.Errorf("entrées créées à côté de la base : %v", extra)
	}
}

// Mode --web : un appel forgé hors base est refusé comme dans la fenêtre native, et n'écrit rien.
func TestRefusDeCheminWeb214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok214", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	avant := snapDir214(t, base)
	forges := []string{
		`{"method":"create","args":{"base":"/tmp","name":"evil"}}`,
		`{"method":"create","args":{"base":` + quote214(base) + `,"category":"../../..","name":"evil"}}`,
		`{"method":"create","args":{"base":` + quote214(base) + `,"category":"TEST","name":"../../evil"}}`,
		`{"method":"categoryCreate","args":{"base":` + quote214(base) + `,"category":"../evil"}}`,
		`{"method":"projectCategories","args":{"base":"/"}}`,
	}
	for _, call := range forges {
		resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok214", call, "application/json", "")
		if resp.StatusCode != 200 {
			t.Fatalf("%s : %d %s", call, resp.StatusCode, body)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(body), &v)
		if res, ok := v["result"].(map[string]any); ok {
			v = res
		}
		if v["ok"] != false || v["field"] == nil {
			t.Errorf("%s → %s : refus {ok:false, field} attendu", call, body)
		}
	}
	// sans session : refusé comme tout appel
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", forges[0], "application/json", ""); resp.StatusCode == 200 {
		t.Error("create accepté sans cookie")
	}
	if apres := snapDir214(t, base); len(apres) != len(avant) {
		t.Errorf("écriture sur le disque malgré les refus : %v", apres)
	}
	// les nouvelles méthodes passent par /api/call avec session
	_, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok214", `{"method":"projectBases","args":{}}`, "application/json", "")
	if !strings.Contains(body, `"bases"`) || !strings.Contains(body, `"default"`) {
		t.Errorf("projectBases en --web : %s", body)
	}
}

func quote214(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestNewInfoBasesEtRoots214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "newInfo", map[string]any{"path": ""})
	bs, _ := v["bases"].([]any)
	if len(bs) != 1 || bs[0].(map[string]any)["path"] != base {
		t.Errorf("newInfo.bases = %v", v["bases"])
	}
	if v["default"] != base {
		t.Errorf("newInfo.default = %v, attendu %s", v["default"], base)
	}
	roots, _ := v["roots"].([]any) // alias conservé (retiré en v7)
	if len(roots) != 1 || roots[0] != base {
		t.Errorf("newInfo.roots = %v, attendu [%s]", v["roots"], base)
	}
	// jamais de valeur secrète dans newInfo
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "GITHUB_TOKEN") {
		t.Error("newInfo expose un jeton")
	}
}

// ── succès de create : chemin final, rien d'autre créé ───────────────────────

// fakeClaudeSession214 : faux claude + tmux dédié isolé (comme les tests de collage), pour que l'ouverture du projet créé
// par create ne lance rien de réel.
func fakeClaudeSession214(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	// cwd = dossier du test : un serveur tmux qui plante ne dépose jamais de core dans le dépôt
	t.Chdir(t.TempDir())
	sock := uniqueDeskSocket("cl-desk-create214-")
	t.Setenv("CL_DESK_SOCKET", sock)
	// dernier enregistré, premier exécuté : l'App est arrêtée (clients tmux détachés, ouvertures terminées) AVANT
	// kill-server — un kill-server pendant un new-session / attach -C en cours faisait planter tmux 3.6 (SIGSEGV)
	t.Cleanup(func() { killDeskServer(sock) })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nexec sleep 300\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
}

func TestCreateSuccesChemins214(t *testing.T) {
	base := base214(t)
	fakeClaudeSession214(t)
	var r recorder
	a := newApp(t, r.emit)
	cleanupApp(t, a)
	for _, c := range []struct {
		name string
		args map[string]any
		rel  string   // chemin attendu sous la base
		made []string // dossiers créés en plus du projet (catégories)
	}{
		{"catégorie existante", map[string]any{"base": base, "category": "TEST", "name": "n1", "raw": true}, "TEST/n1", nil},
		{"nouvelle catégorie", map[string]any{"base": base, "category": "NEW/SUB", "newCategory": true, "name": "n2", "raw": true}, "NEW/SUB/n2", []string{"NEW", "NEW/SUB"}},
		{"racine de la base", map[string]any{"base": base, "name": "n3", "raw": true}, "n3", nil},
		{"blancs de bord", map[string]any{"base": base, "category": "TEST", "name": "  n4  ", "raw": true}, "TEST/n4", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			avant := map[string]bool{}
			for _, p := range stableSnap214(t, base) { // le projet du sous-test précédent a fini d'écrire en arrière-plan (#270)
				avant[p] = true
			}
			v := dispatchJSON(t, a, "create", c.args)
			if v["ok"] == false || v["tab"] == nil {
				t.Fatalf("create = %v, attendu un succès (tab)", v)
			}
			dir := filepath.Join(base, filepath.FromSlash(c.rel))
			if v["tab"] != dir {
				t.Errorf("tab = %v, attendu %s", v["tab"], dir)
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				t.Fatalf("dossier %s non créé : %v", dir, err)
			}
			// « Projet brut » : dossier + git init ; ni CLAUDE.md minimal, ni marqueur /init-project ; `raw` reçu est ignoré
			if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
				t.Errorf("git init absent : %v", err)
			}
			for _, absent := range []string{"CLAUDE.md", filepath.Join(".claude-launcher", "init-proposed")} {
				if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
					t.Errorf("%s créé pour un projet brut", absent)
				}
			}
			if out, _ := exec.Command("git", "-C", dir, "remote").Output(); strings.TrimSpace(string(out)) != "" {
				t.Errorf("remote %q créé sans URL fournie", out)
			}
			allowed := map[string]bool{dir: true}
			for _, m := range c.made {
				allowed[filepath.Join(base, filepath.FromSlash(m))] = true
			}
			if extra := voisins214(t, base); len(extra) != 0 {
				t.Errorf("entrées créées à côté de la base : %v", extra)
			}
			for _, p := range snapDir214(t, base) {
				if avant[p] || allowed[p] {
					continue
				}
				if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
					t.Errorf("créé en dehors du projet attendu : %s", p)
				}
			}
			// l'ouverture (faux claude) se termine avant la fin du test : plus d'écriture dans le bac à sable
			r.wait(t, 20*time.Second, "ready", isTerm(dir, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
			// le projet apparaît dans l'arbre, sous la bonne base
			found := false
			for _, p := range a.projectPaths() {
				if p == dir {
					found = true
				}
			}
			if !found {
				t.Errorf("projet absent de l'arbre : %v", a.projectPaths())
			}
		})
	}
}

// « Projet brut » avec remote : git init + origin = l'URL, rien d'autre (jamais de /init-project, de CLAUDE.md, ni de
// clonage) ; un remote invalide est refusé sans rien créer.
func TestCreateProjetBrutAvecEtSansRemote214(t *testing.T) {
	base := base214(t)
	fakeClaudeSession214(t)
	var r recorder
	a := newApp(t, r.emit)
	cleanupApp(t, a)
	dir := filepath.Join(base, "TEST", "brut-remote")
	v := dispatchJSON(t, a, "create", map[string]any{"base": base, "category": "TEST", "name": "brut-remote", "source": "empty",
		"remote": "git@github.com:moi/depot-test.git"})
	if v["tab"] != dir {
		t.Fatalf("create = %v", v)
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil || strings.TrimSpace(string(out)) != "git@github.com:moi/depot-test.git" {
		t.Errorf("origin = %q, %v", out, err)
	}
	for _, absent := range []string{"CLAUDE.md", filepath.Join(".claude-launcher", "init-proposed")} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s créé pour un projet brut", absent)
		}
	}
	r.wait(t, 20*time.Second, "ready", isTerm(dir, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	for _, e := range r.all() {
		if e["_name"] == "initProposal" {
			t.Errorf("proposition /init-project pour un projet brut : %v", e)
		}
	}
}

// Remote d'un projet brut qui ressemble à une option de git (`-u`, `--upload-pack=x`) : jamais interprété comme option ;
// l'échec est lisible et RIEN n'est laissé (dossier du projet, catégorie créée) ni écrit dans la config.
func TestCreateProjetBrutRemoteEnOption214(t *testing.T) {
	base := base214(t)
	fakeClaudeSession214(t)
	var r recorder
	a := newApp(t, r.emit)
	cleanupApp(t, a)
	for _, remote := range []string{"-u", "--upload-pack=touch /tmp/PREUVE-REMOTE-214"} {
		avant := snapDir214(t, base)
		b, _ := json.Marshal(map[string]any{"base": base, "category": "NEUVE", "newCategory": true, "name": "p", "source": "empty", "remote": remote})
		res, err := a.Dispatch("create", b)
		if err == nil {
			if m, _ := res.(map[string]any); m["tab"] != nil {
				t.Errorf("remote %q accepté : %v", remote, res)
			}
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("remote %q : erreur sans message", remote)
		}
		if apres := snapDir214(t, base); len(apres) != len(avant) {
			t.Errorf("remote %q : reste sur le disque : %v", remote, apres)
		}
		if _, err := os.Stat("/tmp/PREUVE-REMOTE-214"); err == nil {
			_ = os.Remove("/tmp/PREUVE-REMOTE-214")
			t.Errorf("remote %q : commande exécutée", remote)
		}
	}
}

var deskSockSeq atomic.Int64

// uniqueDeskSocket : un socket tmux PAR TEST (et non par processus) : un test ne se connecte jamais au serveur du test
// précédent, encore en train de s'arrêter après son kill-server (seul le premier ouvrait une session sur un socket neuf).
func uniqueDeskSocket(prefix string) string {
	return prefix + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(deskSockSeq.Add(1), 10)
}

// killDeskServer : kill-server puis attente que plus aucun serveur ne réponde sur ce socket (au plus 3 s).
func killDeskServer(sock string) {
	_ = exec.Command("tmux", "-L", sock, "kill-server").Run()
	for i := 0; i < 100; i++ {
		if exec.Command("tmux", "-L", sock, "list-sessions").Run() != nil {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// cleanupApp : arrêt propre d'une App de test qui a pu OUVRIR des projets (tmux). Enregistré après les autres nettoyages,
// donc exécuté avant eux : d'abord on attend la fin de toute ouverture, puis Stop.
//
// Pourquoi attendre ici et pas seulement dans Stop : a.open() lance l'ouverture dans une goroutine qui ne s'inscrit dans
// openGate qu'en début d'exécution (projterm.go run) — juste après open(), le compteur peut encore être à 0 (mesuré : 2 cas sur
// 300). Stop ne voit alors aucune ouverture en cours, le test finit, t.Setenv restaure l'environnement, et l'ouverture
// retardataire lit CL_DESK_SOCKET du test SUIVANT : elle crée « p-<clé> » sur son socket, et l'ouverture de ce test échoue
// « duplicate session » (CI : TestEnvTrustAnswerAcceptIgnoreCancel215, « ouverture après ignorer »).
func cleanupApp(t *testing.T, a *App) {
	t.Helper()
	t.Cleanup(a.Stop)
	t.Cleanup(func() {
		end := time.Now().Add(20 * time.Second)
		for time.Now().Before(end) {
			busy := false
			a.mu.Lock()
			for _, tt := range a.terms {
				if tt.info().State == "opening" {
					busy = true
				}
			}
			a.mu.Unlock()
			a.openGate.mu.Lock()
			if a.openGate.n > 0 {
				busy = true
			}
			a.openGate.mu.Unlock()
			if !busy {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}
