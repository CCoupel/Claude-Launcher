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

// ghidRetry : après un échec de l'API GitHub, aucune nouvelle tentative pour ce login avant ce délai (l'entrée connue sert).
var ghidRetry = tun.Of(5 * time.Minute)

// ghidNow : horloge (tests).
var ghidNow = tun.Of(time.Now)

// cache mémoire du résultat de users/index.json et état des résolutions. ghidMu ne protège QUE ces structures : jamais tenu
// pendant un appel réseau ni une écriture de fichier (SpaceOf est appelé à chaque requête).
var (
	ghidMu      sync.Mutex
	ghidPath    string                    // index chargé en mémoire (change si le dossier de config change)
	ghidSig     string                    // taille + date du fichier au chargement : relu seulement s'il a changé
	ghidIdx     map[string]UserIndexEntry // login → entrée
	ghidRetryAt = map[string]time.Time{}  // login → pas d'appel API avant
	ghidBlock   = map[string]bool{}       // login repris par un autre compte : refusé jusqu'à l'action de l'admin
	ghidFly     = map[string]*ghidCall{}  // résolutions en cours (un seul vol par login)
)

type ghidCall struct {
	done chan struct{}
	uid  string
	err  error
}

// gk : clé d'état d'un login (propre au dossier de config : plusieurs configurations dans un même processus, tests).
func gk(login string) string { return UserIndexPath() + "\x00" + login }

func fileSig(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "absent"
	}
	return fmt.Sprintf("%d/%d", st.Size(), st.ModTime().UnixNano())
}

// ghidLoad : index en mémoire (relu seulement si le fichier a changé). ghidMu tenu.
func ghidLoad() error {
	p := UserIndexPath()
	if sig := fileSig(p); p == ghidPath && sig == ghidSig && ghidIdx != nil {
		return nil
	}
	f, err := readUserIndex()
	if err != nil {
		return err
	}
	ghidPath, ghidSig, ghidIdx = p, fileSig(p), f.Users
	return nil
}

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

// ResolveUID : espace (« github-<id> ») du login GitHub. Chemin chaud : cache mémoire, sans appel réseau. Entrée absente ou
// périmée (ghidTTL) : UN SEUL appel à l'API par login (les autres requêtes du même login attendent ce résultat s'il n'y a pas
// d'entrée, sinon servent l'entrée connue sans attendre), jamais sous verrou global. Un identifiant différent donne
// ErrLoginReused jusqu'à ForgetUserLogin. Échec de l'API : l'entrée connue sert, et aucune nouvelle tentative avant ghidRetry ;
// sans entrée : ErrLoginUnresolved.
func ResolveUID(login string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginRE.MatchString(login) {
		return "", fmt.Errorf("login invalide : %q", login)
	}
	now := ghidNow.Get()()
	ghidMu.Lock()
	if ghidBlock[gk(login)] {
		ghidMu.Unlock()
		return "", fmt.Errorf("%w (%s)", ErrLoginReused, login)
	}
	if err := ghidLoad(); err != nil {
		ghidMu.Unlock()
		return "", err
	}
	cur, known := ghidIdx[login]
	if known && now.Sub(cur.At) < ghidTTL.Get() {
		ghidMu.Unlock()
		return UserUID(cur.ID), nil
	}
	if until, ok := ghidRetryAt[gk(login)]; ok && now.Before(until) { // échec récent : pas d'appel
		ghidMu.Unlock()
		if known {
			return UserUID(cur.ID), nil
		}
		return "", fmt.Errorf("%w : API GitHub indisponible, nouvel essai plus tard", ErrLoginUnresolved)
	}
	if c, flying := ghidFly[gk(login)]; flying {
		ghidMu.Unlock()
		if known { // une résolution est en cours : l'entrée connue sert sans attendre
			return UserUID(cur.ID), nil
		}
		<-c.done
		return c.uid, c.err
	}
	c := &ghidCall{done: make(chan struct{})}
	ghidFly[gk(login)] = c
	ghidMu.Unlock()

	c.uid, c.err = ghidRefresh(login, cur, known)
	ghidMu.Lock()
	delete(ghidFly, gk(login))
	ghidMu.Unlock()
	close(c.done)
	return c.uid, c.err
}

// ghidRefresh : appel à l'API (hors verrou global) puis mise à jour du cache mémoire et du fichier.
func ghidRefresh(login string, cur UserIndexEntry, known bool) (string, error) {
	id, err := githubUserID(login)
	now := ghidNow.Get()()
	switch {
	case err != nil:
		ghidMu.Lock()
		ghidRetryAt[gk(login)] = now.Add(ghidRetry.Get())
		ghidMu.Unlock()
		if known {
			return UserUID(cur.ID), nil // API indisponible : le dernier identifiant connu fait foi
		}
		return "", fmt.Errorf("%w : %v", ErrLoginUnresolved, err)
	case known && id != cur.ID:
		ghidMu.Lock()
		ghidBlock[gk(login)] = true
		ghidMu.Unlock()
		return "", fmt.Errorf("%w (%s : %d connu, %d reçu)", ErrLoginReused, login, cur.ID, id)
	}
	e := UserIndexEntry{ID: id, At: now.UTC()}
	ghidMu.Lock()
	delete(ghidRetryAt, gk(login))
	if ghidIdx != nil && ghidPath == UserIndexPath() {
		ghidIdx[login] = e // le cache sert tout de suite ; le fichier suit (sa signature changera : relu à la prochaine requête)
	}
	ghidMu.Unlock()
	err = WithFileLock(UserIndexPath(), func() error {
		f, err := readUserIndex() // relu sous verrou fichier : un autre launcher a pu écrire
		if err != nil {
			return err
		}
		if o, ok := f.Users[login]; ok && o.ID != id {
			ghidMu.Lock()
			ghidBlock[gk(login)] = true
			ghidMu.Unlock()
			return fmt.Errorf("%w (%s)", ErrLoginReused, login)
		}
		f.Users[login] = e
		return writeUserIndex(f)
	})
	if err != nil {
		return "", err
	}
	return UserUID(id), nil
}

func writeUserIndex(f userIndexFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(UserIndexPath(), append(b, '\n'), 0o600)
}

// ForgetUserLogin : action de l'admin — retire le login de users/index.json et de la mémoire, lève le refus d'un login repris
// (il sera résolu de nouveau à son prochain accès, sous son nouvel identifiant). Ne supprime aucun espace.
func ForgetUserLogin(login string) error {
	login = strings.ToLower(strings.TrimSpace(login))
	ghidMu.Lock()
	delete(ghidBlock, gk(login))
	delete(ghidRetryAt, gk(login))
	if ghidIdx != nil && ghidPath == UserIndexPath() {
		delete(ghidIdx, login)
	}
	ghidMu.Unlock()
	return WithFileLock(UserIndexPath(), func() error {
		f, err := readUserIndex()
		if err != nil {
			return err
		}
		if _, ok := f.Users[login]; !ok {
			return nil
		}
		delete(f.Users, login)
		return writeUserIndex(f)
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
