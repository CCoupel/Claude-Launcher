package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #255 (test-writer) — résolution login → identifiant numérique GitHub (contracts/user-backends.md §2). Écrits depuis le
// contrat : API GitHub factice, dossier de config temporaire, aucun réseau réel.

type gh255 struct {
	ids  map[string]int64
	mu   sync.Mutex
	hits atomic.Int64
	// mode : "" normal ; "500" ; "autre" (répond pour un autre login) ; "lent" (dépasse ghidTimeout)
	mode string
}

func (g *gh255) set(login string, id int64) { g.mu.Lock(); g.ids[login] = id; g.mu.Unlock() }
func (g *gh255) setMode(m string)           { g.mu.Lock(); g.mode = m; g.mu.Unlock() }

func gh255Start(t *testing.T, ids map[string]int64) *gh255 {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	g := &gh255{ids: ids}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		login := strings.TrimPrefix(r.URL.Path, "/")
		g.mu.Lock()
		id, ok := g.ids[login]
		mode := g.mode
		g.mu.Unlock()
		switch {
		case mode == "500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case mode == "lent":
			time.Sleep(1500 * time.Millisecond)
			fmt.Fprintf(w, `{"id":%d,"login":%q}`, id, login)
		case mode == "autre":
			fmt.Fprintf(w, `{"id":%d,"login":"someone-else"}`, id)
		case !ok:
			http.NotFound(w, r)
		default:
			fmt.Fprintf(w, `{"id":%d,"login":%q}`, id, login)
		}
	}))
	t.Cleanup(srv.Close)
	old := GithubUsersURL
	GithubUsersURL = srv.URL + "/"
	t.Cleanup(func() { GithubUsersURL = old })
	return g
}

