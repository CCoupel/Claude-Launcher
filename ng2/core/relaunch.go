package core

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/platform"
)

// ── État du launcher en cours (#219, contracts/desktop-dispatch.md « launcherInfo ») ──
//
// Le binaire est remplacé par une mise à jour pendant que le launcher tourne ; ce fichier dit si c'est le cas, quelle
// version est installée, et quels processus auxiliaires (mirror, layout-watch) du même serveur tmux exécutent encore
// l'ancien binaire.

// LauncherMode : « shell », « desktop », « web » ou « pod ». Posé par l'interface au démarrage ; vide : déduit.
var LauncherMode string

// StaleProc : processus auxiliaire d'un ancien binaire.
type StaleProc struct {
	Role string `json:"role"` // mirror, layout-watch
	PID  int    `json:"pid"`
}

// LauncherBackup : sauvegarde du binaire précédent (renseignée par #218 via LauncherBackupHook).
type LauncherBackup struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Date    string `json:"date"`
}

// LauncherInfo : résultat de `launcherInfo`.
type LauncherInfo struct {
	Running    string          `json:"running"`
	Installed  string          `json:"installed"`
	BinChanged bool            `json:"binChanged"`
	Mode       string          `json:"mode"`
	Stale      []StaleProc     `json:"stale"`
	Backup     *LauncherBackup `json:"backup,omitempty"`
}

// procRoot : racine de procfs (tests : arborescence factice).
var procRoot = "/proc"

// Points d'injection (tests).
var (
	// LauncherBackupHook : sauvegarde disponible (nil : aucune). Posé par la sauvegarde du binaire (#218).
	LauncherBackupHook func() *LauncherBackup
	// installedVersionCmd : commande rendant la version du binaire installé.
	installedVersionCmd = func(ctx context.Context, exe string) ([]byte, error) {
		return exec.CommandContext(ctx, exe, "version").Output()
	}
	installedVersionTimeout = 3 * time.Second
	// launcherExe : chemin du binaire courant.
	launcherExe = platform.SelfExe
)

var (
	startStampOnce sync.Once
	startStamp     FileSig
	startStampOK   bool
)

// MarkLauncherStart : mémorise l'identité du binaire au démarrage (appelé tôt dans main ; sinon au premier usage).
func MarkLauncherStart() {
	startStampOnce.Do(func() {
		if exe, err := launcherExe(); err == nil {
			var serr error
			startStamp, serr = StatSig(exe)
			startStampOK = serr == nil
		}
	})
}

// FileSig : identité d'un fichier (inode/périphérique, taille, date).
type FileSig struct {
	Dev, Ino uint64
	Size     int64
	MTime    int64 // ns
}

// StatSig : identité du fichier path.
func StatSig(path string) (FileSig, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileSig{}, err
	}
	return sigOf(fi), nil
}

// BinChanged : le fichier path n'est plus celui de start (remplacé : inode, date ou taille ; disparu).
func BinChanged(start FileSig, path string) bool {
	cur, err := StatSig(path)
	return err != nil || cur != start
}

// binChanged : le binaire courant n'est plus celui du démarrage.
func binChanged() bool {
	MarkLauncherStart()
	if !startStampOK {
		return false
	}
	exe, err := launcherExe()
	return err != nil || BinChanged(startStamp, exe)
}

var (
	binChangedMu   sync.Mutex
	binChangedAt   time.Time
	binChangedLast bool
)

// LauncherBinChanged : version bon marché (stat, mémorisée 5 s) pour l'instantané de l'interface.
func LauncherBinChanged() bool {
	binChangedMu.Lock()
	defer binChangedMu.Unlock()
	if time.Since(binChangedAt) > 5*time.Second {
		binChangedLast, binChangedAt = binChanged(), time.Now()
	}
	return binChangedLast
}

func launcherMode() string {
	switch {
	case LauncherMode != "":
		return LauncherMode
	case InCluster():
		return "pod"
	}
	return "shell"
}

// InstalledVersion : dernier mot de « <exe> version » (« claude-launcher-ng2 v6.6.0 ») ; "" si illisible ou si le
// délai est dépassé.
func InstalledVersion(exe string, timeout time.Duration) string {
	if NoExecSelf && isSelf(exe) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := installedVersionCmd(ctx, exe)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// NoExecSelf : interdit d'exécuter le binaire en cours (« <soi> version »). Posé par le TestMain des paquets de test :
// le binaire de test relancé avec « version » rejouerait toute la suite en sous-processus (récursion, sockets tmux
// laissés). Les autres binaires (faux binaires des tests) restent exécutables.
var NoExecSelf bool

func isSelf(exe string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	a, e1 := os.Stat(self)
	b, e2 := os.Stat(exe)
	return e1 == nil && e2 == nil && os.SameFile(a, b)
}

func installedVersion() string {
	exe, err := launcherExe()
	if err != nil {
		return ""
	}
	return InstalledVersion(exe, installedVersionTimeout)
}

// LauncherState : état complet (exécute `<exe> version` : à n'appeler que sur demande, pas à chaque instantané).
func LauncherState() LauncherInfo { return LauncherStateFor(launcherMode()) }

// LauncherStateFor : comme LauncherState pour un mode donné (shell, desktop, web, pod) ; /proc n'est parcouru que
// pour la coque tmux.
func LauncherStateFor(mode string) LauncherInfo {
	li := LauncherInfo{Running: Version(), Installed: installedVersion(), BinChanged: binChanged(), Mode: mode, Stale: []StaleProc{}}
	if mode == "shell" {
		if exe, err := launcherExe(); err == nil {
			li.Stale = FindStaleProcs(procRoot, exe, InnerSocket())
		}
	}
	if LauncherBackupHook != nil {
		li.Backup = LauncherBackupHook()
	}
	return li
}

// StaleCount : nombre de processus anciens.
func (li LauncherInfo) StaleCount() int { return len(li.Stale) }

var (
	instMu   sync.Mutex
	instSig  FileSig
	instVer  string
	instBusy bool
)

// LauncherInstalledCached : version installée pour l'instantané — jamais bloquant (`<exe> version` peut durer 3 s) :
// "" tant que la lecture du binaire actuel n'est pas revenue ; relue quand le fichier change. Binaire inchangé :
// la version en cours.
func LauncherInstalledCached() string {
	if !LauncherBinChanged() {
		return Version()
	}
	exe, err := launcherExe()
	if err != nil {
		return ""
	}
	sig, err := StatSig(exe)
	if err != nil {
		return ""
	}
	instMu.Lock()
	defer instMu.Unlock()
	if sig == instSig && instVer != "" {
		return instVer
	}
	if !instBusy {
		instBusy = true
		go func() {
			v := InstalledVersion(exe, installedVersionTimeout)
			instMu.Lock()
			instSig, instVer, instBusy = sig, v, false
			instMu.Unlock()
		}()
	}
	return ""
}
