package core

import (
	"strings"
	"testing"
)

// #174 (v6.4.0) — correspondance des sessions en mode serveur (cluster) : CL_BROWSER_PATH_MAP et CL_BROWSER_HINT visent
// <CL_WEB_URL>/files ; le hub et l'exe gardent le serveur de maquettes en boucle locale (127.0.0.1:8765), inchangé.
// Contrat : contracts/web-server.md « Fichiers des projets par l'interface ». HOME factice.

func browserVars174(t *testing.T) (pathMap, hint string) {
	t.Helper()
	for _, v := range BrowserEnvVars() {
		switch v.K {
		case "CL_BROWSER_PATH_MAP":
			pathMap = v.V
		case "CL_BROWSER_HINT":
			hint = v.V
		}
	}
	return
}

func TestSessionsEnClusterVisentFiles174(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv(InClusterVar, "1")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local")
	pm, hint := browserVars174(t)
	if pm != `[{"container":"/","host":"https://launcher-qualif.local/files","os":"url"}]` {
		t.Errorf("CL_BROWSER_PATH_MAP = %s", pm)
	}
	if !strings.Contains(hint, "https://launcher-qualif.local/files") {
		t.Errorf("la consigne ne vise pas <CL_WEB_URL>/files : %s", hint)
	}
	if strings.Contains(hint, "localhost:8765") || strings.Contains(hint, "127.0.0.1:8765") {
		t.Errorf("le serveur de maquettes en boucle locale ne doit plus être annoncé en cluster : %s", hint)
	}
	if strings.Contains(hint, "file://") && !strings.Contains(hint, "jamais en file://") {
		t.Errorf("la consigne propose file:// : %s", hint)
	}
	// appariement sous /files
	if strings.Contains(hint, "__claude-launcher__/pair") && !strings.Contains(hint, "/files/__claude-launcher__/pair") {
		t.Errorf("l'appariement doit passer par /files : %s", hint)
	}
}

func TestCleDeLAdresseSansSlashFinal174(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv(InClusterVar, "1")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local/")
	if pm, _ := browserVars174(t); !strings.Contains(pm, `"host":"https://launcher-qualif.local/files"`) {
		t.Errorf("slash final de CL_WEB_URL : %s", pm)
	}
}

func TestHubInchange174(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv(InClusterVar, "")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local") // sans effet hors cluster
	pm, hint := browserVars174(t)
	if pm != `[{"container":"/","host":"http://localhost:8765","os":"url"}]` {
		t.Errorf("hub : CL_BROWSER_PATH_MAP = %s, attendu le serveur local inchangé", pm)
	}
	if strings.Contains(pm+hint, "/files") {
		t.Errorf("le hub ne doit pas viser /files : %s %s", pm, hint)
	}
}

func TestBrowserPathsDeclaresPrioritaires174(t *testing.T) {
	// browser_paths déclarés par l'utilisateur : toujours prioritaires, même en cluster
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv(InClusterVar, "1")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local")
	f := parseBackendsYAML(t, "defaults:\n  browser_paths:\n    - { container: /x, host: 'http://pc:9000/x' }\n")
	ps, _, _ := resolveBrowserPaths(f, "pc", nil, false)
	if got := browserPathJSON(ps); !strings.Contains(got, "http://pc:9000/x") || strings.Contains(got, "/files") {
		t.Errorf("browser_paths déclarés non prioritaires : %s", got)
	}
}
