package core

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ── messages sans contenu de l'utilisateur (S8, S25, S26, A6) ──────────────

var (
	quotedRE    = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|«\s*([^»]*?)\s*»`)
	plainNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,63}$`)
	secretRE    = regexp.MustCompile(`(?i)(bearer\s+\S+|token[=: ]+\S+|-----BEGIN[^\n]*|client-key\S*|[A-Za-z0-9+/=_-]{40,})`)
)

// ScrubMessage : un message affiché à un utilisateur ne reprend jamais une valeur qu'il a fournie — toute valeur entre guillemets qui
// n'est pas un simple nom est remplacée par « … » ; jetons, PEM et longues chaînes base64 sont masqués. Une seule ligne.
func ScrubMessage(m string) string {
	if i := strings.IndexByte(m, '\n'); i >= 0 {
		m = m[:i]
	}
	m = quotedRE.ReplaceAllStringFunc(m, func(q string) string {
		sub := quotedRE.FindStringSubmatch(q)
		v := sub[1] + sub[2]
		if plainNameRE.MatchString(v) {
			return q
		}
		if strings.HasPrefix(q, "«") {
			return "« … »"
		}
		return `"…"`
	})
	return secretRE.ReplaceAllString(m, "…")
}

// ScrubProblems : ScrubMessage sur chaque problème.
func ScrubProblems(ps []ConfigProblem) []ConfigProblem {
	for i := range ps {
		ps[i].Message = ScrubMessage(ps[i].Message)
	}
	return ps
}

// ── backendTest d'un utilisateur (S8, S22, S31 ; contrat §3 « Droits vérifiés ») ───────────────────

// requiredVerbs : droits nécessaires dans le namespace de l'utilisateur (vérifiés par SelfSubjectAccessReview : « auth can-i »).
var requiredVerbs = []struct{ verb, resource string }{
	{"create", "statefulsets"}, {"get", "statefulsets"}, {"list", "statefulsets"}, {"patch", "statefulsets"}, {"delete", "statefulsets"},
	{"update", "statefulsets/scale"}, {"get", "pods"}, {"list", "pods"}, {"watch", "pods"}, {"delete", "pods"},
	{"create", "pods/exec"}, {"get", "persistentvolumeclaims"}, {"list", "events"},
}

// MinimalRole : Role minimale suggérée (namespace de l'utilisateur) quand des droits manquent ou que le compte est trop puissant.
func MinimalRole(ns string) string {
	return "Role minimale à appliquer dans le namespace " + ns + " (puis un RoleBinding sur votre ServiceAccount) : " +
		"apiGroups [apps] statefulsets (create get list patch delete) et statefulsets/scale (update) ; " +
		"apiGroups [\"\"] pods (get list watch delete), pods/exec (create), persistentvolumeclaims (get), events (list)"
}

func userStep(label string, ok bool, detail, fix string) TestStep {
	return TestStep{Label: label, OK: ok, Detail: ScrubMessage(detail), Fix: fix}
}

// FirstProblemCat : première catégorie de refus d'hôte relevée dans des problèmes.
func FirstProblemCat(ps []ConfigProblem) string {
	for _, p := range ps {
		if p.Cat != "" {
			return p.Cat
		}
	}
	return ""
}

