package core

import (
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// #253 (v7.0.0) — pods projets k8s sans coffre ni jeton de ServiceAccount.
// Contrat : contracts/helm-values.md « Pods projets (backend k8s) ». Écrits depuis le contrat, avant le code (TDD) :
// RED attendu tant que k8sManifest monte la racine entière, sans cl.mounts ni automountServiceAccountToken.

const (
	t253Root = "/data/p"
	t253Proj = "/data/p/proj"
	t253Cfg  = "/data/p/.claude"
)

// t253Manifest : manifeste du StatefulSet pour le backend b (CLAUDE_CONFIG_DIR = cfg) ; rend aussi le JSON décodé.
func t253Manifest(t *testing.T, b BackendConf, proj, cfg string, env EnvSpec) (m map[string]any, err error) {
	t.Helper()
	withHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if env.Image == "" {
		env.Image = "debian:12"
	}
	raw, err := Engine{b}.k8sManifest(proj, env)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m, nil
}

type t253Mount struct{ Name, Path, Sub string }

// t253Parts : spec du pod, montages du conteneur (dans l'ordre), volumes par nom.
func t253Parts(m map[string]any) (spec map[string]any, mounts []t253Mount, vols map[string]map[string]any) {
	tpl := m["spec"].(map[string]any)["template"].(map[string]any)
	spec = tpl["spec"].(map[string]any)
	ctr := spec["containers"].([]any)[0].(map[string]any)
	for _, x := range ctr["volumeMounts"].([]any) {
		mm := x.(map[string]any)
		s, _ := mm["subPath"].(string)
		mounts = append(mounts, t253Mount{mm["name"].(string), mm["mountPath"].(string), s})
	}
	vols = map[string]map[string]any{}
	for _, x := range spec["volumes"].([]any) {
		v := x.(map[string]any)
		vols[v["name"].(string)] = v
	}
	return
}

func t253K8s(vols map[string]string) BackendConf {
	return BackendConf{Name: "k8s", Type: "k8s", Volumes: vols}
}

// Aucune racine montée : seuls le dossier du projet et CLAUDE_CONFIG_DIR, en subPath, au même chemin que le launcher.
func TestK8s253MountsOnlyProjectAndConfigDir(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), t253Proj, t253Cfg, EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, vols := t253Parts(m)
	got := map[string]string{}
	for _, mt := range mounts {
		if _, pvc := vols[mt.Name]["persistentVolumeClaim"]; pvc {
			if mt.Sub == "" || mt.Path == t253Root {
				t.Errorf("racine de volume montée entière : %+v", mt)
			}
			got[mt.Path] = mt.Sub
		}
	}
	want := map[string]string{t253Proj: "proj", t253Cfg: ".claude"}
	if len(got) != 2 || got[t253Proj] != "proj" || got[t253Cfg] != ".claude" {
		t.Errorf("montages PVC = %v, attendu %v", got, want)
	}
	for _, mt := range mounts {
		for _, banned := range []string{".kube", ".cache", "claude-launcher-ng2", "backends.yaml"} {
			if strings.Contains(mt.Path, banned) || strings.Contains(mt.Sub, banned) {
				t.Errorf("montage exposant %s : %+v", banned, mt)
			}
		}
	}
}

// Un seul PVC pour deux chemins : un seul volume (dédupliqué), deux montages ; deux PVC : chacun son volume.
func TestK8s253VolumesDeduplicated(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), t253Proj, t253Cfg, EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, vols := t253Parts(m)
	pvc := 0
	for _, v := range vols {
		if _, ok := v["persistentVolumeClaim"]; ok {
			pvc++
		}
	}
	if pvc != 1 {
		t.Errorf("volumes PVC = %d, attendu 1 (dédupliqué) : %v", pvc, vols)
	}
	for _, mt := range mounts {
		if _, ok := vols[mt.Name]; !ok {
			t.Errorf("montage %+v sans volume déclaré", mt)
		}
	}

	m, err = t253Manifest(t, t253K8s(map[string]string{t253Root: "pvc-projets", "/cfg": "pvc-config"}), t253Proj, "/cfg/claude", EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, vols = t253Parts(m)
	claims := map[string]string{} // mountPath → claimName
	for _, mt := range mounts {
		if p, ok := vols[mt.Name]["persistentVolumeClaim"].(map[string]any); ok {
			claims[mt.Path] = p["claimName"].(string)
			if mt.Sub == "" {
				t.Errorf("racine montée entière : %+v", mt)
			}
		}
	}
	if claims[t253Proj] != "pvc-projets" || claims["/cfg/claude"] != "pvc-config" || len(claims) != 2 {
		t.Errorf("claims par montage = %v", claims)
	}
}

