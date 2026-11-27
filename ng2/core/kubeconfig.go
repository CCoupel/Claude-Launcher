package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── kubeconfig d'un backend k8s (#193, contracts/backends-remote.md) ──────
//
// « kubeconfig: true » : fichier du coffre (<CL_SECRETS_DIR>/<backend>/kubeconfig) ; « kubeconfig: /chemin » :
// fichier existant (Secret monté), hors partage. Priorité : kubeconfig du backend > ServiceAccount du pod (en
// cluster) > KUBECONFIG / ~/.kube/config. Jamais de repli silencieux : un fichier illisible met le backend en
// erreur (kubectl reçoit toujours --kubeconfig).

const secretKubeFile = "kubeconfig"

// KubeconfigRef : valeur de « kubeconfig » dans backends.yaml (true ou un chemin).
type KubeconfigRef struct {
	Vault bool
	Path  string
}

func (k KubeconfigRef) set() bool { return k.Vault || k.Path != "" }

func (k *KubeconfigRef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return errors.New("« kubeconfig » : true ou un chemin attendu")
	}
	switch n.Tag {
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return err
		}
		*k = KubeconfigRef{Vault: b}
	case "!!null":
		*k = KubeconfigRef{}
	default:
		*k = KubeconfigRef{Path: strings.TrimSpace(n.Value)}
	}
	return nil
}

func (k KubeconfigRef) MarshalYAML() (any, error) {
	switch {
	case k.Vault:
		return true, nil
	case k.Path != "":
		return k.Path, nil
	}
	return nil, nil
}

// IsZero : omitempty.
func (k KubeconfigRef) IsZero() bool { return !k.set() }

// asAny : forme du formulaire (true ou chemin ; nil : aucun).
func (k KubeconfigRef) asAny() any {
	switch {
	case k.Vault:
		return true
	case k.Path != "":
		return k.Path
	}
	return nil
}

func kubeconfigFromAny(v any) KubeconfigRef {
	switch x := v.(type) {
	case bool:
		return KubeconfigRef{Vault: x}
	case string:
		if t := strings.TrimSpace(x); t == "true" {
			return KubeconfigRef{Vault: true}
		} else if t != "" && t != "false" {
			return KubeconfigRef{Path: t}
		}
	}
	return KubeconfigRef{}
}

// hasKubeconfig : le backend porte son propre kubeconfig.
func (b BackendConf) hasKubeconfig() bool { return b.K8s() && b.Kubeconfig.set() }

// kubeconfigFile : fichier à passer à kubectl (--kubeconfig). Pour « true », celui du coffre ; le coffre doit être
// sain à l'usage. Un chemin sous une zone partagée est refusé. Le chemin est rendu même si le fichier manque :
// kubectl échoue alors (aucun repli).
func (b BackendConf) kubeconfigFile() (string, error) {
	if b.user { // backend d'utilisateur : SON coffre seulement (users/<uid>/vault/<backend>/kubeconfig), jamais le coffre commun (S16)
		if !b.Kubeconfig.Vault {
			return "", errors.New("kubeconfig: true obligatoire pour un backend d'utilisateur (aucun repli sur le ServiceAccount du launcher)")
		}
		if err := CheckUserBackendName(b.Name); err != nil {
			return "", err
		}
		if b.vault == "" {
			return "", errors.New("coffre de l'utilisateur inconnu")
		}
		return filepath.Join(b.vault, b.Name, secretKubeFile), nil
	}
	switch {
	case b.Kubeconfig.Vault:
		if checkBackendName(b.Name) != nil {
			return "", fmt.Errorf("nom de backend invalide pour le coffre : %q", b.Name)
		}
		if secretsDirUnsafe() {
			return "", errors.New("coffre des secrets refusé (zone partagée ou droits trop larges) : kubeconfig non utilisé")
		}
		return filepath.Join(secretsDirRaw(), b.Name, secretKubeFile), nil
	case b.Kubeconfig.Path != "":
		p := b.Kubeconfig.Path
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("kubeconfig : un chemin absolu est attendu (%q)", p)
		}
		if kubeconfigPathAllowed(p) {
			return p, nil
		}
		for _, r := range sharedZones() {
			if pathWithin(p, r) {
				return "", fmt.Errorf("kubeconfig refusé : %s est sous la zone partagée %s (admis : le coffre des secrets ou un dossier PRIVATE/ de projet)", p, r)
			}
		}
		return p, nil
	}
	return "", nil
}

