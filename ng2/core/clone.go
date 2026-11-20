package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"claude-launcher-ng2/platform"
)

// ── Clonage d'un dépôt à la création d'un projet (#215) ───────────────────
//
// Sécurité : seules les URL https:// et ssh (ssh://, user@hôte:chemin) sont admises — jamais `-…`, `file://`, `ext::`,
// un chemin local ni des identifiants dans l'URL. Le jeton GitHub de config.conf passe par GIT_ASKPASS (un script
// qui lit une variable d'environnement du seul processus git) : jamais dans argv, ni l'URL, ni .git/config (aucun
// credential helper), ni les messages (masqué). Il n'est présenté qu'à github.com. ssh : clé et known_hosts du coffre
// du backend, StrictHostKeyChecking=yes (sshSecretArgs).

// CloneAuth : authentification du clonage.
type CloneAuth struct {
	Token   string // jeton GitHub (HTTPS, github.com seulement) ; vide : aucun
	Backend string // backend dont le coffre fournit la clé ssh ; vide : « host »
}

var (
	cloneHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	cloneUserRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	cloneScpRe  = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9._-]*)@([A-Za-z0-9][A-Za-z0-9.-]*):(\S+)$`)
)

// CloneURLInfo : URL de clonage admise.
type CloneURLInfo struct {
	Scheme string // https | ssh
	Host   string
}

// ValidateCloneURL : refuse tout ce qui n'est pas https:// ou ssh (ssh://, user@hôte:chemin).
func ValidateCloneURL(raw string) (CloneURLInfo, error) {
	u := strings.TrimSpace(raw)
	switch {
	case u == "":
		return CloneURLInfo{}, errors.New("URL requise")
	case strings.IndexFunc(u, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return CloneURLInfo{}, errors.New("URL invalide (espace ou caractère de contrôle)")
	case strings.HasPrefix(u, "-"):
		return CloneURLInfo{}, errors.New("URL invalide (ne commence pas par -)")
	}
	low := strings.ToLower(u)
	switch {
	case strings.HasPrefix(low, "https://"):
		p, err := url.Parse(u)
		if err != nil || p.Host == "" || p.Hostname() == "" || !cloneHostRe.MatchString(p.Hostname()) {
			return CloneURLInfo{}, errors.New("URL https invalide")
		}
		if p.User != nil {
			return CloneURLInfo{}, errors.New("URL refusée : pas d'identifiants dans l'URL (le jeton est lu de config.conf)")
		}
		if strings.Trim(p.Path, "/") == "" {
			return CloneURLInfo{}, errors.New("URL https invalide (dépôt absent)")
		}
		return CloneURLInfo{Scheme: "https", Host: strings.ToLower(p.Hostname())}, nil
	case strings.HasPrefix(low, "ssh://"):
		p, err := url.Parse(u)
		if err != nil || p.Hostname() == "" || !cloneHostRe.MatchString(p.Hostname()) {
			return CloneURLInfo{}, errors.New("URL ssh invalide")
		}
		if p.User != nil {
			if _, hasPw := p.User.Password(); hasPw || !cloneUserRe.MatchString(p.User.Username()) {
				return CloneURLInfo{}, errors.New("URL ssh invalide (utilisateur)")
			}
		}
		if strings.Trim(p.Path, "/") == "" {
			return CloneURLInfo{}, errors.New("URL ssh invalide (dépôt absent)")
		}
		return CloneURLInfo{Scheme: "ssh", Host: strings.ToLower(p.Hostname())}, nil
	}
	if m := cloneScpRe.FindStringSubmatch(u); m != nil && !strings.Contains(u, "://") && !strings.HasPrefix(m[3], "-") {
		return CloneURLInfo{Scheme: "ssh", Host: strings.ToLower(m[2])}, nil
	}
	return CloneURLInfo{}, errors.New("URL refusée : https://… ou ssh (git@hôte:dépôt, ssh://…) seulement (ni file://, ni chemin local)")
}

// askpassScript : lu par git ; le jeton vient de CL_GIT_TOKEN (environnement du seul processus git). Il ne répond qu'aux
// invites qui nomment EXACTEMENT github.com (« …'https://github.com': », « …'https://x-access-token@github.com': ») :
// toute autre invite (hôte redirigé ou réécrit) est refusée, sans rien écrire.
const askpassScript = `#!/bin/sh
case "$1" in
*"//github.com'"*|*"@github.com'"*) ;;
*) exit 1 ;;
esac
case "$1" in
*sername*) printf '%s\n' "x-access-token" ;;
*) printf '%s\n' "$CL_GIT_TOKEN" ;;
esac
`

var askpassPlain = regexp.MustCompile(`^[A-Za-z0-9/._:+-]+$`)

// askpassValue : valeur de GIT_ASKPASS — git passe par un shell dès que la commande contient un espace ou un
// caractère spécial (chemin d'un profil « Prénom Nom ») : guillemetée alors.
func askpassValue(path string) string {
	p := filepath.ToSlash(path)
	if askpassPlain.MatchString(p) {
		return p
	}
	return Shq(p)
}

// envWithoutGitConfig : environnement hérité sans configuration git injectée (GIT_CONFIG_PARAMETERS, COUNT/KEY_n/VALUE_n),
// ni GIT_EXEC_PATH ni GIT_PROXY_COMMAND : rien ne peut réécrire l'URL (url.insteadOf) après sa validation.
func envWithoutGitConfig() []string {
	var out []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			continue
		}
		switch k {
		case "GIT_ASKPASS", "SSH_ASKPASS", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS",
			"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "GIT_EXEC_PATH", "GIT_PROXY_COMMAND", "CL_GIT_TOKEN":
			continue
		}
		out = append(out, e)
	}
	return out
}

// cloneEnv : environnement du processus git (jamais interactif, protocoles limités, configuration utilisateur
// neutralisée : GIT_CONFIG_GLOBAL/SYSTEM = emptyCfg, un fichier vide).
func cloneEnv(info CloneURLInfo, dir string, auth CloneAuth, askpass, emptyCfg string) []string {
	env := WithSafeDirectory(envWithoutGitConfig(), dir)
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_ALLOW_PROTOCOL=https:ssh", "LC_ALL=C", "LANGUAGE=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+emptyCfg, "GIT_CONFIG_SYSTEM="+emptyCfg)
	switch info.Scheme {
	case "https":
		if auth.Token != "" && info.Host == "github.com" && askpass != "" {
			env = append(env, "GIT_ASKPASS="+askpassValue(askpass), "CL_GIT_TOKEN="+auth.Token)
		}
	case "ssh":
		be := auth.Backend
		if be == "" {
			be = "host"
		}
		args := append([]string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"}, sshSecretArgs(be)...)
		var b strings.Builder
		for i, a := range args {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(Shq(a))
		}
		env = append(env, "GIT_SSH_COMMAND="+b.String())
	}
	return env
}

// RedactSecret : s sans secret (remplacé par ***).
func RedactSecret(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

var pctRe = regexp.MustCompile(`(\d{1,3})%`)

// cloneProgress : « Receiving objects:  43% (…) » → étape et pourcentage global (0-100).
func cloneProgress(line string) (step string, pct int, ok bool) {
	switch {
	case strings.Contains(line, "Receiving objects"):
		step = "réception"
	case strings.Contains(line, "Resolving deltas"):
		step = "résolution des deltas"
	case strings.Contains(line, "Updating files") || strings.Contains(line, "Checking out files"):
		step = "extraction"
	case strings.Contains(line, "Counting objects") || strings.Contains(line, "Compressing objects") || strings.Contains(line, "Enumerating objects"):
		step = "préparation"
	default:
		return "", 0, false
	}
	m := pctRe.FindStringSubmatch(line)
	if m == nil {
		return step, 0, step == "préparation"
	}
	n, _ := strconv.Atoi(m[1])
	if n > 100 {
		n = 100
	}
	switch step {
	case "réception":
		return step, n * 80 / 100, true
	case "résolution des deltas":
		return step, 80 + n*15/100, true
	case "extraction":
		return step, 95 + n*5/100, true
	}
	return step, 0, true
}

// cloneFailure : message court d'un échec git (secret masqué), raison reconnue d'abord.
func cloneFailure(stderr string, info CloneURLInfo, token string) string {
	s := stderr
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "host key verification failed"):
		return "hôte inconnu : known_hosts du coffre à compléter (ssh)"
	case strings.Contains(low, "permission denied (publickey") || strings.Contains(low, "could not read from remote repository") && info.Scheme == "ssh" && strings.Contains(low, "permission denied"):
		return "authentification refusée (ssh, clé du coffre)"
	case strings.Contains(low, "authentication failed") || strings.Contains(low, "terminal prompts disabled") || strings.Contains(low, "could not read username") || strings.Contains(low, "invalid username or password"):
		return "authentification refusée (HTTPS, jeton GITHUB_TOKEN de config.conf)"
	case strings.Contains(low, "repository not found") || strings.Contains(low, "not found") && strings.Contains(low, "repository"):
		return "dépôt introuvable (ou accès refusé)"
	case strings.Contains(low, "could not resolve host") || strings.Contains(low, "network is unreachable") || strings.Contains(low, "connection timed out"):
		return "hôte injoignable"
	case strings.Contains(low, "already exists and is not an empty directory"):
		return "le dossier du projet n'est pas vide"
	}
	lines := strings.Split(strings.TrimSpace(RedactSecret(s, token)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 200 {
		last = last[:200] + "…"
	}
	if last == "" {
		last = "échec de git clone"
	}
	return last
}

// CloneProject : `git clone -- url dir` (dir existant et vide) avec progression ; ctx l'annule. L'erreur est un
// message court sans secret ; context.Canceled est rendu tel quel pour une annulation.
func CloneProject(ctx context.Context, rawURL, dir string, auth CloneAuth, onProgress func(step string, pct int)) error {
	info, err := ValidateCloneURL(rawURL)
	if err != nil {
		return err
	}
	u := strings.TrimSpace(rawURL)
	tmp, err := os.MkdirTemp("", "cl-clone-")
	if err != nil {
		return fmt.Errorf("dossier temporaire impossible : %w", err)
	}
	defer os.RemoveAll(tmp)
	emptyCfg := filepath.Join(tmp, "gitconfig")
	if err := os.WriteFile(emptyCfg, nil, 0o600); err != nil {
		return fmt.Errorf("configuration git neutre impossible : %w", err)
	}
	askpass := ""
	if info.Scheme == "https" && auth.Token != "" && info.Host == "github.com" {
		askpass = filepath.Join(tmp, "askpass.sh")
		if err := os.WriteFile(askpass, []byte(askpassScript), 0o700); err != nil {
			return fmt.Errorf("jeton : script impossible : %w", err)
		}
	}
	cmd := platform.NoWindow(exec.CommandContext(ctx, "git",
		"-c", "credential.helper=", "-c", "core.askPass=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never",
		"-c", "http.followRedirects=false",
		"clone", "--progress", "--", u, dir))
	cmd.Env = cloneEnv(info, dir, auth, askpass, emptyCfg)
	platform.SetProcessGroup(cmd)
	cmd.Cancel = func() error { return platform.KillTree(cmd) } // git ET ses enfants (remote-https, ssh) : sinon stderr reste ouvert
	cmd.WaitDelay = 3 * time.Second
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git : %w", err)
	}
	var tail bytes.Buffer
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sc.Split(splitCRLF)
	for sc.Scan() {
		line := sc.Text()
		if step, pct, ok := cloneProgress(line); ok {
			if onProgress != nil {
				onProgress(step, pct)
			}
			continue
		}
		if tail.Len() < 8192 {
			tail.WriteString(line + "\n")
		}
	}
	_, _ = io.Copy(io.Discard, stderr)
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if werr != nil {
		return errors.New(cloneFailure(tail.String(), info, auth.Token))
	}
	return nil
}

// splitCRLF : lignes terminées par \n ou \r (git rafraîchit la progression avec \r).
func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ── Proposition de /init-project (source « template », #215) ──────────────

// InitProposedPath : marqueur d'une proposition /init-project en attente dans le projet.
func InitProposedPath(dir string) string {
	return filepath.Join(dir, ".claude-launcher", "init-proposed")
}

// initProposedExclude : ligne de .git/info/exclude qui garde le marqueur hors des commits.
const initProposedExclude = "/.claude-launcher/init-proposed"

// MarkInitProposed : pose le marqueur (projet créé depuis le template), exclu de git (.git/info/exclude : jamais commité).
func MarkInitProposed(dir string) error {
	p := InitProposedPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
		ex := filepath.Join(dir, ".git", "info", "exclude")
		if b, _ := os.ReadFile(ex); !strings.Contains(string(b), initProposedExclude) {
			_ = os.MkdirAll(filepath.Dir(ex), 0o755)
			if f, err := os.OpenFile(ex, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
					_, _ = f.WriteString("\n")
				}
				_, _ = f.WriteString(initProposedExclude + "\n")
				f.Close()
			}
		}
	}
	return os.WriteFile(p, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
}

// InitProposalPending : le projet attend sa proposition /init-project.
func InitProposalPending(dir string) bool {
	_, err := os.Stat(InitProposedPath(dir))
	return err == nil
}

// ClearInitProposal : retire le marqueur (/init-project lancé).
func ClearInitProposal(dir string) { _ = os.Remove(InitProposedPath(dir)) }
