package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ── Backend Kubernetes (#45) ──────────────────────────────────────────────
//
// Un projet = un StatefulSet (1 réplique) « clx-<projet>-<empreinte> », dont le
// pod « …-0 » tourne en root (bootstrap) ; les commandes utilisateur passent par
// su vers l'utilisateur créé au bootstrap (kubectl exec n'a pas de -u). Le
// stockage partagé est fait de PVC ReadWriteMany, chacun monté au chemin de sa
// racine partagée (« volumes » : racine → PVC), donc aux mêmes chemins que sur la
// machine du launcher. Labels cl.launcher / cl.backend / cl.ctr (listing),
// annotations cl.project / cl.image (le chemin n'est pas une valeur de label).

func (b BackendConf) k8s() bool { return b.Type == "k8s" }

func (e Engine) kubectl(args ...string) []string {
	out := []string{"kubectl"}
	if e.b.Context != "" {
		out = append(out, "--context", e.b.Context)
	}
	ns := e.b.Namespace
	if ns == "" {
		ns = "default"
	}
	return append(append(out, "-n", ns), args...)
}

func (e Engine) kcmd(args ...string) *exec.Cmd {
	a := e.kubectl(args...)
	return exec.Command(a[0], a[1:]...)
}

// k8sName : nom du StatefulSet (et préfixe du pod) d'un projet.
func k8sName(proj string) string { return "clx-" + strings.TrimPrefix(ctrName(proj), "cl-") }

func k8sPod(proj string) string { return k8sName(proj) + "-0" }

// ctrRef : ce que vise « exec » pour un projet : son pod (k8s) ou son conteneur.
func ctrRef(b BackendConf, proj string) string {
	if b.k8s() {
		return k8sPod(proj)
	}
	return ctrName(proj)
}

// k8sExecArgv : kubectl exec ; en utilisateur, via su (résolu par son uid).
func (e Engine) k8sExecArgv(pod string, root, tty bool, argv ...string) []string {
	a := []string{"exec", "-i"}
	if tty {
		a = append(a, "-t")
	}
	a = append(a, pod, "-c", "main", "--")
	if root {
		return e.kubectl(append(a, argv...)...)
	}
	q := make([]string, len(argv))
	for i, s := range argv {
		q[i] = shq(s)
	}
	cmd := "exec env LANG=C.UTF-8"
	if tty {
		cmd += " TERM=tmux-256color"
	}
	cmd += " " + strings.Join(q, " ")
	user := fmt.Sprintf(`exec su -s /bin/sh "$(getent passwd %d | cut -d: -f1)" -c "$1"`, os.Getuid())
	return e.kubectl(append(a, "sh", "-c", user, "sh", cmd)...)
}

// k8sState : phase du pod ("Running"…) et image en place ; "" si absent.
func (e Engine) k8sState(proj string) (phase, image string) {
	b, err := e.kcmd("get", "statefulset", k8sName(proj), "-o", `jsonpath={.metadata.annotations.cl\.image}`).Output()
	if err != nil {
		return "", ""
	}
	image = strings.TrimSpace(string(b))
	p, _ := e.kcmd("get", "pod", k8sPod(proj), "-o", "jsonpath={.status.phase}").Output()
	phase = strings.TrimSpace(string(p))
	if phase == "" {
		phase = "Pending"
	}
	return phase, image
}

func (e Engine) k8sEnsure(proj string, env EnvSpec, log io.Writer) (string, error) {
	if err := e.checkShared(proj); err != nil {
		return "", err
	}
	if err := ensureSharedConfig(); err != nil {
		return "", err
	}
	pod := k8sPod(proj)
	phase, image := e.k8sState(proj)
	if phase == "Running" && image == env.Image {
		return pod, nil
	}
	if phase == "" {
		fmt.Fprintf(log, "  création du StatefulSet %s (%s)…\n", k8sName(proj), env.Image)
	} else if image != env.Image {
		fmt.Fprintf(log, "  image changée (%s → %s) : pod recréé\n", image, env.Image)
	}
	man, err := e.k8sManifest(proj, env)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	c := e.kcmd("apply", "-f", "-")
	c.Stdin, c.Stdout, c.Stderr = bytes.NewReader(man), &out, &out
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("kubectl apply : %v (%s)", err, strings.TrimSpace(out.String()))
	}
	fmt.Fprintf(log, "  attente du pod %s…\n", pod)
	// le pod peut ne pas exister encore à l'instant de l'apply
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		w := e.kcmd("wait", "--for=condition=Ready", "pod/"+pod, "--timeout=20s")
		if b, err := w.CombinedOutput(); err == nil {
			// un pod recréé (image changée) doit porter la nouvelle image
			if _, img := e.k8sState(proj); img == env.Image {
				return pod, nil
			}
		} else if s := string(b); strings.Contains(s, "ImagePull") || strings.Contains(s, "ErrImage") {
			return "", fmt.Errorf("image %s : %s", env.Image, strings.TrimSpace(s))
		}
		time.Sleep(2 * time.Second)
	}
	ev, _ := e.kcmd("get", "events", "--field-selector", "involvedObject.name="+pod, "-o",
		"jsonpath={range .items[*]}{.reason}: {.message}{\"\\n\"}{end}").Output()
	return "", fmt.Errorf("pod %s pas prêt après 5 min :\n%s", pod, strings.TrimSpace(string(ev)))
}

