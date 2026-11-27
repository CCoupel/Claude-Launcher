package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// LOT-3.1 cycle 2 (#257, GATE refusé B9/B12/B13) — création d'un backend d'UTILISATEUR en une action atomique (userBackendAdd,
// contracts/user-backends.md §3 « Création d'un backend d'utilisateur », §7) et éditeur utilisable après un refus. La séquence
// rejouée est celle de settings.js (stSaveUser) : configModel (mtime) → configApply (champs des backends EXISTANTS) → userBackendAdd par
// NOUVEAU backend, avec sa réponse {ok,backend,warnings} ou {ok:false,field,code,msg}. Écrits depuis le contrat, avant le code.

const (
	cPub  = "https://93.184.216.34:6443"
	cHost = "tcp://93.184.216.34:2376"
)

type cr258 struct{ *ub257 }

func cr258Start(t *testing.T) cr258 { return cr258{ub257Start(t)} }

func (c cr258) model(t *testing.T, user string) (mtime string, raw string, res map[string]any) {
	t.Helper()
	raw, res, _ = c.call(t, user, "configModel", map[string]any{"kind": "backends.yaml"})
	mtime, _ = res["mtime"].(string)
	return
}

func (c cr258) add(t *testing.T, user string, be map[string]any, secrets map[string]any, mtime string) (string, map[string]any, string) {
	t.Helper()
	return c.call(t, user, "userBackendAdd", map[string]any{"backend": be, "secrets": secrets, "mtime": mtime})
}

func cK8s(name string) map[string]any {
	return map[string]any{"name": name, "type": "k8s", "namespace": "ns-" + name, "pvc": "projets"}
}

func cDocker(name string, tls bool) map[string]any {
	b := map[string]any{"name": name, "type": "docker", "host": cHost, "volume": "v"}
	if tls {
		b["tls"] = map[string]any{"enabled": true, "verify": true}
	}
	return b
}

func vaultOf(uid, name string) string { return filepath.Join(core.UsersDir(), uid, "vault", name) }

func yamlOf(t *testing.T, uid string) (string, os.FileInfo) {
	t.Helper()
	p := filepath.Join(core.UsersDir(), uid, "backends.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", nil
	}
	st, _ := os.Stat(p)
	return string(b), st
}

// ── parcours « créer, tester, supprimer » ───────────────────────────────────────────────────────────────

func TestCreate258K8sCreerTesterSupprimer(t *testing.T) {
	c := cr258Start(t)
	mt, _, m0 := c.model(t, "alice") // ouverture de l'éditeur
	if m0 == nil || m0["exists"] != false {
		t.Fatalf("modèle initial : %v", m0)
	}
	raw, res, e := c.add(t, "alice", cK8s("mon-k8s"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}, mt)
	if e != "" || res["ok"] != true {
		t.Fatalf("création refusée : %s", raw)
	}
	if be, _ := res["backend"].(map[string]any); be["name"] != "mon-k8s" || be["type"] != "k8s" {
		t.Errorf("réponse sans backend {name,type} : %v", res)
	}
	if _, ok := res["warnings"]; !ok {
		t.Errorf("réponse sans champ warnings : %v", res)
	}
	// coffre : dossier 0700, fichier 0600 ; backends.yaml porte kubeconfig: true posé par le SERVEUR
	kc := filepath.Join(vaultOf("github-1111", "mon-k8s"), "kubeconfig")
	if st, err := os.Stat(kc); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("kubeconfig du coffre : %v %v", err, st)
	}
	if st, _ := os.Stat(vaultOf("github-1111", "mon-k8s")); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("dossier du coffre : %v", st)
	}
	y, _ := yamlOf(t, "github-1111")
	if !strings.Contains(y, "mon-k8s") || !strings.Contains(y, "kubeconfig: true") || strings.Contains(y, uCanary) {
		t.Errorf("backends.yaml : %s", y)
	}
	// relecture comme l'éditeur ; état du coffre sans contenu
	raw, _, _ = c.call(t, "alice", "configModel", map[string]any{"kind": "backends.yaml"})
	if !strings.Contains(raw, "mon-k8s") {
		t.Errorf("le backend n'est pas dans le modèle : %.300s", raw)
	}
	raw, _, _ = c.call(t, "alice", "backendSecretGet", map[string]any{"backend": "mon-k8s"})
	if strings.Contains(raw, uCanary) || strings.Contains(raw, "BEGIN") || !strings.Contains(strings.ToLower(raw), "kubeconfig") {
		t.Errorf("backendSecretGet : %.300s", raw)
	}
	// tester (action séparée), puis supprimer
	raw, _, _ = c.call(t, "alice", "backendTest", map[string]any{"backend": map[string]any{"name": "mon-k8s", "type": "k8s", "namespace": "ns-mon-k8s", "pvc": "projets", "kubeconfig": true}})
	if !strings.Contains(raw, "steps") || strings.Contains(raw, uCanary) {
		t.Errorf("backendTest : %.300s", raw)
	}
	if raw, res, e = c.call(t, "alice", "userBackendRemove", map[string]any{"name": "mon-k8s"}); e != "" || res["ok"] != true {
		t.Fatalf("suppression : %s", raw)
	}
	if raw, _, _ = c.call(t, "alice", "configModel", map[string]any{"kind": "backends.yaml"}); strings.Contains(raw, "mon-k8s") {
		t.Errorf("backend encore listé après suppression : %.300s", raw)
	}
	c.scanLeaks(t)
}

