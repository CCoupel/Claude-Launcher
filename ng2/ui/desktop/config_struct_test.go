package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Écriture structurée par les appels de l'interface (#118, LOT-3.1) : modèle,
// changements appliqués avec commentaires conservés (critical), validation
// unique, conflit, avertissements hubs relus, test d'un backend sans outil,
// états. Aucun accès réseau ni vrai moteur.
func TestConfigStructuredCalls(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := a.Dispatch(m, b)
		if err != nil {
			t.Fatalf("%s : %v", m, err)
		}
		out, _ := json.Marshal(res)
		var v map[string]any
		_ = json.Unmarshal(out, &v)
		return v
	}
	set := func(at []string, v any) map[string]any {
		return map[string]any{"op": "set", "at": at, "value": v}
	}
	// fichier absent : modèle vide, pas de problème
	m := call("configModel", map[string]any{"kind": "backends.yaml"})
	if m["exists"] != false || len(m["problems"].([]any)) != 0 || m["model"] == nil {
		t.Fatalf("modèle d'un fichier absent : %v", m)
	}
	if _, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"project-env"}`)); err == nil {
		t.Fatal("modèle d'un type sans modèle structuré accepté")
	}
	// création par changements, puis relecture du modèle
	s := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": "", "changes": []any{
		set([]string{"backends", "srv1"}, map[string]any{"type": "docker", "host": "tcp://10.0.0.1:2375", "shared": []any{"/share/MFS/ROOT"}}),
		set([]string{"pools", "labo"}, map[string]any{"hosts": []any{"srv1", "srv2"}}),
	}})
	if s["saved"] != true || s["note"] == "" || s["mtime"] == "" {
		t.Fatalf("création : %v", s)
	}
	m = call("configModel", map[string]any{"kind": "backends.yaml"})
	bes := m["model"].(map[string]any)["backends"].([]any)
	if len(bes) == 0 || bes[len(bes)-1].(map[string]any)["name"] != "srv1" || m["mtime"] != s["mtime"] {
		t.Fatalf("modèle relu : %v", m)
	}
	// critical : un commentaire ajouté à la main survit à un changement
	path := core.BackendsPath()
	b, _ := os.ReadFile(path)
	withComment := "# ne pas toucher à l'IP de srv1\n" + string(b)
	if err := os.WriteFile(path, []byte(withComment), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	mt := st.ModTime().Format(time.RFC3339Nano)
	s2 := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": mt, "changes": []any{
		set([]string{"defaults", "backend"}, "srv1"),
	}})
	if s2["saved"] != true {
		t.Fatalf("changement : %v", s2)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# ne pas toucher à l'IP de srv1") || !strings.Contains(string(got), "backend: srv1") {
		t.Fatalf("commentaire perdu :\n%s", got)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != withComment {
		t.Fatal("copie .bak absente")
	}
	// invalide : refusé par la validation existante, fichier intact
	before, _ := os.ReadFile(path)
	bad := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		set([]string{"backends", "z"}, map[string]any{"type": "vapeur"}),
	}})
	if bad["invalid"] != true || len(bad["problems"].([]any)) == 0 {
		t.Fatalf("invalide accepté : %v", bad)
	}
	badOp := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		map[string]any{"op": "boum", "at": []string{"x"}},
	}})
	if badOp["invalid"] != true {
		t.Fatalf("opération inconnue : %v", badOp)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("fichier modifié malgré l'erreur")
	}
	// conflit : fichier modifié depuis l'ouverture
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(path, future, future)
	c := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		set([]string{"defaults", "image"}, "x:1"),
	}})
	if c["conflict"] != true || c["mtime"] == "" {
		t.Fatalf("conflit : %v", c)
	}
	// avertissements de hubs: recalculés à chaque enregistrement de backends.yaml
	if _, err := a.configSave(core.ConfBackends, "", "backends: {}\nhubs:\n  PC1:\n    pathz: []\n", c["mtime"].(string)); err != nil {
		t.Fatal(err)
	}
	warns := func() int { // relus hors de la réponse (a.mu) : attente conditionnelle
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.hubWarns)
	}
	waitWarns := func(want func(int) bool) int {
		end := time.Now().Add(5 * time.Second)
		for !want(warns()) && time.Now().Before(end) {
			time.Sleep(20 * time.Millisecond)
		}
		return warns()
	}
	if waitWarns(func(n int) bool { return n > 0 }) == 0 {
		t.Fatal("faute de frappe dans hubs: non signalée après l'enregistrement")
	}
	st, _ = os.Stat(path)
	if _, err := a.configSave(core.ConfBackends, "", "backends: {}\n", st.ModTime().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if n := waitWarns(func(n int) bool { return n == 0 }); n != 0 {
		t.Fatalf("avertissement conservé après correction : %d", n)
	}
}

func TestBackendTestAndStatesCalls(t *testing.T) {
	sandbox(t, "", "alpha")
	t.Setenv("PATH", t.TempDir()) // aucun outil : aucun accès réseau possible
	var r recorder
	a := newApp(t, r.emit)
	res, err := a.Dispatch("backendTest", json.RawMessage(`{"backend":{"name":"x","type":"docker","host":"tcp://10.9.9.9:2375","shared":["/share/MFS/ROOT"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	steps := res.(map[string]any)["steps"].([]core.TestStep)
	if len(steps) != 1 || steps[0].OK || steps[0].Label != "outil requis : docker" || steps[0].Fix == "" {
		t.Fatalf("étapes : %+v", steps)
	}
	// forme invalide : une étape d'échec, pas une erreur d'appel
	res, err = a.Dispatch("backendTest", json.RawMessage(`{"backend":{"name":"y","type":"vapeur"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := res.(map[string]any)["steps"].([]core.TestStep); len(st) != 1 || st[0].OK {
		t.Fatalf("type invalide : %+v", st)
	}
	if _, err := a.Dispatch("backendTest", json.RawMessage(`{"backend":"nom"}`)); err == nil {
		t.Fatal("arguments invalides acceptés")
	}
	// états : l'hôte est joignable sans rien calculer ; la charge est un objet
	out, err := a.Dispatch("backendStates", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var v struct {
		States map[string]core.BackendState `json:"states"`
		Load   map[string]map[string]int    `json:"load"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Load == nil {
		t.Fatalf("états : %s %v", b, err)
	}
	if h, ok := v.States["host"]; ok && !h.OK {
		t.Fatalf("host : %+v", h)
	}
}

// Réglages (#118) : les méthodes de la fenêtre ne doivent attendre aucun
// verrou tenu pendant une opération lente (rafraîchissement des conteneurs,
// partage réseau). a.mu est tenu 3 s : chaque appel répond tout de suite.
func TestSettingsCallsDoNotWaitForAppLock(t *testing.T) {
	sandbox(t, "", "alpha")
	t.Setenv("PATH", t.TempDir())
	var r recorder
	a := newApp(t, r.emit)
	a.storeBackends() // lecture des conteneurs déjà passée
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		a.mu.Lock()
		close(held)
		<-release
		a.mu.Unlock()
	}()
	<-held
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	calls := map[string]string{
		"configModel":   `{"kind":"backends.yaml"}`,
		"backendStates": `null`,
		"backendTest":   `{"backend":{"name":"x","type":"docker","host":"tcp://10.9.9.9:2375"}}`,
		"configApply":   `{"kind":"backends.yaml","mtime":"","changes":[{"op":"set","at":["backends","a"],"value":{"type":"podman"}}]}`,
	}
	for _, m := range []string{"configModel", "backendStates", "backendTest", "configApply"} {
		done := make(chan error, 1)
		t0 := time.Now()
		go func() { _, err := a.Dispatch(m, json.RawMessage(calls[m])); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("%s : %v", m, err)
			}
			if d := time.Since(t0); d > time.Second {
				t.Errorf("%s : %v avec a.mu tenu", m, d)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s attend a.mu", m)
		}
	}
	close(release)
}

// Deux relectures des avertissements hubs qui se chevauchent : l'ancienne, qui
// finit après la récente, ne l'écrase pas.
func TestHubWarnsOnlyLatestGenerationApplies(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	slow := make(chan struct{})
	first := a.refreshHubWarns(func() []string { <-slow; return []string{"ancien"} })
	second := a.refreshHubWarns(func() []string { return []string{"récent"} })
	<-second
	close(slow) // l'ancienne finit après la récente
	<-first
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.hubWarns) != 1 || a.hubWarns[0] != "récent" {
		t.Fatalf("avertissements : %v (l'ancienne relecture a écrasé la récente)", a.hubWarns)
	}
}

// Réglages d'un projet (#148) par les appels de l'interface : lecture, écriture
// de env.yaml (commentaires conservés), couleur dans config.conf relue à chaud,
// backend refusé sur un projet ouvert, sans attendre a.mu.
func TestProjectSettingsCalls(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := a.Dispatch(m, b)
		if err != nil {
			t.Fatalf("%s : %v", m, err)
		}
		out, _ := json.Marshal(res)
		var v map[string]any
		_ = json.Unmarshal(out, &v)
		return v
	}
	envFile := core.EnvPath(proj)
	_ = os.MkdirAll(filepath.Dir(envFile), 0o755)
	if err := os.WriteFile(envFile, []byte("# env du projet\nimage: docker.io/library/debian:12\npackages: [jq]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  lab: { type: docker, host: 'tcp://10.0.0.1:2375', shared: [/share] }\n"), 0o644)

	// a.mu tenu : les appels répondent quand même tout de suite
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		a.mu.Lock()
		close(held)
		<-release
		a.mu.Unlock()
	}()
	<-held
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	t0 := time.Now()
	s := call("projectSettings", map[string]any{"path": proj})
	if d := time.Since(t0); d > time.Second {
		t.Errorf("projectSettings : %v avec a.mu tenu", d)
	}
	if s["open"] != false || s["envExists"] != true || s["image"] != "docker.io/library/debian:12" || s["colorAuto"] != true ||
		len(s["merged"].([]any)) == 0 || s["envMtime"] == "" {
		t.Fatalf("réglages : %v", s)
	}
	t0 = time.Now()
	ap := call("projectApply", map[string]any{"path": proj, "color": "22", "env": map[string]any{
		"mtime": s["envMtime"], "changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"jq", "git"}}}}})
	if d := time.Since(t0); d > time.Second {
		t.Errorf("projectApply : %v avec a.mu tenu", d)
	}
	if ap["ok"] != true || ap["env"].(map[string]any)["saved"] != true {
		t.Fatalf("écriture : %v", ap)
	}
	if b, _ := os.ReadFile(envFile); !strings.Contains(string(b), "# env du projet") || !strings.Contains(string(b), "git") {
		t.Fatalf("env.yaml :\n%s", b)
	}
	if a.conf().ProjectColors["alpha"] != "22" {
		t.Errorf("couleur non relue à chaud : %v", a.conf().ProjectColors)
	}
	// backend refusé sur un projet ouvert, accepté fermé
	a.openTabs.Store(proj, true)
	if ap := call("projectApply", map[string]any{"path": proj, "backend": "lab", "image": "docker.io/library/debian:12"}); ap["ok"] != false || !strings.Contains(ap["msg"].(string), "projet ouvert") {
		t.Fatalf("projet ouvert : %v", ap)
	}
	if s := call("projectSettings", map[string]any{"path": proj}); s["open"] != true {
		t.Fatalf("open : %v", s["open"])
	}
	a.openTabs.Delete(proj)
	close(release)
	if ap := call("projectApply", map[string]any{"path": proj, "backend": "lab", "image": "docker.io/library/debian:12"}); ap["ok"] != true {
		t.Fatalf("backend : %v", ap)
	}
	if s := call("projectSettings", map[string]any{"path": proj}); s["backend"] != "lab" || s["assigned"] != true {
		t.Fatalf("backend attribué : %v", s)
	}
	// conflit : env.yaml changé depuis l'ouverture
	if ap := call("projectApply", map[string]any{"path": proj, "env": map[string]any{"mtime": "2020-01-01T00:00:00Z",
		"changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"x"}}}}}); ap["ok"] != false || ap["env"].(map[string]any)["conflict"] != true {
		t.Fatalf("conflit : %v", ap)
	}
	if _, err := a.Dispatch("projectApply", json.RawMessage(`{"path":1}`)); err == nil {
		t.Fatal("arguments invalides acceptés")
	}
}

// Mode --web : le chemin d'un projet fourni par le client doit être un projet
// de l'arbre ; un projet ouvert par un autre launcher (session vivante dans son
// conteneur) est vu ouvert ; un échec après l'écriture d'env.yaml rend le
// résultat partiel (env et colorSaved dans la réponse).
func TestProjectCallsKnownPathOpenElsewhereAndPartial(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) (map[string]any, error) {
		b, _ := json.Marshal(args)
		res, err := a.Dispatch(m, b)
		if err != nil {
			return nil, err
		}
		out, _ := json.Marshal(res)
		var v map[string]any
		_ = json.Unmarshal(out, &v)
		return v, nil
	}
	// chemins inconnus : refusés partout où un projet est visé
	outside := filepath.Join(t.TempDir(), "ailleurs")
	_ = os.MkdirAll(outside, 0o755)
	for m, args := range map[string]map[string]any{
		"projectSettings": {"path": outside},
		"projectApply":    {"path": outside, "color": "5"},
		"configFiles":     {"path": outside},
		"configOpen":      {"kind": "project-env", "path": outside},
		"configValidate":  {"kind": "project-env", "path": outside, "content": "image: x\n"},
		"configSave":      {"kind": "project-env", "path": outside, "content": "image: x\n", "mtime": ""},
		"projectApply2":   {"path": "../../etc", "color": "5"},
	} {
		m = strings.TrimSuffix(m, "2")
		if _, err := call(m, args); err == nil || !strings.Contains(err.Error(), "projet inconnu") {
			t.Errorf("%s %v : %v", m, args["path"], err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude-launcher")); err == nil {
		t.Error("écriture hors de l'arbre")
	}
	// fichiers globaux sans projet : toujours permis
	if _, err := call("configFiles", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// projet ouvert par un autre launcher : session vivante dans un conteneur
	live := map[string]bool{proj: true}
	a.liveSnap.Store(&live)
	s, err := call("projectSettings", map[string]any{"path": proj})
	if err != nil || s["open"] != true {
		t.Fatalf("open ailleurs : %v %v", s["open"], err)
	}
	ap, _ := call("projectApply", map[string]any{"path": proj, "backend": "host"})
	if ap["ok"] != false || !strings.Contains(ap["msg"].(string), "projet ouvert") {
		t.Fatalf("refus : %v", ap)
	}
	none := map[string]bool{}
	a.liveSnap.Store(&none)
	// résultat partiel : config.conf illisible pour l'interface après l'écriture d'env.yaml
	envFile := core.EnvPath(proj)
	_ = os.MkdirAll(filepath.Dir(envFile), 0o755)
	_ = os.WriteFile(envFile, []byte("image: docker.io/library/debian:12\npackages: [jq]\n"), 0o644)
	_ = os.WriteFile(core.ConfigPath(), []byte("eval \"PROJECT_COLORS=([x]=1)\"\n"), 0o644)
	s, _ = call("projectSettings", map[string]any{"path": proj})
	ap, err = call("projectApply", map[string]any{"path": proj, "color": "22", "env": map[string]any{"mtime": s["envMtime"],
		"changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"jq", "git"}}}}})
	if err != nil || ap["ok"] != false || ap["msg"] == nil || ap["env"] == nil || ap["env"].(map[string]any)["saved"] != true {
		t.Fatalf("partiel : %v %v", ap, err)
	}
	ap2, _ := call("projectApply", map[string]any{"path": proj, "env": map[string]any{"mtime": ap["env"].(map[string]any)["mtime"],
		"changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"jq"}}}}})
	if ap2["ok"] != true {
		t.Fatalf("second enregistrement en conflit avec le premier : %v", ap2)
	}
}

// La vérification d'un projet ouvert par un autre launcher est bornée : au délai,
// backend et image sont refusés (jamais laissés passer) ; la couleur et env.yaml
// ne dépendent pas de cette vérification.
func TestProjectApplyForeignOwnerTimeout(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	var r recorder
	a := newApp(t, r.emit)
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  lab: { type: docker, host: 'tcp://10.0.0.1:2375', shared: [/share] }\n"), 0o644)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	oldO, oldT := foreignOwner, foreignOwnerTimeout
	foreignOwner = func(string) string { <-release; return "" } // moteur injoignable : ne répond pas
	foreignOwnerTimeout = 150 * time.Millisecond
	t.Cleanup(func() { foreignOwner, foreignOwnerTimeout = oldO, oldT })
	t0 := time.Now()
	res, err := a.projectApply(proj, core.ProjectChange{Backend: ptr("lab"), Image: ptr("docker.io/library/debian:12")})
	if err != nil || res["ok"] != false || !strings.Contains(res["msg"].(string), "impossible de vérifier si le projet est ouvert ailleurs") {
		t.Fatalf("délai : %v %v", res, err)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("la vérification n'est pas bornée : %v", d)
	}
	if core.Assigned(proj) {
		t.Fatal("affectation écrite malgré l'impossibilité de vérifier")
	}
	// la couleur ne passe pas par cette vérification
	if res, _ := a.projectApply(proj, core.ProjectChange{Color: ptr("22")}); res["ok"] != true {
		t.Fatalf("couleur : %v", res)
	}
}

func ptr(s string) *string { return &s }

// Couleur écrite puis backend en échec SANS changement d'env : la réponse
// partielle dit colorSaved (et pas d'env), config.conf est relue à chaud.
func TestProjectApplyColorSavedWhenBackendFails(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	proj := ps[0]
	var r recorder
	a := newApp(t, r.emit)
	// projects.json est un dossier : l'écriture de l'affectation échoue
	if err := os.MkdirAll(filepath.Join(core.Ng2ConfigDir(), "projects.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := a.projectApply(proj, core.ProjectChange{Color: ptr("22"), Backend: ptr("host")})
	if err != nil || res["ok"] != false || res["msg"] == nil || res["colorSaved"] != true || res["env"] != nil {
		t.Fatalf("partiel : %v %v", res, err)
	}
	if a.conf().ProjectColors["alpha"] != "22" {
		t.Errorf("config.conf non relue à chaud : %v", a.conf().ProjectColors)
	}
}
