package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, proj, body string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	if err := os.WriteFile(EnvPath(proj), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// local / watch : chemins relatifs au projet, sans en sortir
func TestEnvLocalWatchValidation(t *testing.T) {
	unixOnly(t)
	proj := t.TempDir()
	writeEnvFile(t, proj, "image: debian:12\nlocal: [node_modules, ./web/node_modules/, .venv]\nwatch: [package-lock.json]\non_start: [\"true\"]\n")
	e, err := LoadEnv(proj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(e.Local, ",") != "node_modules,web/node_modules,.venv" || e.Watch[0] != "package-lock.json" || len(e.OnStart) != 1 {
		t.Fatalf("env : %+v", e)
	}
	if p := e.localPaths(proj); p[1] != filepath.Join(proj, "web/node_modules") {
		t.Errorf("chemin absolu : %v", p)
	}
	for _, bad := range []string{"/abs", "..", "../x", "a/../../x", ".", "a:b", "a,b"} {
		writeEnvFile(t, proj, "image: debian:12\nlocal: ['"+bad+"']\n")
		if _, err := LoadEnv(proj); err == nil {
			t.Errorf("local %q accepté", bad)
		}
	}
}

// localKey : indépendante de l'ordre, vide sans « local »
func TestLocalKey(t *testing.T) {
	a := EnvSpec{Local: []string{"node_modules", ".venv"}}
	b := EnvSpec{Local: []string{".venv", "node_modules"}}
	if a.localKey() == "" || a.localKey() != b.localKey() {
		t.Errorf("clés : %q %q", a.localKey(), b.localKey())
	}
	if (EnvSpec{}).localKey() != "" || (EnvSpec{Local: []string{"x"}}).localKey() == a.localKey() {
		t.Error("clé vide ou collision")
	}
}

// setupHash : change avec le contenu d'un fichier watch (et son apparition),
// pas avec un fichier non surveillé
func TestSetupHashWatch(t *testing.T) {
	proj := t.TempDir()
	e := EnvSpec{Setup: []string{"npm ci"}, Watch: []string{"package-lock.json"}}
	h0 := e.setupHash(proj)
	_ = os.WriteFile(filepath.Join(proj, "package-lock.json"), []byte("v1"), 0o644)
	h1 := e.setupHash(proj)
	_ = os.WriteFile(filepath.Join(proj, "autre.txt"), []byte("x"), 0o644)
	h2 := e.setupHash(proj)
	_ = os.WriteFile(filepath.Join(proj, "package-lock.json"), []byte("v2"), 0o644)
	h3 := e.setupHash(proj)
	if h0 == h1 || h1 != h2 || h2 == h3 {
		t.Errorf("empreintes : %s %s %s %s", h0, h1, h2, h3)
	}
	if (EnvSpec{Setup: []string{"a"}}).setupHash(proj) == (EnvSpec{Setup: []string{"b"}}).setupHash(proj) {
		t.Error("setup changé, même empreinte")
	}
}

// conteneur : volume anonyme par dossier local + label ; k8s : emptyDir + annotation
func TestLocalMounts(t *testing.T) {
	unixOnly(t)
	withHome(t)
	proj := t.TempDir()
	env := EnvSpec{Image: "debian:12", Local: []string{"node_modules"}}
	e := Engine{BackendConf{Name: "podman", Type: "podman"}}
	a, err := e.runArgs("cl-x", proj, env)
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(a, " ")
	if !strings.Contains(j, "-v "+filepath.Join(proj, "node_modules")+" ") || !strings.Contains(j, "cl.local="+env.localKey()) {
		t.Errorf("runArgs : %s", j)
	}
	k := Engine{BackendConf{Name: "lab", Type: "k8s", Context: "k8s", Namespace: "ns", Volumes: map[string]string{"/share/MFS/ROOT": "pvc-a"}}}
	m, err := k.k8sManifest("/share/MFS/ROOT/p", env)
	if err != nil || !strings.Contains(string(m), `"emptyDir":{}`) || !strings.Contains(string(m), `"mountPath":"/share/MFS/ROOT/p/node_modules"`) || !strings.Contains(string(m), `"cl.local":"`+env.localKey()+`"`) {
		t.Errorf("manifeste : %v %s", err, m)
	}
}
