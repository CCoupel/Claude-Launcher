package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultNavW  = 40
	outerSession = "claude-ui-ng2"
)

func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	d := filepath.Join(base, "claude-launcher-ng2")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// ── Sélection partagée navigateur → vue État ──────────────────────────────

type Selection struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Path string `json:"path"`
	// point d'état de session (#50), pour la légende de la vue « ◈ État »
	Dot    int    `json:"dot,omitempty"`
	DotWhy string `json:"dot_why,omitempty"`
}

func selectionPath() string { return filepath.Join(CacheDir(), "selection.json") }

func writeSelection(n Node, dot sessState, why string) {
	b, _ := json.Marshal(Selection{Name: n.Name, Key: n.Key, Path: n.Path, Dot: int(dot), DotWhy: why})
	tmp := selectionPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, selectionPath())
	}
}

func readSelection() (Selection, bool) {
	var s Selection
	b, err := os.ReadFile(selectionPath())
	if err != nil || json.Unmarshal(b, &s) != nil || s.Path == "" {
		return s, false
	}
	return s, true
}

// ── Largeur de la zone navigation ─────────────────────────────────────────

func navWidthPath() string { return filepath.Join(CacheDir(), "navwidth") }

func loadNavWidth() int {
	if b, err := os.ReadFile(navWidthPath()); err == nil {
		if n, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && n >= 20 {
			return n
		}
	}
	return defaultNavW
}

func saveNavWidth(n int) {
	_ = os.WriteFile(navWidthPath(), []byte(strconv.Itoa(n)), 0o644)
}

// fixWidth : appelé par les hooks tmux (attache, redimensionnement du terminal)
// pour que le navigateur garde sa largeur et que la zone tmux absorbe le reste.
func FixWidth() {
	w, _ := navWidth()
	_ = otmux("resize-pane", "-t", outerSession+":0.0", "-x", strconv.Itoa(w)).Run()
}

// navWidth : largeur mémorisée, ramenée au défaut si elle ne laisserait plus
// de place à la zone tmux ; renvoie aussi la largeur de la fenêtre coque.
func navWidth() (nav, win int) {
	nav = loadNavWidth()
	if b, err := otmux("display-message", "-p", "-t", outerSession+":0", "#{window_width}").Output(); err == nil {
		if win, _ = strconv.Atoi(strings.TrimSpace(string(b))); win > 0 && nav > win-20 {
			nav = defaultNavW
		}
	}
	return nav, win
}

// ── Coque ─────────────────────────────────────────────────────────────────

func stateCommand(exe string) string {
	return supervised(roleInfo, "CL_HUB="+Shq(hubSession)+" CL_OUTER_SOCKET="+Shq(outerSocket())+
		" CL_INNER_SOCKET="+Shq(InnerSocket())+" "+Shq(exe)+" state")
}

func navCommand(exe string) string {
	return supervised(roleNav, fmt.Sprintf("CL_SHELL=1 CL_HUB=%s CL_OUTER_SOCKET=%s CL_INNER_SOCKET=%s %s nav --launch",
		Shq(hubSession), Shq(outerSocket()), Shq(InnerSocket()), Shq(exe)))
}

