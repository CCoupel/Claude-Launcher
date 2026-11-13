package tmux

import (
	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// ── Préparation de la mise à jour du NG (annonce orange → jaune → vert) ──────
//
//	orange : tag poussé, release pas encore publiée (CI en cours)
//	jaune  : release publiée ; le binaire est téléchargé dans le dossier
//	         temporaire et contrôlé, étapes affichées (téléchargement de la
//	         version, repli par l'API si le lien direct est en erreur, SHA256SUMS,
//	         contrôle) ; en échec (lien en 504…), raison
//	         affichée et nouvel essai à la vérification suivante (2 min) ou d'un clic
//	vert   : binaire prêt et contrôlé ; un clic le copie à la place du binaire
//	         en place (rename atomique) et relance le navigateur.

type (
	prefStepMsg struct{ tag, step string }
	prefDoneMsg struct {
		tag, path, sum string
		err            error
	}
)

// prefPath : binaire téléchargé pour tag, dans le dossier temporaire (core.PrefPath).
func prefPath(tag string) string { return core.PrefPath(tag) }

// startPrefetch : lance la préparation de tag ; l'avancement arrive par m.prefCh.
func (m *model) startPrefetch(tag string) tea.Cmd {
	if m.prefCh == nil {
		m.prefCh = make(chan tea.Msg, 64)
	}
	m.prefTag, m.prefBusy, m.prefErr, m.prefStep = tag, true, "", "start"
	ch := m.prefCh
	go func() {
		path, sum, err := prefetch(tag, func(s string) {
			select { // avancement : jamais bloquant
			case ch <- prefStepMsg{tag, s}:
			default:
			}
		})
		ch <- prefDoneMsg{tag: tag, path: path, sum: sum, err: err}
	}()
	return waitPref(ch)
}

func waitPref(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// prefetch : télécharge et contrôle le binaire de tag (core.Prefetch).
func prefetch(tag string, step func(string)) (path, sum string, err error) {
	return core.Prefetch(tag, step)
}

// installPrepared : copie le binaire préparé à la place de dst (core.InstallPrepared).
func installPrepared(path, sum, dst string) error { return core.InstallPrepared(path, sum, dst) }

// installPreparedCmd : clic sur l'annonce verte.
func installPreparedCmd(tag, path, sum string) tea.Cmd {
	return func() tea.Msg { return core.InstallPreparedNow(tag, path, sum) }
}

// shortReason : raison courte d'un échec (core.ShortReason).
func shortReason(err error) string { return core.ShortReason(err) }

// stepText : étape de la préparation, courte ou complète (core.StepText).
func stepText(step string, wide bool) string { return core.StepText(step, wide) }