// kubeconfigPathAllowed : le chemin est dans le coffre commun, ou dans un dossier PRIVATE/ d'une zone partagée
// (<zone>/…/PRIVATE/…, forme résolue : un lien ne fait pas sortir de PRIVATE/). Admis même sous la racine partagée.
func kubeconfigPathAllowed(p string) bool {
	if pathWithin(p, secretsDirRaw()) {
		return true
	}
	rp := realPath(p)
	for _, z := range sharedZones() {
		rel, err := filepath.Rel(realPath(z), rp)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		segs := strings.Split(rel, string(filepath.Separator))
		for i := 0; i < len(segs)-1; i++ { // PRIVATE dans la zone, et le fichier DANS PRIVATE
			if segs[i] == "PRIVATE" {
				return true
			}
		}
	}
	return false
}

// kubeconfigArgs : « --kubeconfig <fichier> », après contrôle du fichier À CHAQUE appel (refus des commandes à
// exécuter et des fichiers du poste, même pour un fichier posé hors du launcher : Secret monté, chemin).
// Refus : erreur, kubectl n'est pas lancé (jamais de repli sur le ServiceAccount ni sur ~/.kube/config).
func (b BackendConf) kubeconfigArgs() ([]string, error) {
	f, err := b.kubeconfigFile()
	if err != nil {
		return nil, err
	}
	raw, rerr := os.ReadFile(f)
	if rerr != nil && b.user { // utilisateur : jamais « kubectl échoue seul » (aucun repli possible) — kubectl n'est pas lancé
		return nil, errors.New("kubeconfig de l'utilisateur absent ou illisible : le déposer dans les réglages du backend")
	}
	if rerr == nil && b.user { // utilisateur : mêmes contrôles qu'au dépôt, serveurs revérifiés (résolution mise en cache)
		if perr := userKubeconfigRecheck(raw); perr != nil {
			return nil, perr
		}
	} else if rerr == nil {
		if _, _, perr := parseKubeconfig(raw); perr != nil {
			return nil, perr
		}
	} // illisible : kubectl échoue seul sur ce fichier (message de kubectl), sans repli
	return []string{"--kubeconfig", f}, nil
}