func TestCreate258DockerTLSCreer(t *testing.T) {
	c := cr258Start(t)
	ca, _ := ub257Cert(t, true)
	cert, key := ub257Cert(t, false)
	mt, _, _ := c.model(t, "alice")
	raw, res, e := c.add(t, "alice", cDocker("nas", true), map[string]any{"tls": map[string]any{"ca": ca, "cert": cert, "key": key}}, mt)
	if e != "" || res["ok"] != true {
		t.Fatalf("création docker TLS refusée : %s", raw)
	}
	for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if st, err := os.Stat(filepath.Join(vaultOf("github-1111", "nas"), f)); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s : %v %v", f, err, st)
		}
	}
	y, _ := yamlOf(t, "github-1111")
	if !strings.Contains(y, "enabled: true") || strings.Contains(y, "kubeconfig") || strings.Contains(y, "PRIVATE KEY") {
		t.Errorf("backends.yaml : %s", y)
	}
	for _, r := range c.seen {
		if strings.Contains(r, "BEGIN") || strings.Contains(r, "PRIVATE KEY") {
			t.Errorf("PEM dans une réponse : %.200s", r)
		}
	}
	// docker sans TLS : création sans secret, avec avertissement « root sur l'hôte »
	mt, _, _ = c.model(t, "alice")
	raw, res, e = c.add(t, "alice", cDocker("plain", false), map[string]any{}, mt)
	if e != "" || res["ok"] != true || !strings.Contains(fmt.Sprint(res["warnings"]), "root sur l'hôte") {
		t.Errorf("docker sans TLS : %s (avertissement « root sur l'hôte » attendu)", raw)
	}
}

// Séquence exacte de stSaveUser : un backend existant modifié par configApply, un nouveau ajouté par userBackendAdd avec le mtime relu.
func TestCreate258SequenceDeLEditeur(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, e := c.add(t, "alice", cDocker("un", false), map[string]any{}, mt); e != "" || res["ok"] != true {
		t.Fatalf("premier backend : %v %q", res, e)
	}
	mt, _, _ = c.model(t, "alice")
	ch := []any{map[string]any{"op": "set", "at": []string{"backends", "un"}, "value": map[string]any{"type": "docker", "host": cHost, "volume": "v2"}}}
	if _, res, e := c.call2(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mt, "changes": ch}); e != "" || res["saved"] != true {
		t.Fatalf("configApply d'un backend existant : %v %q", res, e)
	}
	mt, _, _ = c.model(t, "alice") // l'éditeur relit (reload) puis envoie le nouveau avec ce mtime
	if raw, res, e := c.add(t, "alice", cDocker("deux", false), map[string]any{}, mt); e != "" || res["ok"] != true {
		t.Fatalf("second backend : %s", raw)
	}
	y, _ := yamlOf(t, "github-1111")
	if !strings.Contains(y, "un:") || !strings.Contains(y, "deux:") || !strings.Contains(y, "volume: v2") {
		t.Errorf("backends.yaml : %s", y)
	}
}

