package core

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// #255 (dev) : identifiant GitHub, espace par utilisateur. Dossiers temporaires, API GitHub factice.

func dev255Env(t *testing.T) {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
}

// dev255API : API factice ; ids : login → identifiant ; hits compte les appels.
func dev255API(t *testing.T, ids map[string]*atomic.Int64) *atomic.Int64 {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		login := strings.TrimPrefix(r.URL.Path, "/")
		id, ok := ids[login]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"id":%d,"login":%q}`, id.Load(), login)
	}))
	t.Cleanup(srv.Close)
	old := GithubUsersURL
	GithubUsersURL = srv.URL + "/"
	t.Cleanup(func() { GithubUsersURL = old })
	return &hits
}

func TestResolveUIDCacheEtLoginRepris255(t *testing.T) {
	dev255Env(t)
	id := &atomic.Int64{}
	id.Store(42)
	hits := dev255API(t, map[string]*atomic.Int64{"alice": id})
	uid, err := ResolveUID("Alice")
	if err != nil || uid != "github-42" {
		t.Fatalf("ResolveUID = %q, %v", uid, err)
	}
	if uid, _ = ResolveUID("alice"); uid != "github-42" || hits.Load() != 1 {
		t.Errorf("le cache doit servir : uid %q, %d appel(s) API", uid, hits.Load())
	}
	// entrée périmée, même compte : revérifiée, acceptée
	defer ghidTTL.Set(ghidTTL.Set(time.Nanosecond))
	time.Sleep(2 * time.Millisecond)
	if uid, err = ResolveUID("alice"); err != nil || uid != "github-42" || hits.Load() != 2 {
		t.Fatalf("revérification : %q, %v, %d appel(s)", uid, err, hits.Load())
	}
	// le login est repris par un autre compte : refusé, jusqu'à l'action de l'admin
	id.Store(99)
	time.Sleep(2 * time.Millisecond)
	if _, err = ResolveUID("alice"); !errors.Is(err, ErrLoginReused) {
		t.Fatalf("login repris : %v, ErrLoginReused attendu", err)
	}
	if _, err = ResolveUID("alice"); !errors.Is(err, ErrLoginReused) {
		t.Errorf("le refus doit durer : %v", err)
	}
	if err := ForgetUserLogin("alice"); err != nil {
		t.Fatal(err)
	}
	if uid, err = ResolveUID("alice"); err != nil || uid != "github-99" {
		t.Errorf("après l'action de l'admin : %q, %v", uid, err)
	}
}

func TestResolveUIDInconnuEtApiHorsLigne255(t *testing.T) {
	dev255Env(t)
	hits := dev255API(t, map[string]*atomic.Int64{})
	if _, err := ResolveUID("nobody"); !errors.Is(err, ErrLoginUnresolved) {
		t.Errorf("login inconnu : %v", err)
	}
	if _, err := ResolveUID("../etc"); err == nil {
		t.Error("login invalide accepté")
	}
	if hits.Load() != 1 {
		t.Errorf("login invalide : aucun appel API attendu (%d)", hits.Load())
	}
	// API hors ligne mais entrée connue (périmée) : l'identifiant connu sert
	id := &atomic.Int64{}
	id.Store(7)
	dev255API(t, map[string]*atomic.Int64{"bob": id})
	if uid, err := ResolveUID("bob"); err != nil || uid != "github-7" {
		t.Fatal(uid, err)
	}
	GithubUsersURL = "http://127.0.0.1:1/"
	defer ghidTTL.Set(ghidTTL.Set(time.Nanosecond))
	time.Sleep(2 * time.Millisecond)
	if uid, err := ResolveUID("bob"); err != nil || uid != "github-7" {
		t.Errorf("API hors ligne : %q, %v", uid, err)
	}
}

func TestSpaceOfEtChemins255(t *testing.T) {
	dev255Env(t)
	if sp, err := SpaceOf(Identity{Login: "root", Role: RoleAdmin}); err != nil || !sp.Admin || sp.BackendsPath() != BackendsPath() {
		t.Errorf("admin : %+v, %v", sp, err)
	}
	for _, bad := range []string{"", "..", "github-1/../x", "github-0", "github-01", "admin", "github-1x"} {
		if _, err := UserSpace(bad); err == nil {
			t.Errorf("espace %q accepté", bad)
		}
	}
	sp, err := UserSpace("github-5")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Dir() != filepath.Join(UsersDir(), "github-5") || sp.BackendsPath() != filepath.Join(sp.Dir(), "backends.yaml") ||
		sp.VaultDir() != filepath.Join(sp.Dir(), "vault") {
		t.Errorf("chemins : %s %s %s", sp.Dir(), sp.BackendsPath(), sp.VaultDir())
	}
}

func TestEspaceUtilisateurDroitsEtSchema255(t *testing.T) {
	dev255Env(t)
	sp, _ := UserSpace("github-5")
	if err := sp.Ensure(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, d := range []string{sp.Dir(), sp.VaultDir()} {
			if st, _ := os.Stat(d); st == nil || st.Mode().Perm() != 0o700 {
				t.Errorf("%s : droits %v, 0700 attendu", d, st.Mode().Perm())
			}
		}
	}
	// sans fichier : aucun backend (ni host implicite, ni moteur détecté)
	if bs, err := sp.LoadBackends(); err != nil || len(bs) != 0 {
		t.Errorf("espace vide : %v, %v", bs, err)
	}
	if _, err := sp.SaveBackends([]byte("backends:\n  k:\n    type: k8s\n    namespace: ns\n"), time.Time{}); err != nil {
		t.Fatal(err)
	}
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) != 1 || bs[0].Name != "k" {
		t.Fatalf("backends : %v, %v", bs, err)
	}
	// conflit : le fichier a changé depuis l'ouverture
	if _, err := sp.SaveBackends([]byte("backends: {}\n"), time.Time{}); !errors.Is(err, ErrConfigConflict) {
		t.Errorf("conflit attendu : %v", err)
	}
	if err := sp.SaveSettings(UserSettings{GithubToken: "oauth", ClaudeOptions: "--x"}); err != nil {
		t.Fatal(err)
	}
	if err := sp.SaveSettings(UserSettings{GithubToken: "jeton"}); err == nil {
		t.Error("choix de jeton invalide accepté")
	}
	if err := sp.RecordTrace(Trace{Backend: "k", Kind: "k8s", Object: "o", Project: "p", Category: "c"}); err != nil {
		t.Fatal(err)
	}
	// aucune donnée projet : seuls les fichiers du contrat
	ents, _ := os.ReadDir(sp.Dir())
	for _, e := range ents {
		switch n := e.Name(); {
		case n == "backends.yaml", n == "backends.yaml.bak", n == "vault", n == "config.json", n == "traces.json",
			strings.HasSuffix(n, ".lock"):
		default:
			t.Errorf("fichier hors contrat dans l'espace : %s", n)
		}
	}
}

func TestTracesSchemaEtPurge255(t *testing.T) {
	dev255Env(t)
	sp, _ := UserSpace("github-5")
	t1 := Trace{Backend: "k", Kind: "k8s", Object: "sts-p", Project: "p", Category: "c"}
	if err := sp.RecordTrace(t1); err != nil {
		t.Fatal(err)
	}
	first, _ := sp.LoadTraces()
	time.Sleep(5 * time.Millisecond)
	_ = sp.RecordTrace(t1)
	_ = sp.RecordTrace(Trace{Backend: "k2", Kind: "docker", Object: "o2", Project: "q", Category: "c"})
	ts, _ := sp.LoadTraces()
	if len(ts) != 2 || !ts[0].FirstSeen.Equal(first[0].FirstSeen) || !ts[0].LastSeen.After(first[0].LastSeen) {
		t.Errorf("mise à jour d'une trace : %+v", ts)
	}
	b, _ := os.ReadFile(sp.TracesPath())
	for _, k := range []string{`"backend"`, `"kind"`, `"object"`, `"project"`, `"category"`, `"first_seen"`, `"last_seen"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("champ %s absent de traces.json", k)
		}
	}
	if n, err := sp.PurgeTrace("k", "p"); err != nil || n != 1 {
		t.Errorf("PurgeTrace = %d, %v", n, err)
	}
	if n, _ := sp.PurgeBackendTraces("k2"); n != 1 {
		t.Errorf("PurgeBackendTraces = %d", n)
	}
	if ts, _ = sp.LoadTraces(); len(ts) != 0 {
		t.Errorf("traces restantes : %+v", ts)
	}
	if _, err := AdminSpace().PurgeTrace("k", "p"); err == nil {
		t.Error("l'admin n'a pas de traces")
	}
}

