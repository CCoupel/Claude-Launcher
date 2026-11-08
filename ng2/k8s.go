package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// k8sName : nom du StatefulSet d'un projet, « <catégorie>-<projet> » ; son pod
// (et hostname, imposé par le StatefulSet) : « <catégorie>-<projet>-0 ».
func k8sName(proj string) string { return ctrName(proj) }

func k8sPod(proj string) string { return k8sName(proj) + "-0" }

// legacyK8sName : StatefulSet d'avant v5.6 (« clx-<projet>-<empreinte> »).
func legacyK8sName(proj string) string {
	return "clx-" + strings.TrimPrefix(legacyCtrName(proj), "cl-")
}

// k8sHas : le StatefulSet existe.
func (e Engine) k8sHas(name string) bool {
	return e.kcmd("get", "statefulset", name, "-o", "name").Run() == nil
}

// ctrRef : ce que vise « exec » pour un projet : son pod (k8s) ou son conteneur.
// k8s : un StatefulSet d'avant v5.6 encore là garde son nom jusqu'à sa migration.
func ctrRef(b BackendConf, proj string) string {
	if b.k8s() {
		if e := (Engine{b}); !e.k8sHas(k8sName(proj)) && e.k8sHas(legacyK8sName(proj)) {
			return legacyK8sName(proj) + "-0"
		}
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
	return e.kubectl(append(a, "sh", "-c", k8sAsUser(os.Getuid(), os.Getgid(), tty), "sh", cmd)...)
}

// k8sAsUser : script sh qui exécute "$1" sous l'utilisateur uid (kubectl exec
// n'a pas de -u). Avec un terminal, setpriv plutôt que su : « su -c » ouvre une
// nouvelle session sans terminal de contrôle, et le client tmux ne reçoit alors
// plus SIGWINCH (fenêtre du projet jamais redimensionnée). su reste le repli
// (image sans setpriv) ; l'environnement posé est celui que su poserait.
func k8sAsUser(uid, gid int, tty bool) string {
	su := fmt.Sprintf(`exec su -s /bin/sh "$(getent passwd %d | cut -d: -f1)" -c "$1"`, uid)
	if !tty {
		return su
	}
	return fmt.Sprintf(`if command -v setpriv >/dev/null 2>&1; then `+
		`u=$(getent passwd %d | cut -d: -f1); h=$(getent passwd %d | cut -d: -f6); `+
		`exec setpriv --reuid=%d --regid=%d --init-groups env HOME="$h" USER="$u" LOGNAME="$u" SHELL=/bin/sh sh -c "$1"; fi; %s`,
		uid, uid, uid, gid, su)
}

// k8sState : phase du pod ("Running"…) et image en place ; "" si absent.
func (e Engine) k8sState(proj string) (phase, image string) {
	phase, image, _, _, _ = e.k8sStateFull(k8sName(proj))
	return phase, image
}

// k8sStateFull : phase du pod du StatefulSet name, image, empreintes des
// dossiers « local » et du placement (cl.sched, #60), projet (cl.project).
func (e Engine) k8sStateFull(name string) (phase, image, local, sched, project string) {
	b, err := e.kcmd("get", "statefulset", name, "-o", `jsonpath={.metadata.annotations.cl\.image}|{.metadata.annotations.cl\.local}|{.metadata.annotations.cl\.sched}|{.metadata.annotations.cl\.project}`).Output()
	if err != nil {
		return "", "", "", "", ""
	}
	f := strings.SplitN(strings.TrimSpace(string(b)), "|", 4)
	for len(f) < 4 {
		f = append(f, "")
	}
	image, local, sched, project = f[0], f[1], f[2], f[3]
	p, _ := e.kcmd("get", "pod", name+"-0", "-o", "jsonpath={.status.phase}").Output()
	phase = strings.TrimSpace(string(p))
	if phase == "" {
		phase = "Pending"
	}
	return phase, image, local, sched, project
}

// k8sPodCurrent : le pod porte l'image et les dossiers « local » attendus, et
// n'est pas en cours de suppression.
func (e Engine) k8sPodCurrent(pod string, env EnvSpec) bool {
	b, err := e.kcmd("get", "pod", pod, "-o",
		`jsonpath={.metadata.annotations.cl\.image}|{.metadata.annotations.cl\.local}|{.metadata.annotations.cl\.sched}|{.metadata.deletionTimestamp}`).Output()
	if err != nil {
		return false
	}
	f := strings.SplitN(strings.TrimSpace(string(b)), "|", 4)
	return len(f) == 4 && f[0] == env.Image && f[1] == env.localKey() && f[2] == e.b.schedKey() && f[3] == ""
}

func (e Engine) k8sEnsure(proj string, env EnvSpec, log io.Writer) (string, error) {
	if err := e.checkShared(proj); err != nil {
		return "", err
	}
	if err := ensureSharedConfig(); err != nil {
		return "", err
	}
	pod := k8sPod(proj)
	phase, image, local, sched, owner := e.k8sStateFull(k8sName(proj))
	if phase == "" {
		// StatefulSet d'avant v5.6 : gardé tant que son pod tourne à jour (sa
		// session continue), sinon supprimé et recréé sous le nouveau nom
		old := legacyK8sName(proj)
		if lp, li, ll, _, _ := e.k8sStateFull(old); lp == "Running" && li == env.Image && ll == env.localKey() {
			fmt.Fprintf(log, "  StatefulSet %s (ancien nom) conservé : renommé %s à sa prochaine ouverture après fermeture\n", old, k8sName(proj))
			return old + "-0", nil
		} else if lp != "" {
			fmt.Fprintf(log, "  ancien StatefulSet %s supprimé : recréé sous le nom %s\n", old, k8sName(proj))
			_ = e.kcmd("delete", "statefulset", old, "--wait=true", "--timeout=60s").Run()
		}
	}
	if phase != "" && owner != "" && owner != proj {
		return "", fmt.Errorf("StatefulSet %s déjà pris par le projet %s : renommer la catégorie ou le projet", k8sName(proj), owner)
	}
	if phase == "Running" && image == env.Image && local == env.localKey() {
		// placement changé (#60) : jamais appliqué à un pod en marche (le recréer
		// couperait sa session) ; il le sera au prochain démarrage du pod
		if sched != e.b.schedKey() {
			fmt.Fprintf(log, "  placement modifié dans backends.yaml : appliqué au prochain démarrage du pod\n")
		}
		return pod, nil
	}
	if phase == "" {
		fmt.Fprintf(log, "  création du StatefulSet %s (%s)…\n", k8sName(proj), env.Image)
	} else if phase == "Pending" && image == env.Image && local == env.localKey() {
		fmt.Fprintf(log, "  pod arrêté : remise à 1 réplique…\n")
	} else if image != env.Image {
		fmt.Fprintf(log, "  image changée (%s → %s) : pod recréé\n", image, env.Image)
	} else if local != env.localKey() {
		fmt.Fprintf(log, "  dossiers « local » changés : pod recréé\n")
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
			// un pod recréé (image ou dossiers « local » changés) : le pod prêt doit
			// être le NOUVEAU (ses annotations), pas l'ancien encore en arrêt
			if e.k8sPodCurrent(pod, env) {
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
	// dossiers « local » : emptyDir du pod, monté par-dessus le dossier du projet
	for i, p := range env.localPaths(proj) {
		v := fmt.Sprintf("local%d", i)
		mounts = append(mounts, map[string]any{"name": v, "mountPath": p})
		vols = append(vols, map[string]any{"name": v, "emptyDir": map[string]any{}})
	}
	labels := map[string]string{"cl.launcher": "ng2", "cl.backend": e.b.Name, "cl.ctr": name}
	ann := map[string]string{"cl.project": proj, "cl.image": env.Image, "cl.local": env.localKey(), "cl.sched": e.b.schedKey()}
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
					"containers": []map[string]any{e.b.withResources(map[string]any{
						"name": "main", "image": env.Image, "command": []string{"sh", "-c", pid1},
						"workingDir": proj, "env": envs, "volumeMounts": mounts,
						"securityContext": map[string]any{"runAsUser": 0},
					})},
					"volumes": vols,
				},
			},
		},
	}
	e.b.placePod(m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any))
	return json.Marshal(m)
}

