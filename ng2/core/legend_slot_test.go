package core

import (
	"strings"
	"testing"
	"time"
)

// #122 : chaque badge porte sa colonne (l'interface native les aligne d'une
// ligne à l'autre) ; l'ordre de la ligne de la coque ne change pas.
func TestBadgeSlots(t *testing.T) {
	p := ProjInfo{GitOK: true, Git: GitInfo{IsRepo: true, Branch: "main", Main: "main"}, Tpl: TplOK,
		HasEnv: true, Backend: "b", CtrKnown: true, Ctr: Found{Running: true, Name: "c"},
		Ms: MsState{Kind: MsWorking, PRs: 2}, HasSess: true, Sess: Resumable{Mod: time.Now()}}
	var slots []string
	for _, i := range BadgeIndicators(p) {
		slots = append(slots, i.Slot)
	}
	if got := strings.Join(slots, ","); got != "local,remote,tpl,ctr,ms,pr,resume" {
		t.Errorf("colonnes : %s", got)
	}
	if DotIndicator(p).Slot != "" {
		t.Error("la pastille n'est pas un badge")
	}
}
