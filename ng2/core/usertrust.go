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

// trust.json : {"files":{nom:sha256 hex},"mac":hex}. MAC = HMAC-SHA256(clé, « cl-trust-v1 » ‖ uid ‖ backend ‖ chemin du projet ‖ pour chaque fichier
// trié : nom ‖ condensé), chaque élément précédé de sa longueur sur 8 octets (encodage non ambigu).
type trustJSON struct {
	Files map[string]string `json:"files"`
	MAC   string            `json:"mac"`
}

func digestsOf(files map[string][]byte) map[string]string {
	d := make(map[string]string, len(files))
	for n, b := range files {
		h := sha256.Sum256(b)
		d[n] = hex.EncodeToString(h[:])
	}
	return d
}

func trustMAC(key []byte, uid, backend, project string, digests map[string]string) string {
	h := hmac.New(sha256.New, key)
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put("cl-trust-v1")
	put(uid)
	put(backend)
	put(project)
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

// BuildTrust : contenu de trust.json pour les fichiers d'environnement approuvés d'un projet.
func BuildTrust(key []byte, uid, backend, project string, files map[string][]byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("clé d'approbation invalide")
	}
	d := digestsOf(files)
	return json.Marshal(trustJSON{Files: d, MAC: trustMAC(key, uid, backend, project, d)})
}

// VerifyTrust : l'approbation lue vaut pour les fichiers ACTUELS — HMAC valide ET mêmes fichiers, mêmes condensés. Tout écart (forgé, copié d'un
// autre projet, clé changée, fichier ajouté, retiré ou modifié, trust.json illisible ou > 16 Kio) : faux, l'approbation est redemandée.
func VerifyTrust(key []byte, uid, backend, project string, files map[string][]byte, trust []byte) bool {
	if len(key) != 32 || len(trust) == 0 || len(trust) > 16<<10 || yamlDupKeyCI(trust) {
		return false
	}
	var t trustJSON
	dec := json.NewDecoder(bytes.NewReader(trust))
	dec.DisallowUnknownFields()
	if dec.Decode(&t) != nil || dec.More() || t.Files == nil {
		return false
	}
	cur := digestsOf(files)
	if len(t.Files) != len(cur) {
		return false
	}
	for n, d := range cur {
		if t.Files[n] != d {
			return false
		}
	}
	want, err := hex.DecodeString(t.MAC)
	got, _ := hex.DecodeString(trustMAC(key, uid, backend, project, cur))
	return err == nil && hmac.Equal(want, got)
}
