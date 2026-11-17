package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"claude-launcher-ng2/platform"
)

// #169 / #176 (LOT-3.2) — identité stable (CL_HUB_NAME), dossier de cache (CL_CACHE_DIR), écritures simultanées de
// deux launchers. Contrat : contracts/launcher-env.md (« Identité et état »). Écrit depuis le contrat (TDD).
// HOME factice systématique (withHome) : jamais le vrai dossier personnel.

// L'identité d'un launcher ne dépend plus du nom de la machine (pod recréé = nouveau nom de machine) quand
// CL_HUB_NAME est posé ; sans elle, comportement v6.2.0.
func TestLauncherIDStableWhenMachineNameChanges(t *testing.T) {
	withHome(t)
	t.Setenv("CL_INNER_SOCKET", "t-sock")
	t.Setenv("CL_HUB_NAME", "")
	if id := LauncherID(); !strings.HasPrefix(id, platform.HostName()+":") {
		t.Errorf("sans CL_HUB_NAME : %q, attendu le nom de la machine (v6.2.0)", id)
	}
	t.Setenv("CL_HUB_NAME", "cluster-qualif")
	id1 := LauncherID()
	if !strings.HasPrefix(id1, "cluster-qualif:") || !strings.HasSuffix(id1, ":t-sock") {
		t.Errorf("avec CL_HUB_NAME : %q, attendu cluster-qualif:<uid>:t-sock", id1)
	}
	if h := platform.HostName(); h != "cluster-qualif" && strings.Contains(id1, h+":") {
		t.Errorf("le nom de la machine %q fuit dans l'identité %q", h, id1)
	}
	if id2 := LauncherID(); id2 != id1 {
		t.Errorf("identité instable : %q puis %q", id1, id2)
	}
	t.Setenv("CL_HUB_NAME", "cluster-prod")
	if LauncherID() == id1 {
		t.Errorf("deux noms de hub donnent la même identité : %q", id1)
	}
}

// Le nom du launcher est la clé hubs.<nom> de backends.yaml (HubHost).
func TestHubHostFollowsCLHubName(t *testing.T) {
	withHome(t)
	t.Setenv("CL_HUB_NAME", "cluster-qualif")
	if got := HubHost(); got != "cluster-qualif" {
		t.Errorf("HubHost = %q, attendu cluster-qualif", got)
	}
	m, err := ReadHubModel([]byte("hubs:\n  cluster-qualif:\n    hub_cache: /x\n"), HubHost(), "linux")
	if err != nil || !m.Present || m.Settings.HubCache != "/x" {
		t.Errorf("la section hubs.cluster-qualif n'est pas retrouvée : %+v %v", m, err)
	}
	t.Setenv("CL_HUB_NAME", "")
	if got, want := HubHost(), hubNameFromHostname(); got != want {
		t.Errorf("sans CL_HUB_NAME : %q, attendu le nom de la machine %q", got, want)
	}
}

func hubNameFromHostname() string { h, _ := os.Hostname(); return h }

// CL_CACHE_DIR : dossier de cache utilisé tel quel (créé s'il manque) ; milestones-v2.json compris.
func TestCacheDirOverride(t *testing.T) {
	withHome(t)
	dir := filepath.Join(t.TempDir(), "partage", "cache", "cluster")
	t.Setenv("CL_CACHE_DIR", dir)
	if got := CacheDir(); got != dir {
		t.Errorf("CacheDir = %q, attendu %q (tel quel)", got, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("le dossier de cache n'est pas créé : %v", err)
	}
	if p := MsCachePath(); !strings.HasPrefix(p, dir+string(os.PathSeparator)) {
		t.Errorf("MsCachePath = %q, attendu sous %q", p, dir)
	}
	t.Setenv("CL_CACHE_DIR", "")
	if got := CacheDir(); strings.HasPrefix(got, dir) || !strings.HasSuffix(got, "claude-launcher-ng2") {
		t.Errorf("sans CL_CACHE_DIR : %q (v6.2.0 : <cache utilisateur>/claude-launcher-ng2)", got)
	}
}

// ── #176 : écritures simultanées de deux launchers, sans perte ni corruption ─

const twHelperEnv = "TW_ASSIGN_HELPER"

// TestAssignHelperProcess : sous-processus (même binaire de test) — écrit n affectations distinctes dans le
// projects.json partagé. Sans TW_ASSIGN_HELPER : ne fait rien.
func TestAssignHelperProcess(t *testing.T) {
	who := os.Getenv(twHelperEnv)
	if who == "" {
		t.Skip("sous-processus d'aide")
	}
	for i := 0; i < 60; i++ {
		if err := WriteAssignment(fmt.Sprintf("/p/%s-%03d", who, i), "pod"); err != nil {
			t.Fatalf("WriteAssignment : %v", err)
		}
	}
}

func TestConcurrentProcessesLoseNoAssignment(t *testing.T) {
	unixOnly(t)
	cfg := filepath.Join(t.TempDir(), "config")
	_ = os.MkdirAll(cfg, 0o755)
	var wg sync.WaitGroup
	outs := make([]string, 3)
	for i, who := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(i int, who string) {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestAssignHelperProcess$", "-test.count=1")
			c.Env = append(os.Environ(), twHelperEnv+"="+who, "CL_CONFIG_DIR="+cfg, "HOME="+cfg, "XDG_CACHE_HOME="+cfg, "XDG_STATE_HOME="+cfg, "CL_IN_CLUSTER=0")
			b, err := c.CombinedOutput()
			if err != nil {
				outs[i] = string(b)
			}
		}(i, who)
	}
	wg.Wait()
	for _, o := range outs {
		if o != "" {
			t.Fatalf("sous-processus en échec : %s", o)
		}
	}
	b, err := os.ReadFile(filepath.Join(cfg, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("projects.json corrompu : %v\n%.200s", err, b)
	}
	if len(m) != 180 {
		t.Errorf("%d affectations sur 180 : des écritures simultanées ont été PERDUES (lire-modifier-écrire sans verrou)", len(m))
	}
	// aucun fichier temporaire ni verrou orphelin
	ents, _ := os.ReadDir(cfg)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("fichier temporaire laissé : %s", e.Name())
		}
	}
}
