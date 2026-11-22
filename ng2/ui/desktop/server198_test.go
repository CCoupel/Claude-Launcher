package desktop

// Tests de spécification #198 (v6.4.0) — contracts/web-server.md « Version du serveur » : `server{version,boot}` dans
// init et l'instantané ; boot stable dans un processus, différent à chaque démarrage ; logique du client web
// (bandeau « Serveur mis à jour — Recharger » / reconnexion des terminaux) simulée par node comme st_refresh.js.
// Écrits avant l'implémentation (TDD).

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

func serverOf198(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	if s, ok := m["server"].(map[string]any); ok {
		return s
	}
	if st, ok := m["state"].(map[string]any); ok { // init : l'instantané est sous « state »
		if s, ok := st["server"].(map[string]any); ok {
			return s
		}
	}
	return nil
}

func initOf198(t *testing.T, a *App) map[string]any {
	t.Helper()
	out, err := a.Dispatch("init", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func TestInitPorteLeServeur198(t *testing.T) {
	sandbox(t, "", "alpha")
	old := core.SetNgVersion("v6.4.0")
	defer core.SetNgVersion(old)
	a := newApp(t, (&recorder{}).emit)
	m := initOf198(t, a)
	s := serverOf198(t, m)
	if s == nil {
		t.Fatalf("init sans server{version,boot} : clés %v", keys198(m))
	}
	if s["version"] != "v6.4.0" {
		t.Errorf("server.version = %v, attendu la version du launcher v6.4.0", s["version"])
	}
	if b, _ := s["boot"].(string); len(b) < 8 {
		t.Errorf("server.boot = %q : identifiant aléatoire d'au moins 8 caractères attendu", s["boot"])
	}
}

func TestSnapshotPorteLeServeur198(t *testing.T) {
	sandbox(t, "", "alpha")
	a := newApp(t, (&recorder{}).emit)
	b, _ := json.Marshal(a.Snapshot())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["server"].(map[string]any); !ok {
		t.Errorf("l'instantané (état poussé au client) ne porte pas server : %v", keys198(m))
	}
}

func TestBootStableDansUnProcessus198(t *testing.T) {
	sandbox(t, "", "alpha")
	a1 := newApp(t, (&recorder{}).emit)
	a2 := newApp(t, (&recorder{}).emit) // autre App dans le même processus : même démarrage du serveur
	s1, s1b, s2 := serverOf198(t, initOf198(t, a1)), serverOf198(t, initOf198(t, a1)), serverOf198(t, initOf198(t, a2))
	if s1 == nil || s2 == nil {
		t.Fatal("server absent de init")
	}
	if s1["boot"] != s1b["boot"] || s1["boot"] != s2["boot"] {
		t.Errorf("boot instable dans un processus : %v / %v / %v", s1["boot"], s1b["boot"], s2["boot"])
	}
}

// Un redémarrage = un nouveau processus : le test se relance lui-même deux fois.
func TestHelperBoot198(t *testing.T) {
	if os.Getenv("CL_HELPER_BOOT198") != "1" {
		t.Skip("processus auxiliaire")
	}
	sandbox(t, "", "alpha")
	a := newApp(t, (&recorder{}).emit)
	if s := serverOf198(t, initOf198(t, a)); s != nil {
		os.Stdout.WriteString("BOOT=" + s["boot"].(string) + "\n")
	}
}

func TestBootChangeAuRedemarrage198(t *testing.T) {
	boot := func() string {
		c := exec.Command(os.Args[0], "-test.run", "^TestHelperBoot198$", "-test.v")
		c.Env = append(os.Environ(), "CL_HELPER_BOOT198=1")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("processus auxiliaire : %v\n%s", err, out)
		}
		m := regexp.MustCompile(`BOOT=(\S+)`).FindStringSubmatch(string(out))
		if m == nil {
			t.Fatalf("server.boot absent de init :\n%s", out)
		}
		return m[1]
	}
	if b1, b2 := boot(), boot(); b1 == b2 {
		t.Errorf("boot identique après un redémarrage : %s", b1)
	}
}

func keys198(m map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

// ── Client web ────────────────────────────────────────────────────────────

func TestClientBandeauxServeur198(t *testing.T) {
	js := asset(t, "app.js") + asset(t, "index.html") + asset(t, "style.css")
	for _, want := range []string{"Serveur mis à jour", "Recharger", "Serveur redémarré", "reconnexion des terminaux"} {
		if !strings.Contains(js, want) {
			t.Errorf("texte du bandeau absent du client : %q", want)
		}
	}
	// le client compare le server de l'init de la page à celui d'après reconnexion, dans le flux existant
	if !strings.Contains(asset(t, "app.js"), "serverVersionAction(") {
		t.Error("serverVersionAction absente d'app.js")
	}
}

func TestClientLogiqueVersionServeur198(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "server_version.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientBandeauLauncher219(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "launcher_bar.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientNouveauProjet214(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "new_project.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientNouveauProjetGit215(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "new_git.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientApprobationEnv215(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "env_trust.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientDeplacerProjet216(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "project_move.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}

func TestClientEffetEtOeil226227(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "config_effect.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation : %v\n%s", err, out)
	}
}