// ── Placement des pods (#60) ──────────────────────────────────────────────
//
// spread (défaut : oui) : topologySpreadConstraints par node sur les pods NG2 du
// backend, en ScheduleAnyway — jamais de pod bloqué en Pending quand il y a plus
// de projets que de nodes, et une répartition équilibrée. pod : clés ajoutées
// au spec du pod (resources : au conteneur). Leur empreinte (cl.sched) n'est
// appliquée qu'au démarrage d'un pod arrêté, jamais à une session vivante.

// podKeys : clés admises sous « pod » (le reste du spec est géré par NG2).
var podKeys = map[string]bool{
	"nodeSelector": true, "affinity": true, "tolerations": true, "topologySpreadConstraints": true,
	"priorityClassName": true, "serviceAccountName": true, "automountServiceAccountToken": true,
	"runtimeClassName": true, "schedulerName": true, "nodeName": true, "imagePullSecrets": true,
	"dnsPolicy": true, "dnsConfig": true, "hostAliases": true, "terminationGracePeriodSeconds": true,
	"securityContext": true, "resources": true,
}

func validatePod(pod map[string]any) error {
	var bad []string
	for k := range pod {
		if !podKeys[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		keys := make([]string, 0, len(podKeys))
		for k := range podKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("pod : clé(s) %s non admise(s) (conteneurs et volumes sont gérés par NG2) ; admises : %s",
			strings.Join(bad, ", "), strings.Join(keys, ", "))
	}
	if _, err := json.Marshal(pod); err != nil {
		return fmt.Errorf("pod : %w", err)
	}
	return nil
}

func (b BackendConf) spread() bool { return b.Spread == nil || *b.Spread }

// spreadConstraint : un pod NG2 de ce backend de plus par node au plus que sur
// le node le moins chargé, sans jamais bloquer.
func (b BackendConf) spreadConstraint() map[string]any {
	return map[string]any{
		"maxSkew": 1, "topologyKey": "kubernetes.io/hostname", "whenUnsatisfiable": "ScheduleAnyway",
		"labelSelector": map[string]any{"matchLabels": map[string]string{"cl.launcher": "ng2", "cl.backend": b.Name}},
	}
}

// placePod : spread et clés « pod » (hors resources) ajoutées au spec du pod.
func (b BackendConf) placePod(spec map[string]any) {
	var tsc []any
	if b.spread() {
		tsc = append(tsc, b.spreadConstraint())
	}
	for k, v := range b.Pod {
		switch k {
		case "resources":
		case "topologySpreadConstraints": // les siennes s'ajoutent à celle de spread
			if l, ok := v.([]any); ok {
				tsc = append(tsc, l...)
			}
		default:
			spec[k] = v
		}
	}
	if len(tsc) > 0 {
		spec["topologySpreadConstraints"] = tsc
	}
}

// withResources : « pod.resources » appliqué au conteneur principal.
func (b BackendConf) withResources(c map[string]any) map[string]any {
	if r, ok := b.Pod["resources"]; ok {
		c["resources"] = r
	}
	return c
}

// schedKey : empreinte du placement (annotation cl.sched).
func (b BackendConf) schedKey() string {
	j, _ := json.Marshal(map[string]any{"spread": b.spread(), "pod": b.Pod})
	h := sha256.Sum256(j)
	return hex.EncodeToString(h[:])[:12]
}

// placementText : placement en clair (claude-launcher-ng2 backends).
func (b BackendConf) placementText() string {
	t := "répartition par node : non"
	if b.spread() {
		t = "répartition par node : oui"
	}
	if len(b.Pod) > 0 {
		keys := make([]string, 0, len(b.Pod))
		for k := range b.Pod {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t += " ; pod : " + strings.Join(keys, ", ")
	}
	return t
}

func (e Engine) k8sList() ([]Found, error) {
	b, err := e.kcmd("get", "statefulsets", "-l", "cl.launcher=ng2,cl.backend="+e.b.Name, "-o",
		`jsonpath={range .items[*]}{.metadata.name}|{.spec.replicas}|{.status.readyReplicas}|{.metadata.annotations.cl\.project}{"\n"}{end}`).Output()
	if err != nil {
		return nil, err
	}
	var out []Found
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		// nom|réplicas voulues|prêtes|projet ; 0 réplique = arrêté (fermeture #50)
		f := strings.SplitN(l, "|", 4)
		if len(f) != 4 || f[3] == "" {
			continue
		}
		out = append(out, Found{Backend: e.b.Name, Name: f[0] + "-0", Project: f[3], Running: f[2] == "1",
			Failed: f[1] == "1" && f[2] != "1"})
	}
	return out, nil
}

// k8sNodes : node de chaque pod NG2 du backend (pour « backends »).
func (e Engine) k8sNodes() map[string]string {
	out := map[string]string{}
	b, err := e.kcmd("get", "pods", "-l", "cl.launcher=ng2,cl.backend="+e.b.Name, "-o",
		`jsonpath={range .items[*]}{.metadata.name}|{.spec.nodeName}{"\n"}{end}`).Output()
	if err != nil {
		return out
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if p, n, ok := strings.Cut(l, "|"); ok && n != "" {
			out[p] = n
		}
	}
	return out
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
