package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// LOT-3.2 jalon 4 (test-writer) — Ouvrir / approbation scellée / terminal / Fermer d'un projet d'UTILISATEUR par l'API web (RunWeb, mode
// proxy). Écrits depuis le contrat (§5, §6, §6 bis, §9) et le handoff de dev-backend (formes : open{path} → {tab}, évènement « envTrust »,
// envTrustAnswer{path,choice,sha}, close/forceClose, refreshProject). Faux docker À ÉTAT (conteneurs, fichiers du projet) qui relaie les
// « exec … tmux » vers un vrai tmux sur une socket privée ; aucun vrai projet, canaris CANARY-….

type ut4 struct {
	*ub257
	files string // fichiers du projet (env.yaml, trust.json…)
	state string // état des conteneurs
	sock  string
}

func ut4Start(t *testing.T) *ut4 {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	// canaris : rien du launcher / de l'admin ne doit atteindre un conteneur d'utilisateur
	for k, v := range map[string]string{"GH_TOKEN": "CANARY-gh-1", "GITHUB_TOKEN": "CANARY-gh-2", "ANTHROPIC_API_KEY": "CANARY-ant-3", "CLAUDE_CODE_OAUTH_TOKEN": "CANARY-oauth-4",
		"CL_DESKTOP_TOKEN": "CANARY-tok-5", "CLAUDE_OPTIONS": "--dangerously CANARY-opt-6"} {
		t.Setenv(k, v)
	}
	u := &ut4{ub257: ub257Start(t)}
	u.sock = fmt.Sprintf("cl-t4-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", u.sock, "kill-server").Run() })
	u.files = filepath.Join(u.dir, "files")
	_ = os.MkdirAll(u.files, 0o755)
	u.state = filepath.Join(u.dir, "state")
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2
echo "ARGV: $*" >> "` + u.dir + `/calls.log"
env | grep CANARY | sed 's/^/ENV: /' >> "` + u.dir + `/calls.log"
S="` + u.state + `"; F="` + u.files + `"; touch "$S"
prev=""; prev2=""
for a in "$@"; do prev2="$prev"; prev="$a"; done
case "$1" in
  create)
    name=""; labels=""
    for a in "$@"; do case "$a" in --name=*) name="${a#--name=}" ;; --label=*) labels="$labels,${a#--label=}" ;; esac; done
    echo "$name|created|img|${labels#,}" >> "$S"; echo id ;;
  ps) case "$*" in *State*) cat "$S" ;; *) awk -F"|" '{print $1" "$4}' "$S" ;; esac ;;
  start) sed -i "s/^$2|created|/$2|running|/; s/^$2|exited|/$2|running|/" "$S" ;;
  stop) sed -i "s/^$2|running|/$2|exited|/" "$S" ;;
  inspect) shift; [ "$1" = "-f" ] && { n="$3"; f="$2"; } || n="$1"; grep -q "^$n|" "$S" || exit 1
    case "$f" in *Labels*) grep "^$n|" "$S" | head -1 | tr ',' '\n' | grep '^cl.user=' | cut -d= -f2 ;; *) echo true ;; esac ;;
  exec)
    case "$*" in
      *" tmux "*)
        args=""; seen=0; skip=0
        for a in "$@"; do
          if [ $seen -eq 0 ]; then [ "$a" = tmux ] && seen=1; continue; fi
          if [ $skip -eq 1 ]; then skip=0; continue; fi
          if [ $skip -eq 2 ]; then skip=0; args="$args ` + u.sock + `"; continue; fi
          case "$a" in -f) skip=1; continue ;; -L) skip=2 ;; esac
          args="$args $a"
        done
        eval "exec env -u TMUX tmux $args" ;;
      *tee*) cat > "$F/$(basename "$prev")" ;;
      *"head -c"*) f="$F/$(basename "$prev2")"; [ -f "$f" ] && cat "$f" || exit 1 ;;
      *) : ;;
    esac ;;
  *) : ;;
