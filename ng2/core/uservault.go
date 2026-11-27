package core

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Coffre d'un utilisateur (contracts/user-backends.md §7, S16, S23, S24) : users/<uid>/vault/<backend>/ UNIQUEMENT. Aucune fonction
// de ce fichier ne lit ni n'écrit le coffre commun de l'admin (secretsDirRaw) ; le <uid> vient de l'espace, jamais du client.

// maxUserKubeconfig : taille d'un kubeconfig collé par un utilisateur (contrat : 64 Kio).
const maxUserKubeconfig = 64 << 10

// userBackendRE : nom d'un backend d'utilisateur — étiquette DNS-1123 (minuscules, chiffres, tirets ; 63 caractères au plus).
var userBackendRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// CheckUserBackendName : le nom est valide pour un backend d'utilisateur (jamais « .. », « / », majuscules, plus de 63 caractères).
func CheckUserBackendName(name string) error {
	if !userBackendRE.MatchString(name) {
		return fmt.Errorf("nom de backend invalide : minuscules, chiffres et tirets, 63 caractères au plus (%q)", shortName(name))
	}
	return nil
}

func shortName(s string) string {
	if r := []rune(s); len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return s
}

// vaultDirOf : dossier du coffre d'un backend de l'utilisateur ; create : créé en 0700. Un lien symbolique au nom du dossier est refusé.
func (s Space) vaultDirOf(backend string, create bool) (string, error) {
	if s.Admin {
		return "", errors.New("coffre de l'utilisateur : espace d'un utilisateur seulement")
	}
	if err := CheckUserBackendName(backend); err != nil {
		return "", err
	}
	d := filepath.Join(s.VaultDir(), backend)
	if create {
		if err := s.Ensure(); err != nil {
			return "", err
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", fmt.Errorf("coffre : %w", err)
		}
		_ = os.Chmod(d, 0o700)
	}
	if st, err := os.Lstat(d); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("coffre refusé : lien symbolique")
	}
	return d, nil
}

// SecretSetKubeconfig : dépose le kubeconfig du backend de l'utilisateur dans SON coffre. Rend les contextes (jamais le contenu),
// l'empreinte et les avertissements.
func (s Space) SecretSetKubeconfig(backend, content string, w SecretWrite) (contexts []string, fingerprint string, warnings []string, err error) {
	raw := normKey(content)
	if len(raw) > maxUserKubeconfig {
		return nil, "", nil, errors.New("kubeconfig trop volumineux (64 Kio au plus)")
	}
	contexts, _, warnings, err = parseUserKubeconfig(raw)
	if err != nil {
		return nil, "", nil, err
	}
	d, err := s.vaultDirOf(backend, true)
	if err != nil {
		return nil, "", nil, err
	}
	sum := sha256.Sum256(raw)
	fingerprint = "sha256:" + hex.EncodeToString(sum[:])[:16]
	if err := journalSecret(backend, "kubeconfig", "set", fingerprint, w.Source, w.User); err != nil {
		return nil, "", nil, err
	}
	err = WithFileLock(filepath.Join(s.VaultDir(), vaultLockName), func() error {
		return writeSecretFile(filepath.Join(d, secretKubeFile), raw)
	})
	if err != nil {
		return nil, "", nil, errors.New("écriture du kubeconfig impossible")
	}
	return contexts, fingerprint, warnings, nil
}

// SecretInfo : présence du kubeconfig de ce backend de l'utilisateur et ses contextes (jamais le contenu ni un chemin).
func (s Space) SecretInfo(backend string) (SecretKeyInfo, error) {
	d, err := s.vaultDirOf(backend, false)
	if err != nil {
		return SecretKeyInfo{}, err
	}
	info := SecretKeyInfo{KnownHosts: []KnownHostEntry{}}
	f := filepath.Join(d, secretKubeFile)
	if b, err := os.ReadFile(f); err == nil {
		info.HasKubeconfig = true
		info.KubeconfigModified = modTime(f)
		if ctxs, _, err := parseKubeconfig(b); err == nil {
			info.KubeContexts = ctxs
		}
	}
	if t := s.tlsInfos(d); len(t) > 0 {
		info.TLS = t
	}
	if regularFile(filepath.Join(d, secretKeyPEM)) {
		info.HasTLSKey = true
	}
	return info, nil
}

