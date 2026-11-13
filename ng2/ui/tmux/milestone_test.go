package tmux

import (
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

func TestPRIndicator(t *testing.T) {
	if _, ok := prIndicator(core.MsState{}); ok {
		t.Error("badge PR sans PR ouverte")
	}
	n, d := core.CountPRs([]core.RawPR{{IsDraft: false}, {IsDraft: true}, {IsDraft: false}})
	i, ok := prIndicator(core.MsState{PRs: n, Drafts: d})
	if !ok || i.icon != "⇄3" || !strings.Contains(i.text, "3 pull request") || !strings.Contains(i.text, "dont 1 brouillon") {
		t.Errorf("prIndicator=%+v ok=%v", i, ok)
	}
	ins := badgeIndicators(projInfo{ms: core.MsState{Kind: core.MsWorking, PRs: 2}})
	if len(ins) != 2 || ins[0].icon != "⚑" || ins[1].icon != "⇄2" {
		t.Errorf("ordre des badges=%+v", ins)
	}
}
