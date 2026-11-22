//go:build !windows

package desktop

// #215 (critical) : create source git (asynchrone : job, newJob, createCancel) et template (marqueur, initProposal).
// Faux `git` et faux `claude` dans le PATH / CL_CLAUDE_BIN, tmux dédié isolé, moteurs de conteneurs du PATH écartés
// (backends.yaml vide) : jamais de vrai git, de vrai claude ni de vrai réseau. Bases factices sous t.TempDir().

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// jeton FACTICE : invalide par construction (underscores, longueur) et reconnaissable — testguard échoue s'il apparaît
// dans un vrai settings.json (testguard.FakeTokenMarker)
const tok215 = "ghp_FAKE_TEST_215_not_a_token"

type fix215 struct {
	a    *App
	r    *recorder
	base string
	rec  string // journal du faux git (argv + environnement)
	pid  string // pid écrit par le faux git (mode lent)
	in   string // ce que le faux claude reçoit sur son entrée
}

// newFix215 : bac à sable + jeton dans config.conf + faux git (body) + faux claude.
func newFix215(t *testing.T, gitBody string) *fix215 {
	t.Helper()
	base := base214(t)
	fakeClaudeSession214(t)
	cfgDir := os.Getenv("CL_CONFIG_DIR")
	_ = os.WriteFile(filepath.Join(cfgDir, "backends.yaml"), []byte("backends: {}\n"), 0o644) // pas d'étape « socle »
	_ = os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte("GITHUB_TOKEN=\""+tok215+"\"\n"), 0o600)
	f := &fix215{base: base, rec: filepath.Join(t.TempDir(), "rec"), pid: filepath.Join(t.TempDir(), "pid"), in: filepath.Join(t.TempDir(), "in")}
	bin := t.TempDir()
	realGit, _ := exec.LookPath("git")
	// seul « git … clone » est simulé ; tout autre appel (git init, describe, rev-parse… de l'application) va au vrai git
	script := "#!/bin/sh\ncase \" $* \" in\n*\" clone \"*) ;;\n*) exec " + realGit + " \"$@\";;\nesac\n" +
		"{ echo \"ARGV:$*\"; env | sort; } > " + f.rec + "\nfor last; do :; done; dest=\"$last\"\n" + gitBody + "\n" +
		"mkdir -p \"$dest/.git\"\nprintf '[remote \"origin\"]\\n\\turl = https://github.com/a/b\\n' > \"$dest/.git/config\"\necho hello > \"$dest/README.md\"\nexit 0\n"
	_ = os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// faux claude : écrit tout ce qu'il reçoit (tty brut)
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\nstty raw -echo\necho faux claude prêt\nexec cat > "+f.in+"\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	old := newJobHold.Get()
	newJobHold.Set(time.Minute) // l'état final reste lisible pendant le test
	t.Cleanup(func() { newJobHold.Set(old) })
	f.r = &recorder{}
	f.a = newApp(t, f.r.emit)
	cleanupApp(t, f.a) // avant kill-server (enregistré plus tôt, exécuté plus tard) : clients tmux détachés, ouvertures finies
	return f
}

func (f *fix215) create(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	return dispatchJSON(t, f.a, "create", args)
}

func (f *fix215) job() *NewJob { return f.a.Snapshot().NewJob }

