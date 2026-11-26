package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #269 — un pod k8s en marche à l'ancien modèle de montage (StatefulSet SANS annotation cl.mounts) est un AVERTISSEMENT :
// il ne rend pas le backend inutilisable (sinon la carte ne s'affiche pas et Fermer / Forcer fermer n'arrêtent rien).
// Faux kubectl en tête du PATH (jamais de vrai cluster) ; il journalise ses appels et sert les StatefulSets du fichier « sts »
// (deux formes de jsonpath : diagnostic « nom|réplicas|mounts » et listage « nom|réplicas|prêtes|projet|mounts »).

func t269Kubectl(t *testing.T, sts string) (dir string) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sts"), []byte(sts), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
D="` + dir + `"
echo "$*" >> "$D/calls.log"
while [ "$1" = "-n" ] || [ "$1" = "--context" ] || [ "$1" = "--kubeconfig" ]; do shift 2; done
case "$1 $2" in
  "auth can-i")
    if grep -qx "$3 $4" "$D/deny" 2>/dev/null; then echo no; exit 1; fi
    echo yes ;;
  "get namespace") echo namespace/ns ;;
  "get pvc") printf 'Bound ["ReadWriteMany"]' ;;
  "get statefulsets")
    case "$*" in
      *readyReplicas*) cat "$D/sts-list" 2>/dev/null ;;
      *) cat "$D/sts" ;;
    esac ;;
  *) echo "{}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func t269Backend(name string) BackendConf {
	return BackendConf{Name: name, Type: "k8s", Namespace: "ns", Volumes: map[string]string{"/share/x": "pvc-x"}}
}

func t269Steps(b BackendConf) string { return twDetails(k8sSteps(b)) }

func TestK8s269PodAncienModeleEstUnAvertissementPasUnProbleme(t *testing.T) {
	withHome(t)
	t269Kubectl(t, "p-a|1| ") // en marche, sans cl.mounts
	b := t269Backend("k269-warn")
	if ps := k8sProblems(b); len(ps) != 0 {
		t.Errorf("pod à l'ancien modèle : problème bloquant au lieu d'un avertissement : %v", ps)
	}
	if s := t269Steps(b); !strings.Contains(s, OldMountsNote) || !strings.Contains(s, "p-a-0") {
		t.Errorf("l'avertissement « %s » (pod p-a-0) a disparu du diagnostic :\n%s", OldMountsNote, s)
	}
}

func TestK8s269ModeleCourantSansAvertissement(t *testing.T) {
	withHome(t)
	t269Kubectl(t, "p-a|1|"+mountsModel+" ")
	b := t269Backend("k269-ok")
	if ps := k8sProblems(b); len(ps) != 0 {
		t.Errorf("modèle courant : %v", ps)
	}
	if s := t269Steps(b); strings.Contains(s, OldMountsNote) {
		t.Errorf("avertissement à tort pour un pod au modèle courant :\n%s", s)
	}
}

// Garde-fou : un vrai problème reste bloquant, même avec un pod à l'ancien modèle.
func TestK8s269VraiProblemResteBloquant(t *testing.T) {
	withHome(t)
	dir := t269Kubectl(t, "p-a|1| ")
	if err := os.WriteFile(filepath.Join(dir, "deny"), []byte("create statefulsets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ps := k8sProblems(t269Backend("k269-deny"))
	if len(ps) == 0 || !strings.Contains(ps[0].What, "StatefulSets") {
		t.Errorf("droit refusé non remonté en problème : %v", ps)
	}
}

// Le navigateur (ReadCtrView) ne saute plus le listage du backend : le conteneur est connu, OldMounts levé, aucun Diag.
func TestK8s269ReadCtrViewListeLePodAncienModele(t *testing.T) {
	withHome(t)
	root := t.TempDir() // racine de volume partagé : le projet et la config de claude y sont (diagnostic des chemins partagés)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
	proj := filepath.Join(root, "p")
	if err := os.MkdirAll(filepath.Dir(EnvPath(proj)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(EnvPath(proj), []byte("image: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t269Kubectl(t, "p-a|1| ")
	if err := os.WriteFile(filepath.Join(dir, "sts-list"), []byte("p-a|1|1|"+proj+"|\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(Ng2ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "backends:\n  k269-view:\n    type: k8s\n    namespace: ns\n    volumes:\n      " + root + ": pvc-x\n"
	if err := os.WriteFile(BackendsPath(), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveAssignments(map[string]string{proj: "k269-view"}); err != nil {
		t.Fatal(err)
	}
	v := ReadCtrView([]string{proj})
	if ps := v.Diag["k269-view"]; len(ps) != 0 {
		t.Fatalf("diagnostic bloquant à cause du pod à l'ancien modèle : %v", ps)
	}
	f, ok := v.Found[proj]
	if !ok || !f.Running || !f.OldMounts || f.Name != "p-a-0" {
		t.Errorf("conteneur du projet non listé : %+v (ok=%v)", f, ok)
	}
}

// Cas limite (sans correctif attendu) : deux StatefulSets de même cl.project sont listés tous les deux, sans erreur.
func TestK8s269DeuxStatefulSetsMemeProjet(t *testing.T) {
	withHome(t)
	dir := t269Kubectl(t, "")
	if err := os.WriteFile(filepath.Join(dir, "sts-list"), []byte("a-p|1|1|/r/p|\nb-p|1|1|/r/p|v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err := Engine{t269Backend("k269-dup")}.K8sList()
	if err != nil || len(fs) != 2 {
		t.Fatalf("K8sList = %+v, %v", fs, err)
	}
	if fs[0].Project != fs[1].Project || fs[0].Name == fs[1].Name || !fs[0].OldMounts || fs[1].OldMounts {
		t.Errorf("deux entrées attendues, une seule à l'ancien modèle : %+v", fs)
	}
}
