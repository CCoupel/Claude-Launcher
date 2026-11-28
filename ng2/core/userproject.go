package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Cycle de vie des projets d'un utilisateur SUR SON backend (LOT-3.2 tâches 14–16) : créer (objet labellisé, arrêté), démarrer, lire les
// réglages courants. Toute opération sur un objet existant vérifie d'abord son propriétaire (cl.user, T10).

// UserProjectID : identifiant web d'un projet d'utilisateur « <backend>/<catégorie>/<nom> » (jamais un chemin du serveur).
func UserProjectID(backend, cat, name string) string { return backend + "/" + cat + "/" + name }

// ParseUserProjectID : backend, catégorie et nom d'un identifiant ; erreur générique si la forme n'est pas exacte.
func ParseUserProjectID(id string) (backend, cat, name string, err error) {
	p := strings.Split(id, "/")
	if len(p) != 3 || CheckUserBackendName(p[0]) != nil || ValidUserProjectName(p[1]) != "" || ValidUserProjectName(p[2]) != "" {
		return "", "", "", errors.New("identifiant de projet invalide")
	}
	return p[0], p[1], p[2], nil
}

// UserProjectDir : dossier du projet vu du conteneur.
func UserProjectDir(cat, name string) string { return "/space/PROJETS/" + cat + "/" + name }

// ObjectName : nom de l'objet (StatefulSet ou conteneur) du projet.
func (e Engine) ObjectName(cat, name string) string { return UserObjName(e.B.uid, cat, name) }

// UserProjectExists : l'objet du projet existe déjà sur le backend.
func (e Engine) UserProjectExists(cat, name string) bool {
	obj := e.ObjectName(cat, name)
	if e.B.K8s() {
		return e.kcmd("get", "statefulset", obj, "-o", "name").Run() == nil
	}
	return e.cmd("inspect", "-f", "{{.Id}}", obj).Run() == nil
}

// CreateUserProject : crée l'objet labellisé du projet, ARRÊTÉ (k8s : StatefulSet à 0 réplique ; docker/podman : conteneur créé sans démarrage).
// Le dossier du projet est créé au premier démarrage. Image : validée ; rien n'est créé si le projet existe déjà.
func (e Engine) CreateUserProject(cat, name, image string) error {
	if ValidUserProjectName(cat) != "" || ValidUserProjectName(name) != "" { // minuscules DNS-1123, motif -old-<chiffres> réservé (T14)
		return errors.New("nom de projet invalide")
	}
	proj := UserProjectDir(cat, name)
	env := EnvSpec{Image: image}
	objs, lerr := e.listObjects()
	if lerr != nil && e.B.K8s() { // échec FERMÉ : sans liste fiable, un StatefulSet existant ne doit jamais être écrasé (docker : « create » d'un nom pris échoue seul)
		return errors.New("lecture des projets impossible : réessaie dans un instant")
	}
	for _, o := range objs {
		if o.Name == e.ObjectName(cat, name) {
			return errors.New("un projet porte déjà ce nom")
		}
	}
	if e.B.K8s() {
		raw, err := e.userManifestN(proj, env, 0)
		if err != nil {
			return err
		}
		c := e.kcmd("create", "-f", "-") // « create » et non « apply » : échoue si l'objet existe, jamais mis à jour ni arrêté
		c.Stdin = bytes.NewReader(raw)
		out, err := combinedOut(c)
		if err != nil {
			if pe := ExplainApplyError(string(out)); pe != nil {
				return pe
			}
			return errors.New("création du projet impossible : " + ScrubMessage(firstLine(string(out), err)))
		}
		return nil
	}
	args, err := e.userRunArgs(e.ObjectName(cat, name), proj, env)
	if err != nil {
		return err
	}
	if out, err := combinedOut(e.cmd(args...)); err != nil {
		return errors.New("création du projet impossible : " + ScrubMessage(firstLine(string(out), err)))
	}
	return nil
}

// StartUserProject : démarre le projet arrêté (k8s : 1 réplique ; docker/podman : start). Jamais appelé par le sondage : seulement sur Ouvrir ou F5.
func (e Engine) StartUserProject(cat, name string) error {
	if err := e.ownerOf(e.ObjectName(cat, name)); err != nil {
		return err
	}
	return e.startRaw(cat, name)
}

