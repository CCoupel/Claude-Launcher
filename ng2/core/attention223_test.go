package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #223 (LOT-3a v6.7.0) — teamleader en attente : hooks, état, lecture. Contrat : contracts/attention.md.
// Les commandes des hooks posés dans settings.json sont EXÉCUTÉES (sh) avec une charge utile de faux claude : on teste ce
// que claude exécuterait vraiment. Tout sous dossiers temporaires, jamais le settings.json réel (CLAUDE_CONFIG_DIR isolé).

const attMark = "# cl-attention"

type hookGroup struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

func readHooks(t *testing.T, dir string) map[string][]hookGroup {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json : %v", err)
	}
	var doc struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("settings.json invalide : %v\n%s", err, b)
	}
	return doc.Hooks
}

// ours : groupes du launcher (marque # cl-attention) d'un évènement.
func ours(hooks map[string][]hookGroup, event string) (out []hookGroup) {
	for _, g := range hooks[event] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, attMark) {
				out = append(out, g)
				break
			}
		}
	}
	return
}

func commandOf(g hookGroup) string {
	for _, h := range g.Hooks {
		if strings.Contains(h.Command, attMark) {
			return h.Command
		}
	}
	return ""
}

// ── settings.json : hooks posés une fois, autres conservés ────────────────────────────

func TestSyncAttentionHookEvents223(t *testing.T) {
	unixOnly(t)
	d := t.TempDir()
	if err := SyncAttentionHook(d); err != nil {
		t.Fatal(err)
	}
	h := readHooks(t, d)
	// signaux d'attente : question (PreToolUse AskUserQuestion) et permission (Notification)
	pre := ours(h, "PreToolUse")
	if len(pre) != 1 || pre[0].Matcher != "AskUserQuestion" {
		t.Errorf("PreToolUse : %+v, attendu un groupe filtré sur AskUserQuestion", pre)
	}
	if n := ours(h, "Notification"); len(n) != 1 || n[0].Matcher == "idle_prompt" {
		t.Errorf("Notification : %+v, attendu un groupe (permission), jamais idle_prompt", n)
	}
	// fins d'attente
	for _, ev := range []string{"PostToolUse", "UserPromptSubmit", "Stop", "SessionEnd"} {
		g := ours(h, ev)
		if len(g) != 1 {
			t.Errorf("%s : %d groupe(s) du launcher, attendu 1", ev, len(g))
			continue
		}
		if cmd := commandOf(g[0]); !strings.Contains(cmd, "rm -f") || strings.Contains(cmd, "mv -f") {
			t.Errorf("%s : doit seulement retirer l'état : %s", ev, cmd)
		}
	}
	// Stop / PostToolUse ne SIGNALENT jamais une attente
	for _, ev := range []string{"Stop", "PostToolUse", "UserPromptSubmit", "SessionEnd"} {
		for _, g := range ours(h, ev) {
			if strings.Contains(commandOf(g), "mv -f") {
				t.Errorf("%s écrit un état d'attente : %s", ev, commandOf(g))
			}
		}
	}
}

func TestSyncAttentionHookIdempotentAndKeepsOthers223(t *testing.T) {
	unixOnly(t)
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	orig := `{"model":"opus","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo garde-moi"}]}],` +
		`"SessionStart":[{"hooks":[{"type":"command","command":"echo sess"}]}],` +
		`"Stop":[{"hooks":[{"type":"command","command":"echo stop-user"}]}],` +
		`"Notification":[{"matcher":"idle_prompt","hooks":[{"type":"command","command":"echo idle-user"}]}]}}`
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SyncAttentionHook(d); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	for i := 0; i < 3; i++ {
		if err := SyncAttentionHook(d); err != nil {
			t.Fatal(err)
		}
	}
	again, _ := os.ReadFile(p)
	if string(first) != string(again) {
		t.Errorf("non idempotent :\n%s\n---\n%s", first, again)
	}
	h := readHooks(t, d)
	for ev, n := range map[string]int{"PreToolUse": 1, "Notification": 1, "PostToolUse": 1, "UserPromptSubmit": 1, "Stop": 1, "SessionEnd": 1} {
		if got := len(ours(h, ev)); got != n {
			t.Errorf("%s : %d groupe(s) du launcher, attendu %d (posés une seule fois)", ev, got, n)
		}
	}
	// hooks de l'utilisateur et autres réglages conservés
	keep := map[string]string{"PreToolUse": "echo garde-moi", "SessionStart": "echo sess", "Stop": "echo stop-user", "Notification": "echo idle-user"}
	for ev, cmd := range keep {
		found := false
		for _, g := range h[ev] {
			for _, hk := range g.Hooks {
				found = found || hk.Command == cmd
			}
		}
		if !found {
			t.Errorf("hook utilisateur %q perdu dans %s", cmd, ev)
		}
	}
	var doc map[string]any
	_ = json.Unmarshal(again, &doc)
	if doc["model"] != "opus" {
		t.Errorf("autre réglage perdu : %v", doc)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("droits changés : %v", st.Mode().Perm())
	}
}

