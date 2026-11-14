package desktop

import (
	"encoding/json"
	"errors"
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

// Presse-papiers factice : le presse-papiers réel du poste n'est jamais touché.
func fakeClipboard(t *testing.T) *string {
	t.Helper()
	var content string
	oldSet, oldGet := clipboardSetHook, clipboardGetHook
	clipboardSetHook = func(s string) error { content = s; return nil }
	clipboardGetHook = func() (string, error) { return content, nil }
	t.Cleanup(func() { clipboardSetHook, clipboardGetHook = oldSet, oldGet })
	return &content
}

func dispatchMap(t *testing.T, a *App, m string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(m, b)
	if err != nil {
		t.Fatalf("%s : %v", m, err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func TestClipboardWriteReadAndLimits(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	clip := fakeClipboard(t)
	if w := dispatchMap(t, a, "clipboardWrite", map[string]any{"text": "bonjour é\nligne 2"}); w["ok"] != true || *clip != "bonjour é\nligne 2" {
		t.Fatalf("écriture : %v %q", w, *clip)
	}
	if rd := dispatchMap(t, a, "clipboardRead", nil); rd["ok"] != true || rd["text"] != "bonjour é\nligne 2" {
		t.Fatalf("lecture : %v", rd)
	}
	// texte vide : permis (presse-papiers sans texte → chaîne vide)
	*clip = ""
	if rd := dispatchMap(t, a, "clipboardRead", nil); rd["ok"] != true || rd["text"] != "" {
		t.Fatalf("lecture vide : %v", rd)
	}
	// limites : 8 Mo à l'écriture, 4 Mo à la lecture (taille d'un collage)
	if w := dispatchMap(t, a, "clipboardWrite", map[string]any{"text": strings.Repeat("x", maxClipboardWrite+1)}); w["ok"] != false || w["unavailable"] != nil || w["msg"] == nil {
		t.Errorf("écriture trop grande acceptée : %v", w["ok"])
	}
	*clip = strings.Repeat("y", maxPaste+1)
	if rd := dispatchMap(t, a, "clipboardRead", nil); rd["ok"] != false || rd["text"] != nil || !strings.Contains(fmt.Sprint(rd["msg"]), "trop volumineux") {
		t.Errorf("lecture trop grande acceptée : %v", rd["ok"])
	}
	// erreurs du système : rendues, pas une erreur d'appel
	clipboardSetHook = func(string) error { return errors.New("presse-papiers verrouillé") }
	clipboardGetHook = func() (string, error) { return "", errors.New("presse-papiers verrouillé") }
	for _, m := range []string{"clipboardWrite", "clipboardRead"} {
		if v := dispatchMap(t, a, m, map[string]any{"text": "x"}); v["ok"] != false || v["unavailable"] != nil || !strings.Contains(fmt.Sprint(v["msg"]), "verrouillé") {
			t.Errorf("%s : %v", m, v)
		}
	}
}

// « indisponible » : sans crochets (binaire sans Wails) et en --web — où le
// presse-papiers de la machine hôte n'est jamais lu ni écrit, même si un crochet existe.
func TestClipboardUnavailableAndNeverTheHostClipboardInWebMode(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	oldSet, oldGet := clipboardSetHook, clipboardGetHook
	t.Cleanup(func() { clipboardSetHook, clipboardGetHook = oldSet, oldGet })
	clipboardSetHook, clipboardGetHook = nil, nil
	for _, m := range []string{"clipboardWrite", "clipboardRead"} {
		if v := dispatchMap(t, a, m, map[string]any{"text": "x"}); v["ok"] != false || v["unavailable"] != true || v["msg"] == nil {
			t.Errorf("%s sans crochet : %v", m, v)
		}
	}
	// --web : des crochets existent (poste avec presse-papiers) mais ne sont jamais appelés
	called := 0
	clipboardSetHook = func(string) error { called++; return nil }
	clipboardGetHook = func() (string, error) { called++; return "secret du poste", nil }
	a.web = true
	for _, m := range []string{"clipboardWrite", "clipboardRead"} {
		v := dispatchMap(t, a, m, map[string]any{"text": "x"})
		if v["ok"] != false || v["unavailable"] != true || strings.Contains(fmt.Sprint(v), "secret du poste") {
			t.Errorf("--web %s : %v", m, v)
		}
	}
	if called != 0 {
		t.Fatalf("le presse-papiers de la machine hôte a été touché %d fois en mode web", called)
	}
}

func TestRunWebMarksTheAppAsWeb(t *testing.T) {
	// RunWeb pose a.web avant tout : vérifié sur la source (le serveur réel est testé ailleurs)
	b, err := os.ReadFile("web.go")
	if err != nil || !strings.Contains(string(b), "a.web = true") {
		t.Fatalf("RunWeb ne marque pas l'app comme servie à un navigateur : %v", err)
	}
}

// Découpage d'un collage de plusieurs milliers de lignes : commandes de 256 octets
// au plus, ordre et contenu conservés, caractères multi-octets compris.
func TestPasteChunkingKeepsOrderAndBytes(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&b, "ligne %05d — é€ — %s\n", i, strings.Repeat("x", i%40))
	}
	data := []byte(b.String())
	chunks := hexKeys(data)
	var back []byte
	for i, c := range chunks {
		if len(c) == 0 || len(c) > 256 {
			t.Fatalf("morceau %d de %d octets", i, len(c))
		}
		for _, h := range c {
			n, err := strconv.ParseUint(h, 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			back = append(back, byte(n))
		}
	}
	if string(back) != string(data) {
		t.Fatalf("contenu ou ordre changé (%d octets rendus pour %d)", len(back), len(data))
	}
	if want := (len(data) + 255) / 256; len(chunks) != want {
		t.Errorf("%d morceaux, attendu %d", len(chunks), want)
	}
}

// Collage réel dans un pane (tmux dédié, faux claude) : plusieurs milliers de
// lignes arrivent entières et dans l'ordre ; en lecture seule rien n'est envoyé à
// tmux et le refus est dit ; au-delà de 4 Mo rien n'est envoyé non plus.
func TestPasteBigLinesReadOnlyAndTooBig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-paste-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	var r recorder
	a := newApp(t, r.emit)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(120, 40)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))

	out := filepath.Join(t.TempDir(), "recu.txt")
	win := "=" + hostSession(ps[0]) + ":"
	tm := func(args ...string) string {
		o, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return strings.TrimSpace(string(o))
	}
	pane := tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sh -c 'stty raw -echo; cat > "+out+"'")
	if pane == "" {
		t.Fatal("pane de réception non créé")
	}
	time.Sleep(300 * time.Millisecond)
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "ligne %05d — é€\n", i)
	}
	want := b.String()
	call := func(data string) any {
		raw, _ := json.Marshal(map[string]string{"path": ps[0], "pane": pane, "data": data})
		res, err := a.Dispatch("termInput", raw)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(want); res != nil {
		t.Fatalf("collage refusé : %v", res)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(out); len(b) >= len(want) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got, _ := os.ReadFile(out); string(got) != want {
		t.Fatalf("collage de 5000 lignes : %d octets reçus sur %d (ordre ou contenu changé)", len(got), len(want))
	}
	// au-delà de la taille maximale : refusé, rien n'est envoyé
	size0, _ := os.Stat(out)
	res := call(strings.Repeat("z", maxPaste+1))
	m, _ := res.(map[string]any)
	if m == nil || m["ok"] != false || m["reason"] != "toobig" || m["msg"] == nil {
		t.Fatalf("collage trop grand : %v", res)
	}
	// lecture seule : refus dit, rien n'est envoyé à tmux, message en pied de volet
	pt.mu.Lock()
	pt.readOnly, pt.owner = true, "srv1:42"
	pt.mu.Unlock()
	res = call("pas envoyé\n")
	m, _ = res.(map[string]any)
	if m == nil || m["ok"] != false || m["reason"] != "readonly" || !strings.Contains(fmt.Sprint(m["msg"]), "lecture seule") {
		t.Fatalf("lecture seule : %v", res)
	}
	time.Sleep(500 * time.Millisecond)
	if size1, _ := os.Stat(out); size1.Size() != size0.Size() {
		t.Fatalf("saisie envoyée à tmux malgré la lecture seule (%d → %d)", size0.Size(), size1.Size())
	}
	end := time.Now().Add(3 * time.Second)
	said := false
	for !said && time.Now().Before(end) {
		a.mu.Lock()
		said = strings.Contains(a.msg, "lecture seule")
		a.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	if !said {
		a.mu.Lock()
		t.Errorf("refus non dit en pied de volet (msg=%q)", a.msg)
		a.mu.Unlock()
	}
	// plusieurs refus rapprochés : un seul message par seconde (le résultat, lui, est rendu à chaque appel)
	for i := 0; i < 5; i++ {
		if m, _ := call("x").(map[string]any); m == nil || m["reason"] != "readonly" {
			t.Fatalf("refus %d : %v", i, m)
		}
	}
}
