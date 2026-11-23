package core

// Tests de spécification #238 (v6.8.1) — contracts/template-version.md « Vérification GitHub » point 6 (D1 = C) :
// cycle 2 min / 1 min avec jeton, 5 min / 2 min sans ; résultat partagé réutilisé s'il a moins que cycle − 10 s ;
// vérification suivante calée sur CheckedAt ; quota prioritaire. Faux serveur GitHub (ApiBase), aucun appel réel ;
// poste factice (isolate199 : CL_SCRIPT, CL_CACHE_DIR, XDG_CACHE_HOME temporaires, jetons vidés).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolate238(t *testing.T) string {
	t.Helper()
	dir := isolate199(t)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdgconf")) // config.conf factice : aucun jeton lu dans le vrai poste
	t.Setenv("CL_CONFIG_DIR", filepath.Join(dir, "clconf"))
	old := SetInitSynced(true) // aucune synchronisation d'init-project.md (réseau)
	t.Cleanup(func() { SetInitSynced(old) })
	return dir
}

// shared238 : update-check.json d'un poste dont la dernière vérification date de age.
func shared238(t *testing.T, age time.Duration, launcher, tpl string, tplReleased bool) {
	t.Helper()
	at := time.Now().Add(-age).UTC().Format(time.RFC3339)
	w199(t, filepath.Join(CacheDir(), "update-check.json"), fmt.Sprintf(
		`{"checked_at":%q,"launcher":{"tag":%q,"released":true},"template":{"tag":%q,"sha":"ddd","released":%v},"error":"","retry_after":""}`,
		at, launcher, tpl, tplReleased))
}

func TestCycleSelonJeton238(t *testing.T) {
	isolate238(t)
	for _, c := range []struct {
		token   string
		pending bool
		want    time.Duration
	}{
		{"tok", false, 2 * time.Minute}, {"tok", true, time.Minute},
		{"", false, 5 * time.Minute}, {"", true, 2 * time.Minute},
	} {
		t.Setenv("GH_TOKEN", c.token)
		if got := UpdateCheckEvery(c.pending); got != c.want {
			t.Errorf("jeton=%q release attendue=%v : cycle %v, attendu %v", c.token, c.pending, got, c.want)
		}
	}
	// GITHUB_TOKEN (environnement) compte aussi comme jeton
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "tok2")
	if got := UpdateCheckEvery(false); got != 2*time.Minute {
		t.Errorf("GITHUB_TOKEN : cycle %v, attendu 2 min", got)
	}
}

// Le résultat partagé n'est réutilisé que s'il a moins que cycle − 10 s.
func TestReutilisationSelonCycle238(t *testing.T) {
	for _, c := range []struct {
		name   string
		cycle  time.Duration
		age    time.Duration
		reused bool
	}{
		{"cycle 2 min, résultat de 1 min → réutilisé", 2 * time.Minute, time.Minute, true},
		{"cycle 2 min, résultat de 1 min 55 → appel (> cycle − 10 s)", 2 * time.Minute, 115 * time.Second, false},
		{"cycle 2 min, résultat de 3 min → appel (10 min fixes avant)", 2 * time.Minute, 3 * time.Minute, false},
		{"cycle 5 min, résultat de 4 min → réutilisé", 5 * time.Minute, 4 * time.Minute, true},
		{"cycle 5 min, résultat de 5 min → appel", 5 * time.Minute, 5 * time.Minute, false},
		{"cycle 1 min (attendue), résultat de 90 s → appel", time.Minute, 90 * time.Second, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			isolate238(t)
			adopte199(t, "v5.11.1", "bbb")
			g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
			g.start(t)
			t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.1")
			shared238(t, c.age, "v6.4.0", "v5.11.1", true)
			CheckUpdatesEvery(false, c.cycle)
			if reused := g.count() == 0; reused != c.reused {
				t.Errorf("appels GitHub = %d, réutilisation attendue = %v", g.count(), c.reused)
			}
		})
	}
}

