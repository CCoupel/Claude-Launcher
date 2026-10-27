package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

// Même fichier de cache que le launcher bash : les deux versions cohabitent.
func usageCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher", "usage.json")
}

type Limit struct {
	Kind     string  `json:"kind"`
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	Severity string  `json:"severity"`
	ResetsAt string  `json:"resets_at"`
	Scope    struct {
		Surface string `json:"surface"`
		Model   struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

func (l Limit) Label() string {
	switch l.Kind {
	case "session":
		return "Session"
	case "weekly_all":
		return "Semaine"
	}
	s := l.Scope.Model.DisplayName
	if s == "" {
		s = l.Scope.Model.ID
	}
	if s == "" {
		s = l.Scope.Surface
	}
	weekly := l.Kind == "weekly_scoped" || l.Group == "weekly"
	switch {
	case weekly && s != "":
		return s + " sem."
	case weekly:
		return "Semaine (autre)"
	case s != "":
		return s + " " + l.Kind
	}
	return l.Kind
}

func (l Limit) rank() int {
	switch l.Kind {
	case "session":
		return 0
	case "weekly_all":
		return 1
	case "weekly_scoped":
		return 2
	}
	return 9
}

// Usage est l'état affiché : contenu du cache + éventuel message d'échec.
type Usage struct {
	Limits  []Limit
	ReadAt  time.Time // mtime du cache
	Err     string
	Loading bool
	Expired bool      // connexion claude.ai expirée ou refusée : un /login s'impose
	LoginBy time.Time // fin de validité de la connexion, si elle approche (alerte)
}

// loginWarn : délai avant expiration de la connexion à partir duquel on prévient.
const loginWarn = 3 * 24 * time.Hour

// loginExpiry : fin de validité de la connexion claude.ai. Le jeton d'accès ne dure
// que quelques heures et claude le renouvelle seul ; c'est l'expiration du jeton de
// renouvellement (~30 jours) qui impose un /login. Zéro si inconnue (clé API…).
func loginExpiry() time.Time {
	b, err := os.ReadFile(credentialsPath())
	if err != nil {
		return time.Time{}
	}
	var creds struct {
		O struct {
			ExpiresAt        int64 `json:"expiresAt"`
			RefreshExpiresAt int64 `json:"refreshTokenExpiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &creds) != nil {
		return time.Time{}
	}
	ms := creds.O.RefreshExpiresAt
	if ms == 0 {
		ms = creds.O.ExpiresAt
	}
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// authErr : message d'échec écrit par fetchUsage sur un HTTP 401/403.
const authErr = "non authentifié — lancer claude puis /login"

func readUsage() Usage {
	path := usageCachePath()
	var u Usage
	if b, err := os.ReadFile(path + ".err"); err == nil {
		u.Err = strings.TrimSpace(string(b))
	}
	if exp := loginExpiry(); !exp.IsZero() && time.Until(exp) < loginWarn {
		u.LoginBy = exp
	}
	u.Expired = u.Err == authErr || (!u.LoginBy.IsZero() && !time.Now().Before(u.LoginBy))
	st, err := os.Stat(path)
	if err != nil {
		u.Loading = u.Err == ""
		return u
	}
	u.ReadAt = st.ModTime()
	b, _ := os.ReadFile(path)
	var doc struct {
		Limits []Limit `json:"limits"`
	}
	if json.Unmarshal(b, &doc) == nil {
		for _, l := range doc.Limits {
			if l.Kind != "" {
				u.Limits = append(u.Limits, l)
			}
		}
	}
	sort.SliceStable(u.Limits, func(i, j int) bool {
		a, b := u.Limits[i], u.Limits[j]
		if a.rank() != b.rank() {
			return a.rank() < b.rank()
		}
		return a.Label() < b.Label()
	})
	return u
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

// needsFetch applique la politique du bash : pas d'appel si le cache est frais
// (< 1,5 × intervalle) ou si un échec récent suspend les appels.
func needsFetch(refreshSec int) bool {
	path := usageCachePath()
	interval := time.Duration(refreshSec) * time.Second
	backoff := 2 * interval
	if backoff < 5*time.Minute {
		backoff = 5 * time.Minute
	}
	// Un /login réécrit les identifiants : l'échec précédent ne vaut plus, on réessaie.
	if st, err := os.Stat(path + ".err"); err == nil && time.Since(st.ModTime()) < backoff {
		if cs, err := os.Stat(credentialsPath()); err != nil || !cs.ModTime().After(st.ModTime()) {
			return false
		}
	}
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < interval*3/2 {
		return false
	}
	return true
}

// fetchUsage interroge l'endpoint et met à jour le cache de façon atomique.
func fetchUsage(refreshSec int) {
	path := usageCachePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	fail := func(msg string) { _ = os.WriteFile(path+".err", []byte(msg), 0o644) }

	cb, err := os.ReadFile(credentialsPath())
	var creds struct {
		O struct {
			Token string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err != nil || json.Unmarshal(cb, &creds) != nil || creds.O.Token == "" {
		fail("pas de connexion claude.ai (clé API ?) — lancer claude puis /login")
		return
	}
	req, _ := http.NewRequest("GET", usageURL, nil)
	req.Header.Set("Authorization", "Bearer "+creds.O.Token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		fail("endpoint indisponible")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == 200 && json.Valid(body) && strings.Contains(string(body), `"limits"`):
		tmp := fmt.Sprintf("%s.%d", path, os.Getpid())
		if os.WriteFile(tmp, body, 0o644) == nil && os.Rename(tmp, path) == nil {
			_ = os.Remove(path + ".err")
		}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		fail(authErr)
	case resp.StatusCode == 429:
		fail("trop d’appels à l’endpoint, nouvel essai dans quelques minutes")
	default:
		fail(fmt.Sprintf("endpoint indisponible (HTTP %d)", resp.StatusCode))
	}
}

// openLogin ouvre dans la session des projets une fenêtre « login » qui lance
// claude /login ; elle se ferme quand on quitte claude.
func openLogin(session string) error {
	sess := targetSession(session)
	if tmuxWindows(sess)["login"] {
		return dtmux("select-window", "-t", exactW(sess, "login")).Run()
	}
	return dtmux("new-window", "-t", exactS(sess)+":", "-n", "login", claudeBin()+" /login").Run()
}

var jours = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}

// formatReset : « 42 min », « 15:00 », « demain 12:00 », « mer. 12:00 ».
func formatReset(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	t = t.Add(30 * time.Second).Truncate(time.Minute).Local()
	now := time.Now()
	if !t.After(now) {
		return "passée"
	}
	diff := int(math.Round(t.Sub(now).Minutes()))
	switch {
	case diff <= 0:
		return "imminent"
	case diff < 60:
		return fmt.Sprintf("%d min", diff)
	}
	hm := t.Format("15:04")
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return hm
	case t.YearDay() == now.AddDate(0, 0, 1).YearDay() && y1 == now.AddDate(0, 0, 1).Year():
		return "demain " + hm
	}
	return jours[t.Weekday()] + " " + hm
}
