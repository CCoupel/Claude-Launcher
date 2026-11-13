package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// v5.6 en réel : un StatefulSet « clx-<projet>-<empreinte> » en marche est
// conservé ; arrêté, il est supprimé et recréé « <catégorie>-<projet> ».
// CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>.
func TestE2ELegacyK8sMigration(t *testing.T) {
	spec := os.Getenv("CL_E2E_K8S")
	if spec == "" {
		t.Skip("CL_E2E_K8S=… pour lancer ce test")
	}
	f := strings.Split(spec, ",")
	if len(f) != 4 {
		t.Fatal("CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>")
	}
	withHome(t)
	h := filepath.Join(f[2], ".cl-e2e-name-"+strconv.Itoa(os.Getpid()))
	t.Setenv("HOME", h)
	t.Cleanup(func() { os.RemoveAll(h) })
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	e := Engine{BackendConf{Name: "e2e", Type: "k8s", Context: f[0], Namespace: f[1], Volumes: map[string]string{f[2]: f[3]}, Shared: []string{f[2]}}}
	proj := filepath.Join(h, "PROJETS", "TEST", "demo-name")
	_ = os.MkdirAll(proj, 0o755)
	env := EnvSpec{Image: "docker.io/library/debian:12"}
	old := legacyK8sName(proj)
	defer e.kcmd("delete", "statefulset", old, "--wait=false").Run()
	defer e.kcmd("delete", "statefulset", k8sName(proj), "--wait=false").Run()
	// ancien StatefulSet : le manifeste actuel sous l'ancien nom
	man, err := e.k8sManifest(proj, env)
	if err != nil {
		t.Fatal(err)
	}
	man = bytes.ReplaceAll(man, []byte(`"`+k8sName(proj)+`"`), []byte(`"`+old+`"`))
	c := e.kcmd("apply", "-f", "-")
	c.Stdin = bytes.NewReader(man)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("apply ancien : %v %s", err, out)
	}
	if out, err := e.kcmd("wait", "--for=condition=Ready", "pod/"+old+"-0", "--timeout=180s").CombinedOutput(); err != nil {
		t.Fatalf("ancien pod : %v %s", err, out)
	}
	if r := CtrRef(e.B, proj); r != old+"-0" {
		t.Errorf("ctrRef pendant la transition : %s", r)
	}
	var log bytes.Buffer
	if n, err := e.Ensure(proj, env, &log); err != nil || n != old+"-0" {
		t.Fatalf("en marche : ancien pod attendu : %q %v %s", n, err, log.String())
	}
	if err := e.Stop(proj, old+"-0"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	log.Reset()
	n, err := e.Ensure(proj, env, &log)
	if err != nil || n != "test-demo-name-0" || e.k8sHas(old) {
		t.Fatalf("arrêté : migration attendue : %q %v ancien présent=%v\n%s", n, err, e.k8sHas(old), log.String())
	}
	var out bytes.Buffer
	if err := e.Exec(n, true, nil, &out, "hostname"); err != nil || strings.TrimSpace(out.String()) != "test-demo-name-0" {
		t.Errorf("hostname : %q %v", out.String(), err)
	}
	if r := CtrRef(e.B, proj); r != "test-demo-name-0" {
		t.Errorf("ctrRef après migration : %s", r)
	}
}