func (c cr258) call2(t *testing.T, user, method string, args any) (string, map[string]any, string) {
	return c.call(t, user, method, args)
}

// ── refus nommés : {ok:false, field, code, msg}, rien d'écrit ───────────────────────────────────────

func TestCreate258RefusNommes(t *testing.T) {
	c := cr258Start(t)
	ca, _ := ub257Cert(t, true)
	cert, key := ub257Cert(t, false)
	_, key2 := ub257Cert(t, false)
	encKey := strings.ReplaceAll(key, "PRIVATE KEY", "ENCRYPTED PRIVATE KEY")
	good := map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}
	tlsOK := map[string]any{"tls": map[string]any{"ca": ca, "cert": cert, "key": key}}
	k8s := func(mod func(m map[string]any)) map[string]any { m := cK8s("nouveau"); mod(m); return m }
	dkr := func(mod func(m map[string]any)) map[string]any { m := cDocker("nouveau", true); mod(m); return m }
	type tc struct {
		name    string
		be      map[string]any
		secrets map[string]any
		field   string
		code    []string
		msgPart string
	}
	for _, x := range []tc{
		{"k8s sans kubeconfig", cK8s("nouveau"), map[string]any{}, "kubeconfig", []string{"required"}, "kubeconfig obligatoire pour un backend k8s"},
		{"k8s kubeconfig vide", cK8s("nouveau"), map[string]any{"kubeconfig": ""}, "kubeconfig", []string{"required"}, "kubeconfig obligatoire"},
		{"k8s kubeconfig exec", cK8s("nouveau"), map[string]any{"kubeconfig": ub257Kube(cPub, "    exec:\n      command: sh\n", "")}, "kubeconfig", []string{"forbidden_field", "invalid"}, "exec"},
		{"k8s serveur LAN", cK8s("nouveau"), map[string]any{"kubeconfig": ub257Kube("https://10.0.0.1:6443", "", "")}, "kubeconfig", []string{"forbidden_address"}, uRefus},
		{"k8s kubeconfig trop gros", cK8s("nouveau"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "") + "# " + strings.Repeat("a", 70<<10)}, "kubeconfig", []string{"too_large"}, ""},
		{"k8s namespace invalide", k8s(func(m map[string]any) { m["namespace"] = "Pas Valide" }), good, "namespace", []string{"invalid"}, ""},
		{"k8s sans namespace", k8s(func(m map[string]any) { delete(m, "namespace") }), good, "namespace", []string{"required", "invalid"}, ""},
		{"k8s kubeconfig imposé par le client", k8s(func(m map[string]any) { m["kubeconfig"] = "/etc/rancher/k3s/k3s.yaml" }), good, "kubeconfig", []string{"forbidden_field"}, ""},
		{"k8s pod hostNetwork", k8s(func(m map[string]any) { m["pod"] = map[string]any{"hostNetwork": true} }), good, "pod", []string{"forbidden_field"}, ""},
		{"nom invalide", func() map[string]any { m := cK8s("K8S"); return m }(), good, "name", []string{"invalid"}, ""},
		{"nom traversant", func() map[string]any { m := cK8s("../x"); return m }(), good, "name", []string{"invalid"}, ""},
		{"type ssh", map[string]any{"name": "nouveau", "type": "ssh", "host": "srv1"}, map[string]any{}, "type", []string{"forbidden_field", "invalid"}, "ssh"},
		{"docker hôte LAN", dkr(func(m map[string]any) { m["host"] = "tcp://192.168.1.50:2376" }), tlsOK, "host", []string{"forbidden_address"}, uRefus},
		{"docker loopback", dkr(func(m map[string]any) { m["host"] = "tcp://127.0.0.1:2376" }), tlsOK, "host", []string{"forbidden_address"}, uRefus},
		{"docker unix://", dkr(func(m map[string]any) { m["host"] = "unix:///var/run/docker.sock" }), tlsOK, "host", []string{"forbidden_address", "invalid", "forbidden_field"}, ""},
		{"docker TLS sans clé", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"ca": ca, "cert": cert}}, "tls.key", []string{"required"}, ""},
		{"docker TLS sans certificat", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"ca": ca, "key": key}}, "tls.cert", []string{"required"}, ""},
		{"docker TLS vérifié sans CA", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"cert": cert, "key": key}}, "tls.ca", []string{"required"}, ""},
		{"docker clé chiffrée", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"ca": ca, "cert": cert, "key": encKey}}, "tls.key", []string{"invalid"}, "chiffrée"},
		{"docker clé ≠ certificat", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"ca": ca, "cert": cert, "key": key2}}, "tls.key", []string{"invalid"}, ""},
		{"docker PEM invalide", cDocker("nouveau", true), map[string]any{"tls": map[string]any{"ca": "pas un pem", "cert": cert, "key": key}}, "tls.ca", []string{"invalid"}, ""},
	} {
		mt, _, _ := c.model(t, "alice")
		before, st0 := yamlOf(t, "github-1111")
		raw, res, e := c.add(t, "alice", x.be, x.secrets, mt)
		if e != "" || res == nil || res["ok"] != false {
			t.Errorf("%s : pas de refus {ok:false} : %s", x.name, raw)
			continue
		}
		if res["field"] != x.field {
			t.Errorf("%s : field %v, attendu %q", x.name, res["field"], x.field)
		}
		okCode := false
		for _, cd := range x.code {
			okCode = okCode || res["code"] == cd
		}
		if !okCode {
			t.Errorf("%s : code %v, attendu l'un de %v", x.name, res["code"], x.code)
		}
		if msg, _ := res["msg"].(string); msg == "" || !strings.Contains(strings.ToLower(msg), strings.ToLower(x.msgPart)) {
			t.Errorf("%s : msg %q (doit contenir « %s »)", x.name, msg, x.msgPart)
		}
		if strings.Contains(raw, uCanary) || strings.Contains(raw, "BEGIN") {
			t.Errorf("%s : contenu de secret dans la réponse : %.200s", x.name, raw)
		}
		if msg, _ := res["msg"].(string); res["code"] != "forbidden_address" && strings.Contains(msg, uRefus) {
			t.Errorf("%s : le refus d'adresse est renvoyé pour un autre motif (field %v, code %v) : %q", x.name, res["field"], res["code"], msg)
		}
		after, st1 := yamlOf(t, "github-1111")
		if before != after || (st0 != nil && st1 != nil && !st0.ModTime().Equal(st1.ModTime())) {
			t.Errorf("%s : backends.yaml modifié par un refus", x.name)
		}
		if name, _ := x.be["name"].(string); name != "" && !strings.ContainsAny(name, "./") {
			if _, err := os.Stat(vaultOf("github-1111", name)); err == nil {
				t.Errorf("%s : un coffre a été créé par un refus", x.name)
			}
		}
	}
	c.scanLeaks(t)
}

