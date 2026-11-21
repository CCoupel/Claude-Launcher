package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
	"time"
)

// ── config.conf relu à chaud, par date (#225) ─────────────────────────────

// racyWindow : sur les systèmes à date grossière (9P, MooseFS, SMB, FAT : 1 à 2 s), une édition de même taille faite dans la
// même seconde que la lecture précédente garde le même couple (date, taille) — technique « racy timestamp » de git : tant
// que la date du fichier est à moins de racyWindow de l'instant de la lecture, on ne se fie pas au couple et on compare le
// CONTENU (condensé).
//
// Dérive d'horloge (serveur MooseFS / 9P contre client) : la fenêtre se mesure en valeur ABSOLUE autour de l'instant de lecture
// (|ReadAt − MTime| < racyWindow) et une date dans le FUTUR du client est toujours « racy » — le contenu tranche tant que la
// date n'est pas nettement antérieure à la lecture.
const racyWindow = 2 * time.Second

// confAbsentGrace : durée pendant laquelle l'absence de config.conf (éditeur qui fait supprimer puis recréer) garde la copie en
// mémoire — adopter aussitôt les valeurs par défaut recopierait un jeton vide dans settings.json (famille de l'incident GH_TOKEN).
// Au-delà, le fichier est réellement absent : défauts. Variable : tests.
var confAbsentGrace = 3 * time.Second

var (
	absentMu    sync.Mutex
	absentSince time.Time // début de l'absence en cours (zéro : fichier présent)
)

// absentWithinGrace : config.conf est absent ; vrai tant que l'absence dure depuis moins de confAbsentGrace.
func absentWithinGrace() bool {
	absentMu.Lock()
	defer absentMu.Unlock()
	if absentSince.IsZero() {
		absentSince = time.Now()
	}
	return time.Since(absentSince) < confAbsentGrace
}

func noteConfPresent() {
	absentMu.Lock()
	absentSince = time.Time{}
	absentMu.Unlock()
}

// ConfStamp : empreinte de config.conf à la lecture de la copie en mémoire (zéro : absent / copie construite à la main).
// Invariant : une Config construite à la main (Config{…}) n'a pas de Stamp ; FreshConfig la tient pour périmée si le fichier
// existe. Comparable (==) : une copie inchangée garde un Stamp identique.
type ConfStamp struct {
	MTime  time.Time
	Size   int64
	ReadAt time.Time // instant de la lecture (fenêtre « racy »)
	Sum    string    // condensé du contenu lu
}

func confSum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func confStampOf(path string) ConfStamp {
	if st, err := os.Stat(path); err == nil {
		return ConfStamp{MTime: st.ModTime(), Size: st.Size()}
	}
	return ConfStamp{}
}

// ConfigStamp : date et taille actuelles de config.conf (sans lecture).
func ConfigStamp() ConfStamp { return confStampOf(ConfigPath()) }

// ConfigChanged : config.conf a changé depuis la lecture décrite par cur. Date ou taille différentes ; ou, dans la fenêtre
// « racy », contenu différent. Une erreur transitoire (Stat ou lecture impossible hors « absent ») n'est PAS un changement :
// la copie en mémoire est gardée, le tour suivant réessaie.
func ConfigChanged(cur ConfStamp) bool {
	path := ConfigPath()
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return !cur.MTime.IsZero() // disparu depuis la lecture
		}
		return false
	}
	if !st.ModTime().Equal(cur.MTime) || st.Size() != cur.Size {
		return true
	}
	if d := cur.ReadAt.Sub(cur.MTime); cur.ReadAt.IsZero() || d < racyWindow { // date proche de la lecture ou dans le futur : le contenu tranche
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return false
		}
		return confSum(b) != cur.Sum
	}
	return false
}

// Same : conservé pour les appelants qui comparent deux empreintes (date, taille).
func (s ConfStamp) Same(o ConfStamp) bool { return s == o }

// FreshConfig : la configuration à jour. cur inchangée si config.conf n'a pas changé (ConfigChanged) ; sinon relue. Un
// config.conf INVALIDE garde la dernière copie valide (cur, empreinte mise à jour : pas de revalidation ni de nouveau message
// avant la prochaine modification) et le dit dans warn. Les racines (Roots), figées au démarrage par la surveillance de
// l'arborescence, sont conservées. Un fichier illisible (erreur transitoire) garde cur. Le contenu VALIDÉ est celui qui est
// chargé : un fichier modifié entre la validation et le chargement est revu au tour suivant. Garde « jamais plus ancien »
// avant toute recopie dans settings.json.
func FreshConfig(cur Config) (cfg Config, warn string) {
	if !ConfigChanged(cur.Stamp) {
		return cur, ""
	}
	b, err := os.ReadFile(ConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return cur, "" // illisible maintenant : ne pas retomber sur les valeurs par défaut, réessayer
	}
	if err == nil {
		noteConfPresent()
	} else if !cur.Stamp.MTime.IsZero() && absentWithinGrace() { // supprimé puis recréé par un éditeur : copie gardée un moment
		return cur, ""
	}
	if err == nil {
		if ps := ValidateConfig(ConfConf, b); HasErrors(ps) {
			cur.Stamp = ConfStamp{MTime: confStampOf(ConfigPath()).MTime, Size: int64(len(b)), ReadAt: time.Now(), Sum: confSum(b)}
			return cur, "config.conf invalide : dernière copie valide conservée — " + firstProblem(ps)
		}
	}
	fresh := LoadConfig()
	if err == nil && fresh.Stamp.Sum != confSum(b) { // modifié entre la validation et le chargement : non validé, au tour suivant
		return cur, ""
	}
	fresh.Roots = cur.Roots
	return fresh, ""
}

func firstProblem(ps []ConfigProblem) string {
	for _, p := range ps {
		if p.Severity == SevError {
			return p.Message
		}
	}
	return ""
}