// startRaw : démarrage sans contrôle de propriétaire (objet qu'on vient de créer).
func (e Engine) startRaw(cat, name string) error {
	obj := e.ObjectName(cat, name)
	if e.B.K8s() {
		if out, err := combinedOut(e.kcmd("scale", "statefulset", obj, "--replicas=1")); err != nil {
			return errors.New("démarrage impossible : " + ScrubMessage(firstLine(string(out), err)))
		}
		return nil
	}
	if out, err := combinedOut(e.cmd("start", obj)); err != nil {
		return errors.New("démarrage impossible : " + ScrubMessage(firstLine(string(out), err)))
	}
	return nil
}

// StopUserProject : arrête le projet (k8s : 0 réplique ; docker/podman : stop) — action explicite de l'utilisateur (Fermer), jamais automatique.
func (e Engine) StopUserProject(cat, name string) error {
	obj := e.ObjectName(cat, name)
	if e.B.K8s() {
		if err := e.VerifyOwnerK8s(obj); err != nil {
			return err
		}
		return e.kcmd("scale", "statefulset", obj, "--replicas=0").Run()
	}
	if err := e.VerifyOwner(e.B.uid, obj); err != nil {
		return err
	}
	return e.cmd("stop", obj).Run()
}

// ── options d'un projet existant (§5 bis) ─────────────────────────────────

