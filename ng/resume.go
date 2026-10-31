package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Reprise après crash. Claude Code garde chaque conversation dans
// ~/.claude/projects/<chemin encodé>/<session-id>.jsonl ; « claude --resume <id> »
// la recharge avec le même id, donc la même team (session-<8 premiers caractères>)
// et sa liste de tâches, que cleanupDeadTeams épargne tant que la reprise est
// proposée. Les teammates, eux, ne reviennent pas : le leader repris reçoit une
// consigne pour les relancer d'après la liste de tâches.

// projectDirName : encodage du chemin par Claude Code (tout sauf [A-Za-z0-9] → « - »).
func projectDirName(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Resumable : dernière conversation d'un projet, et sa team s'il y en a une.
type Resumable struct {
	ID        string
	Mod       time.Time
	Teammates []string // teammates de la team de la session (perdus au crash)
	Crashed   bool     // la team désigne un leader dont le processus n'existe plus
	InUse     bool     // … ou un leader toujours en vie : session ouverte ailleurs
	Ended     bool     // close volontairement par /end-session : rien à reprendre
}

// endSessionMark : trace laissée dans la conversation par la commande /end-session.
var endSessionMark = []byte("<command-name>/end-session</command-name>")

// endedCache : verdict de sessionEnded par fichier, tant que taille et date ne
// bougent pas (le navigateur l'interroge pour chaque projet toutes les 10 s).
var endedCache = struct {
	sync.Mutex
	m map[string]endedEntry
}{m: map[string]endedEntry{}}

type endedEntry struct {
	mod   time.Time
	size  int64
	ended bool
}

func sessionEnded(f string) bool {
	info, err := os.Stat(f)
	if err != nil {
		return false
	}
	endedCache.Lock()
	e, ok := endedCache.m[f]
	endedCache.Unlock()
	if ok && e.mod.Equal(info.ModTime()) && e.size == info.Size() {
		return e.ended
	}
	ended := scanEnded(f)
	endedCache.Lock()
	endedCache.m[f] = endedEntry{info.ModTime(), info.Size(), ended}
	endedCache.Unlock()
	return ended
}

// scanEnded : la conversation f a été close par /end-session tapé par
// l'utilisateur (prompt « user » dont le contenu est la commande ; les résultats
// d'outils, qui sont des listes, peuvent citer la marque sans compter).
func scanEnded(f string) bool {
	fh, err := os.Open(f)
	if err != nil {
		return false
	}
	defer fh.Close()
	rd := bufio.NewReaderSize(fh, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if bytes.Contains(line, endSessionMark) {
			var d struct {
				Type    string `json:"type"`
				IsMeta  bool   `json:"isMeta"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			var c string
			if json.Unmarshal(line, &d) == nil && d.Type == "user" && !d.IsMeta &&
				json.Unmarshal(d.Message.Content, &c) == nil &&
				strings.HasPrefix(c, "<command-") && strings.Contains(c, string(endSessionMark)) {
				return true
			}
		}
		if err != nil {
			return false
		}
	}
}

// lastSession : conversation la plus récente du projet ; ok=false s'il n'y en a pas.
func lastSession(root, path string) (r Resumable, ok bool) {
	dir := filepath.Join(root, "projects", projectDirName(path))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return r, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		if !ok || info.ModTime().After(r.Mod) {
			r = Resumable{ID: strings.TrimSuffix(e.Name(), ".jsonl"), Mod: info.ModTime()}
			ok = true
		}
	}
	if ok {
		r.Teammates, r.Crashed, r.InUse = sessionTeam(root, r.ID)
		r.Ended = sessionEnded(filepath.Join(dir, r.ID+".jsonl"))
	}
	return r, ok
}

// teamName : nom de la team créée par une session (cf. ~/.claude/teams).
func teamName(id string) string {
	if len(id) > 8 {
		id = id[:8]
	}
	return "session-" + id
}

// sessionTeam : teammates de la team de la session id, et si son leader a disparu
// sans la libérer (leadProcess encore renseigné, processus absent) : signe de
// crash ; ou s'il tourne encore (la session est ouverte hors de cette fenêtre).
func sessionTeam(root, id string) (teammates []string, crashed, inUse bool) {
	b, err := os.ReadFile(filepath.Join(root, "teams", teamName(id), "config.json"))
	if err != nil {
		return nil, false, false
	}
	var cfg struct {
		LeadSessionID string `json:"leadSessionId"`
		LeadProcess   *struct {
			PID int `json:"pid"`
		} `json:"leadProcess"`
		Members []struct {
			Name      string `json:"name"`
			AgentType string `json:"agentType"`
		} `json:"members"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.LeadSessionID != id {
		return nil, false, false
	}
	for _, m := range cfg.Members {
		if m.AgentType != "team-lead" && m.Name != "" {
			teammates = append(teammates, m.Name)
		}
	}
	if p := cfg.LeadProcess; p != nil && p.PID > 0 {
		err := syscall.Kill(p.PID, 0)
		crashed, inUse = err == syscall.ESRCH, err == nil || err == syscall.EPERM
	}
	return teammates, crashed, inUse
}

// resumableSession : session encore récente (moins de maxAge), non close par
// /end-session, dont la team ne doit pas être nettoyée ; cherchée dans tous les projets.
func resumableSession(root, id string, maxAge time.Duration) bool {
	if maxAge <= 0 || id == "" {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(root, "projects", "*", id+".jsonl"))
	for _, f := range m {
		if info, err := os.Stat(f); err == nil && time.Since(info.ModTime()) < maxAge && !sessionEnded(f) {
			return true
		}
	}
	return false
}

// ageLabel : « 12 min », « 3 h », « 2 j ».
func ageLabel(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moins d'une minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d j", int(d.Hours()/24))
}

// resumePrompt : consigne envoyée au leader repris dont les teammates ont disparu.
func resumePrompt(r Resumable) string {
	if len(r.Teammates) == 0 {
		return ""
	}
	return "Reprise après interruption de la session : tes teammates (" + strings.Join(r.Teammates, ", ") +
		") ont été perdus et ne répondront plus. Relis la liste de tâches de la team, " +
		"relance les teammates nécessaires pour les tâches non terminées, puis poursuis le travail en cours."
}

// resumeShell : préambule bash qui pose (ou non) la question de reprise et met
// _rs à 1 si la session doit être reprise. "" si aucune reprise n'est proposée.
func (c Config) resumeShell(path string) (pre string, r Resumable, ok bool) {
	if c.ResumeMode == "never" {
		return "", r, false
	}
	if r, ok = c.pendingSession(path); !ok {
		return "", r, false
	}
	if c.ResumeMode == "always" {
		return "_rs=1; ", r, true
	}
	q := "↺ session précédente (il y a " + ageLabel(time.Since(r.Mod)) + ")"
	if r.Crashed {
		q = "⚠ session interrompue (il y a " + ageLabel(time.Since(r.Mod)) + ")"
	}
	if n := len(r.Teammates); n > 0 {
		q += fmt.Sprintf(", team de %d teammate(s) à relancer", n)
	}
	q += " — la reprendre ? [O/n] "
	return "_rs=1; read -rp " + shq(q) + ` _r; [[ "$_r" == [nN]* ]] && _rs=''; `, r, true
}

// pendingSession : dernière conversation du projet restée en suspens — ni close
// par /end-session, ni ouverte ailleurs, de moins de RESUME_MAX_AGE_H heures
// (24 h si la reprise est désactivée). C'est elle que la reprise propose, et
// elle que signale l'icône « ↺ » du navigateur.
func (c Config) pendingSession(path string) (r Resumable, ok bool) {
	maxAge := time.Duration(c.ResumeMaxAgeH) * time.Hour
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	r, ok = lastSession(claudeDir(), path)
	if !ok || r.InUse || r.Ended || time.Since(r.Mod) >= maxAge {
		return r, false
	}
	return r, true
}

// resumeMaxAge : fenêtre de reprise (0 si la reprise est désactivée).
func (c Config) resumeMaxAge() time.Duration {
	if c.ResumeMode == "never" {
		return 0
	}
	return time.Duration(c.ResumeMaxAgeH) * time.Hour
}
