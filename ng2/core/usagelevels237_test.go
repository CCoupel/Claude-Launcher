package core

// Tests de spécification #237 (v6.8.1) — contracts/usage-levels.md : fichier usage-levels.json voisin de usage.json.
// Aucun appel réseau ; tout vit dans un dossier temporaire (CLAUDE_CONFIG_DIR, HOME, XDG_CACHE_HOME). Seul ReadUsage
// (API existante) déclenche l'écriture ici : le chemin FetchUsage nécessite l'endpoint Anthropic (non simulable).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func isolate237(t *testing.T) (usagePath, levelsPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CL_SCRIPT", filepath.Join(home, "claude-launcher.sh"))
	t.Setenv(CacheDirVar, filepath.Join(home, "ngcache"))
	if err := os.MkdirAll(sharedUsageDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return usageCachePath(), filepath.Join(sharedUsageDir(), "usage-levels.json")
}

// usageJSON237 : usage.json au format réel de l'endpoint (microsecondes, +00:00, scope null / modèle sans id).
func usageJSON237(now time.Time) string {
	r := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000000") + "+00:00" }
	return fmt.Sprintf(`{"limits":[
 {"kind":"spend","percent":12,"resets_at":%q,"scope":null},
 {"kind":"weekly_scoped","percent":0,"resets_at":%q,"scope":{"model":{"id":null,"display_name":"Fable"},"surface":null}},
 {"kind":"session","percent":60,"resets_at":%q,"scope":null},
 {"kind":"weekly_all","percent":73.5,"resets_at":%q,"scope":null}
]}`, r(2*time.Hour), r(24*time.Hour), r(3*time.Hour), r(24*time.Hour))
}

func put237(t *testing.T, p, s string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

type level237 struct {
	Kind       string          `json:"kind"`
	Scope      json.RawMessage `json:"scope"`
	ResetsAt   *string         `json:"resets_at"`
	Percent    json.Number     `json:"percent"`
	ExhaustsAt *string         `json:"exhausts_at"`
	Level      *string         `json:"level"`
	Alert      *string         `json:"alert"`
}

type levels237 struct {
	Schema      int        `json:"schema"`
	ComputedAt  string     `json:"computed_at"`
	SourceMtime string     `json:"source_mtime"`
	Windows     []level237 `json:"windows"`
}

func read237(t *testing.T, p string) (levels237, []byte) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("usage-levels.json absent : %v", err)
	}
	var l levels237
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&l); err != nil {
		t.Fatalf("usage-levels.json invalide : %v\n%s", err, b)
	}
	return l, b
}

