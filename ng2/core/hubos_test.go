package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Configuration partagée du poste MSI : hub Linux (WSL) et exe Windows, même
// nom de machine, même localhost (#110).
const sharedMSI = `
hubs:
  MSI:
    browser_server: { listen: 127.0.0.1:8080, allow: [/data] }
    windows:
      browser_server: { listen: 127.0.0.1:8081 }
    paths:
      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
`

// Surcharge par système : chaque système a son port, champ par champ.
func TestBrowserServerPerOS(t *testing.T) {
	f := parseBackendsYAML(t, sharedMSI+`
  web:
    browser_server: { listen: "9000", url: "https://cl.maison.lan" }
    windows:
      browser_server: { url: "http://win.maison.lan:9000/" }
  muet:
    browser_server: { listen: off }
    windows:
      browser_server: { listen: "9100" }
  mac:
    darwin:
      browser_server: { listen: "9200" }
`)
	cases := []struct {
		host, goos, listen, base, lsrc, src string
		off                                 bool
	}{
		{"MSI", "linux", "127.0.0.1:8080", "http://localhost:8080", "hubs.MSI.browser_server.listen", "hubs.MSI.browser_server", false},
		{"msi", "windows", "127.0.0.1:8081", "http://localhost:8081", "hubs.MSI.windows.browser_server.listen", "hubs.MSI.windows.browser_server", false},
		{"MSI", "", "127.0.0.1:8080", "http://localhost:8080", "hubs.MSI.browser_server.listen", "hubs.MSI.browser_server", false},
		{"MSI", "darwin", "127.0.0.1:8080", "http://localhost:8080", "hubs.MSI.browser_server.listen", "hubs.MSI.browser_server", false},
		// url surchargée seule : listen hérité
		{"web", "windows", "127.0.0.1:9000", "http://win.maison.lan:9000", "hubs.web.browser_server.listen", "hubs.web.windows.browser_server", false},
		{"web", "linux", "127.0.0.1:9000", "https://cl.maison.lan", "hubs.web.browser_server.listen", "hubs.web.browser_server", false},
		// off pour Linux, rallumé pour Windows
		{"muet", "linux", "off", "", "", "", true},
		{"muet", "windows", "127.0.0.1:9100", "http://localhost:9100", "hubs.muet.windows.browser_server.listen", "hubs.muet.windows.browser_server", false},
		{"mac", "darwin", "127.0.0.1:9200", "http://localhost:9200", "hubs.mac.darwin.browser_server.listen", "hubs.mac.darwin.browser_server", false},
		{"mac", "windows", "127.0.0.1:8766", "http://localhost:8766", "défaut Windows", "défaut", false},
	}
	for _, c := range cases {
		got, err := resolveBrowserServerFor(f, c.host, c.goos)
		if err != nil || got.Off != c.off || !c.off && (got.Listen != c.listen || got.Base != c.base || got.ListenSrc != c.lsrc || got.Source != c.src) {
			t.Errorf("%s/%s : %+v %v", c.host, c.goos, got, err)
		}
	}
	// allow non surchargé : hérité de hubs.MSI
	if got, _ := resolveBrowserServerFor(f, "MSI", "windows"); len(got.Allow) != 1 || got.Allow[0] != "/data" {
		t.Errorf("allow hérité : %+v", got)
	}
	// sans surcharge par système (goos "") : identique à resolveBrowserServer
	for _, h := range []string{"MSI", "web", "muet", "mac", "autre"} {
		a, _ := resolveBrowserServer(f, h)
		b, _ := resolveBrowserServerFor(f, h, "")
		if a.Listen != b.Listen || a.Base != b.Base || a.Off != b.Off || a.Source != b.Source {
			t.Errorf("%s : %+v ≠ %+v", h, a, b)
		}
	}
	// listen invalide dans la surcharge : l'erreur la nomme
	bad := parseBackendsYAML(t, "hubs:\n  pc:\n    windows:\n      browser_server: { listen: nimporte }\n")
	if _, err := resolveBrowserServerFor(bad, "pc", "windows"); err == nil || !strings.Contains(err.Error(), "hubs.pc.windows.browser_server.listen") {
		t.Errorf("erreur : %v", err)
	}
}

