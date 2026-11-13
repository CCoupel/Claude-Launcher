package desktop

import (
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

// panelScript : un faux Claude Code avec panneau latéral peint par effacement
// (fond colour235, EL), suivi de la souris (1002 + SGR) et curseur masqué,
// et de l'historique au-dessus de l'écran.
const panelScript = `#!/bin/sh
i=1; while [ $i -le 60 ]; do echo "historique $i"; i=$((i+1)); done
printf '\033[?1002h\033[?1006h\033[?25l\033[2J\033[H'
printf '\033[1;1H\033[1mClaude Code\033[0m v2'
r=1; while [ $r -le 12 ]; do printf '\033[%d;40H\033[48;5;235m\033[K' $r; r=$((r+1)); done
printf '\033[1;41H\033[38;5;220mSession\033[39m 87 %%\033[2;41HAucun milestone\033[0m'
printf '\033[14;1H\033[44m bandeau \033[0m fin\033[5;3Hprêt'
exec cat
`

// seededPane : ouvre un projet (tmux de test) dont le leader est panelScript,
// redemande l'amorçage et rejoue ce flux dans un pane tmux de référence de
// même taille (un autre serveur) : tmux sert d'émulateur de référence pour
// comparer cellule par cellule. Renvoie un appel tmux sur chaque serveur et
// la cible du pane d'origine.
func seededPane(t *testing.T) (orig, ref func(...string) string, leader string) {
	t.Helper()
	h := openPanel(t)
	pt, sock := h.pt, h.sock
	refSock := sock + "-ref"
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", refSock, "kill-server").Run() })
	leader = pt.lastLayout().Leader
	orig, ref = h.tm, func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", refSock}, args...)...).Output()
		return string(out)
	}
	// amorçage redemandé (comme une recapture après redimensionnement)
	seed := h.reseeded(t, leader, func() { pt.resync() })
	size := strings.Fields(orig("display", "-p", "-t", leader, "#{pane_width} #{pane_height}"))
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "seed"), []byte(seed), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "conf"), []byte("set -g status off\n"), 0o644)
	if out, err := exec.Command("tmux", "-L", refSock, "-f", filepath.Join(dir, "conf"), "new-session", "-d",
		"-x", size[0], "-y", size[1], "cat "+filepath.Join(dir, "seed")+"; exec sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("pane de référence : %v %s", err, out)
	}
	time.Sleep(500 * time.Millisecond)
	return orig, ref, leader
}

// panelHost : projet ouvert sur l'hôte (tmux de test) avec panelScript pour leader.
type panelHost struct {
	a          *App
	r          *recorder
	pt         *projTerm
	path, sock string
	tm         func(...string) string
}

func openPanel(t *testing.T) *panelHost {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-seed-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte(panelScript), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	var r recorder
	a := NewApp(r.emit)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(100, 30)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))
	end := time.Now().Add(10 * time.Second)
	for !strings.Contains(r.output(ps[0]), "prêt") && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	h := &panelHost{a: a, r: &r, pt: pt, path: ps[0], sock: sock}
	h.tm = func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return string(out)
	}
	return h
}

// reseeded : flux d'amorçage du pane qui suit l'action do.
func (h *panelHost) reseeded(t *testing.T, pane string, do func()) string {
	t.Helper()
	h.r.mu.Lock()
	n := len(h.r.evs)
	h.r.mu.Unlock()
	do()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		h.r.mu.Lock()
		for _, e := range h.r.evs[n:] {
			if e["_name"] == "term" && e["kind"] == "out" && e["path"] == h.path {
				for _, c := range e["chunks"].([]any) {
					c := c.(map[string]any)
					if c["pane"] == pane && c["reset"] == true {
						h.r.mu.Unlock()
						return c["data"].(string)
					}
				}
			}
		}
		h.r.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pas d'amorçage de %s", pane)
	return ""
}

// #134 : le pane amorcé a exactement les cellules (caractères et couleurs) de
// la grille de tmux, zones peintes par effacement comprises (panneau latéral
// de Claude Code), en écran comme en historique.
func TestSeedMatchesTmuxGrid(t *testing.T) {
	orig, ref, leader := seededPane(t)
	want := orig("capture-pane", "-p", "-e", "-N", "-t", leader)
	got := ref("capture-pane", "-p", "-e", "-N")
	if !strings.Contains(want, "\x1b[48;5;235m") {
		t.Fatalf("panneau absent de l'original :\n%q", want)
	}
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	if len(wl) != len(gl) {
		t.Fatalf("lignes : %d attendu %d", len(gl), len(wl))
	}
	for i := range wl {
		if wl[i] != gl[i] {
			t.Errorf("ligne %d :\n tmux    %q\n amorcé  %q", i, wl[i], gl[i])
		}
	}
	// historique rejoué au-dessus de l'écran (molette sans suivi de la souris)
	if h := ref("capture-pane", "-p", "-S", "-100", "-E", "-1"); !strings.Contains(h, "historique 60") || !strings.Contains(h, "historique 1\n") {
		t.Errorf("historique :\n%s", h)
	}
}