var rfc3339ms237 = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`)

func TestSchemaConformeAuContrat237(t *testing.T) {
	up, lp := isolate237(t)
	mt := time.Now().Add(-time.Minute).Truncate(time.Millisecond).Add(123 * time.Millisecond)
	put237(t, up, usageJSON237(time.Now()), mt)
	ReadUsage()
	l, raw := read237(t, lp)

	if l.Schema != 1 {
		t.Errorf("schema = %d, attendu 1", l.Schema)
	}
	for name, v := range map[string]string{"computed_at": l.ComputedAt, "source_mtime": l.SourceMtime} {
		if !rfc3339ms237.MatchString(v) {
			t.Errorf("%s = %q : RFC3339 UTC à la milliseconde attendu", name, v)
		}
	}
	if ca, _ := time.Parse(time.RFC3339Nano, l.ComputedAt); time.Since(ca) > time.Minute || time.Until(ca) > time.Minute {
		t.Errorf("computed_at = %s : devrait être l'instant du calcul", l.ComputedAt)
	}
	fi, _ := os.Stat(up)
	if want := fi.ModTime().UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z"); l.SourceMtime != want {
		t.Errorf("source_mtime = %s, attendu la mtime de usage.json à la ms : %s", l.SourceMtime, want)
	}

	// kind « spend » ignoré ; ordre de limits[] conservé (pas le tri d'affichage de ReadUsage)
	var kinds []string
	for _, w := range l.Windows {
		kinds = append(kinds, w.Kind)
	}
	if got := strings.Join(kinds, ","); got != "weekly_scoped,session,weekly_all" {
		t.Fatalf("windows[].kind = %s, attendu weekly_scoped,session,weekly_all (ordre de limits[], quotas seulement)", got)
	}
	scoped, sess, all := l.Windows[0], l.Windows[1], l.Windows[2]

	// scope : recopié tel quel (JSON), null sinon
	var got, want any
	_ = json.Unmarshal(scoped.Scope, &got)
	_ = json.Unmarshal([]byte(`{"model":{"id":null,"display_name":"Fable"},"surface":null}`), &want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("weekly_scoped.scope = %s", scoped.Scope)
	}
	if s := strings.TrimSpace(string(sess.Scope)); s != "null" {
		t.Errorf("session.scope = %q, attendu null", s)
	}
	// resets_at et percent : tels quels
	if sess.ResetsAt == nil || !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}\+00:00$`).MatchString(*sess.ResetsAt) {
		t.Errorf("session.resets_at = %v : microsecondes et +00:00 conservés", sess.ResetsAt)
	}
	if sess.Percent.String() != "60" || all.Percent.String() != "73.5" || scoped.Percent.String() != "0" {
		t.Errorf("percent = %s / %s / %s, attendu 60 / 73.5 / 0 (recopiés)", sess.Percent, all.Percent, scoped.Percent)
	}
	// niveaux : session 60 % à mi-fenêtre+ → rouge + alerte ; weekly_all vert ; Fable 0 % → non fiable (null partout)
	if sess.Level == nil || *sess.Level != "red" || sess.ExhaustsAt == nil || !rfc3339ms237.MatchString(*sess.ExhaustsAt) {
		t.Errorf("session : level=%v exhausts_at=%v", sess.Level, sess.ExhaustsAt)
	}
	if sess.Alert == nil || !strings.HasPrefix(*sess.Alert, "⚠ épuisé ~") {
		t.Errorf("session.alert = %v, attendu « ⚠ épuisé ~… »", sess.Alert)
	}
	if all.Level == nil || *all.Level != "green" || all.Alert != nil {
		t.Errorf("weekly_all : level=%v alert=%v, attendu green / null", all.Level, all.Alert)
	}
	if scoped.Level != nil || scoped.ExhaustsAt != nil || scoped.Alert != nil {
		t.Errorf("weekly_scoped à 0 %% : level/exhausts_at/alert doivent être null (non fiable) : %+v", scoped)
	}
	// clés présentes même à null (le consommateur les lit)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	for _, k := range []string{"schema", "computed_at", "source_mtime", "windows"} {
		if _, ok := generic[k]; !ok {
			t.Errorf("clé %q absente", k)
		}
	}
	for i, w := range generic["windows"].([]any) {
		for _, k := range []string{"kind", "scope", "resets_at", "percent", "exhausts_at", "level", "alert"} {
			if _, ok := w.(map[string]any)[k]; !ok {
				t.Errorf("windows[%d] : clé %q absente (null explicite attendu)", i, k)
			}
		}
	}
}

// Déjà épuisé : exhausts_at = computed_at, alerte sans heure.
func TestDejaEpuise237(t *testing.T) {
	up, lp := isolate237(t)
	r := time.Now().Add(3 * time.Hour).Format(time.RFC3339Nano)
	put237(t, up, fmt.Sprintf(`{"limits":[{"kind":"session","percent":100,"resets_at":%q,"scope":null}]}`, r), time.Now())
	ReadUsage()
	l, _ := read237(t, lp)
	w := l.Windows[0]
	if w.Level == nil || *w.Level != "red" || w.ExhaustsAt == nil || *w.ExhaustsAt != l.ComputedAt {
		t.Errorf("épuisé : level=%v exhausts_at=%v computed_at=%s", w.Level, w.ExhaustsAt, l.ComputedAt)
	}
	if w.Alert == nil || *w.Alert != "⚠ épuisé" {
		t.Errorf("alert = %v, attendu « ⚠ épuisé »", w.Alert)
	}
}