// Sans listen : 8765 hors Windows (inchangé), 8766 pour l'exe Windows ; un
// listen déclaré pour les deux reste celui des deux.
func TestBrowserServerWindowsDefault(t *testing.T) {
	both := parseBackendsYAML(t, "defaults:\n  browser_server: { listen: \"9001\" }\nhubs:\n  pc:\n    browser_server: { listen: \"9002\" }\n")
	cases := []struct {
		f                  backendsFile
		host, goos, listen string
		lsrc               string
	}{
		{backendsFile{}, "MSI", "linux", "127.0.0.1:8765", "défaut"},
		{backendsFile{}, "MSI", "", "127.0.0.1:8765", "défaut"},
		{backendsFile{}, "MSI", "darwin", "127.0.0.1:8765", "défaut"},
		{backendsFile{}, "MSI", "windows", "127.0.0.1:8766", "défaut Windows"},
		{both, "autre", "windows", "127.0.0.1:9001", "defaults.browser_server.listen"},
		{both, "pc", "windows", "127.0.0.1:9002", "hubs.pc.browser_server.listen"},
	}
	for _, c := range cases {
		got, err := resolveBrowserServerFor(c.f, c.host, c.goos)
		if err != nil || got.Listen != c.listen || got.ListenSrc != c.lsrc {
			t.Errorf("%s/%s : %+v %v", c.host, c.goos, got, err)
		}
	}
	if defaultBrowserListen != "127.0.0.1:8765" {
		t.Fatal("défaut Linux modifié")
	}
}

// CL_BROWSER_PATH_MAP et consigne : chaque launcher donne l'URL et le jeton
// de son propre serveur ; une page d'appariement ouverte avec le jeton de
// l'autre launcher est refusée.
func TestBrowserPathMapPerOS(t *testing.T) {
	f := parseBackendsYAML(t, sharedMSI)
	roots := []string{"/share/MFS/ROOT"}
	for goos, want := range map[string]string{
		"linux":   `[{"container":"/","host":"http://localhost:8080","os":"url"}]`,
		"windows": `[{"container":"/","host":"http://localhost:8081","os":"url"}]`,
		"":        `[{"container":"/","host":"http://localhost:8080","os":"url"}]`,
	} {
		ps, src, _ := resolveBrowserPathsFor(f, "MSI", roots, goos == "linux", goos)
		if got := browserPathJSON(ps); got != want || !strings.Contains(src, "serveur web") {
			t.Errorf("%q : %s (%s)", goos, got, src)
		}
	}
	ps, _, _ := resolveBrowserPathsFor(backendsFile{}, "MSI", roots, false, "windows")
	if got := browserPathJSON(ps); got != `[{"container":"/","host":"http://localhost:8766","os":"url"}]` {
		t.Errorf("défaut Windows : %s", got)
	}
	// browser_paths surchargés pour Windows seulement
	g := parseBackendsYAML(t, sharedMSI+`    browser_paths: [{ container: /share/MFS, host: '\\wsl.localhost\Ubuntu\share\MFS' }]
    linux:
      browser_paths: []
`)
	ps, src, _ := resolveBrowserPathsFor(g, "MSI", roots, true, "linux")
	if len(ps) != 0 || src != "hubs.MSI.linux.browser_paths" {
		t.Errorf("linux : %s %s", browserPathJSON(ps), src)
	}
	ps, src, _ = resolveBrowserPathsFor(g, "MSI", roots, false, "windows")
	if len(ps) != 1 || src != "hubs.MSI.browser_paths" {
		t.Errorf("windows : %s %s", browserPathJSON(ps), src)
	}

	lin, _ := resolveBrowserServerFor(f, "MSI", "linux")
	win, _ := resolveBrowserServerFor(f, "MSI", "windows")
	hl := browserHint(lin, "", "", "jetonlinux", "MSI", nil)
	hw := browserHint(win, "", "", "jetonwindows", "MSI", nil)
	if !strings.Contains(hl, "http://localhost:8080"+browserPairPath+"?device=<son deviceId>&token=jetonlinux") || strings.Contains(hl, "8081") {
		t.Errorf("consigne Linux : %s", hl)
	}
	if !strings.Contains(hw, "http://localhost:8081"+browserPairPath+"?device=<son deviceId>&token=jetonwindows") || strings.Contains(hw, "8080") {
		t.Errorf("consigne Windows : %s", hw)
	}
	// le serveur Linux refuse le jeton du launcher Windows
	srv := httptest.NewServer(fileServer{hosts: map[string]bool{"127.0.0.1": true}, roots: func() []string { return nil }, token: func() string { return "jetonlinux" }})
	defer srv.Close()
	resp, err := http.Get(srv.URL + browserPairPath + "?device=bb02d4f4-47aa-4728-976e-4841b526f6ba&token=jetonwindows")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(b), "autre launcher") {
		t.Errorf("jeton de l'autre launcher : %d %q", resp.StatusCode, b)
	}
}

