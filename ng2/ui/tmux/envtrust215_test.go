package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

func trustModel(t *testing.T, env string) (model, string) {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, ".claude-launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.EnvPath(d), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := core.MarkEnvPending(d, "https://github.com/a/b.git", "ubuntu:24.04"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	m := model{cfg: core.Config{}}
	return m.startTrust(core.Node{Name: "proj", Path: d, Project: true}, launchNormal), d
}

func key(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestEcranDApprobation215(t *testing.T) {
	m, d := trustModel(t, "image: debian:12\nsetup:\n  - curl x | sh\n")
	if m.form.mode != formTrust || m.form.trust == nil {
		t.Fatalf("écran non ouvert : %+v", m.form.mode)
	}
	// annuler : rien d'écrit
	nm, _ := m.onFormKey(key("esc"))
	if nm.(model).form.mode != formNone || core.EnvTrust(d) != core.TrustPending {
		t.Error("annuler a modifié l'état")
	}
	// v : fichiers bruts ; accepter : définitif
	nm, _ = m.onFormKey(key("v"))
	if !nm.(model).form.trustRaw {
		t.Error("v n'a pas basculé les fichiers bruts")
	}
	nm, _ = m.onFormKey(key("a"))
	if core.EnvTrust(d) != core.TrustAccepted || nm.(model).form.mode != formNone {
		t.Errorf("accepter : état %s", core.EnvTrust(d))
	}
}

func TestOuvrirSansApprobation215(t *testing.T) {
	m, d := trustModel(t, "image: debian:12\nsetup:\n  - curl x | sh\n")
	if nm, _ := m.onFormKey(key("s")); nm.(model).form.mode != formNone || core.EnvTrust(d) != core.TrustIgnored {
		t.Errorf("ouvrir sans : état %s", core.EnvTrust(d))
	}
	if e, err := core.LoadEnv(d); err != nil || e.Image != "ubuntu:24.04" || len(e.Setup) != 0 {
		t.Errorf("socle seul attendu : %+v %v", e, err)
	}
}

func TestApprobationShaPerime215(t *testing.T) {
	m, d := trustModel(t, "image: debian:12\n")
	_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - evil\n"), 0o644) // modifié pendant la question
	nm, _ := m.onFormKey(key("a"))
	f := nm.(model).form
	if f.mode != formTrust || f.err == "" || core.EnvTrust(d) != core.TrustPending {
		t.Fatalf("sha périmé : mode=%v err=%q état=%s", f.mode, f.err, core.EnvTrust(d))
	}
	if !strings.Contains(strings.Join(f.trust.Spec.Setup, ""), "evil") {
		t.Error("question non relue après la modification")
	}
}

// Aucune séquence de terminal du dépôt n'atteint l'écran.
func TestApprobationEchappeLesSequences215(t *testing.T) {
	m, _ := trustModel(t, "image: debian:12\nsetup:\n  - \"echo \\e[2J\\e]0;pwn\\a\"\n")
	m.form.trust.Spec.Setup = []string{"a\x1b[2Jb\x1b]0;pwn\x07"}
	m.form.trust.Source = "https://x/\x1b[31m"
	for _, raw := range []bool{false, true} {
		m.form.trustRaw = raw
		m.form.trust.Files["env.yaml"] = "x: \x1b[2J\x9b0m\x07"
		out := m.renderTrust(80)
		if strings.IndexByte(out, 0x07) >= 0 || strings.IndexByte(out, 0x9b) >= 0 || strings.IndexByte(out, 0x1b) >= 0 {
			t.Errorf("séquence de terminal dans le rendu (raw=%v) : %q", raw, out)
		}
	}
}
