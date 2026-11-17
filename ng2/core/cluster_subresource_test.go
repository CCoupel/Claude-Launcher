package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Non-régression du faux négatif du diagnostic (rapport droits-k8s-20261117-134354.md) : `kubectl auth can-i create
// pods/exec` lit « pods/exec » comme « le pod nommé exec » (réponse « no » alors que exec est accordé) et
// `patch statefulsets/scale` comme « le StatefulSet nommé scale » (réponse « yes » à tort). Une sous-ressource se
// vérifie par `--subresource=`. Faux kubectl qui répond comme le vrai, selon les arguments ; HOME factice.

func twFakeKubectlSubresource(t *testing.T) (dir, log string) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	log = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
D="` + dir + `"
echo "$*" >> "$D/calls.log"
while [ "$1" = "-n" ] || [ "$1" = "--context" ]; do shift 2; done
if [ "$1 $2" = "auth can-i" ]; then
  verb="$3"; res="$4"; sub=""
  for a in "$@"; do case "$a" in --subresource=*) sub="${a#--subresource=}" ;; esac; done
  if [ -n "$sub" ]; then
    # sous-ressource donnée comme il se doit : réponse d'après le Role simulé (deny : « verbe ressource/sous-ressource »)
    if grep -qx "$verb $res/$sub" "$D/deny" 2>/dev/null; then echo no; exit 1; fi
    echo yes; exit 0
  fi
  case "$res" in
    pods/exec) echo no; exit 1 ;;            # lu comme « le pod nommé exec » : faux négatif
    statefulsets/scale) echo yes; exit 0 ;;  # lu comme « le StatefulSet nommé scale » : faux positif
  esac
  if grep -qx "$verb $res" "$D/deny" 2>/dev/null; then echo no; exit 1; fi
  echo yes; exit 0
fi
case "$1 $2" in
  "get pod") cat "$D/pod.json" ;;
  "get pvc") cat "$D/pvc" ;;
  *) echo "{}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteMany]"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "pod.json"), []byte(`{"spec":{"volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"claude-projects-qualif"}}],"containers":[{"volumeMounts":[{"name":"d","mountPath":"/data/q"}]}]}}`), 0o644)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, log
}

func twHealthyBackend() BackendConf {
	return BackendConf{Name: "k8s", Type: "k8s", Context: "k8s", Namespace: "claude-qualif", Volumes: map[string]string{"/data/q": "claude-projects-qualif"}}
}

// Un backend k8s SAIN (tous les droits accordés, y compris exec et scale) n'est pas « en erreur ».
func TestHealthyK8sBackendIsNotInErrorWithSubresourceProbes(t *testing.T) {
	twClusterEnv(t)
	_, log := twFakeKubectlSubresource(t)
	b := twHealthyBackend()
	steps := twSteps(b)
	for _, s := range steps {
		if !s.OK {
			t.Errorf("étape en échec sur un backend sain : %s | %s", s.Label, s.Detail)
		}
	}
	if ps := k8sProblems(b); len(ps) != 0 {
		t.Errorf("backend sain « en erreur » (faux négatif du diagnostic) : %+v", ps)
	}
	calls, _ := os.ReadFile(log)
	c := string(calls)
	// les sous-ressources passent par --subresource, jamais écrites « type/sous-ressource »
	for _, want := range []string{"--subresource=exec", "--subresource=scale"} {
		if !strings.Contains(c, want) {
			t.Errorf("sonde sans « %s » :\n%s", want, c)
		}
	}
	for _, bad := range []string{"pods/exec", "statefulsets/scale"} {
		for _, l := range strings.Split(c, "\n") {
			if strings.Contains(l, "can-i") && strings.Contains(l, bad) {
				t.Errorf("sonde en forme « %s » (lue comme un nom d'objet) : %s", bad, l)
			}
		}
	}
	if !strings.Contains(c, "can-i create pods --subresource=exec") || !strings.Contains(c, "can-i patch statefulsets --subresource=scale") {
		t.Errorf("formes attendues « create pods --subresource=exec » et « patch statefulsets --subresource=scale » :\n%s", c)
	}
}

// Un droit de sous-ressource réellement manquant est NOMMÉ (create pods/exec, patch statefulsets/scale) : le faux
// positif de scale (patch statefulsets accordé, pas scale) est levé aussi.
func TestMissingSubresourceRightsAreNamed(t *testing.T) {
	twClusterEnv(t)
	dir, _ := twFakeKubectlSubresource(t)
	_ = os.WriteFile(filepath.Join(dir, "deny"), []byte("create pods/exec\npatch statefulsets/scale\n"), 0o644)
	got := twDetails(twSteps(twHealthyBackend()))
	for _, want := range []string{"droit manquant : create pods/exec", "droit manquant : patch statefulsets/scale"} {
		if !strings.Contains(got, want) {
			t.Errorf("« %s » absent :\n%s", want, got)
		}
	}
	// les autres droits restent accordés
	for _, ok := range []string{"get statefulsets", "list pods", "delete pods", "list events"} {
		if strings.Contains(got, "droit manquant : "+ok) {
			t.Errorf("droit accordé (%s) déclaré manquant", ok)
		}
	}
	if ps := k8sProblems(twHealthyBackend()); len(ps) != 2 {
		t.Errorf("%d problème(s), attendu 2 (exec et scale) : %+v", len(ps), ps)
	}
}

// exec seul manquant : scale reste vérifié correctement (et inversement) — chaque sous-ressource est sondée à part.
func TestSubresourcesAreProbedIndependently(t *testing.T) {
	twClusterEnv(t)
	dir, _ := twFakeKubectlSubresource(t)
	_ = os.WriteFile(filepath.Join(dir, "deny"), []byte("patch statefulsets/scale\n"), 0o644)
	got := twDetails(twSteps(twHealthyBackend()))
	if strings.Contains(got, "droit manquant : create pods/exec") {
		t.Errorf("exec déclaré manquant alors qu'il est accordé :\n%s", got)
	}
	if !strings.Contains(got, "droit manquant : patch statefulsets/scale") {
		t.Errorf("scale manquant non détecté (faux positif de « patch statefulsets ») :\n%s", got)
	}
}