// supervised : commande d'un pane de la coque relancée en boucle. Sans elle, le
// pane se fermait avec son processus (plantage, panic, SIGBUS quand le binaire
// sur MooseFS devient illisible, binaire absent après un démontage…) et le
// navigateur disparaissait, emportant F5, Échap… Le code de sortie et la fin de
// stderr sont consignés dans <rôle>.err (relu au redémarrage). Quitter le
// launcher arrête tout le serveur de la coque, donc aussi la boucle.
func supervised(role, cmd string) string {
	errf := filepath.Join(CacheDir(), role+".err")
	out := filepath.Join(CacheDir(), role+".stderr")
	return fmt.Sprintf(`while :; do %s 2>%s; rc=$?; `+
		`case $rc in 2) w=" (panic)";; 127) w=" (binaire introuvable : stockage démonté ?)";; `+
		`135) w=" (SIGBUS : binaire illisible, stockage démonté ?)";; 137) w=" (SIGKILL)";; 139) w=" (SIGSEGV)";; *) w="";; esac; `+
		// Go convertit SIGBUS/SIGSEGV en panic (code 2) : la cause est dans stderr.
		`grep -q SIGBUS %s 2>/dev/null && w=" (SIGBUS : binaire illisible, stockage démonté ?)"; `+
		`{ echo "$(date '+%%Y-%%m-%%d %%H:%%M:%%S') arrêt inattendu ($rc)$w"; head -n 40 %s 2>/dev/null; } >%s; `+
		`printf '\n  ⚠ arrêt inattendu (code %%s)%%s : redémarrage dans 2 s…\n' "$rc" "$w"; sleep 2; done`,
		cmd, Shq(out), Shq(out), Shq(out), Shq(errf))
}

// paneStale : le pane exécute-t-il une autre copie du launcher que exe (binaire
// supprimé ou remplacé par une mise à jour) ? On regarde le processus du pane
// et ses enfants directs (cas où sh -c n'a pas fait exec).
func paneStale(tm func(...string) *exec.Cmd, target, exe string) bool {
	b, err := tm("display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	pids := []string{pid}
	if c, err := os.ReadFile("/proc/" + pid + "/task/" + pid + "/children"); err == nil {
		pids = append(pids, strings.Fields(string(c))...)
	}
	for _, p := range pids {
		l, err := os.Readlink("/proc/" + p + "/exe")
		if err != nil || l == exe {
			continue
		}
		if filepath.Base(strings.TrimSuffix(l, " (deleted)")) == filepath.Base(exe) {
			return true
		}
	}
	return false
}

// ensureInner : crée claude-hub s'il manque (premier onglet : « ⌗ Teamleaders »).
func ensureInner() error {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		if out, err := dtmux("new-session", "-d", "-s", hubSession, "-n", teamWindow, teamPlaceholder).CombinedOutput(); err != nil {
			return fmt.Errorf("new-session %s : %v (%s)", hubSession, err, strings.TrimSpace(string(out)))
		}
	}
	keepTeamWindow(exactW(hubSession, teamWindow))
	// config de claude du launcher : aussi pour les fenêtres ouvertes plus tard,
	// même si le serveur tournait déjà (il garde l'environnement de sa création)
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		_ = dtmux("set-environment", "-g", "CLAUDE_CONFIG_DIR", d).Run()
	}
	// Barre d'onglets en haut ; le style projet est posé par window à l'ouverture.
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status", "on").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-position", "top").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-justify", "left").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-left", "").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-right", "#[fg=colour244]%H:%M ").Run()
	_ = dtmux("select-window", "-t", exactW(hubSession, teamWindow)).Run()
	bindZoom()
	return nil
}

// tmuxDoubleClick : liaison DoubleClick1Pane par défaut de tmux (sélection d'un mot).
const tmuxDoubleClick = `select-pane -t = ; if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -H ; send-keys -X select-word ; run-shell -d 0.3 ; send-keys -X copy-pipe-and-cancel }`

// bindZoom : dans claude-hub, un double-clic sur un pane le passe en plein écran
// (comme préfixe+z) et un nouveau double-clic le remet en taille normale ;
// préfixe+R remet la team en ordre. Les liaisons sont globales au serveur : les
// autres sessions gardent le comportement par défaut.
func bindZoom() {
	cond := "#{==:#{session_name}," + hubSession + "}"
	// fenêtre d'un projet en conteneur (@cl_nested) : le double-clic est transmis
	// au tmux du conteneur, qui zoome lui-même (bootstrap.sh)
	zcond := "#{&&:" + cond + ",#{!:#{@cl_nested}}}"
	_ = dtmux("bind-key", "-n", "DoubleClick1Pane", "if-shell", "-F", zcond,
		"select-pane -t = ; resize-pane -Z -t =", tmuxDoubleClick).Run()
	// préfixe + R : remettre en ordre la team de la fenêtre courante (layout.go)
	_ = dtmux("bind-key", "R", "if-shell", "-F", cond,
		"run-shell -b "+Shq(selfCommand("relayout")+" '#{window_id}'")).Run()
	bindNested()
}

