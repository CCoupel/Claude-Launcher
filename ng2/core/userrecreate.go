package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Recréation d'un conteneur docker/podman d'un projet d'UTILISATEUR après confirmation (LOT-3.2 E1c, P27 ; contrat §5 bis ; sécurité T10, T16–T19).
// Déroulé avec retour arrière : arrêt, renommage de l'ancien en <nom>-old-<ns>-<aléa>, création et démarrage du nouveau, suppression de l'ancien
// SEULEMENT après démarrage ET inspection du nouveau ; un échec supprime le nouveau et rend son nom à l'ancien. Le volume n'est jamais touché
// (jamais « rm -v », jamais une commande « volume »).

var (
	ErrRecreateUnconfirmed = errors.New("recréation non confirmée : jeton absent, périmé, déjà utilisé ou pour d'autres options")
	ErrRecreateBusy        = errors.New("une recréation de ce projet est déjà en cours")
	ErrRecreateNothing     = errors.New("aucune sauvegarde de recréation à reprendre")
	ErrRecreateDuplicate   = errors.New("doublon : le conteneur et sa sauvegarde existent tous deux — rien n'a été supprimé")
)

// jetons à usage unique, liés au projet ET aux options demandées (T17) ; valables 10 minutes
type recreateGrant struct {
	bind  string
	until time.Time
}

var (
	recreateMu     sync.Mutex
	recreateTokens = map[string]recreateGrant{}
	recreateBusy   = map[string]bool{}
)