// Ancienne version de la commande du launcher : remplacée, pas dupliquée.
func TestSyncAttentionHookReplacesOldCommand223(t *testing.T) {
	d := t.TempDir()
	old := `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"echo ancien ` + attMark + `"}]}]}}`
	if err := os.WriteFile(filepath.Join(d, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SyncAttentionHook(d); err != nil {
		t.Fatal(err)
	}
	g := ours(readHooks(t, d), "PostToolUse")
	if len(g) != 1 || strings.Contains(commandOf(g[0]), "ancien") {
		t.Errorf("PostToolUse : %+v, attendu une seule commande à jour", g)
	}
}

// JSON illisible : laissé tel quel (jamais écrasé).
func TestSyncAttentionHookLeavesBrokenJSON223(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	const broken = "{ pas du json"
	_ = os.WriteFile(p, []byte(broken), 0o600)
	_ = SyncAttentionHook(d)
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Errorf("settings.json illisible modifié : %s", b)
	}
}

// SyncLauncherEnv pose les hooks (chemin réel d'une ouverture) dans le CLAUDE_CONFIG_DIR du test.
func TestSyncLauncherEnvInstallsAttentionHooks223(t *testing.T) {
	unixOnly(t)
	root := t.TempDir()
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CL_CONFIG_DIR", "CLAUDE_CONFIG_DIR"} {
		d := filepath.Join(root, strings.ToLower(k))
		_ = os.MkdirAll(d, 0o755)
		t.Setenv(k, d)
	}
	winHome(t, filepath.Join(root, "home"))
	SyncLauncherEnv(Config{})
	if n := len(ours(readHooks(t, ClaudeDir()), "PreToolUse")); n != 1 {
		t.Errorf("PreToolUse : %d groupe(s) après SyncLauncherEnv, attendu 1", n)
	}
}

// ── Les commandes, exécutées ──────────────────────────────────────────────────────────

const (
	payloadQuestion = `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Valider le plan ?","header":"Plan","options":[{"label":"Oui"},{"label":"Non"}]}]}}`
	payloadPerm     = `{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude a besoin de votre permission pour utiliser Bash"}`
)

// hookCommand : commande du launcher pour l'évènement (le groupe d'attente pour PreToolUse / Notification).
func hookCommand(t *testing.T, event string) string {
	t.Helper()
	d := t.TempDir()
	if err := SyncAttentionHook(d); err != nil {
		t.Fatal(err)
	}
	g := ours(readHooks(t, d), event)
	if len(g) != 1 {
		t.Fatalf("%s : %d groupe(s)", event, len(g))
	}
	return commandOf(g[0])
}

// runHook : exécute la commande sous sh avec un environnement MINIMAL (comme claude le ferait) ; rend stdout.
func runHook(t *testing.T, cmd, stdin string, env ...string) string {
	t.Helper()
	c := exec.Command("sh", "-c", cmd)
	c.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	c.Stdin = strings.NewReader(stdin)
	out, err := c.Output()
	if err != nil {
		t.Fatalf("hook : %v (sortie toujours 0)", err)
	}
	return string(out)
}

func TestAttentionHookWritesStateForLeaderPane223(t *testing.T) {
	unixOnly(t)
	state := filepath.Join(t.TempDir(), "proj", ".claude-launcher", "attention")
	for _, tc := range []struct{ event, payload string }{{"PreToolUse", payloadQuestion}, {"Notification", payloadPerm}} {
		cmd := hookCommand(t, tc.event)
		if out := runHook(t, cmd, tc.payload, "CL_ATTENTION_DIR="+state, "TMUX_PANE=%7"); out != "" {
			t.Errorf("%s : sortie sur stdout %q (aucune décision ne doit être rendue)", tc.event, out)
		}
		b, err := os.ReadFile(filepath.Join(state, "7.json"))
		if err != nil || string(b) != tc.payload {
			t.Errorf("%s : état = %q (%v), attendu la charge utile brute", tc.event, b, err)
		}
		if tmp, _ := filepath.Glob(filepath.Join(state, ".*.tmp")); len(tmp) > 0 {
			t.Errorf("%s : fichier temporaire laissé : %v", tc.event, tmp)
		}
		_ = os.RemoveAll(state)
	}
}