// mtime inchangée : aucune écriture ; usage.json réécrit par un tiers : régénéré.
func TestEcritureSeulementSiSourceChange237(t *testing.T) {
	up, lp := isolate237(t)
	m1 := time.Now().Add(-2 * time.Minute)
	put237(t, up, usageJSON237(time.Now()), m1)
	ReadUsage()
	first, rawFirst := read237(t, lp)

	// la mtime du fichier de niveaux est vieillie : toute réécriture la ferait remonter
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lp, old, old); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	ReadUsage()
	ReadUsage()
	fi, _ := os.Stat(lp)
	if fi.ModTime().Sub(old).Abs() > time.Second {
		t.Errorf("usage.json inchangé : usage-levels.json réécrit (mtime %v)", fi.ModTime())
	}
	if _, raw := read237(t, lp); !bytes.Equal(raw, rawFirst) {
		t.Error("usage.json inchangé : le contenu de usage-levels.json a changé")
	}

	// écrivain tiers (panneau, LinkUsageCache) : nouvelle mtime → régénération
	m2 := time.Now().Add(-30 * time.Second).Truncate(time.Millisecond).Add(7 * time.Millisecond)
	put237(t, up, usageJSON237(time.Now()), m2)
	ReadUsage()
	second, _ := read237(t, lp)
	if second.SourceMtime == first.SourceMtime {
		t.Errorf("usage.json réécrit par un tiers : source_mtime inchangé (%s)", second.SourceMtime)
	}
	if want := m2.UTC().Format("2006-01-02T15:04:05.000Z"); second.SourceMtime != want {
		t.Errorf("source_mtime = %s, attendu %s", second.SourceMtime, want)
	}
}

// usage.json absent, illisible ou sans limits → fichier non écrit (l'ancien reste, périmé par source_mtime).
func TestPasDEcritureSansSource237(t *testing.T) {
	up, lp := isolate237(t)
	ReadUsage() // absent
	if _, err := os.Stat(lp); err == nil {
		t.Error("usage.json absent : usage-levels.json ne doit pas être écrit")
	}
	for name, content := range map[string]string{"invalide": "pas du json {", "sans limits": `{"foo":1}`, "limits vide": `{"limits":[]}`} {
		put237(t, up, content, time.Now())
		ReadUsage()
		if _, err := os.Stat(lp); err == nil {
			t.Errorf("usage.json %s : usage-levels.json ne doit pas être écrit", name)
		}
	}
	// un ancien fichier n'est pas touché par une source inexploitable
	if err := os.WriteFile(lp, []byte(`{"schema":1,"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	put237(t, up, "pas du json {", time.Now())
	ReadUsage()
	if b, _ := os.ReadFile(lp); string(b) != `{"schema":1,"old":true}` {
		t.Errorf("l'ancien fichier a été modifié : %s", b)
	}
}

// Atomicité : écrivains concurrents + lecteur en boucle → jamais de JSON partiel, aucun temporaire restant.
func TestEcritureAtomique237(t *testing.T) {
	up, lp := isolate237(t)
	put237(t, up, usageJSON237(time.Now()), time.Now().Add(-time.Hour))
	ReadUsage() // fichier initial
	stop := make(chan struct{})
	var wg, rd sync.WaitGroup
	var bad sync.Once
	var failure string
	rd.Add(1)
	go func() {
		defer rd.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if b, err := os.ReadFile(lp); err == nil && !json.Valid(b) {
				bad.Do(func() { failure = string(b) })
			}
		}
	}()
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				// chaque tour : mtime nouvelle (la source « change ») puis lecture → régénération
				mt := time.Now().Add(-time.Duration(1+w*100+i) * time.Second)
				_ = os.Chtimes(up, mt, mt)
				ReadUsage()
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	rd.Wait()
	if failure != "" {
		t.Fatalf("lecteur : JSON partiel observé : %.200s", failure)
	}
	if _, raw := read237(t, lp); !json.Valid(raw) {
		t.Fatal("fichier final invalide")
	}
	ents, _ := os.ReadDir(filepath.Dir(lp))
	for _, e := range ents {
		switch n := e.Name(); {
		case n == "usage.json", n == "usage-levels.json", n == "usage.json.err", n == "usage.json.wait":
		default:
			t.Errorf("fichier résiduel dans le cache partagé : %s", n)
		}
	}
}