// Nom déjà pris : conflit nommé ; coffre existant sans backend : refusé (le dossier ne doit pas exister).
func TestCreate258NomPrisEtCoffreExistant(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cDocker("pris", false), map[string]any{}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	mt, _, _ = c.model(t, "alice")
	_, res, _ := c.add(t, "alice", cDocker("pris", false), map[string]any{}, mt)
	if res["ok"] != false || res["code"] != "conflict" || res["field"] != "name" || !strings.Contains(fmt.Sprint(res["msg"]), "un backend porte déjà ce nom") {
		t.Errorf("nom pris : %v", res)
	}
	if err := os.MkdirAll(vaultOf("github-1111", "orphelin"), 0o700); err != nil {
		t.Fatal(err)
	}
	mt, _, _ = c.model(t, "alice")
	if _, res, _ = c.add(t, "alice", cDocker("orphelin", false), map[string]any{}, mt); res["ok"] != false {
		t.Errorf("coffre préexistant accepté : %v", res)
	}
}

// Tout ou rien : mtime périmé (échec de l'étape 2) → le coffre écrit à l'étape 1 est retiré ; secret invalide → rien d'écrit.
func TestCreate258ToutOuRien(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cDocker("base", false), map[string]any{}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	stale := mt // l'éditeur n'a pas relu : version périmée
	raw, res, _ := c.add(t, "alice", cK8s("perdu"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}, stale)
	if res["ok"] != false || res["code"] != "conflict" || !strings.Contains(fmt.Sprint(res["msg"]), "rouvre l'éditeur") {
		t.Errorf("version périmée : %s", raw)
	}
	if _, err := os.Stat(vaultOf("github-1111", "perdu")); err == nil {
		t.Error("secrets restés dans le coffre alors que backends.yaml n'a pas été écrit")
	}
	if y, _ := yamlOf(t, "github-1111"); strings.Contains(y, "perdu") {
		t.Error("backend déclaré malgré le conflit")
	}
	// secret invalide : ni coffre ni déclaration
	mt, _, _ = c.model(t, "alice")
	if _, res, _ = c.add(t, "alice", cK8s("invalide"), map[string]any{"kubeconfig": "clusters: ["}, mt); res["ok"] != false {
		t.Errorf("kubeconfig invalide accepté : %v", res)
	}
	if _, err := os.Stat(vaultOf("github-1111", "invalide")); err == nil {
		t.Error("coffre créé pour un secret invalide")
	}
	if y, _ := yamlOf(t, "github-1111"); strings.Contains(y, "invalide") {
		t.Error("backend déclaré avec un secret invalide")
	}
}