// Sans CL_ATTENTION_DIR (session lancée hors launcher) ou avec un numéro de pane douteux : rien n'est écrit, aucune injection.
func TestAttentionHookNoopAndNoInjection223(t *testing.T) {
	unixOnly(t)
	cmd := hookCommand(t, "PreToolUse")
	work := t.TempDir()
	state := filepath.Join(work, "attention")
	runHook(t, cmd, payloadQuestion, "TMUX_PANE=%7") // pas de CL_ATTENTION_DIR
	for _, pane := range []string{"", "7", "%", "%a", "%7;touch " + filepath.Join(work, "PWNED"), "%7 8", "%$(touch " + filepath.Join(work, "PWNED") + ")", "../x", "%-1"} {
		env := []string{"CL_ATTENTION_DIR=" + state}
		if pane != "" {
			env = append(env, "TMUX_PANE="+pane)
		}
		runHook(t, cmd, payloadQuestion, env...)
	}
	if _, err := os.Stat(filepath.Join(work, "PWNED")); err == nil {
		t.Fatal("injection : une commande a été exécutée via TMUX_PANE")
	}
	if ents, _ := os.ReadDir(state); len(ents) > 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		// « 7 » sans « % » est un numéro valide pour le hook (la marque % est retirée si présente) : seul ce fichier est permis
		for _, n := range names {
			if n != "7.json" {
				t.Errorf("fichier d'état inattendu %q pour un pane douteux ou absent", n)
			}
		}
	}
}

// Charge utile > 64 Kio : tronquée à 65536 octets.
func TestAttentionHookTruncatesPayload223(t *testing.T) {
	unixOnly(t)
	state := filepath.Join(t.TempDir(), "attention")
	runHook(t, hookCommand(t, "Notification"), strings.Repeat("x", 200000), "CL_ATTENTION_DIR="+state, "TMUX_PANE=%3")
	if st, err := os.Stat(filepath.Join(state, "3.json")); err != nil || st.Size() != 65536 {
		t.Errorf("état : %v (%v), attendu 65536 octets", st, err)
	}
}

// Fin d'attente : l'état du pane est retiré ; sans état ni variable, sortie 0 et rien de créé.
func TestAttentionEndHooksRemoveState223(t *testing.T) {
	unixOnly(t)
	state := filepath.Join(t.TempDir(), "attention")
	for _, ev := range []string{"PostToolUse", "UserPromptSubmit", "Stop", "SessionEnd"} {
		cmd := hookCommand(t, ev)
		_ = os.MkdirAll(state, 0o755)
		for _, n := range []string{"7.json", "8.json"} {
			_ = os.WriteFile(filepath.Join(state, n), []byte(payloadQuestion), 0o644)
		}
		stdin := "{}"
		if ev == "PostToolUse" { // une question n'est close que par la fin de AskUserQuestion (revue LOT-3)
			runHook(t, cmd, `{"tool_name":"Bash"}`, "CL_ATTENTION_DIR="+state, "TMUX_PANE=%7")
			if _, err := os.Stat(filepath.Join(state, "7.json")); err != nil {
				t.Errorf("PostToolUse d'un autre outil : la question a été retirée")
			}
			stdin = `{"tool_name":"AskUserQuestion"}`
		}
		if out := runHook(t, cmd, stdin, "CL_ATTENTION_DIR="+state, "TMUX_PANE=%7"); out != "" {
			t.Errorf("%s : sortie %q", ev, out)
		}
		if _, err := os.Stat(filepath.Join(state, "7.json")); err == nil {
			t.Errorf("%s : état du pane 7 non retiré", ev)
		}
		if _, err := os.Stat(filepath.Join(state, "8.json")); err != nil {
			t.Errorf("%s : l'état d'un autre pane a été retiré", ev)
		}
		runHook(t, cmd, "{}", "CL_ATTENTION_DIR="+state, "TMUX_PANE=%9") // pas d'état : sans effet
		runHook(t, cmd, "{}", "TMUX_PANE=%7")                            // pas de variable : sans effet
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("?")
	}
}

// ── ReadAttention ─────────────────────────────────────────────────────────────────────

