package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForceCloseable(t *testing.T) {
	other := LiveSess{Owner: OwnerInfo{Id: "autre:1:x", At: time.Now()}, Claude: true}
	cases := []struct {
		name    string
		in      ForceInput
		ok      bool
		whyHas  string
		refused bool
	}{
		{"pod disparu", ForceInput{P: ProjInfo{Open: true, HasEnv: true, Dot: SessHere}, Tab: true, K8s: true}, true, "pod k8s n'existe plus", false},
		{"conteneur disparu", ForceInput{P: ProjInfo{HasEnv: true, Dot: SessAnomaly}, Tab: true}, true, "conteneur n'existe plus", false},
		{"en échec", ForceInput{P: ProjInfo{HasEnv: true, CtrKnown: true, Ctr: Found{Failed: true}, Dot: SessAnomaly}}, true, "en échec", false},
		{"session hôte perdue", ForceInput{P: ProjInfo{Open: true, Dot: SessHere}, Tab: true, HostGone: true}, true, "session tmux locale", false},
		{"session vivante", ForceInput{P: ProjInfo{Open: true, HasEnv: true, HasLive: true, Live: LiveSess{Claude: true, Owner: OwnerInfo{Id: LauncherID(), At: time.Now()}}, Dot: SessHere}, Tab: true}, false, "", false},
		{"affiché fermé", ForceInput{P: ProjInfo{HasEnv: true, Dot: SessNone}}, false, "", false},
		{"autre launcher", ForceInput{P: ProjInfo{HasEnv: true, HasLive: true, Live: other, Dot: SessElse}, Tab: true}, false, "", true},
	}
	for _, c := range cases {
		ok, why, refused := ForceCloseable(c.in)
		if ok != c.ok || !strings.Contains(why, c.whyHas) || (refused != "") != c.refused {
			t.Errorf("%s : ok=%v why=%q refused=%q", c.name, ok, why, refused)
		}
	}
}

func TestReleaseStaleLock(t *testing.T) {
	proj := t.TempDir()
	p := lockPath(proj)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	write := func(s string, age time.Duration) {
		_ = os.WriteFile(p, []byte(s), 0o644)
		old := time.Now().Add(-age)
		_ = os.Chtimes(p, old, old)
	}
	write("autre:1:x 2026-01-01T00:00:00Z\n", time.Second)
	if ReleaseStaleLock(proj) {
		t.Error("verrou frais d'un autre launcher supprimé")
	}
	write("autre:1:x 2026-01-01T00:00:00Z\n", 2*lockStale)
	if !ReleaseStaleLock(proj) {
		t.Error("verrou périmé conservé")
	}
	write(LauncherID()+" 2026-01-01T00:00:00Z\n", time.Second)
	if !ReleaseStaleLock(proj) {
		t.Error("verrou de ce launcher conservé")
	}
}

func TestCleanupProjectKeepsProjectsJSONAndRemovesOrder(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	proj := t.TempDir()
	ord := filepath.Join(t.TempDir(), "x.order")
	_ = os.WriteFile(ord, []byte("%1"), 0o644)
	r := CleanupProject(proj, nil, ord, filepath.Join(proj, "absent"))
	if !r.Marker || r.Orders != 1 {
		t.Errorf("nettoyage : %+v", r)
	}
	if _, err := os.Stat(ord); err == nil {
		t.Error("fichier d'ordre conservé")
	}
}
