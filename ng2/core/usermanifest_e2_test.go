package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	// la commande est un script FIXE du launcher (bootstrap root puis setpriv), jamais une valeur de l'utilisateur
	cmd, _ := ctr["command"].([]any)
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" || !strings.Contains(fmt.Sprint(cmd[2]), e2SetprivPrefix(2000, 2000)) {
		t.Errorf("commande du conteneur : %v (sh -c <script avec %s>)", cmd, e2SetprivPrefix(2000, 2000))
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

var dockerForbidden = []string{"--privileged", "-p", "-P", "--publish", "--publish-all", "--network", "--net", "--pid", "--ipc", "--uts",
	"--volumes-from", "--device", "--userns", "--cgroupns", "--add-host", "--dns", "--dns-search", "--dns-option", "--sysctl", "--ulimit", "--init",
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
		// gabarit (contrat §5 bis) : « create » puis « start », options --opt=valeur, volume nommé monté par --mount=type=volume
		if args[0] != "create" {
			t.Errorf("%s : « create » attendu (puis « start »), reçu %q", typ, args[0])
		}
		if !e2Has(args, "--mount", "type=volume,src=vol,dst=/space") {
			t.Errorf("%s : --mount=type=volume,src=vol,dst=/space absent : %v", typ, args)
		}
		for _, want := range []string{"cl.user=" + e2UID, "cl.category=cat", "cl.project=proj", "cl.backend=d", "cl.launcher=ng2"} {
			if !strings.Contains(j, "--label="+want+" ") && !strings.Contains(j, " --label "+want+" ") {
				t.Errorf("%s : label %s absent de %v", typ, want, args)
			}
		}
		if !e2Has(args, "--workdir", e2Proj) {
			t.Errorf("%s : dossier de travail %s absent : %v", typ, e2Proj, args)
		}
		if !e2Has(args, "-e", "HOME=/space/.claude/home") && !strings.Contains(j, " HOME=/space/.claude/home ") || !strings.Contains(j, "CLAUDE_CONFIG_DIR=/space/.claude ") {
			t.Errorf("%s : HOME=/space/.claude/home et CLAUDE_CONFIG_DIR=/space/.claude attendus : %v", typ, args)
		}
		for _, f := range dockerForbidden {
			for _, a := range args {
				if a == f || strings.HasPrefix(a, f+"=") {
					t.Errorf("%s : option interdite %s dans %v", typ, f, args)
				}
			}
		}
		// jamais de montage de l'hôte ni de « -v » : seul le volume nommé, par --mount=type=volume
		for i, a := range args {
			if a == "-v" || a == "--volume" || strings.HasPrefix(a, "--volume=") || strings.HasPrefix(a, "-v=") {
				t.Errorf("%s : « %s » interdit (gabarit : --mount=type=volume) : %v", typ, a, args)
			}
			if strings.Contains(a, "type=bind") || strings.Contains(a, "type=tmpfs") || strings.HasPrefix(a, "/") && a != e2Proj && (i == 0 || args[i-1] != "--workdir") {
				t.Errorf("%s : montage ou chemin de l'hôte dans l'argv : %q", typ, a)
			}
		}
		// la commande finale est celle du launcher (sh -c pid1), image en un seul argument avant elle
		n := len(args)
		if n < 4 || args[n-3] != "sh" || args[n-2] != "-c" || !strings.Contains(args[n-1], e2SetprivPrefix(2000, 2000)) || args[n-4] != "debian:12" {
			t.Errorf("%s : fin d'argv %v (image, sh, -c, script de bootstrap avec setpriv attendus)", typ, args[max(0, n-5):])
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
	// D3 révisée (P28) : démarrage en ROOT pour le bootstrap seulement ; le traitement passe à 2000:2000 par setpriv
	if csc["runAsUser"] != float64(0) && psc["runAsUser"] != float64(0) {
		t.Errorf("runAsUser = %v / %v (0 attendu : bootstrap root)", csc["runAsUser"], psc["runAsUser"])
	}
	if csc["runAsNonRoot"] == true || psc["runAsNonRoot"] == true {
		t.Error("runAsNonRoot: true posé alors que le bootstrap est root (le profil restricted est refusé nommément)")
	}
	if csc["allowPrivilegeEscalation"] != false {
		t.Errorf("allowPrivilegeEscalation = %v (false attendu)", csc["allowPrivilegeEscalation"])
	}
	sp, _ := csc["seccompProfile"].(map[string]any)
	if sp == nil {
		sp, _ = psc["seccompProfile"].(map[string]any)
	}
	if sp["type"] != "RuntimeDefault" {
		t.Errorf("seccompProfile = %v (RuntimeDefault attendu)", sp)
	}
	caps, _ := csc["capabilities"].(map[string]any)
	if d, _ := caps["drop"].([]any); len(d) != 1 || d[0] != "ALL" {
		t.Errorf("capabilities.drop = %v ([ALL] attendu)", caps["drop"])
	}
	got := map[string]bool{}
	adds, _ := caps["add"].([]any)
	for _, a := range adds {
		got[fmt.Sprint(a)] = true
	}
	want := []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETUID", "SETGID"}
	if len(adds) != len(want) {
		t.Errorf("capabilities.add = %v (EXACTEMENT %v)", adds, want)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("capabilité %s absente de capabilities.add : %v", w, adds)
		}
	}
	if csc["privileged"] != false {
		t.Errorf("privileged = %v (false explicite attendu)", csc["privileged"])
	}
	if csc["readOnlyRootFilesystem"] == true {
		t.Error("racine en lecture seule : le bootstrap installe des paquets")
	}
}

