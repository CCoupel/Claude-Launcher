package desktop

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// ── Onglet d'un projet ouvert ─────────────────────────────────────────────
//
// Ouverture : la sortie (conteneur, bootstrap, questions du groupe de serveurs
// ou du propriétaire) s'affiche dans un terminal d'ouverture, où l'on répond.
// Puis un client tmux en mode contrôle s'attache à la session du projet : un
// terminal xterm.js par pane, placé à la géométrie de tmux, que la mise en page
// de core (leader et cdp en haut, teammates dessous, ratios de TeamRows) règle
// comme dans la coque. Les questions /init-project et de reprise sont posées
// par le script de démarrage, dans le pane du leader, comme sous Linux.

type projTerm struct {
	a               *App
	path, key, name string
	color           string // couleur tmux 256 du projet
	mode            core.LaunchMode
	host            bool        // projet sur l'hôte (Linux), tmux local dédié
	sock, sess, win string      // hôte : socket et session ; win : cible de la fenêtre
	e               core.Engine // conteneur : moteur…
	ctr             string      // … et conteneur (pod)
	lines           chan string // réponses tapées dans le terminal d'ouverture
	launchMu        sync.Mutex
	launchBuf       []byte
	line            string            // ligne en cours de saisie (terminal d'ouverture)
	pty             platform.Terminal // onglet « login » : pseudo-terminal local
	launchLog       strings.Builder   // sortie d'ouverture, rejouée à une resynchronisation
	mu              sync.Mutex
	state, errMsg   string
	readOnly        bool
	owner           string
	client          *tmuxClient
	cols, rows      int
	resized         bool
	layout          *LayoutMsg
	busy            bool
	seen            map[string]bool // panes déjà amorcés (contenu capturé)
	seeding         map[string]bool
	carry           map[string][]byte
	out             []Chunk
	flushCh         chan struct{}
	stop            chan struct{}
	detaching       bool
	members         map[string]core.TeamMember
	leader          string
	watch           watchState
	reads, finds    int32           // list-panes et recherches de team (tests)
	infos           []core.PaneInfo // panes au dernier tour (carte Team, sans appel tmux)
	stopOnce        sync.Once
	tmuxVer         string          // version du tmux du projet (drapeaux de client : 3.2)
	flagged         map[string]bool // clients mis en ignore-size par l'interface (#126)
	reseed          *time.Timer     // recapture des panes après un redimensionnement
}

type watchState struct {
	prevSig, prevRelayout string
	prevCount, beat, wait int
	prevW, prevH          int
}

// Chunk : sortie d'un pane pour l'interface (Reset : contenu capturé, l'écran
// est d'abord effacé).
type Chunk struct {
	Pane  string `json:"pane"`
	Data  string `json:"data"`
	Reset bool   `json:"reset,omitempty"`
}

