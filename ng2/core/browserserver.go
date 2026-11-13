package core

import (
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Serveur web des fichiers pour le navigateur (#106) ───────────────────
//
// L'extension Chrome refuse les URL file:// ; le hub sert donc les fichiers
// des projets en HTTP, en lecture seule. Le chemin de l'URL est le chemin
// interne : /share/MFS/ROOT/x.html → http://localhost:8765/share/MFS/ROOT/x.html.
// Chrome tourne sur la machine du launcher (sous WSL2, le localhost de Windows
// est redirigé vers WSL) ; les conteneurs voient le stockage partagé au même
// chemin que le hub, un seul serveur les couvre tous.

const (
	defaultBrowserListen = "127.0.0.1:8765"
	// exe Windows sans listen (#110) : le hub Linux sous WSL partage le
	// localhost de Windows et tient 8765 ; le port voisin les laisse cohabiter
	defaultBrowserListenWindows = "127.0.0.1:8766"
	browserHealthPath           = "/__claude-launcher__"
	// système du launcher qui sert (réponse de browserHealthPath) : sous
	// Windows, le hub Linux de WSL partage le localhost du poste (#110)
	browserOSHeader = "X-Claude-Launcher-OS"
)

// BrowserServer : defaults.browser_server / hubs.<machine>.browser_server.
type BrowserServer struct {
	Listen string   `yaml:"listen"` // hôte:port, port seul, ou off
	URL    string   `yaml:"url"`    // base annoncée au template (défaut : déduite de listen)
	Allow  []string `yaml:"allow"`  // dossiers servis en plus des racines connues
}

// serverConf : réglage effectif.
type serverConf struct {
	Off    bool
	Listen string // hôte:port d'écoute
	Base   string // http://hôte:port, sans / final
	Allow  []string
	Source string
	// d'où vient l'adresse d'écoute (diag) : hubs.<machine>[.<système>]
	// .browser_server.listen, defaults.browser_server.listen, défaut…
	ListenSrc string
	PerOS     bool // surcharge hubs.<machine>.<système> présente
}

func isOff(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "false", "no", "non", "0", "disabled":
		return true
	}
	return false
}

func isLoopback(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// resolveBrowserServer : hubs.<machine> l'emporte champ par champ sur defaults.
func resolveBrowserServer(f backendsFile, host string) (serverConf, error) {
	return resolveBrowserServerFor(f, host, "")
}

// defaultListenFor : adresse d'écoute sans réglage sur goos.
func defaultListenFor(goos string) (string, string) {
	if goos == "windows" {
		return defaultBrowserListenWindows, "défaut Windows"
	}
	return defaultBrowserListen, "défaut"
}

// resolveBrowserServerFor : réglage effectif sur le système goos ("" : sans
// surcharge par système, défaut commun). hubs.<machine>.<goos>.browser_server
// l'emporte champ par champ sur hubs.<machine>.browser_server, qui l'emporte
// sur defaults.browser_server. Sous Windows, sans listen : 127.0.0.1:8766.
func resolveBrowserServerFor(f backendsFile, host, goos string) (serverConf, error) {
	var s BrowserServer
	src, lsrc := "défaut", ""
	if d := f.Defaults.BrowserServer; d != nil {
		s, src = *d, "defaults.browser_server"
		if s.Listen != "" {
			lsrc = src
		}
	}
	merge := func(o BrowserServer, blk string) {
		if o.Listen != "" {
			s.Listen, lsrc = o.Listen, blk
		}
		if o.URL != "" {
			s.URL = o.URL
		}
		if o.Allow != nil {
			s.Allow = o.Allow
		}
		src = blk
	}
	v := pickHubOS(f.Hubs, host, goos)
	if v.ok && v.base.BrowserServer != nil {
		merge(*v.base.BrowserServer, v.src("browser_server", false))
	}
	perOS := v.ov != nil && v.ov.BrowserServer != nil
	if perOS {
		merge(*v.ov.BrowserServer, v.src("browser_server", true))
	}
	if lsrc == "" {
		lsrc = src // aucun listen déclaré : l'erreur éventuelle vise le bloc
	}
	c := serverConf{Listen: strings.TrimSpace(s.Listen), Allow: s.Allow, Source: src, ListenSrc: lsrc + ".listen", PerOS: perOS}
	if isOff(c.Listen) {
		c.Off = true
		return c, nil
	}
	if c.Listen == "" {
		c.Listen, c.ListenSrc = defaultListenFor(goos)
	} else if !strings.Contains(c.Listen, ":") {
		c.Listen = "127.0.0.1:" + c.Listen // port seul
	}
	h, port, err := net.SplitHostPort(c.Listen)
	if n, e := strconv.Atoi(port); err != nil || e != nil || n < 1 || n > 65535 {
		return serverConf{Off: true, Source: src}, fmt.Errorf("%s %q : hôte:port attendu", c.ListenSrc, s.Listen)
	}
	if u := strings.TrimRight(strings.TrimSpace(s.URL), "/"); u != "" {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return serverConf{Off: true, Source: src}, fmt.Errorf("%s.url %q : http://… attendu", src, s.URL)
		}
		c.Base = u
	} else {
		bh := h
		switch {
		case h == "" || h == "0.0.0.0" || h == "::":
			bh = hostShort(host)
		case isLoopback(h):
			bh = "localhost"
		}
		c.Base = "http://" + net.JoinHostPort(bh, port)
	}
	return c, nil
}

func hostShort(h string) string {
	if h == "" {
		return "localhost"
	}
	s, _, _ := strings.Cut(h, ".")
	return s
}

// dialAddr : adresse locale à interroger pour l'état (écoute sur toutes les
// interfaces → boucle locale).
func (c serverConf) dialAddr() string {
	h, port, _ := net.SplitHostPort(c.Listen)
	if h == "" || h == "0.0.0.0" || h == "::" {
		h = "127.0.0.1"
	}
	return net.JoinHostPort(h, port)
}

// allowedHosts : valeurs acceptées pour l'en-tête Host (anti DNS rebinding :
// une page piégée qui fait pointer son nom vers 127.0.0.1 garde son Host).
func (c serverConf) allowedHosts(machine string) map[string]bool {
	m := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	if h, _, err := net.SplitHostPort(c.Listen); err == nil && h != "" {
		m[strings.ToLower(h)] = true
	}
	if i := strings.Index(c.Base, "://"); i >= 0 {
		hp := c.Base[i+3:]
		hp, _, _ = strings.Cut(hp, "/")
		if h, _, err := net.SplitHostPort(hp); err == nil {
			hp = h
		}
		m[strings.ToLower(strings.Trim(hp, "[]"))] = true
	}
	if machine != "" {
		m[strings.ToLower(machine)] = true
		m[strings.ToLower(hostShort(machine))] = true
	}
	return m
}

// browserServer : réglage effectif de ce launcher, sur son système (surcharge
// hubs.<machine>.<système> comprise).
func browserServer() (serverConf, error) {
	return resolveBrowserServerFor(readBackendsFile(), hubName(), pathsGOOS)
}

// ── Gestionnaire HTTP ─────────────────────────────────────────────────────

type fileServer struct {
	hosts map[string]bool
	roots func() []string // racines servies (absolues)
	token func() string   // jeton d'appariement du hub (browserpair.go)
	// chemin de l'URL (vu des sessions, Linux) → chemin local (#110, pathmap.go) ;
	// nil : le même
	local func(string) string
}

func (s fileServer) toLocal(p string) string {
	if s.local == nil {
		return p
	}
	return s.local(p)
}

// under : p est-il r ou dans r ?
func under(p, r string) bool {
	r = strings.TrimRight(r, "/")
	return r == "" || p == r || strings.HasPrefix(p, r+"/")
}

// underLocal : under pour un chemin local, de forme Windows comprise (casse et
// séparateurs indifférents).
func underLocal(p, r string) bool {
	if !winPathForm(r) {
		return under(p, r)
	}
	_, ok := cutLocal(p, r)
	return ok
}

// allowed : p (chemin propre de l'URL) est sous une racine, liens symboliques
// résolus compris : un lien qui sort des racines est refusé. Rend le chemin
// local du fichier.
func (s fileServer) allowed(p string) (string, bool) {
	roots := s.roots()
	in := false
	for _, r := range roots {
		if under(p, r) {
			in = true
			break
		}
	}
	if !in {
		return "", false
	}
	real, err := filepath.EvalSymlinks(s.toLocal(p))
	if err != nil {
		return "", false
	}
	for _, r := range roots {
		lr := s.toLocal(r)
		if underLocal(real, lr) {
			return real, true
		}
		if rr, err := filepath.EvalSymlinks(lr); err == nil && underLocal(real, rr) {
			return real, true
		}
	}
	return "", false
}

var textTypes = map[string]bool{".md": true, ".markdown": true, ".yaml": true, ".yml": true, ".log": true, ".txt": true, ".go": true, ".sh": true, ".py": true, ".toml": true, ".ini": true, ".conf": true}

func (s fileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !s.hosts[strings.ToLower(strings.Trim(host, "[]"))] {
		http.Error(w, "hôte refusé", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "lecture seule", http.StatusMethodNotAllowed)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if p == browserPairPath && s.token != nil {
		s.servePair(w, r)
		return
	}
	if p == browserHealthPath {
		w.Header().Set(browserOSHeader, runtime.GOOS)
		_, _ = io.WriteString(w, NgName+" "+NgVersion+"\n")
		return
	}
	real, ok := s.allowed(p)
	if !ok {
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(real)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, escapePath(p)+"/", http.StatusMovedPermanently)
			return
		}
		if idx := filepath.Join(real, "index.html"); fileExists(idx) {
			real = idx
			if st, err = os.Stat(real); err != nil {
				http.NotFound(w, r)
				return
			}
		} else {
			s.listDir(w, p, real)
			return
		}
	}
	f, err := os.Open(real)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if textTypes[strings.ToLower(filepath.Ext(real))] {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// escapePath : chemin encodé segment par segment.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, x := range segs {
		segs[i] = url.PathEscape(x)
	}
	return strings.Join(segs, "/")
}

