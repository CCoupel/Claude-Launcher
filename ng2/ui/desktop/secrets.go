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

// secretMethods : appels du coffre.
var secretMethods = map[string]bool{"backendSecretGet": true, "backendSecretSet": true, "backendSecretDelete": true,
	"backendHostScan": true, "backendKnownHostsSet": true}

// DispatchFromAddr : DispatchFrom avec l'adresse source vue par le serveur (journal des écritures de secrets).
func (a *App) DispatchFromAddr(from origin, src, method string, raw json.RawMessage) (any, error) {
	if secretMethods[method] {
		if from != originWeb {
			src = "fenêtre native"
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
		Host       string          `json:"host"`
		PrivateKey string          `json:"privateKey"`
		PublicKey  string          `json:"publicKey"`
		Generate   bool            `json:"generate"`
		Lines      json.RawMessage `json:"lines"`
		What       string          `json:"what"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
	}
	w := core.SecretWrite{Source: src}
	switch method {
	case "backendSecretGet":
		return core.SecretGet(in.Backend)
	case "backendSecretSet":
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
		dest := in.Host
		if bs, err := core.LoadBackends(); err == nil {
			for _, b := range bs {
				if b.Name == in.Backend && b.Type == "ssh" && b.Host != "" {
					dest = b.Host // le backend enregistré l'emporte sur l'adresse du formulaire
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
