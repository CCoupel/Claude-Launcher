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
	Pid  int    `json:"pid"`
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
	startStamp     os.FileInfo
)

// MarkLauncherStart : mémorise l'identité du binaire au démarrage (appelé tôt dans main ; sinon au premier usage).
func MarkLauncherStart() {
	startStampOnce.Do(func() {
		if exe, err := launcherExe(); err == nil {
			startStamp, _ = os.Stat(exe)
		}
	})
}

// binChanged : le fichier du binaire n'est plus celui du démarrage (remplacé : autre inode, date ou taille ;
// supprimé : introuvable).
func binChanged() bool {
	MarkLauncherStart()
	if startStamp == nil {
		return false
	}
	exe, err := launcherExe()
	if err != nil {
		return true
	}
	cur, err := os.Stat(exe)
	if err != nil {
		return true
	}
	return !os.SameFile(startStamp, cur) || cur.ModTime() != startStamp.ModTime() || cur.Size() != startStamp.Size()
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

// installedVersion : « <exe> version » (« claude-launcher-ng2 v6.6.0 »), 3 s ; vide si illisible.
func installedVersion() string {
	exe, err := launcherExe()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), installedVersionTimeout)
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

// LauncherState : état complet (exécute `<exe> version` : à n'appeler que sur demande, pas à chaque instantané).
func LauncherState() LauncherInfo {
	li := LauncherInfo{Running: Version(), Installed: installedVersion(), BinChanged: binChanged(), Mode: launcherMode(), Stale: []StaleProc{}}
	if launcherMode() == "shell" {
		li.Stale = StaleProcs()
	}
	if LauncherBackupHook != nil {
		li.Backup = LauncherBackupHook()
	}
	return li
}

// StaleCount : nombre de processus anciens.
func (li LauncherInfo) StaleCount() int { return len(li.Stale) }