func (s fileServer) listDir(w http.ResponseWriter, p, real string) {
	ents, err := os.ReadDir(real)
	if err != nil {
		http.Error(w, "dossier illisible", http.StatusNotFound)
		return
	}
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return ents[i].Name() < ents[j].Name()
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t := html.EscapeString(p)
	fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>%s</title><style>body{font:14px system-ui,sans-serif;margin:16px}a{text-decoration:none}li{margin:2px 0}</style><h1>%s</h1><ul>", t, t)
	if p != "/" {
		fmt.Fprint(w, `<li><a href="../">../</a></li>`)
	}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		fmt.Fprintf(w, `<li><a href="%s">%s</a></li>`, html.EscapeString(url.PathEscape(e.Name())+strings.TrimPrefix(n, e.Name())), html.EscapeString(n))
	}
	fmt.Fprint(w, "</ul>")
}

// ── Cycle de vie ──────────────────────────────────────────────────────────

func browserServerStatusPath() string { return filepath.Join(CacheDir(), "browser-server.status") }

func setServerStatus(msg string) {
	_ = os.MkdirAll(CacheDir(), 0o755)
	_ = os.WriteFile(browserServerStatusPath(), []byte(time.Now().Format("2006-01-02 15:04:05")+" "+msg+"\n"), 0o644)
}