// bindNested : préfixe transparent pour les projets en conteneur. Le hub n'a plus
// de préfixe (il serait reconnu AVANT les liaisons -n) : Ctrl+B est aiguillé —
// fenêtre ordinaire → table prefix habituelle ; fenêtre d'un conteneur
// (@cl_nested) → table cl_nested, qui renvoie Ctrl+B + la touche au tmux du
// conteneur ; Ctrl+B Ctrl+B y rejoint le préfixe du hub. Serveur NG2 seulement.
func bindNested() {
	_ = dtmux("set-option", "-g", "prefix", "None").Run()
	_ = dtmux("set-option", "-g", "prefix2", "None").Run()
	_ = dtmux("bind-key", "-n", "C-b", "if-shell", "-F", "#{@cl_nested}",
		"switch-client -T cl_nested", "switch-client -T prefix").Run()
	_ = dtmux("bind-key", "-T", "cl_nested", "C-b", "switch-client", "-T", "prefix").Run()
	for _, k := range nestedKeys() {
		if k == ";" { // « ; » sépare les commandes : envoyé en hexadécimal
			_ = dtmux("bind-key", "-T", "cl_nested", "\\;", "send-keys", "-H", "02", "3b").Run()
			continue
		}
		_ = dtmux("bind-key", "-T", "cl_nested", k, "send-keys", "C-b", k).Run()
	}
}

