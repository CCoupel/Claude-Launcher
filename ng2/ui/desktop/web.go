package desktop

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ── Mode web : la même interface dans le navigateur ───────────────────────
//
// « claude-launcher-ng2 desktop --web » sert l'interface (mêmes fichiers que la
// fenêtre Wails) sur 127.0.0.1, protégée par un jeton aléatoire passé dans
// l'URL puis gardé en cookie. Les appels arrivent en POST /api/call, les
// événements partent en flux SSE (/api/events). Sert aux tests (navigateur
// sans tête, Linux sans WebKitGTK) et d'interface de repli sous Linux quand le
// binaire n'est pas compilé avec Wails.

type webBridge struct {
	app   *App
	token string

	mu   sync.Mutex
	subs map[chan []byte]bool
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (w *webBridge) emit(name string, data any) {
	b, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subs {
		select {
		case ch <- b:
		case <-time.After(2 * time.Second): // client bloqué : abandonné
			delete(w.subs, ch)
			close(ch)
		}
	}
}

func (w *webBridge) authorized(r *http.Request) bool {
	if c, err := r.Cookie("cl_token"); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(w.token)) == 1 {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(w.token)) == 1
}

func (w *webBridge) handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(frontendFS, "frontend")
	files := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if !w.authorized(r) {
				http.Error(rw, "jeton manquant : ouvrir l'URL affichée au lancement", http.StatusForbidden)
				return
			}
			http.SetCookie(rw, &http.Cookie{Name: "cl_token", Value: w.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}
		rw.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(rw, r)
	})
	mux.HandleFunc("/api/call", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !w.authorized(r) {
			http.Error(rw, "refusé", http.StatusForbidden)
			return
		}
		var req struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := w.app.Dispatch(req.Method, req.Args)
		out := map[string]any{"result": res}
		if err != nil {
			out["error"] = err.Error()
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(out)
	})
	mux.HandleFunc("/api/events", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorized(r) {
			http.Error(rw, "refusé", http.StatusForbidden)
			return
		}
		fl, ok := rw.(http.Flusher)
		if !ok {
			http.Error(rw, "flux impossible", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-store")
		ch := make(chan []byte, 4096)
		w.mu.Lock()
		w.subs[ch] = true
		w.mu.Unlock()
		defer func() {
			w.mu.Lock()
			if w.subs[ch] {
				delete(w.subs, ch)
				close(ch)
			}
			w.mu.Unlock()
		}()
		fmt.Fprint(rw, ": ok\n\n")
		fl.Flush()
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				fmt.Fprint(rw, ": ping\n\n")
				fl.Flush()
			case b, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(rw, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
	return mux
}

// RunWeb : interface servie sur addr (« 127.0.0.1:0 » : port libre) ; open :
// ouvrir le navigateur par défaut. Bloque jusqu'à la sortie.
func RunWeb(addr string, open bool) error {
	w := &webBridge{token: newToken(), subs: map[chan []byte]bool{}}
	if t := os.Getenv("CL_DESKTOP_TOKEN"); t != "" { // tests
		w.token = t
	}
	a := NewApp(w.emit)
	w.app = a
	// relance après une mise à jour : l'ancien processus vient de libérer le port
	ln, err := listenRetry(addr, 10*time.Second)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), w.token)
	fmt.Println("Claude Launcher (interface web) :", url)
	fmt.Println("Ctrl+C pour arrêter (les sessions des projets continuent dans tmux).")
	srv := &http.Server{Handler: w.handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan struct{})
	quitHook = func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = srv.Close()
		}()
	}
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	// mise à jour installée : même adresse et même jeton pour le nouveau
	// binaire, port libéré avant de le lancer ; l'onglet ouvert se reconnecte
	// (flux d'événements) au lieu d'en ouvrir un second
	restartApp = func() {
		bound := ln.Addr().String()
		_ = srv.Close()
		a.Stop()
		if err := relaunch("desktop", "--web", "--addr", bound, "--no-open"); err != nil {
			fmt.Fprintln(os.Stderr, "relance impossible :", err)
		}
		os.Exit(0)
	}
	restartEnv = []string{"CL_DESKTOP_TOKEN=" + w.token}
	a.Start()
	if open {
		openURL(url)
	}
	<-done
	a.Stop()
	return nil
}

func openURL(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

// listenRetry : net.Listen, réessayé jusqu'à d si l'adresse est encore prise
// (port fixe libéré à l'instant par le processus précédent).
func listenRetry(addr string, d time.Duration) (net.Listener, error) {
	end := time.Now().Add(d)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil || time.Now().After(end) || strings.HasSuffix(addr, ":0") {
			return ln, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}