// kubectlRefused : « commande » qui ne peut pas se lancer et dont l'erreur d'exécution porte la raison du refus
// (« exec: "kubeconfig refusé : … ": executable file not found ») : kubectl n'est pas démarré.
func kubectlRefused(err error) []string {
	r := strings.NewReplacer("/", " ", `\`, " ", "\n", " ")
	return []string{"kubeconfig-refuse: " + r.Replace(err.Error())}
}

// kubeconfigInfo : contextes et contexte courant d'un fichier kubeconfig ; erreur « illisible » si le fichier ne
// l'est pas ou n'est pas un kubeconfig.
func kubeconfigInfo(path string) (contexts []string, current string, err error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, "", fmt.Errorf("kubeconfig illisible sur ce poste : %s", firstLine("", rerr))
	}
	return parseKubeconfig(raw)
}

// ciGet : valeur d'une clé sans tenir compte de la casse (kubectl décode en JSON : « Exec » vaut « exec »).
func ciGet(m map[string]any, key string) (any, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func ciList(m map[string]any, key string) []map[string]any {
	v, _ := ciGet(m, key)
	l, _ := v.([]any)
	var out []map[string]any
	for _, e := range l {
		if em, ok := e.(map[string]any); ok {
			out = append(out, em)
		}
	}
	return out
}

func ciMap(m map[string]any, key string) map[string]any {
	v, _ := ciGet(m, key)
	r, _ := v.(map[string]any)
	return r
}

func ciStr(m map[string]any, key string) string {
	v, _ := ciGet(m, key)
	s, _ := v.(string)
	return s
}

// parseKubeconfig : contextes d'un kubeconfig. Refuse (au dépôt comme à chaque appel de kubectl) ce qui ferait
// exécuter une commande ou lire un fichier du poste : exec, auth-provider, chemins de certificats / jeton
// (utiliser « …-data »), quelle que soit la casse des clés.
func parseKubeconfig(raw []byte) (contexts []string, current string, err error) {
	var k map[string]any
	if yaml.Unmarshal(raw, &k) != nil || len(ciList(k, "clusters")) == 0 || len(ciList(k, "contexts")) == 0 {
		return nil, "", errors.New("kubeconfig illisible : clusters et contexts attendus")
	}
	for _, c := range ciList(k, "clusters") {
		if _, p := ciGet(ciMap(c, "cluster"), "certificate-authority"); p {
			return nil, "", errors.New("kubeconfig refusé : « certificate-authority » désigne un fichier du poste (utiliser certificate-authority-data)")
		}
	}
	for _, u := range ciList(k, "users") {
		user, name := ciMap(u, "user"), ciStr(u, "name")
		for _, bad := range []string{"exec", "auth-provider"} {
			if _, p := ciGet(user, bad); p {
				return nil, "", fmt.Errorf("kubeconfig refusé : l'utilisateur %q utilise %q (exécution de commande) ; fournir un jeton ou un certificat en ligne", name, bad)
			}
		}
		for _, bad := range []string{"client-certificate", "client-key", "tokenFile"} {
			if _, p := ciGet(user, bad); p {
				return nil, "", fmt.Errorf("kubeconfig refusé : l'utilisateur %q lit un fichier du poste (%s) ; utiliser la forme « -data » ou un jeton", name, bad)
			}
		}
	}
	for _, c := range ciList(k, "contexts") {
		contexts = append(contexts, ciStr(c, "name"))
	}
	sort.Strings(contexts)
	return contexts, ciStr(k, "current-context"), nil
}

// SecretSetKubeconfig : dépose le kubeconfig d'un backend k8s dans le coffre. Rend les contextes (jamais le
// contenu) et l'empreinte du contenu (SHA-256), seule trace journalisée.
func SecretSetKubeconfig(backend, content string, w SecretWrite) (contexts []string, fingerprint string, warnings []string, err error) {
	raw := normKey(content)
	if len(raw) > 256<<10 {
		return nil, "", nil, errors.New("kubeconfig trop volumineux (256 Ko au plus)")
	}
	if contexts, _, err = parseKubeconfig(raw); err != nil {
		return nil, "", nil, err
	}
	sum := sha256.Sum256(raw)
	fingerprint = "sha256:" + hex.EncodeToString(sum[:])[:16]
	d, err := secretBackendDir(backend, true)
	if err != nil {
		return nil, "", nil, err
	}
	if err := journalSecret(backend, "kubeconfig", "set", fingerprint, w.Source, w.User); err != nil {
		return nil, "", nil, err
	}
	if err := writeSecretFile(filepath.Join(d, secretKubeFile), raw); err != nil {
		return nil, "", nil, fmt.Errorf("écriture du kubeconfig : %w", err)
	}
	return contexts, fingerprint, kubeconfigWarnings(raw), nil
}

// kubeconfigSteps (#193) : « kubeconfig lisible » (fichier, contextes, contexte demandé présent). Faux : la suite
// du test ne peut pas s'appuyer sur ce kubeconfig.
func kubeconfigSteps(b BackendConf) (steps []TestStep, ok bool) {
	f, err := b.kubeconfigFile()
	if err != nil {
		return []TestStep{{Label: "kubeconfig lisible", Detail: err.Error(), Fix: "déposer le kubeconfig dans les réglages du backend, ou corriger son chemin"}}, false
	}
	ctxs, cur, err := kubeconfigInfo(f)
	if err != nil {
		return []TestStep{{Label: "kubeconfig lisible", Detail: err.Error(), Fix: "déposer à nouveau le kubeconfig du backend (kubectl ne se rabat pas sur le ServiceAccount)"}}, false
	}
	if b.Context != "" && !containsStr(ctxs, b.Context) {
		return []TestStep{{Label: "kubeconfig lisible", Detail: "contexte « " + b.Context + " » absent du kubeconfig (contextes : " + strings.Join(ctxs, ", ") + ")",
			Fix: "corriger « context » du backend, ou le retirer (contexte courant du fichier)"}}, false
	}
	d := fmt.Sprintf("%d contexte(s)", len(ctxs))
	if cur != "" {
		d += ", courant « " + cur + " »"
	}
	return []TestStep{{Label: "kubeconfig lisible", OK: true, Detail: d}}, true
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// kubeconfigWarnings : réglages qui affaiblissent la connexion, signalés au dépôt (le kubeconfig est accepté) :
// vérification TLS désactivée, proxy.
func kubeconfigWarnings(raw []byte) []string {
	var k map[string]any
	if yaml.Unmarshal(raw, &k) != nil {
		return nil
	}
	var out []string
	for _, c := range ciList(k, "clusters") {
		cl, name := ciMap(c, "cluster"), ciStr(c, "name")
		if v, _ := ciGet(cl, "insecure-skip-tls-verify"); v == true {
			out = append(out, fmt.Sprintf("cluster %q : insecure-skip-tls-verify — le certificat du serveur n'est pas vérifié", name))
		}
		if p := ciStr(cl, "proxy-url"); p != "" {
			out = append(out, fmt.Sprintf("cluster %q : proxy-url — le trafic passe par un proxy (%s)", name, p))
		}
	}
	sort.Strings(out)
	return out
}
