package core

// Tests de spécification #199 (v6.4.0) — contracts/template-version.md.
// Aucun test ne touche le vrai ~ : CL_SCRIPT, CL_CACHE_DIR et XDG_CACHE_HOME pointent vers un dossier temporaire.
// Les champs introduits par le contrat (UpdMsg.Err/RetryAt, TeamHead.TplNewer) sont lus par réflexion : le paquet
// compile avant l'implémentation, les tests échouent (RED) jusqu'à elle.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolate199 : poste factice (script, cache, init-project) et NgVersion de la majeure 6.
func isolate199(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("CL_SCRIPT", filepath.Join(dir, "claude-launcher.sh"))
	t.Setenv(CacheDirVar, filepath.Join(dir, "cache"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg"))
	for _, k := range []string{"CL_FAKE_LATEST_TPL", "CL_FAKE_LATEST_LAUNCHER", "CL_FAKE_TPL_RELEASED", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}
	old := NgVersion
	NgVersion = "v6.4.0"
	t.Cleanup(func() { NgVersion = old })
	return dir
}

func w199(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projet199 : projet à la version tag/commit du template (TEMPLATE_claude/.template-source.json).
func projet199(t *testing.T, tag, commit string) string {
	p := t.TempDir()
	w199(t, filepath.Join(p, "TEMPLATE_claude", ".template-source.json"), fmt.Sprintf(`{"tag":%q,"commit":%q}`, tag, commit))
	return p
}

// adopte199 : version adoptée par le launcher (~/.template-version / ~/.template-commit du poste factice).
func adopte199(t *testing.T, tag, commit string) {
	w199(t, TplVersionPath(), tag)
	w199(t, TplCommitPath(), commit)
}

func field199(t *testing.T, v any, name string) reflect.Value {
	t.Helper()
	f := reflect.ValueOf(v).FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("%T n'a pas de champ %s (contrat template-version.md)", v, name)
	}
	return f
}

// ── Ordre des versions ────────────────────────────────────────────────────

func TestVerParts4Champs199(t *testing.T) {
	for in, want := range map[string][3]int{
		"5.11.1.3": {5, 11, 1}, "v5.11.1.3": {5, 11, 1}, "v6.4.0.12": {6, 4, 0}, "v5.11.1": {5, 11, 1}, "v5.11": {5, 11, 0}, "": {0, 0, 0},
	} {
		// les trois premiers champs comptent ; le 4e (a) peut être conservé (type [3]int ou [4]int)
		got := reflect.ValueOf(verParts(in))
		for i := 0; i < 3; i++ {
			if int(got.Index(i).Int()) != want[i] {
				t.Errorf("verParts(%q) = %v, attendu %v en tête", in, got.Interface(), want)
				break
			}
		}
	}
}

func TestVersionNewer199(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v5.11.0", "v5.11.1", true},
		{"v5.11.1", "v5.11.0", false},
		{"v5.11.1", "v5.11.1", false},
		{"v5.9.9", "v5.11.0", true}, // ordre numérique, pas lexical
		{"v5.11.1.3", "v5.11.1.9", false},
		{"v5.11.1.9", "v5.11.2", true},
		{"v5.11.2.1", "v5.11.1.9", false},
	} {
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("VersionNewer(%q, %q) = %v, attendu %v", c.a, c.b, got, c.want)
		}
	}
}

// ── État du template d'un projet (ReadTemplate) ───────────────────────────

func TestReadTemplate199(t *testing.T) {
	isolate199(t)
	for _, c := range []struct {
		name               string
		projTag, projSha   string
		adoptTag, adoptSha string
		want               TemplateState
	}{
		{"projet v5.11.1, cache v5.11.0 : ni ⚠ ni question", "v5.11.1", "bbb", "v5.11.0", "aaa", TplOK},
		{"projet égal au cache", "v5.11.1", "bbb", "v5.11.1", "bbb", TplOK},
		{"cache plus récent : ⚠", "v5.11.0", "aaa", "v5.11.1", "bbb", TplOutdated},
		{"4e champ ignoré : égal", "v5.11.1.4", "bbb", "v5.11.1", "bbb", TplOK},
		{"tag manquant : repli sur l'égalité des commits (égal)", "", "bbb", "v5.11.1", "bbb", TplOK},
		{"tag manquant : repli sur l'égalité des commits (écart)", "", "aaa", "v5.11.1", "bbb", TplOutdated},
	} {
		t.Run(c.name, func(t *testing.T) {
			isolate199(t)
			adopte199(t, c.adoptTag, c.adoptSha)
			proj := projet199(t, c.projTag, c.projSha)
			if got := ReadTemplate(proj, c.adoptSha); got != c.want {
				t.Errorf("ReadTemplate = %v, attendu %v", got, c.want)
			}
		})
	}
}

