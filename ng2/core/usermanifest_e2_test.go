package core

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// LOT-3.2 Batch 0 (#275, E2) — manifeste du StatefulSet et argv docker/podman d'un projet d'UTILISATEUR (contracts/user-backends.md
// §4, §5, §5 bis : liste blanche fermée, jamais de champ libre). Points d'entrée existants : Engine.k8sManifest(proj, env) et
// Engine.runArgs(name, proj, env) sur un backend CHARGÉ d'un Space d'utilisateur (b.user) ; proj = dossier du projet vu du conteneur
// (/space/PROJETS/<catégorie>/<projet>). Si dev-backend crée d'autres points d'entrée, le dire : les tests suivent. Écrits avant le code.

const (
	e2Proj = "/space/PROJETS/cat/proj"
	e2UID  = "github-9"
)

func e2K8s(t *testing.T) BackendConf {
	t.Helper()
	s15Setup(t)
	sp := s15Space(t, e2UID, "backends:\n  k:\n    type: k8s\n    namespace: ns-u\n    pvc: projets\n    kubeconfig: true\n")
	s15Vault(t, sp, "k", s15Kubeconfig("CANARY-e2"))
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Fatalf("backend k8s d'utilisateur non chargé : %v", err)
	}
	return bs[0]
}

func e2Docker(t *testing.T, typ string) BackendConf {
	t.Helper()
	s15Setup(t)
	sp := s15Space(t, e2UID, "backends:\n  d:\n    type: "+typ+"\n    host: tcp://93.184.216.34:2376\n    volume: vol\n")
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Fatalf("backend %s d'utilisateur non chargé : %v", typ, err)
	}
	return bs[0]
}

// walk : toutes les clés d'un document JSON (chemin pointé) et leur valeur.
func walk(v any, path string, f func(path string, val any)) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			p := path + "." + k
			f(p, c)
			walk(c, p, f)
		}
	case []any:
		for i, c := range x {
			walk(c, path+"[]", f)
			_ = i
		}
	}
}

func e2Manifest(t *testing.T, b BackendConf, image string) (map[string]any, error) {
	t.Helper()
	raw, err := Engine{b}.k8sManifest(e2Proj, EnvSpec{Image: image})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m, nil
}

func TestE2K8sManifesteDUnProjetUtilisateur(t *testing.T) {
	b := e2K8s(t)
	m, err := e2Manifest(t, b, "debian:12")
	if err != nil {
		t.Fatalf("manifeste refusé : %v", err)
	}
	labels := m["metadata"].(map[string]any)["labels"].(map[string]any)
	for k, want := range map[string]string{"cl.launcher": "ng2", "cl.backend": "k", "cl.user": e2UID, "cl.category": "cat", "cl.project": "proj"} {
		if labels[k] != want {
			t.Errorf("label %s = %v, attendu %q", k, labels[k], want)
		}
	}
	tpl := m["spec"].(map[string]any)["template"].(map[string]any)
	spec := tpl["spec"].(map[string]any)
	if spec["automountServiceAccountToken"] != false {
		t.Errorf("automountServiceAccountToken = %v (false attendu)", spec["automountServiceAccountToken"])
	}
	ctr := spec["containers"].([]any)[0].(map[string]any)
	if ctr["image"] != "debian:12" || ctr["workingDir"] != e2Proj {
		t.Errorf("image / workingDir : %v %v", ctr["image"], ctr["workingDir"])
	}
	// stockage : le PVC DÉCLARÉ, monté en subPath (dossier du projet + .claude), jamais la racine ; aucun autre volume utilisateur
	vols := spec["volumes"].([]any)
	var claims []string
	for _, v := range vols {
		vm := v.(map[string]any)
		if pvc, ok := vm["persistentVolumeClaim"].(map[string]any); ok {
			claims = append(claims, pvc["claimName"].(string))
		} else if _, empty := vm["emptyDir"]; !empty {
			t.Errorf("volume non prévu : %v", vm)
		}
	}
	if len(claims) == 0 || claims[0] != "projets" || len(claims) > 1 {
		t.Errorf("PVC montés : %v (« projets » seulement)", claims)
	}
	subs := map[string]bool{}
	for _, x := range ctr["volumeMounts"].([]any) {
		if s, _ := x.(map[string]any)["subPath"].(string); s != "" {
			subs[s] = true
		} else if x.(map[string]any)["name"] != nil && strings.HasPrefix(x.(map[string]any)["name"].(string), "shared") {
			t.Errorf("racine du volume montée entière : %v", x)
		}
	}
	if !subs["PROJETS/cat/proj"] || !subs[".claude"] {
		t.Errorf("subPath montés : %v (PROJETS/cat/proj et .claude attendus)", subs)
	}
}

