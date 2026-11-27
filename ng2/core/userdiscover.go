package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Découverte des projets d'un utilisateur SUR SON backend (LOT-3.2 tâche 14, contrat §5) : la source de vérité est le backend. k8s : StatefulSets
// cl.launcher=ng2,cl.user=<uid> du namespace ; docker/podman : conteneurs (arrêtés compris) aux mêmes labels. Un objet n'est retenu que si
// SON NOM redonne celui que le launcher construit depuis ses labels (cl-<h8 de l'uid>-<catégorie>-<projet>) : un objet d'un autre compte,
// sans label ou renommé (sauvegarde de recréation) n'est jamais listé (T9, T10, T16).

// UserProject : un projet découvert.
type UserProject struct {
	Backend, Kind, Object, Category, Project string
	Running                                  bool
	Image                                    string
	Meta                                     *UserMeta // dernier état connu, lisible projet arrêté (cl.meta) ; nil si absent ou illisible
}

// userProjectFrom : valide les labels d'un objet et rend le projet, ou faux (objet à ignorer).
func (e Engine) userProjectFrom(object string, labels map[string]string) (UserProject, bool) {
	cat, proj := labels["cl.category"], labels["cl.project"]
	if labels["cl.launcher"] != "ng2" || labels["cl.user"] != e.B.uid || labels["cl.backend"] != e.B.Name || labels["cl.state"] == "old" ||
		!userSegRE.MatchString(cat) || !userSegRE.MatchString(proj) || UserObjName(e.B.uid, cat, proj) != object {
		return UserProject{}, false
	}
	return UserProject{Backend: e.B.Name, Kind: e.B.Type, Object: object, Category: cat, Project: proj}, true
}

// DiscoverUserProjects : projets de l'utilisateur sur ce backend d'utilisateur. Rien n'est lancé, créé ni modifié.
func (e Engine) DiscoverUserProjects() ([]UserProject, error) {
	if !e.B.user || e.B.uid == "" {
		return nil, errors.New("backend d'utilisateur attendu")
	}
	if e.B.K8s() {
		return e.discoverK8s()
	}
	return e.discoverDocker()
}

func (e Engine) discoverK8s() ([]UserProject, error) {
	out, err := e.kcmd("get", "statefulsets", "-l", "cl.launcher=ng2,cl.user="+e.B.uid, "-o", "json").Output()
	if err != nil {
		return nil, errors.New("lecture des StatefulSets impossible")
	}
	var l struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Replicas int `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas int `json:"readyReplicas"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(out, &l) != nil {
		return nil, errors.New("réponse illisible")
	}
	var ps []UserProject
	for _, it := range l.Items {
		p, ok := e.userProjectFrom(it.Metadata.Name, it.Metadata.Labels)
		if !ok {
			continue
		}
		p.Running, p.Image = it.Status.ReadyReplicas > 0, it.Metadata.Annotations["cl.image"]
		if raw := it.Metadata.Annotations["cl.meta"]; raw != "" {
			if m, err := ParseUserMeta([]byte(raw)); err == nil {
				p.Meta = &m
			}
		}
		ps = append(ps, p)
	}
	return ps, nil
}

func (e Engine) discoverDocker() ([]UserProject, error) {
	out, err := e.cmd("ps", "-a", "--filter", "label=cl.launcher=ng2", "--filter", "label=cl.user="+e.B.uid,
		"--format", "{{.Names}}|{{.State}}|{{.Image}}|{{.Labels}}").Output()
	if err != nil {
		return nil, errors.New("lecture des conteneurs impossible")
	}
	var ps []UserProject
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.SplitN(line, "|", 4)
		if len(f) != 4 {
			continue
		}
		labels := map[string]string{}
		for _, kv := range strings.Split(f[3], ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				labels[k] = v
			}
		}
		if p, ok := e.userProjectFrom(f[0], labels); ok {
			p.Running, p.Image = f[1] == "running", labels["cl.image"]
			ps = append(ps, p)
		}
	}
	return ps, nil
}

// ── état d'un projet arrêté ───────────────────────────────────────────────

// UserMeta : dernier état connu d'un projet, rangé SUR LE BACKEND (annotation cl.meta ou .claude-launcher/state.json) : schéma fixe, 4 Kio au
// plus, jamais un contenu de fichier ni un secret ; affiché seulement, jamais exécuté.
type UserMeta struct {
	Branch string `json:"branch"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
	Head   string `json:"head"`
	Tpl    string `json:"tpl"`
	Team   string `json:"team"`
	Seen   string `json:"seen"` // RFC 3339
}

const maxUserMeta = 4 << 10

// ParseUserMeta : JSON ≤ 4 Kio, champs connus seulement, une seule valeur ; les chaînes sont bornées et débarrassées des caractères de contrôle.
func ParseUserMeta(raw []byte) (UserMeta, error) {
	var m UserMeta
	if len(raw) > maxUserMeta {
		return m, errors.New("état trop gros")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil || dec.More() {
		return UserMeta{}, errors.New("état illisible")
	}
	clean := func(s string, n int) string {
		s = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, s)
		if r := []rune(s); len(r) > n {
			s = string(r[:n])
		}
		return s
	}
	m.Branch, m.Head, m.Tpl, m.Team, m.Seen = clean(m.Branch, 128), clean(m.Head, 64), clean(m.Tpl, 64), clean(m.Team, 128), clean(m.Seen, 40)
	if m.Ahead < 0 || m.Behind < 0 || m.Ahead > 1e6 || m.Behind > 1e6 {
		return UserMeta{}, errors.New("état illisible")
	}
	return m, nil
}

// JSON : forme écrite (≤ 4 Kio).
func (m UserMeta) JSON() ([]byte, error) {
	b, err := json.Marshal(m)
	if err == nil && len(b) > maxUserMeta {
		return nil, errors.New("état trop gros")
	}
	return b, err
}

// WriteUserMeta : écrit l'état d'un projet k8s dans l'annotation cl.meta de son StatefulSet (patch ; le pod n'a pas de jeton pour le faire).
func (e Engine) WriteUserMeta(object string, m UserMeta) error {
	if !e.B.K8s() || !e.B.user {
		return errors.New("annotation cl.meta : backend k8s d'utilisateur seulement")
	}
	b, err := m.JSON()
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{"cl.meta": string(b)}}})
	if err := e.VerifyOwnerK8s(object); err != nil {
		return err
	}
	return e.kcmd("patch", "statefulset", object, "--type=merge", "-p", string(patch)).Run()
}

// VerifyOwnerK8s : le StatefulSet porte cl.user = l'uid de l'espace (T10) ; sinon refus.
func (e Engine) VerifyOwnerK8s(object string) error {
	out, err := e.kcmd("get", "statefulset", object, "-o", `jsonpath={.metadata.labels.cl\.user}`).Output()
	if err != nil || strings.TrimSpace(string(out)) != e.B.uid || e.B.uid == "" {
		return errors.New("l'objet n'appartient pas à cet utilisateur")
	}
	return nil
}

var _ = fmt.Sprint
var _ = exec.Command
