package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Constructeur DÉDIÉ du manifeste k8s et de l'argv docker/podman d'un projet d'UTILISATEUR (LOT-3.2 E2, sécurité T1…T11 : liste blanche
// fermée). Il ne réutilise NI placePod, NI withResources, NI les clés « pod » du backend, NI l'argv du backend de l'admin : seuls les
// champs ci-dessous existent, tout le reste est absent. Pods et conteneurs NON root (2000:2000 par défaut), jamais 0.

// UsersRunAsVar : « uid:gid » posé par le chart (users.runAs) ; défaut 2000:2000.
const UsersRunAsVar = "CL_USERS_RUN_AS"

const defaultRunAs = 2000

// UserRunAs : identité des conteneurs de projet d'un utilisateur — chart (CL_USERS_RUN_AS), puis le backend (run_as, plus tard), jamais 0.
func UserRunAs() (uid, gid int64) {
	uid, gid = defaultRunAs, defaultRunAs
	if p := strings.SplitN(os.Getenv(UsersRunAsVar), ":", 2); len(p) == 2 {
		u, e1 := strconv.ParseInt(p[0], 10, 64)
		g, e2 := strconv.ParseInt(p[1], 10, 64)
		if e1 == nil && e2 == nil && validID(u) && validID(g) {
			uid, gid = u, g
		}
	}
	return uid, gid
}

// validID : identifiant d'utilisateur ou de groupe admis pour un conteneur d'utilisateur (jamais 0).
func validID(n int64) bool { return n >= 1 && n <= 4294967294 }

// runAs : identité d'un projet de ce backend — run_as du backend, sinon le réglage du chart, jamais root.
func (b BackendConf) runAs() (uid, gid int64) {
	if r := b.RunAs; r != nil && validID(r.UID) && validID(r.GID) {
		return r.UID, r.GID
	}
	return UserRunAs()
}

var (
	userSegRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	// image : référence de dépôt en minuscules (registre[:port]/chemin), étiquette facultative, condensé sha256 facultatif (64 hex)
	userImageRE = regexp.MustCompile(`^(?:[a-z0-9]+(?:[._-][a-z0-9]+)*(?::[0-9]+)?/)?[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@sha256:[a-f0-9]{64})?$`)
	userCtrRE   = regexp.MustCompile(`^cl-[a-z0-9][a-z0-9-]{0,59}$`)
)

// userProj : « /space/PROJETS/<catégorie>/<projet> » (chemin vu du conteneur) → catégorie et projet ; tout autre chemin est refusé.
func userProj(proj string) (cat, name string, err error) {
	rest, ok := strings.CutPrefix(proj, "/space/PROJETS/")
	if !ok {
		return "", "", errors.New("projet : chemin /space/PROJETS/<catégorie>/<projet> attendu")
	}
	p := strings.Split(rest, "/")
	if len(p) != 2 || !userSegRE.MatchString(p[0]) || !userSegRE.MatchString(p[1]) || p[0] == ".." || p[1] == ".." {
		return "", "", errors.New("projet : catégorie et nom simples attendus (lettres, chiffres, « . », « _ », « - »)")
	}
	return p[0], p[1], nil
}

// CheckUserImage : référence d'image admise pour un projet d'utilisateur — jamais un drapeau, un espace ni un caractère de shell.
func CheckUserImage(image string) error {
	if len(image) > 255 || !userImageRE.MatchString(image) {
		return errors.New("image refusée : référence d'image simple attendue (registre/nom:étiquette ou @sha256:…)")
	}
	return nil
}

// UserObjName : nom de l'objet (StatefulSet, conteneur) d'un projet d'utilisateur — « cl-<8 hex du condensé de l'uid>-<catégorie>-<projet> »,
// DNS-1123, 63 caractères au plus : deux utilisateurs ne se confondent jamais (T9).
func UserObjName(uid, cat, proj string) string {
	h := sha256.Sum256([]byte(uid))
	return "cl-" + hex.EncodeToString(h[:4]) + "-" + dnsSlug(cat, 20, "projets") + "-" + dnsSlug(proj, 30, "projet")
}

// userCommon : contrôles communs aux deux constructeurs.
func (e Engine) userCommon(proj string, env EnvSpec) (cat, name string, err error) {
	if !e.B.user || e.B.uid == "" {
		return "", "", errors.New("backend d'utilisateur attendu")
	}
	if cat, name, err = userProj(proj); err != nil {
		return "", "", err
	}
	if err := CheckUserImage(env.Image); err != nil {
		return "", "", err
	}
	if len(env.Mounts) > 0 || len(env.Local) > 0 { // montages hôte et dossiers « local » : jamais pour un utilisateur
		return "", "", errors.New("env.yaml : « mounts » et « local » ne sont pas admis sur un backend d'utilisateur")
	}
	return cat, name, nil
}

// userLabels : étiquettes d'un objet de projet d'utilisateur (cl.user, cl.category, cl.project : base de la découverte, §5).
func (e Engine) userLabels(obj, cat, name string) map[string]string {
	return map[string]string{"cl.launcher": "ng2", "cl.backend": e.B.Name, "cl.user": e.B.uid, "cl.category": cat, "cl.project": name, "cl.ctr": obj}
}

