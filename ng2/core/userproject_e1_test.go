package core

import (
	"strings"
	"testing"
)

// LOT-3.2 Batch 0 (#275, E1) — options par projet en LISTE BLANCHE FERMÉE, défauts, limite de projets (contracts/user-backends.md §5 bis).
// Étiquette « s275api » : les tests supposent l'API ci-dessous, que dev-backend peut adopter ou renommer (il me prévient, j'adapte) ; sans
// elle le paquet compile. Proposition :
//
//	type OptionProblem struct{ Field, Path, Code, Msg string }
//	type PodSimple struct{ CPURequest, CPULimit, MemRequest, MemLimit, Ephemeral string }            // champs k8s, quantités k8s
//	func ParsePodOptions(s PodSimple, spec map[string]any) (PodOptions, []OptionProblem)             // zone JSON « Spécification du pod »
//	type ContainerRaw struct{ Cpus, Memory, PidsLimit, ShmSize, Gpus string }                         // docker / podman
//	func ParseContainerOptions(r ContainerRaw) (ContainerOptions, []OptionProblem)
//	func DefaultPodSimple() PodSimple ; func DefaultContainerRaw() ContainerRaw
//	func CheckProjectLimit(existing int) *OptionProblem                                                // users.maxProjects (CL_USERS_MAX_PROJECTS)
//	func ParsePodSpecJSON(raw []byte) (map[string]any, []OptionProblem)                                // T8 : zone JSON lue depuis les octets
//	func ParseContainerOptionsFor(backendType string, r ContainerRaw) (ContainerOptions, []OptionProblem) // podman : gpus refusé
//	func ValidUserProjectName(s string) string                                                         // T14
//	type RunAs struct{ UID, GID int } ; func ParseRunAs(uid, gid string) (RunAs, []OptionProblem) ; func ResolveRunAs(helm, backend, project *RunAs) RunAs  // D3
//	func ParseContainerOptionsMap(backendType string, m map[string]any) (ContainerOptions, []OptionProblem) ; func CheckUserEnv(env EnvSpec, defaultImage string) []OptionProblem // T3, T4, T5
//	func (po PodOptions) Apply(m map[string]any)                                                       // construit dans le manifeste, jamais recopié

func ok275(t *testing.T, ps []OptionProblem) {
	t.Helper()
	if len(ps) != 0 {
		t.Errorf("refus inattendu : %+v", ps)
	}
}

func hasPath(ps []OptionProblem, path string) *OptionProblem {
	for i := range ps {
		if ps[i].Path == path {
			return &ps[i]
		}
	}
	return nil
}

// Zone JSON : tout champ hors liste blanche est refusé avec le CHEMIN exact.
func TestE1ZoneJSONChampsInterdits(t *testing.T) {
	for _, k := range []string{"privileged", "hostNetwork", "hostPID", "hostIPC", "securityContext", "serviceAccountName", "runtimeClassName", "hostPath",
		"volumes", "ports", "containers", "initContainers", "nodeName", "capabilities", "cap_add", "inconnu", "automountServiceAccountToken", "dnsPolicy", "hostAliases"} {
		_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{k: true})
		p := hasPath(ps, "$."+k)
		if p == nil || p.Field != "pod.spec" || p.Code == "" || p.Msg == "" {
			t.Errorf("%s : refus nommé avec le chemin $.%s attendu : %+v", k, k, ps)
		}
	}
	// imbriqué : le chemin est exact
	_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"affinity": map[string]any{"nodeAffinity": map[string]any{"hostNetwork": true}}})
	if len(ps) == 0 {
		t.Error("champ interdit imbriqué dans affinity accepté")
	}
	_, ps = ParsePodOptions(DefaultPodSimple(), map[string]any{"tolerations": []any{map[string]any{"key": "a", "operator": "Exists", "effect": "NoSchedule"}, map[string]any{"key": "b", "operator": "Exists", "effect": "Nope"}}})
	if p := hasPath(ps, "$.tolerations[1].effect"); p == nil || p.Code != "invalid" {
		t.Errorf("toleration[1].effect invalide : %+v", ps)
	}
}

