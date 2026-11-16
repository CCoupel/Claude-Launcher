package core

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ── safe.directory par appel (#158) ───────────────────────────────────────
//
// Un projet sur un partage qui appartient à un autre compte (Windows → WSL /
// MooseFS) est refusé par git (« dubious ownership »), donc par gh qui appelle
// git pour trouver le remote. Les appels git et gh DU LAUNCHER autorisent le seul
// dossier du projet concerné par l'environnement de l'appel (git ≥ 2.31 :
// GIT_CONFIG_COUNT / KEY_n / VALUE_n) : la configuration git de l'utilisateur
// n'est jamais modifiée, et jamais « * ».

// WithSafeDirectory : env, plus safe.directory=<dir> (et ses formes équivalentes
// pour git) ; les entrées GIT_CONFIG_* existantes sont conservées.
func WithSafeDirectory(env []string, dir string) []string {
	return withSafeDirectory(env, safeDirValues(dir, runtime.GOOS == "windows"))
}

func withSafeDirectory(env []string, values []string) []string {
	n := 0
	out := make([]string, 0, len(env)+2*len(values)+1)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			if c, err := strconv.Atoi(v); err == nil && c > 0 {
				n = c
			}
			continue // réécrit plus bas
		}
		out = append(out, kv)
	}
	for _, v := range values {
		i := strconv.Itoa(n)
		out = append(out, "GIT_CONFIG_KEY_"+i+"=safe.directory", "GIT_CONFIG_VALUE_"+i+"="+v)
		n++
	}
	return append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n))
}

// safeDirValues : le chemin tel que git le voit — barres obliques, chemin réel
// (liens suivis) ; pour un partage réseau Windows (UNC), aussi la forme
// %(prefix)/… que git recommande. Aucune valeur générique.
func safeDirValues(dir string, windows bool) []string {
	var vs []string
	add := func(v string) {
		if v == "" || v == "*" {
			return
		}
		for _, o := range vs {
			if o == v {
				return
			}
		}
		vs = append(vs, v)
	}
	p := dir
	if windows {
		p = strings.ReplaceAll(dir, `\`, "/")
	} else {
		p = filepath.ToSlash(filepath.Clean(dir))
	}
	p = strings.TrimRight(p, "/")
	add(p)
	if windows && strings.HasPrefix(p, "//") {
		add("%(prefix)/" + p) // forme proposée par git pour les chemins UNC
	}
	if !windows {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			add(filepath.ToSlash(real))
		}
	}
	return vs
}

// SafeDirectoryHelp : commande à lancer une fois si git refuse malgré tout le
// dépôt (git trop ancien pour GIT_CONFIG_COUNT, autre outil).
func SafeDirectoryHelp(dir string) string {
	p := strings.ReplaceAll(dir, `\`, "/")
	return "git config --global --add safe.directory '" + p + "'"
}
