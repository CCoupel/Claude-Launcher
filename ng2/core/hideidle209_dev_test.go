package core

import (
	"os"
	"strings"
	"testing"
)

// #209 (dev) : PROJECT_HIDE_IDLE — lecture (bash et confLite), écriture, priorité projet > global, refus.

func hideConf209(t *testing.T, conf string) Config {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(ConfigPath(), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	return LoadConfig()
}

func TestDev209PriorityProjectOverGlobal(t *testing.T) {
	c := hideConf209(t, "TEAM_HIDE_IDLE=45\nTEAM_HIDE_IDLE_ENABLED=0\ndeclare -A PROJECT_HIDE_IDLE=([\"p-auto\"]=\"auto\" [\"p-none\"]=\"none\")\n")
	for proj, want := range map[string]struct {
		en   bool
		mode string
		arg  int
	}{"/x/p-auto": {true, "auto", 45}, "/x/p-none": {false, "none", -1}, "/x/autre": {false, "default", -1}} {
		i := c.HideIdleOf(proj)
		if i.Enabled != want.en || i.Mode != want.mode || i.Global || i.Delay != 45 || c.HideIdleArgOf(proj) != want.arg {
			t.Errorf("%s : %+v arg=%d", proj, i, c.HideIdleArgOf(proj))
		}
	}
	g := hideConf209(t, "TEAM_HIDE_IDLE=45\ndeclare -A PROJECT_HIDE_IDLE=([\"p-none\"]=\"none\")\n") // global activé
	if !g.HideIdleOf("/x/autre").Enabled || g.HideIdleOf("/x/p-none").Enabled || !g.HideIdleOf("/x/p-none").Global {
		t.Errorf("global activé : %+v / %+v", g.HideIdleOf("/x/autre"), g.HideIdleOf("/x/p-none"))
	}
	z := hideConf209(t, "TEAM_HIDE_IDLE=0\ndeclare -A PROJECT_HIDE_IDLE=([\"p\"]=\"auto\")\n")
	if z.HideIdleArgOf("/x/p") != 0 || z.HideIdleOf("/x/p").Enabled {
		t.Errorf("délai 0 : arg %d, effectif %v (jamais masqué, masquage à la main permis)", z.HideIdleArgOf("/x/p"), z.HideIdleOf("/x/p").Enabled)
	}
	if msg := c.HideIdleRefusal("/x/p-none"); msg != HideIdleProjectDisabledMsg {
		t.Errorf("refus none : %q", msg)
	}
	if c.HideIdleRefusal("/x/p-auto") != "" || c.HideIdleRefusal("/x/autre") != HideIdleProjectDisabledMsg {
		t.Error("refus auto / default global éteint")
	}
}

func TestDev209WriteReadRoundTrip(t *testing.T) {
	src, err := SetProjectHideIdle("# c\nTEAM_HIDE_IDLE=30\n", "monp", "none")
	if err != nil || !strings.Contains(src, "# c") || !strings.Contains(src, `["monp"]="none"`) {
		t.Fatalf("écriture : %q %v", src, err)
	}
	c := hideConf209(t, src)
	if c.HideIdleModeOf("/a/monp") != "none" {
		t.Errorf("relecture bash : %v", c.ProjectHideIdle)
	}
	back, _ := SetProjectHideIdle(src, "monp", "default")
	if strings.Contains(back, `["monp"]`) || c.HideIdleModeOf("/a/zz") != "default" {
		t.Errorf("retrait : %q", back)
	}
	if _, err := SetProjectHideIdle("", "p", "bizarre"); err == nil {
		t.Error("valeur inconnue acceptée")
	}
	// confLite (Windows) : même lecture
	out := confLite(src, "/home/x")
	if !strings.Contains(out, "phide\tmonp\tnone") {
		t.Errorf("confLite : %q", out)
	}
}