// CPU / mémoire saisis dans la zone : « utilise les champs CPU / mémoire ».
func TestE1ZoneJSONCPUMemoireRefuses(t *testing.T) {
	for _, path := range []string{"cpu", "memory", "ephemeral-storage"} {
		for _, side := range []string{"requests", "limits"} {
			spec := map[string]any{"resources": map[string]any{side: map[string]any{path: "1"}}}
			_, ps := ParsePodOptions(DefaultPodSimple(), spec)
			p := hasPath(ps, "$.resources."+side+"."+path)
			if p == nil || !strings.Contains(strings.ToLower(p.Msg), "utilise le") {
				t.Errorf("%s.%s dans la zone : %+v (« utilise les champs CPU / mémoire » attendu)", side, path, ps)
			}
		}
	}
	// ressources étendues : acceptées
	_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"resources": map[string]any{"limits": map[string]any{"nvidia.com/gpu": "1", "hugepages-2Mi": "512Mi"}}})
	ok275(t, ps)
}

// AUCUN plafond de CPU / mémoire imposé par le launcher (décision utilisateur) ; quantités invalides refusées.
func TestE1QuantitesSansPlafond(t *testing.T) {
	_, ps := ParsePodOptions(PodSimple{CPURequest: "100m", CPULimit: "64", MemRequest: "256Mi", MemLimit: "1Ti", Ephemeral: "500Gi"}, nil)
	ok275(t, ps)
	_, ps = ParsePodOptions(PodSimple{CPURequest: "64", CPULimit: "128", MemRequest: "1Ti", MemLimit: "2Ti"}, nil)
	ok275(t, ps)
	for name, s := range map[string]PodSimple{
		"cpu texte":                {CPURequest: "abc", CPULimit: "2"},
		"cpu négatif":              {CPURequest: "-1", CPULimit: "2"},
		"cpu unité inconnue":       {CPURequest: "1x", CPULimit: "2"},
		"mémoire texte":            {MemRequest: "beaucoup", MemLimit: "4Gi"},
		"mémoire unité":            {MemRequest: "5 Go", MemLimit: "4Gi"},
		"mémoire négative":         {MemRequest: "-5Mi", MemLimit: "4Gi"},
		"éphémère invalide":        {Ephemeral: "zz"},
		"limite < demande":         {CPURequest: "4", CPULimit: "2"},
		"mémoire limite < demande": {MemRequest: "8Gi", MemLimit: "4Gi"},
	} {
		_, ps := ParsePodOptions(s, nil)
		if len(ps) == 0 || ps[0].Field == "" || ps[0].Msg == "" {
			t.Errorf("%s : refus nommé attendu : %+v", name, ps)
		}
	}
}

func TestE1ZoneJSONFormeEtTaille(t *testing.T) {
	big := map[string]any{"nodeSelector": map[string]any{}}
	for i := 0; i < 17; i++ {
		big["nodeSelector"].(map[string]any)["k"+string(rune('a'+i))] = "v"
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), big); len(ps) == 0 {
		t.Error("17 paires nodeSelector acceptées (16 au plus)")
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"nodeSelector": map[string]any{"Bad Key!": "v"}}); hasPath(ps, "$.nodeSelector.Bad Key!") == nil {
		t.Errorf("clé de label invalide : %+v", ps)
	}
	if _, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"affinity": map[string]any{"podAffinity": map[string]any{"preferredDuringSchedulingIgnoredDuringExecution": []any{map[string]any{"weight": 1, "podAffinityTerm": map[string]any{"topologyKey": "kubernetes.io/hostname", "namespaceSelector": map[string]any{}}}}}}}); len(ps) != 0 {
		t.Errorf("namespaceSelector (cluster de l'utilisateur) refusé : %+v", ps)
	}
}

