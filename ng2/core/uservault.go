package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	if contexts, _, err = parseKubeconfig(raw); err != nil {
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
	return contexts, fingerprint, kubeconfigWarnings(raw), nil
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
