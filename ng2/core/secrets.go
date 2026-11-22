package core

import (
	"bytes"
	"claude-launcher-ng2/internal/tun"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"claude-launcher-ng2/platform"
)

// ── Coffre des secrets du launcher (#194, contracts/backends-remote.md) ───
//
// Coffre COMMUN au hub, à l'exe Windows et au pod (v6.5.0, #201) : <CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets/, en clair,
// 0700, un sous-dossier par backend (<coffre>/<backend>/), fichiers en 0600 : « id » (clé privée ssh, jamais relue),
// « id.pub », « known_hosts », « kubeconfig ». CL_SECRETS_DIR le remplace ; sans racine de base ni CL_SECRETS_DIR,
// emplacement local de la v6.4.0. Refusé sous un dossier de projet ou si son chemin résolu (liens symboliques, liens
// pendants compris) sort de l'emplacement attendu. Les secrets de projet sont dans <projet>/PRIVATE/ : le launcher n'y
// écrit jamais. Les contenus ne sont jamais journalisés ni rendus (sauf la clé publique).

// SecretsDirVar : dossier du coffre (fourni par le chart dans un pod).
const SecretsDirVar = "CL_SECRETS_DIR"

// Noms des fichiers d'un backend.
const (
	secretKeyFile   = "id"
	secretPubFile   = "id.pub"
	secretKnownFile = "known_hosts"
	secretJournal   = "secrets-journal.jsonl" // dans CacheDir(), hors du coffre
)

// restrictACL : droits réservés à l'utilisateur (ACL sous Windows) ; remplacé dans les tests. Un échec refuse
// l'écriture dans le coffre.
var restrictACL = tun.Of(platform.RestrictToUser)

var backendNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// SecretsLegacyDirVar : ancien coffre à migrer (posé par le chart sur l'ancien PVC) ; en plus de l'emplacement local v6.4.0.
const SecretsLegacyDirVar = "CL_SECRETS_LEGACY_DIR"

// secretsDefaultDir : emplacement sans surcharge : le coffre commun sous la racine de base, sinon l'emplacement local.
func secretsDefaultDir() string {
	if d := RootBaseConfigDir(); d != "" {
		return filepath.Join(d, "secrets")
	}
	return secretsLocalDir()
}

// secretsDirRaw : emplacement du coffre, sans contrôle ni création.
func secretsDirRaw() string {
	if d := strings.TrimSpace(os.Getenv(SecretsDirVar)); d != "" {
		return filepath.Clean(d)
	}
	return secretsDefaultDir()
}

// secretsLocalDir : emplacement local de la v6.4.0 (propre à chaque poste) ; source de la migration.
func secretsLocalDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "claude-launcher", "secrets")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-launcher", "secrets")
}

// realPath : chemin nettoyé, liens symboliques (liens pendants compris) et, sous Windows, noms courts 8.3
// (C:\Users\RUNNER~1) résolus pour sa partie existante ; la fin qui n'existe pas encore est reposée telle quelle. Sans
// cela, comparer un chemin à créer à une racine existante mêlerait deux formes du même dossier et le refus ne se
// déclencherait pas. Une boucle de liens rend le chemin nettoyé (realPathChecked la signale).
func realPath(s string) string {
	r, err := realPathChecked(s)
	if err != nil {
		return filepath.Clean(s)
	}
	return r
}

// maxSymlinkDepth : liens suivis au plus (au-delà : boucle, refus).
const maxSymlinkDepth = 32

// realPathChecked : comme realPath, avec une erreur sur une boucle de liens. Un lien pendant (cible absente) est suivi :
// Lstat + Readlink, cible relative jointe au dossier du lien, chemin résolu = la cible à créer (jamais le lien lui-même).
func realPathChecked(s string) (string, error) {
	return realPathDepth(filepath.Clean(s), 0)
}

