package main

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"testing"
)

// #172 — message d'échec de connexion des gabarits d'oauth2-proxy (v7.15.6) : « Identifiant ou mot de passe incorrect. » quand
// `.StatusCode` vaut 401 ou 400 (ManualSignIn), rien en 200 ; même texte dans sign_in.html et sign_in.local-only.html ; valeurs du
// proxy auto-échappées (html/template, comme oauth2-proxy) ; page 429 inchangée ; le middleware Traefik `errors` ne capte que le
// 429 (un 401 / 400 remplacé par /too-many masquerait le message). Rendu avec html/template : aucun cluster, aucun helm.

const signInFailure = "Identifiant ou mot de passe incorrect."

type proxyPage struct { // champs lus par les gabarits (oauth2-proxy : pagewriter.signInPageData / errorPageData)
	ProxyPrefix   string
	Redirect      string
	StatusCode    int
	CustomLogin   bool
	SignInMessage string
	Message       string
}

func renderAuthTemplate(t *testing.T, file, name string, d proxyPage) string {
	t.Helper()
	tpl, err := template.New("").ParseFiles(chartDir + "/files/auth-templates/" + file)
	if err != nil {
		t.Fatalf("%s : %v", file, err)
	}
	var sb strings.Builder
	if err := tpl.ExecuteTemplate(&sb, name, d); err != nil {
		t.Fatalf("%s : exécution : %v", file, err)
	}
	return sb.String()
}

var errParagraph = regexp.MustCompile(`<p class="err"[^>]*>[^<]*</p>`)

func TestSignIn172FailureMessageByStatus(t *testing.T) {
	base := proxyPage{ProxyPrefix: "/oauth2", Redirect: "/"}
	for _, f := range []struct {
		file       string
		needsLogin bool // le formulaire local n'est rendu qu'avec CustomLogin (sign_in.html) ; toujours dans local-only
	}{{"sign_in.html", true}, {"sign_in.local-only.html", false}} {
		for _, custom := range []bool{true, false} {
			for status, wantMsg := range map[int]bool{200: false, 401: true, 400: true, 403: false, 500: false, 429: false} {
				d := base
				d.StatusCode, d.CustomLogin = status, custom
				out := renderAuthTemplate(t, f.file, "sign_in.html", d)
				formShown := custom || !f.needsLogin
				has := strings.Contains(out, signInFailure)
				if has != (wantMsg && formShown) {
					t.Errorf("%s CustomLogin=%v StatusCode=%d : message présent = %v, attendu %v", f.file, custom, status, has, wantMsg && formShown)
				}
				if has && !strings.Contains(out, `role="alert"`) {
					t.Errorf("%s %d : message sans role=alert", f.file, status)
				}
				if strings.Count(out, signInFailure) > 1 {
					t.Errorf("%s %d : message répété (« un seul message »)", f.file, status)
				}
			}
		}
	}
}

// Même texte (et même balise) dans les deux gabarits ; le message ne dit pas lequel du couple est faux.
func TestSignIn172SameMessageInBothTemplates(t *testing.T) {
	d := proxyPage{ProxyPrefix: "/oauth2", Redirect: "/", StatusCode: 401, CustomLogin: true}
	a := errParagraph.FindString(renderAuthTemplate(t, "sign_in.html", "sign_in.html", d))
	b := errParagraph.FindString(renderAuthTemplate(t, "sign_in.local-only.html", "sign_in.html", d))
	if a == "" || a != b {
		t.Errorf("paragraphe d'erreur différent ou absent :\n sign_in.html          : %q\n sign_in.local-only.html : %q", a, b)
	}
	if !strings.Contains(a, signInFailure) {
		t.Errorf("texte = %q, attendu %q", a, signInFailure)
	}
	low := strings.ToLower(a)
	for _, bad := range []string{"mot de passe invalide", "identifiant inconnu", "utilisateur inconnu", "unknown user", "invalid password"} {
		if strings.Contains(low, bad) {
			t.Errorf("le message dit lequel est faux : %q", a)
		}
	}
}

