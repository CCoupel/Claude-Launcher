package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"claude-launcher-ng2/platform"
)

// ── Coffre des secrets du launcher (#194, contracts/backends-remote.md) ───
//
// Dossier CL_SECRETS_DIR, 0700, un sous-dossier par backend (<dossier>/<backend>/), fichiers en 0600 : « id »
// (clé privée ssh, jamais relue), « id.pub », « known_hosts ». Refusé sous la racine partagée, sous un volume ou une
// racine partagée d'un backend. Les contenus ne sont jamais journalisés ni rendus (sauf la clé publique).

// SecretsDirVar : dossier du coffre (fourni par le chart dans un pod).
const SecretsDirVar = "CL_SECRETS_DIR"

// Noms des fichiers d'un backend.
const (
	secretKeyFile   = "id"
	secretPubFile   = "id.pub"
	secretKnownFile = "known_hosts"
	secretJournal   = "journal.jsonl"
)

var backendNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// secretsDirRaw : emplacement du coffre, sans contrôle ni création.
func secretsDirRaw() string {
	if d := strings.TrimSpace(os.Getenv(SecretsDirVar)); d != "" {
		return filepath.Clean(d)
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "claude-launcher", "secrets")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-launcher", "secrets")
}

// pathWithin : p est r ou dans r (chemins nettoyés, liens symboliques résolus quand ils existent).
func pathWithin(p, r string) bool {
	if r == "" {
		return false
	}
	real := func(s string) string {
		s = filepath.Clean(s)
		if e, err := filepath.EvalSymlinks(s); err == nil {
			return e
		}
		return s
	}
	rel, err := filepath.Rel(real(r), real(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// secretsForbiddenRoots : racines partagées sous lesquelles le coffre est refusé.
func secretsForbiddenRoots() []string {
	var roots []string
	if d, _ := RootBase(); d != "" {
		roots = append(roots, d)
	}
	for r := range ParseK8sVolumes(os.Getenv(K8sVolumesVar)) {
		roots = append(roots, r)
	}
	if bs, err := LoadBackends(); err == nil {
		for _, b := range bs {
			for r := range b.Volumes {
				roots = append(roots, r)
			}
			roots = append(roots, b.Shared...)
		}
	}
	return roots
}

// checkSecretsDir : le dossier peut-il servir de coffre ? Refus (rien n'est écrit) sous une racine partagée, ou s'il
// existe avec des droits plus larges que 0700 (Unix).
func checkSecretsDir(dir string, roots []string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("coffre des secrets : un chemin absolu est attendu (%s=%q)", SecretsDirVar, dir)
	}
	for _, r := range roots {
		if pathWithin(dir, r) {
			return fmt.Errorf("coffre des secrets refusé : %s est sous la zone partagée %s ; choisir un dossier non partagé (%s)", dir, r, SecretsDirVar)
		}
	}
	if st, err := os.Stat(dir); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("coffre des secrets : %s n'est pas un dossier", dir)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("coffre des secrets refusé : %s a des droits trop larges (%o, 0700 attendu)", dir, st.Mode().Perm())
		}
	}
	return nil
}

// SecretsDir : le coffre, contrôlé et créé en 0700 s'il manque.
func SecretsDir() (string, error) {
	dir := secretsDirRaw()
	if err := checkSecretsDir(dir, secretsForbiddenRoots()); err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("coffre des secrets : %w", err)
		}
		_ = os.Chmod(dir, 0o700)
		_ = platform.RestrictToUser(dir, true)
	}
	return dir, nil
}

func checkBackendName(name string) error {
	if !backendNameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("nom de backend invalide pour le coffre : %q", name)
	}
	return nil
}

// secretBackendDir : <coffre>/<backend>/, créé en 0700 (write) ou simplement désigné (lecture, sans création).
func secretBackendDir(name string, create bool) (string, error) {
	if err := checkBackendName(name); err != nil {
		return "", err
	}
	if !create {
		return filepath.Join(secretsDirRaw(), name), nil
	}
	root, err := SecretsDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(root, name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", fmt.Errorf("coffre des secrets : %w", err)
	}
	_ = os.Chmod(d, 0o700)
	_ = platform.RestrictToUser(d, true)
	return d, nil
}