func TestCreate258OnziemeBackendRefuse(t *testing.T) {
	c := cr258Start(t)
	for i := 1; i <= 10; i++ {
		mt, _, _ := c.model(t, "alice")
		raw, res, _ := c.add(t, "alice", cDocker(fmt.Sprintf("d%02d", i), false), map[string]any{}, mt)
		if res["ok"] != true {
			t.Fatalf("backend %d/10 refusé : %s", i, raw)
		}
	}
	mt, _, _ := c.model(t, "alice")
	raw, res, _ := c.add(t, "alice", cDocker("d11", false), map[string]any{}, mt)
	if res["ok"] != false || res["code"] != "limit" || !strings.Contains(fmt.Sprint(res["msg"]), "(10)") {
		t.Errorf("11e backend : %s (code limit, « nombre maximal de backends atteint (10) » attendus)", raw)
	}
	if _, ok := res["field"]; ok && res["field"] != "" {
		t.Errorf("la limite est une erreur globale (field absent) : %v", res["field"])
	}
	if _, err := os.Stat(vaultOf("github-1111", "d11")); err == nil {
		t.Error("coffre créé pour le 11e backend")
	}
}

// ── dépôt / suppression de secret d'un backend EXISTANT ────────────────────────────────────────────

func TestSecret258RemplacerSupprimerUnSecretExistant(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cK8s("mon-k8s"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	kc := filepath.Join(vaultOf("github-1111", "mon-k8s"), "kubeconfig")
	before, _ := os.ReadFile(kc)
	// remplacement valide
	newKc := strings.Replace(ub257Kube("https://93.184.216.35:6443", "", ""), uCanary, "CANARY-remplace", 1)
	if raw, res, e := c.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": newKc}); e != "" || res["ok"] != true {
		t.Fatalf("remplacement : %s", raw)
	}
	if after, _ := os.ReadFile(kc); string(after) == string(before) || !strings.Contains(string(after), "CANARY-remplace") {
		t.Error("le kubeconfig n'a pas été remplacé")
	}
	// remplacement invalide : l'ancien reste (tout ou rien)
	cur, _ := os.ReadFile(kc)
	if _, res, _ := c.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube("https://10.0.0.1:6443", "", "")}); res["ok"] == true {
		t.Error("kubeconfig LAN accepté en remplacement")
	}
	if after, _ := os.ReadFile(kc); string(after) != string(cur) {
		t.Error("un remplacement refusé a modifié le coffre")
	}
	// suppression du secret : fichier retiré, état « absent » ; le backend reste déclaré mais inutilisable (échec fermé)
	if raw, res, e := c.call(t, "alice", "backendSecretDelete", map[string]any{"backend": "mon-k8s", "what": "kubeconfig"}); e != "" || res["ok"] != true {
		t.Fatalf("suppression : %s", raw)
	}
	if _, err := os.Stat(kc); err == nil {
		t.Error("kubeconfig encore dans le coffre après suppression")
	}
	// nouveau dépôt
	if raw, res, e := c.call(t, "alice", "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube(cPub, "", "")}); e != "" || res["ok"] != true {
		t.Fatalf("nouveau dépôt : %s", raw)
	}
	// un autre utilisateur ne touche pas à ce coffre
	if _, res, _ := c.call(t, "bob", "backendSecretDelete", map[string]any{"backend": "mon-k8s", "what": "kubeconfig"}); res["ok"] == true {
		t.Error("bob a supprimé le secret d'alice")
	}
	if _, err := os.Stat(kc); err != nil {
		t.Error("le coffre d'alice a été touché par bob")
	}
	c.scanLeaks(t)
}

