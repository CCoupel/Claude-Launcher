package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #255 (dev) : routage par identité. Serveur en mode proxy sans App commune (seuls les chemins des utilisateurs sont
// exercés) ; API GitHub factice ; dossiers temporaires.

type sp255 struct {
	w   *webBridge
	srv *httptest.Server
	ids map[string]*atomic.Int64
}

func sp255Start(t *testing.T) *sp255 {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	ids := map[string]*atomic.Int64{}
	for l, id := range map[string]int64{"alice": 11, "bob": 22} {
		v := &atomic.Int64{}
		v.Store(id)
		ids[l] = v
	}
	api := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		l := strings.TrimPrefix(r.URL.Path, "/")
		if id, ok := ids[l]; ok {
			fmt.Fprintf(rw, `{"id":%d,"login":%q}`, id.Load(), l)
			return
		}
		http.NotFound(rw, r)
	}))
	t.Cleanup(api.Close)
	old := core.GithubUsersURL
	core.GithubUsersURL = api.URL + "/"
	t.Cleanup(func() { core.GithubUsersURL = old })
	auth, err := core.ParseAuthConf("root", "alice,bob", "")
	if err != nil {
		t.Fatal(err)
	}
	w := &webBridge{access: core.AccessProxy, auth: auth, subs: map[chan []byte]bool{}, audit: core.NewAudit(t.TempDir())}
	s := &sp255{w: w, srv: httptest.NewServer(w.handler()), ids: ids}
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sp255) call(t *testing.T, login, method, args string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", s.srv.URL+"/api/call", strings.NewReader(`{"method":"`+method+`","args":`+args+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-User", login)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSpacesInitVideEtBackendsIsoles255(t *testing.T) {
	s := sp255Start(t)
	cfg := `{"kind":"backends.yaml","content":"backends:\n  mien:\n    type: k8s\n    namespace: ns\n"}`
	if code, out := s.call(t, "alice", "configSave", cfg); code != 200 || out["error"] != nil || out["result"].(map[string]any)["saved"] != true {
		t.Fatalf("alice configSave : %d %v", code, out)
	}
	// alice voit son backend, bob n'a rien
	_, a := s.call(t, "alice", "init", `{}`)
	_, b := s.call(t, "bob", "init", `{}`)
	ra, rb := a["result"].(map[string]any), b["result"].(map[string]any)
	if n := len(ra["space"].(map[string]any)["backends"].([]any)); n != 1 {
		t.Errorf("alice : %d backend(s), 1 attendu", n)
	}
	if rb["space"].(map[string]any)["empty"] != true {
		t.Errorf("bob doit voir un espace vide : %v", rb["space"])
	}
	if rows := ra["state"].(map[string]any)["rows"].([]any); len(rows) != 0 {
		t.Errorf("l'utilisateur voit des projets : %v", rows)
	}
	if ra["user"] == nil || strings.Contains(fmt.Sprint(a), "github-11") {
		t.Errorf("identifiant numérique exposé ou user absent : %v", a)
	}
	_, o := s.call(t, "bob", "configOpen", `{"kind":"backends.yaml"}`)
	if strings.Contains(fmt.Sprint(o), "mien") {
		t.Errorf("bob lit le backend d'alice : %v", o)
	}
}

