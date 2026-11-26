package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// #255 (test-writer) — état par utilisateur users/<uid>/ (contracts/user-backends.md §2) : fichiers du contrat seulement,
// droits, écriture atomique et verrou, trace minimale sans donnée projet, isolement entre espaces, admin = espace actuel.

func us255(t *testing.T, uid string) Space {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, err := UserSpace(uid)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

const us255Backends = "backends:\n  k:\n    type: k8s\n    namespace: ns\n"

func TestUserSpace255SeulementLesFichiersDuContratEtLesDroits(t *testing.T) {
	sp := us255(t, "github-5")
	if _, err := sp.SaveBackends([]byte(us255Backends), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := sp.SaveSettings(UserSettings{ClaudeOptions: "--x"}); err != nil {
		t.Fatal(err)
	}
	if err := sp.RecordTrace(Trace{Backend: "k", Kind: "k8s", Object: "o", Project: "p", Category: "c"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	ents, _ := os.ReadDir(sp.Dir())
	for _, e := range ents {
		n := e.Name()
		if strings.HasSuffix(n, ".lock") || n == "backends.yaml.bak" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "backends.yaml,config.json,traces.json,vault" {
		t.Errorf("contenu de users/<uid>/ = %s (contrat : backends.yaml, vault, config.json, traces.json)", got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for p, want := range map[string]os.FileMode{sp.Dir(): 0o700, sp.VaultDir(): 0o700, sp.SettingsPath(): 0o600, sp.TracesPath(): 0o600} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != want {
			t.Errorf("%s : droits %v, %v attendu", p, st.Mode().Perm(), want)
		}
	}
}

// La trace ne porte rien d'autre que le schéma du contrat (jamais branche, état git, chemin de fichier, dépôt).
func TestUserSpace255TraceSansDonneeProjet(t *testing.T) {
	sp := us255(t, "github-5")
	if err := sp.RecordTrace(Trace{Backend: "k", Kind: "k8s", Object: "sts-p", Project: "p", Category: "c"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sp.TracesPath())
	var f struct {
		Traces []map[string]any `json:"traces"`
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Traces) != 1 {
		t.Fatalf("traces.json : %v %s", err, b)
	}
	var keys []string
	for k := range f.Traces[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "backend,category,first_seen,kind,last_seen,object,project" {
		t.Errorf("champs de la trace : %s", got)
	}
}

func TestUserSpace255EcrituresConcurrentesSansPerte(t *testing.T) {
	sp := us255(t, "github-5")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := sp.RecordTrace(Trace{Backend: "k", Kind: "k8s", Object: fmt.Sprintf("o%d", i), Project: fmt.Sprintf("p%02d", i), Category: "c"}); err != nil {
				t.Errorf("RecordTrace %d : %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if ts, err := sp.LoadTraces(); err != nil || len(ts) != 20 {
		t.Errorf("%d trace(s) après 20 écritures simultanées (%v) : verrou ou atomicité en défaut", len(ts), err)
	}
}

// Un lecteur ne voit jamais un config.json à moitié écrit.
func TestUserSpace255EcritureAtomique(t *testing.T) {
	sp := us255(t, "github-5")
	if err := sp.SaveSettings(UserSettings{ClaudeOptions: "init"}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = sp.SaveSettings(UserSettings{ClaudeOptions: strings.Repeat("x", 100+i%50), Display: map[string]any{"w": w}})
			}
		}(w)
	}
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := sp.LoadSettings(); err != nil {
			t.Errorf("lecture d'un config.json en cours d'écriture : %v", err)
			break
		}
	}
	close(stop)
	wg.Wait()
}

func TestUserSpace255IsolementEntreEspaces(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	a, _ := UserSpace("github-1")
	b, _ := UserSpace("github-2")
	if _, err := a.SaveBackends([]byte(us255Backends), time.Time{}); err != nil {
		t.Fatal(err)
	}
	_ = a.SaveSettings(UserSettings{ClaudeOptions: "secret-a"})
	_ = a.RecordTrace(Trace{Backend: "k", Kind: "k8s", Object: "o", Project: "p", Category: "c"})
	if bs, _ := b.LoadBackends(); len(bs) != 0 {
		t.Errorf("b voit les backends de a : %v", bs)
	}
	if st, _ := b.LoadSettings(); st.ClaudeOptions != "" {
		t.Errorf("b voit les réglages de a : %+v", st)
	}
	if ts, _ := b.LoadTraces(); len(ts) != 0 {
		t.Errorf("b voit les traces de a : %v", ts)
	}
	if a.Dir() == b.Dir() || strings.HasPrefix(a.Dir(), b.Dir()+string(filepath.Separator)) {
		t.Errorf("espaces imbriqués ou confondus : %s %s", a.Dir(), b.Dir())
	}
	// purger chez b ne touche pas a
	if n, _ := b.PurgeTrace("k", "p"); n != 0 {
		t.Errorf("b a purgé %d trace(s) de a", n)
	}
	if ts, _ := a.LoadTraces(); len(ts) != 1 {
		t.Errorf("traces de a après purge chez b : %v", ts)
	}
}

func TestUserSpace255UidsInvalidesRefuses(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	for _, bad := range []string{"", "admin", "..", "../admin", "github-", "github-0", "github-1/..", "github-1\x00", "GITHUB-1", "github- 1"} {
		if _, err := UserSpace(bad); err == nil {
			t.Errorf("uid %q accepté", bad)
		}
	}
}

// Admin = l'espace actuel, sans migration : mêmes chemins qu'en v7.0.0, rien créé sous users/.
func TestUserSpace255AdminEstLEspaceActuel(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, err := SpaceOf(Identity{Login: "ccoupel", Role: RoleAdmin})
	if err != nil || !sp.Admin {
		t.Fatalf("SpaceOf(admin) = %+v, %v", sp, err)
	}
	local, _ := SpaceOf(Identity{Login: LocalAdminLogin, Role: RoleAdmin})
	if !local.Admin || sp.BackendsPath() != BackendsPath() || sp.Dir() != Ng2ConfigDir() || local.BackendsPath() != BackendsPath() {
		t.Errorf("l'admin n'a pas l'espace actuel : %s ≠ %s", sp.BackendsPath(), BackendsPath())
	}
	if err := sp.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(UsersDir()); err == nil {
		t.Error("Ensure de l'admin a créé users/")
	}
}

// Un utilisateur ne reçoit jamais le « host » implicite ni les moteurs locaux détectés (règle n°2 : pas de calcul plateforme).
func TestUserSpace255SansHostNiMoteursLocaux(t *testing.T) {
	sp := us255(t, "github-5")
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) != 0 {
		t.Fatalf("espace vide : %v, %v", bs, err)
	}
	if _, err := sp.SaveBackends([]byte(us255Backends), time.Time{}); err != nil {
		t.Fatal(err)
	}
	bs, _ = sp.LoadBackends()
	for _, b := range bs {
		if b.Type == "host" || b.Name == "host" {
			t.Errorf("backend local rendu à un utilisateur : %+v", b)
		}
	}
}

func TestUserSpace255ReglagesSansSecretEtTailleBornee(t *testing.T) {
	sp := us255(t, "github-5")
	if err := sp.SaveSettings(UserSettings{GithubToken: "pat", ClaudeOptions: "--model x"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sp.SettingsPath())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for k := range m {
		if k != "display" && k != "claude_options" && k != "github_token" {
			t.Errorf("clé hors contrat dans config.json : %s", k)
		}
	}
	if err := sp.SaveSettings(UserSettings{ClaudeOptions: strings.Repeat("x", 70<<10)}); err == nil {
		t.Error("réglages de 70 Kio acceptés")
	}
	if st, _ := sp.LoadSettings(); st.ClaudeOptions != "--model x" {
		t.Errorf("l'échec a écrasé les réglages : %+v", st)
	}
}

// userList : état « sans backend » / « ok » d'après le backends.yaml de chaque espace.
func TestUserSpace255ListUsersEtats(t *testing.T) {
	gh255Start(t, map[string]int64{"alice": 1, "bob": 2})
	ua, _ := ResolveUID("alice")
	_, _ = ResolveUID("bob")
	sa, _ := UserSpace(ua)
	if _, err := sa.SaveBackends([]byte(us255Backends), time.Time{}); err != nil {
		t.Fatal(err)
	}
	got := map[string]UserSummary{}
	for _, u := range ListUsers() {
		got[u.Login] = u
	}
	if got["alice"].State != "ok" || got["alice"].Backends != 1 || got["bob"].State != "sans backend" || got["bob"].Backends != 0 {
		t.Errorf("ListUsers = %+v", got)
	}
}
