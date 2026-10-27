package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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

	tea "github.com/charmbracelet/bubbletea"
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

func ngAsset() string { return "claude-launcher-ng-" + runtime.GOOS + "-" + runtime.GOARCH }

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
	exe, err := os.Executable()
	if err != nil {
		return "", "", ""
	}
	if releaseVerRe.MatchString(ngVersion) && versionNewer(ngVersion, tag) {
		bin = exe
	}
	if v := localLauncherVersion(); v != "" && versionNewer(v, tag) {
		if s, err := filepath.EvalSymlinks(scriptPath()); err == nil {
			script = s
		}
	}
	return exe, bin, script
}

type updDoneMsg struct {
	tag     string
	binDone bool
	err     error
}

// updateNowCmd : mise à jour lancée d'un clic sur l'annonce « ↑ NG … disponible ».
func updateNowCmd(tag string) tea.Cmd {
	return func() tea.Msg {
		if !releaseVerRe.MatchString(ngVersion) {
			return updDoneMsg{tag: tag, err: fmt.Errorf("binaire de dev (%s) : non remplacé", ngVersion)}
		}
		_, bin, script := updateTargets(tag)
		if bin == "" && script == "" {
			return updDoneMsg{tag: tag}
		}
		binDone, _, err := applyUpdate(releaseBase+"/"+tag, bin, script)
		return updDoneMsg{tag: tag, binDone: binDone, err: err}
	}
}

// restartOnNewBinary : après une mise à jour depuis le navigateur, relance le
// pane d'infos puis le navigateur lui-même sur le binaire installé (respawn-pane
// tue ce processus). Les projets et la zone tmux ne sont pas touchés.
func restartOnNewBinary() {
	exe, err := os.Executable() // sous Linux : chemin du fichier, même remplacé
	if err != nil {
		return
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
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
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
	b, err := httpGet(url, 10*time.Second)
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
	body, err := httpGet(base+"/"+name, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("%s : %w", name, err)
	}
	h := sha256.Sum256(body)
	if hex.EncodeToString(h[:]) != want {
		return fmt.Errorf("%s : somme SHA-256 incorrecte", name)
	}
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
