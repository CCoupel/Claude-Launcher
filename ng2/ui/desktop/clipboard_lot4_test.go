package desktop

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #155 (LOT-4) — presse-papiers et collage, selon contracts/desktop-dispatch.md
// (34a5445). Faux claude, projets factices, tmux dédié. Les tests écrits avant
// l'implémentation (4.2 / 4.3) sont RED jusque-là.

func dispatchJSON(t *testing.T, a *App, method string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(method, b)
	if err != nil {
		t.Fatalf("%s : %v", method, err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

// Sans Wails (binaire de test) : clipboardWrite et clipboardRead répondent
// « indisponible » avec un message, sans texte ; rien n'est lu sur la machine.
func TestClipboardUnavailableWithoutWails(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	for name, args := range map[string]map[string]any{
		"clipboardWrite": {"text": "secret copié"}, "clipboardRead": {},
	} {
		v := dispatchJSON(t, a, name, args)
		if v["ok"] != false || v["unavailable"] != true || strings.TrimSpace(fmt.Sprint(v["msg"])) == "" || v["msg"] == nil {
			t.Errorf("%s : %v (indisponible attendu, avec un message)", name, v)
		}
		if txt, has := v["text"]; has && txt != "" {
			t.Errorf("%s a renvoyé du texte : %v", name, txt)
		}
	}
}

// Mode --web : même réponse par /api/call ; le presse-papiers de la machine qui
// héberge le serveur n'est jamais lu ni écrit pour un client web.
func TestClipboardWebNeverTouchesHostClipboard(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok155", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	for _, call := range []string{
		`{"method":"clipboardRead","args":{}}`,
		`{"method":"clipboardWrite","args":{"text":"du texte du navigateur"}}`,
	} {
		resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok155", call, "application/json", "")
		if resp.StatusCode != 200 {
			t.Fatalf("%s : %d %s", call, resp.StatusCode, body)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(body), &v)
		if res, ok := v["result"].(map[string]any); ok {
			v = res
		}
		if v["ok"] != false || v["unavailable"] != true {
			t.Errorf("%s → %s : indisponible attendu en --web", call, body)
		}
		if strings.Contains(body, `"text":"`) && !strings.Contains(call, "clipboardWrite") {
			t.Errorf("lecture : du texte renvoyé en --web : %s", body)
		}
	}
	// sans cookie : refusé comme tout appel
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", `{"method":"clipboardRead","args":{}}`, "application/json", ""); resp.StatusCode == 200 {
		t.Error("clipboardRead accepté sans cookie")
	}
}

// ── collage : tmux dédié, faux claude qui écrit tout ce qu'il reçoit ───────

type pasteFix struct {
	a      *App
	pt     *projTerm
	proj   string
	pane   string
	out    string
	sock   string
	sizeOf func() int
}

func newPasteFix(t *testing.T) pasteFix {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	t.Chdir(t.TempDir()) // un serveur tmux qui plante ne dépose jamais de core dans le dépôt (#224)
	sock := "cl-desk-paste-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() }) // exécuté APRÈS a.Stop (ci-dessous)
	dir := t.TempDir()
	out := filepath.Join(dir, "recu.txt")
	fake := filepath.Join(dir, "claude")
	// tty en mode brut : tout octet reçu est écrit tel quel, sans édition de ligne
	_ = os.WriteFile(fake, []byte("#!/bin/sh\nstty raw -echo\necho faux claude prêt\nexec cat > "+out+"\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	var r recorder
	a := newApp(t, r.emit)
	t.Cleanup(a.Stop) // clients tmux détachés et ouvertures terminées avant kill-server (SIGSEGV de tmux 3.6 sinon, #224)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(120, 40)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))
	f := pasteFix{a: a, pt: pt, proj: ps[0], pane: pt.lastLayout().Leader, out: out, sock: sock}
	// attendre que cat soit en place (le script a écrit son marqueur)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(out); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.sizeOf = func() int { b, _ := os.ReadFile(out); return len(b) }
	return f
}

func (f pasteFix) termInput(t *testing.T, data string) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"path": f.proj, "pane": f.pane, "data": data})
	res, err := f.a.Dispatch("termInput", b)
	if err != nil {
		t.Fatalf("termInput : %v", err)
	}
	if res == nil {
		return nil
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func (f pasteFix) waitSize(t *testing.T, n int) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if f.sizeOf() >= n {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := os.ReadFile(f.out)
	return string(b)
}

// Un collage de plusieurs milliers de lignes (accents, emoji, tabulations)
// arrive en entier, dans l'ordre, sans perte ni doublon : le découpage en
// commandes send-keys -H de 256 octets ne coupe ni ne mélange rien.
func TestPasteThousandsOfLinesKeepsOrder(t *testing.T) {
	f := newPasteFix(t)
	var sb strings.Builder
	for i := 1; i <= 6000; i++ {
		fmt.Fprintf(&sb, "ligne %05d é à ü 日本 😀\tfin\n", i)
	}
	want := sb.String()
	if res := f.termInput(t, want); res != nil && res["ok"] == false {
		t.Fatalf("collage refusé : %v", res)
	}
	got := f.waitSize(t, len(want))
	if got != want {
		// diagnostic : première différence
		n := 0
		for n < len(got) && n < len(want) && got[n] == want[n] {
			n++
		}
		t.Fatalf("collage altéré : %d octets reçus sur %d, première différence à l'octet %d", len(got), len(want), n)
	}
}

// Collage de 4 Mo + 1 octet : refusé (« toobig »), rien n'est envoyé au pane.
func TestPasteTooBigRefusedNothingSent(t *testing.T) {
	f := newPasteFix(t)
	res := f.termInput(t, strings.Repeat("x", 4<<20+1))
	if res == nil || res["ok"] != false || res["reason"] != "toobig" || res["msg"] == "" {
		t.Fatalf("collage trop gros : %v", res)
	}
	time.Sleep(500 * time.Millisecond)
	if n := f.sizeOf(); n != 0 {
		t.Errorf("%d octets envoyés malgré le refus", n)
	}
}

// Lecture seule : la saisie est refusée avec « readonly » et un message, plus
// ignorée en silence ; rien n'est envoyé à tmux ; une fois la main revenue le
// même collage passe.
func TestPasteReadOnlyRefusedWithMessage(t *testing.T) {
	f := newPasteFix(t)
	f.pt.mu.Lock()
	f.pt.readOnly = true
	f.pt.mu.Unlock()
	res := f.termInput(t, "collé en lecture seule\n")
	if res == nil || res["ok"] != false || res["reason"] != "readonly" || strings.TrimSpace(fmt.Sprint(res["msg"])) == "" {
		t.Fatalf("lecture seule : %v", res)
	}
	time.Sleep(500 * time.Millisecond)
	if n := f.sizeOf(); n != 0 {
		t.Errorf("%d octets envoyés en lecture seule", n)
	}
	f.pt.mu.Lock()
	f.pt.readOnly = false
	f.pt.mu.Unlock()
	if res := f.termInput(t, "ok\n"); res != nil && res["ok"] == false {
		t.Fatalf("collage refusé hors lecture seule : %v", res)
	}
	if got := f.waitSize(t, 3); got != "ok\n" {
		t.Errorf("reçu %q", got)
	}
}

// Saisie ordinaire (une frappe, Ctrl+C) : retour vide, comme avant — pas de
// faux refus pour les touches de tous les jours.
func TestTermInputOrdinaryKeysReturnNothing(t *testing.T) {
	f := newPasteFix(t)
	for _, k := range []string{"a", "\x03", "\r", "\x1b[A"} {
		if res := f.termInput(t, k); res != nil && len(res) > 0 {
			t.Errorf("touche %q : retour %v", k, res)
		}
	}
}

// ── frontend embarqué : les raccourcis sont branchés ───────────────────────

func TestFrontendClipboardShortcutsPresent(t *testing.T) {
	js := asset(t, "app.js")
	for _, want := range []struct{ name, pattern string }{
		{"appel clipboardWrite", `clipboardWrite`},
		{"appel clipboardRead", `clipboardRead`},
		{"repli navigator.clipboard", `navigator\.clipboard`},
		{"collage entre crochets par xterm", `\.paste\(`},
		{"copie à la sélection", `onSelectionChange|getSelection\(\)`},
		{"clic droit dans un terminal", `['"]contextmenu['"]`},
		{"Ctrl+Maj+C", `shiftKey[^\n]{0,80}['"]?[cC]['"]|['"][cC]['"][^\n]{0,80}shiftKey`},
		{"Ctrl+Maj+V / Ctrl+V", `shiftKey[^\n]{0,80}['"]?[vV]['"]|['"][vV]['"][^\n]{0,80}shiftKey|key\s*===?\s*['"][vV]['"]`},
		{"lecture seule : message de refus", `readonly`},
	} {
		if !regexp.MustCompile(want.pattern).MatchString(js) {
			t.Errorf("%s absent de app.js (motif %q)", want.name, want.pattern)
		}
	}
	// Ctrl+C ne doit jamais être pris pour la copie : aucun cas « ctrlKey sans shiftKey + C » qui retourne false
	if regexp.MustCompile(`ctrlKey\s*&&\s*!ev\.shiftKey\s*&&\s*ev\.key\s*===?\s*['"][cC]['"]`).MatchString(js) {
		t.Error("Ctrl+C seul semble pris par la copie : il doit rester au programme (interruption)")
	}
}

func TestHelpMentionsCopyPaste(t *testing.T) {
	b, err := os.ReadFile("help.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ctrl+Maj+C", "Ctrl+Maj+V"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("aide (F1) : « %s » absent de help.go", want)
		}
	}
}