// #135 : les modes demandés par l'application (suivi de la souris en SGR,
// curseur masqué) survivent à l'amorçage : sans eux, xterm.js ne transmet
// plus la souris (redimensionnement du panneau de Claude Code impossible).
func TestSeedKeepsMouseModes(t *testing.T) {
	orig, ref, leader := seededPane(t)
	f := "#{mouse_standard_flag}#{mouse_button_flag}#{mouse_all_flag}#{mouse_sgr_flag} #{cursor_flag}"
	want := strings.TrimSpace(orig("display", "-p", "-t", leader, f))
	if want != "0101 0" {
		t.Fatalf("modes de l'original : %q", want)
	}
	if got := strings.TrimSpace(ref("display", "-p", f)); got != want {
		t.Errorf("modes après amorçage : %q, attendu %q", got, want)
	}
}

func TestSeedDataModes(t *testing.T) {
	m := parseModes("3 4 0 1 1 0 0 1 0 1 1 0 0 0 2 9 20")
	d := seedData(m, "ignoré", "a\x1b[44mb\nc")
	for _, w := range []string{"\x1b[?1049h", "\x1b[?1000h", "\x1b[?1006h", "\x1b[?1h", "\x1b=", "\x1b[?7l", "\x1b[?25l", "\x1b[3;10r", "\x1b[5;4H", "\x1b[0m\r\n\x1b[44mc"} {
		if !strings.Contains(d, w) {
			t.Errorf("%q absent de %q", w, d)
		}
	}
	if strings.Contains(d, "ignoré") {
		t.Error("historique rejoué en écran alternatif")
	}
	// vieux tmux sans ces formats : curseur visible, retour à la ligne
	if d := seedData(parseModes("0 0"), "", "x"); strings.Contains(d, "?25l") || strings.Contains(d, "?7l") {
		t.Errorf("vieux tmux : %q", d)
	}
}

// #135 : une bordure glissée dans l'interface redimensionne le pane dans tmux
// (resize-pane), et le pane redimensionné est recapturé (le terminal de
// l'interface replierait ses lignes autrement que tmux) ; la souris X10
// (onBinary) passe octet pour octet ; l'interface relaie bordures et souris.
func TestPaneBorderDragAndMouse(t *testing.T) {
	h := openPanel(t)
	leader := h.pt.lastLayout().Leader
	h.tm("split-window", "-h", "-d", "-t", leader, "exec cat")
	end := time.Now().Add(10 * time.Second)
	for len(h.pt.lastLayout().Panes) < 2 && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	if len(h.pt.lastLayout().Panes) < 2 {
		t.Fatal("second pane absent de la mise en page")
	}
	w0, _ := strconv.Atoi(strings.TrimSpace(h.tm("display", "-p", "-t", leader, "#{pane_width}")))
	axis := "x"
	if strings.TrimSpace(h.tm("display", "-p", "-t", leader, "#{pane_at_right}")) == "1" {
		axis = "y" // l'un sous l'autre (mise en page du launcher)
	}
	want := 20
	f := "#{pane_width}"
	if axis == "y" {
		f = "#{pane_height}"
	}
	seed := h.reseeded(t, leader, func() {
		if _, err := h.a.Dispatch("resizePane", []byte(`{"path":`+strconv.Quote(h.path)+`,"pane":"`+leader+`","kind":"`+axis+`","cols":20}`)); err != nil {
			t.Fatal(err)
		}
	})
	if got := strings.TrimSpace(h.tm("display", "-p", "-t", leader, f)); got != strconv.Itoa(want) {
		t.Errorf("%s du leader : %s, attendu %d (largeur avant %d)", axis, got, want, w0)
	}
	if !strings.Contains(seed, "\x1b[?1002h") {
		t.Errorf("recapture sans les modes : %q", seed)
	}
	// lecture seule : la bordure ne bouge pas
	h.pt.mu.Lock()
	h.pt.readOnly = true
	h.pt.mu.Unlock()
	h.pt.resizePane(leader, axis, 30)
	time.Sleep(300 * time.Millisecond)
	if got := strings.TrimSpace(h.tm("display", "-p", "-t", leader, f)); got != strconv.Itoa(want) {
		t.Errorf("lecture seule : %s", got)
	}
	if b := binaryBytes("\x1b[M \u00a1\u00ff"); b != "\x1b[M \xa1\xff" {
		t.Errorf("souris X10 : %q", b)
	}
	js := asset(t, "app.js")
	for _, w := range []string{"term.onBinary(", "call('resizePane'", "function paneBorder("} {
		if !strings.Contains(js, w) {
			t.Errorf("%s absent de app.js", w)
		}
	}
}
