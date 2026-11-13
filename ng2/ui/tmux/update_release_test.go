package tmux

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// faux GitHub : tags des deux dépôts, release du template, fichiers de release
// du NG et init-project.md ; un tag est poussé avant que sa release ne sorte.
func TestUpdatesTagThenRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_SCRIPT", filepath.Join(dir, "claude-launcher.sh"))
	tplSha, ngPublished, tplPublished := "t1", false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.Path; {
		case p == "/repos/"+core.LauncherRepo+"/tags":
			fmt.Fprint(w, `[{"name":"v9.9.9","commit":{"sha":"n1"}}]`)
		case p == "/repos/"+core.TemplateRepo+"/tags":
			fmt.Fprintf(w, `[{"name":"v5.0.0","commit":{"sha":%q}}]`, tplSha)
		case p == "/repos/"+core.TemplateRepo+"/releases/tags/v5.0.0" && tplPublished:
			fmt.Fprint(w, `{}`)
		case p == "/repos/"+core.LauncherRepo+"/releases/tags/v9.9.9" && ngPublished:
			fmt.Fprintf(w, `{"assets":[{"id":1,"name":"SHA256SUMS","state":"uploaded"},{"id":2,"name":%q,"state":"uploaded"}]}`, core.NgAsset())
		case p == "/rel/v9.9.9/SHA256SUMS" && ngPublished:
			fmt.Fprintf(w, "abc  %s\n", core.NgAsset())
		case strings.HasSuffix(p, "/init-project.md"):
			fmt.Fprint(w, "init "+p)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldAPI, oldRaw, oldRel, oldVer, oldSynced := core.ApiBase, core.RawBase, core.ReleaseBase, core.NgVersion, core.InitSynced
	core.ApiBase, core.RawBase, core.ReleaseBase, core.NgVersion, core.InitSynced = srv.URL, srv.URL, srv.URL+"/rel", "v5.0.0", false
	defer func() {
		core.ApiBase, core.RawBase, core.ReleaseBase, core.NgVersion, core.InitSynced = oldAPI, oldRaw, oldRel, oldVer, oldSynced
	}()

	check := func() core.UpdMsg {
		t.Helper()
		msg, ok := core.CheckUpdates(false)().(core.UpdMsg)
		if !ok || !msg.Ok {
			t.Fatalf("vérification en échec : %+v", msg)
		}
		return msg
	}
	// sans cache (première installation) : template adopté d'office
	if msg := check(); msg.TplTag != "" || core.ReadTrim(core.TplCommitPath()) != "t1" {
		t.Fatalf("première installation : %+v, cache %q", msg, core.ReadTrim(core.TplCommitPath()))
	}
	// tags poussés, releases pas encore publiées : annoncés, rien d'adopté
	tplSha = "t2"
	msg := check()
	if msg.LauncherTag != "v9.9.9" || msg.LauncherReady || msg.TplTag != "v5.0.0" || msg.TplReady {
		t.Fatalf("tags sans release : %+v", msg)
	}
	if core.ReadTrim(core.TplCommitPath()) != "t1" {
		t.Fatal("le template ne doit pas être adopté avant le clic")
	}
	m := model{newLauncher: msg.LauncherTag, newTpl: msg.TplTag}
	if a := m.alertText("launcher"); !strings.Contains(a, "nouvelle version en préparation") {
		t.Errorf("annonce NG sans release : %q", a)
	}
	// releases publiées : installables / adoptables
	ngPublished, tplPublished = true, true
	if msg = check(); !msg.LauncherReady || !msg.TplReady || msg.TplSha != "t2" {
		t.Fatalf("releases publiées : %+v", msg)
	}
	m.launcherOK, m.tplOK = true, true
	if a := m.alertText("template"); !strings.Contains(a, "cliquer pour installer") {
		t.Errorf("annonce template publiée : %q", a)
	}
	// clic : adoption
	if done := core.AdoptTplCmd(msg.TplTag, msg.TplSha)().(core.TplAdoptedMsg); !done.Synced {
		t.Fatal("init-project.md non téléchargé")
	}
	if core.ReadTrim(core.TplCommitPath()) != "t2" || core.ReadTrim(core.TplVersionPath()) != "v5.0.0" {
		t.Fatal("cache non mis à jour après adoption")
	}
	if msg = check(); msg.TplTag != "" {
		t.Errorf("template adopté encore annoncé : %+v", msg)
	}
}