// Échec fermé : un backends.yaml illisible est tenu pour touchant un champ R1 ; il n'est jamais enregistré ni validé comme bon.
func TestUserAdminFieldsEchecFerme255(t *testing.T) {
	dev255Env(t)
	for _, bad := range []string{"backends: [", "backends:\n  k: {type: k8s\n", "\tx: : :"} {
		if !UserAdminFields([]byte(bad)) {
			t.Errorf("YAML illisible %q : vrai attendu", bad)
		}
	}
	if UserAdminFields([]byte("backends:\n  k:\n    type: k8s\n    namespace: ns\n")) || UserAdminFields(nil) {
		t.Error("contenu sain tenu pour R1")
	}
	sp, _ := UserSpace("github-5")
	if ps := sp.ValidateBackends([]byte("backends: [")); !HasErrors(ps) {
		t.Errorf("validation d'un YAML illisible : %v", ps)
	}
	if _, err := sp.SaveBackends([]byte("backends: ["), time.Time{}); !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("enregistrement d'un YAML illisible : %v", err)
	}
	if _, err := os.Stat(sp.BackendsPath()); err == nil {
		t.Error("fichier écrit malgré un YAML illisible")
	}
}

// Revue LOT-2 : un appel API lent ne bloque ni les autres logins, ni les autres requêtes du même login ; un seul vol par login ;
// après un échec, pas de nouvel appel avant ghidRetry.
func TestResolveUIDApiLenteSansBlocage255(t *testing.T) {
	dev255Env(t)
	release := make(chan struct{})
	var slowHits, otherHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "slow":
			slowHits.Add(1)
			<-release
			fmt.Fprint(w, `{"id":5,"login":"slow"}`)
		case "fast":
			otherHits.Add(1)
			fmt.Fprint(w, `{"id":6,"login":"fast"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer func(old string) { GithubUsersURL = old }(GithubUsersURL)
	GithubUsersURL = srv.URL + "/"
	defer close(release)

	// trois requêtes concurrentes du login lent : un seul appel API
	res := make(chan string, 3)
	for i := 0; i < 3; i++ {
		go func() { uid, _ := ResolveUID("slow"); res <- uid }()
	}
	for slowHits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	// pendant ce temps, un autre login est résolu sans attendre
	done := make(chan string, 1)
	go func() { uid, _ := ResolveUID("fast"); done <- uid }()
	select {
	case uid := <-done:
		if uid != "github-6" {
			t.Errorf("fast = %q", uid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("un login lent bloque les autres utilisateurs")
	}
	release <- struct{}{}
	for i := 0; i < 3; i++ {
		if uid := <-res; uid != "github-5" {
			t.Errorf("slow = %q", uid)
		}
	}
	if slowHits.Load() != 1 {
		t.Errorf("%d appels API pour un login, 1 attendu (un seul vol)", slowHits.Load())
	}
}

func TestResolveUIDEchecPasDeNouvelEssaiAvant5Min255(t *testing.T) {
	dev255Env(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "rate limit", http.StatusForbidden)
	}))
	defer srv.Close()
	defer func(old string) { GithubUsersURL = old }(GithubUsersURL)
	GithubUsersURL = srv.URL + "/"
	clock := time.Now()
	defer ghidNow.Set(ghidNow.Set(func() time.Time { return clock }))
	for i := 0; i < 5; i++ {
		if _, err := ResolveUID("carol"); !errors.Is(err, ErrLoginUnresolved) {
			t.Fatalf("essai %d : %v", i, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d appels pendant l'attente, 1 attendu", hits.Load())
	}
	clock = clock.Add(6 * time.Minute) // le délai est passé : nouvel essai
	_, _ = ResolveUID("carol")
	if hits.Load() != 2 {
		t.Errorf("%d appels après 6 min, 2 attendus", hits.Load())
	}
	// entrée connue périmée + API en panne : l'entrée sert, une seule tentative par fenêtre
	id := &atomic.Int64{}
	id.Store(8)
	dev255API(t, map[string]*atomic.Int64{"dave": id})
	if uid, _ := ResolveUID("dave"); uid != "github-8" {
		t.Fatal(uid)
	}
	GithubUsersURL = srv.URL + "/"
	hits.Store(0)
	clock = clock.Add(48 * time.Hour)
	for i := 0; i < 4; i++ {
		if uid, err := ResolveUID("dave"); err != nil || uid != "github-8" {
			t.Fatalf("dave : %q %v", uid, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d appels pour une entrée périmée en panne, 1 attendu", hits.Load())
	}
}

// Contrat §3 : « unix:// » refusé à un utilisateur (socket local = calcul sur la plateforme) ; tcp:// et k8s acceptés.
func TestUserAdminFieldsUnixRefuse255(t *testing.T) {
	for _, bad := range []string{
		"backends:\n  d: {type: docker, host: 'unix:///var/run/docker.sock'}\n",
		"backends:\n  d: {type: podman, host: UNIX:///run/podman/podman.sock}\n",
		"backends:\n  d: {type: docker, engine_host: ' unix:///x'}\n",
		"backends:\n  d: {type: docker, hosts: ['tcp://a:2376', 'unix:///x']}\n",
	} {
		if !UserAdminFields([]byte(bad)) {
			t.Errorf("unix:// accepté : %q", bad)
		}
	}
	for _, ok := range []string{
		"backends:\n  d: {type: docker, host: 'tcp://nas.lan:2376'}\n",
		"backends:\n  k: {type: k8s, namespace: ns}\n",
	} {
		if UserAdminFields([]byte(ok)) {
			t.Errorf("contenu légitime refusé : %q", ok)
		}
	}
}

// Revue LOT-2 cycle 2 : liste blanche — chaque contournement de la liste noire est refusé ; les déclarations légitimes passent.
func TestUserAdminFieldsListeBlanche255(t *testing.T) {
	for name, bad := range map[string]string{
		"pool et socket local": "pools:\n  p:\n    hosts: ['unix:///var/run/docker.sock']\nbackends:\n  b: {type: docker, pool: p, host: '{}'}\n",
		"docker sans host":     "backends:\n  d: {type: docker}\n",
		"podman sans host":     "backends:\n  d: {type: podman, volume: v}\n",
		"chemin nu":            "backends:\n  d: {type: docker, host: /var/run/docker.sock}\n",
		"npipe":                "backends:\n  d: {type: docker, host: 'npipe:////./pipe/docker_engine'}\n",
		"fd":                   "backends:\n  d: {type: docker, host: 'fd://'}\n",
		"ssh://":               "backends:\n  d: {type: docker, host: 'ssh://root@h'}\n",
		"https://":             "backends:\n  d: {type: docker, host: 'https://h:2376'}\n",
		"modèle de pool":       "backends:\n  d: {type: docker, host: 'tcp://{}:2375'}\n",
		"type ssh":             "backends:\n  s: {type: ssh, host: h}\n",
		"type host":            "backends:\n  s: {type: host}\n",
		"type absent":          "backends:\n  s: {namespace: ns}\n",
		"type inconnu":         "backends:\n  s: {type: lxc, host: 'tcp://h:1'}\n",
		"rubrique pools":       "pools:\n  p: {hosts: [a]}\nbackends:\n  k: {type: k8s, namespace: ns}\n",
		"rubrique hubs":        "hubs:\n  m: {}\nbackends:\n  k: {type: k8s, namespace: ns}\n",
		"rubrique defaults":    "defaults:\n  browser_server: {listen: off}\nbackends:\n  k: {type: k8s, namespace: ns}\n",
		"rubrique images":      "images:\n  - x\nbackends:\n  k: {type: k8s, namespace: ns}\n",
		"k8s kubeconfig":       "backends:\n  k: {type: k8s, namespace: ns, kubeconfig: /etc/k3s.yaml}\n",
		"k8s context":          "backends:\n  k: {type: k8s, namespace: ns, context: c}\n",
		"k8s volumes":          "backends:\n  k: {type: k8s, namespace: ns, volumes: {/a: p}}\n",
		"k8s host":             "backends:\n  k: {type: k8s, namespace: ns, host: 'tcp://h:1'}\n",
		"k8s pod sensible":     "backends:\n  k: {type: k8s, namespace: ns, pod: {serviceAccountName: x}}\n",
		"k8s pod tolerations":  "backends:\n  k: {type: k8s, namespace: ns, pod: {tolerations: []}}\n",
		"docker engine_host":   "backends:\n  d: {type: docker, host: 'tcp://h:1', engine_host: 'unix:///x'}\n",
		"docker hosts":         "backends:\n  d: {type: docker, host: 'tcp://h:1', hosts: ['unix:///x']}\n",
		"docker shared":        "backends:\n  d: {type: docker, host: 'tcp://h:1', shared: [/a]}\n",
		"docker volumes":       "backends:\n  d: {type: docker, host: 'tcp://h:1', volumes: {/a: b}}\n",
		"docker champ inconnu": "backends:\n  d: {type: docker, host: 'tcp://h:1', privileged: true}\n",
		"tls hors schéma":      "backends:\n  d: {type: docker, host: 'tcp://h:1', tls: {ca: /etc/ca.pem}}\n",
		"backends pas une map": "backends: [a]\n",
		"backend pas une map":  "backends:\n  d: oui\n",
		"racine pas une map":   "- backends\n",
		"fusion YAML (<<)":     "x: &a {type: docker}\nbackends:\n  d:\n    <<: *a\n    host: 'unix:///x'\n",
	} {
		if !UserAdminFields([]byte(bad)) {
			t.Errorf("%s : accepté", name)
		}
	}
	for name, ok := range map[string]string{
		"vide":                "",
		"backends vide":       "backends: {}\n",
		"backends null":       "backends:\n",
		"k8s namespace":       "backends:\n  k: {type: k8s, namespace: ns}\n",
		"k8s pvc, ressources": "backends:\n  k: {type: k8s, namespace: ns, pvc: p, spread: true, pod: {resources: {requests: {cpu: '1'}}, nodeSelector: {a: b}}}\n",
		"docker tcp + TLS":    "backends:\n  d:\n    type: docker\n    host: tcp://nas.lan:2376\n    volume: v\n    tls: {enabled: true, verify: true}\n",
		"podman tcp":          "backends:\n  p: {type: podman, host: 'TCP://h:8080'}\n",
	} {
		if UserAdminFields([]byte(ok)) {
			t.Errorf("%s : refusé à tort", name)
		}
	}
}