// docker / podman : cinq options plates.
func TestE1ConteneurOptions(t *testing.T) {
	_, ps := ParseContainerOptions(ContainerRaw{Cpus: "64", Memory: "1024g", PidsLimit: "100000", ShmSize: "16g", Gpus: "all"})
	ok275(t, ps) // aucun plafond
	_, ps = ParseContainerOptions(ContainerRaw{Gpus: "2"})
	ok275(t, ps)
	for name, r := range map[string]ContainerRaw{
		"cpus 0": {Cpus: "0"}, "cpus négatif": {Cpus: "-1"}, "cpus texte": {Cpus: "deux"},
		"memory < 64m": {Memory: "32m"}, "memory unité": {Memory: "4 Go"}, "memory texte": {Memory: "x"},
		"pids texte": {PidsLimit: "beaucoup"}, "pids décimal": {PidsLimit: "1.5"}, "pids 0": {PidsLimit: "0"},
		"shm invalide": {ShmSize: "gros"}, "gpus invalide": {Gpus: "device=0"}, "gpus injection": {Gpus: "all --privileged"},
	} {
		if _, ps := ParseContainerOptions(r); len(ps) == 0 || ps[0].Field == "" || !strings.HasPrefix(ps[0].Field, "container.") {
			t.Errorf("%s : refus nommé « container.<champ> » attendu : %+v", name, ps)
		}
	}
}

func TestE1Defauts(t *testing.T) {
	d := DefaultPodSimple()
	if d.CPURequest != "100m" || d.MemRequest != "256Mi" || d.CPULimit != "2" || d.MemLimit != "4Gi" {
		t.Errorf("défauts k8s : %+v (requests 100m/256Mi, limits 2/4Gi)", d)
	}
	c := DefaultContainerRaw()
	if c.Cpus != "2" || c.Memory != "4g" || c.PidsLimit != "4096" || c.ShmSize != "256m" || c.Gpus != "" {
		t.Errorf("défauts docker : %+v (cpus 2, memory 4g, pids_limit 4096, shm_size 256m)", c)
	}
	if _, ps := ParsePodOptions(d, nil); len(ps) != 0 {
		t.Errorf("les défauts doivent être valides : %+v", ps)
	}
	if _, ps := ParseContainerOptions(c); len(ps) != 0 {
		t.Errorf("les défauts docker doivent être valides : %+v", ps)
	}
}

// Limite de projets : 30 par défaut (Helm users.maxProjects, modifiable) ; refus nommé.
func TestE1LimiteDeProjets(t *testing.T) {
	t.Setenv("CL_USERS_MAX_PROJECTS", "")
	if p := CheckProjectLimit(29); p != nil {
		t.Errorf("30e projet refusé : %+v", p)
	}
	p := CheckProjectLimit(30)
	if p == nil || p.Code != "limit" || !strings.Contains(p.Msg, "nombre maximal de projets atteint (30)") {
		t.Errorf("31e projet : %+v", p)
	}
	t.Setenv("CL_USERS_MAX_PROJECTS", "5")
	if CheckProjectLimit(4) != nil || CheckProjectLimit(5) == nil || !strings.Contains(CheckProjectLimit(5).Msg, "(5)") {
		t.Error("limite modifiable (5)")
	}
	t.Setenv("CL_USERS_MAX_PROJECTS", "n'importe quoi")
	if CheckProjectLimit(30) == nil {
		t.Error("valeur illisible : retour au défaut (30)")
	}
}

// ── Exigences de sécurité (_work/reports/security-lot3.2.md) ───────────────────────────────────────────

