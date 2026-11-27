package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 (dev) : découverte par labels, état d'un projet arrêté, approbation scellée. Faux docker / kubectl, dossiers temporaires.

func TestDecouverteDockerParLabels275(t *testing.T) {
	r := rcTools(t, true)
	e := Engine{rcBackend(t)}
	good := UserObjName("github-9", "cat", "proj")
	other := UserObjName("github-10", "cat", "proj")
	lines := []string{
		good + "|running|debian:12|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj,cl.image=debian:12",
		other + "|running|x|cl.launcher=ng2,cl.user=github-10,cl.backend=d,cl.category=cat,cl.project=proj",           // autre utilisateur
		good + "-old-1-abcd|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj",   // sauvegarde de recréation : nom ≠ nom canonique
		"cl-zzzzzzzz-cat-p|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=p",       // nom ne redonne pas les labels
		good + "|exited|x|cl.launcher=ng2,cl.user=github-9,cl.backend=d,cl.category=cat,cl.project=proj,cl.state=old", // étiquette de sauvegarde
		"cl-x|exited|x|cl.launcher=ng2", // sans label d'utilisateur
	}
	_ = os.WriteFile(filepath.Join(r.dir, "ps.out"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	ps, err := e.DiscoverUserProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Object != good || ps[0].Category != "cat" || ps[0].Project != "proj" || !ps[0].Running {
		t.Errorf("projets découverts : %+v (un seul attendu)", ps)
	}
	for _, c := range r.calls() {
		if !strings.HasPrefix(c, "ps ") {
			t.Errorf("commande autre que la lecture : %s", c)
		}
	}
}

func TestMetaProjetArrete275(t *testing.T) {
	m, err := ParseUserMeta([]byte(`{"branch":"main","dirty":true,"ahead":1,"behind":0,"head":"abc1234","tpl":"v5","team":"t","seen":"2026-11-27T10:00:00Z"}`))
	if err != nil || m.Branch != "main" || !m.Dirty || m.Ahead != 1 {
		t.Fatalf("état valide : %+v %v", m, err)
	}
	for name, raw := range map[string]string{
		"champ inconnu":  `{"branch":"a","contenu":"x"}`,
		"deux valeurs":   `{"branch":"a"}{"branch":"b"}`,
		"trop gros":      `{"branch":"` + strings.Repeat("x", 5000) + `"}`,
		"type inattendu": `{"branch":1}`,
		"négatif":        `{"ahead":-1}`,
		"JSON invalide":  `{"branch":`,
	} {
		if _, err := ParseUserMeta([]byte(raw)); err == nil {
			t.Errorf("%s accepté", name)
		}
	}
	m2, _ := ParseUserMeta([]byte("{\"branch\":\"a\\u0000b\\nc\",\"head\":\"" + strings.Repeat("h", 200) + "\"}"))
	if strings.ContainsAny(m2.Branch, "\x00\n") || len(m2.Head) != 64 {
		t.Errorf("chaînes non assainies : %q %d", m2.Branch, len(m2.Head))
	}
}

func TestApprobationScellee275(t *testing.T) {
	dev255Env(t)
	sp, _ := UserSpace("github-5")
	key, err := sp.ApprovalKey()
	if err != nil || len(key) != 32 {
		t.Fatalf("clé : %v %d", err, len(key))
	}
	if k2, _ := sp.ApprovalKey(); string(k2) != string(key) {
		t.Error("la clé change à chaque lecture")
	}
	if st, err := os.Stat(filepath.Join(sp.VaultDir(), approvalKeyFile)); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("droits de la clé : %v %v", err, st)
	}
	d := map[string]string{"env.yaml": "aa11", "env-template.yaml": "bb22"}
	tf := SealTrust(key, "github-5", d, time.Now())
	if !VerifyTrust(key, "github-5", tf, d) {
		t.Fatal("approbation valide refusée")
	}
	// forgé : MAC absent ou inventé ; copié pour un autre contenu ; autre uid ; clé changée ; fichier ajouté ou retiré
	forged := tf
	forged.MAC = strings.Repeat("0", 64)
	other := map[string]string{"env.yaml": "cc33", "env-template.yaml": "bb22"}
	if VerifyTrust(key, "github-5", forged, d) || VerifyTrust(key, "github-5", TrustFile{Digests: d}, d) {
		t.Error("MAC forgé ou absent accepté")
	}
	if VerifyTrust(key, "github-5", tf, other) {
		t.Error("approbation copiée pour un autre contenu acceptée")
	}
	if VerifyTrust(key, "github-6", tf, d) {
		t.Error("approbation d'un autre utilisateur acceptée")
	}
	if VerifyTrust(append([]byte{1}, key[1:]...), "github-5", tf, d) {
		t.Error("clé changée : approbation acceptée")
	}
	if VerifyTrust(key, "github-5", tf, map[string]string{"env.yaml": "aa11"}) || VerifyTrust(key, "github-5", tf, map[string]string{"env.yaml": "aa11", "env-template.yaml": "bb22", "x": "1"}) {
		t.Error("jeu de fichiers différent accepté")
	}
	// encodage non ambigu : ("ab","c") ≠ ("a","bc")
	if trustMAC(key, "u", map[string]string{"ab": "c"}) == trustMAC(key, "u", map[string]string{"a": "bc"}) {
		t.Error("encodage ambigu du HMAC")
	}
	// trust.json lu des octets : clé en double, champ inconnu, taille
	for name, raw := range map[string]string{
		"clé en double":   `{"digests":{"a":"1"},"digests":{"a":"2"},"date":"x","mac":"y"}`,
		"casse en double": `{"digests":{"a":"1","A":"2"},"date":"x","mac":"y"}`,
		"champ inconnu":   `{"digests":{},"date":"x","mac":"y","extra":1}`,
		"trop gros":       `{"digests":{"a":"` + strings.Repeat("x", 17<<10) + `"},"date":"x","mac":"y"}`,
		"sans digests":    `{"date":"x","mac":"y"}`,
	} {
		if _, err := ParseTrust([]byte(raw)); err == nil {
			t.Errorf("trust.json %s accepté", name)
		}
	}
	if _, err := ParseTrust([]byte(`{"digests":{"env.yaml":"aa"},"date":"d","mac":"m"}`)); err != nil {
		t.Errorf("trust.json valide refusé : %v", err)
	}
	// la clé n'est jamais écrite ailleurs que dans le coffre de l'utilisateur
	if _, err := AdminSpace().ApprovalKey(); err == nil {
		t.Error("clé d'approbation pour l'espace de l'admin")
	}
}