func gh255Index(t *testing.T) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(UserIndexPath())
	if err != nil {
		t.Fatalf("users/index.json : %v", err)
	}
	var f struct {
		Users map[string]map[string]any `json:"users"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("users/index.json invalide : %v", err)
	}
	return f.Users
}

func TestGhID255IndexJSONContrat(t *testing.T) {
	gh255Start(t, map[string]int64{"alice": 1001})
	if uid, err := ResolveUID("Alice"); err != nil || uid != "github-1001" {
		t.Fatalf("ResolveUID = %q, %v", uid, err)
	}
	e := gh255Index(t)["alice"] // login en minuscules, valeur : identifiant et date, rien d'autre
	if e == nil || len(e) != 2 || e["id"] == nil || e["at"] == nil {
		t.Errorf("entrée de users/index.json hors contrat : %v", e)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(UserIndexPath()); st == nil || st.Mode().Perm() != 0o600 {
			t.Errorf("droits de index.json : %v (0600 attendu)", st.Mode().Perm())
		}
	}
}

func TestGhID255LoginsInvalidesSansAppelAPI(t *testing.T) {
	g := gh255Start(t, map[string]int64{})
	for _, bad := range []string{"", "..", "a/b", "a b", "alice@local", "-x", strings.Repeat("a", 40)} {
		if uid, err := ResolveUID(bad); err == nil || uid != "" {
			t.Errorf("login %q accepté : %q", bad, uid)
		}
	}
	if g.hits.Load() != 0 {
		t.Errorf("%d appel(s) API pour des logins invalides", g.hits.Load())
	}
}

func TestGhID255LoginReprisLaisseLeCacheIntact(t *testing.T) {
	g := gh255Start(t, map[string]int64{"alice": 5})
	if _, err := ResolveUID("alice"); err != nil {
		t.Fatal(err)
	}
	defer ghidTTL.Set(ghidTTL.Set(time.Nanosecond))
	g.set("alice", 6) // le login est repris par un autre compte
	time.Sleep(2 * time.Millisecond)
	for i := 0; i < 3; i++ {
		if uid, err := ResolveUID("alice"); !errors.Is(err, ErrLoginReused) || uid != "" {
			t.Fatalf("essai %d : %q, %v (ErrLoginReused attendu, aucun espace rendu)", i, uid, err)
		}
	}
	if id := gh255Index(t)["alice"]["id"]; id != float64(5) {
		t.Errorf("le cache a été modifié par un login repris : %v", id)
	}
}

func TestGhID255ReponseDunAutreLoginRefusee(t *testing.T) {
	g := gh255Start(t, map[string]int64{"alice": 5})
	g.setMode("autre")
	if uid, err := ResolveUID("alice"); !errors.Is(err, ErrLoginUnresolved) || uid != "" {
		t.Errorf("réponse pour un autre login : %q, %v", uid, err)
	}
	if _, err := os.Stat(UserIndexPath()); err == nil {
		if _, ok := gh255Index(t)["alice"]; ok {
			t.Error("entrée écrite à partir d'une réponse inattendue")
		}
	}
}

func TestGhID255ErreursReseau(t *testing.T) {
	g := gh255Start(t, map[string]int64{"alice": 5})
	defer ghidRetry.Set(ghidRetry.Set(0)) // pas d'attente de 5 min entre deux essais : chaque étape rejoue l'API (revue LOT-2)
	// sans entrée connue : erreur franche, rien d'écrit
	g.setMode("500")
	if uid, err := ResolveUID("alice"); !errors.Is(err, ErrLoginUnresolved) || uid != "" {
		t.Errorf("HTTP 500 sans entrée : %q, %v", uid, err)
	}
	// API trop lente : abandonnée au délai (point d'injection ghidTimeout)
	g.setMode("lent")
	defer ghidTimeout.Set(ghidTimeout.Set(150 * time.Millisecond))
	t0 := time.Now()
	if _, err := ResolveUID("alice"); !errors.Is(err, ErrLoginUnresolved) {
		t.Errorf("API lente : %v", err)
	}
	if d := time.Since(t0); d > time.Second {
		t.Errorf("le délai n'est pas respecté : %v", d)
	}
	// avec une entrée connue (même périmée) : l'identifiant connu sert, API en panne
	g.setMode("")
	if _, err := ResolveUID("alice"); err != nil {
		t.Fatal(err)
	}
	defer ghidTTL.Set(ghidTTL.Set(time.Nanosecond))
	time.Sleep(2 * time.Millisecond)
	g.setMode("500")
	if uid, err := ResolveUID("alice"); err != nil || uid != "github-5" {
		t.Errorf("API en panne, entrée connue : %q, %v", uid, err)
	}
}

func TestGhID255UnSeulAppelEnConcurrence(t *testing.T) {
	g := gh255Start(t, map[string]int64{"alice": 5})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if uid, err := ResolveUID("alice"); err != nil || uid != "github-5" {
				t.Errorf("ResolveUID = %q, %v", uid, err)
			}
		}()
	}
	wg.Wait()
	if g.hits.Load() != 1 {
		t.Errorf("%d appels API pour 20 résolutions simultanées d'un même login (1 attendu)", g.hits.Load())
	}
}

func TestGhID255LoginRenommeGardeSonEspace(t *testing.T) {
	gh255Start(t, map[string]int64{"alice": 5, "alice-2": 5, "bob": 6})
	a, _ := ResolveUID("alice")
	a2, _ := ResolveUID("alice-2") // même compte GitHub sous un nouveau login : même espace
	b, _ := ResolveUID("bob")
	if a != "github-5" || a2 != a || b != "github-6" || a == b {
		t.Errorf("espaces : alice %q, alice-2 %q, bob %q", a, a2, b)
	}
}

// userList (admin) : jamais d'identifiant numérique ni d'espace brut dans ce que l'on montre.
func TestGhID255ListUsersSansIdentifiant(t *testing.T) {
	gh255Start(t, map[string]int64{"alice": 1234, "bob": 5678})
	_, _ = ResolveUID("alice")
	_, _ = ResolveUID("bob")
	b, _ := json.Marshal(ListUsers())
	if s := string(b); !strings.Contains(s, "alice") || !strings.Contains(s, "bob") || strings.Contains(s, "1234") || strings.Contains(s, "5678") || strings.Contains(s, "github-") {
		t.Errorf("ListUsers = %s", s)
	}
}
