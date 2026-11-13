package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const wslMFS = `\\wsl.localhost\Ubuntu\share\MFS`

func TestPathMapperTranslate(t *testing.T) {
	m, warns := NewPathMapper([]PathMap{
		{Local: wslMFS + `\`, Remote: "/share/MFS/"},
		{Local: `\\wsl.localhost\Ubuntu\share\MFS\ROOT\AUTRE`, Remote: "/autre"}, // plus long : prioritaire
		{Local: "C:", Remote: "/mnt/c"},
		{Local: "/mnt/mfs", Remote: "/share/MFS2"}, // poste Linux à montage différent
		{Local: "relatif", Remote: "/x"},           // ignorée
		{Local: `D:\x`, Remote: "pas-absolu"},      // ignorée
	})
	if len(warns) != 2 {
		t.Fatalf("warns : %q", warns)
	}
	toRemote := []struct{ in, want string }{
		{wslMFS + `\ROOT\CAT\proj`, "/share/MFS/ROOT/CAT/proj"},
		{`\\WSL.LOCALHOST\ubuntu\SHARE\mfs\ROOT\CAT\proj`, "/share/MFS/ROOT/CAT/proj"},  // casse indifférente
		{`//wsl.localhost/Ubuntu/share/MFS/ROOT/CAT/proj/`, "/share/MFS/ROOT/CAT/proj"}, // séparateurs « / »
		{wslMFS, "/share/MFS"},
		{wslMFS + `X\proj`, wslMFS + `X\proj`}, // pas une frontière de dossier
		{wslMFS + `\ROOT\AUTRE\p`, "/autre/p"},
		{`C:\Users\moi\GITHUB`, "/mnt/c/Users/moi/GITHUB"},
		{"/mnt/mfs/ROOT/p", "/share/MFS2/ROOT/p"},
		{"/mnt/mfsX/p", "/mnt/mfsX/p"},
		{"/share/MFS/ROOT/p", "/share/MFS/ROOT/p"}, // déjà vu des backends : inchangé
	}
	for _, c := range toRemote {
		if got, _ := m.ToRemote(c.in); got != c.want {
			t.Errorf("ToRemote(%q) = %q, attendu %q", c.in, got, c.want)
		}
	}
	toLocal := []struct{ in, want string }{
		{"/share/MFS/ROOT/CAT/proj", wslMFS + `\ROOT\CAT\proj`},
		{"/share/MFS", wslMFS},
		{"/autre/p", `\\wsl.localhost\Ubuntu\share\MFS\ROOT\AUTRE\p`},
		{"/mnt/c/Users", `C:\Users`},
		{"/share/MFS2/ROOT/p", "/mnt/mfs/ROOT/p"},
		{"/share/MFSX/p", "/share/MFSX/p"},
		{"/ailleurs", "/ailleurs"},
	}
	for _, c := range toLocal {
		if got, _ := m.ToLocal(c.in); got != c.want {
			t.Errorf("ToLocal(%q) = %q, attendu %q", c.in, got, c.want)
		}
	}
	// aller-retour
	for _, p := range []string{wslMFS + `\ROOT\CAT\proj`, `C:\Users\moi`} {
		r, _ := m.ToRemote(p)
		if l, _ := m.ToLocal(r); !strings.EqualFold(l, p) {
			t.Errorf("aller-retour %q → %q → %q", p, r, l)
		}
	}
}

// sans déclaration : tout chemin est rendu tel quel (Linux inchangé)
func TestPathMapperEmptyIsIdentity(t *testing.T) {
	var m PathMapper
	for _, p := range []string{"/share/MFS/ROOT/p", "", "relatif/x", `C:\x`, "/a//b/"} {
		if r, ok := m.ToRemote(p); r != p || ok {
			t.Errorf("ToRemote(%q) = %q %v", p, r, ok)
		}
		if l, ok := m.ToLocal(p); l != p || ok {
			t.Errorf("ToLocal(%q) = %q %v", p, l, ok)
		}
	}
	if _, err := m.remoteChecked("/share/x", "linux", "hub"); err != nil {
		t.Error(err)
	}
	if _, err := m.remoteChecked(`C:\x`, "linux", "hub"); err != nil {
		t.Error("hors Windows, jamais d'erreur :", err)
	}
	if _, err := m.remoteChecked(`C:\x`, "windows", "PC"); err == nil || !strings.Contains(err.Error(), "hubs.PC.paths") {
		t.Errorf("Windows, chemin sans correspondance : %v", err)
	}
	if _, err := m.remoteChecked("/share/x", "windows", "PC"); err != nil {
		t.Error("chemin déjà Linux :", err)
	}
}

func TestResolveHubSettings(t *testing.T) {
	var f backendsFile
	src := "hubs:\n  mon-pc:\n    paths:\n      - { local: '" + wslMFS + "', remote: /share/MFS }\n    remote_user: { uid: 1001, name: cyril }\n  autre: { paths: [{ local: 'C:\\x', remote: /x }] }\n"
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	e := resolveHubSettings(f, "MON-PC.maison.lan")
	if e.src != "hubs.mon-pc.paths" || e.paths.Empty() || e.user == nil || *e.user.UID != 1001 {
		t.Fatalf("réglages : %+v", e)
	}
	if r, _ := e.paths.ToRemote(wslMFS + `\ROOT\p`); r != "/share/MFS/ROOT/p" {
		t.Errorf("ToRemote : %q", r)
	}
	if e := resolveHubSettings(f, "hub-linux"); !e.paths.Empty() || e.user != nil || e.src != "" {
		t.Errorf("machine non déclarée : %+v", e)
	}
}

func TestResolveRemoteUser(t *testing.T) {
	local := RemoteUser{UID: 1000, GID: 1000, Name: "cyril", Home: "/home/cyril", Source: "système"}
	if u := resolveRemoteUser("linux", local, nil, ""); u != local {
		t.Errorf("Linux sans déclaration : %+v", u)
	}
	win := RemoteUser{UID: -1, GID: -1, Name: `MAISON\Cyril Coupel`, Home: `C:\Users\Cyril`}
	u := resolveRemoteUser("windows", win, nil, "")
	if u.UID != 1000 || u.GID != 1000 || u.Name != "cyrilcoupel" || u.Home != "/home/cyrilcoupel" {
		t.Errorf("Windows par défaut : %+v", u)
	}
	uid, gid := 1001, 1002
	u = resolveRemoteUser("windows", win, &RemoteUserConf{UID: &uid, GID: &gid, Name: "cc"}, "hubs.pc.remote_user")
	if u.UID != 1001 || u.GID != 1002 || u.Name != "cc" || u.Home != "/home/cc" || u.Source != "hubs.pc.remote_user" {
		t.Errorf("Windows déclaré : %+v", u)
	}
	u = resolveRemoteUser("linux", local, &RemoteUserConf{Home: "/data/home"}, "x")
	if u.UID != 1000 || u.Home != "/data/home" || u.Name != "cyril" {
		t.Errorf("Linux déclaré : %+v", u)
	}
	if n := unixName(`\\`); n != "cl" {
		t.Errorf("unixName vide : %q", n)
	}
}

// withHubPaths : backends.yaml de test déclarant, pour cette machine, la
// correspondance d'un poste Windows (le chemin local est de forme UNC : la
// traduction, logique pure, ne dépend pas du système).
func withHubPaths(t *testing.T, extra string) {
	t.Helper()
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	y := "hubs:\n  " + hubName() + ":\n    paths:\n      - { local: '" + wslMFS + "', remote: /share/MFS }\n" + extra
	if err := os.WriteFile(BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Même projet vu de Windows (UNC) et d'un hub Linux : mêmes nom, labels,
// montages, workdir, annotations — sinon le conteneur serait dupliqué.
func TestSameNamesFromWindowsAndLinux(t *testing.T) {
	withHubPaths(t, "")
	win := wslMFS + `\ROOT\CLAUDE_PROJETS\PRO\Mon_Projet`
	lin := "/share/MFS/ROOT/CLAUDE_PROJETS/PRO/Mon_Projet"
	if RemotePath(win) != lin || LocalPath(lin) != win {
		t.Fatalf("RemotePath %q LocalPath %q", RemotePath(win), LocalPath(lin))
	}
	for _, f := range []struct {
		n  string
		fn func(string) string
	}{{"CtrName", CtrName}, {"legacyCtrName", legacyCtrName}, {"ctrHost", ctrHost}, {"k8sName", k8sName}, {"legacyK8sName", legacyK8sName}} {
		if a, b := f.fn(win), f.fn(lin); a != b {
			t.Errorf("%s : Windows %q ≠ Linux %q", f.n, a, b)
		}
	}
	if CtrName(win) != "pro-mon-projet" {
		t.Errorf("CtrName : %q", CtrName(win))
	}
	env := EnvSpec{Image: "debian:12", Local: []string{"node_modules"}}
	if lp := env.localPaths(win); len(lp) != 1 || lp[0] != lin+"/node_modules" {
		t.Errorf("localPaths : %q", lp)
	}
	k := Engine{BackendConf{Name: "lab", Type: "k8s", Volumes: map[string]string{"/share/MFS/ROOT": "pvc"}}}
	mw, err1 := k.k8sManifest(win, env)
	ml, err2 := k.k8sManifest(lin, env)
	if err1 != nil || err2 != nil || string(mw) != string(ml) {
		t.Errorf("manifestes différents :\n%s\n%s", mw, ml)
	}
	var man struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(mw, &man)
	if man.Metadata.Annotations["cl.project"] != lin {
		t.Errorf("annotation cl.project : %q", man.Metadata.Annotations["cl.project"])
	}
	v := bootstrapVars(win, env)
	if !strings.Contains(v, "CL_PROJ='"+lin+"'") || !strings.Contains(v, "CL_LOCAL='"+lin+"/node_modules'") {
		t.Errorf("bootstrapVars : %s", v)
	}
	if s := CleanExitShell(win); s != CleanExitShell(lin) || strings.Contains(s, `\`) {
		t.Errorf("CleanExitShell : %s", s)
	}
}

// labels, montages, workdir et env d'un conteneur : chemins Linux
func TestRunArgsUseMapping(t *testing.T) {
	withHubPaths(t, "")
	win := wslMFS + `\ROOT\CAT\proj`
	t.Setenv("CLAUDE_CONFIG_DIR", wslMFS+`\ROOT\.claude`)
	e := Engine{BackendConf{Name: "podman", Type: "podman"}} // local : pas de contrôle du partage
	a, err := e.runArgs(CtrName(win), win, EnvSpec{Image: "debian:12"})
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(a, " ")
	for _, want := range []string{"cl.project=/share/MFS/ROOT/CAT/proj", "src=/share/MFS/ROOT/CAT/proj,dst=/share/MFS/ROOT/CAT/proj", "-w /share/MFS/ROOT/CAT/proj", "CLAUDE_CONFIG_DIR=/share/MFS/ROOT/.claude"} {
		if !strings.Contains(j, want) {
			t.Errorf("runArgs sans %q : %s", want, j)
		}
	}
	if strings.Contains(j, `\`) {
		t.Errorf("chemin Windows transmis au moteur : %s", j)
	}
}

// projects.json (commun à tous les hubs) : indexé par le chemin Linux
func TestAssignmentsKeyedByRemotePath(t *testing.T) {
	withHubPaths(t, "")
	win := wslMFS + `\ROOT\CAT\proj`
	if err := WriteAssignment(win, "lab"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(assignPath())
	var raw map[string]string
	_ = json.Unmarshal(b, &raw)
	if raw["/share/MFS/ROOT/CAT/proj"] != "lab" || len(raw) != 1 {
		t.Errorf("projects.json : %s", b)
	}
	if ReadAssignments()[win] != "lab" || !Assigned(win) {
		t.Errorf("lecture : %v", ReadAssignments())
	}
}

// sans déclaration : projects.json et noms inchangés
func TestAssignmentsUnchangedWithoutMapping(t *testing.T) {
	withHome(t)
	p := "/share/MFS/ROOT/CAT/proj"
	if err := WriteAssignment(p, "lab"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(assignPath())
	if !strings.Contains(string(b), `"/share/MFS/ROOT/CAT/proj": "lab"`) {
		t.Errorf("projects.json : %s", b)
	}
	if RemotePath(p) != p || LocalPath(p) != p || CtrName(p) != "cat-proj" {
		t.Error("chemins modifiés sans déclaration")
	}
}

func TestSharedProblemsUseMapping(t *testing.T) {
	root := t.TempDir() // tient lieu du partage vu du poste
	withHome(t)
	h := os.Getenv("HOME")
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	y := "hubs:\n  " + hubName() + ":\n    paths:\n      - { local: '" + root + "', remote: /share/MFS }\n"
	_ = os.WriteFile(BackendsPath(), []byte(y), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "ROOT", ".claude"), 0o755)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "ROOT", ".claude"))
	b := BackendConf{Name: "pool", Type: "docker", Host: "tcp://srv:2375", Shared: []string{"/share/MFS"}}
	if ps := sharedProblems(b, filepath.Join(root, "ROOT", "CAT", "proj")); len(ps) != 0 {
		t.Errorf("tout est sur le partage (via la correspondance) : %v", ps)
	}
	if ps := sharedProblems(b, "/ailleurs/proj"); len(ps) != 1 || !strings.Contains(ps[0].What, "hors du stockage partagé") {
		t.Errorf("projet hors partage : %v", ps)
	}
}

func TestSharedRootFix(t *testing.T) {
	if f := sharedRootFixFor("linux", "/share/MFS", "srv1"); !strings.Contains(f, "mfsmount /share/MFS") {
		t.Errorf("Linux : %s", f)
	}
	if f := sharedRootFixFor("windows", "/share/MFS", "srv1"); !strings.Contains(f, `\share\MFS`) || !strings.Contains(f, "hubs.<machine>.paths") {
		t.Errorf("Windows : %s", f)
	}
}

func TestCleanRelSlashForm(t *testing.T) {
	for in, want := range map[string]string{"a/b/../c": "a/c", "node_modules/": "node_modules", " x/y ": "x/y"} {
		if got, err := cleanRel(in); err != nil || got != want {
			t.Errorf("cleanRel(%q) = %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"/etc", "../x", "", ".", "a:b"} {
		if _, err := cleanRel(bad); err == nil {
			t.Errorf("cleanRel(%q) accepté", bad)
		}
	}
}

func TestMappedBrowserPaths(t *testing.T) {
	m, _ := NewPathMapper([]PathMap{{Local: wslMFS, Remote: "/share/MFS"}})
	ps := mappedBrowserPaths([]string{wslMFS + `\ROOT\PROJETS`, "/share/MFS/ROOT", `C:\Users\x\GITHUB`, "/share/MFS/ROOT"}, m)
	if len(ps) != 2 || ps[0].Container != "/share/MFS/ROOT/PROJETS" || ps[0].Host != wslMFS+`\ROOT\PROJETS` || ps[0].OS != "windows" ||
		ps[1].Container != "/share/MFS/ROOT" || ps[1].Host != wslMFS+`\ROOT` {
		t.Errorf("mappedBrowserPaths : %+v", ps)
	}
	// browser_server off + correspondance : chemins locaux pour Chrome
	var f backendsFile
	_ = yaml.Unmarshal([]byte("defaults: { browser_server: { listen: off } }\nhubs:\n  pc: { paths: [{ local: '"+wslMFS+"', remote: /share/MFS }] }\n"), &f)
	got, src, _ := resolveBrowserPaths(f, "pc", []string{"/share/MFS/ROOT"}, false)
	if len(got) != 1 || got[0].Host != wslMFS+`\ROOT` || !strings.Contains(src, "hubs.pc.paths") {
		t.Errorf("resolveBrowserPaths : %+v %s", got, src)
	}
	got, _, _ = resolveBrowserPaths(f, "hub-linux", []string{"/share/MFS/ROOT"}, false)
	if len(got) != 1 || got[0].Host != "/share/MFS/ROOT" {
		t.Errorf("autre machine : défaut inchangé : %+v", got)
	}
}
