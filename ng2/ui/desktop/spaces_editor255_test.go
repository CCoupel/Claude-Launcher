package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #255 (test-writer, retour QUALIF) — « + Ajouter un backend » d'un utilisateur sans backend ouvrait Réglages › Backends sur
// « Lecture impossible : configModel : indisponible dans votre espace ». Les tests précédents rejouaient configOpen/configSave,
// que l'éditeur n'appelle pas. Ici : la séquence EXACTE de ng2/ui/desktop/frontend/settings.js, en HTTP, avec l'en-tête
// X-Forwarded-User d'un utilisateur sans backend (RunWeb en mode proxy, API GitHub factice).
//
// À l'ouverture (stOpen) : configModel{backends.yaml} ; en parallèle configModel{config.conf}, {env.yaml}, {hub}
// (docLoad) ; backendStates (stPoll). À l'enregistrement (stSave) : configApply{kind: backends.yaml, path: "", mtime, changes}.
// Fiches de backend : backendSecretGet / Set / Delete, backendTest.

const ed255Admin = "admin-marqueur-secret"

func ed255Start(t *testing.T) *px172 {
	t.Helper()
	s := sp255Server(t)
	// l'admin a un backend reconnaissable : il ne doit apparaître dans aucune réponse faite à un utilisateur
	y := "backends:\n  " + ed255Admin + ":\n    type: k8s\n    namespace: ns-admin\n"
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

// ed255Do : appel d'un utilisateur ; rend le JSON brut (pour les contrôles d'absence), l'erreur, le résultat.
func ed255Do(t *testing.T, s *px172, user, method string, args any) (raw string, errMsg string, res map[string]any) {
	t.Helper()
	b, _ := json.Marshal(args)
	v, st := s.call(user, px172Ext, `{"method":"`+method+`","args":`+string(b)+`}`)
	if st != 200 || v == nil {
		t.Fatalf("%s %s : statut HTTP %d", user, method, st)
	}
	rb, _ := json.Marshal(v)
	if e, ok := v["error"].(string); ok {
		errMsg = e
	}
	res, _ = v["result"].(map[string]any)
	return string(rb), errMsg, res
}

// refusal : la réponse est un refus clair — error non vide, ou {ok:false, forbidden, msg} (règle R1) — et jamais un résultat utile.
func ed255Refusal(res map[string]any, errMsg string) (msg string, ok bool) {
	if errMsg != "" {
		return errMsg, true
	}
	if res != nil && res["ok"] == false {
		m, _ := res["msg"].(string)
		return m, m != "" || res["forbidden"] != nil
	}
	return "", false
}

// Ouverture de Réglages › Backends : AUCUNE erreur « indisponible », modèle valide limité à l'espace de l'utilisateur.
func TestEditor255OuvertureUtilisateurSansBackend(t *testing.T) {
	s := ed255Start(t)
	raw, errMsg, res := ed255Do(t, s, "alice", "configModel", map[string]any{"kind": "backends.yaml"})
	if errMsg != "" || res == nil {
		t.Fatalf("configModel backends.yaml : « %s » — l'éditeur affiche « Lecture impossible » (res=%v)", errMsg, res)
	}
	if strings.Contains(raw, "indisponible dans votre espace") {
		t.Errorf("message d'indisponibilité dans la réponse : %s", raw)
	}
	m, _ := res["model"].(map[string]any)
	if m == nil {
		t.Fatalf("pas de modèle : %v", res)
	}
	if bs, _ := m["backends"].([]any); len(bs) != 0 {
		t.Errorf("backends du modèle d'un utilisateur sans backend : %v", bs)
	}
	if ps, _ := res["problems"].([]any); len(ps) != 0 {
		t.Errorf("problèmes de lecture : %v", ps)
	}
	if res["exists"] != false {
		t.Errorf("exists = %v : son backends.yaml n'existe pas encore", res["exists"])
	}
	// ni chemin du serveur ni identifiant numérique
	p, _ := res["path"].(string)
	if strings.Contains(raw, core.Ng2ConfigDir()) || strings.Contains(raw, "github-") || strings.Contains(p, "/") {
		t.Errorf("chemin du serveur ou identifiant exposé : path=%q", p)
	}
	// états des backends (stPoll) : pas d'erreur, rien de l'admin
	raw, errMsg, _ = ed255Do(t, s, "alice", "backendStates", map[string]any{})
	if errMsg != "" || strings.Contains(raw, ed255Admin) {
		t.Errorf("backendStates : erreur « %s » ou backend de l'admin : %s", errMsg, raw)
	}
}

// Les autres rubriques de Réglages (chargées en parallèle à l'ouverture) sont refusées avec un message clair, sans contenu.
func TestEditor255AutresRubriquesRefuseesClairement(t *testing.T) {
	s := ed255Start(t)
	for _, kind := range []string{"config.conf", "env.yaml", "hub"} {
		raw, errMsg, res := ed255Do(t, s, "alice", "configModel", map[string]any{"kind": kind})
		msg, ok := ed255Refusal(res, errMsg)
		if !ok || res != nil && res["model"] != nil {
			t.Errorf("configModel %s : pas de refus clair : %s", kind, raw)
			continue
		}
		if !strings.Contains(strings.ToLower(msg), "administrateur") {
			t.Errorf("configModel %s : message peu clair « %s » (dire que la rubrique est réservée à l'administrateur)", kind, msg)
		}
		if strings.Contains(raw, ed255Admin) || strings.Contains(raw, core.Ng2ConfigDir()) {
			t.Errorf("configModel %s : contenu de l'admin dans la réponse : %s", kind, raw)
		}
	}
	// chemin choisi par le client : jamais suivi
	for _, a := range []map[string]any{{"kind": "backends.yaml", "path": "/etc"}, {"kind": "backends.yaml", "path": core.Ng2ConfigDir()}, {"kind": "project-env", "path": "/tmp"}} {
		raw, errMsg, res := ed255Do(t, s, "alice", "configModel", a)
		if _, ok := ed255Refusal(res, errMsg); !ok && (res == nil || strings.Contains(raw, ed255Admin)) {
			t.Errorf("configModel %v : %s", a, raw)
		}
		if strings.Contains(raw, ed255Admin) {
			t.Errorf("configModel %v expose l'admin : %s", a, raw)
		}
	}
}

// Isolation : configModel ne rend jamais un élément de l'admin, quelle que soit la forme de l'appel.
func TestEditor255ConfigModelNeRendJamaisLAdmin(t *testing.T) {
	s := ed255Start(t)
	for _, a := range []map[string]any{
		{"kind": "backends.yaml"}, {"kind": "backends.yaml", "path": ""}, {"kind": "BACKENDS.YAML"}, {"kind": "backends.yaml", "project": core.Ng2ConfigDir()},
		{"kind": "hub"}, {"kind": "config.conf"}, {"kind": "env.yaml"}, {"kind": "../backends.yaml"}, {},
	} {
		raw, _, _ := ed255Do(t, s, "alice", "configModel", a)
		for _, forb := range []string{ed255Admin, "ns-admin", core.Ng2ConfigDir()} {
			if strings.Contains(raw, forb) {
				t.Errorf("configModel %v : « %s » dans la réponse d'un utilisateur : %s", a, forb, raw)
			}
		}
	}
	// et dans l'autre sens : ce qu'alice enregistre n'est pas dans le modèle de bob
	ch := []map[string]any{{"op": "set", "at": []string{"backends", "marqueur-alice"}, "value": map[string]any{"type": "k8s", "namespace": "ns-alice"}}}
	if _, e, r := ed255Do(t, s, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": "", "changes": ch}); e != "" || r["saved"] != true {
		t.Fatalf("configApply alice : %q %v", e, r)
	}
	if raw, _, _ := ed255Do(t, s, "bob", "configModel", map[string]any{"kind": "backends.yaml"}); strings.Contains(raw, "marqueur-alice") || strings.Contains(raw, "ns-alice") {
		t.Errorf("bob lit le backend d'alice : %s", raw)
	}
}

// Enregistrement : un backend k8s et un backend docker valides sont acceptés, relus par configModel, dans SON espace seulement.
func TestEditor255EnregistrementDunBackendValide(t *testing.T) {
	s := ed255Start(t)
	_, _, m0 := ed255Do(t, s, "alice", "configModel", map[string]any{"kind": "backends.yaml"})
	mtime, _ := m0["mtime"].(string)
	ch := []map[string]any{
		{"op": "set", "at": []string{"backends", "mon-k8s"}, "value": map[string]any{"type": "k8s", "namespace": "ns-a"}},
		{"op": "set", "at": []string{"backends", "mon-docker"}, "value": map[string]any{"type": "docker", "host": "tcp://nas.lan:2376"}},
	}
	raw, e, r := ed255Do(t, s, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mtime, "changes": ch})
	if e != "" || r["saved"] != true || r["invalid"] != nil || r["conflict"] != nil {
		t.Fatalf("enregistrement refusé : %s", raw)
	}
	if strings.Contains(raw, "indisponible dans votre espace") || strings.Contains(raw, core.Ng2ConfigDir()) {
		t.Errorf("réponse d'enregistrement : %s", raw)
	}
	// relecture comme le fait l'éditeur (stLoad après enregistrement)
	raw, e, r = ed255Do(t, s, "alice", "configModel", map[string]any{"kind": "backends.yaml"})
	if e != "" || !strings.Contains(raw, "mon-k8s") || !strings.Contains(raw, "mon-docker") || r["exists"] != true {
		t.Errorf("relecture après enregistrement : %s", raw)
	}
	// l'init de l'interface en tient compte (plus « sans backend »)
	if _, _, ini := ed255Do(t, s, "alice", "init", map[string]any{}); ini["space"].(map[string]any)["empty"] != false {
		t.Errorf("espace toujours vide après enregistrement : %v", ini["space"])
	}
	// admin : son fichier n'a pas bougé
	if b, _ := os.ReadFile(core.BackendsPath()); strings.Contains(string(b), "mon-k8s") || !strings.Contains(string(b), ed255Admin) {
		t.Errorf("backends.yaml de l'admin modifié : %s", b)
	}
	// conflit : une ouverture périmée est signalée, pas écrasée
	if _, _, r := ed255Do(t, s, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": mtime, "changes": []map[string]any{{"op": "delete", "at": []string{"backends", "mon-k8s"}}}}); r["conflict"] != true {
		t.Errorf("ouverture périmée non détectée : %v", r)
	}
}