// cachedRoots : racines servies, relues toutes les 5 s (config modifiable à chaud).
func cachedRoots(allow func() []string) func() []string {
	var mu sync.Mutex
	var at time.Time
	var roots []string
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > 5*time.Second {
			roots = servedRoots(knownRoots(), allow())
			at = time.Now()
		}
		return roots
	}
}

// servedRoots : racines absolues, sans / final, dédoublonnées.
func servedRoots(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lists {
		for _, r := range l {
			// racine dans l'espace des URL : chemin vu des sessions (pathmap.go)
			r = path.Clean(RemotePath(r))
			if !strings.HasPrefix(r, "/") || r == "/" || seen[r] {
				continue // « / » refusé : tout le système de fichiers
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// RunBrowserServer : lancé avec le pane des cartes du hub (supervisé). Réglage
// relu toutes les 10 s : changement d'adresse ou off pris en compte sans
// relance ; port occupé → nouvel essai.
func RunBrowserServer() {
	var cur serverConf
	var srv *http.Server
	var allow []string
	var amu sync.Mutex
	roots := cachedRoots(func() []string { amu.Lock(); defer amu.Unlock(); return allow })
	for {
		c, err := browserServer()
		if err != nil {
			setServerStatus("config invalide : " + err.Error())
		}
		amu.Lock()
		allow = c.Allow
		amu.Unlock()
		if srv != nil && (c.Off || c.Listen != cur.Listen || c.Base != cur.Base) {
			_ = srv.Close()
			srv = nil
		}
		if srv == nil && !c.Off && runtime.GOOS == "windows" {
			// port tenu par le hub Linux de WSL (localhost partagé) : ne pas le lui disputer
			if v, hos, perr := probeBrowserServerOS(c); perr == nil {
				if who, foreign := foreignHolder(runtime.GOOS, hos); foreign {
					setServerStatus("port " + c.Listen + " tenu par " + who + " (" + v + ") : pas de serveur propre à ce launcher")
					time.Sleep(10 * time.Second)
					continue
				}
			}
		}
		if srv == nil && !c.Off {
			ln, err := net.Listen("tcp", c.Listen)
			if err != nil {
				setServerStatus("écoute impossible sur " + c.Listen + " : " + err.Error())
			} else {
				srv = &http.Server{Handler: fileServer{hosts: c.allowedHosts(hubName()), roots: roots, token: browserToken, local: LocalPath}, ReadHeaderTimeout: 10 * time.Second}
				cur = c
				setServerStatus("à l'écoute sur " + c.Listen + " (" + c.Base + ")")
				go func(s *http.Server) { _ = s.Serve(ln) }(srv)
			}
		} else if c.Off && err == nil {
			setServerStatus("désactivé (" + c.Source + ")")
		}
		time.Sleep(10 * time.Second)
	}
}

// probeBrowserServer : version annoncée par le serveur à l'écoute, ou erreur.
func probeBrowserServer(c serverConf) (string, error) {
	v, _, err := probeBrowserServerOS(c)
	return v, err
}

// foreignHolder : le serveur qui tient le port est-il celui d'un autre
// launcher que ce système en lancerait un (logique pure) ? holderOS : son
// en-tête X-Claude-Launcher-OS ("" : launcher antérieur, qui ne le donne pas).
// Sous Windows, le hub Linux de WSL partage le localhost du poste : le même
// port, réglé dans la configuration partagée, y est vu tenu.
func foreignHolder(myOS, holderOS string) (string, bool) {
	switch {
	case holderOS == myOS:
		return "", false
	case myOS == "windows" && holderOS == "":
		return "un autre launcher (sans indication de système : vraisemblablement le hub Linux sous WSL, qui partage le localhost de Windows)", true
	case myOS == "windows":
		return "le hub " + holderOS + " (WSL partage le localhost de Windows)", true
	case holderOS == "":
		return "", false // hors Windows : comportement d'avant
	}
	return "le launcher " + holderOS + " du même poste", true
}

// probeBrowserServerOS : version et système du launcher qui sert, ou erreur.
func probeBrowserServerOS(c serverConf) (string, string, error) {
	cl := http.Client{Timeout: 1500 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodGet, "http://"+c.dialAddr()+browserHealthPath, nil)
	req.Host = "localhost"
	resp, err := cl.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(b), NgName) {
		return "", "", fmt.Errorf("port occupé par un autre service (HTTP %d)", resp.StatusCode)
	}
	return strings.TrimSpace(string(b)), resp.Header.Get(browserOSHeader), nil
}

// listenOriginLine : d'où vient l'adresse d'écoute, pour ce système (diag).
// Rien sans surcharge par système hors Windows : sortie d'avant.
func listenOriginLine(c serverConf, goos string) string {
	if !c.PerOS && goos != "windows" {
		return ""
	}
	sys := osKey(goos)
	if sys == "" {
		sys = goos
	}
	return "réglage pour " + sys + " : listen de " + c.ListenSrc
}

// ownPortHint : comment donner à ce launcher un serveur propre quand le port
// est tenu par le launcher d'un autre système du même poste.
func ownPortHint(machine, goos string) string {
	sys := osKey(goos)
	if sys == "" {
		sys = goos
	}
	port := "8766"
	if goos != "windows" {
		port = "8765"
	}
	return "Pour un serveur propre à ce launcher : un autre port pour ce système, hubs." + machine + "." + sys +
		".browser_server.listen (ex. 127.0.0.1:" + port + ") ; hubs." + machine + ".browser_server vaut pour les deux systèmes du même nom de machine, voir le README (section Windows)."
}

// PrintBrowserServer : section de « claude-launcher-ng2 backends ».
func PrintBrowserServer() {
	c, err := browserServer()
	fmt.Printf("\nServeur web pour le navigateur (%s) :\n", c.Source)
	if err != nil {
		fmt.Println("  ⚠ " + err.Error() + " : serveur désactivé")
		return
	}
	if c.Off {
		fmt.Println("  désactivé : les fichiers sont donnés en file:// (browser_paths)")
		return
	}
	fmt.Printf("  écoute %s, URL %s/<chemin>\n", c.Listen, c.Base)
	if l := listenOriginLine(c, runtime.GOOS); l != "" {
		fmt.Println("  " + l)
	}
	if v, hos, err := probeBrowserServerOS(c); err == nil {
		if who, foreign := foreignHolder(runtime.GOOS, hos); foreign {
			fmt.Printf("  ⚠ port tenu par %s : %s\n", who, v)
			fmt.Println("    → ce n'est pas le serveur de ce launcher. Il sert les mêmes chemins (ceux des backends) : le navigateur de ce poste peut s'en servir tant qu'il tourne.")
			fmt.Println("      " + ownPortHint(hubName(), runtime.GOOS))
		} else {
			fmt.Println("  actif : " + v)
		}
	} else {
		fmt.Println("  ⚠ ne répond pas : " + err.Error())
		if b, e := os.ReadFile(browserServerStatusPath()); e == nil {
			fmt.Println("  dernier état : " + strings.TrimSpace(string(b)))
		} else {
			fmt.Println("  il est lancé par le pane des cartes du hub")
		}
	}
	rs := servedRoots(knownRoots(), c.Allow)
	if len(rs) == 0 {
		fmt.Println("  aucune racine servie")
	}
	for _, r := range rs {
		fmt.Println("  sert " + r)
	}
}