func attDir(t *testing.T) (proj, dir string) {
	t.Helper()
	proj = t.TempDir()
	dir = filepath.Join(proj, ".claude-launcher", "attention")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func putState(t *testing.T, dir, pane, content string, age time.Duration) {
	t.Helper()
	f := filepath.Join(dir, pane+".json")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	_ = os.Chtimes(f, mt, mt)
}

func TestReadAttentionKinds223(t *testing.T) {
	proj, dir := attDir(t)
	if _, ok := ReadAttention(proj); ok {
		t.Fatal("dossier vide : aucune attente")
	}
	putState(t, dir, "4", payloadQuestion, time.Second)
	a, ok := ReadAttention(proj)
	if !ok || a.Kind != "question" || a.Text != "Valider le plan ?" {
		t.Fatalf("question : %+v %v", a, ok)
	}
	if !strings.HasPrefix(a.ID, "4-") || a.Since.IsZero() {
		t.Errorf("id = %q, since = %v : attendu « 4-<ms> » et une date", a.ID, a.Since)
	}
	putState(t, dir, "4", payloadPerm, time.Second)
	a, ok = ReadAttention(proj)
	if !ok || a.Kind != "permission" || !strings.Contains(a.Text, "permission pour utiliser Bash") {
		t.Fatalf("permission : %+v %v", a, ok)
	}
}

func TestReadAttentionIgnoresUnusableState223(t *testing.T) {
	proj, dir := attDir(t)
	for name, content := range map[string]string{
		"1": "{pas du json",
		"2": `{"hook_event_name":"PostToolUse"}`,
		"3": `{"hook_event_name":"Stop"}`,
		"5": "",
	} {
		putState(t, dir, name, content, time.Second)
	}
	if a, ok := ReadAttention(proj); ok {
		t.Errorf("états inexploitables pris en compte : %+v", a)
	}
}

func TestReadAttentionIDChangesPerWait223(t *testing.T) {
	proj, dir := attDir(t)
	putState(t, dir, "4", payloadQuestion, 10*time.Second)
	a1, _ := ReadAttention(proj)
	putState(t, dir, "4", payloadQuestion, 2*time.Second)
	a2, _ := ReadAttention(proj)
	if a1.ID == "" || a1.ID == a2.ID {
		t.Errorf("une attente = un id : %q puis %q", a1.ID, a2.ID)
	}
	b1, _ := ReadAttention(proj)
	if a2.ID != b1.ID {
		t.Errorf("même fichier, id différent : %q / %q", a2.ID, b1.ID)
	}
}

// Texte tronqué à 200 caractères, caractères de contrôle retirés.
func TestReadAttentionSanitizesText223(t *testing.T) {
	proj, dir := attDir(t)
	q := strings.Repeat("é", 300)
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse",
		"tool_input": map[string]any{"questions": []map[string]any{{"question": "a\x1b[31mb\x00c\nd\te" + q}}}})
	putState(t, dir, "4", string(payload), time.Second)
	a, ok := ReadAttention(proj)
	if !ok {
		t.Fatal("attente non lue")
	}
	if n := len([]rune(a.Text)); n > 200 {
		t.Errorf("texte de %d caractères, attendu ≤ 200", n)
	}
	for _, r := range a.Text {
		if r < 0x20 || r == 0x7f {
			t.Errorf("caractère de contrôle %q conservé dans %q", r, a.Text)
		}
	}
}

// Plusieurs fichiers : le plus récent. Fichier de plus de 24 h : ignoré.
func TestReadAttentionNewestAndStale223(t *testing.T) {
	proj, dir := attDir(t)
	putState(t, dir, "4", payloadQuestion, 30*time.Second)
	putState(t, dir, "9", payloadPerm, 5*time.Second)
	if a, ok := ReadAttention(proj); !ok || a.Kind != "permission" || !strings.HasPrefix(a.ID, "9-") {
		t.Errorf("le plus récent attendu : %+v %v", a, ok)
	}
	proj2, dir2 := attDir(t)
	putState(t, dir2, "4", payloadQuestion, 25*time.Hour)
	if a, ok := ReadAttention(proj2); ok {
		t.Errorf("fichier de plus de 24 h pris en compte : %+v", a)
	}
}

// ── Export de CL_ATTENTION_DIR dans le script de démarrage ────────────────────────────

