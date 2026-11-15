package tmux

import (
	"claude-launcher-ng2/core"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Relecture complète du projet sélectionné (F5, #158) ───────────────────

// refreshedMsg : ce que F5 a relu pour un projet, sans cache.
type refreshedMsg struct {
	node core.Node
	ctr  ctrView
	git  core.GitInfo
	tpl  core.TemplateState
	sess core.Resumable
	has  bool
}

// refreshProjectCmd : conteneurs (diagnostic des backends forcé), session de
// reprise, git, état du template et registre des agents, sans les caches.
func refreshProjectCmd(n core.Node, cfg core.Config, paths []string) tea.Cmd {
	return func() tea.Msg {
		v := core.ReadCtrViewForced(paths)
		core.ForgetTplInfo(n.Path)
		core.ReadTplInfo(n.Path)
		gitSem <- struct{}{}
		g, t := core.ReadGit(n.Path), core.ReadTemplate(n.Path, readLatestTemplate())
		<-gitSem
		r, has := cfg.PendingSession(n.Path)
		return refreshedMsg{node: n, git: g, tpl: t, sess: r, has: has,
			ctr: ctrView{env: v.Env, assign: v.Assign, found: v.Found, errs: v.Errs, diag: v.Diag, bad: v.Bad,
				live: v.Live, dups: v.Dups, down: v.Down}}
	}
}

// onRefreshed : applique la relecture et dit le résultat en pied de navigateur.
func (m model) onRefreshed(r refreshedMsg) model {
	m.ctr = r.ctr
	m.git[r.node.Path], m.tpl[r.node.Path] = r.git, r.tpl
	if m.sessions == nil {
		m.sessions = map[string]core.Resumable{}
	}
	if r.has {
		m.sessions[r.node.Path] = r.sess
	} else {
		delete(m.sessions, r.node.Path)
	}
	m.syncSelection()
	p := m.projInfo(r.node)
	k8s := false
	if b, ok := core.FindBackend(p.backend); ok {
		k8s = b.K8s()
	}
	m.msg = "✓ " + r.node.Name + " relu"
	if ok, why, _ := core.ForceCloseable(core.ForceInput{P: p.core(), Tab: p.open, K8s: k8s}); ok {
		m.msg += " : " + why + " — Suppr propose la fermeture forcée"
	}
	return m
}
