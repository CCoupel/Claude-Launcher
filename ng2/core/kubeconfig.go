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
		for _, r := range secretsForbiddenRoots() {
			if pathWithin(p, r) {
				return "", fmt.Errorf("kubeconfig refusé : %s est sous la zone partagée %s", p, r)
			}
		}
		return p, nil
	}
	return "", nil
}

// kubeconfigArgs : « --kubeconfig <fichier> » ; en cas de refus, un chemin qui n'existe pas (kubectl échoue,
// jamais de repli sur le ServiceAccount ni sur ~/.kube/config).
func (b BackendConf) kubeconfigArgs() []string {
	f, err := b.kubeconfigFile()
	if err != nil {
		f = filepath.Join(os.TempDir(), "cl-kubeconfig-refuse", "absent")
	}
	return []string{"--kubeconfig", f}
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

type kubeFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string         `yaml:"name"`
		Cluster map[string]any `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name string `yaml:"name"`
	} `yaml:"contexts"`
	Users []struct {
		Name string         `yaml:"name"`
		User map[string]any `yaml:"user"`
	} `yaml:"users"`
}

// parseKubeconfig : contextes d'un kubeconfig. Refuse (au dépôt comme à la lecture) ce qui ferait exécuter une
// commande ou lire un fichier du poste : exec, auth-provider, chemins de certificats / jeton (utiliser « …-data »).
func parseKubeconfig(raw []byte) (contexts []string, current string, err error) {
	var k kubeFile
	if yaml.Unmarshal(raw, &k) != nil || len(k.Clusters) == 0 || len(k.Contexts) == 0 {
		return nil, "", errors.New("kubeconfig illisible : clusters et contexts attendus")
	}
	for _, c := range k.Clusters {
		if _, p := c.Cluster["certificate-authority"]; p {
			return nil, "", errors.New("kubeconfig refusé : « certificate-authority » désigne un fichier du poste (utiliser certificate-authority-data)")
		}
	}
	for _, u := range k.Users {
		for _, bad := range []string{"exec", "auth-provider"} {
			if _, p := u.User[bad]; p {
				return nil, "", fmt.Errorf("kubeconfig refusé : l'utilisateur %q utilise %q (exécution de commande) ; fournir un jeton ou un certificat en ligne", u.Name, bad)
			}
		}
		for _, bad := range []string{"client-certificate", "client-key", "tokenFile"} {
			if _, p := u.User[bad]; p {
				return nil, "", fmt.Errorf("kubeconfig refusé : l'utilisateur %q lit un fichier du poste (%s) ; utiliser la forme « -data » ou un jeton", u.Name, bad)
			}
		}
	}
	for _, c := range k.Contexts {
		contexts = append(contexts, c.Name)
	}
	sort.Strings(contexts)
	return contexts, k.CurrentContext, nil
}

// SecretSetKubeconfig : dépose le kubeconfig d'un backend k8s dans le coffre. Rend les contextes (jamais le
// contenu) et l'empreinte du contenu (SHA-256), seule trace journalisée.
func SecretSetKubeconfig(backend, content string, w SecretWrite) (contexts []string, fingerprint string, err error) {
	raw := normKey(content)
	if len(raw) > 256<<10 {
		return nil, "", errors.New("kubeconfig trop volumineux (256 Ko au plus)")
	}
	if contexts, _, err = parseKubeconfig(raw); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	fingerprint = "sha256:" + hex.EncodeToString(sum[:])[:16]
	d, err := secretBackendDir(backend, true)
	if err != nil {
		return nil, "", err
	}
	if err := journalSecret(backend, "kubeconfig", "set", fingerprint, w.Source); err != nil {
		return nil, "", err
	}
	if err := writeSecretFile(filepath.Join(d, secretKubeFile), raw); err != nil {
		return nil, "", fmt.Errorf("écriture du kubeconfig : %w", err)
	}
	return contexts, fingerprint, nil
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