// userPodOptions : défauts du backend admis pour le pod — resources, nodeSelector, affinity SEULEMENT (liste blanche fermée).
func (e Engine) userPodOptions(spec, ctr map[string]any) {
	for k, v := range e.B.Pod {
		switch k {
		case "resources":
			ctr["resources"] = v
		case "nodeSelector", "affinity":
			spec[k] = v
		}
	}
}

// userManifest : StatefulSet d'un projet d'utilisateur. PVC DÉCLARÉ monté en subPath (dossier du projet et .claude), jamais la racine ;
// aucun PVC créé ; non root ; securityContext durci ; environnement et commande fixés par le launcher.
func (e Engine) userManifest(proj string, env EnvSpec) ([]byte, error) {
	cat, name, err := e.userCommon(proj, env)
	if err != nil {
		return nil, err
	}
	if !dnsLabelRE.MatchString(e.B.PVC) || !dnsLabelRE.MatchString(e.B.Namespace) {
		return nil, errors.New("backend k8s d'utilisateur : namespace et pvc obligatoires")
	}
	obj := UserObjName(e.B.uid, cat, name)
	uid, gid := e.B.runAs()
	labels := e.userLabels(obj, cat, name)
	ann := map[string]string{"cl.image": env.Image, "cl.mounts": mountsModel}
	envs := []map[string]string{{"name": "HOME", "value": "/space/.claude/home"}, {"name": "CLAUDE_CONFIG_DIR", "value": "/space/.claude"},
		{"name": "LANG", "value": "C.UTF-8"}, {"name": "DISABLE_AUTOUPDATER", "value": "1"}}
	ctr := map[string]any{
		"name": "main", "image": env.Image, "command": []string{"sh", "-c", pid1}, "workingDir": proj, "env": envs,
		"volumeMounts": []map[string]any{
			{"name": "space", "mountPath": proj, "subPath": "PROJETS/" + cat + "/" + name},
			{"name": "space", "mountPath": "/space/.claude", "subPath": ".claude"},
		},
		"securityContext": map[string]any{
			"runAsUser": uid, "runAsGroup": gid, "runAsNonRoot": true, "allowPrivilegeEscalation": false,
			"capabilities":           map[string]any{"drop": []string{"ALL"}},
			"seccompProfile":         map[string]any{"type": "RuntimeDefault"},
			"privileged":             false,
			"readOnlyRootFilesystem": false,
		},
	}
	spec := map[string]any{
		"terminationGracePeriodSeconds": 10, "automountServiceAccountToken": false, "enableServiceLinks": false,
		// fsGroup : le volume partagé reste écrivable par l'identité du projet (changement de propriétaire seulement si nécessaire)
		"securityContext": map[string]any{"fsGroup": gid, "fsGroupChangePolicy": "OnRootMismatch"},
		"containers":      []map[string]any{ctr},
		"volumes":         []map[string]any{{"name": "space", "persistentVolumeClaim": map[string]any{"claimName": e.B.PVC}}},
	}
	e.userPodOptions(spec, ctr)
	m := map[string]any{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]any{"name": obj, "labels": labels, "annotations": ann},
		"spec": map[string]any{
			"replicas": 1, "serviceName": obj,
			"selector": map[string]any{"matchLabels": map[string]string{"cl.ctr": obj}},
			"template": map[string]any{"metadata": map[string]any{"labels": labels, "annotations": ann}, "spec": spec},
		},
	}
	return json.Marshal(m)
}

// userRunArgs : argv « docker run » d'un projet d'utilisateur — volume nommé DÉCLARÉ monté sur /space (jamais un montage de l'hôte),
// non root, toutes capacités retirées, no-new-privileges ; aucune option libre, aucune valeur de l'utilisateur interprétée.
func (e Engine) userRunArgs(name, proj string, env EnvSpec) ([]string, error) {
	cat, pname, err := e.userCommon(proj, env)
	if err != nil {
		return nil, err
	}
	if !userCtrRE.MatchString(name) {
		return nil, errors.New("nom de conteneur invalide")
	}
	if !volumeNameRE.MatchString(e.B.Volume) {
		return nil, errors.New("backend docker d'utilisateur : volume obligatoire")
	}
	uid, gid := e.B.runAs()
	l := e.userLabels(name, cat, pname)
	// « create » puis « start » (contrat §5 bis) ; options en --opt=valeur, volume nommé par --mount=type=volume ; jamais « -v », jamais de
	// montage de l'hôte ; aucune valeur de l'utilisateur n'est un argument séparé qui pourrait se lire comme un drapeau
	args := []string{"create", "--name=" + name, "--hostname=" + dnsSlug(pname, 30, "projet"),
		"--label=cl.launcher=ng2", "--label=cl.backend=" + l["cl.backend"], "--label=cl.user=" + l["cl.user"],
		"--label=cl.category=" + cat, "--label=cl.project=" + pname, "--label=cl.image=" + env.Image,
		"--mount=type=volume,src=" + e.B.Volume + ",dst=/space", "--workdir=" + proj,
		"-e", "HOME=/space/.claude/home", "-e", "CLAUDE_CONFIG_DIR=/space/.claude", "-e", "LANG=C.UTF-8", "-e", "DISABLE_AUTOUPDATER=1",
		fmt.Sprintf("--user=%d:%d", uid, gid), "--cap-drop=ALL", "--security-opt=no-new-privileges"}
	return append(args, env.Image, "sh", "-c", pid1), nil
}