// Débit des dépôts : 60 par minute et par utilisateur, 429 au-delà (forme {ok:false, code:"rate"}).
func TestSecret258DebitDesDepots(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cK8s("mon-k8s"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	body := `{"method":"backendSecretSet","args":` + ub257JSON(map[string]any{"backend": "mon-k8s", "kubeconfig": ub257Kube(cPub, "", "")}) + `}`
	status429 := 0
	for i := 0; i < 70; i++ {
		v, st := c.s.call("alice", px172Ext, body)
		if st == 429 || strings.Contains(fmt.Sprint(v), `"code":"rate"`) || strings.Contains(fmt.Sprint(v), "code:rate") {
			status429++
		}
	}
	if status429 == 0 {
		t.Error("aucun refus de débit après 70 dépôts en une minute")
	}
	// par utilisateur : bob n'est pas limité
	ub16Declare(t, "github-2222", "k")
	if v, st := c.s.call("bob", px172Ext, `{"method":"backendSecretSet","args":`+ub257JSON(map[string]any{"backend": "k", "kubeconfig": ub257Kube(cPub, "", "")})+`}`); st == 429 {
		t.Errorf("bob limité par les dépôts d'alice : %v", v)
	}
}

// ── éditeur utilisable après un refus (B6) ─────────────────────────────────────────────────────────

func TestEditeur258ApresRefusOnCorrigeEtReenregistre(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cDocker("a", false), map[string]any{}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	y0, st0 := yamlOf(t, "github-1111")
	mt, _, m := c.model(t, "alice")
	bad := []any{map[string]any{"op": "set", "at": []string{"backends", "b"}, "value": map[string]any{"type": "docker", "host": "tcp://192.168.1.50:2376", "volume": "v"}}}
	for i := 0; i < 3; i++ { // plusieurs refus de suite : l'éditeur ne se bloque pas
		raw, res, e := c.call(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mt, "changes": bad})
		if e != "" || res["saved"] == true || (res["invalid"] != true && res["ok"] != false) {
			t.Fatalf("refus %d attendu : %s", i, raw)
		}
		if !strings.Contains(raw, uRefus) {
			t.Errorf("refus %d sans la raison nommée : %.300s", i, raw)
		}
		y1, st1 := yamlOf(t, "github-1111")
		if y1 != y0 || !st1.ModTime().Equal(st0.ModTime()) {
			t.Fatalf("refus %d : backends.yaml ou son mtime modifié", i)
		}
		// le modèle renvoyé reste utilisable : même mtime, backend « a » présent, aucun problème
		_, raw2, m2 := c.model(t, "alice")
		if m2["mtime"] != m["mtime"] || !strings.Contains(raw2, `"name":"a"`) || m2["exists"] != true {
			t.Fatalf("modèle inutilisable après un refus : %.300s", raw2)
		}
		if ps, _ := m2["problems"].([]any); len(ps) != 0 {
			t.Errorf("problèmes de lecture après un refus : %v", ps)
		}
	}
	// correction (hôte public) puis réenregistrement avec le MÊME mtime : sans éditer le YAML
	good := []any{map[string]any{"op": "set", "at": []string{"backends", "b"}, "value": map[string]any{"type": "docker", "host": cHost, "volume": "v"}}}
	raw, res, e := c.call(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mt, "changes": good})
	if e != "" || res["saved"] != true {
		t.Fatalf("réenregistrement après correction refusé : %s", raw)
	}
	if y, _ := yamlOf(t, "github-1111"); !strings.Contains(y, "a:") || !strings.Contains(y, "b:") {
		t.Errorf("backends.yaml : %s", y)
	}
}

