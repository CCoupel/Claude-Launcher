package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #220 (LOT-1 v6.7.0) — bascule d'organisation bornée quand le partage de config.conf ne répond plus.
// Contrat : contracts/desktop-dispatch.md § « v6.7.0 » (Écriture bornée de config.conf). Le partage mort est simulé par
// core.ShareWriteHook (point d'injection déterministe : un canal, aucune pause fixe), un hook bloquant tient lieu
// d'appel système qui ne revient pas.

// deadShare : installe un partage « mort » pour config.conf jusqu'à release(). entered / left comptent les écritures
// entrées dans le hook / sorties du hook.
type deadShare struct {
	gate          chan struct{}
	entered, left atomic.Int64
	once          atomic.Bool
}

func installDeadShare(t *testing.T) *deadShare {
	t.Helper()
	d := &deadShare{gate: make(chan struct{})}
	old := core.ShareWriteHook
	core.ShareWriteHook = func(path string) {
		if !strings.HasSuffix(filepath.ToSlash(path), "config.conf") {
			return
		}
		d.entered.Add(1)
		<-d.gate
		d.left.Add(1)
	}
	t.Cleanup(func() {
		d.release()
		waitUntil(t, "écriture abandonnée terminée", 5*time.Second, func() bool { return d.left.Load() == d.entered.Load() })
		core.ShareWriteHook = old
	})
	return d
}

func (d *deadShare) release() {
	if d.once.CompareAndSwap(false, true) {
		close(d.gate)
	}
}