// T8 — zone JSON « Spécification du pod » : lue depuis les OCTETS (clés en double détectées avant tout décodage en map) ;
// API proposée : func ParsePodSpecJSON(raw []byte) (map[string]any, []OptionProblem).
func TestT8ZoneJSONDurcie(t *testing.T) {
	deep := strings.Repeat(`{"a":`, 21) + "1" + strings.Repeat("}", 21)
	many := `{"nodeSelector":{` + strings.TrimSuffix(strings.Repeat(`"k":"v",`, 1), ",") + `}}`
	_ = many
	var nodes strings.Builder
	nodes.WriteString(`{"tolerations":[`)
	for i := 0; i < 300; i++ {
		if i > 0 {
			nodes.WriteString(",")
		}
		nodes.WriteString(`{"key":"k","operator":"Exists"}`)
	}
	nodes.WriteString("]}")
	for name, raw := range map[string]string{
		"17 Kio":                  `{"nodeSelector":{"a":"` + strings.Repeat("x", 17<<10) + `"}}`,
		"profondeur 21":           deep,
		"plus de 256 nœuds":       nodes.String(),
		"clé en double":           `{"nodeSelector":{"a":"1"},"nodeSelector":{"b":"2"}}`,
		"clé en double (casse)":   `{"nodeSelector":{"a":"1"},"NodeSelector":{"b":"2"}}`,
		"clé en double (Unicode)": `{"nodeSelector":{"a":"1"},"nodeſelector":{"b":"2"}}`,
		"champ inconnu":           `{"nodeSelector":{"a":"1"},"inconnu":true}`,
		"type inattendu":          `{"nodeSelector":["a"]}`,
		"JSON invalide":           `{"nodeSelector":`,
		"valeur nulle interdite":  `{"affinity":null,"hostNetwork":null}`,
		"NUL":                     "{\"nodeSelector\":{\"a\":\"b\x00\"}}",
	} {
		_, ps := ParsePodSpecJSON([]byte(raw))
		if len(ps) == 0 {
			t.Errorf("%s : accepté", name)
		}
	}
	if _, ps := ParsePodSpecJSON([]byte(`{"nodeSelector":{"disk":"ssd"},"tolerations":[{"key":"gpu","operator":"Exists","effect":"NoSchedule"}]}`)); len(ps) != 0 {
		t.Errorf("spécification valide refusée : %+v", ps)
	}
	// aucune valeur de l'utilisateur dans un message de refus (S8/T8)
	_, ps := ParsePodSpecJSON([]byte(`{"nodeSelector":{"CANARY-zone":"CANARY-valeur"},"hostNetwork":"CANARY-secret"}`))
	for _, p := range ps {
		if strings.Contains(p.Msg+p.Path+p.Field, "CANARY") && !strings.HasPrefix(p.Path, "$.nodeSelector") {
			t.Errorf("valeur de l'utilisateur dans le refus : %+v", p)
		}
		if strings.Contains(p.Msg, "CANARY-secret") {
			t.Errorf("valeur citée dans le message : %q", p.Msg)
		}
	}
}

// podman : gpus refusé (API compatible docker sans --gpus) ; docker : accepté. API proposée : ParseContainerOptionsFor(backendType, raw).
func TestE1PodmanGPUsRefuse(t *testing.T) {
	if _, ps := ParseContainerOptionsFor("podman", ContainerRaw{Gpus: "all"}); len(ps) == 0 || ps[0].Field != "container.gpus" {
		t.Errorf("podman gpus accepté : %+v", ps)
	}
	if _, ps := ParseContainerOptionsFor("docker", ContainerRaw{Gpus: "all"}); len(ps) != 0 {
		t.Errorf("docker gpus refusé : %+v", ps)
	}
	// valeurs strictes (T7) : forme exacte, jamais d'espace ni de second drapeau
	for name, r := range map[string]ContainerRaw{
		"cpus 5 décimales": {Cpus: "1.12345"}, "cpus 5 chiffres": {Cpus: "10000"}, "memory 16 chiffres": {Memory: "1234567890123456g"},
		"pids 4194305": {PidsLimit: "4194305"}, "gpus 17": {Gpus: "17"}, "gpus 0": {Gpus: "0"}, "cpus espace": {Cpus: "2 "}, "shm négatif": {ShmSize: "-1m"},
	} {
		if _, ps := ParseContainerOptionsFor("docker", r); len(ps) == 0 {
			t.Errorf("%s accepté", name)
		}
	}
	if _, ps := ParseContainerOptionsFor("docker", ContainerRaw{PidsLimit: "-1"}); len(ps) != 0 {
		t.Errorf("pids_limit -1 (illimité) refusé : %+v", ps)
	}
}

// T14 — noms de projet/catégorie d'un utilisateur : DNS-1123, ≤ 63, SANS le motif réservé « -old-<chiffres> » (collision avec les sauvegardes).
// API proposée : func ValidUserProjectName(s string) string (« » si valide, sinon la raison).
func TestT14NomsDeProjet(t *testing.T) {
	for _, bad := range []string{"foo-old-20261127", "x-old-1", "A", "a_b", "a b", "-a", "a-", "", strings.Repeat("a", 64), "a.b", "../x", "a/b", "é", "foo-OLD-1"} {
		if ValidUserProjectName(bad) == "" {
			t.Errorf("nom %q accepté", bad)
		}
	}
	for _, ok := range []string{"foo", "a", "mon-projet", "p1", "old", "old-school", "x-old", "x-older-2", strings.Repeat("a", 63)} {
		if r := ValidUserProjectName(ok); r != "" {
			t.Errorf("nom %q refusé : %s", ok, r)
		}
	}
}