// Liste blanche fermée : rien de dangereux dans le manifeste, quoi que contienne la déclaration du backend.
func TestE2K8sJamaisDeChampDangereux(t *testing.T) {
	b := e2K8s(t)
	m, err := e2Manifest(t, b, "debian:12")
	if err != nil {
		t.Fatalf("manifeste refusé : %v", err)
	}
	forbidden := map[string]bool{"hostPath": true, "hostNetwork": true, "hostPID": true, "hostIPC": true, "privileged": true, "serviceAccountName": true,
		"serviceAccount": true, "runtimeClassName": true, "hostPort": true, "ports": true, "nodeName": true, "shareProcessNamespace": true,
		"priorityClassName": true, "schedulerName": true, "dnsConfig": true, "dnsPolicy": true, "hostAliases": true, "initContainers": true,
		"ephemeralContainers": true, "imagePullSecrets": true, "hostUsers": true, "envFrom": true, "args": true, "lifecycle": true, "volumeDevices": true}
	walk(m, "", func(p string, v any) {
		k := p[strings.LastIndex(p, ".")+1:]
		if forbidden[k] {
			if b, ok := v.(bool); ok && !b {
				return // « privileged: false » explicite est sans risque
			}
			t.Errorf("clé interdite dans le manifeste d'un utilisateur : %s = %v", p, v)
		}
	})
	// securityContext fixé par le launcher : aucune élévation
	ctr := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	if sc, _ := ctr["securityContext"].(map[string]any); sc == nil || sc["privileged"] == true || sc["allowPrivilegeEscalation"] == true {
		t.Errorf("securityContext du conteneur : %v", sc)
	}
	// la commande est celle du launcher, identique à celle d'un projet de l'admin
	cmd, _ := ctr["command"].([]any)
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != pid1 {
		t.Errorf("commande du conteneur : %v", cmd)
	}
	// ni PVC ni volume créés par le launcher
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "volumeClaimTemplates") {
		t.Error("volumeClaimTemplates : le launcher ne crée aucun PVC")
	}
}

func TestE2ImagesInvalidesRefusees(t *testing.T) {
	b := e2K8s(t)
	for _, bad := range []string{"debian:12; touch /tmp/x", "debian:12 --privileged", "--privileged", "-v /:/host", "debian:12\nevil", "deb ian", "$(id)", "`id`",
		"debian:12|sh", "a&&b", "", " ", "../etc", "debian:12'x", `debian:12"x`} {
		if _, err := e2Manifest(t, b, bad); err == nil {
			t.Errorf("image %q acceptée pour un utilisateur", bad)
		}
	}
	for _, ok := range []string{"debian:12", "ghcr.io/ccoupel/claude-base:1.2.3", "registry.example.org:5000/team/app:v1", "ubuntu@sha256:" + strings.Repeat("a", 64)} {
		if _, err := e2Manifest(t, b, ok); err != nil {
			t.Errorf("image %q refusée : %v", ok, err)
		}
	}
}

// ── docker / podman ────────────────────────────────────────────────────────────────────────────────────

var dockerForbidden = []string{"--privileged", "-p", "-P", "--publish", "--publish-all", "--network", "--net", "--pid", "--ipc", "--uts", "--cap-add",
	"--volumes-from", "--device", "--userns", "--cgroupns", "--add-host", "--dns", "--dns-search", "--dns-option", "--mount", "--sysctl", "--ulimit", "--init",
	"--entrypoint", "--restart", "--tmpfs", "--env-file", "--label-file", "--group-add", "--pids-limit-unlimited", "--ipc=host", "--link", "--mac-address", "--ip", "--ip6"}

func e2Args(t *testing.T, b BackendConf, image string) ([]string, error) {
	t.Helper()
	return Engine{b}.runArgs("cl-ctr", e2Proj, EnvSpec{Image: image})
}

