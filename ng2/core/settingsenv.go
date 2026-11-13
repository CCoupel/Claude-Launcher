package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Variables d'environnement du launcher pour toutes les sessions claude :
// recopiées dans le bloc « env » du settings.json utilisateur
// ($CLAUDE_CONFIG_DIR/settings.json). Claude le lit quel que soit son dossier
// de lancement — projet, worktree, teammate démarré dans un sous-dossier (dont
// le .claude/settings.local.json du projet n'est pas vu), hôte ou conteneur —
// au lieu de les taper dans le shell du premier pane, que les teammates (panes
// tmux créés par claude) n'héritent pas.
//
// Seules les clés du launcher sont touchées ; le reste du fichier est gardé.
// La liste des clés écrites est tenue dans claude-launcher/settings-env.json,
// pour retirer une variable enlevée de la config. Un settings.local.json de
// projet l'emporte toujours sur ces valeurs.

type envVar struct{ K, V string }

// EnvVars : GITHUB_TOKEN (GH_TOKEN et GITHUB_PERSONAL_ACCESS_TOKEN),
// CLAUDE_DISABLE_MOUSE, CLAUDE_EXPERIMENTAL_TEAMS, puis EXTRA_ENVS (« NOM=valeur »,
// une paire de guillemets autour de la valeur ôtée) ; la dernière l'emporte.
func (c Config) EnvVars() []envVar {
	var v []envVar
	if c.GithubToken != "" {
		v = append(v, envVar{"GITHUB_PERSONAL_ACCESS_TOKEN", c.GithubToken}, envVar{"GH_TOKEN", c.GithubToken})
	}
	if c.DisableMouse {
		v = append(v, envVar{"CLAUDE_CODE_DISABLE_MOUSE", "1"})
	}
	if c.ExpTeams {
		v = append(v, envVar{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "1"})
	}
	for _, e := range c.ExtraEnvs {
		k, val, ok := strings.Cut(strings.TrimSpace(e), "=")
		if !ok || !validEnvName(k) {
			continue
		}
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		v = append(v, envVar{k, val})
	}
	return v
}

func validEnvName(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, r := range k {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// secretEnv : nom d'une variable à ne jamais taper dans un shell (historique,
// scrollback tmux) : elle ne passe que par settings.json.
func secretEnv(k string) bool {
	u := strings.ToUpper(k)
	for _, s := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

func settingsEnvKeysPath(dir string) string {
	return filepath.Join(dir, "claude-launcher", "settings-env.json")
}

// SyncSettingsEnv : met le bloc env de <dir>/settings.json à jour avec vars ;
// n'écrit que si quelque chose change. Fichier illisible (JSON invalide) :
// laissé tel quel. Une valeur secrète présente : fichier en 0600.
func SyncSettingsEnv(dir string, vars []envVar) error {
	path := filepath.Join(dir, "settings.json")
	doc := map[string]json.RawMessage{}
	mode := os.FileMode(0o644)
	if b, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &doc); err != nil {
				return err
			}
		}
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	env := map[string]string{}
	if raw, ok := doc["env"]; ok {
		if err := json.Unmarshal(raw, &env); err != nil {
			return err
		}
	}
	var prev []string
	if b, err := os.ReadFile(settingsEnvKeysPath(dir)); err == nil {
		_ = json.Unmarshal(b, &prev)
	}

	changed := false
	want := map[string]string{}
	for _, v := range vars {
		want[v.K] = v.V
	}
	for _, k := range prev { // variable retirée de la config du launcher
		if _, ok := want[k]; !ok {
			if _, ok := env[k]; ok {
				delete(env, k)
				changed = true
			}
		}
	}
	secret := false
	for k, v := range want {
		if env[k] != v {
			env[k] = v
			changed = true
		}
		secret = secret || secretEnv(k)
	}
	if secret && mode&0o077 != 0 {
		mode, changed = 0o600, true
	}
	if changed {
		var eb bytes.Buffer
		ee := json.NewEncoder(&eb)
		ee.SetEscapeHTML(false)
		if err := ee.Encode(env); err != nil {
			return err
		}
		doc["env"] = json.RawMessage(bytes.TrimSpace(eb.Bytes()))
		if len(env) == 0 {
			delete(doc, "env")
		}
		// sans échappement HTML (« 2>&1 » d'une commande reste lisible)
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			return err
		}
		out := bytes.TrimRight(buf.Bytes(), "\n")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp := path + ".cl-tmp"
		if err := os.WriteFile(tmp, append(out, '\n'), mode); err != nil {
			return err
		}
		_ = os.Chmod(tmp, mode)
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !sameStrings(keys, prev) {
		b, _ := json.Marshal(keys)
		if err := os.MkdirAll(filepath.Dir(settingsEnvKeysPath(dir)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(settingsEnvKeysPath(dir), b, 0o644)
	}
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	s := append([]string{}, b...)
	sort.Strings(s)
	for i := range a {
		if a[i] != s[i] {
			return false
		}
	}
	return true
}
