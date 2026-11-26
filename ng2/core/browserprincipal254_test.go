//go:build !windows

package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// #254 (P7) — l'adresse annoncée aux sessions claude (CL_BROWSER_PATH_MAP, CL_BROWSER_HINT) est l'adresse PRINCIPALE (CL_WEB_URL = hôte
// externe quand l'ingress externe est activé), jamais une adresse alternative (CL_WEB_ALT_URLS = hôte interne). Contrats : auth.md
// « Adresses publiques », helm-values.md « Ingress externe ».

func envP7(t *testing.T, web, alts string) (map[string]string, string) {
	t.Helper()
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_IN_CLUSTER", "1")
	t.Setenv("CL_WEB_URL", web)
	t.Setenv("CL_WEB_ALT_URLS", alts)
	proj := "/share/ROOT/PROJETS/QUALIF/factice-1"
	env := map[string]string{}
	for _, v := range BrowserEnvVarsFor(proj) {
		env[v.K] = v.V
	}
	return env, CtrName(proj)
}

func TestBrowserEnv254AnnouncesPrincipalAddress(t *testing.T) {
	env, name := envP7(t, "https://launcher.example.org", "https://launcher-test.coupel.lan")
	var ps []BrowserPath
	if err := json.Unmarshal([]byte(env[browserPathVar]), &ps); err != nil || len(ps) != 1 || ps[0].Host != "https://launcher.example.org/files/"+name {
		t.Fatalf("CL_BROWSER_PATH_MAP = %s (%v), attendu l'adresse principale (hôte externe)", env[browserPathVar], err)
	}
	hint := env[browserHintVar]
	if !strings.Contains(hint, "https://launcher.example.org/files/") || !strings.Contains(hint, "https://launcher.example.org/files/__claude-launcher__/pair?device=<") {
		t.Errorf("CL_BROWSER_HINT sans l'adresse principale : %s", hint)
	}
	for _, alt := range []string{"launcher-test.coupel.lan", "coupel.lan"} {
		for k, v := range env {
			if strings.Contains(v, alt) {
				t.Errorf("%s annonce l'adresse alternative %s : %.200s", k, alt, v)
			}
		}
	}
}

// Sans ingress externe : l'adresse principale est l'interne, aucune alternative.
func TestBrowserEnv254InternalOnly(t *testing.T) {
	env, name := envP7(t, "https://launcher-test.coupel.lan", "")
	var ps []BrowserPath
	if err := json.Unmarshal([]byte(env[browserPathVar]), &ps); err != nil || len(ps) != 1 || ps[0].Host != "https://launcher-test.coupel.lan/files/"+name {
		t.Fatalf("CL_BROWSER_PATH_MAP = %s (%v)", env[browserPathVar], err)
	}
}

// L'ordre de CL_WEB_ALT_URLS n'influe pas : seule CL_WEB_URL est annoncée ; une alternative invalide ne change pas l'annonce.
func TestBrowserEnv254AltOrderAndGarbageIgnored(t *testing.T) {
	for _, alts := range []string{"https://a.example,https://b.example", "https://b.example , https://a.example", "http://pas-https.example, n'importe quoi"} {
		env, name := envP7(t, "https://principal.example", alts)
		var ps []BrowserPath
		if err := json.Unmarshal([]byte(env[browserPathVar]), &ps); err != nil || len(ps) != 1 || ps[0].Host != "https://principal.example/files/"+name {
			t.Errorf("CL_WEB_ALT_URLS=%q : CL_BROWSER_PATH_MAP = %s (%v)", alts, env[browserPathVar], err)
		}
		if strings.Contains(env[browserHintVar], "a.example") || strings.Contains(env[browserHintVar], "b.example") {
			t.Errorf("CL_WEB_ALT_URLS=%q : l'indication cite une alternative : %.200s", alts, env[browserHintVar])
		}
	}
}

// Addendum 4 (sous-chemin) : l'adresse principale porte la base (CL_WEB_URL = https://hôte/claude-qualif) : les correspondances et
// l'indication annoncées aux sessions claude la contiennent (<CL_WEB_URL>/files/<projet>), l'appariement aussi ; l'alternative interne
// (sans chemin) n'est jamais annoncée.
func TestBrowserEnv254PrincipalAddressWithBase(t *testing.T) {
	env, name := envP7(t, "https://coupel.net/claude-qualif", "https://launcher-test.coupel.lan")
	var ps []BrowserPath
	if err := json.Unmarshal([]byte(env[browserPathVar]), &ps); err != nil || len(ps) != 1 || ps[0].Host != "https://coupel.net/claude-qualif/files/"+name {
		t.Fatalf("CL_BROWSER_PATH_MAP = %s (%v), attendu https://coupel.net/claude-qualif/files/%s", env[browserPathVar], err, name)
	}
	hint := env[browserHintVar]
	for _, want := range []string{"https://coupel.net/claude-qualif/files/" + name + "/<chemin du fichier", "https://coupel.net/claude-qualif/files/__claude-launcher__/pair?device=<"} {
		if !strings.Contains(hint, want) {
			t.Errorf("CL_BROWSER_HINT sans %q : %s", want, hint)
		}
	}
	if strings.Contains(hint, "launcher-test.coupel.lan") || strings.Contains(hint, "https://coupel.net/files/") {
		t.Errorf("l'indication cite l'alternative ou une URL sans la base : %s", hint)
	}
}

func TestServerFilesBase254WithBase(t *testing.T) {
	t.Setenv("CL_IN_CLUSTER", "1")
	for in, want := range map[string]string{
		"https://coupel.net/claude-qualif": "https://coupel.net/claude-qualif/files",
		"https://x.local/a/b":              "https://x.local/a/b/files",
		"https://x.local":                  "https://x.local/files",
		"https://x.local/":                 "https://x.local/files",
	} {
		t.Setenv("CL_WEB_URL", in)
		if got := ServerFilesBase(); got != want {
			t.Errorf("CL_WEB_URL=%q : %q, attendu %q", in, got, want)
		}
	}
}
