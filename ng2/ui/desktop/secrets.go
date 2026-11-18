package desktop

import (
	"encoding/json"
	"fmt"
	"strings"

	"claude-launcher-ng2/core"
)

// Secrets des backends (#194, contracts/backends-remote.md « Appels ») : clés ssh et known_hosts d'un backend ssh.
// Règle d'écriture provisoire de la v6.4.0 : tous les clients écrivent (fenêtre native, client web du hub, ingress
// interne) ; la clé privée n'est jamais renvoyée ; chaque écriture est journalisée (sans contenu) avec sa source.

// nativeSource : source journalisée des appels de la fenêtre native.
const nativeSource = "fenêtre native"

// secretMethods : appels du coffre.
var secretMethods = map[string]bool{"backendSecretGet": true, "backendSecretSet": true, "backendSecretDelete": true,
	"backendHostScan": true, "backendKnownHostsSet": true}

// DispatchFromAddr : DispatchFrom avec l'adresse source vue par le serveur (journal des écritures de secrets).
func (a *App) DispatchFromAddr(from origin, src, method string, raw json.RawMessage) (any, error) {
	if secretMethods[method] {
		if from != originWeb {
			src = nativeSource
		} else if src == "" {
			src = "navigateur"
		}
		return a.secretCall(src, method, raw)
	}
	return a.DispatchFrom(from, method, raw)
}

func (a *App) secretCall(src, method string, raw json.RawMessage) (any, error) {
	var in struct {
		Backend    string          `json:"backend"`
		PrivateKey string          `json:"privateKey"`
		Kubeconfig string          `json:"kubeconfig"`
		PublicKey  string          `json:"publicKey"`
		Generate   bool            `json:"generate"`
		Lines      json.RawMessage `json:"lines"`
		What       string          `json:"what"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
	}
	w := core.SecretWrite{Source: src}
	if method == "backendSecretSet" || method == "backendKnownHostsSet" || method == "backendHostScan" { // écriture et relevé : seulement pour un backend ssh déclaré
		if err := declaredFor(in.Backend, in.Kubeconfig != ""); err != nil {
			return map[string]any{"ok": false, "msg": err.Error()}, nil
		}
	}
	switch method {
	case "backendSecretGet":
		info, err := core.SecretGet(in.Backend)
		if src != nativeSource { // le chemin du coffre n'est montré qu'à la fenêtre native
			info.Dir = ""
		}
		return info, err
	case "backendSecretSet":
		if in.Kubeconfig != "" { // kubeconfig d'un backend k8s (#193) : contextes rendus, jamais le contenu
			ctxs, fp, err := core.SecretSetKubeconfig(in.Backend, in.Kubeconfig, w)
			if err != nil {
				return map[string]any{"ok": false, "msg": err.Error()}, nil
			}
			return map[string]any{"ok": true, "fingerprint": fp, "contexts": ctxs, "msg": "kubeconfig enregistré"}, nil
		}
		fp, pub, err := core.SecretSetKey(in.Backend, in.PrivateKey, in.PublicKey, in.Generate, w)
		if err != nil {
			return map[string]any{"ok": false, "msg": err.Error()}, nil
		}
		msg := "clé enregistrée"
		if in.Generate {
			msg = "paire générée : copier la clé publique dans authorized_keys de l'hôte"
		}
		return map[string]any{"ok": true, "fingerprint": fp, "publicKey": pub, "msg": msg}, nil
	case "backendHostScan":
		dest := "" // l'adresse est celle du backend déclaré, jamais celle du client
		if bs, err := core.LoadBackends(); err == nil {
			for _, b := range bs {
				if b.Name == in.Backend && b.Type == "ssh" {
					dest = b.Host
				}
			}
		}
		if dest == "" {
			return map[string]any{"ok": false, "msg": "backend ssh sans adresse"}, nil
		}
		keys, err := core.HostScan(dest)
		if err != nil {
			return map[string]any{"ok": false, "msg": err.Error()}, nil
		}
		return map[string]any{"ok": true, "keys": keys}, nil
	case "backendKnownHostsSet":
		var lines string
		if len(in.Lines) > 0 {
			if json.Unmarshal(in.Lines, &lines) != nil { // tableau de lignes admis
				var arr []string
				if err := json.Unmarshal(in.Lines, &arr); err != nil {
					return nil, fmt.Errorf("%s : « lines » : texte ou tableau attendu", method)
				}
				lines = strings.Join(arr, "\n")
			}
		}
		n, err := core.SecretSetKnownHosts(in.Backend, lines, w)
		if err != nil {
			return map[string]any{"ok": false, "msg": err.Error()}, nil
		}
		return map[string]any{"ok": true, "msg": fmt.Sprintf("%d empreinte(s) enregistrée(s)", n)}, nil
	case "backendSecretDelete":
		if err := core.SecretDelete(in.Backend, in.What, w); err != nil {
			return map[string]any{"ok": false, "msg": err.Error()}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	return nil, fmt.Errorf("%s : inconnu", method)
}

// declaredFor : le backend est déclaré, de type k8s pour un kubeconfig, sinon ssh.
func declaredFor(name string, kube bool) error {
	if kube {
		return backendDeclared(name, "k8s", "le kubeconfig ne concerne que les backends k8s")
	}
	return sshBackendDeclared(name)
}

func sshBackendDeclared(name string) error {
	return backendDeclared(name, "ssh", "les clés ssh ne concernent que les backends ssh")
}

// backendDeclared : le backend existe dans backends.yaml avec ce type (enregistrer le backend d'abord).
func backendDeclared(name, typ, why string) error {
	bs, err := core.LoadBackends()
	if err != nil {
		return err
	}
	for _, b := range bs {
		if b.Name == name {
			if b.Type != typ {
				return fmt.Errorf("backend %q : %s", name, why)
			}
			return nil
		}
	}
	return fmt.Errorf("backend %q inconnu : l'enregistrer dans les réglages avant d'y déposer une clé", name)
}