func TestE2DockerArgvDUnProjetUtilisateur(t *testing.T) {
	for _, typ := range []string{"docker", "podman"} {
		b := e2Docker(t, typ)
		args, err := e2Args(t, b, "debian:12")
		if err != nil {
			t.Errorf("%s : argv refusé : %v", typ, err)
			continue
		}
		if len(args) == 0 || (args[0] != "create" && args[0] != "run") {
			t.Errorf("%s : premier argument %v", typ, args)
		}
		j := " " + strings.Join(args, " ") + " "
		for _, want := range []string{" -v vol:/space ", " cl.user=" + e2UID + " ", " cl.category=cat ", " cl.project=proj ", " cl.backend=d ", " cl.launcher=ng2 "} {
			if !strings.Contains(j, want) {
				t.Errorf("%s : %q absent de %v", typ, strings.TrimSpace(want), args)
			}
		}
		if !strings.Contains(j, " --workdir "+e2Proj+" ") && !strings.Contains(j, " -w "+e2Proj+" ") {
			t.Errorf("%s : dossier de travail %s absent : %v", typ, e2Proj, args)
		}
		for _, f := range dockerForbidden {
			for _, a := range args {
				if a == f || strings.HasPrefix(a, f+"=") {
					t.Errorf("%s : option interdite %s dans %v", typ, f, args)
				}
			}
		}
		// jamais de montage de l'hôte : seul le volume nommé, monté sur /space
		for i, a := range args {
			if (a == "-v" || a == "--volume") && i+1 < len(args) && args[i+1] != "vol:/space" {
				t.Errorf("%s : volume ou montage hôte inattendu %q", typ, args[i+1])
			}
			if strings.Contains(a, "type=bind") || strings.HasPrefix(a, "/") && a != e2Proj {
				t.Errorf("%s : chemin de l'hôte dans l'argv : %q", typ, a)
			}
		}
		// la commande finale est celle du launcher (sh -c pid1), image en un seul argument avant elle
		n := len(args)
		if n < 4 || args[n-3] != "sh" || args[n-2] != "-c" || args[n-1] != pid1 || args[n-4] != "debian:12" {
			t.Errorf("%s : fin d'argv %v (image, sh, -c, pid1 attendus)", typ, args[max(0, n-5):])
		}
	}
}

// Aucune valeur de l'utilisateur n'est interprétée : image ou nom hostile refusé (jamais un drapeau ni un second argument).
func TestE2DockerInjectionRefusee(t *testing.T) {
	b := e2Docker(t, "docker")
	if _, err := e2Args(t, b, "debian:12"); err != nil { // précondition : le cas valide fonctionne, sinon les refus ci-dessous ne prouvent rien
		t.Fatalf("argv d'un projet valide refusé : %v", err)
	}
	for _, bad := range []string{"--privileged", "-v /:/host", "debian:12 --privileged", "debian:12; touch /tmp/x", "$(id)", "`id`", "debian:12\n--privileged", "-p 22:22"} {
		args, err := e2Args(t, b, bad)
		if err == nil {
			for _, a := range args {
				if a == bad {
					t.Errorf("image %q transmise telle quelle dans l'argv", bad)
				}
			}
			t.Errorf("image %q acceptée", bad)
		}
	}
	for _, proj := range []string{"/space/PROJETS/cat/a b", "/space/PROJETS/cat/a;touch x", "/space/PROJETS/cat/$(id)", "/space/PROJETS/../etc/x", "/space/PROJETS/cat/--privileged", "/etc/passwd", "/space/PROJETS/cat/p\nq"} {
		pa, perr := Engine{b}.runArgs("cl-ctr", proj, EnvSpec{Image: "debian:12"})
		if perr == nil {
			t.Errorf("projet %q accepté : %v", proj, pa)
		}
	}
	// le nom du conteneur (--name) ne peut pas être un drapeau
	nameArgs, nameErr := Engine{b}.runArgs("--privileged", e2Proj, EnvSpec{Image: "debian:12"})
	if nameErr == nil {
		t.Errorf("nom de conteneur « --privileged » accepté : %v", nameArgs)
	}
}

