package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// podSpecOf : spec du pod et conteneur principal du manifeste généré.
func podSpecOf(t *testing.T, b BackendConf) (spec, ctr, ann map[string]any) {
	t.Helper()
	raw, err := Engine{b}.k8sManifest("/p/proj", EnvSpec{Image: "debian:12"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	tpl := m["spec"].(map[string]any)["template"].(map[string]any)
	spec = tpl["spec"].(map[string]any)
	ctr = spec["containers"].([]any)[0].(map[string]any)
	ann = tpl["metadata"].(map[string]any)["annotations"].(map[string]any)
	return
}

func TestK8sSpread(t *testing.T) {
	// défaut : répartition par node, sans bloquer, limitée aux pods NG2 du backend
	spec, _, ann := podSpecOf(t, BackendConf{Name: "lab", Type: "k8s"})
	tsc, _ := spec["topologySpreadConstraints"].([]any)
	if len(tsc) != 1 {
		t.Fatalf("spread par défaut : %v", spec["topologySpreadConstraints"])
	}
	c := tsc[0].(map[string]any)
	sel := c["labelSelector"].(map[string]any)["matchLabels"].(map[string]any)
	if c["topologyKey"] != "kubernetes.io/hostname" || c["whenUnsatisfiable"] != "ScheduleAnyway" || c["maxSkew"].(float64) != 1 ||
		sel["cl.launcher"] != "ng2" || sel["cl.backend"] != "lab" {
		t.Errorf("contrainte : %v", c)
	}
	if ann["cl.sched"] == "" {
		t.Error("annotation cl.sched absente")
	}

	// spread: false : aucune contrainte
	no := false
	spec, _, ann2 := podSpecOf(t, BackendConf{Name: "lab", Type: "k8s", Spread: &no})
	if _, ok := spec["topologySpreadConstraints"]; ok {
		t.Errorf("spread désactivé : %v", spec["topologySpreadConstraints"])
	}
	if ann2["cl.sched"] == ann["cl.sched"] {
		t.Error("cl.sched identique avec et sans spread")
	}
}

func TestK8sPodKeys(t *testing.T) {
	b := BackendConf{Name: "lab", Type: "k8s", Pod: map[string]any{
		"nodeSelector": map[string]any{"node-role/claude": "true"},
		"tolerations":  []any{map[string]any{"key": "dedie", "operator": "Exists"}},
		"resources":    map[string]any{"limits": map[string]any{"memory": "4Gi"}},
		"topologySpreadConstraints": []any{map[string]any{"maxSkew": 1, "topologyKey": "topology.kubernetes.io/zone",
			"whenUnsatisfiable": "ScheduleAnyway"}},
	}}
	spec, ctr, _ := podSpecOf(t, b)
	if spec["nodeSelector"].(map[string]any)["node-role/claude"] != "true" || len(spec["tolerations"].([]any)) != 1 {
		t.Errorf("clés du pod : %v", spec)
	}
	if _, ok := spec["resources"]; ok {
		t.Error("resources posé sur le pod au lieu du conteneur")
	}
	if ctr["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "4Gi" {
		t.Errorf("resources du conteneur : %v", ctr["resources"])
	}
	if tsc := spec["topologySpreadConstraints"].([]any); len(tsc) != 2 {
		t.Errorf("contraintes : spread + la sienne attendues, %d", len(tsc))
	}
	// NG2 garde la main sur ses conteneurs et volumes
	if ctr["name"] != "main" || spec["volumes"] == nil && len(spec) == 0 {
		t.Errorf("conteneur : %v", ctr)
	}
}

func TestValidatePod(t *testing.T) {
	if err := validatePod(map[string]any{"affinity": map[string]any{}, "resources": map[string]any{}}); err != nil {
		t.Errorf("clés valides refusées : %v", err)
	}
	for _, k := range []string{"containers", "volumes", "initContainers", "restartPolicy"} {
		if err := validatePod(map[string]any{k: 1}); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s accepté : %v", k, err)
		}
	}
	// au chargement de backends.yaml, avec le nom du backend
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	_ = os.WriteFile(filepath.Join(d, "backends.yaml"), []byte("backends:\n  lab: {type: k8s, context: c, namespace: n, pod: {volumes: []}}\n"), 0o644)
	if _, err := loadBackends(); err == nil || !strings.Contains(err.Error(), `"lab"`) || !strings.Contains(err.Error(), "volumes") {
		t.Errorf("backends.yaml : %v", err)
	}
	_ = os.WriteFile(filepath.Join(d, "backends.yaml"), []byte("backends:\n  lab: {type: k8s, context: c, namespace: n, spread: false, pod: {nodeSelector: {a: b}}}\n"), 0o644)
	bs, err := loadBackends()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if b.Name == "lab" && (b.spread() || b.placementText() != "répartition par node : non ; pod : nodeSelector") {
			t.Errorf("chargement : spread=%v %q", b.spread(), b.placementText())
		}
	}
}
