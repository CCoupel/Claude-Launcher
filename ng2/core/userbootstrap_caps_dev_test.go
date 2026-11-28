package core

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// QUALIF 7.1.0.7 : « setpriv --bounding-set=-all » exige CAP_SETPCAP, absente des cinq capacités → le pod bouclait. Le bootstrap ne réduit plus le
// bounding set (le conteneur démarre déjà en « drop ALL » + cinq capacités) ; l'abandon de root vide permitted/effective, no_new_privs interdit tout regain.

var userFiveCaps = []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETUID", "SETGID"}

func TestBootstrapSansSetpcap275(t *testing.T) {
	sc := UserBootstrapScript(2000, 2000)
	if strings.Contains(sc, "bounding-set") || strings.Contains(sc, "SETPCAP") {
		t.Errorf("bounding-set / SETPCAP dans le script de démarrage :\n%s", sc)
	}
	for _, want := range []string{"--no-new-privs", "--inh-caps=-all", "--reuid=2000", "--regid=2000", "--clear-groups"} {
		if !strings.Contains(sc, want) {
			t.Errorf("%s absent du script", want)
		}
	}
	if p := SetprivPrefix(2000, 2000); strings.Contains(p, "bounding-set") || !strings.Contains(p, "--no-new-privs") {
		t.Errorf("préfixe des exec : %s", p)
	}
	// un échec de setpriv n'est PAS dit « stockage non inscriptible » : message propre, distinct
	i, j := strings.Index(sc, "échec de l'abandon des privilèges"), strings.Index(sc, "Le stockage n'est pas accessible en écriture")
	if i < 0 || j < 0 || i > j {
		t.Errorf("l'échec de setpriv doit être contrôlé et nommé AVANT le test d'inscriptibilité (%d, %d)", i, j)
	}
}

func TestCapacitesExactes275(t *testing.T) {
	// docker/podman : exactement cap-drop=ALL + cinq cap-add, jamais SETPCAP
	b := e2Docker(t, "docker")
	args, err := e2Args(t, b, "docker.io/library/debian:12")
	if err != nil {
		t.Fatal(err)
	}
	var adds []string
	drop := false
	for i, a := range args {
		switch {
		case a == "--cap-drop=ALL":
			drop = true
		case strings.HasPrefix(a, "--cap-add="):
			adds = append(adds, strings.Split(strings.TrimPrefix(a, "--cap-add="), ",")...)
		case a == "--cap-add" && i+1 < len(args):
			adds = append(adds, args[i+1])
		}
	}
	got := strings.Join(adds, ",")
	if !drop || got != strings.Join(userFiveCaps, ",") || strings.Contains(strings.Join(args, " "), "SETPCAP") {
		t.Errorf("docker : drop=%v add=%q (cinq capacités exactes, sans SETPCAP)", drop, got)
	}
	// k8s : drop ALL + les cinq, jamais SETPCAP
	m, err := e2Manifest(t, e2K8s(t), "docker.io/library/debian:12")
	if err != nil {
		t.Fatal(err)
	}
	jb, _ := json.Marshal(m)
	raw := string(jb)
	if strings.Contains(raw, "SETPCAP") || !strings.Contains(raw, `"drop":["ALL"]`) || !strings.Contains(raw, `"add":["CHOWN","DAC_OVERRIDE","FOWNER","SETUID","SETGID"]`) {
		t.Errorf("k8s : capacités inattendues dans %s", raw)
	}
}

// Test réel (si un démon docker local et l'image sont disponibles) : session finale en uid 2000, sans capacité (CapEff=CapPrm=0), NoNewPrivs=1 — avec
// EXACTEMENT les capacités du conteneur (drop ALL + cinq, sans SETPCAP). Ignoré sinon : à vérifier en QUALIF.
func TestAbandonDePrivilegesReel275(t *testing.T) {
	unixOnly(t)
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("démon docker indisponible : vérification en QUALIF (CapEff=0, CapPrm=0, NoNewPrivs=1, uid 2000)")
	}
	const img = "docker.io/library/debian:12"
	if exec.Command("docker", "image", "inspect", img).Run() != nil {
		t.Skip("image debian:12 absente en local (pas de téléchargement en test)")
	}
	args := []string{"run", "--rm", "--cap-drop=ALL"}
	for _, c := range userFiveCaps {
		args = append(args, "--cap-add="+c)
	}
	args = append(args, "--security-opt=no-new-privileges", img, "sh", "-c", SetprivPrefix(2000, 2000)+` sh -c 'grep -E "^(Uid|CapInh|CapPrm|CapEff|CapBnd|CapAmb|NoNewPrivs|Seccomp):" /proc/self/status'`)
	c := exec.Command("docker", args...)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = c.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		_ = c.Process.Kill()
		t.Fatal("délai dépassé")
	}
	s := string(out)
	// valeurs attendues (avis security) ; Seccomp 2 : profil par défaut du moteur — vérifié aussi en QUALIF sur pid 1, un shell de session tmux et un exec court
	want := []string{"Uid:\t2000", "CapInh:\t0000000000000000", "CapPrm:\t0000000000000000", "CapEff:\t0000000000000000", "CapAmb:\t0000000000000000",
		"CapBnd:\t00000000000000cb", "NoNewPrivs:\t1", "Seccomp:\t2"}
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("« %s » absent (err=%v) :\n%s", w, err, s)
		}
	}
}

// QUALIF 7.1.0.8 : docker/podman refusent « --cap-add=A,B,C » — UNE option par capacité, valeur sans virgule ni préfixe CAP_ ; le marqueur du chown vit
// dans le volume .claude (persistant) et un marqueur à jour ne rejoue pas le chown récursif.
func TestCapAddUneParOption275(t *testing.T) {
	for _, typ := range []string{"docker", "podman"} {
		args, err := e2Args(t, e2Docker(t, typ), "docker.io/library/debian:12")
		if err != nil {
			t.Fatal(err)
		}
		var adds []string
		for _, a := range args {
			if strings.HasPrefix(a, "--cap-add") {
				v, ok := strings.CutPrefix(a, "--cap-add=")
				if !ok || strings.ContainsAny(v, ", ") || strings.HasPrefix(v, "CAP_") || v == "" {
					t.Errorf("%s : option invalide pour le moteur : %q (une capacité par option, nom seul)", typ, a)
				}
				adds = append(adds, v)
			}
		}
		if strings.Join(adds, ",") != strings.Join(userFiveCaps, ",") {
			t.Errorf("%s : capacités %v (cinq exactes attendues)", typ, adds)
		}
	}
}

func TestMarqueurChownPersistant275(t *testing.T) {
	sc := UserBootstrapScript(2000, 2000)
	if !strings.Contains(sc, "M=/space/.claude/.cl-owner") || strings.Contains(sc, "M=/space/.claude-launcher/owner") {
		t.Errorf("marqueur hors du volume .claude :\n%s", sc)
	}
	// chown récursif seulement si le marqueur manque ou diffère ; propriétaire de /space seul : chown NON récursif
	if !strings.Contains(sc, `if [ "$cur" != "$U:$G" ]; then`) || !strings.Contains(sc, "elif [ \"$own\" != \"$U:$G\" ]; then chown -h 2000:2000 /space") {
		t.Errorf("idempotence du chown :\n%s", sc)
	}
}