// paths, remote_user et browser_device surchargeables ; le filtre par forme
// de paths reste appliqué.
func TestHubSettingsPerOS(t *testing.T) {
	f := parseBackendsYAML(t, `
hubs:
  MSI:
    browser_device: commun
    remote_user: { uid: 1000 }
    paths:
      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
      - { local: /mnt/data, remote: /data }
    windows:
      browser_device: bb02d4f4-47aa-4728-976e-4841b526f6ba
      remote_user: { uid: 2000 }
      paths:
        - { local: 'D:\MFS', remote: /share/MFS }
`)
	w := resolveHubSettingsFor(f, "MSI", "windows")
	if r, _ := w.paths.ToRemote(`D:\MFS\x`); r != "/share/MFS/x" || w.src != "hubs.MSI.windows.paths" {
		t.Errorf("paths windows : %q %s", r, w.src)
	}
	if w.user == nil || *w.user.UID != 2000 || w.userSrc != "hubs.MSI.windows.remote_user" {
		t.Errorf("remote_user windows : %+v %s", w.user, w.userSrc)
	}
	l := resolveHubSettingsFor(f, "MSI", "linux")
	if l.src != "hubs.MSI.paths" || l.skipped != 1 || len(l.paths.Entries()) != 1 || l.userSrc != "hubs.MSI.remote_user" {
		t.Errorf("linux : %+v", l)
	}
	a, b := resolveHubSettings(f, "MSI"), resolveHubSettingsFor(f, "MSI", "")
	if a.src != b.src || len(a.paths.Entries()) != 2 || len(b.paths.Entries()) != 2 {
		t.Errorf("sans système : %+v / %+v", a, b)
	}
	if id, src := browserDeviceFor(f, "MSI", "windows"); id != "bb02d4f4-47aa-4728-976e-4841b526f6ba" || src != "hubs.MSI.windows.browser_device" {
		t.Errorf("device windows : %s %s", id, src)
	}
	if id, src := browserDeviceFor(f, "MSI", "linux"); id != "commun" || src != "hubs.MSI.browser_device" {
		t.Errorf("device linux : %s %s", id, src)
	}
}

// Fautes de frappe dans une surcharge par système : avertissement avec la
// ligne, clé ignorée, le reste du bloc et du fichier s'applique ; jamais
// d'erreur (config partagée avec le hub Linux de production).
func TestHubOSUnknownKeys(t *testing.T) {
	head := "hubs:\n  MSI:\n    browser_server: { listen: \"8080\" }\n    windows:\n      browser_device: dev-win\n"
	cases := []struct{ y, want string }{
		{head + "      browser_sever: { listen: \"8081\" }\n", `ligne 6 : clé "browser_sever" inconnue, ignorée — vouliez-vous dire browser_server ?`},
		{head + "      linux: {}\n", `ligne 6 : surcharge "linux" imbriquée`},
		{head + "      browser_server: nimporte\n", "valeur illisible, ignorée"},
		{"hubs:\n  MSI:\n    browser_server: { listen: \"8080\" }\n    windows: [8081]\n", "ligne 4 : objet attendu"},
	}
	for _, c := range cases {
		var f backendsFile
		if err := yaml.Unmarshal([]byte(c.y), &f); err != nil {
			t.Errorf("%q : erreur %v", c.y, err)
		}
		ws := hubConfigWarnings(f)
		if len(ws) != 1 || !strings.HasPrefix(ws[0], "hubs.MSI.windows : ") || !strings.Contains(ws[0], c.want) {
			t.Errorf("%q : %q", c.y, ws)
		}
		// valeurs valides appliquées : port Linux commun, Windows sur le défaut
		if l, _ := resolveBrowserServerFor(f, "MSI", "linux"); l.Listen != "127.0.0.1:8080" {
			t.Errorf("%q : linux %+v", c.y, l)
		}
		if w, _ := resolveBrowserServerFor(f, "MSI", "windows"); w.Listen != "127.0.0.1:8080" {
			t.Errorf("%q : windows %+v", c.y, w)
		}
		if strings.Contains(c.y, "browser_device") {
			if id, _ := browserDeviceFor(f, "MSI", "windows"); id != "dev-win" {
				t.Errorf("%q : reste du bloc perdu (%q)", c.y, id)
			}
		}
		writeBackends(t, c.y+"backends: {}\n")
		if _, err := LoadBackends(); err != nil {
			t.Errorf("LoadBackends %q : %v", c.y, err)
		}
	}
	// toutes les clés connues : aucun avertissement
	var f backendsFile
	if err := yaml.Unmarshal([]byte("hubs:\n  MSI:\n    linux: { browser_server: {}, browser_paths: [], browser_device: x, paths: [], remote_user: {} }\n"), &f); err != nil || len(hubConfigWarnings(f)) != 0 {
		t.Error(err, hubConfigWarnings(f))
	}
	if ws := hubConfigWarnings(parseBackendsYAML(t, sharedMSI)); len(ws) != 0 {
		t.Errorf("config valide : %q", ws)
	}
}