// writeSecretFile : écriture atomique (fichier temporaire du même dossier, 0600, puis renommage).
func writeSecretFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".secret-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error { f.Close(); _ = os.Remove(tmp); return err }
	if err := os.Chmod(tmp, 0o600); err != nil && runtime.GOOS != "windows" {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = platform.RestrictToUser(tmp, false)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ── Clés ssh ──────────────────────────────────────────────────────────────

// SecretWrite : sources d'une écriture, pour le journal.
type SecretWrite struct{ Source string }

// normKey : fins de ligne Unix et retour final (une clé collée depuis Windows porte des CRLF, que ssh refuse).
func normKey(s string) []byte {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return []byte(strings.TrimSpace(s) + "\n")
}

// checkKeyPair : la clé privée (sans phrase de passe) et la clé publique vont ensemble ; rend l'empreinte.
func checkKeyPair(priv, pub []byte) (fingerprint string, err error) {
	raw, err := ssh.ParseRawPrivateKey(priv)
	if err != nil {
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			return "", errors.New("clé privée protégée par une phrase de passe : refusée (le launcher appelle ssh sans saisie) — fournir une clé sans phrase de passe")
		}
		return "", errors.New("clé privée illisible (format OpenSSH ou PEM attendu)")
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return "", errors.New("clé privée non gérée")
	}
	pk, _, _, _, err := ssh.ParseAuthorizedKey(pub)
	if err != nil {
		return "", errors.New("clé publique illisible (ligne « ssh-ed25519 AAAA… » attendue)")
	}
	if !bytes.Equal(pk.Marshal(), signer.PublicKey().Marshal()) {
		return "", errors.New("la clé publique ne correspond pas à la clé privée")
	}
	return ssh.FingerprintSHA256(pk), nil
}

// generateKeyPair : ed25519 ; clé privée au format OpenSSH, clé publique en une ligne.
func generateKeyPair(comment string) (priv, pub []byte, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	blk, err := ssh.MarshalPrivateKey(sk, comment)
	if err != nil {
		return nil, nil, err
	}
	spk, err := ssh.NewPublicKey(pk)
	if err != nil {
		return nil, nil, err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(spk)))
	if comment != "" {
		line += " " + comment
	}
	return pem.EncodeToMemory(blk), []byte(line + "\n"), nil
}

// SecretKeyInfo : ce que montre l'interface d'un backend (jamais la clé privée).
type SecretKeyInfo struct {
	HasKey             bool             `json:"hasKey"`
	PublicKey          string           `json:"publicKey,omitempty"`
	Fingerprint        string           `json:"fingerprint,omitempty"`
	KnownHosts         []KnownHostEntry `json:"knownHosts"`
	KeyModified        string           `json:"keyModified,omitempty"`        // « modifié le … » (RFC 3339)
	KnownHostsModified string           `json:"knownHostsModified,omitempty"` // idem
	Dir                string           `json:"dir"`
}

// KnownHostEntry : une empreinte d'hôte.
type KnownHostEntry struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Line        string `json:"line,omitempty"`
}

func modTime(p string) string {
	if st, err := os.Stat(p); err == nil {
		return st.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}

// parseKnownHosts : lignes known_hosts → empreintes ; refuse les lignes illisibles et les marqueurs (@cert-authority,
// @revoked) qui élargiraient la confiance.
func parseKnownHosts(text string) ([]KnownHostEntry, error) {
	var out []KnownHostEntry
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		marker, _, pk, _, _, err := ssh.ParseKnownHosts([]byte(l))
		if err != nil {
			return nil, fmt.Errorf("ligne known_hosts illisible : %.60q", l)
		}
		if marker != "" {
			return nil, fmt.Errorf("marqueur %q refusé dans known_hosts", marker)
		}
		out = append(out, KnownHostEntry{Type: pk.Type(), Fingerprint: ssh.FingerprintSHA256(pk), Line: l})
	}
	return out, nil
}