func TestStartExportsAttentionDir223(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	s.teamName(t, "alpha-team")
	s.team(t, "alpha-team", ClaudePath(s.proj)) // pas de question /start-session
	envFile := filepath.Join(filepath.Dir(s.argsFile), "env.txt")
	fake := filepath.Join(filepath.Dir(s.argsFile), "claude-env")
	script := "#!/bin/sh\nprintf '%s' \"$CL_ATTENTION_DIR\" > " + Shq(envFile) + "\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_CLAUDE_BIN", fake)
	stale := filepath.Join(s.proj, ".claude-launcher", "attention")
	_ = os.MkdirAll(stale, 0o755)
	_ = os.WriteFile(filepath.Join(stale, "7.json"), []byte(payloadQuestion), 0o644)

	start, _ := s.run(t, LaunchNormal, "\n")
	got, _ := os.ReadFile(envFile)
	if string(got) != stale {
		t.Errorf("CL_ATTENTION_DIR vu par claude = %q, attendu %q\n%s", got, stale, start)
	}
	if _, err := os.Stat(filepath.Join(stale, "7.json")); err == nil {
		t.Error("état d'une session précédente non effacé au lancement neuf")
	}
	if i, j := strings.Index(start, "rm -rf"), strings.Index(start, "CL_ATTENTION_DIR"); i < 0 || j < 0 {
		t.Errorf("script sans rm -rf / export de CL_ATTENTION_DIR :\n%s", start)
	}
}

// La variable n'est pas dans l'environnement tmux/shell hérité : seulement dans le script de démarrage de claude
// (les teammates ne l'héritent pas) — elle ne figure pas dans les exports communs de la session.
func TestAttentionDirNotInSessionExports223(t *testing.T) {
	if strings.Contains(Config{}.Exports(true), "CL_ATTENTION_DIR") {
		t.Error("CL_ATTENTION_DIR exporté dans l'environnement commun de la session : les teammates l'hériteraient")
	}
}

// Projet ouvert ici : seul le fichier du pane leader compte (garde-fou si un teammate héritait de la variable).
func TestReadAttentionLeaderFilter223(t *testing.T) {
	proj, dir := attDir(t)
	putState(t, dir, "4", payloadQuestion, 30*time.Second) // leader
	putState(t, dir, "9", payloadPerm, 5*time.Second)      // teammate, plus récent
	if a, ok := ReadAttentionOf(proj, "%4"); !ok || a.Kind != "question" || a.Pane != "4" {
		t.Errorf("leader %%4 : %+v %v, attendu l'attente du pane 4", a, ok)
	}
	if a, ok := ReadAttentionOf(proj, "4"); !ok || a.Pane != "4" {
		t.Errorf("leader 4 : %+v %v", a, ok)
	}
	proj2, dir2 := attDir(t)
	putState(t, dir2, "9", payloadPerm, 5*time.Second) // seul un teammate a écrit
	if a, ok := ReadAttentionOf(proj2, "%4"); ok {
		t.Errorf("teammate seul : attente prise en compte %+v", a)
	}
}

// Un fichier de plus de 24 h est supprimé par la lecture.
func TestReadAttentionRemovesStaleFile223(t *testing.T) {
	proj, dir := attDir(t)
	putState(t, dir, "4", payloadQuestion, 25*time.Hour)
	ReadAttention(proj)
	if _, err := os.Stat(filepath.Join(dir, "4.json")); err == nil {
		t.Error("fichier périmé non supprimé")
	}
}

// Le dossier d'état n'est jamais commité : exclusion git ajoutée une fois au démarrage ; aucune pour un projet brut tout juste créé.
func TestStartExcludesAttentionDirFromGit223(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	if out, err := exec.Command("git", "-C", s.proj, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git indisponible : %v %s", err, out)
	}
	for i := 0; i < 2; i++ {
		StartCommands(s.cfg, s.proj, "", LaunchNormal)
	}
	b, _ := os.ReadFile(filepath.Join(s.proj, ".git", "info", "exclude"))
	if n := strings.Count(string(b), "/.claude-launcher/attention/"); n != 1 {
		t.Errorf("exclude contient l'entrée %d fois, attendu 1 :\n%s", n, b)
	}
}

// Permission (état « Notification ») : retirée par la fin de n'importe quel outil.
func TestAttentionPostToolUseEndsPermission223(t *testing.T) {
	unixOnly(t)
	state := filepath.Join(t.TempDir(), "attention")
	_ = os.MkdirAll(state, 0o755)
	_ = os.WriteFile(filepath.Join(state, "7.json"), []byte(`{"hook_event_name":"Notification","message":"permission"}`), 0o644)
	runHook(t, hookCommand(t, "PostToolUse"), `{"tool_name":"Bash"}`, "CL_ATTENTION_DIR="+state, "TMUX_PANE=%7")
	if _, err := os.Stat(filepath.Join(state, "7.json")); err == nil {
		t.Error("permission non retirée par la fin de l'outil")
	}
}
