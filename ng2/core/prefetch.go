package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ── Préparation de la mise à jour du NG (annonce orange → jaune → vert) ──────
// Commune à la coque tmux (ui/tmux/prefetch.go) et à l'interface native.

// PrefPath : binaire téléchargé pour tag, dans le dossier temporaire.
func PrefPath(tag string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("claude-launcher-ng2-%s-%s", tag, strings.TrimPrefix(NgAsset(), "claude-launcher-ng2-")))
}

// Prefetch : télécharge le binaire de tag (sauf s'il est déjà là), puis
// SHA256SUMS, puis contrôle la somme ; renvoie le fichier prêt et sa somme.
func Prefetch(tag string, step func(string)) (path, sum string, err error) {
	base := ReleaseBase + "/" + tag
	path = PrefPath(tag)
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

// InstallPrepared : copie le binaire préparé à la place de dst, après un
// dernier contrôle de sa somme (le dossier temporaire est partagé).
func InstallPrepared(path, sum, dst string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("binaire préparé introuvable (%v) : il va être retéléchargé", err)
	}
	if SumOf(body) != sum {
		_ = os.Remove(path)
		return fmt.Errorf("binaire préparé modifié : il va être retéléchargé")
	}
	return InstallBinary(dst, body)
}

// InstallPreparedNow : clic sur l'annonce verte (sans bubbletea).
func InstallPreparedNow(tag, path, sum string) UpdDoneMsg {
	if !ReleaseVerRe.MatchString(NgVersion) {
		return UpdDoneMsg{Tag: tag, Err: fmt.Errorf("binaire de dev (%s) : non remplacé", NgVersion)}
	}
	_, bin, _ := UpdateTargets(tag)
	if bin == "" {
		return UpdDoneMsg{Tag: tag}
	}
	if err := InstallPrepared(path, sum, bin); err != nil {
		return UpdDoneMsg{Tag: tag, Err: err, Stale: true}
	}
	_ = os.Remove(path)
	return UpdDoneMsg{Tag: tag, BinDone: true}
}

var httpCodeRe = regexp.MustCompile(`HTTP \d{3}`)

// ShortReason : raison courte d'un échec (« HTTP 504 », « délai dépassé »…).
func ShortReason(err error) string {
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

// StepText : étape de la préparation, courte (40 colonnes) ou complète.
func StepText(step string, wide bool) string {
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
