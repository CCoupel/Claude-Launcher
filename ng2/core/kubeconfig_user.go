package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Kubeconfig d'un UTILISATEUR, durci (LOT-3.1 0c : S10, S13, S17–S21, S25). Mêmes refus que parseKubeconfig, plus : taille avant
// analyse, alias/ancres, clés en double, profondeur, proxy-url, fichiers du poste sous toutes leurs formes, « server: » https borné
// (tous les clusters). Les messages ne citent JAMAIS le contenu collé (un message d'analyse YAML cite des lignes du fichier : A6).

const maxKubeconfigDepth = 20

var errKubeUnreadable = errors.New("kubeconfig illisible : clusters et contexts attendus (YAML valide, sans alias ni clé en double)")

// yamlShape : refuse alias, ancres et profondeur excessive avant tout décodage.
func yamlShape(n *yaml.Node, depth int) error {
	if n == nil {
		return nil
	}
	if depth > maxKubeconfigDepth || n.Kind == yaml.AliasNode || n.Anchor != "" {
		return errKubeUnreadable
	}
	for _, c := range n.Content {
		if err := yamlShape(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// userKubeFileKeys : clés qui lisent un fichier du poste (toutes formes, toutes casses).
var userKubeFileKeys = []string{"certificate-authority", "client-certificate", "client-key", "tokenFile", "token-file", "tokenfile"}

// parseUserKubeconfig : contextes, contexte courant, avertissements ; erreur générique si refusé.
func parseUserKubeconfig(raw []byte) (contexts []string, current string, warnings []string, err error) {
	if len(raw) > maxUserKubeconfig {
		return nil, "", nil, errors.New("kubeconfig trop volumineux (64 Kio au plus)")
	}
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil || yamlShape(&doc, 0) != nil {
		return nil, "", nil, errKubeUnreadable
	}
	var k map[string]any
	if yaml.Unmarshal(raw, &k) != nil || len(ciList(k, "clusters")) == 0 || len(ciList(k, "contexts")) == 0 {
		return nil, "", nil, errKubeUnreadable
	}
	for _, c := range ciList(k, "clusters") {
		cl, name := ciMap(c, "cluster"), ciStr(c, "name")
		for _, key := range []string{"proxy-url", "certificate-authority", "tls-server-name-file"} {
			if _, p := ciGet(cl, key); p {
				return nil, "", nil, errors.New("kubeconfig refusé : proxy ou fichier du poste dans un cluster (utiliser certificate-authority-data, sans proxy)")
			}
		}
		if err := CheckUserServer(ciStr(cl, "server")); err != nil {
			return nil, "", nil, err
		}
		if v, _ := ciGet(cl, "insecure-skip-tls-verify"); v == true {
			warnings = append(warnings, fmt.Sprintf("cluster %q : certificat de l'API non vérifié (insecure-skip-tls-verify)", name))
		}
	}
	for _, u := range ciList(k, "users") {
		user := ciMap(u, "user")
		for _, bad := range []string{"exec", "auth-provider"} {
			if _, p := ciGet(user, bad); p {
				return nil, "", nil, errors.New("kubeconfig refusé : exécution de commande (exec, auth-provider) ; fournir un jeton ou un certificat en ligne")
			}
		}
		for _, bad := range userKubeFileKeys[1:] {
			if _, p := ciGet(user, bad); p {
				return nil, "", nil, errors.New("kubeconfig refusé : fichier du poste ; utiliser la forme « -data » ou un jeton")
			}
		}
	}
	for _, c := range ciList(k, "contexts") {
		contexts = append(contexts, ciStr(c, "name"))
	}
	sort.Strings(contexts)
	sort.Strings(warnings)
	return contexts, ciStr(k, "current-context"), warnings, nil
}

// userKubeconfigServers : « server: » de chaque cluster d'un fichier du coffre (revérifiés avant chaque appel, D1).
func userKubeconfigRecheck(raw []byte) error {
	_, _, _, err := parseUserKubeconfig(raw)
	return err
}

var _ = strings.TrimSpace
