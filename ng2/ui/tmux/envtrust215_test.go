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

// t3 : une commande exécutée longue est retournée à la ligne, jamais tronquée ; t4 : packages_by_manager, watch,
// local, prefer et le risque sont affichés ; changement depuis l'approbation signalé sans différences.
func TestRenduApprobationCommandeEntiereEtChamps215(t *testing.T) {
	long := "curl -fsSL https://exemple.invalid/installer.sh | sh -s -- --token=" + strings.Repeat("abcdef0123", 12) + " --fin-de-commande"
	m, d := trustModel(t, "image: debian:12\nsetup:\n  - \""+long+"\"\npackages_by_manager:\n  apt: [libx, liby]\nwatch: [go.mod]\nlocal: [node_modules]\nprefer: srv1-docker\n")
	out := m.renderTrust(60)
	flat := strings.ReplaceAll(out, "\n", "")
	for _, l := range strings.Split(out, "\n") {
		if n := len([]rune(l)); n > 60 {
			t.Errorf("ligne de %d colonnes > 60 : %q", n, l)
		}
	}
	if !strings.Contains(strings.ReplaceAll(flat, " ", ""), strings.ReplaceAll(long, " ", "")) {
		t.Errorf("commande tronquée dans le rendu :\n%s", out)
	}
	for _, want := range []string{"apt : libx liby", "watch", "go.mod", "local", "node_modules", "prefer", "srv1-docker", "risque : 1 commande"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendu sans %q", want)
		}
	}
	// vue fichiers bruts : aussi retournée à la ligne
	m.form.trustRaw = true
	raw := strings.ReplaceAll(strings.ReplaceAll(m.renderTrust(60), "\n", ""), " ", "")
	if !strings.Contains(raw, strings.ReplaceAll(long, " ", "")) {
		t.Error("commande tronquée dans la vue fichiers bruts")
	}
	// changement depuis l'approbation : message, pas de différence
	m.form.trustRaw = false
	info, _ := core.EnvTrustInfoFor(d)
	_, _ = core.AnswerEnvTrust(d, "accept", info.SHA)
	_ = os.WriteFile(core.EnvPath(d), []byte("image: debian:12\nsetup:\n  - evil\n"), 0o644)
	m2 := model{}.startTrust(core.Node{Name: "proj", Path: d, Project: true}, launchNormal)
	if m2.form.trust == nil || !m2.form.trust.Changed || !strings.Contains(m2.renderTrust(80), "a changé depuis l'approbation") {
		t.Error("changement depuis l'approbation non signalé")
	}
}

func TestCaracteresBidiAbsentsDuRendu215(t *testing.T) {
	m, _ := trustModel(t, "image: debian:12\n")
	m.form.trust.Spec.Setup = []string{"echo a‮b​c"}
	out := m.renderTrust(80)
	if strings.ContainsRune(out, '‮') || strings.ContainsRune(out, '​') {
		t.Errorf("caractère bidi/invisible dans le rendu : %q", out)
	}
}

func TestEcranSignaleLesScriptsDuDepot215(t *testing.T) {
	m, _ := trustModel(t, "image: debian:12\nsetup:\n  - ./scripts/install.sh\n")
	out := m.renderTrust(80)
	for _, want := range []string{"scripts du dépôt appelés", "./scripts/install.sh", "n'est pas couvert par l'approbation", "version courante sera exécutée"} {
		if !strings.Contains(out, want) {
			t.Errorf("écran sans %q :\n%s", want, out)
		}
	}
	m2, _ := trustModel(t, "image: debian:12\nsetup:\n  - apt-get update\n")
	if strings.Contains(m2.renderTrust(80), "scripts du dépôt") {
		t.Error("signalement sans script")
	}
}