// Le launcher ne crée jamais de stockage : ni volume (docker), ni PVC (k8s) — aucune commande « volume create » / « create pvc » dans l'argv.
func TestE2AucunStockageCree(t *testing.T) {
	b := e2Docker(t, "docker")
	args, err := e2Args(t, b, "debian:12")
	if err != nil {
		t.Fatalf("argv d'un projet valide refusé : %v", err)
	}
	j := strings.Join(args, " ")
	for _, bad := range []string{"volume create", "pvc", "--mount type=volume"} {
		if strings.Contains(j, bad) {
			t.Errorf("« %s » dans l'argv : %s", bad, j)
		}
	}
}

// ── D3 : pods NON root par défaut ───────────────────────────────────────────────────────────────────────

func TestE2K8sNonRoot(t *testing.T) {
	m, err := e2Manifest(t, e2K8s(t), "debian:12")
	if err != nil {
		t.Fatalf("manifeste refusé : %v", err)
	}
	spec := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	ctr := spec["containers"].([]any)[0].(map[string]any)
	csc, _ := ctr["securityContext"].(map[string]any)
	psc, _ := spec["securityContext"].(map[string]any)
	pick := func(k string) any { // au niveau du conteneur, à défaut du pod
		if v, ok := csc[k]; ok {
			return v
		}
		return psc[k]
	}
	if pick("runAsUser") != float64(2000) || pick("runAsGroup") != float64(2000) || pick("runAsNonRoot") != true {
		t.Errorf("identité : runAsUser=%v runAsGroup=%v runAsNonRoot=%v (2000, 2000, true attendus)", pick("runAsUser"), pick("runAsGroup"), pick("runAsNonRoot"))
	}
	if csc["allowPrivilegeEscalation"] != false {
		t.Errorf("allowPrivilegeEscalation = %v (false attendu)", csc["allowPrivilegeEscalation"])
	}
	if sp, _ := pick("seccompProfile").(map[string]any); sp["type"] != "RuntimeDefault" {
		t.Errorf("seccompProfile = %v (RuntimeDefault attendu)", pick("seccompProfile"))
	}
	caps, _ := csc["capabilities"].(map[string]any)
	if d, _ := caps["drop"].([]any); len(d) != 1 || d[0] != "ALL" {
		t.Errorf("capabilities.drop = %v ([ALL] attendu)", caps["drop"])
	}
	if _, add := caps["add"]; add {
		t.Error("capabilities.add présent")
	}
}

func e2Has(args []string, flag, val string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == val || a == flag+"="+val {
			return true
		}
	}
	return false
}

func TestE2DockerNonRootEtDurci(t *testing.T) {
	for _, typ := range []string{"docker", "podman"} {
		args, err := e2Args(t, e2Docker(t, typ), "debian:12")
		if err != nil {
			t.Errorf("%s : %v", typ, err)
			continue
		}
		if !e2Has(args, "--user", "2000:2000") {
			t.Errorf("%s : --user 2000:2000 absent : %v", typ, args)
		}
		for i, a := range args {
			if a == "--user" && i+1 < len(args) && (args[i+1] == "0" || args[i+1] == "root" || args[i+1] == "0:0") {
				t.Errorf("%s : conteneur root : %v", typ, args)
			}
		}
		if !e2Has(args, "--cap-drop", "ALL") {
			t.Errorf("%s : --cap-drop=ALL absent : %v", typ, args)
		}
		if !e2Has(args, "--security-opt", "no-new-privileges") && !e2Has(args, "--security-opt", "no-new-privileges:true") {
			t.Errorf("%s : --security-opt no-new-privileges absent : %v", typ, args)
		}
		for _, a := range args {
			if a == "--userns=keep-id" || a == "--user=0" {
				t.Errorf("%s : %s (identité de l'hôte / root)", typ, a)
			}
		}
	}
}

// Le launcher ne pose que SON environnement et SES options de sécurité : rien d'autre (--env, --restart, --ulimit…).
func TestE2DockerEnvEtOptionsFixes(t *testing.T) {
	args, err := e2Args(t, e2Docker(t, "docker"), "debian:12")
	if err != nil {
		t.Fatalf("argv refusé : %v", err)
	}
	fixed := map[string]bool{"HOME": true, "CLAUDE_CONFIG_DIR": true, "LANG": true, "DISABLE_AUTOUPDATER": true}
	for i, a := range args {
		if (a == "-e" || a == "--env") && i+1 < len(args) {
			if k := strings.SplitN(args[i+1], "=", 2)[0]; !fixed[k] {
				t.Errorf("variable d'environnement hors liste du launcher : %s", args[i+1])
			}
		}
		if a == "--security-opt" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "no-new-privileges") {
			t.Errorf("--security-opt inattendu : %s", args[i+1])
		}
		if a == "--cap-drop" && i+1 < len(args) && args[i+1] != "ALL" {
			t.Errorf("--cap-drop inattendu : %s", args[i+1])
		}
	}
}