// PaneView : un pane tel que l'interface le place (cellules) et le colore.
type PaneView struct {
	ID     string `json:"id"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Bg     string `json:"bg"`     // fond (#rrggbb)
	Name   string `json:"name"`   // agent (team-lead, go-dev…) ou commande
	Role   string `json:"role"`   // leader, cdp, actif, inactif, ""
	Active bool   `json:"active"` // pane actif de tmux
}

// LayoutMsg : géométrie de la fenêtre du projet.
type LayoutMsg struct {
	Cols   int        `json:"cols"`
	Rows   int        `json:"rows"`
	Zoomed bool       `json:"zoomed"`
	Leader string     `json:"leader"`
	Panes  []PaneView `json:"panes"`
}

func (t *projTerm) info() TabInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TabInfo{Path: t.path, Name: t.name, Color: core.Colour256Hex(t.color), Busy: t.busy, State: t.state, ReadOnly: t.readOnly}
}

func (t *projTerm) isReadOnly() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readOnly
}

func (t *projTerm) ownerID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.owner
}

func (t *projTerm) emit(kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["path"], data["kind"] = t.path, kind
	t.a.emit("term", data)
}

func (t *projTerm) setState(state, msg string) {
	t.mu.Lock()
	t.state, t.errMsg = state, msg
	ro := t.readOnly
	t.mu.Unlock()
	t.emit("status", map[string]any{"state": state, "msg": msg, "readOnly": ro})
	t.a.changed()
}

// ── Terminal d'ouverture ──────────────────────────────────────────────────

// Write : sortie de l'ouverture (io.Writer), vers le terminal d'ouverture.
func (t *projTerm) Write(p []byte) (int, error) {
	t.launchMu.Lock()
	buf := append(t.launchBuf, p...)
	ok, rest := utf8Carry(buf)
	t.launchBuf = append([]byte(nil), rest...)
	t.launchLog.Write(ok)
	t.launchMu.Unlock()
	if len(ok) > 0 {
		t.emit("launch", map[string]any{"data": string(ok)})
	}
	return len(p), nil
}

// launchInput : frappe dans le terminal d'ouverture : écho et édition de ligne,
// la ligne part à Entrée (réponse aux questions de l'ouverture).
func (t *projTerm) launchInput(data string) {
	t.mu.Lock()
	pty := t.pty
	t.mu.Unlock()
	if pty != nil {
		_, _ = pty.Write([]byte(data))
		return
	}
	var echo strings.Builder
	t.launchMu.Lock()
	line := []rune(t.line)
	for _, r := range data {
		switch {
		case r == '\r' || r == '\n':
			echo.WriteString("\r\n")
			select {
			case t.lines <- string(line) + "\n":
			default:
			}
			line = nil
		case r == 0x7f || r == 0x08:
			if len(line) > 0 {
				line = line[:len(line)-1]
				echo.WriteString("\b \b")
			}
		case r == 0x03: // Ctrl+C : ligne abandonnée
			line = nil
			echo.WriteString("^C\r\n")
		case r >= 0x20:
			line = append(line, r)
			echo.WriteRune(r)
		}
	}
	t.line = string(line)
	t.launchMu.Unlock()
	if echo.Len() > 0 {
		t.emit("launch", map[string]any{"data": echo.String()})
	}
}

// lineReader : io.Reader des lignes tapées (pour bufio, core.Asker).
type lineReader struct {
	t   *projTerm
	cur string
}

func (r *lineReader) Read(p []byte) (int, error) {
	for r.cur == "" {
		select {
		case l := <-r.t.lines:
			r.cur = l
		case <-r.t.stop:
			return 0, io.EOF
		}
	}
	n := copy(p, r.cur)
	r.cur = r.cur[n:]
	return n, nil
}

// ── Ouverture ─────────────────────────────────────────────────────────────

func newProjTerm(a *App, n core.Node, mode core.LaunchMode) *projTerm {
	t := &projTerm{a: a, path: n.Path, key: n.Key, name: n.Name, color: a.cfg.ProjectColor(n.Key), mode: mode,
		lines: make(chan string, 16), state: "opening", seen: map[string]bool{}, seeding: map[string]bool{},
		carry: map[string][]byte{}, flushCh: make(chan struct{}, 1), stop: make(chan struct{})}
	return t
}

// run : ouverture complète, puis attente de la fin du client.
func (t *projTerm) run() {
	go t.flusher()
	if err := t.prepare(); err != nil {
		fmt.Fprintf(t, "\r\n\033[1;31m⚠ %v\033[0m\r\n", strings.ReplaceAll(err.Error(), "\n", "\r\n"))
		t.setState("error", err.Error())
		return
	}
	if t.state == "closed" {
		return
	}
	if err := t.attach(); err != nil {
		fmt.Fprintf(t, "\r\n\033[1;31m⚠ attache : %v\033[0m\r\n", err)
		t.setState("error", err.Error())
		return
	}
	t.setState("ready", "")
	go t.watcher()
	<-t.client.Done()
	t.ended()
}

// prepare : backend, script de démarrage, conteneur ou session de l'hôte.
func (t *projTerm) prepare() error {
	cfg := t.a.cfg
	be, err := core.LaunchBackend(t.path)
	if err != nil {
		return err
	}
	synced := core.SyncLauncherEnv(cfg)
	start, copyInit := core.StartCommands(cfg, t.path, latestTemplate(), t.mode)
	if _, inCtr := core.EngineOf(be); !inCtr {
		return t.prepareHost(cfg, start, copyInit, synced)
	}
	script := []byte(core.CtrStartScript(cfg, t.path, start, t.mode, synced))
	op, err := core.OpenCtr(t.path, core.CtrOpenIO{
		Out:   t,
		In:    bufio.NewReader(&lineReader{t: t}),
		Start: func() []byte { return script },
		Size:  t.size,
	})
	if err != nil {
		return err
	}
	if op.Canceled {
		t.setState("closed", "ouverture annulée")
		t.a.removeTab(t.path, t)
		return nil
	}
	t.mu.Lock()
	t.e, t.ctr, t.readOnly = op.E, op.Name, op.ReadOnly
	if op.ReadOnly {
		t.owner = op.E.Owner(op.Name).Id
	}
	t.sess, t.win = core.CtrSession, "="+core.CtrSession+":"
	t.mu.Unlock()
	return nil
}

// deskSocket : serveur tmux local des projets sur l'hôte ouverts par
// l'interface native (Linux) ; CL_DESK_SOCKET le remplace (tests).
func deskSocket() string {
	if s := os.Getenv("CL_DESK_SOCKET"); s != "" {
		return s
	}
	return "claude-desk-ng2"
}

// hostSession : session tmux d'un projet sur l'hôte.
func hostSession(path string) string { return "p-" + core.CtrName(path) }

func (t *projTerm) localTmux(args ...string) *exec.Cmd {
	c := platform.NoWindow(exec.Command("tmux", append([]string{"-L", t.sock}, args...)...))
	c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
	return c
}

// prepareHost : session dans le tmux local dédié, créée si besoin, où le
// démarrage est tapé comme dans la coque.
func (t *projTerm) prepareHost(cfg core.Config, start, copyInit string, synced bool) error {
	if hostRefused() {
		return fmt.Errorf("exécution sur l'hôte impossible sous Windows : choisir un backend conteneur (F2)")
	}
	if who := core.ForeignOwner(t.path); who != "" {
		return fmt.Errorf("projet en cours dans un conteneur, piloté par %s — le fermer là-bas ou F2 pour le rejoindre sur ce backend", who)
	}
	t.mu.Lock()
	t.host, t.sock, t.sess = true, deskSocket(), hostSession(t.path)
	t.win = "=" + t.sess + ":"
	t.mu.Unlock()
	fmt.Fprintf(t, "\033[1m▲ %s — hôte (tmux %s)\033[0m\r\n", t.name, t.sock)
	if t.localTmux("has-session", "-t", "="+t.sess).Run() == nil {
		return nil
	}
	w, h := t.size()
	if out, err := t.localTmux("new-session", "-d", "-s", t.sess, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h), "-c", t.path).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux : %s", strings.TrimSpace(string(out)))
	}
	_ = t.localTmux("set-option", "-t", "="+t.sess, "status", "off").Run()
	_ = t.localTmux("set-option", "-p", "-t", t.win, "window-style", "bg=colour"+t.color).Run()
	keys := core.HostStartKeys(cfg, t.path, start, copyInit, synced)
	if out, err := t.localTmux("send-keys", "-t", t.win, keys, "Enter").CombinedOutput(); err != nil {
		return fmt.Errorf("send-keys : %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// size : taille demandée par l'interface (zone des terminaux), 200×50 à défaut.
func (t *projTerm) size() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cols > 0 && t.rows > 0 {
		return t.cols, t.rows
	}
	return 200, 50
}

// attach : client en mode contrôle sur la session du projet.
func (t *projTerm) attach() error {
	t.mu.Lock()
	ro := t.readOnly
	t.mu.Unlock()
	att := []string{"-C", "attach-session", "-t", "=" + t.sess}
	if ro {
		att = append(att, "-r")
	}
	var argv []string
	if t.host {
		argv = append([]string{"tmux", "-L", t.sock}, att...)
	} else {
		argv = t.e.ExecArgv(t.ctr, false, false, core.TmuxIn(att...)...)
	}
	c, err := startClient(argv, t.onNotif)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.client = c
	w, h := t.cols, t.rows
	t.mu.Unlock()
	if w > 0 && h > 0 {
		_ = c.Send("refresh-client", "-C", clientSize(w, h))
	}
	if t.host {
		_ = c.Send("set-option", "-t", "="+t.sess, "status", "off")
	}
	if _, err := c.Output("display-message", "-p", "ok"); err != nil {
		return fmt.Errorf("%v (%s)", err, c.Why())
	}
	if b, err := c.Output("display-message", "-p", "#{version}"); err == nil {
		t.mu.Lock()
		t.tmuxVer = strings.TrimSpace(string(b))
		t.mu.Unlock()
	}
	t.govern() // un autre client attaché (hub Linux) ne dispute pas la taille (#126)
	t.resync()
	return nil
}

// ended : le client s'est arrêté (session terminée, conteneur arrêté, détaché).
func (t *projTerm) ended() {
	t.mu.Lock()
	detaching := t.detaching
	t.mu.Unlock()
	t.closeStop()
	if detaching {
		return
	}
	why := t.client.Why()
	if !t.host && t.a.cfg.CtrStopOnExit && !t.isReadOnly() && !t.e.HasSession(t.ctr) {
		fmt.Fprintf(t, "\r\n⏻ claude terminé : arrêt du conteneur (CTR_STOP_ON_EXIT)\r\n")
		_ = t.e.Stop(t.path, t.ctr)
	}
	t.setState("closed", why)
	t.a.removeTab(t.path, t)
	t.a.setMsg("✓ " + t.name + " : " + why)
	go t.a.readCtr()
	go t.a.readSessions()
}

// detach : l'interface se ferme ou l'onglet est retiré ; la session continue.
func (t *projTerm) detach() {
	t.releaseOthers() // les autres clients retrouvent leur rôle dans la taille
	t.mu.Lock()
	t.detaching = true
	c := t.client
	t.mu.Unlock()
	t.closeStop()
	if c != nil {
		c.Close()
	}
}

// ── Notifications du mode contrôle (goroutine de lecture : jamais d'Output) ─

func (t *projTerm) onNotif(n Notif) {
	switch n.Name {
	case "output":
		t.mu.Lock()
		if t.seeding[n.Pane] || !t.seen[n.Pane] {
			t.mu.Unlock()
			return // avant la capture de son contenu : déjà dedans
		}
		data, rest := utf8Carry(append(t.carry[n.Pane], n.Data...))
		t.carry[n.Pane] = append([]byte(nil), rest...)
		t.push(Chunk{Pane: n.Pane, Data: string(data)})
		t.mu.Unlock()
	case "layout-change", "window-pane-changed", "session-window-changed", "window-add", "window-close", "unlinked-window-close", "pane-mode-changed":
		t.poke()
	}
}

// push : sortie en attente d'envoi (t.mu tenu).
func (t *projTerm) push(c Chunk) {
	if c.Reset {
		// contenu capturé : la sortie antérieure du pane est déjà dedans
		kept := t.out[:0]
		for _, o := range t.out {
			if o.Pane != c.Pane {
				kept = append(kept, o)
			}
		}
		t.out = kept
	} else if k := len(t.out) - 1; k >= 0 && t.out[k].Pane == c.Pane {
		t.out[k].Data += c.Data
		goto signal
	}
	t.out = append(t.out, c)
signal:
	select {
	case t.flushCh <- struct{}{}:
	default:
	}
}

// flusher : sortie des panes envoyée par paquets (au plus ~60 par seconde).
func (t *projTerm) flusher() {
	for {
		select {
		case <-t.stop:
			return
		case <-t.flushCh:
		}
		time.Sleep(16 * time.Millisecond)
		t.mu.Lock()
		out := t.out
		t.out = nil
		t.mu.Unlock()
		if len(out) > 0 {
			t.emit("out", map[string]any{"chunks": out})
		}
	}
}

// poke : relecture rapide de la fenêtre (mise en page changée).
var pokeKey sync.Map

func (t *projTerm) poke() {
	if ch, ok := pokeKey.Load(t); ok {
		select {
		case ch.(chan struct{}) <- struct{}{}:
		default:
		}
	}
}

// seed : amorce un pane avec son contenu actuel (historique et écran tirés de
// la grille de tmux, modes du terminal : seed.go) ; sa sortie est ignorée
// jusque-là (elle est dans la capture).
func (t *projTerm) seed(pane string) {
	c := t.client
	t.mu.Lock()
	t.seeding[pane], t.seen[pane] = true, true
	t.mu.Unlock()
	var m paneModes
	var hist string
	_ = c.sendCB([]string{"display-message", "-p", "-t", pane, modesFormat}, func(r Reply) {
		m = parseModes(string(r.Body))
	})
	_ = c.sendCB([]string{"capture-pane", "-p", "-e", "-t", pane, "-S", strconv.Itoa(-seedHistory), "-E", "-1"}, func(r Reply) {
		if !r.Err && m.hist > 0 && !m.alt { // sans historique, tmux renverrait la ligne 0
			hist = string(r.Body)
		}
	})
	err := c.sendCB([]string{"capture-pane", "-p", "-e", "-N", "-t", pane}, func(r Reply) {
		data := seedData(m, hist, string(r.Body))
		if r.Err {
			data = ""
		}
		t.mu.Lock()
		t.seeding[pane] = false
		t.carry[pane] = nil
		t.push(Chunk{Pane: pane, Data: data, Reset: true})
		t.mu.Unlock()
	})
	if err != nil {
		t.mu.Lock()
		t.seeding[pane] = false
		t.mu.Unlock()
	}
}

// resync : l'interface (re)construit l'onglet : sortie d'ouverture, mise en
// page et contenu de tous les panes.
func (t *projTerm) resync() {
	t.launchMu.Lock()
	log := t.launchLog.String()
	t.launchMu.Unlock()
	t.mu.Lock()
	st, msg, ro, c := t.state, t.errMsg, t.readOnly, t.client
	t.layout = nil
	t.seen = map[string]bool{}
	t.mu.Unlock()
	t.emit("status", map[string]any{"state": st, "msg": msg, "readOnly": ro, "replay": log})
	if c != nil {
		t.tick(true)
	}
}

// ── Saisie et commandes de l'interface ────────────────────────────────────

func (t *projTerm) input(pane, data string) {
	t.mu.Lock()
	c, ro := t.client, t.readOnly
	t.mu.Unlock()
	if c == nil || ro || pane == "" {
		return
	}
	for _, hex := range hexKeys([]byte(data)) {
		_ = c.Send(append([]string{"send-keys", "-t", pane, "-H"}, hex...)...)
	}
}

// binaryBytes : données « binaires » de xterm.js (onBinary : souris en
// encodage X10 par défaut, coordonnées > 95) : un caractère = un octet, à ne
// pas réencoder en UTF-8 (#135).
func binaryBytes(data string) string {
	b := make([]byte, 0, len(data))
	for _, r := range data {
		b = append(b, byte(r))
	}
	return string(b)
}

// resizePane : bordure entre deux panes glissée dans l'interface (#135),
// comme la souris dans la coque : nouvelle largeur (axis « x ») ou hauteur
// (« y ») du pane, en cellules.
func (t *projTerm) resizePane(pane, axis string, n int) {
	t.mu.Lock()
	c, ro := t.client, t.readOnly
	t.mu.Unlock()
	if c == nil || ro || pane == "" || n < 1 || (axis != "x" && axis != "y") {
		return
	}
	_ = c.Send("resize-pane", "-t", pane, "-"+axis, strconv.Itoa(n))
	t.poke()
}

func (t *projTerm) resize(cols, rows int) {
	if cols < 10 || rows < 4 {
		return
	}
	t.mu.Lock()
	changed := cols != t.cols || rows != t.rows
	t.cols, t.rows = cols, rows
	if t.pty != nil {
		_ = t.pty.Resize(cols, rows)
	}
	c := t.client
	if changed {
		t.resized = true
	}
	t.mu.Unlock()
	if c != nil && changed {
		_ = c.Send("refresh-client", "-C", clientSize(cols, rows))
		t.poke()
		t.reseedLater()
	}
}

// clientSize : taille d'un client de contrôle pour refresh-client -C, sous la
// forme « L,H » comprise par toutes les versions de tmux (« LxH » n'existe
// qu'à partir de la 3.1).
func clientSize(cols, rows int) string { return fmt.Sprintf("%d,%d", cols, rows) }

// reseedDelay : après le dernier redimensionnement (anti-rebond : une fenêtre
// glissée en envoie des dizaines), le contenu de chaque pane est recapturé :
// les applications redessinent à leur nouvelle taille, et un terminal resté
// sur l'ancienne géométrie (lignes repliées, maximiser / restaurer) repart
// d'un écran exact.
var reseedDelay = 400 * time.Millisecond

func (t *projTerm) reseedLater() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reseed != nil {
		t.reseed.Stop()
	}
	t.reseed = time.AfterFunc(reseedDelay, func() {
		t.mu.Lock()
		t.seen = map[string]bool{}
		t.mu.Unlock()
		t.poke()
	})
}

func (t *projTerm) focus(pane string) {
	if c := t.cl(); c != nil && pane != "" {
		_ = c.Send("select-pane", "-t", pane)
	}
}

func (t *projTerm) zoom(pane string) {
	if c := t.cl(); c != nil && pane != "" {
		_ = c.Send("resize-pane", "-Z", "-t", pane)
		t.poke()
	}
}

func (t *projTerm) cl() *tmuxClient {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.client
}

// paneInfos : panes du projet, pour la carte Team (version de claude).
func (t *projTerm) paneInfos() []core.PaneInfo {
	// relevés par le watcher (readWin) : jamais d'appel tmux ici, les cartes
	// sont calculées sur la goroutine d'émission de l'interface
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]core.PaneInfo(nil), t.infos...)
}

// closeStop : fin de l'onglet (session terminée ou détachement), une seule fois.
func (t *projTerm) closeStop() { t.stopOnce.Do(func() { close(t.stop) }) }

// ── Mise en page (règles de core, comme le layout-watch de la coque) ──────

const paneFormat = "#{pane_id}|#{pane_index}|#{window-style}|#{window_width}|#{window_height}|#{window_zoomed_flag}|#{pane_active}|#{pane_pid}|#{window_visible_layout}|#{pane_current_command}"

func (t *projTerm) orderFile() string {
	return filepath.Join(os.TempDir(), "claude_desk_"+core.CtrName(t.path)+".order")
}

// watcher : toutes les secondes (et à chaque changement de mise en page) :
// propriétaire, classement de la team, teintes, onglet allumé, géométrie.
func (t *projTerm) watcher() {
	poke := make(chan struct{}, 1)
	pokeKey.Store(t, poke)
	defer pokeKey.Delete(t)
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tk.C:
			t.tick(false)
		case <-poke:
			time.Sleep(40 * time.Millisecond)
			t.tick(true)
		}
	}
}

type winPaneFull struct {
	core.WinPane
	active bool
	cmd    string
}

func (t *projTerm) readWin() (panes []winPaneFull, w, h int, zoomed bool, visible string, err error) {
	c := t.cl()
	if c == nil {
		return nil, 0, 0, false, "", errClientClosed
	}
	atomic.AddInt32(&t.reads, 1)
	b, err := c.Output("list-panes", "-t", t.win, "-F", paneFormat)
	if err != nil {
		return nil, 0, 0, false, "", err
	}
	var infos []core.PaneInfo
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.SplitN(l, "|", 10) // la commande en dernier (peut contenir « | »)
		if len(f) != 10 || f[0] == "" {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		w, _ = strconv.Atoi(f[3])
		h, _ = strconv.Atoi(f[4])
		zoomed = f[5] == "1"
		visible = f[8]
		panes = append(panes, winPaneFull{WinPane: core.WinPane{ID: f[0], Index: idx, Style: f[2]}, active: f[6] == "1", cmd: f[9]})
		pid := f[7]
		if !t.host {
			pid = "" // conteneur : PID d'un autre espace
		}
		infos = append(infos, core.PaneInfo{ID: f[0], PID: pid, Cmd: f[9]})
	}
	sortPanes(panes)
	t.mu.Lock()
	t.infos = infos
	t.mu.Unlock()
	return panes, w, h, zoomed, visible, nil
}

func sortPanes(p []winPaneFull) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].Index < p[j-1].Index; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

// tick : un tour du watcher ; fast : sur notification (sans propriétaire ni team).
func (t *projTerm) tick(fast bool) {
	cfg := t.a.cfg
	panes, w, h, zoomed, visible, err := t.readWin()
	if err != nil || len(panes) == 0 {
		return
	}
	var ids strings.Builder
	cps := corePanes(panes)
	for _, p := range panes {
		ids.WriteString(p.ID + "\n")
	}
	leader := leaderID(ids.String())
	t.mu.Lock()
	ro := t.readOnly
	resized := false
	if !fast {
		// consommé par le seul tour lent, qui remet la team en page
		resized, t.resized = t.resized, false
	}
	t.mu.Unlock()

	// team du projet : lue une fois par tour
	var tc *core.TeamConfig
	if tp := t.findTeam(); tp != "" {
		if x, err := core.ReadTeam(tp); err == nil {
			tc = &x
		}
	}
	arranged := false
	arrange := func() {
		t.arrange(cps, w, h, leader, tc)
		arranged = true
	}
	if !fast {
		// toutes les 5 s : propriétaire (conteneur) et règle de taille (#126)
		if t.watch.beat++; t.watch.beat%5 == 1 {
			if !t.host && !ro {
				t.heartbeat()
			}
			t.govern()
		}
	}
	if !fast && !t.host && !ro {
		// Ctrl+B R dans le tmux du conteneur incrémente @cl_relayout (bootstrap.sh)
		if b, err := t.cl().Output("show-options", "-wqv", "-t", t.win, "@cl_relayout"); err == nil {
			r := strings.TrimSpace(string(b))
			if r != t.watch.prevRelayout && (t.watch.prevRelayout != "" || r != "") && len(panes) > 1 && tc != nil {
				arrange()
			}
			t.watch.prevRelayout = r
		}
	}
	busy := false
	if tc != nil {
		sig := core.ActiveSignature(*tc)
		busy = sig != ""
		if !ro && !fast && sig != t.watch.prevSig {
			t.watch.prevSig = sig
			arrange()
		}
	}
	// un pane de plus : attendre que la team ait inscrit tous les agents (le
	// leader n'a pas de tmuxPaneId), au plus 10 tours, puis mise en page
	if n := len(panes); !fast && !ro {
		if n != t.watch.prevCount {
			t.watch.prevCount = n
			if n > 1 {
				t.watch.wait = 10
			}
		}
		if t.watch.wait > 0 {
			t.watch.wait--
			registered := 0
			if tc != nil {
				for _, m := range tc.Members {
					if m.PaneID != "" {
						registered++
					}
				}
			}
			if registered >= n-1 || t.watch.wait == 0 {
				t.watch.wait = 0
				arrange()
			}
		}
		// fenêtre redimensionnée : la géométrie de la team (TeamRows) est
		// réappliquée à la nouvelle taille
		if resized && n > 1 && (w != t.watch.prevW || h != t.watch.prevH) {
			arrange()
		}
		t.watch.prevW, t.watch.prevH = w, h
	}
	if arranged {
		// panes échangés et géométrie changée : relus une seule fois
		if panes2, w2, h2, z2, v2, err := t.readWin(); err == nil && len(panes2) > 0 {
			panes, w, h, zoomed, visible = panes2, w2, h2, z2, v2
			cps = corePanes(panes)
		}
	}
	// teintes : actifs éclaircis, inactifs assombris (après la mise en page,
	// qui remet la couleur de base) ; l'interface les reçoit tout de suite
	if tc != nil && !ro {
		for _, m := range tc.Members {
			if m.PaneID == "" || m.PaneID == leader || !hasPane(panes, m.PaneID) {
				continue
			}
			m := m
			shade := core.PaneShade(cfg, t.color, &m, false)
			core.SetPaneColor(t.cl(), cps, m.PaneID, shade)
			for i := range panes {
				if panes[i].ID == m.PaneID {
					panes[i].Style = "bg=colour" + shade
				}
			}
		}
	}
	t.publish(panes, w, h, zoomed, visible, leader, tc, busy)
}

// findTeam : config.json de la team du projet (compté pour les tests : une
// recherche par tour).
func (t *projTerm) findTeam() string {
	atomic.AddInt32(&t.finds, 1)
	return core.FindTeamConfig(t.path)
}

// heartbeat : propriétaire (#48) ; si un autre launcher a pris la main,
// l'onglet passe en lecture seule.
func (t *projTerm) heartbeat() {
	c := t.cl()
	b, err := c.Output("display-message", "-p", "-t", t.win, core.OwnerFormat)
	if err != nil {
		return
	}
	if o := core.ParseOwner(strings.TrimSpace(string(b))); o.Id != "" && o.Id != core.LauncherID() {
		t.mu.Lock()
		t.readOnly, t.owner = true, o.Id
		t.mu.Unlock()
		t.a.setMsg("⬢ " + o.Id + " a pris la main sur " + t.name + " : lecture seule — Entrée sur le projet pour la reprendre")
		t.setState("ready", "")
		t.govern() // le pilote décide de la taille (#126)
		return
	}
	_ = c.Send("set-option", "-t", t.win, "@cl_owner_at", strconv.FormatInt(time.Now().Unix(), 10))
}

// relayout : mise en page forcée (Ctrl+B R de la coque).
func (t *projTerm) relayout(force bool) {
	panes, w, h, _, _, err := t.readWin()
	if err != nil || len(panes) <= 1 {
		t.a.setMsg("Relayout : pas de team active (1 seul pane)")
		return
	}
	var ids strings.Builder
	for _, p := range panes {
		ids.WriteString(p.ID + "\n")
	}
	tp := core.FindTeamConfig(t.path)
	if tp == "" && force {
		t.a.setMsg("Relayout : aucune team trouvée pour " + t.name)
	}
	var tc *core.TeamConfig
	if tp != "" {
		if x, err := core.ReadTeam(tp); err == nil {
			tc = &x
		}
	}
	t.arrange(corePanes(panes), w, h, leaderID(ids.String()), tc)
	t.poke()
}

func (t *projTerm) arrange(panes []core.WinPane, w, h int, leader string, tc *core.TeamConfig) {
	if t.isReadOnly() || len(panes) <= 1 {
		return
	}
	ids := make([]string, len(panes))
	for i, p := range panes {
		ids[i] = p.ID
	}
	top, bot := core.Classify(ids, leader, tc, t.orderFile())
	core.ArrangeTeam(t.a.cfg, t.cl(), t.win, panes, w, h, top, bot, t.color)
}

// publish : géométrie, couleurs et noms des panes pour l'interface ; panes
// nouveaux amorcés.
func (t *projTerm) publish(panes []winPaneFull, w, h int, zoomed bool, visible, leader string, tc *core.TeamConfig, busy bool) {
	geo := map[string]PaneGeom{}
	if _, _, g, err := parseLayout(visible); err == nil {
		for _, p := range g {
			geo[p.ID] = p
		}
	}
	members := map[string]core.TeamMember{}
	if tc != nil {
		for _, m := range tc.Members {
			if m.PaneID != "" {
				members[m.PaneID] = m
			}
		}
	}
	msg := &LayoutMsg{Cols: w, Rows: h, Zoomed: zoomed, Leader: leader}
	for _, p := range panes {
		g, ok := geo[p.ID]
		if !ok {
			continue // masqué (zoom d'un autre pane)
		}
		v := PaneView{ID: p.ID, X: g.X, Y: g.Y, W: g.W, H: g.H, Active: p.active, Name: p.cmd,
			Bg: core.Colour256Hex(strings.TrimPrefix(styleBg(p.Style), "colour"))}
		if v.Bg == "" {
			v.Bg = core.Colour256Hex(t.color)
		}
		switch m, ok := members[p.ID]; {
		case p.ID == leader:
			v.Name, v.Role = "team-lead", "leader"
		case ok && m.AgentType == "cdp":
			v.Name, v.Role = m.Name, "cdp"
		case ok && m.IsActive:
			v.Name, v.Role = m.Name, "actif"
		case ok:
			v.Name, v.Role = m.Name, "inactif"
		}
		msg.Panes = append(msg.Panes, v)
	}
	t.mu.Lock()
	same := t.layout != nil && layoutEqual(*t.layout, *msg)
	// pane redimensionné par tmux (bordure glissée #135, zoom, autre client) :
	// le terminal de l'interface replie ses lignes autrement que tmux ; il est
	// recapturé une fois la taille stable
	paneResized := false
	if t.layout != nil {
		old := map[string][2]int{}
		for _, p := range t.layout.Panes {
			old[p.ID] = [2]int{p.W, p.H}
		}
		for _, p := range msg.Panes {
			if o, ok := old[p.ID]; ok && o != [2]int{p.W, p.H} {
				paneResized = true
			}
		}
	}
	t.layout = msg
	busyChanged := t.busy != busy
	t.busy = busy
	var fresh []string
	for _, p := range panes {
		if !t.seen[p.ID] {
			fresh = append(fresh, p.ID)
		}
	}
	t.mu.Unlock()
	if !same {
		t.emit("layout", map[string]any{"layout": msg})
	}
	for _, p := range fresh {
		t.seed(p)
	}
	if paneResized {
		t.reseedLater()
	}
	if busyChanged {
		t.a.changed()
	}
}

func layoutEqual(a, b LayoutMsg) bool {
	if a.Cols != b.Cols || a.Rows != b.Rows || a.Zoomed != b.Zoomed || a.Leader != b.Leader || len(a.Panes) != len(b.Panes) {
		return false
	}
	for i := range a.Panes {
		if a.Panes[i] != b.Panes[i] {
			return false
		}
	}
	return true
}

// styleBg : « colourN » de « bg=colourN » (window-style d'un pane).
func styleBg(style string) string {
	for _, f := range strings.Split(style, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(f), "bg="); ok {
			return v
		}
	}
	return ""
}

func hasPane(panes []winPaneFull, id string) bool {
	for _, p := range panes {
		if p.ID == id {
			return true
		}
	}
	return false
}

func corePanes(panes []winPaneFull) []core.WinPane {
	out := make([]core.WinPane, len(panes))
	for i, p := range panes {
		out[i] = p.WinPane
	}
	return out
}

// leaderID : le teamleader est le premier pane créé (plus petit %id).
func leaderID(ids string) string {
	id, _ := core.LeaderOf(ids)
	return id
}