// SecretDeleteKubeconfig : retire le kubeconfig du backend de l'utilisateur.
func (s Space) SecretDeleteKubeconfig(backend string, w SecretWrite) error {
	d, err := s.vaultDirOf(backend, false)
	if err != nil {
		return err
	}
	if err := journalSecret(backend, "kubeconfig", "delete", "", w.Source, w.User); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(d, secretKubeFile)); err != nil && !os.IsNotExist(err) {
		return errors.New("suppression impossible")
	}
	_ = os.Remove(d)
	return nil
}

// ── certificats TLS d'un backend docker / podman (S12, S32) ────────────────

const (
	secretCAFile   = "ca.pem"
	secretCertFile = "cert.pem"
	secretKeyPEM   = "key.pem"
)

var errPEM = errors.New("PEM invalide")

// onePEM : exactement un bloc PEM, de l'un des types admis.
func onePEM(s string, types ...string) (*pem.Block, error) {
	s = strings.TrimSpace(s)
	if len(s) > maxUserKubeconfig {
		return nil, errors.New("trop volumineux (64 Kio au plus)")
	}
	blk, rest := pem.Decode([]byte(s))
	if blk == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errPEM
	}
	for _, t := range types {
		if blk.Type == t {
			return blk, nil
		}
	}
	return nil, errPEM
}

// TLSCertInfo : ce que l'interface montre d'un certificat enregistré — jamais le PEM.
type TLSCertInfo struct {
	Fingerprint string `json:"fingerprint"`
	Subject     string `json:"subject"`
	NotAfter    string `json:"notAfter"`
	CA          bool   `json:"ca,omitempty"`
}

func certInfo(c *x509.Certificate) TLSCertInfo {
	sum := sha256.Sum256(c.Raw)
	return TLSCertInfo{Fingerprint: "sha256:" + hex.EncodeToString(sum[:])[:16], Subject: c.Subject.String(),
		NotAfter: c.NotAfter.UTC().Format(time.RFC3339), CA: c.IsCA}
}

// AddError : refus de validation d'une création ou d'un dépôt — {field, code, msg} côté API. Reason : raison interne d'un refus d'hôte
// (form | range | dns), pour l'audit seulement.
type AddError struct{ Field, Code, Msg, Reason string }

func (e *AddError) Error() string { return e.Msg }

const errEncryptedKey = "clé privée chiffrée : colle la clé déchiffrée"