// Chemin hors de tout volume : erreur « stockage partagé », jamais un repli sur la racine.
func TestK8s253PathOutsideVolumeRefused(t *testing.T) {
	b := t253K8s(map[string]string{t253Root: "claude-projets"})
	for name, c := range map[string]struct{ proj, cfg string }{
		"projet hors volume":    {"/ailleurs/proj", t253Cfg},
		"config hors volume":    {t253Proj, "/ailleurs/.claude"},
		"projet évadé par ..":   {"/data/p/../etc/proj", t253Cfg},
		"préfixe de nom voisin": {"/data/pp/proj", t253Cfg}, // /data/pp n'est pas sous /data/p
	} {
		t.Run(name, func(t *testing.T) {
			_, err := t253Manifest(t, b, c.proj, c.cfg, EnvSpec{})
			if err == nil || !strings.Contains(err.Error(), "stockage partagé") {
				t.Errorf("erreur attendue « stockage partagé », obtenu %v", err)
			}
		})
	}
}

// Chemins nettoyés (path.Clean) : « .. » et « / » final ne changent ni le subPath ni le mountPath.
func TestK8s253PathsCleaned(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), "/data/p/x/../proj/", t253Cfg, EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, _ := t253Parts(m)
	ok := false
	for _, mt := range mounts {
		if mt.Sub == "proj" && path.Clean(mt.Path) == t253Proj {
			ok = true
		}
		if strings.Contains(mt.Sub, "..") || strings.HasSuffix(mt.Sub, "/") || strings.HasPrefix(mt.Sub, "/") {
			t.Errorf("subPath non nettoyé : %+v", mt)
		}
	}
	if !ok {
		t.Errorf("montage du projet nettoyé absent : %+v", mounts)
	}
}

// Projet à la racine même d'un volume ET CLAUDE_CONFIG_DIR sous elle : le projet est monté (subPath vide permis car
// c'est son dossier), jamais plus que lui ; ici on vérifie seulement qu'aucun montage ne couvre un parent du projet.
func TestK8s253NoMountCoversParentOfProject(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), t253Proj, t253Cfg, EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, vols := t253Parts(m)
	for _, mt := range mounts {
		if _, pvc := vols[mt.Name]["persistentVolumeClaim"]; pvc && mt.Path != t253Proj && mt.Path != t253Cfg {
			t.Errorf("montage PVC inattendu : %+v", mt)
		}
	}
}

// Dossiers « local » (emptyDir) inchangés : montés par-dessus le dossier du projet, donc après lui.
func TestK8s253LocalDirsUnchanged(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), t253Proj, t253Cfg,
		EnvSpec{Local: []string{"node_modules", ".cache"}})
	if err != nil {
		t.Fatal(err)
	}
	_, mounts, vols := t253Parts(m)
	projIdx, locals := -1, map[string]int{}
	for i, mt := range mounts {
		if mt.Path == t253Proj {
			projIdx = i
		}
		if _, ed := vols[mt.Name]["emptyDir"]; ed {
			locals[mt.Path] = i
			if mt.Sub != "" {
				t.Errorf("emptyDir avec subPath : %+v", mt)
			}
		}
	}
	for _, p := range []string{t253Proj + "/node_modules", t253Proj + "/.cache"} {
		i, ok := locals[p]
		if !ok {
			t.Errorf("dossier « local » %s non monté en emptyDir : %+v", p, mounts)
		} else if projIdx < 0 || i < projIdx {
			t.Errorf("%s monté avant le dossier du projet (masqué) : %+v", p, mounts)
		}
	}
}

