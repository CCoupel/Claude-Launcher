package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Création atomique d'un backend d'utilisateur (contracts/user-backends.md §3 « Création », GATE LOT-3.1) : le backend et ses identifiants
// se créent dans UN envoi. Validation COMPLÈTE avant toute écriture ; écriture tout ou rien (coffre, puis backends.yaml ; si le YAML
// échoue, le coffre est retiré).

// AddRequest : requête userBackendAdd. Backend : la déclaration envoyée par le client (jamais « kubeconfig », posé par le serveur).
type AddRequest struct {
	Backend map[string]any
	Secrets struct {
		Kubeconfig string
		TLS        struct{ CA, Cert, Key string }
	}
	MTime string
}

// AddResult : succès de userBackendAdd.
type AddResult struct {
	Name, Type string
	Warnings   []string
}

const msgConflictMTime = "le fichier a changé depuis l'ouverture de l'éditeur : rouvre l'éditeur"

func aerr(field, code, msg string) *AddError { return &AddError{Field: field, Code: code, Msg: msg} }

// hostAddError : refus d'hôte en {field, code, msg} — forme : « invalid » avec son message ; plage / DNS : « forbidden_address », texte unique.
func hostAddError(field string, err error) *AddError {
	e := &AddError{Field: field, Code: "invalid", Msg: HostMessage(err), Reason: HostRefusalCategory(err)}
	if HostRefusalUniform(err) {
		e.Code = "forbidden_address"
	}
	return e
}

var addAllowed = map[string]bool{"name": true, "type": true, "namespace": true, "pvc": true, "host": true, "volume": true, "tls": true, "pod": true}

// validateAddDeclaration : liste blanche de la déclaration, formats, hôte borné (DNS compris). Rend la déclaration à écrire (sans secrets).
func validateAddDeclaration(be map[string]any) (name, typ string, decl map[string]any, e *AddError) {
	for k := range be {
		if !addAllowed[k] {
			return "", "", nil, aerr(k, "forbidden_field", "champ non admis ou posé par le serveur")
		}
	}
	str := func(k string) string { v, _ := be[k].(string); return v }
	name, typ = str("name"), str("type")
	if err := CheckUserBackendName(name); err != nil {
		return "", "", nil, aerr("name", "invalid", err.Error())
	}
	decl = map[string]any{"type": typ}
	switch typ {
	case "k8s":
		for _, k := range []string{"host", "volume", "tls"} {
			if _, has := be[k]; has {
				return "", "", nil, aerr(k, "forbidden_field", "champ propre aux backends docker")
			}
		}
		ns := str("namespace")
		switch {
		case ns == "":
			return "", "", nil, aerr("namespace", "required", "namespace obligatoire (nom DNS-1123)")
		case !dnsLabelRE.MatchString(ns):
			return "", "", nil, aerr("namespace", "invalid", "namespace : un nom DNS-1123 (minuscules, chiffres, tirets, 63 caractères au plus)")
		}
		decl["namespace"] = ns
		if pvc := str("pvc"); pvc != "" {
			if !dnsLabelRE.MatchString(pvc) {
				return "", "", nil, aerr("pvc", "invalid", "pvc : un nom DNS-1123 (minuscules, chiffres, tirets, 63 caractères au plus)")
			}
			decl["pvc"] = pvc
		}
		if p, has := be["pod"]; has && p != nil {
			pod, ok := p.(map[string]any)
			if !ok {
				return "", "", nil, aerr("pod", "invalid", "pod : un objet attendu")
			}
			for k := range pod {
				if k != "resources" && k != "nodeSelector" && k != "affinity" {
					return "", "", nil, aerr("pod", "forbidden_field", "pod : seuls resources, nodeSelector et affinity sont admis")
				}
			}
			decl["pod"] = pod
		}
	case "docker", "podman":
		for _, k := range []string{"namespace", "pvc", "pod"} {
			if _, has := be[k]; has {
				return "", "", nil, aerr(k, "forbidden_field", "champ propre aux backends k8s")
			}
		}
		host := str("host")
		if strings.TrimSpace(host) == "" {
			return "", "", nil, aerr("host", "required", "adresse obligatoire (tcp://hôte:port)")
		}
		h, _, err := parseUserEndpoint(host, "tcp")
		if err == nil {
			err = checkResolved(h)
		}
		if err != nil {
			return "", "", nil, hostAddError("host", err)
		}
		decl["host"] = strings.TrimSuffix(strings.TrimSpace(host), "/")
		if v := str("volume"); v != "" {
			if !volumeNameRE.MatchString(v) {
				return "", "", nil, aerr("volume", "invalid", "volume : un nom de volume seulement (lettres, chiffres, « _ », « . », « - » ; jamais un chemin)")
			}
			decl["volume"] = v
		}
		if t, has := be["tls"]; has && t != nil {
			tm, ok := t.(map[string]any)
			if !ok {
				return "", "", nil, aerr("tls", "invalid", "tls : un objet {enabled, verify} attendu")
			}
			out := map[string]any{}
			for k, v := range tm {
				b, isBool := v.(bool)
				if (k != "enabled" && k != "verify") || !isBool {
					return "", "", nil, aerr("tls", "forbidden_field", "tls : seuls enabled et verify (booléens) sont admis")
				}
				out[k] = b
			}
			if typ == "podman" && out["enabled"] == true {
				return "", "", nil, aerr("tls", "invalid", "podman n'a pas de TLS : le placer derrière un proxy TLS et déclarer un backend docker")
			}
			decl["tls"] = out
		}
	case "ssh":
		return "", "", nil, aerr("type", "forbidden_field", SSHRefusal)
	default:
		return "", "", nil, aerr("type", "invalid", "type : k8s, docker ou podman")
	}
	return name, typ, decl, nil
}