// ── Contrat D1–D6 (c527353) ────────────────────────────────────────────────────────────────────────────
// API proposée en plus : func ParseContainerOptionsMap(backendType string, m map[string]any) (ContainerOptions, []OptionProblem)
// (clés hors des cinq options → refus « container.<clé> ») ; func CheckUserEnv(env EnvSpec, defaultImage string) []OptionProblem (T4, T5).

// T3 — table « Jamais » k8s COMPLÈTE : chaque champ refusé NOMMÉMENT avec son chemin, à la racine de la zone JSON.
func TestT3JamaisK8s(t *testing.T) {
	fields := []string{"serviceAccountName", "nodeName", "schedulerName", "runtimeClassName", "priorityClassName", "priority", "preemptionPolicy",
		"imagePullSecrets", "dnsPolicy", "dnsConfig", "hostAliases", "hostNetwork", "hostPID", "hostIPC", "hostUsers", "shareProcessNamespace",
		"initContainers", "ephemeralContainers", "env", "envFrom", "command", "args", "ports", "lifecycle", "livenessProbe", "readinessProbe", "startupProbe",
		"securityContext", "volumes", "hostPath", "enableServiceLinks", "restartPolicy", "overhead", "activeDeadlineSeconds", "subdomain",
		"setHostnameAsFQDN", "os", "topologySpreadConstraints", "containers", "volumeMounts", "automountServiceAccountToken", "terminationGracePeriodSeconds",
		"privileged", "capabilities", "serviceAccount", "hostname", "image", "workingDir", "stdin", "tty"}
	if len(fields) < 40 {
		t.Fatalf("%d entrées (≥ 40 attendues)", len(fields))
	}
	for _, k := range fields {
		_, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{k: map[string]any{"x": 1}})
		if p := hasPath(ps, "$."+k); p == nil || p.Field != "pod.spec" || p.Msg == "" {
			t.Errorf("%s : refus nommé avec le chemin $.%s attendu : %+v", k, k, ps)
		}
	}
	// resources.claims et securityContext « à tout niveau » (dans affinity, tolerations…)
	if _, ps := ParsePodOptions(DefaultPodSimple(), map[string]any{"resources": map[string]any{"claims": []any{map[string]any{"name": "x"}}}}); hasPath(ps, "$.resources.claims") == nil {
		t.Errorf("resources.claims : %+v", ps)
	}
}

// T3 — table « Jamais » docker/podman : toute clé hors des cinq options (cpus, memory, pids_limit, shm_size, gpus) → refus « container.<clé> ».
func TestT3JamaisDocker(t *testing.T) {
	keys := []string{"privileged", "cap_add", "cap-add", "cap_drop", "security_opt", "security-opt", "ulimit", "sysctl", "userns", "pid", "ipc", "uts", "cgroup_parent", "cgroupns",
		"network", "net", "dns", "dns_search", "add_host", "ports", "publish", "expose", "device", "devices", "volumes_from", "volumes", "volume", "mount", "mounts", "env",
		"env_file", "entrypoint", "user", "restart", "log_driver", "log_opt", "label_file", "runtime", "storage_opt", "tmpfs", "init", "oom_kill_disable", "oom_score_adj",
		"cpuset_cpus", "cpuset_mems", "blkio_weight", "read_only", "platform", "hostname", "name", "image", "command", "labels", "working_dir"}
	if len(keys) < 40 {
		t.Fatalf("%d entrées (≥ 40 attendues)", len(keys))
	}
	for _, typ := range []string{"docker", "podman"} {
		for _, k := range keys {
			_, ps := ParseContainerOptionsMap(typ, map[string]any{k: "x"})
			if len(ps) == 0 || ps[0].Field != "container."+k || ps[0].Msg == "" {
				t.Errorf("%s : %q accepté ou refus non nommé (container.%s attendu) : %+v", typ, k, k, ps)
			}
		}
	}
	if _, ps := ParseContainerOptionsMap("podman", map[string]any{"gpus": "all"}); len(ps) == 0 || ps[0].Field != "container.gpus" {
		t.Errorf("podman gpus accepté : %+v", ps)
	}
}