// UserProjectValues : valeurs COURANTES des options du projet, lues sur son objet. k8s : cpu/memory (demandes, limites) et ephemeral ; docker/podman :
// cpus, memory, pids_limit, shm_size, gpus.
func (e Engine) UserProjectValues(cat, name string) (map[string]string, error) {
	obj := e.ObjectName(cat, name)
	v := map[string]string{}
	if e.B.K8s() {
		out, err := e.output(e.kcmdD(userReadTimeout, "get", "statefulset", obj, "-o", "json"), maxListOut)
		if err != nil {
			return nil, errors.New("lecture du projet impossible")
		}
		var sts struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Resources struct {
								Requests, Limits map[string]string
							} `json:"resources"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		if json.Unmarshal(out, &sts) != nil || len(sts.Spec.Template.Spec.Containers) == 0 || sts.Metadata.Labels["cl.user"] != e.B.uid {
			return nil, errors.New("projet illisible ou d'un autre utilisateur")
		}
		r := sts.Spec.Template.Spec.Containers[0].Resources
		v["cpuRequest"], v["memRequest"] = r.Requests["cpu"], r.Requests["memory"]
		v["cpuLimit"], v["memLimit"], v["ephemeral"] = r.Limits["cpu"], r.Limits["memory"], r.Limits["ephemeral-storage"]
		return v, nil
	}
	if err := e.VerifyOwner(e.B.uid, obj); err != nil {
		return nil, err
	}
	out, err := e.output(e.cmdD(userReadTimeout, "inspect", "-f", "{{.HostConfig.NanoCpus}}|{{.HostConfig.Memory}}|{{.HostConfig.PidsLimit}}|{{.HostConfig.ShmSize}}", obj), maxSmallOut)
	if err != nil {
		return nil, errors.New("lecture du projet impossible")
	}
	f := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(f) != 4 {
		return nil, errors.New("projet illisible")
	}
	if n, _ := strconv.ParseFloat(f[0], 64); n > 0 {
		v["cpus"] = strconv.FormatFloat(n/1e9, 'f', -1, 64)
	}
	if n, _ := strconv.ParseInt(f[1], 10, 64); n > 0 {
		v["memory"] = strconv.FormatInt(n>>20, 10) + "m"
	}
	if f[2] != "" && f[2] != "0" {
		v["pids_limit"] = f[2]
	}
	if n, _ := strconv.ParseInt(f[3], 10, 64); n > 0 {
		v["shm_size"] = strconv.FormatInt(n>>20, 10) + "m"
	}
	return v, nil
}

// ApplyPodOptions (k8s) : patch du StatefulSet — ressources du conteneur « main », nodeSelector, tolérances, affinité, construits depuis les
// options VALIDÉES ; appliqué au prochain démarrage du pod. Jamais un objet recopié du client.
func (e Engine) ApplyPodOptions(cat, name string, po PodOptions) error {
	obj := e.ObjectName(cat, name)
	if err := e.VerifyOwnerK8s(obj); err != nil {
		return err
	}
	spec := map[string]any{"containers": []any{map[string]any{"name": "main", "resources": po.Resources()}}}
	po.Apply(spec)
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"spec": spec}}})
	if out, err := combinedOut(e.kcmd("patch", "statefulset", obj, "--type=strategic", "-p", string(patch))); err != nil {
		return errors.New("modification impossible : " + ScrubMessage(firstLine(string(out), err)))
	}
	return nil
}

// UpdateContainerHot (docker/podman) : cpus, memory et pids_limit changés À CHAUD par « update », sans recréer.
func (e Engine) UpdateContainerHot(cat, name string, c ContainerOptions) error {
	obj := e.ObjectName(cat, name)
	if err := e.VerifyOwner(e.B.uid, obj); err != nil {
		return err
	}
	args := []string{"update"}
	for _, kv := range [][2]string{{"--cpus=", c.Cpus}, {"--memory=", c.Memory}, {"--memory-swap=", c.Memory}, {"--pids-limit=", c.PidsLimit}} {
		if kv[1] != "" {
			args = append(args, kv[0]+kv[1])
		}
	}
	if len(args) == 1 {
		return nil
	}
	if out, err := combinedOut(e.cmd(append(args, obj)...)); err != nil {
		return errors.New("modification impossible : " + ScrubMessage(firstLine(string(out), err)))
	}
	return nil
}

// RecreateWithOptions (docker/podman) : recrée le conteneur avec de nouvelles options (shm_size, gpus : non modifiables à chaud) après
// confirmation par jeton. createArgs : l'argv de « create » du nouveau conteneur.
func (e Engine) RecreateArgs(cat, name, image string, c ContainerOptions) ([]string, error) {
	return e.userRunArgsOpts(e.ObjectName(cat, name), UserProjectDir(cat, name), EnvSpec{Image: image}, c.Args())
}

// UserDefaultImage : image par défaut d'un projet d'utilisateur (defaults.image de la configuration de la plateforme, sinon la première image
// proposée) ; jusqu'au LOT-3.3 c'est la SEULE image admise pour un projet (env.yaml : CheckUserEnv).
func UserDefaultImage() string {
	if _, img := ExecDefaults(); img != "" {
		return img
	}
	if l := BaseImages(); len(l) > 0 {
		return l[0]
	}
	return "docker.io/library/debian:12"
}

// ensureUser : Ensure d'un backend d'utilisateur — jamais le chemin de l'admin : constructeurs dédiés (création puis démarrage), image validée,
// propriétaire contrôlé. proj : « /space/PROJETS/<catégorie>/<projet> ». Rend le nom du conteneur (k8s : du pod).
func (e Engine) ensureUser(proj string, env EnvSpec, log io.Writer) (string, error) {
	rest, ok := strings.CutPrefix(proj, "/space/PROJETS/")
	cat, name, ok2 := strings.Cut(rest, "/")
	if !ok || !ok2 || ValidUserProjectName(cat) != "" || ValidUserProjectName(name) != "" {
		return "", errors.New("projet d'utilisateur : chemin invalide")
	}
	if env.Image == "" {
		env.Image = UserDefaultImage()
	}
	obj := e.ObjectName(cat, name)
	objs, _ := e.listObjects()
	exists := false
	for _, o := range objs {
		exists = exists || o.Name == obj
	}
	if !exists {
		fmt.Fprintf(log, "  création du projet %s/%s\n", cat, name)
		if err := e.CreateUserProject(cat, name, env.Image); err != nil {
			return "", err // ExplainApplyError déjà appliqué par CreateUserProject (PSA baseline)
		}
		if err := e.startRaw(cat, name); err != nil {
			return "", err
		}
	} else if err := e.StartUserProject(cat, name); err != nil {
		return "", err
	}
	if e.B.K8s() {
		return obj + "-0", nil
	}
	return obj, nil
}