func tlsOn(decl map[string]any) (on, verify bool) {
	t, _ := decl["tls"].(map[string]any)
	on, _ = t["enabled"].(bool)
	verify = true
	if v, has := t["verify"].(bool); has {
		verify = v
	}
	return on, verify
}

// AddBackend : crée le backend et ses secrets, tout ou rien (voir l'en-tête). Erreur : refus de validation nommé.
func (s Space) AddBackend(req AddRequest, w SecretWrite) (*AddResult, *AddError) {
	if s.Admin {
		return nil, aerr("", "forbidden_field", "espace de l'admin : éditeur actuel")
	}
	name, typ, decl, e := validateAddDeclaration(req.Backend)
	if e != nil {
		return nil, e
	}
	// secrets obligatoires selon le type, puis contrôle des contenus
	var files map[string][]byte
	var warnings []string
	var secretKind, fp string
	switch typ {
	case "k8s":
		kc := normKey(req.Secrets.Kubeconfig)
		switch {
		case strings.TrimSpace(string(kc)) == "":
			return nil, aerr("kubeconfig", "required", "kubeconfig obligatoire pour un backend k8s")
		case len(kc) > maxUserKubeconfig:
			return nil, aerr("kubeconfig", "too_large", "kubeconfig trop volumineux (64 Kio au plus)")
		}
		_, _, ws, err := parseUserKubeconfig(kc)
		if err != nil {
			if HostRefusalCategory(err) != "" {
				return nil, hostAddError("kubeconfig", err)
			}
			code := "invalid"
			if strings.Contains(err.Error(), "refusé") {
				code = "forbidden_field"
			}
			return nil, aerr("kubeconfig", code, ScrubMessage(err.Error()))
		}
		warnings = append(warnings, ws...)
		files, secretKind = map[string][]byte{secretKubeFile: kc}, "kubeconfig"
		sum := sha256.Sum256(kc)
		fp = "sha256:" + hex.EncodeToString(sum[:])[:16]
		decl["kubeconfig"] = true // posé par le serveur, jamais par le client
	default:
		on, verify := tlsOn(decl)
		if on {
			t := req.Secrets.TLS
			switch {
			case t.Cert == "":
				return nil, aerr("tls.cert", "required", "certificat du client obligatoire (TLS activé)")
			case t.Key == "":
				return nil, aerr("tls.key", "required", "clé du client obligatoire (TLS activé)")
			case verify && t.CA == "":
				return nil, aerr("tls.ca", "required", "CA du serveur obligatoire pour vérifier son certificat")
			}
			var infos map[string]TLSCertInfo
			var pe *AddError
			if files, infos, pe = parseTLSMaterial(t.CA, t.Cert, t.Key); pe != nil {
				return nil, pe
			}
			ws, _ := tlsExpiry(infos, time.Now())
			warnings = append(warnings, ws...)
			secretKind = "tls"
			sum := sha256.New()
			for _, n := range []string{secretCAFile, secretCertFile, secretKeyPEM} {
				sum.Write(files[n])
			}
			fp = "sha256:" + hex.EncodeToString(sum.Sum(nil))[:16]
		}
		if !on || !verify {
			warnings = append(warnings, RootOnHostWarning)
		}
	}
	// mtime, nom libre, limite : validés avant toute écriture, puis REVÉRIFIÉS sous le verrou
	if err := s.Ensure(); err != nil {
		return nil, aerr("", "invalid", "espace illisible")
	}
	path := s.BackendsPath()
	var created string
	var res *AddResult
	var fail *AddError
	werr := WithFileLock(path, func() error {
		cur, rerr := os.ReadFile(path)
		if rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
		st, serr := os.Stat(path)
		var expected time.Time
		if req.MTime != "" {
			var perr error
			if expected, perr = time.Parse(time.RFC3339Nano, req.MTime); perr != nil {
				fail = aerr("", "conflict", msgConflictMTime)
				return nil
			}
		}
		if (serr == nil && (expected.IsZero() || !st.ModTime().Equal(expected))) || (serr != nil && !expected.IsZero()) {
			fail = aerr("", "conflict", msgConflictMTime)
			return nil
		}
		var existing struct {
			Backends map[string]any `yaml:"backends"`
		}
		_ = yaml.Unmarshal(cur, &existing)
		if _, taken := existing.Backends[name]; taken {
			fail = aerr("name", "conflict", "un backend porte déjà ce nom")
			return nil
		}
		if max := UserMaxBackends(); len(existing.Backends)+1 > max {
			fail = aerr("", "limit", fmt.Sprintf("nombre maximal de backends atteint (%d)", max))
			return nil
		}
		out, aerrApply := ApplyConfigChanges(ConfBackends, cur, []ConfigChange{{Op: "set", At: []string{"backends", name}, Value: decl}})
		if aerrApply != nil {
			fail = aerr("", "invalid", ScrubMessage(aerrApply.Error()))
			return nil
		}
		if ps := s.ValidateBackends(out); HasErrors(ps) { // validation complète du fichier résultant
			for _, p := range ps {
				if p.Severity == SevError {
					fail = aerr("", "invalid", p.Message)
					fail.Reason = p.Cat
					return nil
				}
			}
		}
		// étape 1 : le coffre — le dossier ne doit pas exister (même sans secret : un reste d'un ancien backend n'est jamais repris)
		if _, lerr := os.Lstat(filepath.Join(s.VaultDir(), name)); lerr == nil {
			fail = aerr("name", "conflict", "un coffre existe déjà pour ce nom : choisir un autre nom")
			return nil
		}
		if len(files) > 0 {
			dir := filepath.Join(s.VaultDir(), name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				fail = aerr("name", "conflict", "un coffre existe déjà pour ce nom")
				return nil
			}
			created = dir
			_ = os.Chmod(dir, 0o700)
			if err := journalSecret(name, secretKind, "set", fp, w.Source, w.User); err != nil {
				return err
			}
			for n, b := range files {
				if err := writeSecretFile(filepath.Join(dir, n), b); err != nil {
					return err
				}
			}
		}
		// étape 2 : backends.yaml (écriture atomique, contrôle de la date, .bak)
		if err := saveBackendsLocked(path, out, expected); err != nil {
			if errors.Is(err, ErrConfigConflict) {
				fail = aerr("", "conflict", msgConflictMTime)
				return nil
			}
			return err
		}
		created = "" // tout est écrit : rien à retirer
		res = &AddResult{Name: name, Type: typ, Warnings: warnings}
		return nil
	})
	if created != "" { // étape 2 manquée : plus de secret sans son backend
		_ = os.RemoveAll(created)
	}
	if fail != nil {
		return nil, fail
	}
	if werr != nil {
		return nil, aerr("", "invalid", "écriture impossible")
	}
	return res, nil
}