// automountServiceAccountToken: false par défaut ; une valeur explicite de pod: dans backends.yaml l'emporte.
func TestK8s253AutomountServiceAccountToken(t *testing.T) {
	for name, c := range map[string]struct {
		pod  map[string]any
		want bool
	}{
		"défaut":             {nil, false},
		"explicite true":     {map[string]any{"automountServiceAccountToken": true}, true},
		"explicite false":    {map[string]any{"automountServiceAccountToken": false}, false},
		"autres clés du pod": {map[string]any{"priorityClassName": "bas"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			b := t253K8s(map[string]string{t253Root: "claude-projets"})
			b.Pod = c.pod
			m, err := t253Manifest(t, b, t253Proj, t253Cfg, EnvSpec{})
			if err != nil {
				t.Fatal(err)
			}
			spec, _, _ := t253Parts(m)
			v, present := spec["automountServiceAccountToken"]
			if !present || v != c.want {
				t.Errorf("automountServiceAccountToken = %v (présent %v), attendu %v", v, present, c.want)
			}
		})
	}
}

// Annotation cl.mounts: "v2" sur le StatefulSet et sur le pod.
func TestK8s253MountsAnnotation(t *testing.T) {
	m, err := t253Manifest(t, t253K8s(map[string]string{t253Root: "claude-projets"}), t253Proj, t253Cfg, EnvSpec{})
	if err != nil {
		t.Fatal(err)
	}
	ss := m["metadata"].(map[string]any)["annotations"].(map[string]any)
	pod := m["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
	for n, a := range map[string]map[string]any{"statefulset": ss, "pod": pod} {
		if a["cl.mounts"] != "v2" {
			t.Errorf("annotation cl.mounts du %s = %v, attendu v2", n, a["cl.mounts"])
		}
	}
}

// Diagnostic : « jeton de ServiceAccount monté » seulement si pod.automountServiceAccountToken est posé à true.
// (Hypothèse : ligne portée par les étapes du diagnostic en cluster — k8sStepsInCluster ; voir handoff.)
func TestK8s253DiagSignalsMountedToken(t *testing.T) {
	setup := func(pod map[string]any) string {
		twClusterEnv(t)
		dir, _ := twFakeKubectl(t)
		_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteMany]"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "pod.json"), []byte(`{"spec":{"volumes":[],"containers":[]}}`), 0o644)
		b := BackendConf{Name: "k8s", Type: "k8s", Pod: pod}
		return twDetails(twSteps(b))
	}
	if got := setup(map[string]any{"automountServiceAccountToken": true}); !strings.Contains(got, "jeton de ServiceAccount monté") {
		t.Errorf("« jeton de ServiceAccount monté » absent :\n%s", got)
	}
	if got := setup(nil); strings.Contains(got, "jeton de ServiceAccount monté") {
		t.Errorf("signalé alors que le jeton n'est pas monté :\n%s", got)
	}
}

// ── Ancien modèle de montage (P6) ─────────────────────────────────────────────────────────────────────────────

// t253Shared : racine partagée réelle (dossier temporaire) pour passer checkShared dans k8sEnsure.
func t253Shared(t *testing.T) (root, proj string, b BackendConf) {
	t.Helper()
	root = t.TempDir()
	b = t253K8s(map[string]string{root: "claude-projets"})
	b.Shared = []string{root}
	return root, filepath.Join(root, "proj"), b
}

