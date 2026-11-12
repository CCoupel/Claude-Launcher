package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// prefPath : binaire téléchargé pour tag, dans le dossier temporaire.
func prefPath(tag string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("claude-launcher-ng2-%s-%s", tag, strings.TrimPrefix(NgAsset(), "claude-launcher-ng2-")))
}

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

// prefetch : télécharge le binaire de tag (sauf s'il est déjà là), puis
// SHA256SUMS, puis contrôle la somme ; renvoie le fichier prêt et sa somme.
func prefetch(tag string, step func(string)) (path, sum string, err error) {
	base := ReleaseBase + "/" + tag
	path = prefPath(tag)
	body, rerr := os.ReadFile(path)
	if rerr != nil {
		step("bin:0")
		last := -1
		body, err = ReleaseGetP(base+"/"+NgAsset(), 3*time.Minute, func(n, total int64) {
			if n < 0 {
				step("api")
				last = -1
				return
			}
			if total > 0 {
				if pc := int(n * 100 / total); pc/5 != last/5 {
					last = pc
					step(fmt.Sprintf("bin:%d", pc))
				}
			}
		})
		if err != nil {
			return "", "", fmt.Errorf("version : %w", err)
		}
	} else {
		step("cached")
	}
	step("sha")
	sums, err := FetchSums(base + "/SHA256SUMS")
	if err != nil {
		return "", "", err
	}
	step("check")
	want, ok := sums[NgAsset()]
	if !ok {
		return "", "", fmt.Errorf("%s absent de SHA256SUMS", NgAsset())
	}
	if SumOf(body) != want {
		_ = os.Remove(path)
		return "", "", fmt.Errorf("somme SHA-256 incorrecte")
	}
	if rerr != nil {
		if err := WriteAtomic(path, body); err != nil {
			return "", "", err
		}
		_ = os.Chmod(path, 0o755)
	}
	return path, want, nil
}

// installPrepared : copie le binaire préparé à la place de dst, après un
// dernier contrôle de sa somme (le dossier temporaire est partagé).
func installPrepared(path, sum, dst string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("binaire préparé introuvable (%v) : il va être retéléchargé", err)
	}
	if SumOf(body) != sum {
		_ = os.Remove(path)
		return fmt.Errorf("binaire préparé modifié : il va être retéléchargé")
	}
	return WriteAtomic(dst, body)
}

// installPreparedCmd : clic sur l'annonce verte.
func installPreparedCmd(tag, path, sum string) tea.Cmd {
	return func() tea.Msg {
		if !ReleaseVerRe.MatchString(NgVersion) {
			return UpdDoneMsg{Tag: tag, Err: fmt.Errorf("binaire de dev (%s) : non remplacé", NgVersion)}
		}
		_, bin, _ := UpdateTargets(tag)
		if bin == "" {
			return UpdDoneMsg{Tag: tag}
		}
		if err := installPrepared(path, sum, bin); err != nil {
			return UpdDoneMsg{Tag: tag, Err: err, Stale: true}
		}
		_ = os.Remove(path)
		return UpdDoneMsg{Tag: tag, BinDone: true}
	}
}

var httpCodeRe = regexp.MustCompile(`HTTP \d{3}`)

// shortReason : raison courte d'un échec (« HTTP 504 », « délai dépassé »…).
func shortReason(err error) string {
	m := err.Error()
	switch {
	case httpCodeRe.MatchString(m):
		return httpCodeRe.FindString(m)
	case strings.Contains(m, "somme"):
		return "somme incorrecte"
	case strings.Contains(m, "Timeout") || strings.Contains(m, "deadline"):
		return "délai dépassé"
	default:
		return "réseau"
	}
}

// stepText : étape de la préparation, courte (40 colonnes) ou complète.
func stepText(step string, wide bool) string {
	if pc, ok := strings.CutPrefix(step, "bin:"); ok {
		if wide {
			return "téléchargement de la version… " + pc + " %"
		}
		return "↓ version " + pc + " %"
	}
	long := map[string]string{"start": "disponible, préparation…", "cached": "version déjà téléchargée",
		"sha": "téléchargement de SHA256SUMS…", "check": "contrôle SHA-256…",
		"api": "lien direct en échec, téléchargement par l'API GitHub…"}
	short := map[string]string{"start": "préparation…", "cached": "version en cache",
		"sha": "↓ SHA256SUMS", "check": "contrôle SHA-256", "api": "↓ par l'API GitHub…"}
	if wide {
		return long[step]
	}
	return short[step]
}