// RemoveBackend : retire le backend de l'utilisateur (déclaration, coffre, traces) ; rien n'est supprimé sur le backend lui-même.
func (s Space) RemoveBackend(name string, w SecretWrite) *AddError {
	if s.Admin {
		return aerr("", "forbidden_field", "espace de l'admin : éditeur actuel")
	}
	if err := CheckUserBackendName(name); err != nil {
		return aerr("name", "invalid", err.Error())
	}
	path := s.BackendsPath()
	var fail *AddError
	err := WithFileLock(path, func() error {
		cur, rerr := os.ReadFile(path)
		if rerr != nil {
			fail = aerr("name", "invalid", "backend inconnu")
			return nil
		}
		var existing struct {
			Backends map[string]any `yaml:"backends"`
		}
		_ = yaml.Unmarshal(cur, &existing)
		if _, ok := existing.Backends[name]; !ok {
			fail = aerr("name", "invalid", "backend inconnu")
			return nil
		}
		out, aerrApply := ApplyConfigChanges(ConfBackends, cur, []ConfigChange{{Op: "delete", At: []string{"backends", name}}})
		if aerrApply != nil {
			return aerrApply
		}
		st, serr := os.Stat(path)
		if serr != nil {
			return serr
		}
		return saveBackendsLocked(path, out, st.ModTime())
	})
	if fail != nil {
		return fail
	}
	if err != nil {
		return aerr("", "invalid", "suppression impossible")
	}
	if d, derr := s.vaultDirOf(name, false); derr == nil {
		_ = journalSecret(name, "backend", "delete", "", w.Source, w.User)
		_ = os.RemoveAll(d)
	}
	_, _ = s.PurgeBackendTraces(name)
	return nil
}
