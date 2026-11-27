package core

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"os/exec"
	"path"
	"strings"

	"claude-launcher-ng2/platform"
)

// /files d'un projet d'utilisateur (LOT-3.2 tâche 15, contrat §6) : le launcher ne lit RIEN du stockage de l'utilisateur lui-même ; chaque lecture est
// un « cat » ou « ls » exécuté DANS le conteneur du projet, sous l'uid du traitement. Mêmes refus que les projets de l'admin (éléments sensibles,
// « .. »), plafonds par utilisateur (4 flux, 40 exec), taille bornée. Jamais appelé pour un projet arrêté (404 décidé par l'appelant, sans commande).

const maxUserFileBytes = 256 << 20

// ServeUserFile : sert <projet>/<sub> d'un projet EN MARCHE.
func (e Engine) ServeUserFile(w http.ResponseWriter, r *http.Request, cat, name, sub string, g *UserGate) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "méthode refusée", http.StatusMethodNotAllowed)
		return
	}
	clean := strings.TrimPrefix(path.Clean("/"+sub), "/")
	if sensitiveRel(clean) || strings.Contains(sub, "\x00") {
		http.NotFound(w, r)
		return
	}
	relStream, err := g.AcquireStream(e.B.uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	defer relStream()
	relExec, err := g.AcquireExec(e.B.uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	defer relExec()
	obj := e.ObjectName(cat, name)
	abs := UserProjectDir(cat, name)
	if clean != "" {
		abs += "/" + clean
	}
	if strings.HasSuffix(sub, "/") || clean == "" { // dossier : liste
		e.serveUserDir(w, r, obj, abs, clean)
		return
	}
	argv := e.ExecArgv(obj, false, false, "cat", "--", abs)
	cmd := platform.NoWindow(exec.Command(argv[0], argv[1:]...))
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		http.Error(w, "lecture impossible", http.StatusBadGateway)
		return
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	br := bufio.NewReader(io.LimitReader(out, maxUserFileBytes))
	head, _ := br.Peek(512)
	if len(head) == 0 { // vide ou absent : « cat » a échoué avant toute sortie → 404 (un fichier vide est servi vide)
		if err := cmd.Wait(); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	ct := mime.TypeByExtension(path.Ext(clean))
	if ct == "" {
		ct = http.DetectContentType(head)
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", filesCSP)
	h.Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, br)
}

func (e Engine) serveUserDir(w http.ResponseWriter, r *http.Request, obj, abs, clean string) {
	argv := e.ExecArgv(obj, false, false, "ls", "-1Ap", "--", abs)
	out, err := platform.NoWindow(exec.Command(argv[0], argv[1:]...)).Output()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=utf-8><title>" + html.EscapeString("/"+clean) + "</title><pre>")
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l == "" || n >= 2000 || sensitiveRel(path.Join(clean, strings.TrimSuffix(l, "/"))) {
			continue
		}
		n++
		fmt.Fprintf(&b, "<a href=\"%s\">%s</a>\n", html.EscapeString((&urlPath{l}).String()), html.EscapeString(l))
	}
	b.WriteString("</pre>")
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", filesCSP)
	h.Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, b.String())
}

type urlPath struct{ s string }

func (u *urlPath) String() string {
	r := strings.NewReplacer("%", "%25", " ", "%20", "#", "%23", "?", "%3F", "\"", "%22", "<", "%3C", ">", "%3E")
	return r.Replace(u.s)
}