func TestReadTemplateSansSource199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	if got := ReadTemplate(t.TempDir(), "bbb"); got != TplNone {
		t.Errorf("projet sans .template-source.json : %v, attendu TplNone", got)
	}
}

// ── Carte TEAM ────────────────────────────────────────────────────────────

func TestCarteTeamVersionDuProjet199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.0", "aaa") // cache plus ancien que le projet
	proj := projet199(t, "v5.11.1", "bbb")
	w199(t, filepath.Join(proj, ".claude", "project-config.json"), `{"agents":{"registry":[
		{"name":"teamleader","tag":"v5.11.1"},{"name":"dev-backend","tag":"v5.11.1"},
		{"name":"qa","tag":"v5.10.0"},{"name":"doc-updater","tag":"v5.12.0"}]}}`)

	info := ReadTplInfo(proj)
	if info.Target != "v5.11.1" {
		t.Fatalf("version cible = %q, attendu la version du projet v5.11.1 (pas la version adoptée v5.11.0)", info.Target)
	}
	lines := info.Apply([]TeamLine{{Name: "leader", Tag: "v5.11.1"}, {Name: "dev-backend", Tag: "v5.11.1"},
		{Name: "qa", Tag: "v5.10.0"}, {Name: "doc-updater", Tag: "v5.12.0"}})
	lines, head := ApplyTeamHead(lines, info.Target)
	if head.Tpl != "v5.11.1" {
		t.Errorf("teamHead.tpl = %q, attendu v5.11.1", head.Tpl)
	}
	want := map[string]string{"leader": TplLineOK, "dev-backend": TplLineOK, "qa": TplLineLate, "doc-updater": TplLineNone}
	for _, l := range lines {
		if l.TplState != want[l.Name] {
			t.Errorf("%s : tplState = %q, attendu %q", l.Name, l.TplState, want[l.Name])
		}
		if l.TplTarget != "v5.11.1" {
			t.Errorf("%s : tplTarget = %q", l.Name, l.TplTarget)
		}
	}
	// aucune ligne « late » pour un projet à v5.11.1 avec un cache v5.11.0 : seul qa (v5.10.0) l'est
	if head.Odd != 1 && head.Odd != 2 { // qa ; doc-updater (plus récent) compte ou non selon l'implémentation
		t.Errorf("odd = %d", head.Odd)
	}
	for _, l := range lines {
		if l.Name == "dev-backend" && len(l.Odd) != 0 {
			t.Errorf("dev-backend ne doit pas être « odd » : %v", l.Odd)
		}
		if l.Name == "qa" && (len(l.Odd) == 0 || l.Odd[0] != OddTpl) {
			t.Errorf("qa doit être odd tpl : %v", l.Odd)
		}
	}
}

func TestTeamHeadTplNewer199(t *testing.T) {
	for _, c := range []struct{ name, adopt, want string }{
		{"dernière publiée plus récente : ⚠", "v5.12.0", "v5.12.0"},
		{"dernière publiée égale : rien", "v5.11.1", ""},
		{"dernière publiée plus ancienne : rien", "v5.11.0", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			isolate199(t)
			adopte199(t, c.adopt, "aaa")
			_, h := ApplyTeamHead([]TeamLine{{Name: "leader", Tag: "v5.11.1"}}, "v5.11.1")
			if h.Tpl != "v5.11.1" || h.TplNewer != c.want {
				t.Errorf("tpl = %q, tplNewer = %q, attendu tplNewer %q", h.Tpl, h.TplNewer, c.want)
			}
		})
	}
}

func TestTeamHeadTplNewerChamp199(t *testing.T) {
	f, ok := reflect.TypeOf(TeamHead{}).FieldByName("TplNewer")
	if !ok {
		t.Fatal("TeamHead.TplNewer manquant (contrat : teamHead.tplNewer)")
	}
	if tag := f.Tag.Get("json"); tag != "tplNewer" {
		t.Errorf("tag json = %q, attendu tplNewer", tag)
	}
	if f.Type.Kind() != reflect.String {
		t.Errorf("TplNewer doit être une chaîne (\"\" = rien de plus récent), type %v", f.Type)
	}
	b, _ := json.Marshal(TeamHead{Tpl: "v5.11.1"})
	if !strings.Contains(string(b), `"tplNewer":""`) {
		t.Errorf("sérialisation sans tplNewer vide : %s", b)
	}
}

// ── Vérification GitHub : faux serveur ────────────────────────────────────

type ghFake struct {
	mu                  sync.Mutex
	hits                int
	auth, inm           []string // en-têtes vus
	tplTag, tplSha      string
	launcherTag         string
	etag                string
	mode                string // "" | "quota" | "quota429"
	reset               time.Time
	notModified, served int
}

