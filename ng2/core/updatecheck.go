package core

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Vérification GitHub (#199, contracts/template-version.md) : jeton si disponible, requêtes conditionnelles
// (ETag / 304), résultat partagé par poste (<CacheDir>/update-check.json) et pause jusqu'à X-RateLimit-Reset.

// Err* : UpdMsg.Err.
const (
	UpdErrQuota   = "quota"
	UpdErrNetwork = "network"
)

// updateCheckTTL : durée pendant laquelle le résultat partagé évite tout appel (F5 la contourne).
const updateCheckTTL = 10 * time.Minute

// updRepo : dernier tag connu d'un dépôt.
type updRepo struct {
	Tag      string `json:"tag"`
	Sha      string `json:"sha,omitempty"`
	Etag     string `json:"etag,omitempty"`
	Released bool   `json:"released"`
}

// updCheck : contenu de update-check.json.
type updCheck struct {
	CheckedAt  string  `json:"checked_at"`
	Launcher   updRepo `json:"launcher"`
	Template   updRepo `json:"template"`
	Error      string  `json:"error"`
	RetryAfter string  `json:"retry_after"`
}

func updateCheckPath() string { return filepath.Join(CacheDir(), "update-check.json") }

func readUpdateCheck() (c updCheck) {
	if b, err := os.ReadFile(updateCheckPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// writeUpdateCheck : écriture atomique (fichier temporaire puis renommage).
func writeUpdateCheck(c updCheck) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(updateCheckPath()), "update-check-*.tmp") // unique : instances concurrentes
	if err != nil {
		return
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	if cerr := f.Close(); werr != nil || cerr != nil || os.Chmod(tmp, 0o644) != nil || os.Rename(tmp, updateCheckPath()) != nil {
		_ = os.Remove(tmp)
	}
}

func (c updCheck) retryAt() time.Time {
	t, _ := time.Parse(time.RFC3339, c.RetryAfter)
	return t
}

// githubToken : GH_TOKEN > GITHUB_TOKEN (environnement) > GITHUB_TOKEN de config.conf ; "" : appels anonymes.
// Jamais journalisé ni écrit dans un résultat.
func githubToken() string {
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		return strings.TrimSpace(ConfTokenValue(string(b)))
	}
	return ""
}

// ghHeaders : en-têtes communs des appels à l'API GitHub.
func ghHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	if t := githubToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
}

// ghError : échec d'un appel GitHub, classé.
type ghError struct {
	Kind  string // UpdErrQuota | UpdErrNetwork
	Retry time.Time
	Msg   string
}

func (e *ghError) Error() string { return e.Msg }

// quotaError : réponse 403/429 avec X-RateLimit-Remaining: 0 (429 sans en-tête : quota aussi) ; nil sinon.
func quotaError(resp *http.Response) *ghError {
	if resp.StatusCode != 403 && resp.StatusCode != 429 {
		return nil
	}
	if resp.Header.Get("X-RateLimit-Remaining") != "0" && resp.StatusCode != 429 {
		return nil
	}
	retry := time.Now().Add(15 * time.Minute)
	if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && n > 0 {
		retry = time.Unix(n, 0)
	} else if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 {
		retry = time.Now().Add(time.Duration(n) * time.Second)
	}
	return &ghError{Kind: UpdErrQuota, Retry: retry, Msg: "quota GitHub épuisé (HTTP " + strconv.Itoa(resp.StatusCode) + ")"}
}

// NextUpdateDelay : délai avant la prochaine vérification — celui du cycle (def), ou la reprise annoncée quand le
// quota est épuisé (jamais d'appel avant X-RateLimit-Reset).
func NextUpdateDelay(msg UpdMsg, def time.Duration) time.Duration {
	if msg.Err == UpdErrQuota && !msg.RetryAt.IsZero() {
		if d := time.Until(msg.RetryAt) + time.Second; d > def {
			return d
		}
	}
	return def
}