func recreateBinding(name string, args []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:%s\x00", len(name), name)
	for _, a := range args { // longueur préfixée : « a b » et « a », « b » ne se confondent jamais
		fmt.Fprintf(h, "%d:%s\x00", len(a), a)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RecreateToken : jeton de confirmation à usage unique, lié au nom du conteneur et à l'argv de « create » demandé. Chaque émission est distincte.
func (e Engine) RecreateToken(name string, createArgs []string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	recreateMu.Lock()
	defer recreateMu.Unlock()
	now := time.Now()
	for k, g := range recreateTokens { // jetons périmés purgés
		if now.After(g.until) {
			delete(recreateTokens, k)
		}
	}
	recreateTokens[tok] = recreateGrant{bind: e.B.uid + "|" + recreateBinding(name, createArgs), until: now.Add(10 * time.Minute)}
	return tok
}

// consumeRecreateToken : vrai (et le jeton est consommé) seulement s'il correspond au projet et aux options.
func (e Engine) consumeRecreateToken(tok, name string, args []string) bool {
	recreateMu.Lock()
	defer recreateMu.Unlock()
	g, ok := recreateTokens[tok]
	if !ok || time.Now().After(g.until) || g.bind != e.B.uid+"|"+recreateBinding(name, args) {
		return false
	}
	delete(recreateTokens, tok)
	return true
}

// options admises dans l'argv de « create » : celles que le constructeur dédié produit — jamais autre chose (T3).
var createAllowed = map[string]bool{"--name": true, "--hostname": true, "--label": true, "--mount": true, "--workdir": true, "-w": true, "-e": true,
	"--env": true, "--user": true, "--cap-drop": true, "--security-opt": true, "--cpus": true, "--memory": true, "--pids-limit": true,
	"--shm-size": true, "--gpus": true, "--cap-add": true}

// checkCreateArgs : « create » puis des options de la liste blanche, l'image, la commande fixe ; jamais un drapeau étranger.
func checkCreateArgs(args []string) error {
	if len(args) < 3 || args[0] != "create" {
		return errors.New("argv de création invalide")
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if CheckUserImage(a) != nil { // premier argument sans « - » hors valeur d'option : l'image
				if i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.Contains(args[i-1], "=") && createAllowed[args[i-1]] {
					continue // valeur d'une option « --opt valeur »
				}
				return errors.New("argv de création invalide")
			}
			if i > 0 && strings.HasPrefix(args[i-1], "-") && !strings.Contains(args[i-1], "=") && createAllowed[args[i-1]] {
				continue // valeur qui ressemble à une image (ex. nom) : option « --opt valeur »
			}
			return nil // l'image et la commande fixe : fin des options
		}
		name := a
		if k := strings.IndexByte(a, '='); k > 0 {
			name = a[:k]
		}
		if !createAllowed[name] {
			return errors.New("option de création non admise")
		}
		if name == "--cap-add" { // seules les cinq capacités du bootstrap
			v := strings.TrimPrefix(a, "--cap-add=")
			if !strings.Contains(a, "=") && i+1 < len(args) {
				v = args[i+1]
			}
			for _, c := range strings.Split(v, ",") {
				if !map[string]bool{"CHOWN": true, "DAC_OVERRIDE": true, "FOWNER": true, "SETUID": true, "SETGID": true}[c] {
					return errors.New("capacité non admise")
				}
			}
		}
	}
	return nil
}

// ownerLabel : étiquette cl.user de l'objet ; known faux si le moteur n'en dit rien d'exploitable.
func (e Engine) ownerLabel(name string) string {
	out, err := e.output(e.cmdD(userReadTimeout, "inspect", "-f", `{{index .Config.Labels "cl.user"}}`, name), maxSmallOut)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// VerifyOwner : l'objet porte EXACTEMENT l'étiquette cl.user de l'appelant (T10) — ni autre, ni vide, ni variante de casse. À utiliser avant
// toute opération sur un objet découvert par son nom.
func (e Engine) VerifyOwner(uid, name string) error {
	if got := e.ownerLabel(name); got != uid || uid == "" {
		return errors.New("l'objet n'appartient pas à cet utilisateur")
	}
	return nil
}

// ownerRefused : refus de la recréation quand l'objet porte l'étiquette d'UN AUTRE utilisateur. (Un objet sans étiquette n'est jamais proposé à
// la recréation : la découverte ne liste que les objets cl.user=<uid>.)
func (e Engine) ownerRefused(name string) bool {
	got := e.ownerLabel(name)
	return userUIDRE.MatchString(got) && got != e.B.uid
}

var userUIDRE = regexp.MustCompile(`^github-[0-9]+$`)

// RecreateContainer : recrée le conteneur `name` avec l'argv `createArgs` (« create … »). token : celui de RecreateToken pour CES options.
// wasRunning : l'ancien tournait (arrêté, puis redémarré si la recréation échoue).
func (e Engine) RecreateContainer(name string, createArgs []string, token string, wasRunning bool, now time.Time) error {
	if !e.B.user || !userCtrRE.MatchString(name) {
		return errors.New("recréation : conteneur d'un projet d'utilisateur attendu")
	}
	if err := checkCreateArgs(createArgs); err != nil {
		return err
	}
	if e.ownerRefused(name) {
		return errors.New("l'objet n'appartient pas à cet utilisateur")
	}
	if !e.consumeRecreateToken(token, name, createArgs) {
		return ErrRecreateUnconfirmed
	}
	key := e.B.uid + "|" + e.B.Name + "|" + name
	recreateMu.Lock()
	if recreateBusy[key] {
		recreateMu.Unlock()
		return ErrRecreateBusy
	}
	recreateBusy[key] = true
	recreateMu.Unlock()
	defer func() { recreateMu.Lock(); delete(recreateBusy, key); recreateMu.Unlock() }()

	run := func(args ...string) error { return e.cmd(args...).Run() }
	if wasRunning {
		_ = run("stop", name) // un arrêt qui échoue (déjà arrêté) n'empêche pas le renommage ; un conteneur resté actif ferait échouer la suite, rétablie
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	old := fmt.Sprintf("%s-old-%d-%s", name, now.UnixNano(), hex.EncodeToString(suffix))
	if err := run("rename", name, old); err != nil {
		if wasRunning {
			_ = run("start", name)
		}
		return errors.New("renommage de l'ancien conteneur impossible")
	}
	rollback := func(msg string) error { // le nouveau est supprimé (sans -v), l'ancien reprend son nom et redémarre s'il tournait
		_ = run("rm", "-f", name)
		_ = run("rename", old, name)
		if wasRunning {
			_ = run("start", name)
		}
		return errors.New(msg)
	}
	if err := run(createArgs...); err != nil {
		return rollback("création du nouveau conteneur impossible : l'ancien est rétabli")
	}
	if err := run("start", name); err != nil {
		return rollback("démarrage du nouveau conteneur impossible : l'ancien est rétabli")
	}
	if _, err := e.cmd("inspect", "-f", "{{.State.Running}}", name).Output(); err != nil {
		return rollback("inspection du nouveau conteneur impossible : l'ancien est rétabli")
	}
	if err := run("rm", old); err != nil { // sans -v : le volume n'est jamais touché ; l'ancien reste identifiable (doublon signalé à la reprise)
		return errors.New("le nouveau conteneur fonctionne ; l'ancien n'a pas pu être supprimé : " + old)
	}
	return nil
}

// RestoreInterrupted : reprise après un arrêt brutal. La sauvegarde est identifiée par ses ÉTIQUETTES (cl.recreate-of=<nom> et cl.state=old), ou — docker ne
// permettant pas d'étiqueter un conteneur existant — par ses étiquettes d'origine (cl.user, cl.project du projet) avec un nom <nom>-old-<ns>-<aléa> :
// jamais par le motif du nom seul (un projet « x-old-<chiffres> » n'est jamais touché, T16). Sauvegarde seule : restaurée ; les deux : doublon,
// rien n'est supprimé ; aucune : ErrRecreateNothing.
func (e Engine) RestoreInterrupted(name string) error {
	if !e.B.user || !userCtrRE.MatchString(name) {
		return errors.New("reprise : conteneur d'un projet d'utilisateur attendu")
	}
	out, err := e.output(e.cmdD(userReadTimeout, "ps", "-a", "--format", "{{.Names}} {{.Labels}}"), maxListOut)
	if err != nil {
		return errors.New("lecture des conteneurs impossible")
	}
	var backup string
	nominal := false
	oldRE := regexp.MustCompile("^" + regexp.QuoteMeta(name) + `-old-[0-9]+-[a-z0-9]{4,}$`)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(f) == 0 {
			continue
		}
		labels := map[string]bool{}
		kv := map[string]string{}
		for _, l := range f[1:] {
			labels[l] = true
			if k, v, ok := strings.Cut(l, "="); ok {
				kv[k] = v
			}
		}
		// sauvegarde : étiquettes explicites, ou étiquettes d'origine qui, recalculées, redonnent le nom de l'objet
		original := kv["cl.user"] == e.B.uid && UserObjName(e.B.uid, kv["cl.category"], kv["cl.project"]) == name
		switch {
		case f[0] == name:
			nominal = true
		case oldRE.MatchString(f[0]) && (labels["cl.recreate-of="+name] && labels["cl.state=old"] || original):
			backup = f[0]
		}
	}
	switch {
	case backup == "":
		return ErrRecreateNothing
	case nominal:
		return ErrRecreateDuplicate
	}
	if err := e.cmd("rename", backup, name).Run(); err != nil {
		return errors.New("restauration impossible")
	}
	return nil
}

var _ = exec.Command