// nestedKeys : touches transmises après Ctrl+B au tmux d'un conteneur.
func nestedKeys() []string {
	var ks []string
	for _, r := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&()*+,-./:;<=>?@[]^_`{|}~\"'\\" {
		ks = append(ks, string(r))
	}
	return append(ks, "Up", "Down", "Left", "Right", "M-Up", "M-Down", "M-Left", "M-Right",
		"C-Up", "C-Down", "C-Left", "C-Right", "PPage", "NPage", "Space", "Enter", "Escape",
		"BSpace", "Tab", "C-o", "C-z")
}

// ── Panes de la coque ─────────────────────────────────────────────────────
//
// navigateur en haut à gauche, infos du projet sélectionné dessous, zone tmux
// (claude-hub) à droite sur toute la hauteur. Chaque pane porte son rôle dans
// l'option @cl : les index changent quand un pane est recréé.

const (
	roleNav  = "nav"
	roleInfo = "info"
	roleHub  = "hub"
)

// outerPanes : rôle -> id de pane (%N) ; untagged : panes sans rôle, par index.
func outerPanes() (roles map[string]string, untagged []string) {
	roles = map[string]string{}
	b, err := otmux("list-panes", "-t", outerSession+":0", "-F", "#{pane_id}|#{@cl}").Output()
	if err != nil {
		return roles, nil
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		id, role, _ := strings.Cut(l, "|")
		switch {
		case id == "":
		case role == "":
			untagged = append(untagged, id)
		default:
			roles[role] = id
		}
	}
	return roles, untagged
}

func outerPane(role string) string {
	roles, _ := outerPanes()
	return roles[role]
}

func tagPane(id, role string) { _ = otmux("set-option", "-p", "-t", id, "@cl", role).Run() }

// shellWithoutNav : la coque n'a plus de navigateur. Une coque d'avant les
// rôles (aucun pane étiqueté) n'est pas concernée : ensureLayout l'étiquette.
func shellWithoutNav() bool {
	roles, untagged := outerPanes()
	if roles[roleNav] != "" {
		return false
	}
	return !(len(roles) == 0 && len(untagged) > 0)
}

// navErrFile : dernière erreur de sortie du navigateur (son pane se ferme avec
// lui, le message serait perdu).
func navErrFile() string { return filepath.Join(CacheDir(), "nav.err") }

func RecordNavError(err error) {
	line := time.Now().Format("2006-01-02 15:04:05") + " " + NgVersion + " : " + strings.TrimSpace(err.Error())
	_ = os.WriteFile(navErrFile(), []byte(line+"\n"), 0o644)
}

// takeNavCrash : arrêt inattendu consigné par la boucle du pane (supervised),
// lu une fois au redémarrage ; le fichier est gardé en .last pour le diagnostic.
func takeNavCrash() string {
	s := lastNavError()
	if s == "" {
		return ""
	}
	_ = os.Rename(navErrFile(), navErrFile()+".last")
	if _, rest, ok := strings.Cut(s, " arrêt inattendu "); ok {
		return "arrêt " + rest
	}
	return "une erreur : " + s
}

func lastNavError() string {
	b, err := os.ReadFile(navErrFile())
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ensureLayout complète la coque : rôles posés (coque d'avant les rôles :
// pane 0 = navigateur, l'autre = zone tmux), zone tmux et pane d'infos recréés
// s'ils manquent. Appelé par la coque et par
// un navigateur redémarré après mise à jour : un verrou évite les doublons.
func EnsureLayout(exe string) error {
	if f, err := os.OpenFile(filepath.Join(CacheDir(), "layout.lock"), os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer f.Close()
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	roles, untagged := outerPanes()
	if len(roles) == 0 && len(untagged) > 0 {
		tagPane(untagged[0], roleNav)
		roles[roleNav] = untagged[0]
		if len(untagged) == 2 {
			tagPane(untagged[1], roleHub)
			roles[roleHub] = untagged[1]
		}
	}
	nav := roles[roleNav]
	if nav == "" {
		// Navigateur disparu (pane fermé) : on le recrée au-dessus des infos, ou à
		// gauche de la zone tmux s'il n'y a plus qu'elle.
		args := []string{"split-window", "-v", "-b", "-d", "-P", "-F", "#{pane_id}", "-l", "55%", "-t", roles[roleInfo]}
		if roles[roleInfo] == "" {
			if roles[roleHub] == "" {
				return fmt.Errorf("coque sans navigateur")
			}
			w, _ := navWidth()
			args = []string{"split-window", "-h", "-b", "-d", "-P", "-F", "#{pane_id}", "-l", strconv.Itoa(w), "-t", roles[roleHub]}
		}
		out, err := otmux(append(args, navCommand(exe))...).Output()
		if err != nil {
			return fmt.Errorf("tmux split-window (navigateur) : %v", err)
		}
		nav = strings.TrimSpace(string(out))
		tagPane(nav, roleNav)
	}
	if roles[roleHub] == "" {
		// -f : pleine hauteur, à droite du navigateur et du pane d'infos.
		// Taille posée d'emblée : un split à 50 % serait mémorisé comme un glissement.
		args := []string{"split-window", "-h", "-f", "-d", "-P", "-F", "#{pane_id}", "-t", nav}
		if w, win := navWidth(); win > w+1 {
			args = append(args, "-l", strconv.Itoa(win-w-1))
		}
		out, err := otmux(append(args, innerCommand())...).Output()
		if err != nil {
			return fmt.Errorf("tmux split-window (zone tmux) : %v", err)
		}
		roles[roleHub] = strings.TrimSpace(string(out))
		tagPane(roles[roleHub], roleHub)
	}
	if roles[roleInfo] == "" {
		out, err := otmux("split-window", "-v", "-d", "-P", "-F", "#{pane_id}", "-l", "45%", "-t", nav, stateCommand(exe)).Output()
		if err != nil {
			return fmt.Errorf("tmux split-window (infos) : %v", err)
		}
		tagPane(strings.TrimSpace(string(out)), roleInfo)
	} else if paneStale(otmux, roles[roleInfo], exe) {
		_ = otmux("respawn-pane", "-k", "-t", roles[roleInfo], stateCommand(exe)).Run()
	}
	_ = otmux("bind-key", "-n", "M-Left", "select-pane", "-t", nav).Run()
	_ = otmux("bind-key", "-n", "M-Right", "select-pane", "-t", roles[roleHub]).Run()
	return nil
}

func innerCommand() string {
	return fmt.Sprintf("env -u TMUX tmux -L %s attach-session -t %s", Shq(InnerSocket()), Shq(exactS(hubSession)))
}

func RunShell() error {
	exe, err := SelfExe()
	if err != nil {
		return err
	}
	if created, err := EnsureConfig(); err == nil && created {
		fmt.Println("  ✓ config par défaut créée :", ConfigPath(), "(claude-launcher-ng2 configure pour l'éditer)")
	}
	if err := ensureInner(); err != nil {
		return err
	}
	nav := navCommand(exe)
	if otmux("has-session", "-t", outerSession).Run() == nil && shellWithoutNav() {
		// Coque survivante dont le navigateur a disparu (plantage, processus
		// tué) : elle ne contient que les infos et l'attache à claude-hub, rien
		// à garder. On la recrée au lieu d'échouer (« coque sans navigateur »).
		fmt.Println("  ⚠ navigateur absent de la coque : coque recréée (projets conservés)")
		if why := lastNavError(); why != "" {
			fmt.Println("    dernière sortie du navigateur :", why)
			_ = os.Remove(navErrFile()) // affichée une fois : pas de cause périmée la fois suivante
		}
		_ = otmux("kill-server").Run()
	}
	if otmux("has-session", "-t", outerSession).Run() != nil {
		steps := [][]string{
			{"-f", "/dev/null", "new-session", "-d", "-s", outerSession, "-n", "ui", nav},
			// Coque transparente : pas de barre, pas de préfixe (tout passe au tmux intérieur).
			{"set-option", "-t", outerSession, "status", "off"},
			{"set-option", "-g", "prefix", "None"},
			{"set-option", "-g", "mouse", "on"},
			{"set-option", "-g", "escape-time", "10"},
			{"set-option", "-g", "pane-border-style", "fg=colour238"},
			{"set-option", "-g", "pane-active-border-style", "fg=colour27"},
			{"set-option", "-g", "pane-border-lines", "heavy"},
			// tmux refocalise le pane sous la souris à la fin d'un double-clic, ce qui
			// annulerait le passage du focus à la zone tmux après « ouvrir » : on se
			// contente de transmettre l'événement au programme du pane.
			{"bind-key", "-n", "DoubleClick1Pane", "send-keys", "-M"},
			{"bind-key", "-n", "TripleClick1Pane", "send-keys", "-M"},
			{"set-hook", "-t", outerSession, "client-attached", "run-shell " + Shq(Shq(exe)+" fixwidth")},
			{"set-hook", "-t", outerSession, "client-resized", "run-shell " + Shq(Shq(exe)+" fixwidth")},
			{"set-option", "-p", "-t", outerSession + ":0.0", "@cl", roleNav},
		}
		for _, s := range steps {
			if out, err := otmux(s...).CombinedOutput(); err != nil {
				return fmt.Errorf("tmux %s : %v (%s)", s[0], err, strings.TrimSpace(string(out)))
			}
		}
	} else {
		// Coque survivante dont le navigateur tourne sur un binaire remplacé
		// depuis (mise à jour) : on le redémarre sur le binaire installé.
		p := outerPane(roleNav)
		if p == "" {
			p = outerSession + ":0.0" // coque d'avant les rôles
		}
		if paneStale(otmux, p, exe) {
			_ = otmux("respawn-pane", "-k", "-t", p, nav).Run()
		}
	}
	if err := EnsureLayout(exe); err != nil {
		return err
	}
	_ = otmux("select-pane", "-t", outerPane(roleNav)).Run()
	// Attache le terminal courant à la coque (environnement propre : pas d'imbrication).
	c := exec.Command("tmux", "-L", outerSocket(), "attach-session", "-t", outerSession)
	c.Env = EnvWithout("TMUX", "TMUX_PANE")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
