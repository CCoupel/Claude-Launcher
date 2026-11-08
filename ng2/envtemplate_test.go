package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeProjFile(t *testing.T, proj, rel, content string) {
	t.Helper()
	p := filepath.Join(proj, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// #79 : env-template.yaml (template) puis env.yaml (projet).
func TestEnvTemplateFusion(t *testing.T) {
	proj := t.TempDir()
	writeProjFile(t, proj, envTemplateFile, `image: ubuntu:24.04
packages: [jq, gh]
packages_by_manager: {apt: [build-essential], dnf: [gcc]}
setup: [tpl-1, tpl-2]
on_start: [tpl-start]
watch: [go.sum]
local: [node_modules]
mounts: [{path: ../commun}, {path: ../ro, ro: false}]
`)
	writeProjFile(t, proj, envFile, `image: fedora:41
prefer: pod
packages: [gh, ripgrep]
packages_by_manager: {apt: [libpq-dev], apk: [x]}
setup: [proj-1]
on_start: [proj-start]
watch: [go.sum, package-lock.json]
mounts: [{path: ../ro, ro: true}, {path: ../autre}]
`)
	e, err := loadEnv(proj)
	if err != nil {
		t.Fatal(err)
	}
	if e.Image != "fedora:41" || e.Prefer != "pod" {
		t.Fatalf("image/prefer du projet attendus : %q %q", e.Image, e.Prefer)
	}
	eq := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %v, attendu %v", name, got, want)
		}
	}
	eq("packages", e.Packages, []string{"jq", "gh", "ripgrep"})
	eq("pbm", e.PackagesByManager, map[string][]string{"apt": {"build-essential", "libpq-dev"}, "dnf": {"gcc"}, "apk": {"x"}})
	eq("setup", e.Setup, []string{"tpl-1", "tpl-2", "proj-1"})
	eq("on_start", e.OnStart, []string{"tpl-start", "proj-start"})
	eq("watch", e.Watch, []string{"go.sum", "package-lock.json"})
	eq("local", e.Local, []string{"node_modules"})
	if len(e.Mounts) != 3 || e.Mounts[1].Path != "../ro" || e.Mounts[1].RO == nil || !*e.Mounts[1].RO {
		t.Fatalf("mounts : %+v", e.Mounts)
	}
	if !hasEnv(proj) {
		t.Fatal("hasEnv")
	}
	// avec le launcher : ses étapes d'abord
	full := e.withLauncher(EnvSpec{Setup: []string{"launcher"}, raw: []byte("x")})
	eq("setup complet", full.Setup, []string{"launcher", "tpl-1", "tpl-2", "proj-1"})
}

// Template seul : suffit (image fournie), pas de prefer autorisé.
func TestEnvTemplateSeul(t *testing.T) {
	proj := t.TempDir()
	writeProjFile(t, proj, envTemplateFile, "image: ubuntu:24.04\nsetup: [a]\n")
	if !hasEnv(proj) {
		t.Fatal("template seul : environnement attendu")
	}
	e, err := loadEnv(proj)
	if err != nil || e.Image != "ubuntu:24.04" {
		t.Fatalf("%v %q", err, e.Image)
	}
	h1 := e.hash()
	writeProjFile(t, proj, envTemplateFile, "image: ubuntu:24.04\nsetup: [a, b]\n")
	e2, _ := loadEnv(proj)
	if e2.hash() == h1 {
		t.Fatal("un changement du template doit changer l'empreinte")
	}
	writeProjFile(t, proj, envTemplateFile, "image: ubuntu:24.04\nprefer: pod\n")
	if _, err := loadEnv(proj); err == nil || !strings.Contains(err.Error(), "prefer") {
		t.Fatalf("prefer dans le template : erreur attendue, %v", err)
	}
}

// Compatibilité : un env.yaml seul garde exactement son empreinte d'avant.
func TestEnvSeulInchange(t *testing.T) {
	proj := t.TempDir()
	content := "image: ubuntu:24.04\npackages: [jq]\n"
	writeProjFile(t, proj, envFile, content)
	e, err := loadEnv(proj)
	if err != nil {
		t.Fatal(err)
	}
	if string(e.raw) != content {
		t.Fatalf("raw modifié : %q", e.raw)
	}
	_ = os.Remove(envPath(proj))
	if _, err := loadEnv(proj); !os.IsNotExist(err) {
		t.Fatalf("aucun fichier : IsNotExist attendu, %v", err)
	}
	if hasEnv(proj) {
		t.Fatal("aucun fichier : pas d'environnement")
	}
}

// Sans image nulle part : erreur ; image du template + socle identique choisi :
// env.yaml non créé ; autre socle : env.yaml créé avec ce socle.
func TestEnvTemplateImage(t *testing.T) {
	proj := t.TempDir()
	writeProjFile(t, proj, envTemplateFile, "packages: [jq]\n")
	if _, err := loadEnv(proj); err == nil {
		t.Fatal("sans image : erreur attendue")
	}
	writeProjFile(t, proj, envTemplateFile, "image: ubuntu:24.04\n")
	if err := applyImage(proj, "ubuntu:24.04"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(envPath(proj)); err == nil {
		t.Fatal("socle du template : env.yaml ne doit pas être créé")
	}
	if err := applyImage(proj, "fedora:41"); err != nil {
		t.Fatal(err)
	}
	if e, _ := loadEnv(proj); e.Image != "fedora:41" {
		t.Fatalf("socle choisi : %q", e.Image)
	}
}
