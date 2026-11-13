package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Journal (#130) : chaque ligne dit l'étape, sa durée et le chemin lu.
func TestPerfTraceLines(t *testing.T) {
	sandbox(t, "", "alpha")
	var p perfLog
	p.trace("cartes : team", "/projets/alpha/.claude", 250*time.Millisecond)
	p.trace("git (badges)", "/projets/beta", 5*time.Millisecond) // trop court : pas de ligne
	p.trace("cache du hub", "/hub", 3*time.Millisecond, "2 états lus sur 3 projets")
	p.flushTrace()
	b, err := os.ReadFile(filepath.Join(core.CacheDir(), "desktop-perf.log"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "cartes : team") || !strings.Contains(s, "250ms /projets/alpha/.claude") ||
		strings.Contains(s, "/projets/beta") || !strings.Contains(s, "/hub · 2 états lus sur 3 projets") {
		t.Errorf("journal :\n%s", s)
	}
}