// shareFixture : app + projet alpha, SHARE_WRITE_TIMEOUT=2 (borne minimale : le test dure ~2 s).
func shareFixture(t *testing.T) (a *App, path string) {
	t.Helper()
	ps := sandbox(t, "", "alpha")
	var r recorder
	a = newApp(t, r.emit)
	if err := os.WriteFile(core.ConfigPath(), []byte("SHARE_WRITE_TIMEOUT=2\nLAYOUT_MODE=top\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configReload(core.ConfConf)
	return a, ps[0]
}

func applyLayout(t *testing.T, a *App, path, layout string) (map[string]any, time.Duration) {
	t.Helper()
	t0 := time.Now()
	res := l153Call(t, a, "projectApply", map[string]any{"path": path, "layout": layout})
	return res, time.Since(t0)
}

func noLockFiles(t *testing.T) {
	t.Helper()
	var found []string
	_ = filepath.Walk(filepath.Dir(core.ConfigPath()), func(p string, st os.FileInfo, err error) error {
		if err == nil && st != nil && !st.IsDir() && strings.HasSuffix(p, ".lock") {
			found = append(found, p)
		}
		return nil
	})
	if len(found) > 0 {
		t.Errorf("verrou fantôme laissé : %v", found)
	}
}

// Partage injoignable : erreur au plus SHARE_WRITE_TIMEOUT + 1 s après le clic, au format du contrat, organisation
// précédente conservée ; l'interface reste réactive pendant que l'écriture est bloquée.
func TestShareDeadToggleErrorsWithinTimeout220(t *testing.T) {
	a, path := shareFixture(t)
	d := installDeadShare(t)

	res, took := applyLayout(t, a, path, "left")
	if took > 3*time.Second {
		t.Errorf("erreur rendue après %v, attendu ≤ 3 s (SHARE_WRITE_TIMEOUT=2 + 1 s)", took)
	}
	if took < 1500*time.Millisecond {
		t.Errorf("erreur rendue après %v : avant le délai configuré (2 s), l'écriture n'a pas été attendue", took)
	}
	if res["ok"] != false || res["timeout"] != true {
		t.Fatalf("réponse = %v, attendu {ok:false, timeout:true}", res)
	}
	msg, _ := res["msg"].(string)
	for _, want := range []string{"partage injoignable", "config.conf non écrit après 2 s", "retour à l'organisation précédente"} {
		if !strings.Contains(msg, want) {
			t.Errorf("msg = %q : %q manquant", msg, want)
		}
	}
	if d.entered.Load() != 1 {
		t.Errorf("%d écritures entrées dans le partage, attendu 1", d.entered.Load())
	}
	// organisation précédente (top) : le clic n'est pas resté en mémoire
	if s := l153Call(t, a, "projectSettings", map[string]any{"path": path}); s["layout"] != "top" {
		t.Errorf("layout après délai = %v, attendu top (retour à l'organisation précédente)", s["layout"])
	}
	// interface réactive : un autre appel n'attend ni writeMu ni confMu
	t0 := time.Now()
	l153Call(t, a, "projectSettings", map[string]any{"path": path})
	a.confNow()
	if d := time.Since(t0); d > time.Second {
		t.Errorf("appel d'interface retenu %v pendant le partage mort (verrou tenu)", d)
	}
}

// Une seule écriture abandonnée par fichier : tant qu'elle est bloquée, les clics suivants échouent tout de suite avec
// le même message, sans nouvelle goroutine ni nouvelle écriture.
func TestShareDeadOneAbandonedWritePerFile220(t *testing.T) {
	a, path := shareFixture(t)
	d := installDeadShare(t)
	if res, _ := applyLayout(t, a, path, "left"); res["timeout"] != true {
		t.Fatalf("premier clic : %v", res)
	}
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		layout := []string{"top", "left"}[i%2]
		res, took := applyLayout(t, a, path, layout)
		if res["ok"] != false {
			t.Fatalf("clic %d : %v, attendu un échec (écriture précédente toujours bloquée)", i, res)
		}
		if msg, _ := res["msg"].(string); !strings.Contains(msg, "partage injoignable") {
			t.Errorf("clic %d : msg = %q, attendu le même message que le premier", i, msg)
		}
		if took > 500*time.Millisecond {
			t.Errorf("clic %d : %v, attendu un échec immédiat", i, took)
		}
	}
	if n := d.entered.Load(); n != 1 {
		t.Errorf("%d écritures entrées dans le partage, attendu 1 (une seule écriture abandonnée par fichier)", n)
	}
	time.Sleep(100 * time.Millisecond) // tâches de fond lancées par les clics (remise en page) : laissées finir
	if after := runtime.NumGoroutine(); after > before+3 {
		t.Errorf("goroutines : %d → %d après 10 clics, une écriture abandonnée par fichier au plus", before, after)
	}
}

// Reprise automatique : dès que le partage répond, les clics écrivent de nouveau, sans relance ; aucun verrou fantôme.
func TestShareDeadResumesAutomatically220(t *testing.T) {
	a, path := shareFixture(t)
	d := installDeadShare(t)
	if res, _ := applyLayout(t, a, path, "left"); res["timeout"] != true {
		t.Fatalf("premier clic : %v", res)
	}
	d.release() // le partage revient
	waitUntil(t, "écriture abandonnée terminée", 5*time.Second, func() bool { return d.left.Load() == 1 })

	var res map[string]any
	waitUntil(t, "clic écrit de nouveau", 5*time.Second, func() bool {
		res, _ = applyLayout(t, a, path, "top")
		return res["ok"] == true
	})
	if res["colorSaved"] != true {
		t.Errorf("réponse = %v, attendu colorSaved:true (config.conf écrit)", res)
	}
	if got := confText(t); !strings.Contains(got, "PROJECT_LAYOUTS") || !strings.Contains(got, "top") {
		t.Errorf("config.conf après reprise :\n%s", got)
	}
	noLockFiles(t)
	// deux clics de plus : aucune écriture ne reste coincée
	for _, l := range []string{"left", "top"} {
		if r, took := applyLayout(t, a, path, l); r["ok"] != true || took > 2*time.Second {
			t.Errorf("clic %s après reprise : %v en %v", l, r, took)
		}
	}
}

// Un partage lent mais vivant (écriture plus courte que le délai) n'est pas une erreur.
func TestShareSlowButAliveIsNotAnError220(t *testing.T) {
	a, path := shareFixture(t)
	old := core.ShareWriteHook
	core.ShareWriteHook = func(string) { time.Sleep(300 * time.Millisecond) }
	defer func() { core.ShareWriteHook = old }()
	res, _ := applyLayout(t, a, path, "left")
	if res["ok"] != true || res["timeout"] == true {
		t.Errorf("écriture lente (0,3 s < 2 s) : %v, attendu ok", res)
	}
}

// Œil de masquage et couleur passent par la même attente bornée.
func TestShareDeadBoundsColorAndHide220(t *testing.T) {
	a, path := shareFixture(t)
	installDeadShare(t)
	for _, args := range []map[string]any{
		{"path": path, "color": "22"},
		{"path": path, "hideIdle": "none"},
	} {
		raw, _ := json.Marshal(args)
		t0 := time.Now()
		res, err := a.Dispatch("projectApply", raw)
		if err != nil {
			t.Fatalf("%v : %v", args, err)
		}
		if took := time.Since(t0); took > 3500*time.Millisecond {
			t.Errorf("%v : %v, attendu ≤ SHARE_WRITE_TIMEOUT + 1 s", args, took)
		}
		if m, _ := res.(map[string]any); m["ok"] != false {
			t.Errorf("%v : %v, attendu une erreur de délai", args, res)
		}
	}
}
