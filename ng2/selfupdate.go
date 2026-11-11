package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Mise à jour automatique au lancement (« shell ») : si la dernière release est
// plus récente, le binaire NG et ~/claude-launcher.sh (simple redirection de
// l'ancienne commande depuis la v4) sont remplacés,
// puis le NG se relance. Le navigateur d'une coque déjà ouverte, resté sur
// l'ancien binaire, est redémarré par runShell : les projets ne sont pas touchés.
//
// Désactivation : --no-update ou CL_NO_UPDATE=1. Un binaire compilé à la main
// (version absente ou suffixée, ex. « dev », « v3.0.1+fix ») ne se remplace pas.

// releaseBase : racine des fichiers de release (CL_RELEASE_BASE la remplace, pour les tests).
var releaseBase = func() string {
	if b := os.Getenv("CL_RELEASE_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://github.com/" + launcherRepo + "/releases/download"
}()

var releaseVerRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func ngAsset() string { return "claude-launcher-ng2-" + runtime.GOOS + "-" + runtime.GOARCH }

func updateDisabled() bool {
	if os.Getenv("CL_NO_UPDATE") == "1" {
		return true
	}
	for _, a := range os.Args[1:] {
		if a == "--no-update" {
			return true
		}
	}
	return false
}

// selfUpdate : à appeler avant runShell. Hors ligne ou en cas d'échec, on
// démarre simplement avec la version en place. Si le binaire a été remplacé,
// le processus est remplacé par le nouveau binaire (pas de retour).
func selfUpdate() {
	if updateDisabled() {
		return
	}
	tag, _, err := latestTag(launcherRepo, "CL_FAKE_LATEST_LAUNCHER")
	if err != nil || !releaseVerRe.MatchString(tag) {
		return
	}
	exe, bin, script := updateTargets(tag)
	if bin == "" && script == "" {
		return
	}
	if !releaseReady(releaseBase + "/" + tag) {
		fmt.Printf("\033[38;5;208m  ↑ %s %s : release pas encore publiée — démarrage avec la version en place\033[0m\n", ngName, tag)
		return
	}
	fmt.Printf("\033[1;32m  ↑ %s %s disponible — mise à jour…\033[0m\n", ngName, tag)
	binDone, scriptDone, err := applyUpdate(releaseBase+"/"+tag, bin, script)
	if scriptDone {
		fmt.Println("  ✓ script mis à jour :", script)
	}
	if err != nil {
		fmt.Printf("\033[1;33m  ⚠ mise à jour incomplète : %v — démarrage avec la version en place\033[0m\n", err)
		time.Sleep(2 * time.Second)
	}
	if binDone {
		fmt.Println("  ✓ launcher mis à jour. Relancement…")
		_ = os.Setenv("CL_NO_UPDATE", "1") // le nouveau binaire ne revérifie pas
		err := syscall.Exec(exe, os.Args, os.Environ())
		fmt.Fprintln(os.Stderr, "relance impossible :", err)
		os.Exit(1)
	}
}

// updateTargets : fichiers à remplacer pour passer à tag (chemin vide : à jour,
// ou binaire de dev qui ne se remplace pas).
func updateTargets(tag string) (exe, bin, script string) {
	exe, err := selfExe()
	if err != nil {
		return "", "", ""
	}
	if releaseVerRe.MatchString(ngVersion) && versionNewer(ngVersion, tag) {
		bin = exe
	}
	// v5 ne livre plus claude-launcher.sh (redirection v3 → v4) : seul le binaire est mis à jour.
	return exe, bin, ""
}

type updDoneMsg struct {
	tag     string
	binDone bool
	err     error
	stale   bool // binaire préparé disparu ou modifié : à repréparer
}

// restartOnNewBinary : après une mise à jour depuis le navigateur, relance le
// pane d'infos puis le navigateur lui-même sur le binaire installé (respawn-pane
// tue ce processus). Les projets et la zone tmux ne sont pas touchés.
func restartOnNewBinary() {
	exe, err := selfExe() // chemin du fichier, même remplacé ou après un remontage
	if err != nil {
		return
	}
	if p := outerPane(roleInfo); p != "" {
		_ = otmux("respawn-pane", "-k", "-t", p, stateCommand(exe)).Run()
	}
	_ = otmux("respawn-pane", "-k", "-t", outerPane(roleNav), navCommand(exe)).Run()
}

// applyUpdate télécharge depuis base (…/releases/download/<tag>) le binaire de
// la plateforme vers bin et le script vers script (chemin vide : ignoré). Chaque
// fichier est vérifié contre SHA256SUMS avant de remplacer l'existant d'un seul
// rename : jamais de fichier à moitié écrit.
func applyUpdate(base, bin, script string) (binDone, scriptDone bool, err error) {
	sums, err := fetchSums(base + "/SHA256SUMS")
	if err != nil {
		return false, false, err
	}
	var errs []string
	if script != "" {
		if err := replaceVerified(base, "claude-launcher.sh", sums, script); err != nil {
			errs = append(errs, err.Error())
		} else {
			scriptDone = true
		}
	}
	if bin != "" {
		if err := replaceVerified(base, ngAsset(), sums, bin); err != nil {
			errs = append(errs, err.Error())
		} else {
			binDone = true
		}
	}
	if len(errs) > 0 {
		err = fmt.Errorf("%s", strings.Join(errs, " ; "))
	}
	return binDone, scriptDone, err
}

func httpGet(url string, timeout time.Duration) ([]byte, error) {
	return httpGetP(url, timeout, "", nil)
}

// progress : octets reçus, taille totale (≤ 0 : inconnue) ; (-1, -1) : passage par l'API.
type progress func(n, total int64)

// httpGetP : GET avec en-tête Accept facultatif et suivi de l'avancement.
func httpGetP(url string, timeout time.Duration, accept string, prog progress) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if prog == nil {
		return io.ReadAll(resp.Body)
	}
	var buf []byte
	chunk := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(chunk)
		buf = append(buf, chunk[:n]...)
		prog(int64(len(buf)), resp.ContentLength)
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// releaseGet : fichier de release. Le lien direct (github.com/…/releases/download)
// peut répondre 5xx pour certains fichiers alors que l'API les sert (vu en v5.3.0 :
// 504 constant) : un nouvel essai, puis l'API (releases/tags/<tag> → assets/<id>).
func releaseGet(url string, timeout time.Duration) ([]byte, error) {
	return releaseGetP(url, timeout, nil)
}

func releaseGetP(url string, timeout time.Duration, prog progress) ([]byte, error) {
	b, err := httpGetP(url, timeout, "", prog)
	if err == nil || !retryable(err) {
		return b, err
	}
	time.Sleep(releaseRetryWait)
	if b, err = httpGetP(url, timeout, "", prog); err == nil || !retryable(err) {
		return b, err
	}
	if !strings.HasPrefix(url, releaseBase+"/") {
		return nil, err
	}
	rest := strings.SplitN(strings.TrimPrefix(url, releaseBase+"/"), "/", 2) // <tag>/<nom>
	if len(rest) != 2 {
		return nil, err
	}
	if prog != nil {
		prog(-1, -1) // repli par l'API (affiché comme une étape)
	}
	b, err2 := apiAsset(rest[0], rest[1], timeout, prog)
	if err2 != nil {
		return nil, fmt.Errorf("%v ; par l'API : %v", err, err2)
	}
	return b, nil
}

var releaseRetryWait = 2 * time.Second

// retryable : erreur réseau ou réponse 5xx (pas un 404 : fichier absent).
func retryable(err error) bool {
	m := err.Error()
	return !strings.HasPrefix(m, "HTTP ") || strings.HasPrefix(m, "HTTP 5")
}

// apiAsset : fichier name de la release tag, par l'API GitHub.
func apiAsset(tag, name string, timeout time.Duration, prog progress) ([]byte, error) {
	rel, err := releaseAssets(tag)
	if err != nil {
		return nil, err
	}
	for _, a := range rel {
		if a.Name == name {
			return httpGetP(fmt.Sprintf("%s/repos/%s/releases/assets/%d", apiBase, launcherRepo, a.ID), timeout, "application/octet-stream", prog)
		}
	}
	return nil, fmt.Errorf("%s absent de la release %s", name, tag)
}

type relAsset struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// releaseAssets : fichiers de la release tag du launcher, selon l'API.
func releaseAssets(tag string) ([]relAsset, error) {
	b, err := httpGet(apiBase+"/repos/"+launcherRepo+"/releases/tags/"+tag, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var rel struct {
		Assets []relAsset `json:"assets"`
	}
	if err := json.Unmarshal(b, &rel); err != nil {
		return nil, err
	}
	return rel.Assets, nil
}

// releasePublished : la release tag porte-t-elle SHA256SUMS et le binaire de la
// plateforme ? Selon l'API (indépendante des liens directs, parfois en 504) ;
// API injoignable (autre qu'un 404) : par le lien direct de SHA256SUMS.
func releasePublished(tag string) bool {
	as, err := releaseAssets(tag)
	if err != nil {
		if err.Error() == "HTTP 404" {
			return false
		}
		return releaseReady(releaseBase + "/" + tag)
	}
	got := map[string]bool{}
	for _, a := range as {
		if a.State == "" || a.State == "uploaded" {
			got[a.Name] = true
		}
	}
	return got["SHA256SUMS"] && got[ngAsset()]
}

// releaseReady : la release est-elle téléchargeable ? Le tag existe dès le push,
// les fichiers seulement à la fin de la CI.
func releaseReady(base string) bool {
	sums, err := fetchSums(base + "/SHA256SUMS")
	_, ok := sums[ngAsset()]
	return err == nil && ok
}

// fetchSums : « <sha256>  <nom> » par ligne (format de sha256sum).
func fetchSums(url string) (map[string]string, error) {
	b, err := releaseGet(url, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("SHA256SUMS : %w", err)
	}
	sums := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			sums[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return sums, nil
}

func replaceVerified(base, name string, sums map[string]string, dst string) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("%s absent de SHA256SUMS", name)
	}
	body, err := releaseGet(base+"/"+name, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("%s : %w", name, err)
	}
	if sumOf(body) != want {
		return fmt.Errorf("%s : somme SHA-256 incorrecte", name)
	}
	return writeAtomic(dst, body)
}

func sumOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// writeAtomic : remplace dst par body d'un seul rename (fichier temporaire à
// côté de dst : même système de fichiers), droits de dst conservés.
func writeAtomic(dst string, body []byte) error {
	mode := os.FileMode(0o755)
	if st, err := os.Stat(dst); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return fmt.Errorf("%s : %w", dst, err)
	}
	defer os.Remove(tmp.Name()) // sans effet après le rename
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("%s : %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s : %w", dst, err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("%s : %w", dst, err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("%s : %w", dst, err)
	}
	return nil
}