// Valeurs du proxy auto-échappées : `rd`, SignInMessage et Message hostiles ne produisent ni balise, ni attribut, ni script.
func TestSignIn172HostileValuesEscaped(t *testing.T) {
	evil := `"><script>alert(1)</script><img src=x onerror=alert(2)>'`
	for _, file := range []string{"sign_in.html", "sign_in.local-only.html"} {
		for _, custom := range []bool{true, false} {
			d := proxyPage{ProxyPrefix: "/oauth2", Redirect: evil, StatusCode: 401, CustomLogin: custom, SignInMessage: evil, Message: evil}
			out := renderAuthTemplate(t, file, "sign_in.html", d)
			for _, bad := range []string{"<script", "<img", `value=""><`, `" onerror=`} {
				if strings.Contains(out, bad) {
					t.Errorf("%s CustomLogin=%v : « %s » non échappé dans la page", file, custom, bad)
				}
			}
			if !strings.Contains(out, "&#34;&gt;&lt;script&gt;") && strings.Contains(out, `name="rd"`) {
				t.Errorf("%s CustomLogin=%v : la valeur de rd n'apparaît pas échappée (&#34;&gt;&lt;script&gt;)", file, custom)
			}
		}
	}
	// `javascript:` dans rd reste une simple valeur de champ caché (jamais un lien)
	d := proxyPage{ProxyPrefix: "/oauth2", Redirect: "javascript:alert(1)", StatusCode: 200, CustomLogin: true}
	out := renderAuthTemplate(t, "sign_in.html", "sign_in.html", d)
	if regexp.MustCompile(`href="javascript:`).MatchString(out) || regexp.MustCompile(`action="javascript:`).MatchString(out) {
		t.Error("rd = javascript: : utilisé comme lien ou action")
	}
}

// Page d'erreur : 429 inchangée (titre, texte, bouton Réessayer vers la connexion), 403 « Accès refusé », message hostile échappé ;
// le message d'échec de connexion n'y figure jamais.
func TestSignIn172ErrorPageUnchanged(t *testing.T) {
	d := proxyPage{ProxyPrefix: "/oauth2", Redirect: "/"}
	d.StatusCode = 429
	out := renderAuthTemplate(t, "error.html", "error.html", d)
	for _, w := range []string{"Trop de tentatives", `href="/oauth2/sign_in"`, "Réessayer"} {
		if !strings.Contains(out, w) {
			t.Errorf("error.html 429 sans « %s »", w)
		}
	}
	if !strings.Contains(out, "<title>Trop de tentatives — Claude Launcher</title>") {
		t.Errorf("titre de la page 429 modifié : %s", regexp.MustCompile(`<title>[^<]*</title>`).FindString(out))
	}
	for _, status := range []int{429, 403, 500, 401, 400} {
		d.StatusCode = status
		d.Message = `<script>alert(1)</script>`
		page := renderAuthTemplate(t, "error.html", "error.html", d)
		if strings.Contains(page, signInFailure) {
			t.Errorf("error.html %d : contient le message d'échec de connexion", status)
		}
		if strings.Contains(page, "<script") {
			t.Errorf("error.html %d : Message non échappé", status)
		}
	}
	d.StatusCode, d.Message = 403, ""
	if out := renderAuthTemplate(t, "error.html", "error.html", d); !strings.Contains(out, "Accès refusé") || !strings.Contains(out, "/sign_out?rd=%2Fsigned-out") {
		t.Error("error.html 403 : « Accès refusé » ou lien de déconnexion absent")
	}
}

// Traefik `errors` : status limité à 429 — un 401 / 400 (échec du formulaire) remplacé par /too-many masquerait le message.
func TestChartAuth172ErrorsMiddlewareOnly429(t *testing.T) {
	needHelm(t)
	for name, v := range map[string]m{"GitHub et local": t172Local(), "avec l'ingress externe": t172Ext()} {
		o := must(t, v)
		n := 0
		for _, mw := range o["Middleware"] {
			e, ok := get(mw, "spec", "errors").(m)
			if !ok {
				continue
			}
			n++
			st, _ := e["status"].([]any)
			if len(st) == 0 {
				t.Errorf("%s : %v : status vide (capterait toutes les erreurs)", name, get(mw, "metadata", "name"))
			}
			for _, s := range st {
				if fmt.Sprint(s) != "429" {
					t.Errorf("%s : %v : status %v, attendu seulement 429 (jamais 401 ni 400 : le message d'échec serait masqué)", name, get(mw, "metadata", "name"), s)
				}
			}
			if e["query"] != "/too-many" {
				t.Errorf("%s : query = %v, attendu /too-many", name, e["query"])
			}
		}
		if n == 0 {
			t.Errorf("%s : aucun middleware errors", name)
		}
	}
}
