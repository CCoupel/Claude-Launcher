package core

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Approbation scellée de l'environnement d'un projet cloné (LOT-3.2 tâche 16, contrat §6 bis, P21) : <projet>/.claude-launcher/trust.json dans le
// volume du projet porte, pour chaque fichier d'environnement approuvé, son condensé SHA-256, la date et un HMAC calculé avec la clé d'approbation
// de l'utilisateur (users/<uid>/vault/approval.key, JAMAIS envoyée au backend). Un trust.json sans HMAC valide (forgé, copié pour un autre contenu,
// clé changée) est ignoré : l'approbation est redemandée.

const approvalKeyFile = "approval.key"

// ApprovalKey : clé d'approbation de l'utilisateur (32 octets aléatoires, 0600), créée au premier besoin ; jamais dans une réponse ni un journal.
func (s Space) ApprovalKey() ([]byte, error) {
	if s.Admin {
		return nil, errors.New("clé d'approbation : espace d'un utilisateur seulement")
	}
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	p := filepath.Join(s.VaultDir(), approvalKeyFile)
	var key []byte
	err := WithFileLock(p, func() error {
		if st, err := os.Lstat(p); err == nil {
			if err := safeVaultFile(p); err != nil {
				return err
			}
			if st.Size() == 32 {
				key, err = os.ReadFile(p)
				return err
			}
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		return writeSecretFile(p, key)
	})
	return key, err
}

// TrustFile : contenu de trust.json.
type TrustFile struct {
	Digests map[string]string `json:"digests"` // fichier d'environnement → SHA-256 (hex)
	Date    string            `json:"date"`
	MAC     string            `json:"mac"` // HMAC-SHA256 (hex)
}

// trustMAC : HMAC-SHA256(clé, uid ‖ liste triée des (fichier, condensé)) à encodage NON AMBIGU (longueurs préfixées).
func trustMAC(key []byte, uid string, digests map[string]string) string {
	h := hmac.New(sha256.New, key)
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(uid)
	names := make([]string, 0, len(digests))
	for n := range digests {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		put(n)
		put(digests[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SealTrust : approbation scellée des condensés donnés.
func SealTrust(key []byte, uid string, digests map[string]string, now time.Time) TrustFile {
	d := make(map[string]string, len(digests))
	for k, v := range digests {
		d[k] = v
	}
	return TrustFile{Digests: d, Date: now.UTC().Format(time.RFC3339), MAC: trustMAC(key, uid, d)}
}

// ParseTrust : trust.json lu des OCTETS (16 Kio, champs connus, une seule valeur, aucune clé en double) ; illisible : erreur.
func ParseTrust(raw []byte) (TrustFile, error) {
	var t TrustFile
	if len(raw) > 16<<10 {
		return t, errors.New("trust.json trop gros")
	}
	if yamlDupKeyCI(raw) { // un JSON est du YAML : même détection des clés en double (casse comprise)
		return t, errors.New("trust.json : clé en double")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil || dec.More() || t.Digests == nil {
		return TrustFile{}, errors.New("trust.json illisible")
	}
	return t, nil
}

// VerifyTrust : l'approbation lue vaut pour les fichiers d'environnement ACTUELS — HMAC valide ET mêmes fichiers, mêmes condensés. Tout écart
// (forgé, copié d'un autre projet pour un autre contenu, clé changée, fichier ajouté ou modifié) : faux, l'approbation est redemandée.
func VerifyTrust(key []byte, uid string, t TrustFile, current map[string]string) bool {
	if len(t.Digests) != len(current) {
		return false
	}
	for n, d := range current {
		if t.Digests[n] != d {
			return false
		}
	}
	want, err := hex.DecodeString(t.MAC)
	got, _ := hex.DecodeString(trustMAC(key, uid, t.Digests))
	return err == nil && hmac.Equal(want, got)
}
