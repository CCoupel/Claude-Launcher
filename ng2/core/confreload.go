package core

import (
	"os"
	"time"
)

// ── config.conf relu à chaud, par date (#225) ─────────────────────────────

// ConfStamp : date et taille de config.conf (zéro : absent). Pas de surveillance de fichier (9P, MooseFS) : on compare.
type ConfStamp struct {
	MTime time.Time
	Size  int64
}

func confStampOf(path string) ConfStamp {
	if st, err := os.Stat(path); err == nil {
		return ConfStamp{MTime: st.ModTime(), Size: st.Size()}
	}
	return ConfStamp{}
}

// ConfigStamp : état actuel de config.conf.
func ConfigStamp() ConfStamp { return confStampOf(ConfigPath()) }

// Same : mêmes date et taille.
func (s ConfStamp) Same(o ConfStamp) bool { return s.MTime.Equal(o.MTime) && s.Size == o.Size }

// FreshConfig : la configuration à jour. cur inchangée si config.conf n'a pas changé depuis sa lecture (date, taille) ;
// sinon relue. Un config.conf INVALIDE garde la dernière copie valide (cur, marquée vue : pas de revalidation à chaque appel)
// et le dit dans warn (une seule fois par modification). Les racines (Roots), figées au démarrage par la surveillance de
// l'arborescence, sont conservées. Garde « jamais plus ancien » avant toute recopie dans settings.json.
func FreshConfig(cur Config) (cfg Config, warn string) {
	now := ConfigStamp()
	if now.Same(cur.Stamp) {
		return cur, ""
	}
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		if ps := ValidateConfig(ConfConf, b); HasErrors(ps) {
			cur.Stamp = now
			return cur, "config.conf invalide : dernière copie valide conservée — " + firstProblem(ps)
		}
	}
	fresh := LoadConfig()
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
