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
	"strconv"
	"strings"
	"time"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

// Contrat avec le template : le panneau milestone des projets lit (et alimente)
// ~/.cache/claude-launcher/usage.json (et usage.json.err) — chemin partagé avec
// le launcher v4, PAS dans le cache propre à NG2.
//
// Ce chemin n'est commun qu'à une même machine : le $HOME d'un conteneur lui est
// propre, si bien que le panneau de chaque projet en conteneur appelait
// l'endpoint de son côté, sans voir le cache ni les 429 des autres (appels
// cumulés → 429 sans fin). Le vrai cache est donc dans la config claude
// (sharedUsageDir), montée au même chemin dans tous les conteneurs et propre au
// compte, comme l'endpoint :
//   - le hub y lit et écrit directement ;
//   - sur la machine du hub, ~/.cache/claude-launcher garde ses autres fichiers
//     (v4, état…) : seuls usage.json et usage.json.err y deviennent des liens ;
//   - dans un conteneur, le dossier entier devient un lien (linkUsageScript).
// Template inchangé : il suit les liens.

// legacyUsagePath : ~/.cache/claude-launcher/usage.json (chemin du contrat).
func legacyUsagePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher", "usage.json")
}

// sharedUsageDir : dossier du cache de consommation commun à tous.
func sharedUsageDir() string { return filepath.Join(claudeDir(), "claude-launcher-cache") }

// usageCachePath : cache lu et écrit par le hub (commun).
func usageCachePath() string { return filepath.Join(sharedUsageDir(), "usage.json") }

// linkUsageCache : sur la machine du hub, usage.json et usage.json.err de
// ~/.cache/claude-launcher deviennent des liens vers le cache commun (un
// usage.json local plus récent y est d'abord copié). Rejoué à chaque relevé :
// une écriture du panneau par remplacement de fichier casserait le lien.
func linkUsageCache() {
	dir := filepath.Dir(legacyUsagePath())
	target := sharedUsageDir()
	if os.MkdirAll(target, 0o755) != nil {
		return
	}
	if t, err := os.Readlink(dir); err == nil && t == target {
		return // dossier entier déjà lié
	}
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	for _, n := range []string{"usage.json", "usage.json.err", "usage.json.wait"} {
		l, t := filepath.Join(dir, n), filepath.Join(target, n)
		if cur, err := os.Readlink(l); err == nil && cur == t {
			continue
		}
		// fichier local (avant la v5.4.1) plus récent que le commun : copié, date
		// comprise (celle de .err porte l'attente après un 429) — copie et non
		// rename : disque local → stockage partagé
		if ls, err := os.Lstat(l); err == nil && ls.Mode().IsRegular() {
			if ts, err := os.Stat(t); err != nil || ls.ModTime().After(ts.ModTime()) {
				if b, err := os.ReadFile(l); err == nil && os.WriteFile(t+".tmp", b, 0o644) == nil {
					_ = os.Rename(t+".tmp", t)
					_ = os.Chtimes(t, ls.ModTime(), ls.ModTime())
				}
			}
		}
		_ = os.Remove(l)
		if n != "usage.json.wait" { // .wait : propre au launcher, pas de lien
			_ = os.Symlink(t, l)
		}
	}
}

// linkUsageScript : même lien, dans un conteneur (en utilisateur, à chaque ouverture).
// Un conteneur local créé avant la v5.4 monte ~/.cache/claude-launcher : ce
// montage, qui désigne déjà le dossier commun (-ef), est laissé tel quel.
func linkUsageScript() string {
	return `d="$HOME/.cache/claude-launcher"; t=` + shq(sharedUsageDir()) + `
mkdir -p "$HOME/.cache" "$t"
# déjà le dossier commun (lien, ou montage d'un conteneur local créé avant la v5.4) : rien
if [ "$(readlink "$d" 2>/dev/null)" != "$t" ] && ! [ "$d" -ef "$t" ]; then rm -rf "$d"; ln -sfn "$t" "$d"; fi
`
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
	// Échec récent (sa date peut être dans le futur : attente prolongée après un
	// 429). Un /login réécrit les identifiants : l'échec ne vaut plus, on réessaie
	// — sauf après un 429 (claude renouvelle seul son jeton toutes
	// les quelques heures, ce qui lèverait l'attente).
	if st, err := os.Stat(path + ".err"); err == nil && time.Since(st.ModTime()) < backoff {
		b, _ := os.ReadFile(path + ".err")
		cs, cerr := os.Stat(credentialsPath())
		if strings.HasPrefix(string(b), "trop d") || cerr != nil || !cs.ModTime().After(st.ModTime()) {
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
			_ = os.Remove(path + ".wait")
		}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		fail(authErr)
	case resp.StatusCode == 429:
		wait := tooMany(path, resp.Header.Get("Retry-After"))
		fail("trop d’appels à l’endpoint, nouvel essai vers " + time.Now().Add(wait).Format("15:04"))
		// .err daté de façon que l'attente commune (5 min après sa date, pour le
		// launcher comme pour le panneau du template) finisse dans « wait »
		at := time.Now().Add(wait - usageMinBackoff)
		_ = os.Chtimes(path+".err", at, at)
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

// usageMinBackoff : pause minimale après un échec (launcher et panneau du template).
const usageMinBackoff = 5 * time.Minute

// usageMaxWait : attente maximale entre deux essais après des 429 successifs.
var usageMaxWait = 30 * time.Minute

// tooMany : attente après un 429 — Retry-After s'il est donné (secondes), sinon
// le double de la précédente (5 min, 10, 20, puis 30 au plus) ; notée dans
// <cache>.wait, effacée au premier succès.
func tooMany(path, retryAfter string) time.Duration {
	wait := usageMinBackoff
	if n, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && n > 0 {
		wait = time.Duration(n) * time.Second
	} else if b, err := os.ReadFile(path + ".wait"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			wait = 2 * time.Duration(n) * time.Second
		}
	}
	if wait < usageMinBackoff {
		wait = usageMinBackoff
	}
	if wait > usageMaxWait {
		wait = usageMaxWait
	}
	_ = os.WriteFile(path+".wait", []byte(strconv.Itoa(int(wait.Seconds()))), 0o644)
	return wait
}