// Variables d'environnement du conteneur k8s : seulement celles du launcher, jamais de valeur venue de l'utilisateur.
func TestE2K8sEnvFixe(t *testing.T) {
	m, err := e2Manifest(t, e2K8s(t), "debian:12")
	if err != nil {
		t.Fatalf("manifeste refusé : %v", err)
	}
	ctr := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	fixed := map[string]bool{"HOME": true, "CLAUDE_CONFIG_DIR": true, "LANG": true, "DISABLE_AUTOUPDATER": true}
	for _, e := range ctr["env"].([]any) {
		if n := e.(map[string]any)["name"].(string); !fixed[n] {
			t.Errorf("variable d'environnement hors liste : %s", n)
		}
		if _, vf := e.(map[string]any)["valueFrom"]; vf {
			t.Error("env.valueFrom présent")
		}
	}
	if sl, ok := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["enableServiceLinks"]; ok && sl != false {
		t.Errorf("enableServiceLinks = %v", sl)
	}
}

// T1 : des clés « pod » sensibles dans la DÉCLARATION du backend d'un utilisateur n'atteignent jamais le manifeste (constructeur dédié).
func TestE2K8sPodDuBackendJamaisRecopie(t *testing.T) {
	s15Setup(t)
	pod := "    pod:\n      serviceAccountName: launcher\n      nodeName: node-1\n      runtimeClassName: gvisor\n      priorityClassName: system-node-critical\n" +
		"      schedulerName: x\n      hostAliases: [{ip: 1.2.3.4, hostnames: [a]}]\n      dnsConfig: {nameservers: [1.1.1.1]}\n      securityContext: {runAsUser: 0}\n"
	sp := s15Space(t, e2UID, "backends:\n  k:\n    type: k8s\n    namespace: ns-u\n    pvc: projets\n    kubeconfig: true\n"+pod)
	s15Vault(t, sp, "k", s15Kubeconfig("CANARY-e2"))
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		return // déclaration refusée au chargement : échec fermé
	}
	m, merr := e2Manifest(t, bs[0], "debian:12")
	if merr != nil {
		return
	}
	raw, _ := json.Marshal(m)
	for _, bad := range []string{"serviceAccountName", "node-1", "gvisor", "system-node-critical", "hostAliases", "dnsConfig", "1.1.1.1", `"runAsUser":0`} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("« %s » du pod du backend recopié dans le manifeste d'un projet d'utilisateur", bad)
		}
	}
}

// T9 : noms d'objets avec un condensé d'uid — deux utilisateurs, même namespace/PVC/projet → noms différents ; DNS-1123, ≤ 63.
func TestE2NomsAvecCondensedUid(t *testing.T) {
	s15Setup(t)
	names := map[string]string{}
	for _, uid := range []string{"github-9", "github-10"} {
		sp := s15Space(t, uid, "backends:\n  k:\n    type: k8s\n    namespace: ns-u\n    pvc: projets\n    kubeconfig: true\n")
		s15Vault(t, sp, "k", s15Kubeconfig("CANARY-e2"))
		bs, err := sp.LoadBackends()
		if err != nil || len(bs) == 0 {
			t.Fatalf("%s : %v", uid, err)
		}
		m, merr := e2Manifest(t, bs[0], "debian:12")
		if merr != nil {
			t.Fatalf("%s : %v", uid, merr)
		}
		n := m["metadata"].(map[string]any)["name"].(string)
		names[uid] = n
		if len(n) > 63 || !regexp.MustCompile(`^cl-[0-9a-f]{8}-cat-proj$`).MatchString(n) {
			t.Errorf("%s : nom %q (cl-<8 hex>-cat-proj attendu)", uid, n)
		}
	}
	if names["github-9"] == names["github-10"] {
		t.Errorf("deux utilisateurs, même nom d'objet : %s", names["github-9"])
	}
}