// Champs réservés à l'administrateur (ressources de la plateforme) : refusés, rien d'écrit.
func TestEditor255ChampsR1Refuses(t *testing.T) {
	s := ed255Start(t)
	for name, v := range map[string]map[string]any{
		"kubeconfig local":      {"type": "k8s", "namespace": "ns", "kubeconfig": "/etc/rancher/k3s/k3s.yaml"},
		"backend host":          {"type": "host"},
		"contexte du poste":     {"type": "k8s", "namespace": "ns", "context": "admin-ctx"},
		"volumes de la racine":  {"type": "k8s", "namespace": "ns", "volumes": map[string]any{"/share": "pvc"}},
		"ServiceAccount du pod": {"type": "k8s", "namespace": "ns", "pod": map[string]any{"serviceAccountName": "launcher"}},
	} {
		ch := []map[string]any{{"op": "set", "at": []string{"backends", "intrus"}, "value": v}}
		raw, e, r := ed255Do(t, s, "alice", "configApply", map[string]any{"kind": "backends.yaml", "path": "", "mtime": "", "changes": ch})
		if r["saved"] == true {
			t.Errorf("%s : enregistré (%s)", name, raw)
		}
		if _, ok := ed255Refusal(r, e); !ok && r["invalid"] != true {
			t.Errorf("%s : ni refus ni invalidation : %s", name, raw)
		}
	}
	if raw, _, _ := ed255Do(t, s, "alice", "configModel", map[string]any{"kind": "backends.yaml"}); strings.Contains(raw, "intrus") {
		t.Errorf("un champ réservé a été écrit : %s", raw)
	}
	// autres fichiers en écriture : R1
	for _, kind := range []string{"config.conf", "env.yaml", "hub"} {
		raw, e, r := ed255Do(t, s, "alice", "configApply", map[string]any{"kind": kind, "path": "", "mtime": "", "changes": []map[string]any{{"op": "set", "at": []string{"X"}, "value": "1"}}})
		if _, ok := ed255Refusal(r, e); !ok || r["saved"] == true {
			t.Errorf("configApply %s : pas de refus clair : %s", kind, raw)
		}
	}
}

// Coffre (fiches de backend) : refusé avec un message clair, jamais de contenu de l'admin ni d'identifiant en réponse.
func TestEditor255CoffreRefuseClairement(t *testing.T) {
	s := ed255Start(t)
	for method, args := range map[string]map[string]any{
		"backendSecretGet":    {"backend": ed255Admin},
		"backendSecretSet":    {"backend": "mon-k8s", "kubeconfig": "apiVersion: v1"},
		"backendSecretDelete": {"backend": ed255Admin, "what": "kubeconfig"},
	} {
		raw, e, r := ed255Do(t, s, "alice", method, args)
		if _, ok := ed255Refusal(r, e); !ok {
			t.Errorf("%s : pas de refus clair : %s", method, raw)
		}
		if strings.Contains(raw, "apiVersion") || strings.Contains(raw, core.Ng2ConfigDir()) || strings.Contains(fmt.Sprint(r["info"]), ed255Admin+"-secret") {
			t.Errorf("%s : fuite : %s", method, raw)
		}
	}
}