// Surcharge : run_as du backend (uid/gid non nuls) → UTILISATEUR DU TRAITEMENT (exec, setpriv) ; le conteneur démarre toujours en root
// (bootstrap) et ne reçoit jamais « --user » à la création.
func TestE2RunAsDuBackend(t *testing.T) {
	s15Setup(t)
	sp := s15Space(t, e2UID, "backends:\n  k:\n    type: k8s\n    namespace: ns-u\n    pvc: projets\n    kubeconfig: true\n    run_as: {uid: 3000, gid: 4000}\n"+
		"  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: vol\n    run_as: {uid: 3000, gid: 4000}\n")
	s15Vault(t, sp, "k", s15Kubeconfig("CANARY-e2"))
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) != 2 {
		t.Fatalf("run_as non admis dans la déclaration d'un backend d'utilisateur : %v (%d backends)", err, len(bs))
	}
	for _, b := range bs {
		switch b.Type {
		case "k8s":
			m, merr := e2Manifest(t, b, "debian:12")
			if merr != nil {
				t.Fatalf("manifeste : %v", merr)
			}
			spec := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
			csc, _ := spec["containers"].([]any)[0].(map[string]any)["securityContext"].(map[string]any)
			if csc["runAsUser"] != float64(0) {
				t.Errorf("k8s : runAsUser=%v (0 : bootstrap root)", csc["runAsUser"])
			}
			cmd := fmt.Sprint(spec["containers"].([]any)[0].(map[string]any)["command"])
			if !strings.Contains(cmd, e2SetprivPrefix(3000, 4000)) {
				t.Errorf("k8s : le bootstrap doit passer à 3000:4000 par setpriv : %s", cmd)
			}
			if j := strings.Join(Engine{b}.ExecArgv("cl-x-0", false, false, "id"), " "); !strings.Contains(j, e2SetprivPrefix(3000, 4000)) {
				t.Errorf("k8s : exec sans setpriv 3000:4000 : %s", j)
			}
		default:
			args, aerr := e2Args(t, b, "debian:12")
			if aerr != nil {
				t.Fatalf("argv : %v", aerr)
			}
			for _, a := range args {
				if a == "--user" || strings.HasPrefix(a, "--user=") || a == "-u" {
					t.Errorf("docker : --user ne doit pas figurer au create (bootstrap root) : %v", args)
				}
			}
			if j := strings.Join(Engine{b}.ExecArgv("cl-x", false, false, "id"), " "); !strings.Contains(j, "--user=3000:4000") {
				t.Errorf("docker : exec sans --user=3000:4000 : %s", j)
			}
		}
	}
}

