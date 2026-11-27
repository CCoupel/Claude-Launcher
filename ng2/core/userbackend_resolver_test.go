package core

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// LOT-3.1 Batch 2 (#257, D1) — revérification DNS d'un hôte d'utilisateur. Nécessite deux points d'injection que le dev doit exposer
// (étiquette de build « s257seams » tant qu'ils n'existent pas, pour ne pas casser la compilation du paquet) :
//
//	var userHostResolver = tun.Of(func(host string) ([]net.IP, error)) // résolveur du launcher (défaut : net.LookupIP)
//	var userHostCacheTTL = tun.Of(30 * time.Second)                    // durée du cache de résolution
//
// Le dev retire l'étiquette (ou me demande de le faire) quand les noms existent. Entrée observée : Space.ValidateBackends
// (enregistrement) et Space.LoadBackends + Engine.cmd (avant chaque appel).

func resolverFake(answers ...[]net.IP) (*atomic.Int64, func(string) ([]net.IP, error)) {
	resetResolveCache() // le cache de résolution est global : un nouveau résolveur factice repart d'un cache vide
	var n atomic.Int64
	return &n, func(string) ([]net.IP, error) {
		i := int(n.Add(1)) - 1
		if i >= len(answers) {
			i = len(answers) - 1
		}
		return answers[i], nil
	}
}

func TestD1257NomResolvantVersUneIPPriveeRefuseAIEnregistrement(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	_, fn := resolverFake([]net.IP{net.ParseIP("10.0.0.7")})
	defer userHostResolver.Set(userHostResolver.Set(fn))
	sp, _ := UserSpace("github-5")
	if ms := errMsgs(sp.ValidateBackends(hb("tcp://docker.example.org:2376"))); !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
		t.Errorf("nom public résolvant vers une IP privée accepté : %v", ms)
	}
}

func TestD1257UneSeuleIPInterditeSuffit(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	_, fn := resolverFake([]net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")})
	defer userHostResolver.Set(userHostResolver.Set(fn))
	sp, _ := UserSpace("github-5")
	if ms := errMsgs(sp.ValidateBackends(hb("tcp://docker.example.org:2376"))); !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
		t.Errorf("une IP interdite parmi plusieurs acceptée : %v", ms)
	}
}

// Résolveur factice : public à l'enregistrement, privé ensuite (rebinding). Le second appel (cache expiré) est refusé AVANT de lancer docker.
func TestD1257RebindingRefuseAuSecondAppel(t *testing.T) {
	s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	calls, fn := resolverFake([]net.IP{net.ParseIP("93.184.216.34")}, []net.IP{net.ParseIP("10.0.0.7")})
	defer userHostResolver.Set(userHostResolver.Set(fn))
	defer userHostCacheTTL.Set(userHostCacheTTL.Set(5 * time.Millisecond))
	sp := s15Space(t, "github-9", "backends:\n  d:\n    type: docker\n    host: tcp://docker.example.org:2376\n    volume: v\n")
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Fatalf("premier chargement (public) refusé : %v", err)
	}
	if err := (Engine{bs[0]}).cmd("version").Run(); err != nil {
		t.Fatalf("premier appel (public) : %v", err)
	}
	time.Sleep(20 * time.Millisecond) // cache expiré
	before := calls.Load()
	if err := (Engine{bs[0]}).cmd("version").Run(); err == nil {
		t.Error("second appel accepté alors que l'hôte résout maintenant vers 10.0.0.7")
	}
	if calls.Load() == before {
		t.Error("l'hôte n'a pas été résolu de nouveau avant le second appel")
	}
}

// Cache : dans la durée, une seule résolution pour plusieurs appels.
func TestD1257CacheDe30s(t *testing.T) {
	s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	calls, fn := resolverFake([]net.IP{net.ParseIP("93.184.216.34")})
	defer userHostResolver.Set(userHostResolver.Set(fn))
	defer userHostCacheTTL.Set(userHostCacheTTL.Set(time.Hour))
	sp := s15Space(t, "github-9", "backends:\n  d:\n    type: docker\n    host: tcp://docker.example.org:2376\n    volume: v\n")
	bs, _ := sp.LoadBackends()
	for i := 0; i < 5; i++ {
		_ = (Engine{bs[0]}).cmd("version").Run()
	}
	if n := calls.Load(); n > 2 { // enregistrement/chargement + un appel au plus
		t.Errorf("%d résolutions pour 5 appels dans la durée du cache", n)
	}
}

// L'admin n'est jamais soumis à la résolution ni au bornage.
func TestD1257AdminSansResolution(t *testing.T) {
	calls, fn := resolverFake([]net.IP{net.ParseIP("10.0.0.7")})
	defer userHostResolver.Set(userHostResolver.Set(fn))
	if ms := errMsgs(ValidateConfig(ConfBackends, hb("tcp://nas.example.org:2375"))); strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
		t.Errorf("admin refusé : %v", ms)
	}
	if calls.Load() != 0 {
		t.Errorf("%d résolution(s) pour l'admin", calls.Load())
	}
}