// D3 — utilisateur du pod / conteneur (deux champs simples) : uid et gid non nuls ; surcharge du backend par le projet ; root jamais.
// API proposée : func ParseRunAs(uid, gid string) (RunAs, []OptionProblem) ; func ResolveRunAs(helm, backend, project *RunAs) RunAs (Helm → backend → projet).
func TestD3RunAsDuProjet(t *testing.T) {
	for name, c := range map[string][2]string{"uid 0": {"0", "2000"}, "gid 0": {"2000", "0"}, "négatif": {"-1", "2000"}, "texte": {"root", "2000"}, "vide": {"", ""},
		"énorme": {"4294967296", "2000"}, "décimal": {"2000.5", "2000"}, "espace": {"2000 ", "2000"}} {
		if _, ps := ParseRunAs(c[0], c[1]); len(ps) == 0 || !strings.HasPrefix(ps[0].Field, "pod.run_as") && !strings.HasPrefix(ps[0].Field, "container.run_as") && !strings.Contains(ps[0].Field, "user") {
			t.Errorf("%s : refus nommé attendu : %+v", name, ps)
		}
	}
	if ra, ps := ParseRunAs("1000", "1000"); len(ps) != 0 || ra.UID != 1000 || ra.GID != 1000 {
		t.Errorf("1000:1000 : %+v %+v", ra, ps)
	}
	h, b, p := &RunAs{UID: 2000, GID: 2000}, &RunAs{UID: 3000, GID: 3100}, &RunAs{UID: 4000, GID: 4100}
	if r := ResolveRunAs(h, nil, nil); r != *h {
		t.Errorf("Helm seul : %+v", r)
	}
	if r := ResolveRunAs(h, b, nil); r != *b {
		t.Errorf("backend surcharge Helm : %+v", r)
	}
	if r := ResolveRunAs(h, b, p); r != *p {
		t.Errorf("projet surcharge backend : %+v", r)
	}
	if r := ResolveRunAs(nil, nil, nil); r.UID != 2000 || r.GID != 2000 {
		t.Errorf("défaut : %+v (2000:2000)", r)
	}
	if r := ResolveRunAs(&RunAs{UID: 0, GID: 0}, nil, nil); r.UID == 0 || r.GID == 0 {
		t.Errorf("Helm à 0 : root jamais, retour au défaut : %+v", r)
	}
}

// D4 / T4 / T5 — env.yaml d'un projet d'utilisateur : image = image par défaut (LOT-3.2) ; mounts et local refusés ; syntaxe de l'image.
func TestT4ImageDEnvYAML(t *testing.T) {
	def := "ghcr.io/ccoupel/claude-base:1.0"
	if ps := CheckUserEnv(EnvSpec{Image: def}, def); len(ps) != 0 {
		t.Errorf("image par défaut refusée : %+v", ps)
	}
	if ps := CheckUserEnv(EnvSpec{}, def); len(ps) != 0 {
		t.Errorf("env.yaml sans image refusé : %+v", ps)
	}
	ps := CheckUserEnv(EnvSpec{Image: "debian:12"}, def)
	if len(ps) == 0 || !strings.Contains(ps[0].Msg, "image non autorisée : seule l'image par défaut est admise") || ps[0].Field != "env.image" {
		t.Errorf("image ≠ défaut : %+v", ps)
	}
	for _, bad := range []string{"-v /:/host", "--privileged", "a b", "x;y", "A/B:1", strings.Repeat("a", 256), "a@sha256:xyz", "ghcr.io//x", "x:", ":1"} {
		if ps := CheckUserEnv(EnvSpec{Image: bad}, def); len(ps) == 0 {
			t.Errorf("image %q acceptée", bad)
		}
	}
	// T5 : liaisons hôte et volumes anonymes ignorés/refusés — jamais d'accès au système de fichiers du launcher
	ro := false
	for name, e := range map[string]EnvSpec{"mounts": {Mounts: []EnvMount{{Path: "/etc"}}}, "mounts ro": {Mounts: []EnvMount{{Path: "../x", RO: &ro}}}, "local": {Local: []string{"node_modules"}}} {
		if ps := CheckUserEnv(e, def); len(ps) == 0 || ps[0].Field == "" {
			t.Errorf("%s : refus nommé attendu : %+v", name, ps)
		}
	}
}