// run_as : uid ou gid nul / négatif / non entier / absent / hors plage → refusé nommément (root jamais pour le traitement).
func TestE2RunAsRefuse(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	for name, ra := range map[string]string{"uid 0": "{uid: 0, gid: 2000}", "gid 0": "{uid: 2000, gid: 0}", "tous deux 0": "{uid: 0, gid: 0}", "uid négatif": "{uid: -1, gid: 2000}",
		"uid texte": "{uid: root, gid: 2000}", "uid décimal": "{uid: 2000.5, gid: 2000}", "gid absent": "{uid: 2000}", "uid absent": "{gid: 2000}",
		"uid énorme": "{uid: 4294967296, gid: 2000}", "clé en plus": "{uid: 2000, gid: 2000, groups: [0]}", "liste": "[2000, 2000]"} {
		y := "backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: v\n    run_as: " + ra + "\n"
		if ms := errMsgs(sp.ValidateBackends([]byte(y))); len(ms) == 0 {
			t.Errorf("run_as %s accepté", name)
		}
	}
	ok := "backends:\n  d:\n    type: docker\n    host: tcp://93.184.216.34:2376\n    volume: v\n    run_as: {uid: 1000, gid: 1000}\n"
	if ms := errMsgs(sp.ValidateBackends([]byte(ok))); len(ms) != 0 {
		t.Errorf("run_as 1000:1000 refusé : %v", ms)
	}
}

// e2SetprivPrefix : préfixe imposé à tout exec k8s et au bootstrap (D3 révisée).
func e2SetprivPrefix(uid, gid int) string {
	return fmt.Sprintf("setpriv --reuid=%d --regid=%d --clear-groups --no-new-privs --bounding-set=-all --inh-caps=-all", uid, gid)
}

// Chaque exec du launcher : docker/podman → --user=<uid>:<gid> ; k8s → préfixe setpriv ; JAMAIS en root, même si l'appelant le demande.
func TestE2ExecToujoursSousLUtilisateurDuTraitement(t *testing.T) {
	cases := map[string]BackendConf{"k8s": e2K8s(t), "docker": e2Docker(t, "docker"), "podman": e2Docker(t, "podman")}
	for typ, b := range cases {
		for _, root := range []bool{false, true} {
			for _, tty := range []bool{false, true} {
				argv := Engine{b}.ExecArgv("cl-ctr", root, tty, "tmux", "ls")
				j := strings.Join(argv, " ")
				if typ == "k8s" {
					if !strings.Contains(j, e2SetprivPrefix(2000, 2000)) {
						t.Errorf("%s root=%v tty=%v : préfixe setpriv absent : %s", typ, root, tty, j)
					}
					for _, bad := range []string{"--init-groups", "exec su ", " su -"} {
						if strings.Contains(j, bad) {
							t.Errorf("%s root=%v tty=%v : « %s » (repli ou groupes hérités) : %s", typ, root, tty, bad, j)
						}
					}
				} else {
					if !strings.Contains(j, "--user=2000:2000") {
						t.Errorf("%s root=%v tty=%v : --user=2000:2000 absent : %s", typ, root, tty, j)
					}
					for _, bad := range []string{"-u 0", "--user=0", "--user 0", "-u root", "--user=root"} {
						if strings.Contains(j, bad) {
							t.Errorf("%s root=%v tty=%v : exec en root (%s) : %s", typ, root, tty, bad, j)
						}
					}
				}
			}
		}
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
		for _, a := range args {
			if a == "--user" || a == "-u" || strings.HasPrefix(a, "--user=") {
				t.Errorf("%s : %s au create (le bootstrap démarre en root ; --user seulement sur chaque exec) : %v", typ, a, args)
			}
			if a == "--userns=keep-id" {
				t.Errorf("%s : --userns=keep-id", typ)
			}
		}
		if !e2Has(args, "--cap-drop", "ALL") {
			t.Errorf("%s : --cap-drop=ALL absent : %v", typ, args)
		}
		caps := map[string]bool{}
		for i, a := range args {
			v := ""
			switch {
			case a == "--cap-add" && i+1 < len(args):
				v = args[i+1]
			case strings.HasPrefix(a, "--cap-add="):
				v = strings.TrimPrefix(a, "--cap-add=")
			}
			for _, c := range strings.Split(v, ",") {
				if c != "" {
					caps[c] = true
				}
			}
		}
		want := []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETUID", "SETGID"}
		if len(caps) != len(want) {
			t.Errorf("%s : capacités ajoutées %v (EXACTEMENT %v)", typ, caps, want)
		}
		for _, w := range want {
			if !caps[w] {
				t.Errorf("%s : --cap-add %s absent : %v", typ, w, args)
			}
		}
		if !e2Has(args, "--security-opt", "no-new-privileges") && !e2Has(args, "--security-opt", "no-new-privileges:true") {
			t.Errorf("%s : --security-opt no-new-privileges absent : %v", typ, args)
		}
	}
}

