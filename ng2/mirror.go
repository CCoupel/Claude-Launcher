package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"
)

// Vignette de l'onglet « ⌗ Teamleaders » (ex-mode --mirror du script bash) :
// affiche en direct le pane du teamleader d'une fenêtre projet et lui relaie
// les touches. Terminal en mode brut : Ctrl+C, Échap, flèches… vont au leader ;
// seul le préfixe tmux reste capté par tmux. Un seul appel tmux par
// rafraîchissement (curseur, focus et contenu ensemble).

const mirrorPeriod = 250 * time.Millisecond

// rawMode : équivalent de « stty -echo -icanon -isig -ixon -ixoff -iexten » ;
// renvoie la fonction de restauration.
func rawMode(fd int) (func(), error) {
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Iflag &^= unix.IXON | unix.IXOFF | unix.ICRNL
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, old) }, nil
}

// mirrorLeader : le teamleader est le pane d'index le plus bas de la fenêtre
// (doLayout le place en premier), y compris sans team. "" : fenêtre fermée.
func mirrorLeader(win string) string {
	ws, err := readWin(win)
	if err != nil {
		return ""
	}
	return ws.panes[0].id
}

// mirrorFrame : rendu complet de la vignette (rows × cols) à partir de l'écran
// du leader : les dernières lignes jusqu'à la dernière non vide (ou le curseur),
// la zone de saisie de claude étant en bas.
func mirrorFrame(name, color string, rows, cols int, focus bool, curX, curY int, frame []string) string {
	avail := rows - 1
	last := 0
	for i, l := range frame {
		if strings.TrimSpace(ansi.Strip(l)) != "" {
			last = i + 1
		}
	}
	last = max(last, curY+1, 1)
	start := max(last-avail+1, 1)

	var b strings.Builder
	icon := "  "
	if focus {
		icon = "⌨ "
	}
	fmt.Fprintf(&b, "\033[1;1H\033[1;30;48;5;%sm %s%s \033[0m\033[K", color, icon, name)
	r := 2
	for i := start; i <= last && r <= rows; i++ {
		l := ""
		if i-1 < len(frame) {
			l = ansi.Truncate(frame[i-1], cols, "")
		}
		fmt.Fprintf(&b, "\033[%d;1H\033[0m%s\033[0m\033[K", r, l)
		r++
	}
	for ; r <= rows; r++ {
		fmt.Fprintf(&b, "\033[%d;1H\033[K", r)
	}
	if cy := curY + 1 - start + 2; focus && cy >= 2 && cy <= rows {
		fmt.Fprintf(&b, "\033[%d;%dH\033[?25h", cy, curX+1)
	} else {
		b.WriteString("\033[?25l")
	}
	return b.String()
}

func runMirror(win, name string) error {
	cfg := loadConfig()
	color := cfg.projectColor(name)
	self := os.Getenv("TMUX_PANE")
	fd := int(os.Stdin.Fd())
	restore, err := rawMode(fd)
	if err != nil {
		return err
	}
	// pas de retour à la ligne auto (lignes rognées), curseur masqué
	fmt.Print("\033[?7l\033[?25l\033[2J")
	defer func() {
		fmt.Print("\033[?7h\033[?25h\033[0m")
		restore()
	}()

	keys := make(chan []byte, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				close(keys) // pane fermé
				return
			}
			keys <- append([]byte(nil), buf[:n]...)
		}
	}()

	leader, leaderAt := "", time.Time{}
	prev := ""
	tick := time.NewTicker(mirrorPeriod)
	defer tick.Stop()
	for {
		// le leader change rarement (relayout, fermeture) : relu toutes les 2 s
		if leader == "" || time.Since(leaderAt) > 2*time.Second {
			if leader = mirrorLeader(win); leader == "" {
				return nil
			}
			leaderAt = time.Now()
		}
		args := []string{"display-message", "-p", "-t", leader, "#{cursor_x} #{cursor_y}", ";",
			"capture-pane", "-p", "-e", "-t", leader}
		if self != "" {
			args = append([]string{"display-message", "-p", "-t", self, "#{pane_active}", ";"}, args...)
		}
		out, err := dtmux(args...).Output()
		if err != nil {
			leader = "" // leader disparu : relu au tour suivant
		} else {
			lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
			focus := false
			if self != "" && len(lines) > 0 {
				focus, lines = lines[0] == "1", lines[1:]
			}
			var cx, cy int
			if len(lines) > 0 {
				if f := strings.Fields(lines[0]); len(f) == 2 {
					cx, _ = strconv.Atoi(f[0])
					cy, _ = strconv.Atoi(f[1])
				}
				lines = lines[1:]
			}
			rows, cols := 24, 80
			if ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil && ws.Row > 0 {
				rows, cols = int(ws.Row), int(ws.Col)
			}
			if s := mirrorFrame(name, color, rows, cols, focus, cx, cy, lines); s != prev {
				prev = s
				os.Stdout.WriteString(s)
			}
		}

		select {
		case k, ok := <-keys:
			if !ok {
				return nil
			}
			if leader != "" {
				// octets bruts : texte, collage, séquences d'échappement, « ; »
				hex := make([]string, 0, len(k)+4)
				hex = append(hex, "send-keys", "-t", leader, "-H")
				for _, c := range k {
					hex = append(hex, fmt.Sprintf("%02x", c))
				}
				_ = dtmux(hex...).Run()
			}
			time.Sleep(30 * time.Millisecond) // laisse le leader réagir avant de redessiner
		case <-tick.C:
		}
	}
}
