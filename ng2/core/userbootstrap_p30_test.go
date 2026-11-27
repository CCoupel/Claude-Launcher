package core

import (
	"regexp"
	"strings"
	"testing"
)

// LOT-3.2 (#275, D3 révisée P28 / P30) — bootstrap ROOT du conteneur d'un projet d'utilisateur : paquets, chown unique du stockage par le
// bootstrap (marqueur /space/.claude-launcher/owner), contrôle d'inscriptibilité sous l'uid du traitement, passage définitif à 2000:2000 par
// setpriv. Étiquette « s275api » ; API proposée :
//
//	func UserBootstrapScript(uid, gid int) string                                   // le script « sh -c » du conteneur (pid1)
//	func StorageChownNeeded(marker string, ownerUID, ownerGID, uid, gid int) bool   // idempotence : marqueur « <uid>:<gid> » et propriétaire de /space
//	func StorageRefusal(uid, gid int, dir string) string                             // refus nommé avec la commande chown -R <uid>:<gid> <dossier>
//	func ExplainApplyError(stderr string) error                                      // refus PodSecurity « restricted » → message nommé

func TestP30ScriptDeBootstrap(t *testing.T) {
	s := UserBootstrapScript(2000, 2000)
	// jamais de sudo ; jamais de chown suivant les liens ni sortant de /space
	for _, bad := range []string{"sudo", "sudoers", "chown -R -L", "chown -RL", "chown -R 2000:2000 /\n", "chown -R 2000:2000 / ", "chmod 777", "chmod -R 777", "--init-groups"} {
		if strings.Contains(s, bad) {
			t.Errorf("« %s » dans le script de bootstrap", bad)
		}
	}
	idx := func(sub string) int { return strings.Index(s, sub) }
	chown := regexp.MustCompile(`chown -R -h 2000:2000 /space`).FindStringIndex(s)
	if chown == nil {
		t.Fatalf("chown -R -h 2000:2000 /space absent :\n%s", s)
	}
	if idx("/space/.claude-launcher/owner") < 0 {
		t.Error("marqueur /space/.claude-launcher/owner absent")
	}
	setpriv := idx(e2SetprivPrefix(2000, 2000))
	if setpriv < 0 {
		t.Fatalf("passage à 2000:2000 par setpriv absent :\n%s", s)
	}
	if chown[0] > setpriv {
		t.Error("le chown doit précéder le passage à 2000:2000 (root)")
	}
	// contrôle d'inscriptibilité SOUS l'uid du traitement, avant la session, avec le refus nommé
	if !strings.Contains(s, "chown -R 2000:2000") || !strings.Contains(s, "Le stockage n'est pas accessible en écriture à l'utilisateur 2000:2000") {
		t.Error("refus nommé avec la commande « chown -R 2000:2000 <dossier> » absent")
	}
	if w := strings.Index(s, "test -w"); w < 0 && !strings.Contains(s, "touch ") {
		t.Error("contrôle d'inscriptibilité absent")
	}
	for _, env := range []string{"HOME=/space/.claude/home", "CLAUDE_CONFIG_DIR=/space/.claude", "XDG_CONFIG_HOME=/space/.claude/home/.config"} {
		if !strings.Contains(s, env) {
			t.Errorf("%s absent", env)
		}
	}
	// uid/gid variables : le script suit run_as (3000:4000), jamais une valeur codée en dur à 2000
	s2 := UserBootstrapScript(3000, 4000)
	if !strings.Contains(s2, "chown -R -h 3000:4000 /space") || !strings.Contains(s2, e2SetprivPrefix(3000, 4000)) || strings.Contains(s2, "2000:2000") {
		t.Errorf("run_as 3000:4000 non suivi :\n%s", s2)
	}
}

// uid/gid 0 : refusés à la construction du script (le traitement n'est jamais root).
func TestP30ScriptRefuseRoot(t *testing.T) {
	for _, c := range [][2]int{{0, 2000}, {2000, 0}, {0, 0}, {-1, 2000}} {
		if s := UserBootstrapScript(c[0], c[1]); s != "" && !strings.Contains(s, "exit 1") && strings.Contains(s, "--reuid=0") {
			t.Errorf("script produit pour %v avec reuid=0", c)
		}
		if s := UserBootstrapScript(c[0], c[1]); strings.Contains(s, "--reuid=0") || strings.Contains(s, "--regid=0") {
			t.Errorf("%v : traitement en root", c)
		}
	}
}

// Idempotence : marqueur présent et propriétaire égal → aucun chown ; stockage root:root → chown ; uid du projet changé → nouveau chown.
func TestP30Idempotence(t *testing.T) {
	cases := []struct {
		name           string
		marker         string
		ownerU, ownerG int
		uid, gid       int
		want           bool
	}{
		{"première ouverture (root:root, sans marqueur)", "", 0, 0, 2000, 2000, true},
		{"déjà fait", "2000:2000", 2000, 2000, 2000, 2000, false},
		{"marqueur présent mais propriétaire changé de l'extérieur", "2000:2000", 0, 0, 2000, 2000, true},
		{"uid du projet changé (M4)", "2000:2000", 2000, 2000, 3000, 4000, true},
		{"marqueur illisible", "xx", 2000, 2000, 2000, 2000, true},
		{"marqueur d'un autre uid, propriétaire égal à l'ancien", "3000:4000", 3000, 4000, 2000, 2000, true},
	}
	for _, c := range cases {
		if got := StorageChownNeeded(c.marker, c.ownerU, c.ownerG, c.uid, c.gid); got != c.want {
			t.Errorf("%s : chown=%v, attendu %v", c.name, got, c.want)
		}
	}
}

// Chown refusé (NFS root_squash, MooseFS sans droits) : refus nommé, jamais un démarrage de session.
func TestP30RefusNomme(t *testing.T) {
	m := StorageRefusal(2000, 2000, "/data/projets-alice")
	for _, want := range []string{"Le stockage n'est pas accessible en écriture à l'utilisateur 2000:2000", "chown -R 2000:2000 /data/projets-alice"} {
		if !strings.Contains(m, want) {
			t.Errorf("refus sans « %s » : %s", want, m)
		}
	}
	// dossier hostile : jamais interpolé tel quel dans une commande à copier
	if m := StorageRefusal(2000, 2000, "/x; rm -rf /"); strings.Contains(m, "; rm -rf /") {
		t.Errorf("dossier non protégé dans la commande proposée : %s", m)
	}
}

// PodSecurity « restricted » : refus nommé (le bootstrap root exige baseline).
func TestP28PSARestricted(t *testing.T) {
	stderr := `Error from server (Forbidden): error when creating "STDIN": statefulsets.apps "cl-ab12cd34-cat-proj" is forbidden: violates PodSecurity "restricted:latest": ` +
		`allowPrivilegeEscalation != false, unrestricted capabilities, runAsNonRoot != true (container "main" must not set runAsUser=0)`
	err := ExplainApplyError(stderr)
	if err == nil || !strings.Contains(err.Error(), "le namespace impose le profil restricted : le démarrage d'un projet exige baseline") {
		t.Errorf("PSA restricted : %v", err)
	}
	if err := ExplainApplyError("Error from server (NotFound): namespaces \"x\" not found"); err != nil && strings.Contains(err.Error(), "restricted") {
		t.Errorf("autre erreur prise pour du PSA : %v", err)
	}
	if err := ExplainApplyError(`violates PodSecurity "baseline:latest": privileged`); err != nil && strings.Contains(err.Error(), "exige baseline") {
		t.Errorf("refus baseline présenté comme restricted : %v", err)
	}
}