// parseTLSMaterial : contrôle CA du serveur (facultative), certificat et clé du client (ensemble) sans rien écrire. Un bloc PEM, clé privée
// NON chiffrée, clé correspondant au certificat, CA = CA:TRUE ou auto-signé, 64 Kio au plus. Rend les fichiers à écrire et leur état.
func parseTLSMaterial(ca, cert, key string) (files map[string][]byte, infos map[string]TLSCertInfo, aerr *AddError) {
	big := func(field string) *AddError {
		return &AddError{Field: field, Code: "too_large", Msg: "trop volumineux (64 Kio au plus)"}
	}
	bad := func(field, msg string) *AddError { return &AddError{Field: field, Code: "invalid", Msg: msg} }
	for f, v := range map[string]string{"tls.ca": ca, "tls.cert": cert, "tls.key": key} {
		if len(v) > maxUserKubeconfig {
			return nil, nil, big(f)
		}
	}
	if (cert == "") != (key == "") {
		if cert == "" {
			return nil, nil, &AddError{Field: "tls.cert", Code: "required", Msg: "certificat du client obligatoire avec sa clé"}
		}
		return nil, nil, &AddError{Field: "tls.key", Code: "required", Msg: "clé du client obligatoire avec son certificat"}
	}
	if ca == "" && cert == "" {
		return nil, nil, &AddError{Field: "tls.ca", Code: "required", Msg: "rien à enregistrer : CA du serveur, ou certificat et clé du client"}
	}
	infos, files = map[string]TLSCertInfo{}, map[string][]byte{}
	if ca != "" {
		blk, err := onePEM(ca, "CERTIFICATE")
		if err != nil {
			return nil, nil, bad("tls.ca", "CA : un seul bloc CERTIFICATE PEM attendu")
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, nil, bad("tls.ca", "CA : certificat illisible")
		}
		if !c.IsCA && c.CheckSignatureFrom(c) != nil {
			return nil, nil, bad("tls.ca", "CA : ni CA:TRUE ni auto-signé")
		}
		infos["ca"], files[secretCAFile] = certInfo(c), pem.EncodeToMemory(blk)
	}
	if cert != "" {
		cb, err := onePEM(cert, "CERTIFICATE")
		if err != nil {
			return nil, nil, bad("tls.cert", "certificat : un seul bloc CERTIFICATE PEM attendu")
		}
		c, err := x509.ParseCertificate(cb.Bytes)
		if err != nil {
			return nil, nil, bad("tls.cert", "certificat : illisible")
		}
		kb, err := onePEM(key, "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY")
		if err != nil {
			if blk, _ := pem.Decode([]byte(strings.TrimSpace(key))); blk != nil && strings.Contains(blk.Type, "ENCRYPTED") {
				return nil, nil, bad("tls.key", errEncryptedKey)
			}
			return nil, nil, bad("tls.key", "clé : une clé privée en un seul bloc PEM attendue")
		}
		if _, hdr := kb.Headers["DEK-Info"]; hdr {
			return nil, nil, bad("tls.key", errEncryptedKey)
		}
		if _, err := tls.X509KeyPair(pem.EncodeToMemory(cb), pem.EncodeToMemory(kb)); err != nil {
			return nil, nil, bad("tls.key", "la clé ne correspond pas au certificat")
		}
		infos["cert"] = certInfo(c)
		files[secretCertFile], files[secretKeyPEM] = pem.EncodeToMemory(cb), pem.EncodeToMemory(kb)
	}
	return files, infos, nil
}

// SecretSetTLS : dépose les certificats d'un backend docker EXISTANT (parseTLSMaterial) dans SON coffre. Rien n'est écrit si un contrôle échoue.
func (s Space) SecretSetTLS(backend, ca, cert, key string, w SecretWrite) (infos map[string]TLSCertInfo, err error) {
	files, infos, aerr := parseTLSMaterial(ca, cert, key)
	if aerr != nil {
		return nil, errors.New(aerr.Msg)
	}
	d, err := s.vaultDirOf(backend, true)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, n := range names {
		sum.Write(files[n])
	}
	if err := journalSecret(backend, "tls", "set", "sha256:"+hex.EncodeToString(sum.Sum(nil))[:16], w.Source, w.User); err != nil {
		return nil, err
	}
	err = WithFileLock(filepath.Join(s.VaultDir(), vaultLockName), func() error {
		for _, n := range names {
			if err := writeSecretFile(filepath.Join(d, n), files[n]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("écriture des certificats impossible")
	}
	return infos, nil
}

// SecretDeleteTLS : retire ca.pem, cert.pem et key.pem du backend.
func (s Space) SecretDeleteTLS(backend string, w SecretWrite) error {
	d, err := s.vaultDirOf(backend, false)
	if err != nil {
		return err
	}
	if err := journalSecret(backend, "tls", "delete", "", w.Source, w.User); err != nil {
		return err
	}
	for _, n := range []string{secretCAFile, secretCertFile, secretKeyPEM} {
		if err := os.Remove(filepath.Join(d, n)); err != nil && !os.IsNotExist(err) {
			return errors.New("suppression impossible")
		}
	}
	_ = os.Remove(d)
	return nil
}

// tlsInfos : état des certificats d'un backend (empreinte, sujet, expiration ; jamais le contenu).
func (s Space) tlsInfos(d string) map[string]TLSCertInfo {
	out := map[string]TLSCertInfo{}
	for k, f := range map[string]string{"ca": secretCAFile, "cert": secretCertFile} {
		if b, err := os.ReadFile(filepath.Join(d, f)); err == nil {
			if blk, _ := pem.Decode(b); blk != nil {
				if c, err := x509.ParseCertificate(blk.Bytes); err == nil {
					out[k] = certInfo(c)
				}
			}
		}
	}
	return out
}