func (f *fix215) waitState(t *testing.T, want string) *NewJob {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if j := f.job(); j != nil && j.State == want {
			return j
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("newJob n'atteint pas %q : %+v", want, f.job())
	return nil
}

func (f *fix215) noSecret(t *testing.T) {
	t.Helper()
	snap, _ := json.Marshal(f.a.Snapshot())
	evs, _ := json.Marshal(f.r.all())
	for what, s := range map[string]string{"instantané": string(snap), "évènements": string(evs)} {
		if strings.Contains(s, tok215) {
			t.Errorf("jeton dans %s", what)
		}
	}
}

func gitArgs215(base string) map[string]any {
	return map[string]any{"base": base, "category": "CLONES", "newCategory": true, "name": "repo", "source": "git",
		"url": "https://github.com/CCoupel/repo.git"}
}

func TestCreateGitSucces215(t *testing.T) {
	f := newFix215(t, "")
	v := f.create(t, gitArgs215(f.base))
	id, _ := v["job"].(string)
	if id == "" || v["tab"] != nil {
		t.Fatalf("create git = %v, attendu {job} immédiat", v)
	}
	if j := f.job(); j == nil || j.ID != id || j.Name != "repo" {
		t.Errorf("newJob = %+v", j)
	}
	j := f.waitState(t, "done")
	if j.Pct != 100 || j.Msg != "" {
		t.Errorf("job terminé = %+v", j)
	}
	dir := filepath.Join(f.base, "CLONES", "repo")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("dépôt non cloné en %s : %v", dir, err)
	}
	b, _ := os.ReadFile(f.rec)
	argv := strings.SplitN(string(b), "\n", 2)[0]
	if strings.Contains(argv, tok215) || !strings.Contains(argv, "-- https://github.com/CCoupel/repo.git "+dir) {
		t.Errorf("argv = %s", argv)
	}
	if !strings.Contains(string(b), "CL_GIT_TOKEN="+tok215) || !strings.Contains(string(b), "GIT_ASKPASS=") {
		t.Error("jeton de config.conf non transmis au processus git (GIT_ASKPASS)")
	}
	if cfg, _ := os.ReadFile(filepath.Join(dir, ".git", "config")); strings.Contains(string(cfg), tok215) {
		t.Error("jeton dans .git/config")
	}
	f.noSecret(t)
	// projet ouvert à la fin comme pour les autres sources
	f.r.wait(t, 20*time.Second, "ouverture", isTerm(dir, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	// une création terminée n'empêche pas la suivante
	args := gitArgs215(f.base)
	args["name"] = "repo2"
	if v := f.create(t, args); v["job"] == nil {
		t.Errorf("seconde création après la première terminée : %v", v)
	}
	f.waitState(t, "done")
}

func TestCreateGitEchecNettoieToutSansSecret215(t *testing.T) {
	f := newFix215(t, `printf 'fatal: Authentication failed for https://x-access-token:`+tok215+`@github.com/a/b\n' >&2
mkdir -p "$dest/.git"; echo partiel > "$dest/.git/partiel"
exit 128`)
	args := gitArgs215(f.base) // catégorie neuve : doit disparaître aussi
	before := snapDir214(t, f.base)
	assignBefore, _ := os.ReadFile(filepath.Join(os.Getenv("CL_CONFIG_DIR"), "projects.json"))
	f.create(t, args)
	j := f.waitState(t, "failed")
	if !strings.Contains(j.Msg, "authentification refusée") || !strings.Contains(j.Msg, "supprimé") {
		t.Errorf("message = %q, attendu la raison et ce qui a été nettoyé", j.Msg)
	}
	if strings.Contains(j.Msg, tok215) {
		t.Error("jeton dans le message d'échec")
	}
	if after := snapDir214(t, f.base); len(after) != len(before) {
		t.Errorf("reste sur le disque : avant=%v après=%v", before, after)
	}
	if _, err := os.Stat(filepath.Join(f.base, "CLONES")); err == nil {
		t.Error("catégorie créée par l'appel non retirée")
	}
	if now, _ := os.ReadFile(filepath.Join(os.Getenv("CL_CONFIG_DIR"), "projects.json")); string(now) != string(assignBefore) {
		t.Error("projects.json modifié par un clonage échoué")
	}
	f.noSecret(t)
	// catégorie qui existait déjà : conservée, seul le dossier du projet part
	_ = os.MkdirAll(filepath.Join(f.base, "GARDEE"), 0o755)
	args["category"], args["newCategory"], args["name"] = "GARDEE", false, "echec2"
	f.create(t, args)
	f.waitState(t, "failed")
	if _, err := os.Stat(filepath.Join(f.base, "GARDEE")); err != nil {
		t.Error("catégorie préexistante supprimée")
	}
	if _, err := os.Stat(filepath.Join(f.base, "GARDEE", "echec2")); err == nil {
		t.Error("dossier du projet échoué conservé")
	}
}

func TestCreateGitLentAnnulable215(t *testing.T) {
	f := newFix215(t, "echo $$ > "+"$CL_TEST_PID"+"\nsleep 120")
	t.Setenv("CL_TEST_PID", f.pid)
	before := snapDir214(t, f.base)
	f.create(t, gitArgs215(f.base))
	var id string
	if j := f.job(); j != nil {
		id = j.ID
	}
	// deuxième création pendant la première : refusée
	if v := f.create(t, gitArgs215(f.base)); v["ok"] != false || !strings.Contains(v["msg"].(string), "création en cours") {
		t.Errorf("seconde création simultanée = %v, attendu refus « création en cours »", v)
	}
	var pid int
	for i := 0; i < 200 && pid == 0; i++ {
		if b, err := os.ReadFile(f.pid); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(30 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("le faux git lent n'a pas démarré")
	}
	if j := f.job(); j == nil || j.State != "running" {
		t.Errorf("job = %+v, attendu running", j)
	}
	if v := dispatchJSON(t, f.a, "createCancel", map[string]any{"job": "inconnu"}); v["ok"] != false {
		t.Errorf("annulation d'un job inconnu = %v", v)
	}
	if v := dispatchJSON(t, f.a, "createCancel", map[string]any{"job": id}); v["ok"] != true {
		t.Fatalf("createCancel = %v", v)
	}
	j := f.waitState(t, "failed")
	if !strings.Contains(j.Msg, "annulée") || !strings.Contains(j.Msg, "supprimé") {
		t.Errorf("message = %q", j.Msg)
	}
	if after := snapDir214(t, f.base); len(after) != len(before) {
		t.Errorf("reste sur le disque après annulation : %v", after)
	}
	gone := false
	for i := 0; i < 100 && !gone; i++ {
		gone = syscall.Kill(pid, 0) != nil
		if b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); strings.Contains(string(b), ") Z ") {
			gone = true
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !gone {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Error("processus git encore vivant après l'annulation")
	}
	f.noSecret(t)
}

func TestCreateGitURLRefusees215(t *testing.T) {
	f := newFix215(t, "")
	before := snapDir214(t, f.base)
	for _, u := range []string{"", "file:///etc/passwd", "-uevil", "--upload-pack=x", "/tmp/depot", "../depot", "ext::sh -c id",
		"git://github.com/a/b", "http://github.com/a/b", "https://user:" + tok215 + "@github.com/a/b", "ssh://-oProxyCommand=x/a"} {
		args := gitArgs215(f.base)
		args["url"] = u
		v := f.create(t, args)
		if v["ok"] != false || v["field"] != "url" || strings.TrimSpace(v["msg"].(string)) == "" {
			t.Errorf("url %q → %v, attendu {ok:false, field:url, msg}", u, v)
		}
		if s, _ := json.Marshal(v); strings.Contains(string(s), tok215) {
			t.Errorf("url %q : jeton dans la réponse", u)
		}
	}
	if _, err := os.Stat(f.rec); err == nil {
		t.Error("git lancé pour une URL refusée")
	}
	if after := snapDir214(t, f.base); len(after) != len(before) {
		t.Errorf("création malgré une URL refusée : %v", after)
	}
	if f.job() != nil {
		t.Errorf("newJob présent après un refus : %+v", f.job())
	}
}

func TestCreateGitCheminForgeRefuse215(t *testing.T) {
	f := newFix215(t, "")
	for _, c := range []map[string]any{
		{"base": "/tmp", "name": "x"}, {"base": f.base, "category": "../..", "name": "x"}, {"base": f.base, "category": "A", "name": "../x"},
	} {
		c["source"], c["url"] = "git", "https://github.com/a/b"
		if v := f.create(t, c); v["ok"] != false || v["field"] == nil {
			t.Errorf("%v → %v : refus de chemin attendu", c, v)
		}
	}
	if _, err := os.Stat(f.rec); err == nil {
		t.Error("git lancé pour un chemin refusé")
	}
}

func countProposals215(r *recorder, path string) int {
	n := 0
	for _, e := range r.all() {
		if e["_name"] == "initProposal" && e["path"] == path {
			n++
		}
	}
	return n
}

func TestCreateTemplateMarqueurEtProposition215(t *testing.T) {
	f := newFix215(t, "")
	v := f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": "tpl", "source": "template"})
	dir := filepath.Join(f.base, "TEST", "tpl")
	if v["tab"] != dir {
		t.Fatalf("create template = %v, attendu {tab:%s}", v, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("projet géré sans git init : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-launcher", "init-proposed")); err != nil {
		t.Fatalf("marqueur init-proposed absent : %v", err)
	}
	f.r.wait(t, 20*time.Second, "initProposal", func(e map[string]any) bool { return e["_name"] == "initProposal" && e["path"] == dir })
	// jamais lancé d'office : le faux claude n'a rien reçu
	time.Sleep(300 * time.Millisecond)
	if b, _ := os.ReadFile(f.in); strings.Contains(string(b), "init-project") {
		t.Errorf("/init-project tapé sans réponse de l'utilisateur : %q", b)
	}
	// une seule proposition par processus, même si l'état « ready » revient
	f.a.proposeInit(dir)
	f.a.proposeInit(dir)
	if n := countProposals215(f.r, dir); n != 1 {
		t.Errorf("%d évènements initProposal, attendu 1", n)
	}
	// « Plus tard » : plus de proposition, marqueur conservé pour le prochain lancement
	if v := dispatchJSON(t, f.a, "initProposalAnswer", map[string]any{"path": dir, "run": false}); v["ok"] != true {
		t.Errorf("initProposalAnswer(run:false) = %v", v)
	}
	f.a.proposeInit(dir)
	if n := countProposals215(f.r, dir); n != 1 {
		t.Errorf("proposition renouvelée après « Plus tard » : %d", n)
	}
	if !coreMarker215(dir) {
		t.Error("marqueur supprimé par « Plus tard »")
	}
	// « Lancer » : réponse acceptée et marqueur retiré (la frappe dans le leader : TestInitProposalLancerTapeDansLeLeader215)
	if v := dispatchJSON(t, f.a, "initProposalAnswer", map[string]any{"path": dir, "run": true}); v["ok"] != true {
		t.Fatalf("initProposalAnswer(run:true) = %v", v)
	}
	if coreMarker215(dir) {
		t.Error("marqueur non retiré après « Lancer »")
	}
	if v := dispatchJSON(t, f.a, "initProposalAnswer", map[string]any{"path": dir, "run": true}); v["ok"] != false {
		t.Errorf("seconde réponse sans proposition en attente = %v", v)
	}
}

// « Lancer /init-project » tape réellement la commande dans le leader. ÉCHEC CONNU au moment de l'écriture :
// initProposalAnswer lit projTerm.leader, champ jamais renseigné (le leader est dans lastLayout().Leader) ; input()
// ignore alors la saisie en silence (pane vide) et la réponse dit quand même ok:true en retirant le marqueur.
func TestInitProposalLancerTapeDansLeLeader215(t *testing.T) {
	f := newFix215(t, "")
	dir := filepath.Join(f.base, "TEST", "tpl2")
	if v := f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": "tpl2", "source": "template"}); v["tab"] != dir {
		t.Fatalf("create template = %v", v)
	}
	f.r.wait(t, 20*time.Second, "layout", isTerm(dir, "layout", nil))
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(f.in); err == nil { // le faux claude a ouvert son entrée
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if v := dispatchJSON(t, f.a, "initProposalAnswer", map[string]any{"path": dir, "run": true}); v["ok"] != true {
		t.Fatalf("initProposalAnswer(run:true) = %v", v)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(f.in); strings.Contains(string(b), "/init-project") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	b, _ := os.ReadFile(f.in)
	t.Errorf("/init-project non tapé dans le leader (reçu %q) alors que la réponse était ok:true et le marqueur retiré", b)
}

func TestCreateEmptyNePoseAucunMarqueur215(t *testing.T) {
	f := newFix215(t, "")
	dir := filepath.Join(f.base, "TEST", "vide")
	v := f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": "vide"})
	if v["tab"] != dir {
		t.Fatalf("create = %v", v)
	}
	if coreMarker215(dir) {
		t.Error("marqueur init-proposed posé pour une source « empty »")
	}
	time.Sleep(300 * time.Millisecond)
	if n := countProposals215(f.r, dir); n != 0 {
		t.Errorf("%d proposition(s) pour une source « empty »", n)
	}
	if _, err := os.Stat(f.rec); err == nil {
		t.Error("git clone lancé pour une source « empty »")
	}
}

func coreMarker215(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-launcher", "init-proposed"))
	return err == nil
}