// Ajout d'un k8s sans kubeconfig par configApply : refusé avec le même message que userBackendAdd, fichier intact.
func TestEditeur258ConfigApplyK8sSansKubeconfig(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	ch := []any{map[string]any{"op": "set", "at": []string{"backends", "k"}, "value": map[string]any{"type": "k8s", "namespace": "ns"}}}
	raw, res, e := c.call(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mt, "changes": ch})
	if e != "" || res["saved"] == true || !strings.Contains(raw, "kubeconfig obligatoire pour un backend k8s") {
		t.Errorf("k8s sans kubeconfig par configApply : %s", raw)
	}
	if _, st := yamlOf(t, "github-1111"); st != nil {
		t.Error("backends.yaml créé par un refus")
	}
}

// Diagnostic : la raison INTERNE d'un refus d'hôte est dans l'audit (administrateur), jamais dans la réponse de l'utilisateur
// (message uniforme, S8). Sans cela, « tcp://yahoo.fr » refusé est indiagnostiquable en production (DNS, plage ou forme ?).
func TestDiagnostic258RaisonDuRefusDansLAudit(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	raw, res, _ := c.add(t, "alice", func() map[string]any { m := cDocker("x", false); m["host"] = "tcp://192.168.1.50:2376"; return m }(), map[string]any{}, mt)
	if res["ok"] != false || !strings.Contains(raw, uRefus) {
		t.Fatalf("refus attendu : %s", raw)
	}
	for _, bad := range []string{"plage", "privé", "dns", "192.168", "range"} {
		if strings.Contains(strings.ToLower(raw), bad) {
			t.Errorf("la réponse de l'utilisateur révèle la raison (« %s ») : %.200s", bad, raw)
		}
	}
	found := false
	for _, l := range c.s.auditLines(t) {
		b, _ := json.Marshal(l)
		s := string(b)
		if strings.Contains(s, "alice") && (strings.Contains(s, "backend.refused") || strings.Contains(s, "userBackendAdd")) && strings.Contains(s, "reason") {
			found = true
			if strings.Contains(s, "192.168.1.50") || strings.Contains(s, uCanary) {
				t.Errorf("l'audit contient l'adresse ou un secret : %s", s)
			}
		}
	}
	if !found {
		t.Error("aucune ligne d'audit « backend.refused » avec un champ « reason » (dns | range | form) pour ce refus")
	}
}

