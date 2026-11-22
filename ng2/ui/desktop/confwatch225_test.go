//go:build !windows

package desktop

import (
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Le surveillant d'un projet ouvert (tick → confRefresh) et configReload passent par le MÊME chemin que conf() : un config.conf
// INVALIDE garde la dernière copie valide (jeton compris) et n'est PAS recopié dans settings.json — avec ou sans projet ouvert.
// (Mutation : remettre core.LoadConfig() sans validation dans configReload / le tour fait échouer ce test.)
func TestWatcherEtReloadGardentLaCopieValide225(t *testing.T) {
	sandbox(t, "", "alpha")
	t0 := time.Now().Add(-time.Hour)
	writeConf225(t, "GITHUB_TOKEN=ghp_FAKE_TEST_valide\nCLAUDE_DISABLE_MOUSE=1\n", t0)
	var r recorder
	a := newApp(t, r.emit)
	if a.conf().GithubToken != "ghp_FAKE_TEST_valide" {
		t.Fatalf("précondition : %q", a.conf().GithubToken)
	}
	if !core.SyncLauncherEnv(a.conf()) {
		t.Fatal("recopie initiale")
	}
	bad := "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n"
	if ps := core.ValidateConfig(core.ConfConf, []byte(bad)); !core.HasErrors(ps) {
		t.Skip("contenu jugé valide par la validation actuelle")
	}
	writeConf225(t, bad, t0.Add(10*time.Second))
	for i := 0; i < 3; i++ { // tours du surveillant, relectures explicites : même résultat
		a.confRefresh(true)
		a.configReload(core.ConfConf)
	}
	if got := a.conf().GithubToken; got != "ghp_FAKE_TEST_valide" {
		t.Errorf("copie valide perdue : jeton = %q", got)
	}
	if v, _ := settingsMouse225(t); v != "1" {
		t.Errorf("settings.json recopié depuis un config.conf invalide : souris = %q, attendu 1", v)
	}
	// corrigé : la nouvelle valeur est prise et recopiée
	writeConf225(t, "GITHUB_TOKEN=ghp_FAKE_TEST_valide\nCLAUDE_DISABLE_MOUSE=0\n", t0.Add(20*time.Second))
	a.configReload(core.ConfConf)
	waitFor225(t, 5*time.Second, "souris 0 recopiée", func() bool { v, _ := settingsMouse225(t); return v == "" })
}
