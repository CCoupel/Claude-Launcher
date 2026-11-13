package platform

import (
	"errors"
	"io"
	"strings"
	"unicode/utf16"
)

// ── Terminal interactif (pseudo-terminal) ────────────────────────────────
//
// Pour l'interface Windows (#111) : « docker exec -it », « kubectl exec -it »,
// « ssh -t » vers le tmux d'un conteneur, affichés dans un terminal natif
// (xterm.js). Implémentations : pty_unix.go (github.com/creack/pty) et
// pty_windows.go (ConPTY, par golang.org/x/sys/windows). La coque tmux Linux
// n'en dépend pas : elle garde ses fenêtres tmux.

// TermSpec : commande à lancer dans un terminal.
type TermSpec struct {
	Argv       []string // programme et arguments (recherché dans le PATH)
	Env        []string // nil : environnement du launcher
	Dir        string   // "" : dossier courant
	Cols, Rows int      // ≤ 0 : 80 × 24
}

// Terminal : processus attaché à un pseudo-terminal. Read lit ce que le
// terminal affiche, Write envoie la frappe.
type Terminal interface {
	io.ReadWriter
	Resize(cols, rows int) error // nouvelle taille (SIGWINCH côté processus)
	Wait() error                 // attend la fin du processus (statut ≠ 0 : erreur)
	Close() error                // ferme le terminal ; le processus est tué s'il tourne
	Pid() int
}

// ErrNoCommand : TermSpec sans programme.
var ErrNoCommand = errors.New("terminal : commande vide")

// termSize : taille bornée (1…32767, 80 × 24 par défaut).
func termSize(cols, rows int) (int, int) {
	clamp := func(v, def int) int {
		switch {
		case v <= 0:
			return def
		case v > 32767:
			return 32767
		}
		return v
	}
	return clamp(cols, 80), clamp(rows, 24)
}

// ComposeCommandLine : ligne de commande Windows (CreateProcess) des arguments,
// chacun protégé selon les règles de CommandLineToArgvW (comme syscall.EscapeArg).
func ComposeCommandLine(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = escapeWinArg(a)
	}
	return strings.Join(q, " ")
}

func escapeWinArg(s string) string {
	if s == "" {
		return `""`
	}
	needBS, space := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			needBS = true
		case ' ', '\t':
			space = true
		}
	}
	if !needBS && !space {
		return s
	}
	if !needBS {
		return `"` + s + `"`
	}
	var b []byte
	if space {
		b = append(b, '"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b = append(b, '\\')
			}
			b = append(b, '\\')
		default:
			slashes = 0
		}
		b = append(b, c)
	}
	if space {
		for ; slashes > 0; slashes-- {
			b = append(b, '\\')
		}
		b = append(b, '"')
	}
	return string(b)
}

// envBlock : bloc d'environnement UTF-16 de CreateProcess (« K=V\0…\0\0 »).
func envBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 {
			continue
		}
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	return append(b, 0)
}