func (g *ghFake) start(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.hits++
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		g.inm = append(g.inm, r.Header.Get("If-None-Match"))
		if g.mode != "" {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(g.reset.Unix(), 10))
			if g.mode == "quota429" {
				w.WriteHeader(429)
			} else {
				w.WriteHeader(403)
			}
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/releases/") { // release publiée
			_, _ = w.Write([]byte(`{"assets":[{"name":"SHA256SUMS"}]}`))
			return
		}
		if g.etag != "" {
			w.Header().Set("ETag", g.etag)
			if r.Header.Get("If-None-Match") == g.etag {
				g.notModified++
				w.WriteHeader(304)
				return
			}
		}
		g.served++
		tag, sha := g.tplTag, g.tplSha
		if strings.Contains(r.URL.Path, "Claude-Launcher") {
			tag, sha = g.launcherTag, "ccc"
		}
		_, _ = fmt.Fprintf(w, `[{"name":%q,"commit":{"sha":%q}}]`, tag, sha)
	}))
	t.Cleanup(srv.Close)
	old := ApiBase
	ApiBase = srv.URL
	t.Cleanup(func() { ApiBase = old })
}

func (g *ghFake) count() int { g.mu.Lock(); defer g.mu.Unlock(); return g.hits }

func sharedResult199(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(CacheDir(), "update-check.json"))
	if err != nil {
		t.Fatalf("update-check.json absent : %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("update-check.json invalide : %v", err)
	}
	return m
}

func TestAnnonceSeulementSiPlusRecent199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.0", tplSha: "aaa", launcherTag: "v6.4.0"} // commit différent mais tag plus ancien
	g.start(t)
	if out := CheckUpdatesNow(true); out.TplTag != "" {
		t.Errorf("dernier tag v5.11.0 < adopté v5.11.1 : aucune annonce attendue, TplTag = %q", out.TplTag)
	}
	g.tplTag, g.tplSha = "v5.11.2", "ddd"
	if out := CheckUpdatesNow(true); out.TplTag != "v5.11.2" {
		t.Errorf("dernier tag v5.11.2 > adopté v5.11.1 : TplTag = %q", out.TplTag)
	}
	if got := ReadTrim(TplVersionPath()); got != "v5.11.1" {
		t.Errorf("la version adoptée ne change qu'au clic : %q", got)
	}
}

func TestQuota403AlerteEtPauseGithub199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{mode: "quota", reset: time.Now().Add(30 * time.Minute)}
	g.start(t)
	out := CheckUpdatesNow(true)
	if e := field199(t, out, "Err").String(); e != "quota" {
		t.Errorf("Err = %q, attendu quota", e)
	}
	if field199(t, out, "RetryAt").IsZero() {
		t.Error("RetryAt doit porter l'heure de X-RateLimit-Reset")
	}
	if m := sharedResult199(t); m["error"] != "quota" || m["retry_after"] == "" {
		t.Errorf("update-check.json : %v", m)
	}
	n := g.count()
	for _, manual := range []bool{false, true} { // F5 compris : message immédiat, aucun appel
		out = CheckUpdatesNow(manual)
		if e := field199(t, out, "Err").String(); e != "quota" {
			t.Errorf("manual=%v : Err = %q, attendu quota", manual, e)
		}
	}
	if g.count() != n {
		t.Errorf("%d appel(s) GitHub avant l'heure de reprise, attendu 0", g.count()-n)
	}
	if out.TplTag != "" || out.LauncherReady {
		t.Errorf("aucune annonce pendant le quota : %+v", out)
	}
}

func TestQuota429Reconnu199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{mode: "quota429", reset: time.Now().Add(10 * time.Minute)}
	g.start(t)
	if e := field199(t, CheckUpdatesNow(true), "Err").String(); e != "quota" {
		t.Errorf("429 + X-RateLimit-Remaining: 0 : Err = %q, attendu quota", e)
	}
}

func TestReprisePassee199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	w199(t, filepath.Join(CacheDir(), "update-check.json"),
		fmt.Sprintf(`{"checked_at":%q,"error":"quota","retry_after":%q}`,
			time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)))
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	out := CheckUpdatesNow(true)
	if g.count() == 0 {
		t.Error("heure de reprise passée : GitHub doit être rappelé")
	}
	if e := field199(t, out, "Err").String(); e != "" {
		t.Errorf("Err = %q après la reprise", e)
	}
}

func Test304ConserveLAnnonce199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.0", "aaa")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0", etag: `"abc"`}
	g.start(t)
	first := CheckUpdatesNow(true)
	if first.TplTag != "v5.11.1" {
		t.Fatalf("première vérification : TplTag = %q", first.TplTag)
	}
	second := CheckUpdatesNow(true) // F5 : ignore les 10 min, mais requête conditionnelle
	g.mu.Lock()
	nm, saw := g.notModified, strings.Join(g.inm, "|")
	g.mu.Unlock()
	if nm == 0 || !strings.Contains(saw, `"abc"`) {
		t.Errorf("If-None-Match non envoyé (304 reçus : %d, en-têtes : %q)", nm, saw)
	}
	if second.TplTag != "v5.11.1" || !second.Ok {
		t.Errorf("un 304 réutilise la réponse mémorisée : %+v", second)
	}
	if e := field199(t, second, "Err").String(); e != "" {
		t.Errorf("un 304 n'est pas une erreur : Err = %q", e)
	}
}

