package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #176 (LOT-3.2, cycle 1 — revue M1 et M2) : reprise d'un verrou périmé, détenteur lent, témoin de présence,
// listes de clés de settings.json par launcher. Doivent ÉCHOUER sur 5993663 puis passer avec le correctif.

func twAge(t *testing.T, p string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

// M1 : plusieurs lanceurs constatent le MÊME verrou périmé en même temps : un seul entre à la fois (jamais deux
// détenteurs), tous finissent par passer.
func TestStaleLockTakeoverNeverGivesTwoHolders(t *testing.T) {
	old := fileLockStale.Get()
	fileLockStale.Set(150 * time.Millisecond)
	defer func() { fileLockStale.Set(old) }()
	dir := t.TempDir()
	target := filepath.Join(dir, "shared.json")
	for round := 0; round < 6; round++ {
		lock := target + ".lock"
		_ = os.WriteFile(lock, []byte("mort 1 x\n"), 0o644)
		twAge(t, lock, time.Minute) // périmé : tous le voient
		var inside, double, done int32
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := WithFileLock(target, func() error {
					if atomic.AddInt32(&inside, 1) > 1 {
						atomic.AddInt32(&double, 1)
					}
					time.Sleep(10 * time.Millisecond)
					atomic.AddInt32(&inside, -1)
					return nil
				})
				if err == nil {
					atomic.AddInt32(&done, 1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if double > 0 {
			t.Fatalf("tour %d : %d entrée(s) alors qu'un autre détenait le verrou (reprise du périmé non atomique)", round, double)
		}
		if done != 8 {
			t.Fatalf("tour %d : %d/8 sections exécutées", round, done)
		}
		_ = os.Remove(lock)
	}
}

// M1 : un détenteur LENT (dépasse la péremption, le verrou lui a été repris) ne supprime pas, en finissant, le verrou
// d'un autre : un troisième lanceur ne doit pas entrer pendant que le repreneur travaille.
func TestSlowHolderDoesNotReleaseAnotherLock(t *testing.T) {
	// Chronologie (péremption 600 ms) : A tient 800 ms (dépasse la péremption) ; B reprend le périmé à 650 ms et
	// travaille 400 ms (650-1050, très en dessous de la péremption : son propre verrou ne peut pas être repris
	// légitimement pendant sa section) ; A finit à 800 : s'il supprimait le verrou de B, C (arrivé à 900) entrerait
	// pendant la section de B. Le drapeau de B est posé ET retiré DANS sa section : aucune dépendance à un minuteur
	// externe (sur un runner lent, un drapeau retiré par minuteur pouvait être encore levé après la fin de B).
	old := fileLockStale.Get()
	fileLockStale.Set(600 * time.Millisecond)
	defer func() { fileLockStale.Set(old) }()
	target := filepath.Join(t.TempDir(), "shared.json")
	var bInside atomic.Bool
	var cEnteredDuringB atomic.Bool
	var wg sync.WaitGroup
	run := func(at time.Duration, section func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(at)
			_ = WithFileLock(target, func() error { section(); return nil })
		}()
	}
	run(0, func() { time.Sleep(800 * time.Millisecond) }) // A : lent, dépasse la péremption
	run(650*time.Millisecond, func() {                    // B : reprend le périmé
		bInside.Store(true)
		time.Sleep(400 * time.Millisecond)
		bInside.Store(false)
	})
	run(900*time.Millisecond, func() { // C : arrive après la fin de A, pendant B
		if bInside.Load() {
			cEnteredDuringB.Store(true)
		}
	})
	wg.Wait()
	if cEnteredDuringB.Load() {
		t.Error("le détenteur lent a supprimé le verrou de son repreneur : un troisième lanceur est entré pendant sa section")
	}
}

// Présence : deux lanceurs de même nom constatent ensemble un témoin périmé → un seul démarre.
func TestPresenceStaleTakeoverStartsOnlyOne(t *testing.T) {
	withHome(t)
	oldStale := presenceStale.Get()
	presenceStale.Set(150 * time.Millisecond)
	defer func() { presenceStale.Set(oldStale) }()
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_HUB_NAME", "hub-presence")
	for round := 0; round < 6; round++ {
		p := presencePath()
		_ = os.WriteFile(p, []byte("autre-machine 424242 2020-01-01T00:00:00Z\n"), 0o644)
		twAge(t, p, time.Minute)
		var ok int32
		var rels []func()
		var mu sync.Mutex
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if rel, err := AcquirePresence(); err == nil {
					atomic.AddInt32(&ok, 1)
					mu.Lock()
					rels = append(rels, rel)
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		for _, r := range rels {
			r()
		}
		if ok != 1 {
			t.Fatalf("tour %d : %d lanceurs ont démarré sur un témoin périmé, attendu exactement 1", round, ok)
		}
		_ = os.Remove(p)
	}
}

// M2 : deux launchers (CL_HUB_NAME différents) sur le même CLAUDE_CONFIG_DIR, aux variables différentes : chacun garde
// ses clés après des synchronisations alternées ; un launcher ne retire que ce qu'il a posé ; clés racine intactes.
func TestTwoLaunchersKeepTheirOwnSettingsKeys(t *testing.T) {
	withHome(t)
	dir := filepath.Join(t.TempDir(), "claude")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(path, []byte(`{"model":"opus","env":{"MA_VAR":"perso"}}`), 0o644)
	as := func(h string) { t.Setenv("CL_HUB_NAME", h) }
	varsA := []envVar{{"TW_A1", "a1"}, {"TW_A2", "a2"}}
	varsB := []envVar{{"TW_B1", "b1"}, {"TW_B2", "b2"}}
	for i := 0; i < 4; i++ {
		as("hub-a")
		if err := SyncSettingsEnv(dir, varsA); err != nil {
			t.Fatal(err)
		}
		as("hub-b")
		if err := SyncSettingsEnv(dir, varsB); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (map[string]string, map[string]any) {
		b, _ := os.ReadFile(path)
		var d struct {
			Env map[string]string `json:"env"`
		}
		var root map[string]any
		if err := json.Unmarshal(b, &d); err != nil || json.Unmarshal(b, &root) != nil {
			t.Fatalf("JSON invalide : %v %s", err, b)
		}
		return d.Env, root
	}
	env, root := read()
	for _, k := range []string{"TW_A1", "TW_A2", "TW_B1", "TW_B2", "MA_VAR"} {
		if env[k] == "" {
			t.Errorf("clé %s effacée par l'autre launcher (ou perdue) : %v", k, env)
		}
	}
	if root["model"] != "opus" {
		t.Errorf("clé racine de l'utilisateur perdue : %v", root)
	}
	// A retire TW_A2 : seule sa clé disparaît
	as("hub-a")
	if err := SyncSettingsEnv(dir, varsA[:1]); err != nil {
		t.Fatal(err)
	}
	env, _ = read()
	if _, ok := env["TW_A2"]; ok {
		t.Errorf("TW_A2 retirée de la liste de A mais toujours présente : %v", env)
	}
	for _, k := range []string{"TW_A1", "TW_B1", "TW_B2", "MA_VAR"} {
		if env[k] == "" {
			t.Errorf("le retrait de A a emporté %s : %v", k, env)
		}
	}
	// même clé, valeurs différentes : le dernier écrivain gagne (documenté), pas d'erreur
	as("hub-b")
	if err := SyncSettingsEnv(dir, []envVar{{"TW_A1", "b-gagne"}, {"TW_B1", "b1"}, {"TW_B2", "b2"}}); err != nil {
		t.Fatal(err)
	}
	if env, _ = read(); env["TW_A1"] != "b-gagne" {
		t.Errorf("même clé : dernier écrivain attendu gagnant, TW_A1=%q", env["TW_A1"])
	}
}

// Sans CL_HUB_NAME : comportement de la v6.2.0 — liste historique <dir>/claude-launcher/settings-env.json.
func TestSettingsKeysWithoutHubNameUseLegacyFile(t *testing.T) {
	withHome(t)
	t.Setenv("CL_HUB_NAME", "")
	dir := filepath.Join(t.TempDir(), "claude")
	if err := SyncSettingsEnv(dir, []envVar{{"TW_X", "1"}, {"TW_Y", "2"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "claude-launcher", "settings-env.json"))
	if err != nil {
		t.Fatalf("fichier historique absent : %v", err)
	}
	var keys []string
	if json.Unmarshal(b, &keys) != nil || len(keys) != 2 {
		t.Errorf("liste historique : %s", b)
	}
	if err := SyncSettingsEnv(dir, []envVar{{"TW_X", "1"}}); err != nil {
		t.Fatal(err)
	}
	s, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var d struct{ Env map[string]string }
	_ = json.Unmarshal(s, &d)
	if _, ok := d.Env["TW_Y"]; ok || d.Env["TW_X"] != "1" {
		t.Errorf("v6.2.0 : TW_Y devait être retirée, TW_X gardée : %v", d.Env)
	}
}
