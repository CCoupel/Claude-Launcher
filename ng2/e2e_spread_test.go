package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Bout en bout k8s (#60, CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>) :
// trois projets répartis sur les nodes ; placement changé jamais appliqué à un
// pod en marche, appliqué à son redémarrage.
func TestE2EK8sSpread(t *testing.T) {
	spec := os.Getenv("CL_E2E_K8S")
	if spec == "" {
		t.Skip("CL_E2E_K8S=… pour lancer ce test")
	}
	f := strings.Split(spec, ",")
	if len(f) != 4 {
		t.Fatal("CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>")
	}
	h := filepath.Join(f[2], ".cl-e2e-spread-"+strconv.Itoa(os.Getpid()))
	t.Setenv("HOME", h)
	t.Cleanup(func() { os.RemoveAll(h) })
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	b := BackendConf{Name: "e2e-spread", Type: "k8s", Context: f[0], Namespace: f[1], Volumes: map[string]string{f[2]: f[3]}, Shared: []string{f[2]}}
	e := Engine{b}

	var projs []string
	for i := 1; i <= 3; i++ {
		p := filepath.Join(h, "PROJETS", "TEST", "spread-"+strconv.Itoa(i))
		_ = os.MkdirAll(p, 0o755)
		writeEnvFile(t, p, "image: docker.io/library/debian:12\n")
		projs = append(projs, p)
		defer e.kcmd("delete", "statefulset", k8sName(p), "--wait=false").Run()
	}
	env, _ := loadEnv(projs[0])

	// les trois en parallèle, comme trois ouvertures rapprochées
	var wg sync.WaitGroup
	errs := make([]error, len(projs))
	for i, p := range projs {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			var out bytes.Buffer
			_, errs[i] = e.ensure(p, env, &out)
		}(i, p)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("ensure %s : %v", projs[i], err)
		}
	}
	nodes := e.k8sNodes()
	count := map[string]int{}
	for _, p := range projs {
		count[nodes[k8sPod(p)]]++
	}
	t.Logf("répartition : %v", count)
	if len(count) < 2 {
		t.Errorf("les trois pods sur le même node (%v) : spread sans effet, ou un seul node disponible", count)
	}
	for n, c := range count {
		if n == "" || c > 2 {
			t.Errorf("répartition déséquilibrée : %v", count)
		}
	}

	// placement changé : le pod en marche n'est pas recréé…
	p := projs[0]
	uid := func() string {
		out, _ := e.kcmd("get", "pod", k8sPod(p), "-o", "jsonpath={.metadata.uid}").Output()
		return strings.TrimSpace(string(out))
	}
	before := uid()
	no := false
	e2 := Engine{b}
	e2.b.Spread = &no
	var out bytes.Buffer
	if _, err := e2.ensure(p, env, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "appliqué au prochain démarrage") || uid() != before {
		t.Fatalf("pod en marche touché : %q, uid %s → %s", out.String(), before, uid())
	}
	// … il l'est à son redémarrage (pod arrêté : 0 réplique, puis ouverture)
	if err := e2.stop(p, k8sPod(p)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60 && uid() != ""; i++ {
		time.Sleep(time.Second)
	}
	out.Reset()
	if _, err := e2.ensure(p, env, &out); err != nil {
		t.Fatalf("redémarrage : %v\n%s", err, out.String())
	}
	got, _ := e.kcmd("get", "pod", k8sPod(p), "-o", `jsonpath={.metadata.annotations.cl\.sched}|{.spec.topologySpreadConstraints}`).Output()
	if s := string(got); !strings.HasPrefix(s, e2.b.schedKey()+"|") || strings.Contains(s, "kubernetes.io/hostname") {
		t.Errorf("nouveau placement non appliqué au redémarrage : %s", s)
	}
}