esac
`
	if err := os.WriteFile(filepath.Join(u.dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	ub257DeclareDocker2(t, "github-2222", "e", "tcp://93.184.216.34:2376")
	return u
}

func ub257DeclareDocker2(t *testing.T, uid, name, host string) {
	ub257DeclareDocker(t, uid, name, host, "")
}

func (u *ut4) do(t *testing.T, user, m string, a any) map[string]any {
	t.Helper()
	_, res, e := u.call(t, user, m, a)
	if e != "" {
		return map[string]any{"error": e}
	}
	return res
}

func (u *ut4) callsText() string { return u.calls() }

// stream : flux SSE d'un utilisateur ; get() rend les évènements reçus (name → données brutes).
type sse struct {
	mu  sync.Mutex
	evs []string
}

func (u *ut4) stream(t *testing.T, user string) (*sse, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+fmt.Sprint(u.s.port)+"/api/events", nil)
	req.Host = px172Ext
	req.Header.Set("X-Forwarded-User", user)
	req.Header.Set("X-Forwarded-For", "192.168.1.20")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := &sse{}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				s.mu.Lock()
				s.evs = append(s.evs, strings.TrimPrefix(l, "data: "))
				s.mu.Unlock()
			}
		}
	}()
	return s, cancel
}

func (s *sse) named(name string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, e := range s.evs {
		var m map[string]any
		if json.Unmarshal([]byte(e), &m) == nil && m["name"] == name {
			out = append(out, m)
		}
	}
	return out
}

func (s *sse) wait(name string, d time.Duration) []map[string]any {
	dl := time.Now().Add(d)
	for time.Now().Before(dl) {
		if r := s.named(name); len(r) > 0 {
			return r
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func (u *ut4) tabState(t *testing.T, user, path string) string {
	t.Helper()
	for _, tb := range u.do(t, user, "init", map[string]any{})["state"].(map[string]any)["tabs"].([]any) {
		if m := tb.(map[string]any); m["path"] == path {
			return fmt.Sprint(m["state"])
		}
	}
	return ""
}

func (u *ut4) waitTab(t *testing.T, user, path, want string, d time.Duration) string {
	dl := time.Now().Add(d)
	s := u.tabState(t, user, path)
	for s != want && time.Now().Before(dl) {
		time.Sleep(150 * time.Millisecond)
		s = u.tabState(t, user, path)
	}
	return s
}

const p4 = "d/cat/proj"

func (u *ut4) create(t *testing.T) {
	t.Helper()
	if r := u.do(t, "alice", "create", map[string]any{"backend": "d", "category": "cat", "name": "proj"}); r["ok"] != true {
		t.Fatalf("create : %v", r)
	}
}

func (u *ut4) noSession() bool {
	c := u.calls()
	return !strings.Contains(c, "new-session") && !strings.Contains(c, "send-keys")
}

// ── ouverture, tous les exec sous l'uid du traitement ────────────────────────────────────────────────────

func TestUT4OuvertureSousLUidEtSansDonneesDuLauncher(t *testing.T) {
	u := ut4Start(t)
	u.create(t)
	if r := u.do(t, "alice", "open", map[string]any{"path": p4}); r["tab"] != p4 {
		t.Fatalf("open : %v", r)
	}
	if s := u.waitTab(t, "alice", p4, "ready", 40*time.Second); s != "ready" {
		t.Fatalf("onglet %q (ready attendu) :\n%s", s, u.calls())
	}
	execs := 0
	for _, c := range strings.Split(u.calls(), "\n") {
		if !strings.HasPrefix(c, "ARGV: exec ") {
			continue
		}
		execs++
		if !strings.Contains(c, "--user=2000:2000") {
			t.Errorf("exec hors uid du traitement : %s", c)
		}
		for _, bad := range []string{"-u 0", "--user=0", "--user 0", "--user=root", " -u root"} {
			if strings.Contains(c, bad) {
				t.Errorf("exec en root (%s) : %s", bad, c)
			}
		}
		// variables transmises au conteneur : seulement celles du launcher pour un utilisateur
		f := strings.Fields(c)
		for i, a := range f {
			if (a == "-e" || a == "--env") && i+1 < len(f) {
				k := strings.SplitN(f[i+1], "=", 2)[0]
				if !map[string]bool{"HOME": true, "CLAUDE_CONFIG_DIR": true, "XDG_CONFIG_HOME": true, "TERM": true, "LANG": true}[k] {
					t.Errorf("variable d'environnement hors liste dans un exec : %s", f[i+1])
				}
			}
		}
	}
	if execs == 0 {
		t.Fatal("aucun exec journalisé")
	}
	// canari : aucune donnée du launcher / de l'admin (jeton, CLAUDE_OPTIONS, clés) dans une commande ni dans l'environnement du client docker transmis
	for _, l := range strings.Split(u.calls(), "\n") {
		if strings.Contains(l, "CANARY") && strings.HasPrefix(l, "ARGV:") {
			t.Errorf("donnée du launcher dans une commande : %s", l)
		}
	}
	if strings.Contains(u.calls(), "/etc/cl-tmux.conf") {
		t.Error("configuration tmux de l'image/de l'admin utilisée")
	}
}

// ── approbation scellée de l'env.yaml ──────────────────────────────────────────────────────────────────

func TestUT4ApprobationManquanteAcceptationEtChangement(t *testing.T) {
	u := ut4Start(t)
	u.create(t)
	env := "image: " + core.UserDefaultImage() + "\nsetup:\n  - echo bonjour\n"
	_ = os.WriteFile(filepath.Join(u.files, "env.yaml"), []byte(env), 0o644)
	alice, ca := u.stream(t, "alice")
	bob, cb := u.stream(t, "bob")
	defer ca()
	defer cb()
	_ = u.do(t, "alice", "open", map[string]any{"path": p4})
	evs := alice.wait("envTrust", 20*time.Second)
	if len(evs) == 0 {
		t.Fatalf("aucune question d'approbation :\n%s", u.calls())
	}
	data, _ := evs[0]["data"].(map[string]any)
	sha, _ := data["sha"].(string)
	if data["path"] != p4 || sha == "" || data["changed"] == true {
		t.Fatalf("question : %v", data)
	}
	if !u.noSession() {
		t.Errorf("session démarrée avant la réponse :\n%s", u.calls())
	}
	if len(bob.named("envTrust")) != 0 {
		t.Error("la question d'approbation d'alice est parvenue à bob")
	}
	if u.waitTab(t, "alice", p4, "ready", 2*time.Second) == "ready" {
		t.Error("onglet prêt sans approbation")
	}
	// bob ne peut pas répondre pour alice
	if r := u.do(t, "bob", "envTrustAnswer", map[string]any{"path": p4, "choice": "accept", "sha": sha}); r["ok"] == true {
		t.Errorf("bob a approuvé l'environnement d'alice : %v", r)
	}
	if _, err := os.Stat(filepath.Join(u.files, "trust.json")); err == nil {
		t.Fatal("trust.json écrit par la réponse de bob")
	}
	// annuler : rien ne démarre, rien n'est écrit
	u.do(t, "alice", "envTrustAnswer", map[string]any{"path": p4, "choice": "cancel", "sha": sha})
	time.Sleep(500 * time.Millisecond)
	if !u.noSession() || u.tabState(t, "alice", p4) == "ready" {
		t.Errorf("annulation : session ou onglet créé :\n%s", u.calls())
	}
	if _, err := os.Stat(filepath.Join(u.files, "trust.json")); err == nil {
		t.Error("trust.json écrit après annulation")
	}
	// accepter avec un condensé qui ne correspond pas aux fichiers : refusé
	_ = u.do(t, "alice", "open", map[string]any{"path": p4})
	alice.wait("envTrust", 5*time.Second)
	if r := u.do(t, "alice", "envTrustAnswer", map[string]any{"path": p4, "choice": "accept", "sha": strings.Repeat("0", 64)}); r["ok"] == true {
		t.Errorf("approbation acceptée avec un mauvais condensé : %v", r)
	}
	if _, err := os.Stat(filepath.Join(u.files, "trust.json")); err == nil {
		t.Error("trust.json écrit pour un mauvais condensé")
	}
	// une réponse erronée ne laisse pas de question à moitié ouverte : on rouvre, puis on accepte
	n0 := len(alice.named("envTrust"))
	_ = u.do(t, "alice", "open", map[string]any{"path": p4})
	for dl := time.Now().Add(15 * time.Second); len(alice.named("envTrust")) == n0 && time.Now().Before(dl); {
		time.Sleep(100 * time.Millisecond)
	}
	// accepter : trust.json scellé (MAC) dans le volume, session ouverte
	if r := u.do(t, "alice", "envTrustAnswer", map[string]any{"path": p4, "choice": "accept", "sha": sha}); r["error"] != nil {
		t.Fatalf("acceptation : %v", r)
	}
	if s := u.waitTab(t, "alice", p4, "ready", 40*time.Second); s != "ready" {
		t.Fatalf("après acceptation : onglet %q :\n%s", s, u.calls())
	}
	b, err := os.ReadFile(filepath.Join(u.files, "trust.json"))
	if err != nil || !strings.Contains(string(b), `"mac"`) || strings.Contains(string(b), "CANARY") {
		t.Errorf("trust.json : %v %.200s", err, b)
	}
	// forgé par le dépôt (pas de clé) : redemandé ; contenu modifié ensuite : redemandé avec changed
	u.do(t, "alice", "close", map[string]any{"path": p4})
	_ = os.WriteFile(filepath.Join(u.files, "trust.json"), []byte(`{"files":{"env.yaml":"x"},"mac":"deadbeef"}`), 0o644)
	n := len(alice.named("envTrust"))
	u.do(t, "alice", "open", map[string]any{"path": p4})
	dl := time.Now().Add(15 * time.Second)
	for len(alice.named("envTrust")) == n && time.Now().Before(dl) {
		time.Sleep(100 * time.Millisecond)
	}
	if len(alice.named("envTrust")) == n {
		t.Error("trust.json forgé accepté (approbation non redemandée)")
	}
}

// ── isolement entre deux utilisateurs ───────────────────────────────────────────────────────────────

func TestUT4IsolementDesOngletsEtDuTerminal(t *testing.T) {
	u := ut4Start(t)
	u.create(t)
	bob, cb := u.stream(t, "bob")
	defer cb()
	u.do(t, "alice", "open", map[string]any{"path": p4})
	if s := u.waitTab(t, "alice", p4, "ready", 40*time.Second); s != "ready" {
		t.Fatalf("onglet %q", s)
	}
	for _, m := range []string{"termInput", "termResize", "termFocus", "termZoom", "resizePane", "launchInput", "termSync", "close", "forceClose", "closeConfirm", "forceCloseConfirm", "open"} {
		r := u.do(t, "bob", m, map[string]any{"path": p4, "data": "echo CANARY-bob\n", "cols": 80, "rows": 24})
		if r["ok"] == true && m != "open" || r["tab"] == p4 {
			t.Errorf("bob : %s sur l'onglet d'alice accepté : %v", m, r)
		}
	}
	if s := u.tabState(t, "alice", p4); s != "ready" {
		t.Errorf("l'onglet d'alice a été touché par bob : %q", s)
	}
	if u.tabState(t, "bob", p4) != "" {
		t.Error("l'onglet d'alice apparaît dans l'état de bob")
	}
	if b, _ := os.ReadFile(u.state); !strings.Contains(string(b), "|running|") {
		t.Errorf("le conteneur d'alice a été arrêté par bob :\n%s", b)
	}
	time.Sleep(500 * time.Millisecond)
	for _, ev := range bob.named("term") {
		t.Errorf("évènement de terminal d'alice reçu par bob : %v", ev)
	}
	for _, e := range bob.evs {
		if strings.Contains(e, p4) && strings.Contains(e, "tab") {
			t.Errorf("évènement lié à l'onglet d'alice reçu par bob : %.200s", e)
		}
	}
}

// ── Fermer arrête ; le sondage ne relance JAMAIS (P20) ; F5 démarre sans session ───────────────────────

func TestUT4FermerSansRelanceEtF5SansSession(t *testing.T) {
	u := ut4Start(t)
	u.create(t)
	u.do(t, "alice", "open", map[string]any{"path": p4})
	if s := u.waitTab(t, "alice", p4, "ready", 40*time.Second); s != "ready" {
		t.Fatalf("onglet %q", s)
	}
	if r := u.do(t, "alice", "close", map[string]any{"path": p4}); r["ok"] != true {
		t.Fatalf("close : %v", r)
	}
	if b, _ := os.ReadFile(u.state); !strings.Contains(string(b), "|exited|") {
		t.Fatalf("conteneur non arrêté :\n%s", b)
	}
	mark := len(strings.Split(u.calls(), "\n"))
	// le sondage (rescan / refresh / init répétés pendant plusieurs cycles) ne démarre rien
	for i := 0; i < 6; i++ {
		for _, m := range []string{"rescan", "select", "cardsFor"} {
			u.do(t, "alice", m, map[string]any{"path": p4})
		}
		u.do(t, "alice", "init", map[string]any{})
		time.Sleep(250 * time.Millisecond)
	}
	for _, c := range strings.Split(u.calls(), "\n")[mark:] {
		if strings.HasPrefix(c, "ARGV: start") || strings.Contains(c, "scale") || strings.Contains(c, "new-session") {
			t.Errorf("relance d'un projet arrêté par un sondage : %s", c)
		}
	}
	if b, _ := os.ReadFile(u.state); strings.Contains(string(b), "|running|") {
		t.Errorf("projet relancé :\n%s", b)
	}
	// /files d'un projet arrêté : 404, aucune commande
	mark = len(strings.Split(u.calls(), "\n"))
	hdr := map[string]string{"X-Forwarded-User": "alice", "X-Forwarded-For": "192.168.1.20"}
	if r, _ := u.s.do("GET", "/files/proj/readme.md", px172Ext, hdr, ""); r == nil || r.StatusCode != 404 {
		t.Errorf("/files d'un projet arrêté : %v", r)
	}
	if len(strings.Split(u.calls(), "\n")) != mark {
		t.Errorf("commandes lancées par un /files de projet arrêté :\n%s", u.calls())
	}
	// F5 : démarre le conteneur, SANS session claude, relit l'état ; il reste en marche
	mark = len(strings.Split(u.calls(), "\n"))
	_ = exec.Command("tmux", "-L", u.sock, "kill-server").Run()
	r := u.do(t, "alice", "refreshProject", map[string]any{"path": p4})
	if r["ok"] != true || r["started"] != true {
		t.Fatalf("F5 : %v", r)
	}
	after := strings.Join(strings.Split(u.calls(), "\n")[mark:], "\n")
	if !strings.Contains(after, "ARGV: start") {
		t.Errorf("F5 n'a pas démarré le conteneur :\n%s", after)
	}
	for _, bad := range []string{"new-session", "send-keys", core.UserStartCommand("cat", "proj")} {
		if strings.Contains(after, bad) {
			t.Errorf("F5 : « %s » (aucune session claude à l'ouverture d'un F5) :\n%s", bad, after)
		}
	}
	if b, _ := os.ReadFile(u.state); !strings.Contains(string(b), "|running|") {
		t.Error("le conteneur ne reste pas en marche après F5")
	}
}

// Ouvrir un projet qui n'existe pas dans l'espace de l'utilisateur est REFUSÉ : « open » ne crée jamais (la création passe par « create » :
// validation des noms, limite de projets, options). Chemin hostile ou d'un backend étranger : refusé, aucune commande de création.
func TestUT4OpenNeCreeJamais(t *testing.T) {
	u := ut4Start(t)
	u.create(t) // alice a un projet : d/cat/proj
	mark := len(strings.Split(u.calls(), "\n"))
	for _, p := range []string{"e/cat/fantome", "d/cat/fantome", "d/cat/proj-old-1", "d/cat/../x", "d/../cat/proj", "d/cat/a b", "d/cat/$(id)", "../d/cat/proj", "inconnu/cat/proj", "d/cat", "d", "", "d/cat/proj/extra", "d/Cat/Proj"} {
		r := u.do(t, "bob", "open", map[string]any{"path": p})
		if r["tab"] != nil && r["tab"] != "" {
			t.Errorf("bob : open %q accepté : %v", p, r)
		}
		r = u.do(t, "alice", "open", map[string]any{"path": p})
		if p != "d/cat/proj" && r["tab"] != nil && r["tab"] != "" {
			t.Errorf("alice : open %q (projet inexistant ou chemin hostile) accepté : %v", p, r)
		}
	}
	for _, c := range strings.Split(u.calls(), "\n")[mark:] {
		if strings.HasPrefix(c, "ARGV: create") && !strings.Contains(c, "cl-"+u.h8("github-1111")+"-cat-proj") {
			t.Errorf("open a créé un objet : %s", c)
		}
	}
}

func (u *ut4) h8(uid string) string {
	return strings.TrimPrefix(strings.SplitN(core.UserObjName(uid, "cat", "proj"), "-cat-", 2)[0], "cl-")
}