func TestSpacesMethodesEtFichiersRefuses255(t *testing.T) {
	s := sp255Start(t)
	// écritures R1 : « forbidden » comme en v7.0.0
	for _, m := range []string{"backendSecretSet", "relaunch", "configApply", "userList"} {
		_, out := s.call(t, "alice", m, `{}`)
		if r, _ := out["result"].(map[string]any); r["forbidden"] != "admin" {
			t.Errorf("%s : forbidden attendu pour un utilisateur : %v", m, out)
		}
	}
	for _, m := range []string{"open", "refresh", "create", "termInput", "backendTest", "quit", "secretReveal"} {
		_, out := s.call(t, "alice", m, `{}`)
		if out["error"] == nil || !strings.Contains(fmt.Sprint(out["error"]), msgNoSpaceMethod) {
			t.Errorf("%s accepté pour un utilisateur : %v", m, out)
		}
	}
	for _, k := range []string{"config.conf", "env.yaml", "hub", "project-env"} {
		_, out := s.call(t, "alice", "configOpen", `{"kind":"`+k+`"}`)
		if out["error"] == nil {
			t.Errorf("configOpen %s accepté pour un utilisateur", k)
		}
	}
	if _, out := s.call(t, "alice", "configOpen", `{"kind":"backends.yaml","path":"/etc"}`); out["error"] == nil {
		t.Error("chemin du client accepté")
	}
	req, _ := http.NewRequest("GET", s.srv.URL+"/files/p/readme.md", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 404 {
		t.Errorf("/files d'un utilisateur : %v %v, 404 attendu", resp, err)
	}
}

func TestSpacesLoginReprisRefuse255(t *testing.T) {
	s := sp255Start(t)
	if code, _ := s.call(t, "alice", "clientInfo", `{}`); code != 200 {
		t.Fatalf("premier accès : %d", code)
	}
	if code, _ := s.call(t, "inconnu", "clientInfo", `{}`); code != 403 {
		t.Errorf("login hors liste : %d, 403 attendu", code)
	}
	// l'entrée d'alice devient périmée puis son login est repris par un autre compte : 401, jusqu'à l'action de l'admin
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(core.UserIndexPath(), []byte(`{"users":{"alice":{"id":11,"at":"`+old+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.ids["alice"].Store(999)
	if code, _ := s.call(t, "alice", "init", `{}`); code != 401 {
		t.Errorf("login repris : %d, 401 attendu", code)
	}
	if code, _ := s.call(t, "bob", "init", `{}`); code != 200 {
		t.Errorf("un autre utilisateur n'est pas touché : %d", code)
	}
	if err := core.ForgetUserLogin("alice"); err != nil {
		t.Fatal(err)
	}
	if code, _ := s.call(t, "alice", "init", `{}`); code != 200 {
		t.Errorf("après l'action de l'admin : %d", code)
	}
	if s.w.spaces.count() == 0 {
		t.Error("aucune instance créée")
	}
}

func TestSpacesEvenementsParIdentite255(t *testing.T) {
	s := sp255Start(t)
	open := func(login string) (<-chan string, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", s.srv.URL+"/api/events", nil)
		req.Header.Set("X-Forwarded-User", login)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan string, 16)
		go func() {
			defer resp.Body.Close()
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if l := sc.Text(); strings.HasPrefix(l, "data: ") {
					ch <- l
				}
			}
		}()
		return ch, cancel
	}
	chA, cancelA := open("alice")
	chB, cancelB := open("bob")
	defer cancelA()
	defer cancelB()
	deadline := time.Now().Add(5 * time.Second)
	for { // les deux flux sont enregistrés
		s.w.subMu.Lock()
		n := len(s.w.subs)
		s.w.subMu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("flux non enregistrés")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.w.emit("admin-only", map[string]any{"x": 1})          // App commune : aucun utilisateur ne la reçoit
	s.w.emitTo("github-11", "pour-alice", map[string]any{}) // alice seule
	drain := func(ch <-chan string) (got []string) {
		timeout := time.After(700 * time.Millisecond)
		for {
			select {
			case l := <-ch:
				got = append(got, l)
			case <-timeout:
				return
			}
		}
	}
	a, b := strings.Join(drain(chA), "\n"), strings.Join(drain(chB), "\n")
	if !strings.Contains(a, "pour-alice") {
		t.Errorf("alice ne reçoit pas son évènement : %q", a)
	}
	for _, g := range []string{a, b} {
		if strings.Contains(g, "admin-only") {
			t.Errorf("évènement de l'App commune reçu par un utilisateur : %q", g)
		}
	}
	if strings.Contains(b, "pour-alice") {
		t.Errorf("bob reçoit l'évènement d'alice : %q", b)
	}
}

func TestSpacesLiberationApres10Minutes255(t *testing.T) {
	var h spaceHub
	sp, _ := core.UserSpace("github-5")
	u := h.get(sp, "alice")
	now := time.Now()
	if h.reap(now) != 0 || h.count() != 1 {
		t.Fatal("instance récente libérée")
	}
	u.clients.Add(1) // un client connecté : jamais libérée
	if h.reap(now.Add(time.Hour)) != 0 {
		t.Error("instance avec client libérée")
	}
	u.clients.Add(-1)
	u.touch()
	if h.reap(now.Add(9*time.Minute)) != 0 {
		t.Error("libérée avant 10 min")
	}
	if h.reap(now.Add(11*time.Minute)) != 1 || h.count() != 0 {
		t.Error("non libérée après 10 min sans client")
	}
}

func TestSpacesYamlIllisibleRefuse255(t *testing.T) {
	s := sp255Start(t)
	_, out := s.call(t, "alice", "configSave", `{"kind":"backends.yaml","content":"backends: ["}`)
	if r, _ := out["result"].(map[string]any); r["forbidden"] != "admin" || r["saved"] != nil {
		t.Errorf("configSave d'un YAML illisible : %v", out)
	}
	_, out = s.call(t, "alice", "configValidate", `{"kind":"backends.yaml","content":"backends: ["}`)
	if ps := fmt.Sprint(out["result"]); !strings.Contains(ps, "error") && !strings.Contains(ps, "erreur") {
		t.Errorf("configValidate d'un YAML illisible sans erreur : %v", out)
	}
}

// Séquence de l'éditeur de backends actuel (Réglages › Backends), pour un utilisateur : à l'ouverture configModel puis
// backendStates, à l'enregistrement configApply (changements structurés), puis relecture. Limité à SON backends.yaml.
func TestSpacesEditeurBackendsSequence255(t *testing.T) {
	s := sp255Start(t)
	res := func(login, method, args string) map[string]any {
		t.Helper()
		code, out := s.call(t, login, method, args)
		if code != 200 || out["error"] != nil {
			t.Fatalf("%s %s : %d %v", login, method, code, out)
		}
		r, _ := out["result"].(map[string]any)
		return r
	}
	// ouverture : modèle vide valide (fichier absent), états vides
	m := res("alice", "configModel", `{"kind":"backends.yaml"}`)
	if m["model"] == nil || m["path"] != "backends.yaml" || m["exists"] != false {
		t.Fatalf("configModel à l'ouverture : %v", m)
	}
	if ps, _ := m["problems"].([]any); len(ps) != 0 {
		t.Errorf("problèmes sur un espace vide : %v", ps)
	}
	if st := res("alice", "backendStates", `{}`); st["states"] == nil || st["load"] == nil {
		t.Errorf("backendStates : %v", st)
	}
	// enregistrement : ajout d'un backend k8s (namespace libre)
	add := `{"kind":"backends.yaml","path":"","mtime":"","changes":[{"op":"set","at":["backends","mien"],"value":{"type":"k8s","namespace":"ns-alice"}}]}`
	if r := res("alice", "configApply", add); r["saved"] != true {
		t.Fatalf("configApply : %v", r)
	}
	m = res("alice", "configModel", `{"kind":"backends.yaml"}`)
	if !strings.Contains(fmt.Sprint(m["model"]), "mien") || m["exists"] != true || m["mtime"] == "" {
		t.Errorf("le modèle ne montre pas le backend ajouté : %v", m)
	}
	if got := fmt.Sprint(res("bob", "configModel", `{"kind":"backends.yaml"}`)["model"]); strings.Contains(got, "mien") {
		t.Errorf("bob voit le backend d'alice : %s", got)
	}
	// conflit : mtime périmé
	if r := res("alice", "configApply", `{"kind":"backends.yaml","mtime":"2000-01-01T00:00:00Z","changes":[{"op":"set","at":["backends","x"],"value":{"type":"k8s","namespace":"n"}}]}`); r["conflict"] != true {
		t.Errorf("conflit attendu : %v", r)
	}
	// champs R1 : refusés ; changement inapplicable : invalid
	if r := res("alice", "configApply", `{"kind":"backends.yaml","mtime":"`+m["mtime"].(string)+`","changes":[{"op":"set","at":["backends","mien","volumes"],"value":{"/a":"pvc"}}]}`); r["forbidden"] != "admin" {
		t.Errorf("volumes : forbidden attendu : %v", r)
	}
	if r := res("alice", "configApply", `{"kind":"backends.yaml","changes":[{"op":"boum","at":["a"]}]}`); r["invalid"] != true {
		t.Errorf("changement inapplicable : %v", r)
	}
	// autres fichiers : refusés, jamais ceux de l'admin
	for _, k := range []string{"config.conf", "env.yaml", "hub"} {
		_, out := s.call(t, "alice", "configModel", `{"kind":"`+k+`"}`)
		if out["error"] == nil || !strings.Contains(fmt.Sprint(out["error"]), "administrateur") {
			t.Errorf("configModel %s : refus attendu : %v", k, out)
		}
		if r := res("alice", "configApply", `{"kind":"`+k+`","changes":[]}`); r["forbidden"] != "admin" {
			t.Errorf("configApply %s : forbidden attendu : %v", k, r)
		}
	}
}