// TestBackend : étapes du test d'un backend DÉCLARÉ PAR l'utilisateur (e : formulaire ou entrée du fichier). Même contrôle de forme
// qu'à l'enregistrement ; puis, sans jamais rendre une réponse du backend (diagnostic grossier, S8) : joignabilité, droits (reviews
// seulement, rien n'est créé pour sonder), PVC / volume existant, avertissements (root sur l'hôte, TLS non vérifié, compte trop puissant).
func (s Space) TestBackend(e BackendEntry) []TestStep {
	if s.Admin {
		return []TestStep{userStep("espace", false, "test d'un utilisateur seulement", "")}
	}
	c := map[string]any{"type": e.Type}
	set := func(k string, v any) {
		switch x := v.(type) {
		case string:
			if x != "" {
				c[k] = x
			}
		case nil:
		default:
			c[k] = v
		}
	}
	set("host", e.Host)
	set("namespace", e.Namespace)
	set("pvc", e.PVC)
	set("volume", e.Volume)
	if e.TLS != nil {
		t := map[string]any{"enabled": e.TLS.Enabled}
		if e.TLS.Verify != nil {
			t["verify"] = *e.TLS.Verify
		}
		c["tls"] = t
	}
	if kc := kubeconfigFromAny(e.Kubeconfig); kc.Vault {
		c["kubeconfig"] = true
	} else if e.Kubeconfig != nil && e.Type == "k8s" {
		c["kubeconfig"] = e.Kubeconfig
	}
	if e.Pod != nil {
		c["pod"] = e.Pod
	}
	content, _ := yaml.Marshal(map[string]any{"backends": map[string]any{e.Name: c}})
	if UserAdminFields(content) {
		return []TestStep{userStep("déclaration", false, "champ réservé à l'administrateur dans ce backend", "retirer ce champ")}
	}
	ps := s.ValidateBackends(content)
	for _, p := range ps {
		if p.Severity == SevError {
			st := userStep("déclaration", false, p.Message, "corriger la déclaration du backend")
			st.Cat = p.Cat
			return []TestStep{st}
		}
	}
	bs, err := loadBackendsData(content, nil)
	if err != nil || len(bs) == 0 {
		return []TestStep{userStep("déclaration", false, "déclaration illisible", "")}
	}
	var b BackendConf
	for _, x := range bs {
		if x.Name == e.Name {
			b = x
		}
	}
	b.user, b.vault = true, s.VaultDir()
	steps := []TestStep{userStep("déclaration", true, "", "")}
	shared := TestStep{Label: "avertissement", Warn: true, Detail: SharedBackendWarning}
	switch b.Type {
	case "k8s":
		return append(append(steps, userK8sSteps(b)...), shared)
	case "docker", "podman":
		return append(append(steps, userEngineSteps(b)...), shared)
	}
	return append(steps, userStep("type", false, "type de backend non pris en charge", ""))
}

func userEngineSteps(b BackendConf) []TestStep {
	var steps []TestStep
	e := Engine{b}
	if t := b.TLS; t != nil && t.Enabled {
		d := b.vault + "/" + b.Name
		if t.verifies() && !regularFile(d+"/"+secretCAFile) {
			return append(steps, userStep("certificats TLS", false, "CA du serveur requise pour vérifier le certificat (tls.verify)", "coller la CA du serveur dans l'encadré TLS"))
		}
		steps = append(steps, userStep("certificats TLS", true, "", ""))
		ws, expired := tlsExpiry(Space{UID: "x"}.tlsInfos(d), time.Now())
		for _, w := range ws {
			steps = append(steps, TestStep{Label: "avertissement", Warn: true, Detail: w, Fix: "renouveler le certificat dans l'encadré TLS"})
		}
		if expired {
			return steps
		}
	}
	if _, err := runQuiet(15*time.Second, e.cli("version", "--format", "{{.Server.Version}}")...); err != nil {
		return append(steps, userStep("démon joignable", false, "backend injoignable depuis la plateforme", "vérifier l'adresse, le port, le TLS et que le démon accepte les connexions depuis Internet"))
	}
	steps = append(steps, userStep("démon joignable", true, "", ""))
	if b.Volume != "" {
		if _, err := runQuiet(15*time.Second, e.cli("volume", "inspect", b.Volume)...); err != nil {
			return append(steps, userStep("volume", false, "volume « "+b.Volume+" » introuvable sur l'hôte", "le créer sur l'hôte (le launcher n'en crée aucun)"))
		}
		steps = append(steps, userStep("volume", true, "volume « "+b.Volume+" » présent", ""))
	}
	if b.TLS == nil || !b.TLS.verifies() {
		steps = append(steps, TestStep{Label: "avertissement", Warn: true, Detail: RootOnHostWarning})
	}
	return steps
}

