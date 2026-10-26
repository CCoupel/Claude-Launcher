package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// faux GitHub : tags des deux dépôts, release du template, fichiers de release
// du NG et init-project.md ; un tag est poussé avant que sa release ne sorte.
func TestUpdatesTagThenRelease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_SCRIPT", filepath.Join(dir, "claude-launcher.sh"))
	tplSha, ngPublished, tplPublished := "t1", false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.Path; {
		case p == "/repos/"+launcherRepo+"/tags":
			fmt.Fprint(w, `[{"name":"v9.9.9","commit":{"sha":"n1"}}]`)
		case p == "/repos/"+templateRepo+"/tags":
			fmt.Fprintf(w, `[{"name":"v5.0.0","commit":{"sha":%q}}]`, tplSha)
		case p == "/repos/"+templateRepo+"/releases/tags/v5.0.0" && tplPublished:
			fmt.Fprint(w, `{}`)
		case p == "/rel/v9.9.9/SHA256SUMS" && ngPublished:
			fmt.Fprintf(w, "abc  %s\n", ngAsset())
		case strings.HasSuffix(p, "/init-project.md"):
			fmt.Fprint(w, "init "+p)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldAPI, oldRaw, oldRel, oldVer, oldSynced := apiBase, rawBase, releaseBase, ngVersion, initSynced
	apiBase, rawBase, releaseBase, ngVersion, initSynced = srv.URL, srv.URL, srv.URL+"/rel", "v1.0.0", false
	defer func() {
		apiBase, rawBase, releaseBase, ngVersion, initSynced = oldAPI, oldRaw, oldRel, oldVer, oldSynced
	}()

	check := func() updMsg {
		t.Helper()
		msg, ok := checkUpdates(false)().(updMsg)
		if !ok || !msg.ok {
			t.Fatalf("vérification en échec : %+v", msg)
		}
		return msg
	}
	// sans cache (première installation) : template adopté d'office
	if msg := check(); msg.tplTag != "" || readTrim(tplCommitPath()) != "t1" {
		t.Fatalf("première installation : %+v, cache %q", msg, readTrim(tplCommitPath()))
	}
	// tags poussés, releases pas encore publiées : annoncés, rien d'adopté
	tplSha = "t2"
	msg := check()
	if msg.launcherTag != "v9.9.9" || msg.launcherReady || msg.tplTag != "v5.0.0" || msg.tplReady {
		t.Fatalf("tags sans release : %+v", msg)
	}
	if readTrim(tplCommitPath()) != "t1" {
		t.Fatal("le template ne doit pas être adopté avant le clic")
	}
	m := model{newLauncher: msg.launcherTag, newTpl: msg.tplTag}
	if a := m.alertText("launcher"); !strings.Contains(a, "mise à jour disponible") {
		t.Errorf("annonce NG sans release : %q", a)
	}
	// releases publiées : installables / adoptables
	ngPublished, tplPublished = true, true
	if msg = check(); !msg.launcherReady || !msg.tplReady || msg.tplSha != "t2" {
		t.Fatalf("releases publiées : %+v", msg)
	}
	m.launcherOK, m.tplOK = true, true
	if a := m.alertText("template"); !strings.Contains(a, "cliquer pour installer") {
		t.Errorf("annonce template publiée : %q", a)
	}
	// clic : adoption
	if done := adoptTplCmd(msg.tplTag, msg.tplSha)().(tplAdoptedMsg); !done.synced {
		t.Fatal("init-project.md non téléchargé")
	}
	if readTrim(tplCommitPath()) != "t2" || readTrim(tplVersionPath()) != "v5.0.0" {
		t.Fatal("cache non mis à jour après adoption")
	}
	if msg = check(); msg.tplTag != "" {
		t.Errorf("template adopté encore annoncé : %+v", msg)
	}
}