// Le refus « adresse non autorisée » ne sort QUE d'un refus d'adresse : jamais pour un backend inconnu, un secret ou un test sur un
// backend non enregistré (GATE LOT-3.1 : « yahoo.fr toujours rejeté » — refus probablement mal attribué).
func TestRefus258AdresseJamaisPourUnAutreMotif(t *testing.T) {
	c := cr258Start(t)
	kc := ub257Kube(cPub, "", "")
	calls := []struct {
		method string
		args   map[string]any
	}{
		{"backendSecretGet", map[string]any{"backend": "inconnu"}},
		{"backendSecretSet", map[string]any{"backend": "inconnu", "kubeconfig": kc}},
		{"backendSecretSet", map[string]any{"backend": "inconnu", "ca": "x", "cert": "y", "key": "z"}},
		{"backendSecretDelete", map[string]any{"backend": "inconnu", "what": "kubeconfig"}},
		{"backendTest", map[string]any{"backend": map[string]any{"name": "inconnu", "type": "docker", "host": cHost, "volume": "v"}}},
		{"userBackendRemove", map[string]any{"name": "inconnu"}},
		{"configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": "", "changes": []any{map[string]any{"op": "set", "at": []string{"backends", "a"}, "value": map[string]any{"type": "docker", "host": "tcp://yahoo.fr:2376", "volume": "v"}}}}},
	}
	for _, x := range calls {
		raw, _, _ := c.call(t, "alice", x.method, x.args)
		if strings.Contains(raw, uRefus) {
			t.Errorf("%s sur un backend inconnu / hôte public : « %s » renvoyé : %.250s", x.method, uRefus, raw)
		}
	}
	// refus d'un backend inconnu : message qui dit ce qui manque, distinct d'un refus d'adresse
	_, res, e := c.call(t, "alice", "backendSecretGet", map[string]any{"backend": "inconnu"})
	if msg, ok := ub257Refusal(res, e); ok && !strings.Contains(strings.ToLower(msg), "backend") {
		t.Errorf("backendSecretGet inconnu : message sans « backend » : %q", msg)
	}
	// un hôte public est accepté par configApply et par userBackendAdd
	raw, res, e := c.call(t, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": "", "changes": []any{map[string]any{"op": "set", "at": []string{"backends", "pub"}, "value": map[string]any{"type": "docker", "host": cHost, "volume": "v"}}}})
	if e != "" || res["saved"] != true {
		t.Errorf("hôte public refusé par configApply : %s", raw)
	}
}

// Échec d'écriture de backends.yaml (étape 2) : le coffre écrit à l'étape 1 est retiré (aucun secret sans backend).
func TestCreate258RollbackSiLEcritureDuYAMLEchoue(t *testing.T) {
	c := cr258Start(t)
	mt, _, _ := c.model(t, "alice")
	if _, res, _ := c.add(t, "alice", cDocker("base", false), map[string]any{}, mt); res["ok"] != true {
		t.Fatalf("création : %v", res)
	}
	// la copie .bak de l'étape 2 ne peut pas s'écrire : un DOSSIER occupe son nom
	if err := os.MkdirAll(filepath.Join(core.UsersDir(), "github-1111", "backends.yaml.bak", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	mt, _, _ = c.model(t, "alice")
	raw, res, _ := c.add(t, "alice", cK8s("perdu"), map[string]any{"kubeconfig": ub257Kube(cPub, "", "")}, mt)
	if res["ok"] != false {
		t.Skipf("l'écriture n'a pas échoué (implémentation sans .bak bloquant) : %.200s", raw)
	}
	if _, err := os.Stat(vaultOf("github-1111", "perdu")); err == nil {
		t.Error("coffre laissé en place alors que backends.yaml n'a pas été écrit")
	}
	if y, _ := yamlOf(t, "github-1111"); strings.Contains(y, "perdu") {
		t.Error("backend déclaré malgré l'échec d'écriture")
	}
}