func TestResultatPartageMoinsDe10Min199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.0", "aaa")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	CheckUpdatesNow(false)
	m := sharedResult199(t)
	for _, k := range []string{"checked_at", "launcher", "template"} {
		if _, ok := m[k]; !ok {
			t.Errorf("update-check.json sans %q : %v", k, m)
		}
	}
	if tpl, _ := m["template"].(map[string]any); tpl["tag"] != "v5.11.1" {
		t.Errorf("template.tag = %v", tpl["tag"])
	}
	n := g.count()
	out := CheckUpdatesNow(false) // autre instance, < 10 min : pas d'appel
	if g.count() != n {
		t.Errorf("résultat de moins de 10 min : %d appel(s) en trop", g.count()-n)
	}
	if out.TplTag != "v5.11.1" {
		t.Errorf("le résultat partagé doit servir l'annonce : %+v", out)
	}
	CheckUpdatesNow(true) // F5 force l'appel
	if g.count() == n {
		t.Error("F5 doit rappeler GitHub")
	}
	// les écritures sont atomiques : pas de fichier temporaire oublié
	ents, _ := os.ReadDir(CacheDir())
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "update-check") && e.Name() != "update-check.json" {
			t.Errorf("fichier temporaire oublié : %s", e.Name())
		}
	}
}

func TestJetonPriorite199(t *testing.T) {
	for _, c := range []struct {
		name, gh, ghub, conf, want string
	}{
		{"GH_TOKEN prioritaire", "tok-gh", "tok-github", "tok-conf", "tok-gh"},
		{"GITHUB_TOKEN de l'environnement", "", "tok-github", "tok-conf", "tok-github"},
		{"GITHUB_TOKEN de config.conf", "", "", "tok-conf", "tok-conf"},
		{"anonyme sans jeton", "", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := isolate199(t)
			t.Setenv("GH_TOKEN", c.gh)
			t.Setenv("GITHUB_TOKEN", c.ghub)
			t.Setenv("CL_CONFIG_DIR", filepath.Join(dir, "cfg"))
			if c.conf != "" {
				w199(t, ConfigPath(), "GITHUB_TOKEN="+c.conf+"\n")
			}
			g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
			g.start(t)
			CheckUpdatesNow(true)
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.hits == 0 {
				t.Fatal("aucun appel")
			}
			for _, a := range g.auth {
				if c.want == "" && a != "" {
					t.Errorf("appel anonyme attendu, Authorization = %q", a)
				}
				if c.want != "" && !strings.Contains(a, c.want) {
					t.Errorf("Authorization = %q, attendu le jeton %q", a, c.want)
				}
			}
		})
	}
}

func TestJetonJamaisJournalise199(t *testing.T) {
	isolate199(t)
	t.Setenv("GH_TOKEN", "SECRET-199-xyz")
	g := &ghFake{mode: "quota", reset: time.Now().Add(time.Hour)}
	g.start(t)
	CheckUpdatesNow(true)
	ents, _ := os.ReadDir(CacheDir())
	for _, e := range ents {
		if b, err := os.ReadFile(filepath.Join(CacheDir(), e.Name())); err == nil && strings.Contains(string(b), "SECRET-199-xyz") {
			t.Errorf("le jeton figure dans %s", e.Name())
		}
	}
}

func TestInjoignable199(t *testing.T) {
	isolate199(t)
	adopte199(t, "v5.11.1", "bbb")
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // plus personne n'écoute
	old := ApiBase
	ApiBase = url
	defer func() { ApiBase = old }()
	out := CheckUpdatesNow(true)
	if e := field199(t, out, "Err").String(); e != "network" {
		t.Errorf("Err = %q, attendu network", e)
	}
	if out.Ok {
		t.Error("Ok doit rester faux quand GitHub est injoignable")
	}
}

// ── Garde : aucun test n'écrit dans le vrai ~ ─────────────────────────────

func TestGardeSansVraiHome199(t *testing.T) {
	dir := isolate199(t)
	for _, p := range []string{TplVersionPath(), TplCommitPath(), InitProjectPath(), filepath.Join(CacheDir(), "update-check.json")} {
		if !strings.HasPrefix(p, dir) {
			t.Errorf("%s sort du dossier temporaire %s (risque d'écriture dans le vrai ~)", p, dir)
		}
	}
}
