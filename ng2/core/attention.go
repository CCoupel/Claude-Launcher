package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ── Teamleader en attente de réponse (#223, contracts/attention.md) ───────
//
// Hooks Claude Code posés par le launcher dans settings.json : le teamleader qui pose une question (PreToolUse
// AskUserQuestion) ou demande une permission (Notification) écrit sa charge utile dans
// <projet>/.claude-launcher/attention/<pane>.json ; elle est effacée à la fin de l'attente. Lu par chaque launcher.

// attentionHookMark : marque des groupes de hooks du launcher dans settings.json (remplacés si la commande change).
const attentionHookMark = "# cl-attention"

// Commandes sh POSIX (aussi dans les conteneurs et les pods), sans jq ni binaire du launcher ; sortie toujours 0 et
// jamais de sortie sur stdout. Le numéro de pane est validé (chiffres seuls) : aucune injection par TMUX_PANE.
const (
	attentionBeginCmd = `d="$CL_ATTENTION_DIR"; p="${TMUX_PANE#%}"; case "$p" in ''|*[!0-9]*) p=;; esac; ` +
		`[ -n "$d" ] && [ -n "$p" ] && mkdir -p "$d" && head -c 65536 > "$d/.$p.tmp" && mv -f "$d/.$p.tmp" "$d/$p.json"; true ` + attentionHookMark
	attentionEndCmd = `d="$CL_ATTENTION_DIR"; p="${TMUX_PANE#%}"; case "$p" in ''|*[!0-9]*) p=;; esac; ` +
		`[ -n "$d" ] && [ -n "$p" ] && [ -e "$d/$p.json" ] && rm -f "$d/$p.json"; true ` + attentionHookMark
	// fin d'attente sur PostToolUse (sans matcher : le nom de l'outil de la permission n'est pas connu d'avance) : une
	// question n'est close que par la fin de AskUserQuestion — un autre outil du leader qui se termine pendant qu'elle
	// est ouverte ne retire plus le badge ; une permission (état « Notification ») l'est par tout outil terminé.
	attentionEndToolCmd = `d="$CL_ATTENTION_DIR"; p="${TMUX_PANE#%}"; case "$p" in ''|*[!0-9]*) p=;; esac; ` +
		`f="$d/$p.json"; [ -n "$d" ] && [ -n "$p" ] && [ -f "$f" ] || exit 0; i=$(head -c 65536); ` +
		`if grep -Eq '"hook_event_name" *: *"Notification"' "$f" || printf '%s' "$i" | grep -Eq '"tool_name" *: *"AskUserQuestion"'; then rm -f "$f"; fi; true ` + attentionHookMark
)

// attentionHookSpec : évènement Claude Code → (matcher, commande). Notification : les noms de matcher existent dans
// claude 2.1.330 (permission_prompt, elicitation_dialog) ; jamais idle_prompt ni Stop comme signal d'attente.
type attentionHook struct{ event, matcher, cmd string }

var attentionHooks = []attentionHook{
	{"PreToolUse", "AskUserQuestion", attentionBeginCmd},
	{"Notification", "permission_prompt|elicitation_dialog", attentionBeginCmd},
	{"PostToolUse", "", attentionEndToolCmd},
	{"UserPromptSubmit", "", attentionEndCmd},
	{"Stop", "", attentionEndCmd},
	{"SessionEnd", "", attentionEndCmd},
}

// SyncAttentionHook : hooks d'attente du launcher dans <dir>/settings.json, posés une fois (idempotent) ; les autres
// hooks sont gardés ; JSON illisible : laissé tel quel. Même fichier et même verrou (#176) que SyncSettingsEnv.
func SyncAttentionHook(dir string) error {
	return WithFileLock(filepath.Join(dir, "settings.json"), func() error { return syncAttentionHook(dir) })
}

func syncAttentionHook(dir string) error {
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
	hooks := map[string]json.RawMessage{}
	if raw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return err
		}
	}
	enc := func(v any) (json.RawMessage, error) {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		if err := e.Encode(v); err != nil {
			return nil, err
		}
		return json.RawMessage(bytes.TrimSpace(b.Bytes())), nil
	}
	changed := false
	// évènements portant des groupes du launcher : les voulus, plus d'éventuels restes d'une version précédente
	events := map[string]bool{}
	for k := range hooks {
		events[k] = true
	}
	for _, h := range attentionHooks {
		events[h.event] = true
	}
	for ev := range events {
		var groups []map[string]any
		if raw, ok := hooks[ev]; ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return err
			}
		}
		var want []attentionHook
		for _, h := range attentionHooks {
			if h.event == ev {
				want = append(want, h)
			}
		}
		var kept []map[string]any
		have := map[string]bool{}
		evChanged := false
		for _, g := range groups {
			mine, cmd := isAttentionGroup(g)
			if !mine {
				kept = append(kept, g)
				continue
			}
			m, _ := g["matcher"].(string)
			ok := false
			for _, h := range want {
				if h.matcher == m && h.cmd == cmd && !have[m] {
					ok, have[m] = true, true
				}
			}
			if ok {
				kept = append(kept, g)
			} else {
				changed, evChanged = true, true // commande périmée ou doublon : retiré
			}
		}
		for _, h := range want {
			if have[h.matcher] {
				continue
			}
			g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": h.cmd}}}
			if h.matcher != "" {
				g["matcher"] = h.matcher
			}
			kept = append(kept, g)
			changed, evChanged = true, true
		}
		if len(kept) == len(groups) && !evChanged {
			continue
		}
		if len(kept) == 0 {
			delete(hooks, ev)
			continue
		}
		var err error
		if hooks[ev], err = enc(kept); err != nil {
			return err
		}
	}
	if !changed {
		return nil // déjà en place
	}
	var err error
	if doc["hooks"], err = enc(hooks); err != nil {
		return err
	}
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes(), mode)
}