func userK8sSteps(b BackendConf) []TestStep {
	e := Engine{b}
	steps, ok := kubeconfigSteps(b)
	if !ok {
		for i := range steps {
			steps[i].Detail = ScrubMessage(steps[i].Detail)
		}
		return steps
	}
	if _, err := runQuiet(15*time.Second, e.kubectl("get", "statefulsets", "-o", "name")...); err != nil {
		return append(steps, userStep("cluster joignable", false, "backend injoignable ou accès refusé", "vérifier le serveur du kubeconfig, le jeton et le namespace"))
	}
	steps = append(steps, userStep("cluster joignable, namespace "+b.Namespace, true, "", ""))
	// 13 revues de droits (« auth can-i ») en parallèle, 4 à la fois : un backend lointain ne coûte pas 13 allers-retours en série
	denied := make([]bool, len(requiredVerbs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, r := range requiredVerbs {
		wg.Add(1)
		go func(i int, verb, res string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, _ := runQuiet(15*time.Second, e.kubectl("auth", "can-i", verb, res)...)
			denied[i] = strings.TrimSpace(out) != "yes"
		}(i, r.verb, r.resource)
	}
	wg.Wait()
	var missing []string
	for i, r := range requiredVerbs {
		if denied[i] {
			missing = append(missing, r.verb+" "+r.resource)
		}
	}
	if len(missing) > 0 {
		return append(steps, userStep("droits dans "+b.Namespace, false, "droits manquants : "+strings.Join(missing, ", "), MinimalRole(b.Namespace)))
	}
	steps = append(steps, userStep("droits dans "+b.Namespace, true, "", ""))
	if b.PVC != "" {
		s, err := runQuiet(15*time.Second, e.kubectl("get", "pvc", b.PVC, "-o", "jsonpath={.status.phase} {.spec.accessModes}")...)
		switch {
		case err != nil:
			return append(steps, userStep("PVC "+b.PVC, false, "PVC « "+b.PVC+" » introuvable dans le namespace", "le créer (ReadWriteMany) : le launcher n'en crée aucun"))
		case !strings.HasPrefix(s, "Bound"):
			return append(steps, userStep("PVC "+b.PVC, false, "PVC « "+b.PVC+" » non lié", "attendre son liage au volume"))
		case !strings.Contains(s, "ReadWriteMany"):
			return append(steps, userStep("PVC "+b.PVC, false, "PVC « "+b.PVC+" » n'est pas ReadWriteMany", "plusieurs projets ouverts le montent : ReadWriteMany requis"))
		}
		steps = append(steps, userStep("PVC "+b.PVC, true, "", ""))
	}
	// compte plus puissant que nécessaire (P19) : revue de droits sur tous les namespaces, jamais une création pour sonder
	if out, _ := runQuiet(15*time.Second, e.kubectl("auth", "can-i", "*", "*", "--all-namespaces")...); strings.TrimSpace(out) == "yes" {
		steps = append(steps, TestStep{Label: "avertissement", Warn: true,
			Detail: "Compte plus puissant que nécessaire. Le coffre est en clair : préfère un ServiceAccount limité au namespace.",
			Fix:    MinimalRole(b.Namespace)})
	}
	for _, w := range kubeconfigInsecureWarning(b) {
		steps = append(steps, TestStep{Label: "avertissement", Warn: true, Detail: w})
	}
	return steps
}

// kubeconfigInsecureWarning : « certificat de l'API non vérifié » relevé dans le kubeconfig du coffre.
func kubeconfigInsecureWarning(b BackendConf) []string {
	f, err := b.kubeconfigFile()
	if err != nil {
		return nil
	}
	raw, err := readFileRetry(f)
	if err != nil {
		return nil
	}
	if _, _, w, err := parseUserKubeconfig(raw); err == nil {
		return w
	}
	return nil
}

var _ = fmt.Sprint