// Le launcher ne pose que SON environnement et SES options de sécurité : rien d'autre (--env, --restart, --ulimit…).
func TestE2DockerEnvEtOptionsFixes(t *testing.T) {
	args, err := e2Args(t, e2Docker(t, "docker"), "debian:12")
	if err != nil {
		t.Fatalf("argv refusé : %v", err)
	}
	fixed := map[string]bool{"HOME": true, "CLAUDE_CONFIG_DIR": true, "XDG_CONFIG_HOME": true, "LANG": true, "DISABLE_AUTOUPDATER": true}
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
		if a == "--cap-add" && i+1 < len(args) {
			for _, c := range strings.Split(args[i+1], ",") {
				if !map[string]bool{"CHOWN": true, "DAC_OVERRIDE": true, "FOWNER": true, "SETUID": true, "SETGID": true}[c] {
					t.Errorf("capacité hors liste du launcher : %s", c)
				}
			}
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
	fixed := map[string]bool{"HOME": true, "CLAUDE_CONFIG_DIR": true, "XDG_CONFIG_HOME": true, "LANG": true, "DISABLE_AUTOUPDATER": true}
	vals := map[string]string{}
	for _, e := range ctr["env"].([]any) {
		em := e.(map[string]any)
		vals[em["name"].(string)], _ = em["value"].(string)
		if n := em["name"].(string); !fixed[n] {
			t.Errorf("variable d'environnement hors liste : %s", n)
		}
		if _, vf := e.(map[string]any)["valueFrom"]; vf {
			t.Error("env.valueFrom présent")
		}
	}
	for k, want := range map[string]string{"HOME": "/space/.claude/home", "CLAUDE_CONFIG_DIR": "/space/.claude", "XDG_CONFIG_HOME": "/space/.claude/home/.config"} {
		if vals[k] != want {
			t.Errorf("%s = %q (attendu %q)", k, vals[k], want)
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
	for _, bad := range []string{"serviceAccountName", "node-1", "gvisor", "system-node-critical", "hostAliases", "dnsConfig", "1.1.1.1"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("« %s » du pod du backend recopié dans le manifeste d'un projet d'utilisateur", bad)
		}
	}
	// « securityContext: {runAsUser: 0} » du backend : le runAsUser 0 du bootstrap est celui du launcher, AU NIVEAU DU CONTENEUR seulement ; le
	// pod n'a aucun securityContext venu du backend
	if psc := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["securityContext"]; psc != nil {
		t.Errorf("securityContext du pod recopié du backend : %v", psc)
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
		h := sha256.Sum256([]byte(uid))
		if want := "cl-" + hex.EncodeToString(h[:])[:8] + "-cat-proj"; n != want || len(n) > 63 {
			t.Errorf("%s : nom %q (attendu %q : 8 premiers hex du SHA-256 de l'uid)", uid, n, want)
		}
	}
	if names["github-9"] == names["github-10"] {
		t.Errorf("deux utilisateurs, même nom d'objet : %s", names["github-9"])
	}
}