// t253Cluster : faux kubectl qui émule les annotations (ann.<nom>) lues en jsonpath, la phase (fichier phase) et
// l'existence du StatefulSet (fichier ss) ; « apply » enregistre le manifeste (applied.json) et pose cl.mounts=v2.
func t253Cluster(t *testing.T) (dir string) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	script := `#!/bin/sh
D="` + dir + `"
echo "$*" >> "$D/calls.log"
while [ "$1" = "-n" ] || [ "$1" = "--context" ] || [ "$1" = "--kubeconfig" ]; do shift 2; done
case "$1 $2" in
  "apply -f") cat > "$D/applied.json"; echo v2 > "$D/ann.mounts"; echo applied ;;
  "get statefulset"|"get pod")
    [ "$1 $2" = "get statefulset" ] && [ ! -e "$D/ss" ] && { echo "NotFound" >&2; exit 1; }
    jp=""; for a in "$@"; do case "$a" in jsonpath=*) jp="${a#jsonpath=}";; esac; done
    out="$jp"
    for n in image local sched project mounts; do
      v=$(cat "$D/ann.$n" 2>/dev/null)
      out=$(printf '%s' "$out" | sed "s#{.metadata.annotations.cl\\\\.$n}#$v#g")
    done
    ph=$(cat "$D/phase" 2>/dev/null)
    out=$(printf '%s' "$out" | sed "s#{.status.phase}#$ph#g; s#{.metadata.deletionTimestamp}##g")
    printf '%s' "$out" ;;
  "wait pod/"*|"wait --for=condition=Ready") exit 0 ;;
  *) echo "{}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// t253Existing : état d'un StatefulSet existant à jour (image, local, placement, projet), avec ou sans cl.mounts.
func t253Existing(t *testing.T, dir string, b BackendConf, proj, phase, mounts string) {
	t.Helper()
	w := func(n, v string) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("ss", "1")
	w("ann.image", "debian:12")
	w("ann.local", "")
	w("ann.sched", b.schedKey())
	w("ann.project", RemotePath(proj))
	if mounts != "" {
		w("ann.mounts", mounts)
	}
	if phase != "" {
		w("phase", phase)
	}
}

func t253Ensure(t *testing.T, dir string, b BackendConf, proj string) (pod, logs, calls string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(filepath.Dir(proj), ".claude"))
	var sb strings.Builder
	p, err := Engine{b}.k8sEnsure(proj, EnvSpec{Image: "debian:12"}, io.Writer(&sb))
	if err != nil {
		t.Fatalf("k8sEnsure : %v\n%s", err, sb.String())
	}
	c, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	return p, sb.String(), string(c)
}

// Pod EN MARCHE à l'ancien modèle (sans cl.mounts) : conservé (sa session continue), jamais réappliqué, signalé.
func TestK8s253OldModelRunningKeptAndReported(t *testing.T) {
	withHome(t)
	dir := t253Cluster(t)
	_, proj, b := t253Shared(t)
	t253Existing(t, dir, b, proj, "Running", "")
	pod, logs, calls := t253Ensure(t, dir, b, proj)
	if pod != k8sPod(proj) {
		t.Errorf("pod = %q, attendu %q", pod, k8sPod(proj))
	}
	if strings.Contains(calls, "apply") || strings.Contains(calls, "delete") {
		t.Errorf("pod en marche modifié (apply/delete) :\n%s", calls)
	}
	if !strings.Contains(logs, "avant v7.0.0") {
		t.Errorf("avertissement « Montages d'avant v7.0.0 : fermer puis rouvrir le projet pour les restreindre » absent :\n%s", logs)
	}
}

// Pod déjà au nouveau modèle : conservé sans avertissement.
func TestK8s253NewModelRunningKeptSilently(t *testing.T) {
	withHome(t)
	dir := t253Cluster(t)
	_, proj, b := t253Shared(t)
	t253Existing(t, dir, b, proj, "Running", "v2")
	_, logs, calls := t253Ensure(t, dir, b, proj)
	if strings.Contains(calls, "apply") {
		t.Errorf("pod à jour réappliqué :\n%s", calls)
	}
	if strings.Contains(logs, "avant v7.0.0") {
		t.Errorf("avertissement à tort :\n%s", logs)
	}
}

// Pod ARRÊTÉ à l'ancien modèle : recréé au démarrage suivant, au nouveau modèle (cl.mounts=v2, montages restreints).
func TestK8s253OldModelStoppedRecreated(t *testing.T) {
	withHome(t)
	dir := t253Cluster(t)
	root, proj, b := t253Shared(t)
	t253Existing(t, dir, b, proj, "", "") // phase vide : pod absent (Pending)
	_, _, calls := t253Ensure(t, dir, b, proj)
	if !strings.Contains(calls, "apply") {
		t.Fatalf("pod arrêté non recréé :\n%s", calls)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "applied.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	ann := m["metadata"].(map[string]any)["annotations"].(map[string]any)
	if ann["cl.mounts"] != "v2" {
		t.Errorf("manifeste appliqué sans cl.mounts=v2 : %v", ann)
	}
	_, mounts, _ := t253Parts(m)
	for _, mt := range mounts {
		if mt.Path == root {
			t.Errorf("racine montée dans le pod recréé : %+v", mt)
		}
	}
}