// F5 (manuel) contourne toujours le résultat partagé.
func TestManuelContourneLeResultatPartage238(t *testing.T) {
	isolate238(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.1")
	shared238(t, 5*time.Second, "v6.4.0", "v5.11.1", true)
	CheckUpdatesEvery(true, 5*time.Minute)
	if g.count() == 0 {
		t.Error("F5 doit appeler GitHub même avec un résultat de 5 s")
	}
}

// Régression : release du template attendue, cycle accéléré — le passage au prêt est vu au cycle, pas après 10 min.
func TestReleaseAttendueVueAuCycleAccelere238(t *testing.T) {
	isolate238(t)
	t.Setenv("GH_TOKEN", "tok")
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.2", tplSha: "ddd", launcherTag: "v6.4.0"}
	g.start(t)
	shared238(t, 90*time.Second, "v6.4.0", "v5.11.2", false) // tag annoncé, release pas encore prête
	t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.2")              // elle vient d'être publiée
	out := CheckUpdatesEvery(false, UpdateCheckEvery(true))  // 1 min avec jeton
	if out.TplTag != "v5.11.2" || !out.TplReady {
		t.Errorf("TplTag=%q TplReady=%v : la release publiée doit être vue au cycle accéléré (résultat de 90 s)", out.TplTag, out.TplReady)
	}
}

// Une nouvelle version apparaît en ≤ 1 cycle, sans F5.
func TestNouvelleVersionEnUnCycle238(t *testing.T) {
	isolate238(t)
	t.Setenv("GH_TOKEN", "tok")
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.1")
	CheckUpdatesEvery(false, UpdateCheckEvery(false)) // état initial : rien de nouveau
	// un nouveau tag paraît ; le résultat partagé a maintenant plus d'un cycle (2 min avec jeton)
	g.mu.Lock()
	g.launcherTag = "v6.9.0"
	g.mu.Unlock()
	shared238(t, 2*time.Minute+5*time.Second, "v6.4.0", "v5.11.1", true)
	out := CheckUpdatesEvery(false, UpdateCheckEvery(false))
	if out.LauncherTag != "v6.9.0" {
		t.Errorf("LauncherTag = %q, attendu v6.9.0 vu au premier cycle suivant", out.LauncherTag)
	}
}

// CheckedAt : heure du résultat (le nôtre ou le partagé) ; la prochaine vérification s'y cale.
func TestDelaiSuivantCaleSurCheckedAt238(t *testing.T) {
	isolate238(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.1")
	cycle := 5 * time.Minute

	shared238(t, 3*time.Minute, "v6.4.0", "v5.11.1", true)
	out := CheckUpdatesEvery(false, cycle) // réutilisé : CheckedAt = il y a 3 min
	if g.count() != 0 {
		t.Fatalf("résultat de 3 min, cycle 5 min : réutilisation attendue (appels = %d)", g.count())
	}
	if d := time.Since(out.CheckedAt); d < 3*time.Minute-3*time.Second || d > 3*time.Minute+3*time.Second {
		t.Errorf("CheckedAt = il y a %v, attendu ~3 min (heure du résultat partagé)", d)
	}
	if d := NextCheckDelay(out, cycle); d < 2*time.Minute-3*time.Second || d > 2*time.Minute+3*time.Second {
		t.Errorf("délai suivant = %v, attendu CheckedAt + cycle − maintenant ≈ 2 min", d)
	}
	// plancher : un résultat plus vieux que le cycle n'impose pas une boucle serrée
	if d := NextCheckDelay(UpdMsg{CheckedAt: time.Now().Add(-time.Hour)}, cycle); d < 10*time.Second || d > 11*time.Second {
		t.Errorf("résultat trop ancien : délai = %v, attendu le plancher de 10 s", d)
	}
	// vérification fraîche : CheckedAt ≈ maintenant → un cycle plein
	out = CheckUpdatesEvery(true, cycle)
	if d := NextCheckDelay(out, cycle); d < cycle-3*time.Second || d > cycle {
		t.Errorf("après vérification fraîche : délai = %v, attendu ~%v", d, cycle)
	}
	// jamais plus long qu'un cycle, même si CheckedAt est dans le futur (horloges)
	if d := NextCheckDelay(UpdMsg{CheckedAt: time.Now().Add(time.Hour)}, cycle); d > cycle {
		t.Errorf("CheckedAt futur : délai %v > cycle %v", d, cycle)
	}
}

// Quota épuisé : prioritaire sur le cycle.
func TestQuotaPrioritaire238(t *testing.T) {
	isolate238(t)
	retry := time.Now().Add(30 * time.Minute)
	msg := UpdMsg{Err: UpdErrQuota, RetryAt: retry, CheckedAt: time.Now()}
	for _, cycle := range []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute} {
		if d := NextCheckDelay(msg, cycle); d < 29*time.Minute {
			t.Errorf("cycle %v : délai %v, attendu la reprise annoncée (~30 min)", cycle, d)
		}
	}
	// aucun appel avant la reprise, même avec un résultat périmé
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	w199(t, filepath.Join(CacheDir(), "update-check.json"), fmt.Sprintf(
		`{"checked_at":"2020-01-01T00:00:00Z","launcher":{"tag":"v6.4.0"},"template":{},"error":"quota","retry_after":%q}`,
		retry.UTC().Format(time.RFC3339)))
	if out := CheckUpdatesEvery(false, time.Minute); out.Err != UpdErrQuota || g.count() != 0 {
		t.Errorf("Err=%q appels=%d : aucun appel avant X-RateLimit-Reset", out.Err, g.count())
	}
}

// Appels bornés : 3 instances du même poste (CacheDir partagé) ne font pas plus de 2 requêtes (launcher + template)
// par cycle ; avec jeton, la requête de suivi est conditionnelle (If-None-Match, 304).
func TestAppelsBornesParPoste238(t *testing.T) {
	isolate238(t)
	adopte199(t, "v5.11.1", "bbb")
	g := &ghFake{tplTag: "v5.11.1", tplSha: "bbb", launcherTag: "v6.4.0"}
	g.start(t)
	t.Setenv("CL_FAKE_TPL_RELEASED", "v5.11.1")
	for i := 0; i < 3; i++ { // nav, fenêtre, pod : mêmes cycle et résultat partagé
		CheckUpdatesEvery(false, UpdateCheckEvery(false))
	}
	if n := g.count(); n > 2 {
		t.Errorf("3 instances sur un cycle : %d appels GitHub, attendu ≤ 2 (résultat partagé)", n)
	}
	// cycle suivant avec jeton : requêtes conditionnelles
	t.Setenv("GH_TOKEN", "tok")
	g.mu.Lock()
	g.etag = `"e1"`
	g.mu.Unlock()
	CheckUpdatesEvery(true, 2*time.Minute) // mémorise l'ETag
	CheckUpdatesEvery(true, 2*time.Minute) // suivant : If-None-Match → 304
	g.mu.Lock()
	nm := g.notModified
	g.mu.Unlock()
	if nm == 0 {
		t.Error("avec jeton, la vérification suivante doit être conditionnelle (304)")
	}
	_ = os.Remove(filepath.Join(CacheDir(), "update-check.json"))
}