// Clés inconnues directement sous hubs.<machine> (avant : ignorées en
// silence) : avertissement avec suggestion, clés connues inchangées.
func TestHubUnknownKeys(t *testing.T) {
	y := `
hubs:
  MSI:
    browser_server: { listen: 127.0.0.1:8080 }
    window:
      browser_server: { listen: 127.0.0.1:8081 }
    browser_sever: { listen: "9999" }
    macos: {}
    zzz: 1
    paths:
      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
backends: {}
`
	var f backendsFile
	if err := yaml.Unmarshal([]byte(y), &f); err != nil {
		t.Fatal(err)
	}
	ws := hubConfigWarnings(f)
	want := []string{
		`hubs.MSI : ligne 5 : clé "window" inconnue, ignorée — vouliez-vous dire windows ?`,
		`hubs.MSI : ligne 7 : clé "browser_sever" inconnue, ignorée — vouliez-vous dire browser_server ?`,
		`hubs.MSI : ligne 8 : clé "macos" inconnue, ignorée — vouliez-vous dire darwin ?`,
		`hubs.MSI : ligne 9 : clé "zzz" inconnue, ignorée (clés : `,
	}
	if len(ws) != len(want) {
		t.Fatalf("%q", ws)
	}
	for i, w := range want {
		if !strings.HasPrefix(ws[i], w) {
			t.Errorf("%d : %q, attendu %q", i, ws[i], w)
		}
	}
	// clés connues appliquées, clés inconnues sans effet
	for _, goos := range []string{"linux", "windows", ""} {
		if c, _ := resolveBrowserServerFor(f, "MSI", goos); c.Listen != "127.0.0.1:8080" {
			t.Errorf("%q : %+v", goos, c)
		}
	}
	if hs := resolveHubSettingsFor(f, "MSI", "windows"); hs.paths.Empty() {
		t.Error("paths perdus")
	}
	writeBackends(t, y)
	if _, err := LoadBackends(); err != nil {
		t.Errorf("LoadBackends : %v", err)
	}
	if ws := HubConfigWarnings(); len(ws) != 4 {
		t.Errorf("HubConfigWarnings : %q", ws)
	}
}

func TestSuggestKey(t *testing.T) {
	for k, want := range map[string]string{
		"window": "windows", "Windows": "windows", "linus": "linux", "darwn": "darwin", "win": "windows", "osx": "darwin",
		"browser_sever": "browser_server", "browserserver": "browser_server", "browser_path": "browser_paths",
		"path": "paths", "remote_usr": "remote_user", "zzz": "", "listen": "", "url": "",
	} {
		if got := suggestKey(k, hubKeys); got != want {
			t.Errorf("%q : %q, attendu %q", k, got, want)
		}
	}
}

// Diag : origine du listen affichée avec une surcharge ou sous Windows,
// rien d'autre sous Linux sans surcharge (sortie d'avant).
func TestListenOriginLine(t *testing.T) {
	f := parseBackendsYAML(t, sharedMSI)
	lin, _ := resolveBrowserServerFor(f, "MSI", "linux")
	win, _ := resolveBrowserServerFor(f, "MSI", "windows")
	def, _ := resolveBrowserServerFor(backendsFile{}, "MSI", "windows")
	if l := listenOriginLine(lin, "linux"); l != "" {
		t.Errorf("linux sans surcharge : %q", l)
	}
	if l := listenOriginLine(win, "windows"); l != "réglage pour windows : listen de hubs.MSI.windows.browser_server.listen" {
		t.Errorf("windows : %q", l)
	}
	if l := listenOriginLine(def, "windows"); l != "réglage pour windows : listen de défaut Windows" {
		t.Errorf("windows défaut : %q", l)
	}
	if h := ownPortHint("MSI", "windows"); !strings.Contains(h, "hubs.MSI.windows.browser_server.listen (ex. 127.0.0.1:8766)") {
		t.Errorf("conseil : %q", h)
	}
}
