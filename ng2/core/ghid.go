package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/internal/tun"
)

// Identifiant numérique GitHub d'un utilisateur (contracts/user-backends.md §2, #255).
//
// oauth2-proxy ne transmet que le login (X-Forwarded-User) ; un login peut être renommé puis repris par un autre
// compte. L'espace d'un utilisateur est donc rangé sous son identifiant numérique, résolu par l'API GitHub au premier
// accès et gardé dans <config>/users/index.json (login → id, date). Un login dont l'identifiant change est refusé
// jusqu'à action de l'administrateur (ForgetUserLogin).

// GithubUsersURL : point d'accès de la résolution (variable : tests).
var GithubUsersURL = "https://api.github.com/users/"

// ghidTTL : au-delà, l'entrée du cache est revérifiée auprès de GitHub (un login repris par un autre compte est détecté).
var ghidTTL = tun.Of(24 * time.Hour)

// ghidTimeout : durée d'une résolution.
var ghidTimeout = tun.Of(8 * time.Second)

var (
	// ErrLoginReused : le login désigne maintenant un autre compte GitHub que celui connu ; refusé (401) jusqu'à l'action de l'admin.
	ErrLoginReused = errors.New("login GitHub repris par un autre compte")
	// ErrLoginUnresolved : l'identifiant numérique n'a pu être établi (API injoignable, login inconnu) et aucune entrée n'est connue.
	ErrLoginUnresolved = errors.New("identifiant GitHub introuvable")
)

// UserIndexEntry : une entrée de users/index.json.
type UserIndexEntry struct {
	ID int64     `json:"id"`
	At time.Time `json:"at"`
}

type userIndexFile struct {
	Users map[string]UserIndexEntry `json:"users"`
}

// UsersDir : dossier des espaces des utilisateurs.
func UsersDir() string { return filepath.Join(Ng2ConfigDir(), "users") }

// UserIndexPath : users/index.json.
func UserIndexPath() string { return filepath.Join(UsersDir(), "index.json") }

// UserUID : nom de l'espace d'un compte GitHub (« github-<id> »).
func UserUID(id int64) string { return fmt.Sprintf("github-%d", id) }

var ghidMu sync.Mutex // sérialise les résolutions (un seul appel API à la fois ; le cache est relu sous verrou fichier)

func readUserIndex() (userIndexFile, error) {
	f := userIndexFile{Users: map[string]UserIndexEntry{}}
	b, err := readFileRetry(UserIndexPath())
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &f); err != nil {
			return f, fmt.Errorf("users/index.json : %w", err)
		}
		if f.Users == nil {
			f.Users = map[string]UserIndexEntry{}
		}
	case !errors.Is(err, os.ErrNotExist):
		return f, err
	}
	return f, nil
}

// githubUserID : identifiant numérique du login d'après l'API GitHub.
func githubUserID(login string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghidTimeout.Get())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GithubUsersURL+login, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "claude-launcher")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("api GitHub : %s", resp.Status)
	}
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return 0, err
	}
	if u.ID <= 0 || !strings.EqualFold(u.Login, login) { // la réponse doit bien être celle du login demandé
		return 0, fmt.Errorf("api GitHub : réponse inattendue pour %q", login)
	}
	return u.ID, nil
}

// ResolveUID : espace (« github-<id> ») du login GitHub. Cache d'abord ; entrée périmée (ghidTTL) : revérifiée, et un
// identifiant différent donne ErrLoginReused (le cache n'est pas modifié). API injoignable : l'entrée connue, même
// périmée, sert ; sans entrée, ErrLoginUnresolved.
func ResolveUID(login string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginRE.MatchString(login) {
		return "", fmt.Errorf("login invalide : %q", login)
	}
	ghidMu.Lock()
	defer ghidMu.Unlock()
	idx, err := readUserIndex()
	if err != nil {
		return "", err
	}
	cur, known := idx.Users[login]
	if known && time.Since(cur.At) < ghidTTL.Get() {
		return UserUID(cur.ID), nil
	}
	id, err := githubUserID(login)
	switch {
	case err != nil && known:
		return UserUID(cur.ID), nil // API indisponible : le dernier identifiant connu fait foi
	case err != nil:
		return "", fmt.Errorf("%w : %v", ErrLoginUnresolved, err)
	case known && id != cur.ID:
		return "", fmt.Errorf("%w (%s : %d connu, %d reçu)", ErrLoginReused, login, cur.ID, id)
	}
	err = WithFileLock(UserIndexPath(), func() error {
		f, err := readUserIndex() // relu sous verrou : un autre launcher a pu écrire
		if err != nil {
			return err
		}
		if o, ok := f.Users[login]; ok && o.ID != id {
			return fmt.Errorf("%w (%s)", ErrLoginReused, login)
		}
		f.Users[login] = UserIndexEntry{ID: id, At: time.Now().UTC()}
		b, err := json.MarshalIndent(f, "", "  ")
		if err != nil {
			return err
		}
		return WriteFileAtomic(UserIndexPath(), append(b, '\n'), 0o600)
	})
	if err != nil {
		return "", err
	}
	return UserUID(id), nil
}

// ForgetUserLogin : action de l'admin — retire le login de users/index.json (il sera résolu de nouveau à son prochain
// accès, sous son nouvel identifiant). Ne supprime aucun espace.
func ForgetUserLogin(login string) error {
	login = strings.ToLower(strings.TrimSpace(login))
	ghidMu.Lock()
	defer ghidMu.Unlock()
	return WithFileLock(UserIndexPath(), func() error {
		f, err := readUserIndex()
		if err != nil {
			return err
		}
		if _, ok := f.Users[login]; !ok {
			return nil
		}
		delete(f.Users, login)
		b, err := json.MarshalIndent(f, "", "  ")
		if err != nil {
			return err
		}
		return WriteFileAtomic(UserIndexPath(), append(b, '\n'), 0o600)
	})
}

// UserIndexLogins : logins connus et leur espace (liste des utilisateurs de l'admin ; jamais d'identifiant brut ailleurs).
func UserIndexLogins() map[string]string {
	out := map[string]string{}
	idx, err := readUserIndex()
	if err != nil {
		return out
	}
	for l, e := range idx.Users {
		out[l] = UserUID(e.ID)
	}
	return out
}
