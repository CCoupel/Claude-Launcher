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
	Version string `json:"version"` // core.NgVersion (« v6.4.0 »)
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

func serverInfo() ServerInfo { return ServerInfo{Version: core.NgVersion, Boot: serverBoot} }
