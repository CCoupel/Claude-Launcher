package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"claude-launcher-ng2/platform"
)

// Toute commande lancée vers un backend d'UTILISATEUR (kubectl, docker, exec dans son conteneur) a un DÉLAI et toute sortie lue par le launcher est
// BORNÉE (T22, audit LOT-3.2 B1) : un projet hostile (fichier de plusieurs Gio, dépôt de millions de fichiers, FIFO, processus qui ne rend jamais la main)
// ne peut ni épuiser la mémoire du pod launcher partagé ni retenir un processus ou un compteur indéfiniment.

var (
	userCmdTimeout   = 60 * time.Second // opérations (start, stop, patch, exec courts)
	userReadTimeout  = 15 * time.Second // lectures et sondages
	userLongTimeout  = 5 * time.Minute  // clone, installation de claude
	userOpenTimeout  = 30 * time.Second // ouverture d'un fichier servi (premier octet)
	userServeTimeout = 10 * time.Minute // durée totale d'un fichier servi
)

// Plafonds de sortie lue.
const (
	maxListOut  = 8 << 20  // JSON de découverte / liste d'objets
	maxSmallOut = 64 << 10 // inspect, résolution de chemin, listing de dossier
	maxGitOut   = 4 << 10  // état git
)

// ErrOutputTooLarge : la sortie d'une commande dépasse le plafond ; la commande est tuée.
var ErrOutputTooLarge = errors.New("sortie trop volumineuse")

// newCmd : commande de l'exec argv. Backend d'utilisateur : délai d, tuée à l'échéance (et ses tubes libérés peu après).
func (e Engine) newCmd(d time.Duration, argv []string) *exec.Cmd {
	if !e.B.user {
		return platform.NoWindow(exec.Command(argv[0], argv[1:]...))
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	time.AfterFunc(d+time.Second, cancel) // libère le contexte ; l'échéance a déjà tué la commande
	c := platform.NoWindow(exec.CommandContext(ctx, argv[0], argv[1:]...))
	c.WaitDelay = 2 * time.Second
	return c
}

// readBounded : lance c, lit au plus max octets de sa sortie standard ; au-delà la commande est tuée et ErrOutputTooLarge rendue avec les max premiers octets.
func readBounded(c *exec.Cmd, max int64) ([]byte, error) {
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	b, _ := io.ReadAll(io.LimitReader(out, max+1))
	if int64(len(b)) > max {
		_ = c.Process.Kill()
		_ = c.Wait()
		return b[:max], ErrOutputTooLarge
	}
	return b, c.Wait()
}

// output : c.Output() borné (sortie plafonnée à max octets, délai de la commande).
func (e Engine) output(c *exec.Cmd, max int64) ([]byte, error) { return readBounded(c, max) }

// execOut : exec dans le conteneur, sortie bornée, délai d.
func (e Engine) execOut(obj string, d time.Duration, max int64, stdin []byte, argv ...string) ([]byte, error) {
	a := e.ExecArgv(obj, false, false, argv...)
	c := e.newCmd(d, a)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	return readBounded(c, max)
}

// cappedBuf : tampon qui garde au plus max octets et jette le reste (messages d'erreur d'une commande : jamais plus que le plafond en mémoire).
type cappedBuf struct {
	b   bytes.Buffer
	max int
}

func (c *cappedBuf) Write(p []byte) (int, error) {
	if r := c.max - c.b.Len(); r > 0 {
		if len(p) < r {
			r = len(p)
		}
		c.b.Write(p[:r])
	}
	return len(p), nil
}