// isAttentionGroup : le groupe de hooks est celui du launcher (une seule commande portant la marque).
func isAttentionGroup(g map[string]any) (mine bool, cmd string) {
	hs, ok := g["hooks"].([]any)
	if !ok {
		return false, ""
	}
	for _, h := range hs {
		if hm, ok := h.(map[string]any); ok {
			if c, _ := hm["command"].(string); strings.Contains(c, attentionHookMark) {
				if len(hs) != 1 {
					return true, "" // groupe mêlé : refait
				}
				return true, c
			}
		}
	}
	return false, ""
}

// ── Fichier d'état ────────────────────────────────────────────────────────

// Attention : un teamleader attend une réponse.
type Attention struct {
	Kind  string    `json:"kind"` // question | permission
	Text  string    `json:"text"` // question ou permission, ≤ 200 caractères
	Since time.Time `json:"since"`
	ID    string    `json:"id"`   // <pane>-<since en ms> : une attente = un id
	Pane  string    `json:"pane"` // numéro du pane (sans %)
}

// attentionMaxAge : une attente plus ancienne est ignorée (et supprimée).
const attentionMaxAge = 24 * time.Hour

// attentionTextMax : longueur maximale du texte (caractères).
const attentionTextMax = 200

// AttentionDir : dossier d'état d'un projet (vu du launcher).
func AttentionDir(proj string) string { return filepath.Join(proj, ".claude-launcher", "attention") }

// EnsureAttentionExclude : /.claude-launcher/attention/ dans .git/info/exclude du projet (jamais commité).
func EnsureAttentionExclude(proj string) { excludeFromGit(proj, attentionExclude) }

const attentionExclude = "/.claude-launcher/attention/"

// parseAttention : interprétation de la charge utile d'un hook (contrat § 4) ; faux si autre chose / illisible.
func parseAttention(b []byte) (kind, text string, ok bool) {
	var p struct {
		Event     string `json:"hook_event_name"`
		Message   string `json:"message"`
		ToolInput struct {
			Questions []struct {
				Question string `json:"question"`
			} `json:"questions"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(b, &p) != nil {
		return "", "", false
	}
	switch p.Event {
	case "PreToolUse":
		if len(p.ToolInput.Questions) > 0 {
			text = p.ToolInput.Questions[0].Question
		}
		return "question", cleanAttentionText(text), true
	case "Notification":
		return "permission", cleanAttentionText(p.Message), true
	}
	return "", "", false
}

// cleanAttentionText : caractères de contrôle retirés, tronqué à 200 caractères.
func cleanAttentionText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > attentionTextMax {
		s = string(r[:attentionTextMax-1]) + "…"
	}
	return s
}

// ReadAttention : l'attente en cours du projet — le fichier valide le plus récent. Les fichiers de plus de 24 h sont
// supprimés.
func ReadAttention(proj string) (Attention, bool) { return ReadAttentionOf(proj, "") }

// ReadAttentionOf : ReadAttention restreinte au pane leader (« %5 » ou « 5 ») quand il est connu — pour un projet
// ouvert ici, seul le fichier du pane leader compte (garde-fou si une version de claude transmettait
// CL_ATTENTION_DIR aux teammates) ; leader vide : pas de filtre.
func ReadAttentionOf(proj, leader string) (Attention, bool) {
	dir := AttentionDir(proj)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return Attention{}, false
	}
	leader = strings.TrimPrefix(leader, "%")
	var best Attention
	found := false
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		pane := strings.TrimSuffix(n, ".json")
		if _, err := strconv.Atoi(pane); err != nil {
			continue
		}
		f := filepath.Join(dir, n)
		st, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(st.ModTime()) > attentionMaxAge {
			_ = os.Remove(f)
			continue
		}
		if leader != "" && pane != leader {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		kind, text, ok := parseAttention(b)
		if !ok {
			continue
		}
		a := Attention{Kind: kind, Text: text, Since: st.ModTime(), Pane: pane, ID: fmt.Sprintf("%s-%d", pane, st.ModTime().UnixMilli())}
		if !found || a.Since.After(best.Since) {
			best, found = a, true
		}
	}
	return best, found
}
