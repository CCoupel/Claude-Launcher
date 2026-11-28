package core

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// /files d'un projet d'utilisateur (LOT-3.2 tâche 15, contrat §6) : le launcher ne lit RIEN du stockage de l'utilisateur lui-même ; chaque lecture est
// un « cat » ou « ls » exécuté DANS le conteneur du projet, sous l'uid du traitement. Mêmes refus que les projets de l'admin (éléments sensibles,
// « .. »), plafonds par utilisateur (4 flux, 40 exec), taille bornée. Jamais appelé pour un projet arrêté (404 décidé par l'appelant, sans commande).

const maxUserFileBytes = 256 << 20

// userResolveScript : chemin RÉEL (liens suivis) dans le conteneur : doit rester sous le dossier réel du projet, être un fichier ordinaire ou un dossier
// (jamais FIFO, périphérique ni socket). Sort trois lignes : genre (f|d), chemin réel, dossier réel du projet.
const userResolveScript = `b=$(realpath -e -- "$2") || exit 3; p=$(realpath -e -- "$1") || exit 3
case "$p" in "$b"|"$b"/*) ;; *) exit 4 ;; esac
if [ -f "$p" ]; then k=f; elif [ -d "$p" ]; then k=d; else exit 5; fi
printf '%s\n%s\n%s\n' "$k" "$p" "$b"`

// resolveUserPath : (genre, chemin réel, chemin relatif au dossier réel du projet), ok faux si absent, hors du projet, d'un autre genre ou sensible.
func (e Engine) resolveUserPath(obj, abs, projDir string) (kind, real, rel string, ok bool) {
	out, err := e.execOut(obj, userReadTimeout, 4<<10, nil, "sh", "-c", userResolveScript, "sh", abs, projDir)
	if err != nil {
		return "", "", "", false
	}
	l := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(l) != 3 || (l[0] != "f" && l[0] != "d") || !strings.HasPrefix(l[1], "/") || l[1] != l[2] && !strings.HasPrefix(l[1], strings.TrimRight(l[2], "/")+"/") {
		return "", "", "", false
	}
	rel = strings.TrimPrefix(strings.TrimPrefix(l[1], l[2]), "/")
	if sensitiveRel(rel) { // contrôlé aussi sur le chemin RÉEL : un lien vers .env, .claude… ne contourne rien (T21)
		return "", "", "", false
	}
	return l[0], l[1], rel, true
}

// ServeUserFile : sert <projet>/<sub> d'un projet EN MARCHE. Annulé avec la requête ; premier octet sous 30 s, durée totale 10 min, 256 Mio au plus.
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
	obj, projDir := e.ObjectName(cat, name), UserProjectDir(cat, name)
	abs := projDir
	if clean != "" {
		abs += "/" + clean
	}
	kind, real, _, ok := e.resolveUserPath(obj, abs, projDir)
	if !ok || (kind == "d") != (strings.HasSuffix(sub, "/") || clean == "") {
		http.NotFound(w, r)
		return
	}
	if kind == "d" {
		e.serveUserDir(w, r, obj, real, clean)
		return
	}
	cmd := e.newCmd(userServeTimeout, e.ExecArgv(obj, false, false, "cat", "--", real))
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		http.Error(w, "lecture impossible", http.StatusBadGateway)
		return
	}
	done := make(chan struct{})
	defer func() { close(done); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	opened := time.AfterFunc(userOpenTimeout, func() { _ = cmd.Process.Kill() }) // aucun octet à temps (FIFO, système de fichiers bloqué)
	defer opened.Stop()
	go func() {
		select {
		case <-r.Context().Done(): // client parti : le « cat » est tué, les compteurs libérés
			_ = cmd.Process.Kill()
		case <-done:
		}
	}()
	br := bufio.NewReader(io.LimitReader(out, maxUserFileBytes))
	head, _ := br.Peek(512)
	opened.Stop()
	if len(head) == 0 { // vide ou échec : un fichier vide est servi vide, une erreur de « cat » est un 404
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

func (e Engine) serveUserDir(w http.ResponseWriter, r *http.Request, obj, real, clean string) {
	out, err := e.execOut(obj, userReadTimeout, maxSmallOut, nil, "ls", "-1Ap", "--", real)
	if err != nil && !errors.Is(err, ErrOutputTooLarge) {
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
