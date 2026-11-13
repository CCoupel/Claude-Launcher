package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// #79 en réel : env-template.yaml + env.yaml fusionnés dans le conteneur
// (paquets des deux, setup du template puis du projet, changement du template
// → setup rejoué). CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>.
func TestE2EEnvTemplate(t *testing.T) {
	spec := os.Getenv("CL_E2E_K8S")
	if spec == "" {
		t.Skip("CL_E2E_K8S=… pour lancer ce test")
	}
	f := strings.Split(spec, ",")
	if len(f) != 4 {
		t.Fatal("CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>")
	}
	withHome(t)
	h := filepath.Join(f[2], ".cl-e2e-tpl-"+strconv.Itoa(os.Getpid()))
	t.Setenv("HOME", h)
	t.Cleanup(func() { os.RemoveAll(h) })
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	e := Engine{BackendConf{Name: "e2e", Type: "k8s", Context: f[0], Namespace: f[1], Volumes: map[string]string{f[2]: f[3]}, Shared: []string{f[2]}}}
	proj := filepath.Join(h, "PROJETS", "TEST", "demo-tpl")
	writeProjFile(t, proj, envTemplateFile, "image: docker.io/library/debian:12\npackages: [jq]\nsetup: [\"echo tpl >> setup.log\"]\n")
	writeProjFile(t, proj, envFile, "packages: [file]\nsetup: [\"echo proj >> setup.log\"]\n")
	defer e.kcmd("delete", "statefulset", k8sName(proj), "--wait=false").Run()
	run := func() string {
		t.Helper()
		en, err := LoadEnv(proj)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		name, err := e.Ensure(proj, en, &out)
		if err != nil {
			t.Fatalf("ensure : %v\n%s", err, out.String())
		}
		if err := e.Bootstrap(name, proj, en, &out); err != nil {
			t.Fatalf("bootstrap : %v\n%s", err, out.String())
		}
		out.Reset()
		if err := e.Exec(name, false, nil, &out, "sh", "-c", "command -v jq file >/dev/null && echo outils-ok"); err != nil || !strings.Contains(out.String(), "outils-ok") {
			t.Fatalf("paquets des deux fichiers : %v %q", err, out.String())
		}
		b, _ := os.ReadFile(filepath.Join(proj, "setup.log"))
		return string(b)
	}
	if got := run(); got != "tpl\nproj\n" {
		t.Fatalf("setup : %q, attendu template puis projet", got)
	}
	writeProjFile(t, proj, envTemplateFile, "image: docker.io/library/debian:12\npackages: [jq]\nsetup: [\"echo tpl2 >> setup.log\"]\n")
	if got := run(); got != "tpl\nproj\ntpl2\nproj\n" {
		t.Fatalf("template modifié : setup %q", got)
	}
}
