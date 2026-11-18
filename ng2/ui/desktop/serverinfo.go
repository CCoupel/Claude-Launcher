package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"claude-launcher-ng2/core"
)

// Version et identifiant de démarrage du serveur (#198, contracts/web-server.md « Version du serveur ») : portés par
// chaque `init` et chaque instantané. Le client web garde ceux du premier `init` et, après une reconnexion du flux,
// distingue « version différente » (serveur mis à jour : recharger la page) de « même version, autre boot »
// (serveur simplement redémarré : reconnecter les terminaux).

// ServerInfo : serveur qui répond.
type ServerInfo struct {
	Version string `json:"version"` // core.Version() (« v6.4.0 »)
	Boot    string `json:"boot"`    // aléatoire, différent à chaque démarrage du processus
}

// serverBoot : fixé au démarrage du processus (16 octets aléatoires).
var serverBoot = func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil { // improbable ; l'heure suffit à distinguer deux démarrages
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}()

func serverInfo() ServerInfo { return ServerInfo{Version: core.Version(), Boot: serverBoot} }

// projectRootByName : racine (chemin vu des sessions) du projet connu de ce launcher dont le nom est name — le nom du
// projet dans le launcher, <catégorie>-<projet> (core.CtrName) ; ce que /files/<projet>/… résout (#174).
func (a *App) projectRootByName(name string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, n := range a.nodes {
		if n.Project && core.CtrName(n.Path) == name {
			return core.RemotePath(n.Path), true
		}
	}
	return "", false
}