func realPathDepth(s string, depth int) (string, error) {
	if depth > maxSymlinkDepth {
		return "", fmt.Errorf("trop de liens symboliques (boucle ?) : %s", s)
	}
	rest := ""
	for cur := s; ; {
		if e, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(e, rest), nil
		}
		if st, err := os.Lstat(cur); err == nil && st.Mode()&os.ModeSymlink != 0 {
			// lien dont la résolution échoue : pendant (cible absente) ou boucle
			tgt, err := os.Readlink(cur)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(tgt) {
				tgt = filepath.Join(filepath.Dir(cur), tgt)
			}
			return realPathDepth(filepath.Join(filepath.Clean(tgt), rest), depth+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return s, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// pathWithin : p est r ou dans r (formes réelles des deux chemins ; filepath.Rel ignore la casse sous Windows).
func pathWithin(p, r string) bool {
	if r == "" {
		return false
	}
	rel, err := filepath.Rel(realPath(r), realPath(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sharedZones : racines partagées (racine de base, volumes k8s, volumes et dossiers partagés des backends). Le coffre
// n'en est plus refusé (v6.5.0) ; un kubeconfig désigné par chemin n'y est admis que dans le coffre ou un PRIVATE/.
func sharedZones() []string {
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

// secretsProjectRoots : racines des projets du launcher (un coffre ne peut pas être sous un dossier de projet).
func secretsProjectRoots() []string { return LoadConfig().Roots }

// samePath : égalité de deux chemins déjà résolus (insensible à la casse sous Windows).
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// checkSecretsDir : le dossier peut-il servir de coffre ? Refus (rien n'est écrit) : chemin non absolu ; sous un dossier de
// projet (projectRoots) ; chemin résolu (liens symboliques, liens pendants compris) autre que l'emplacement attendu
// (parents résolus + nom du dossier) ; dossier existant avec des droits plus larges que 0700 (Unix, y compris un
// partage vu depuis Linux). L'emplacement par défaut n'est jamais refusé comme « sous un projet ».
func checkSecretsDir(dir string, projectRoots []string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("coffre des secrets : un chemin absolu est attendu (%s=%q)", SecretsDirVar, dir)
	}
	dir = filepath.Clean(dir)
	if def := secretsDefaultDir(); !samePath(realPath(dir), realPath(def)) {
		for _, r := range projectRoots {
			if pathWithin(dir, r) {
				return fmt.Errorf("coffre des secrets refusé : %s est sous un dossier de projet (%s) ; les secrets d'un projet vont dans <projet>/PRIVATE/", dir, r)
			}
		}
	}
	res, err := realPathChecked(dir)
	if err != nil {
		return fmt.Errorf("coffre des secrets refusé : %w", err)
	}
	// emplacement attendu : sous la racine de base (résolue), le chemin exact <racine>/.config/claude-launcher-ng2/secrets
	// (un lien posé sur .config ou claude-launcher-ng2 le ferait sortir) ; sinon les parents résolus + le nom du dossier
	var want string
	if rb, _ := RootBase(); rb != "" && samePath(dir, filepath.Join(filepath.Clean(rb), ".config", "claude-launcher-ng2", "secrets")) {
		rr, err := realPathChecked(rb)
		if err != nil {
			return fmt.Errorf("coffre des secrets refusé : %w", err)
		}
		want = filepath.Join(rr, ".config", "claude-launcher-ng2", "secrets")
	} else {
		parent, err := realPathChecked(filepath.Dir(dir))
		if err != nil {
			return fmt.Errorf("coffre des secrets refusé : %w", err)
		}
		want = filepath.Join(parent, filepath.Base(dir))
	}
	if !samePath(res, want) {
		return fmt.Errorf("coffre des secrets refusé : %s se résout en %s (lien symbolique), %s attendu", dir, res, want)
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

// restrictSecret : droits réservés à l'utilisateur (ACL sous Windows). Sur un chemin UNC (\\wsl.localhost\…, 9P) il n'y a pas
// d'ACL Windows : les droits sont ceux, POSIX, de l'uid WSL (0700/0600 posés par Chmod) ; on n'applique rien au lieu de refuser.
func restrictSecret(path string, dir bool) error {
	if runtime.GOOS == "windows" && platform.IsUNCPath(path) {
		return nil
	}
	return restrictACL.Get()(path, dir)
}

// SecretsDir : le coffre, contrôlé et créé en 0700 s'il manque.
func SecretsDir() (string, error) {
	dir := secretsDirRaw()
	if err := checkSecretsDir(dir, secretsProjectRoots()); err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("coffre des secrets : %w", err)
		}
		_ = os.Chmod(dir, 0o700)
		if err := restrictSecret(dir, true); err != nil {
			return "", fmt.Errorf("coffre des secrets : droits du dossier impossibles à restreindre : %w", err)
		}
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
	if err := checkSecretsSubdir(root, d); err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", fmt.Errorf("coffre des secrets : %w", err)
	}
	_ = os.Chmod(d, 0o700)
	if err := restrictSecret(d, true); err != nil {
		return "", fmt.Errorf("coffre des secrets : droits du dossier impossibles à restreindre : %w", err)
	}
	return d, nil
}

// checkSecretsSubdir : le dossier d'un backend ne sort pas du coffre par un lien symbolique (ni pendant).
func checkSecretsSubdir(root, d string) error {
	res, err := realPathChecked(d)
	if err != nil {
		return fmt.Errorf("coffre des secrets refusé : %w", err)
	}
	rr, err := realPathChecked(root)
	if err != nil {
		return fmt.Errorf("coffre des secrets refusé : %w", err)
	}
	if want := filepath.Join(rr, filepath.Base(d)); !samePath(res, want) {
		return fmt.Errorf("coffre des secrets refusé : %s se résout en %s (lien symbolique), %s attendu", d, res, want)
	}
	return nil
}

// vaultLockName : verrou du coffre (<coffre>/.vault.lock) pris par toutes les instances qui l'écrivent.
const vaultLockName = ".vault"

// inVault : fn sous le verrou fichier du coffre (plusieurs instances : hub, exe, pod) ; d = dossier du backend, créé.
// La dernière écriture l'emporte ; chaque écriture est atomique et journalisée.
func inVault(backend string, fn func(d string) error) error {
	d, err := secretBackendDir(backend, true)
	if err != nil {
		return err
	}
	return WithFileLock(filepath.Join(filepath.Dir(d), vaultLockName), func() error { return fn(d) })
}

// withVaultLockAt : fn sous le verrou du coffre root ; sans coffre (rien à supprimer), fn seul.
func withVaultLockAt(root string, fn func() error) error {
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fn()
	}
	return WithFileLock(filepath.Join(root, vaultLockName), fn)
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
	if err := restrictSecret(tmp, false); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("droits du fichier impossibles à restreindre : %w", err)
	}
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
	// kubeconfig (#193) : présence, contextes listés (jamais le contenu), « modifié le … »
	HasKubeconfig      bool     `json:"hasKubeconfig"`
	KubeContexts       []string `json:"kubeContexts,omitempty"`
	KubeconfigModified string   `json:"kubeconfigModified,omitempty"`
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
	if b, err := os.ReadFile(filepath.Join(d, secretKubeFile)); err == nil {
		info.HasKubeconfig = true
		info.KubeconfigModified = modTime(filepath.Join(d, secretKubeFile))
		if ctxs, _, err := parseKubeconfig(b); err == nil {
			info.KubeContexts = ctxs
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
	what := "set"
	if generate {
		what = "generate"
	}
	err = inVault(backend, func(d string) error {
		if err := journalSecret(backend, "ssh-key", what, fingerprint, w.Source); err != nil {
			return err
		}
		// la clé publique d'abord : un échec n'écrase jamais une clé privée par une moitié de paire
		if err := writeSecretFile(filepath.Join(d, secretPubFile), pubB); err != nil {
			return fmt.Errorf("écriture de la clé publique : %w", err)
		}
		if err := writeSecretFile(filepath.Join(d, secretKeyFile), privB); err != nil {
			return fmt.Errorf("écriture de la clé privée : %w", err)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
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
	var sb strings.Builder
	fps := make([]string, 0, len(es))
	for _, e := range es {
		sb.WriteString(e.Line + "\n")
		fps = append(fps, e.Fingerprint)
	}
	err = inVault(backend, func(d string) error {
		if err := journalSecret(backend, "known_hosts", "set", strings.Join(fps, ","), w.Source); err != nil {
			return err
		}
		return writeSecretFile(filepath.Join(d, secretKnownFile), []byte(sb.String()))
	})
	if err != nil {
		return 0, err
	}
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
	case "kubeconfig":
		names = []string{secretKubeFile}
	case "all":
		names = []string{secretKeyFile, secretPubFile, secretKnownFile, secretKubeFile}
	default:
		return fmt.Errorf("suppression : « %s » inconnu (key, known_hosts, kubeconfig, all)", what)
	}
	return withVaultLockAt(filepath.Dir(d), func() error {
		if err := journalSecret(backend, what, "delete", "", w.Source); err != nil {
			return err
		}
		for _, n := range names {
			if err := os.Remove(filepath.Join(d, n)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if what == "all" {
			_ = os.Remove(d) // le dossier, s'il est vide
		}
		return nil
	})
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
var keyscan = tun.Of(func(host, port string) ([]byte, error) {
	args := []string{"-T", "5"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, "--", host)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return platform.NoWindow(exec.CommandContext(ctx, "ssh-keyscan", args...)).Output()
})

// HostScan : relève les empreintes de l'hôte du backend (ssh-keyscan). Rien n'est enregistré.
func HostScan(dest string) ([]KnownHostEntry, error) {
	host, port, err := sshHostPort(dest)
	if err != nil {
		return nil, err
	}
	ip, err := resolveScanTarget(host)
	if err != nil {
		return nil, err
	}
	out, err := keyscan.Get()(ip.String(), port) // l'adresse vérifiée, jamais le nom (pas de seconde résolution)
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
	// les lignes portent l'hôte demandé (celui que ssh cherchera dans known_hosts), pas l'adresse sondée
	token := host
	if port != "" && port != "22" {
		token = "[" + host + "]:" + port
	}
	for i := range es {
		if _, _, pk, _, _, err := ssh.ParseKnownHosts([]byte(es[i].Line)); err == nil {
			es[i].Line = token + " " + pk.Type() + " " + base64.StdEncoding.EncodeToString(pk.Marshal())
		}
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
	if secretsDirUnsafe() {
		return nil // coffre déplacé sous une zone partagée ou aux droits trop larges : jamais utilisé
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

// journalSecret : une ligne par écriture, dans <CacheDir>/secrets-journal.jsonl (hors du coffre) ; écrite AVANT le secret :
// un journal inaccessible refuse l'écriture ; (backend, type, action, empreinte, source) — jamais de contenu.
func journalSecret(backend, secret, action, fingerprint, source string) error {
	rec := map[string]string{"at": time.Now().UTC().Format(time.RFC3339), "backend": backend, "secret": secret,
		"action": action, "source": source, "instance": secretsInstance(), "launcher": LauncherID()}
	if fingerprint != "" { // une révélation ne porte NI valeur NI longueur NI empreinte : la clé n'est pas écrite du tout
		rec["fingerprint"] = fingerprint
	}
	b, _ := json.Marshal(rec)
	f, err := os.OpenFile(filepath.Join(CacheDir(), secretJournal), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("journal des écritures inaccessible (%s) : écriture refusée", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("journal des écritures inaccessible (%s) : écriture refusée", err)
	}
	return nil
}

// secretsInstance : « windows » (exe), « pod » (en cluster) ou « hub » (coque Linux), pour le journal.
func secretsInstance() string {
	switch {
	case runtime.GOOS == "windows":
		return "windows"
	case os.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return "pod"
	}
	return "hub"
}

// secretsDirUnsafe : le coffre est-il refusé à l'usage ? Contrôle (racines partagées : lecture de backends.yaml)
// mémorisé 30 s par emplacement : ssh est appelé très souvent.
func secretsDirUnsafe() bool {
	dir := secretsDirRaw()
	secretsCheck.Lock()
	defer secretsCheck.Unlock()
	if e, ok := secretsCheck.m[dir]; ok && time.Since(e.at) < 30*time.Second {
		return e.bad
	}
	bad := checkSecretsDir(dir, secretsProjectRoots()) != nil
	secretsCheck.m[dir] = secretsCheckEntry{at: time.Now(), bad: bad}
	return bad
}

type secretsCheckEntry struct {
	at  time.Time
	bad bool
}

var secretsCheck = struct {
	sync.Mutex
	m map[string]secretsCheckEntry
}{m: map[string]secretsCheckEntry{}}

// resolveScanTarget : résout l'hôte UNE fois, vérifie chaque adresse et rend celle qu'on sondera (pas de seconde
// résolution : pas de rebinding). Refus : boucle locale, adresse non spécifiée, link-local (169.254.0.0/16 dont les
// métadonnées cloud, fe80::/10), multicast, adresses du pod / de la machine du launcher.
func resolveScanTarget(host string) (net.IP, error) {
	if strings.EqualFold(host, "localhost") {
		return nil, fmt.Errorf("relevé refusé : %s est la machine du launcher", host)
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("relevé impossible : %s ne se résout pas", host)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	local := map[string]bool{}
	if as, err := net.InterfaceAddrs(); err == nil {
		for _, a := range as {
			if ipn, ok := a.(*net.IPNet); ok {
				local[ipn.IP.String()] = true
			}
		}
	}
	for _, ip := range ips {
		switch {
		case ip.IsLoopback() || ip.IsUnspecified() || local[ip.String()]:
			return nil, fmt.Errorf("relevé refusé : %s désigne la machine du launcher (%s)", host, ip)
		case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast():
			return nil, fmt.Errorf("relevé refusé : %s désigne une adresse locale au lien ou de métadonnées (%s)", host, ip)
		}
	}
	return ips[0], nil
}