func (e Engine) k8sManifest(proj string, env EnvSpec) ([]byte, error) {
	home, _ := os.UserHomeDir()
	name := k8sName(proj)
	roots := make([]string, 0, len(e.b.Volumes))
	for r := range e.b.Volumes {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	var mounts, vols []map[string]any
	for i, r := range roots {
		v := fmt.Sprintf("shared%d", i)
		mounts = append(mounts, map[string]any{"name": v, "mountPath": r})
		vols = append(vols, map[string]any{"name": v, "persistentVolumeClaim": map[string]any{"claimName": e.b.Volumes[r]}})
	}
	labels := map[string]string{"cl.launcher": "ng2", "cl.backend": e.b.Name, "cl.ctr": name}
	ann := map[string]string{"cl.project": proj, "cl.image": env.Image}
	envs := []map[string]string{{"name": "HOME", "value": home}, {"name": "CLAUDE_CONFIG_DIR", "value": claudeDir()},
		{"name": "LANG", "value": "C.UTF-8"}, {"name": "DISABLE_AUTOUPDATER", "value": "1"}}
	m := map[string]any{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]any{"name": name, "labels": labels, "annotations": ann},
		"spec": map[string]any{
			"replicas": 1, "serviceName": name,
			"selector": map[string]any{"matchLabels": map[string]string{"cl.ctr": name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels, "annotations": ann},
				"spec": map[string]any{
					"terminationGracePeriodSeconds": 10,
					"containers": []map[string]any{{
						"name": "main", "image": env.Image, "command": []string{"sh", "-c", pid1},
						"workingDir": proj, "env": envs, "volumeMounts": mounts,
						"securityContext": map[string]any{"runAsUser": 0},
					}},
					"volumes": vols,
				},
			},
		},
	}
	return json.Marshal(m)
}

func (e Engine) k8sList() ([]Found, error) {
	b, err := e.kcmd("get", "statefulsets", "-l", "cl.launcher=ng2,cl.backend="+e.b.Name, "-o",
		`jsonpath={range .items[*]}{.metadata.name}|{.status.readyReplicas}|{.metadata.annotations.cl\.project}{"\n"}{end}`).Output()
	if err != nil {
		return nil, err
	}
	var out []Found
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) != 3 || f[2] == "" {
			continue
		}
		out = append(out, Found{Backend: e.b.Name, Name: f[0] + "-0", Project: f[2], Running: f[1] == "1"})
	}
	return out, nil
}

// k8sProblems : kubectl, contexte, namespace, droits, PVC (liés, ReadWriteMany).
func k8sProblems(b BackendConf) []problem {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return []problem{{"kubectl absent de cette machine", "l'installer (https://kubernetes.io/docs/tasks/tools/) et configurer ~/.kube/config"}}
	}
	e := Engine{b}
	ns := b.Namespace
	if ns == "" {
		ns = "default"
	}
	if out, err := runQuiet(15*time.Second, e.kubectl("get", "namespace", ns, "-o", "name")...); err != nil {
		if strings.Contains(out, "NotFound") || strings.Contains(out, "not found") {
			return []problem{{"namespace " + ns + " absent", "le créer : kubectl" + ctxArg(b) + " create namespace " + ns}}
		}
		return []problem{{"cluster injoignable (contexte « " + b.Context + " ») : " + firstLine(out, err), "vérifier kubectl config get-contexts / ~/.kube/config"}}
	}
	if out, _ := runQuiet(15*time.Second, e.kubectl("auth", "can-i", "create", "statefulsets")...); strings.TrimSpace(out) != "yes" {
		return []problem{{"droit de créer des StatefulSets refusé dans " + ns, "accorder un rôle (ex. edit) sur le namespace " + ns}}
	}
	if len(b.Volumes) == 0 {
		return []problem{{"aucun volume partagé déclaré", "« volumes: { /racine/partagée: <pvc ReadWriteMany> } » dans " + backendsPath()}}
	}
	var out []problem
	for r, pvc := range b.Volumes {
		s, err := runQuiet(15*time.Second, e.kubectl("get", "pvc", pvc, "-o", "jsonpath={.status.phase} {.spec.accessModes}")...)
		switch {
		case err != nil:
			out = append(out, problem{"PVC " + pvc + " (pour " + r + ") absent de " + ns,
				"créer un PVC ReadWriteMany sur le stockage partagé (ex. MooseFS : PV csi.moosefs.com, volumeAttributes.mfsSubDir = le dossier qui correspond à " + r + ")"})
		case !strings.HasPrefix(s, "Bound"):
			out = append(out, problem{"PVC " + pvc + " non lié (" + s + ")", "vérifier son PV : kubectl" + ctxArg(b) + " -n " + ns + " describe pvc " + pvc})
		case !strings.Contains(s, "ReadWriteMany"):
			out = append(out, problem{"PVC " + pvc + " sans ReadWriteMany (" + s + ")", "le recréer en ReadWriteMany : plusieurs pods et la machine du launcher y accèdent"})
		}
	}
	return out
}

func ctxArg(b BackendConf) string {
	if b.Context != "" {
		return " --context " + b.Context
	}
	return ""
}