// SecretGet : état des secrets ssh d'un backend.
func SecretGet(backend string) (SecretKeyInfo, error) {
	d, err := secretBackendDir(backend, false)
	if err != nil {
		return SecretKeyInfo{}, err
	}
	info := SecretKeyInfo{KnownHosts: []KnownHostEntry{}, Dir: d}
	if _, err := os.Stat(filepath.Join(d, secretKeyFile)); err == nil {
		info.HasKey = true
		info.KeyModified = modTime(filepath.Join(d, secretKeyFile))
		if pub, err := os.ReadFile(filepath.Join(d, secretPubFile)); err == nil {
			info.PublicKey = strings.TrimSpace(string(pub))
			if pk, _, _, _, err := ssh.ParseAuthorizedKey(pub); err == nil {
				info.Fingerprint = ssh.FingerprintSHA256(pk)
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(d, secretKnownFile)); err == nil {
		if es, err := parseKnownHosts(string(b)); err == nil {
			for _, e := range es {
				e.Line = ""
				info.KnownHosts = append(info.KnownHosts, e)
			}
		}
		info.KnownHostsModified = modTime(filepath.Join(d, secretKnownFile))
	}
	return info, nil
}

// SecretSetKey : enregistre la paire (ou en génère une). Rend l'empreinte et la clé publique ; la clé privée n'est
// jamais rendue.
func SecretSetKey(backend, privateKey, publicKey string, generate bool, w SecretWrite) (fingerprint, pub string, err error) {
	var privB, pubB []byte
	if generate {
		if privB, pubB, err = generateKeyPair("claude-launcher-" + backend); err != nil {
			return "", "", err
		}
	} else {
		privB, pubB = normKey(privateKey), normKey(publicKey)
	}
	if fingerprint, err = checkKeyPair(privB, pubB); err != nil {
		return "", "", err
	}
	d, err := secretBackendDir(backend, true)
	if err != nil {
		return "", "", err
	}
	// la clé publique d'abord : un échec n'écrase jamais une clé privée par une moitié de paire
	if err := writeSecretFile(filepath.Join(d, secretPubFile), pubB); err != nil {
		return "", "", fmt.Errorf("écriture de la clé publique : %w", err)
	}
	if err := writeSecretFile(filepath.Join(d, secretKeyFile), privB); err != nil {
		return "", "", fmt.Errorf("écriture de la clé privée : %w", err)
	}
	what := "set"
	if generate {
		what = "generate"
	}
	journalSecret(backend, "ssh-key", what, fingerprint, w.Source)
	return fingerprint, strings.TrimSpace(string(pubB)), nil
}

// SecretSetKnownHosts : enregistre les lignes known_hosts (après la confirmation des empreintes par l'utilisateur).
func SecretSetKnownHosts(backend, lines string, w SecretWrite) (int, error) {
	es, err := parseKnownHosts(lines)
	if err != nil {
		return 0, err
	}
	if len(es) == 0 {
		return 0, errors.New("aucune ligne known_hosts")
	}
	d, err := secretBackendDir(backend, true)
	if err != nil {
		return 0, err
	}
	var sb strings.Builder
	fps := make([]string, 0, len(es))
	for _, e := range es {
		sb.WriteString(e.Line + "\n")
		fps = append(fps, e.Fingerprint)
	}
	if err := writeSecretFile(filepath.Join(d, secretKnownFile), []byte(sb.String())); err != nil {
		return 0, err
	}
	journalSecret(backend, "known_hosts", "set", strings.Join(fps, ","), w.Source)
	return len(es), nil
}

// SecretDelete : « key » (clé privée et publique), « known_hosts » ou « all ».
func SecretDelete(backend, what string, w SecretWrite) error {
	d, err := secretBackendDir(backend, false)
	if err != nil {
		return err
	}
	var names []string
	switch what {
	case "key":
		names = []string{secretKeyFile, secretPubFile}
	case "known_hosts":
		names = []string{secretKnownFile}
	case "all":
		names = []string{secretKeyFile, secretPubFile, secretKnownFile}
	default:
		return fmt.Errorf("suppression : « %s » inconnu (key, known_hosts, all)", what)
	}
	for _, n := range names {
		if err := os.Remove(filepath.Join(d, n)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if what == "all" {
		_ = os.Remove(d) // le dossier, s'il est vide
	}
	journalSecret(backend, what, "delete", "", w.Source)
	return nil
}

// ── Relevé de l'empreinte d'un hôte ───────────────────────────────────────

// sshHostPort : hôte et port d'une destination ssh (« [user@]hôte », « ssh://[user@]hôte[:port] »).
func sshHostPort(dest string) (host, port string, err error) {
	d := strings.TrimSpace(dest)
	d = strings.TrimPrefix(d, "ssh://")
	if i := strings.LastIndex(d, "@"); i >= 0 {
		d = d[i+1:]
	}
	d = strings.TrimSuffix(d, "/")
	if strings.HasPrefix(d, "[") { // [::1]:22
		if j := strings.Index(d, "]"); j > 0 {
			host, d = d[1:j], d[j+1:]
			port = strings.TrimPrefix(d, ":")
		}
	} else if i := strings.LastIndex(d, ":"); i >= 0 && strings.Count(d, ":") == 1 {
		host, port = d[:i], d[i+1:]
	} else {
		host = d
	}
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\n'\"`;|&$<>") {
		return "", "", fmt.Errorf("destination ssh invalide : %q", dest)
	}
	if port != "" && !regexp.MustCompile(`^[0-9]{1,5}$`).MatchString(port) {
		return "", "", fmt.Errorf("port ssh invalide : %q", port)
	}
	return host, port, nil
}

// keyscan : lanceur de ssh-keyscan (remplacé dans les tests).
var keyscan = func(host, port string) ([]byte, error) {
	args := []string{"-T", "5"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, "--", host)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return platform.NoWindow(exec.CommandContext(ctx, "ssh-keyscan", args...)).Output()
}

// HostScan : relève les empreintes de l'hôte du backend (ssh-keyscan). Rien n'est enregistré.
func HostScan(dest string) ([]KnownHostEntry, error) {
	host, port, err := sshHostPort(dest)
	if err != nil {
		return nil, err
	}
	out, err := keyscan(host, port)
	if err != nil && len(out) == 0 {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return nil, errors.New("ssh-keyscan absent de cette machine (installer le client OpenSSH)")
		}
		return nil, fmt.Errorf("relevé impossible : %v", err)
	}
	es, perr := parseKnownHosts(string(out))
	if perr != nil || len(es) == 0 {
		return nil, fmt.Errorf("aucune clé d'hôte reçue de %s", host)
	}
	return es, nil
}

// ── Utilisation par ssh ───────────────────────────────────────────────────

// sshSecretArgs : options ssh d'un backend qui a une clé enregistrée (identité et known_hosts du coffre,
// StrictHostKeyChecking=yes). Sans clé : aucune option (agent ssh et ~/.ssh du poste, comportement de la v6.3.0).
func sshSecretArgs(backend string) []string {
	if backend == "" || checkBackendName(backend) != nil {
		return nil
	}
	d := filepath.Join(secretsDirRaw(), backend)
	key := filepath.Join(d, secretKeyFile)
	if _, err := os.Stat(key); err != nil {
		return nil
	}
	return []string{"-i", key, "-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=" + filepath.Join(d, secretKnownFile),
		"-o", "StrictHostKeyChecking=yes"}
}

// sshBaseArgs : options communes des appels ssh vers le backend.
func sshBaseArgs(backend string, connectTimeout string) []string {
	return append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=" + connectTimeout}, sshSecretArgs(backend)...)
}

// ── Journal des écritures ─────────────────────────────────────────────────

// journalSecret : une ligne par écriture (backend, type, action, empreinte, source) — jamais de contenu.
func journalSecret(backend, secret, action, fingerprint, source string) {
	dir := secretsDirRaw()
	b, _ := json.Marshal(map[string]string{"at": time.Now().UTC().Format(time.RFC3339), "backend": backend, "secret": secret,
		"action": action, "fingerprint": fingerprint, "source": source})
	f, err := os.OpenFile(filepath.Join(dir, secretJournal), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
