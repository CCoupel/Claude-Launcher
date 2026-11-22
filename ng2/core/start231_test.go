package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #231 (LOT-2 v6.7.0) — proposition de /start-session à l'ouverture. Contrat : contracts/desktop-dispatch.md § « v6.7.0 »
// (Proposition de /start-session) : lancement neuf (LaunchNormal) d'un projet géré (.claude/commands/start-session.md)
// dont la team n'existe pas (<CLAUDE_CONFIG_DIR>/teams/<team_name>/config.json) ; /init-project toujours proposé d'abord ;
// défaut O ; refus non reposé ; aucune question pour LaunchNew*, projet non géré, team présente ou reprise.
// Le script généré par StartCommands est EXÉCUTÉ (bash) avec un faux claude qui consigne ses arguments : projet factice,
// CLAUDE_CONFIG_DIR et caches temporaires, jamais de vrai projet ni de vraie team.

const startQuestion231 = "session non démarrée — lancer /start-session ? [O/n] "

type start231 struct {
	proj, claudeDir, argsFile string
	cfg                       Config
}

// newStart231 : projet factice (dépôt temporaire) + environnement isolé + faux claude ; managed pose start-session.md.
func newStart231(t *testing.T, managed bool) *start231 {
	t.Helper()
	unixOnly(t)
	root := t.TempDir()
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CL_CONFIG_DIR", "CLAUDE_CONFIG_DIR"} {
		d := filepath.Join(root, strings.ToLower(k))
		_ = os.MkdirAll(d, 0o755)
		t.Setenv(k, d)
	}
	winHome(t, filepath.Join(root, "home"))
	s := &start231{proj: filepath.Join(root, "PROJETS", "alpha"), claudeDir: filepath.Join(root, "claude_config_dir"), argsFile: filepath.Join(root, "args.txt"),
		cfg: Config{ResumeMode: "ask", ResumeMaxAgeH: 24}}
	if err := os.MkdirAll(filepath.Join(s.proj, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if managed {
		s.write(t, ".claude/commands/start-session.md", "# start-session\n")
	}
	fake := filepath.Join(root, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + Shq(s.argsFile) + "\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_CLAUDE_BIN", fake)
	t.Setenv("CL_NO_UPDATE", "1")
	return s
}

func (s *start231) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(s.proj, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// template : état du template du projet par rapport à la version adoptée par le poste.
func (s *start231) template(t *testing.T, state TemplateState) {
	t.Helper()
	switch state {
	case TplOK, TplOutdated:
		s.write(t, ".claude/.template-source.json", `{"tag":"v1.0.0","commit":"c1"}`)
		adopted := "v1.0.0"
		if state == TplOutdated {
			adopted = "v2.0.0"
		}
		_ = os.MkdirAll(filepath.Dir(TplVersionPath()), 0o755)
		if err := os.WriteFile(TplVersionPath(), []byte(adopted+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := ReadTemplate(s.proj, ""); got != state {
		t.Fatalf("précondition : état du template = %v, attendu %v", got, state)
	}
}

// team : <CLAUDE_CONFIG_DIR>/teams/<name>/config.json ; cwd = chemin vu par claude du premier membre.
func (s *start231) team(t *testing.T, name, cwd string) {
	t.Helper()
	d := filepath.Join(s.claudeDir, "teams", name)
	_ = os.MkdirAll(d, 0o755)
	body := fmt.Sprintf(`{"members":[{"name":"team-lead","agentType":"team-lead","cwd":%q}]}`, cwd)
	if err := os.WriteFile(filepath.Join(d, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *start231) teamName(t *testing.T, name string) {
	t.Helper()
	s.write(t, ".claude/project-config.json", fmt.Sprintf(`{"team_name":%q}`, name))
}

// run : génère le script (StartCommands) puis l'exécute avec stdin ; rend le script et les arguments reçus par claude
// (une ligne par lancement, vide si claude n'a pas été lancé).
func (s *start231) run(t *testing.T, mode LaunchMode, stdin string) (start string, claudeArgs []string) {
	t.Helper()
	start, _ = StartCommands(s.cfg, s.proj, "", mode)
	cmd := exec.Command("bash", "-c", start)
	cmd.Dir = s.proj
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script de démarrage : %v\n%s\n--- script ---\n%s", err, out, start)
	}
	b, _ := os.ReadFile(s.argsFile)
	if len(b) > 0 { // une ligne par lancement, vide quand claude part sans argument
		for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			claudeArgs = append(claudeArgs, strings.TrimSpace(l))
		}
	}
	return start, claudeArgs
}

func wantArgs(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s : claude lancé avec %q, attendu %q", what, got, want)
	}
}

// ── Projet géré, sans team : la question est posée, défaut O ─────────────────────────────

func TestStartSessionProposedManagedNoTeam231(t *testing.T) {
	for _, tc := range []struct{ name, stdin, want string }{
		{"Entrée = O (défaut)", "\n", "/start-session"},
		{"o", "o\n", "/start-session"},
		{"O", "O\n", "/start-session"},
		{"y", "y\n", "/start-session"},
		{"n : refus", "n\n", ""},
		{"N : refus", "N\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStart231(t, true)
			s.template(t, TplOK)
			s.teamName(t, "alpha-team")
			start, got := s.run(t, LaunchNormal, tc.stdin)
			if !strings.Contains(start, startQuestion231) {
				t.Errorf("question absente du script :\n%s", start)
			}
			wantArgs(t, tc.name, got, tc.want)
		})
	}
}

// Sans team_name lisible : projet géré, aucune team dont le premier membre est dans le projet → question posée.
func TestStartSessionProposedWithoutTeamName231(t *testing.T) {
	s := newStart231(t, true)
	s.team(t, "autre-projet", "/ailleurs/beta") // team d'un autre projet : ne compte pas
	start, got := s.run(t, LaunchNormal, "\n")
	if !strings.Contains(start, startQuestion231) {
		t.Errorf("question absente :\n%s", start)
	}
	wantArgs(t, "team d'un autre projet", got, "/start-session")
}

// ── Aucune question ───────────────────────────────────────────────────────────────────

// Team présente (team_name) = reprise : aucune proposition, claude part sans argument.
func TestStartSessionNotProposedTeamExistsByName231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	s.teamName(t, "alpha-team")
	s.team(t, "alpha-team", ClaudePath(s.proj))
	start, got := s.run(t, LaunchNormal, "\n")
	if strings.Contains(start, "/start-session") {
		t.Errorf("question posée alors que la team existe :\n%s", start)
	}
	wantArgs(t, "team présente", got, "")
}

// team_name absent : repli sur la team dont le premier membre a le projet pour cwd.
func TestStartSessionNotProposedTeamExistsByCwd231(t *testing.T) {
	s := newStart231(t, true)
	s.team(t, "nom-quelconque", ClaudePath(s.proj))
	start, got := s.run(t, LaunchNormal, "\n")
	if strings.Contains(start, "/start-session") {
		t.Errorf("question posée alors que la team du projet existe (repli cwd) :\n%s", start)
	}
	wantArgs(t, "team par cwd", got, "")
}

// team_name illisible : même repli.
func TestStartSessionNotProposedBrokenTeamName231(t *testing.T) {
	s := newStart231(t, true)
	s.write(t, ".claude/project-config.json", "{pas du json")
	s.team(t, "t", ClaudePath(s.proj))
	start, _ := s.run(t, LaunchNormal, "\n")
	if strings.Contains(start, "/start-session") {
		t.Errorf("question posée malgré la team du projet (project-config.json illisible) :\n%s", start)
	}
}

// Projet non géré (pas de start-session.md) : jamais de proposition.
func TestStartSessionNotProposedUnmanaged231(t *testing.T) {
	s := newStart231(t, false)
	start, got := s.run(t, LaunchNormal, "\n")
	if strings.Contains(start, "/start-session") {
		t.Errorf("question posée pour un projet non géré :\n%s", start)
	}
	wantArgs(t, "non géré", got, "")
}

// Création depuis le launcher (LaunchNew*) : jamais de question /start-session.
func TestStartSessionNotProposedLaunchNew231(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode LaunchMode
	}{{"LaunchNewManaged", LaunchNewManaged}, {"LaunchNewRaw", LaunchNewRaw}, {"LaunchNewProposed", LaunchNewProposed}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStart231(t, true)
			start, _ := StartCommands(s.cfg, s.proj, "", tc.mode)
			if strings.Contains(start, "/start-session") {
				t.Errorf("%s : question /start-session posée :\n%s", tc.name, start)
			}
		})
	}
}

// Reprise de conversation choisie : ni /init-project ni /start-session ; refusée : la question suit.
func TestStartSessionResumeChosen231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	root := fakeClaude(t, s.proj, map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	s.claudeDir = root
	start, got := s.run(t, LaunchNormal, "\n") // « la reprendre ? [O/n] » → O
	if len(got) != 1 || !strings.Contains(got[0], "--resume") || strings.Contains(got[0], "/start-session") || strings.Contains(got[0], "/init-project") {
		t.Errorf("reprise choisie : claude lancé avec %q (start = %s)", got, start)
	}
}

func TestStartSessionResumeRefusedThenProposed231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	root := fakeClaude(t, s.proj, map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	s.claudeDir = root
	_, got := s.run(t, LaunchNormal, "n\n\n") // reprise refusée, puis /start-session accepté (défaut)
	if len(got) != 1 || strings.Contains(got[0], "--resume") || !strings.HasSuffix(got[0], "/start-session") {
		t.Errorf("reprise refusée puis O : claude lancé avec %q", got)
	}
}

// ── Ordre avec /init-project ──────────────────────────────────────────────────────────

// Template en retard : /init-project d'abord. Accepté → claude part sur /init-project seul, pas de /start-session.
func TestStartSessionInitAcceptedSkipsStartSession231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOutdated)
	_, got := s.run(t, LaunchNormal, "o\n")
	wantArgs(t, "init accepté", got, "/init-project")
}

// Init refusé : /start-session proposé ensuite (O → lancé, n → rien) ; l'ordre des questions dans le script est fixe.
func TestStartSessionInitRefusedThenStartSession231(t *testing.T) {
	for _, tc := range []struct{ name, stdin, want string }{
		{"init refusé, start-session accepté", "n\nO\n", "/start-session"},
		{"init refusé (défaut N), start-session par défaut", "\n\n", "/start-session"},
		{"init refusé, start-session refusé", "n\nn\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStart231(t, true)
			s.template(t, TplOutdated)
			start, got := s.run(t, LaunchNormal, tc.stdin)
			i, j := strings.Index(start, "/init-project ? [o/N]"), strings.Index(start, startQuestion231)
			if i < 0 || j < 0 || i > j {
				t.Errorf("ordre des questions : /init-project (%d) doit précéder /start-session (%d)\n%s", i, j, start)
			}
			wantArgs(t, tc.name, got, tc.want)
		})
	}
}

// Template en retard mais team présente : seulement la question /init-project.
func TestStartSessionInitOnlyWhenTeamExists231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOutdated)
	s.team(t, "t", ClaudePath(s.proj))
	start, got := s.run(t, LaunchNormal, "n\n")
	if strings.Contains(start, "/start-session") {
		t.Errorf("/start-session proposé alors que la team existe :\n%s", start)
	}
	wantArgs(t, "init refusé, team présente", got, "")
}

// Template manquant (clone frais) : /init-project d'abord (O par défaut) ; refusé, puis /start-session.
func TestStartSessionAfterMissingTemplate231(t *testing.T) {
	s := newStart231(t, true)
	s.write(t, ".claude/project-config.json", `{"template_version":{"commit":"abc"},"team_name":"alpha-team"}`)
	if got := ReadTemplate(s.proj, ""); got != TplMissing {
		t.Fatalf("précondition : %v, attendu TplMissing", got)
	}
	_, got := s.run(t, LaunchNormal, "\n")
	wantArgs(t, "template manquant, init accepté", got, "/init-project")
	_ = os.Remove(s.argsFile)
	_, got = s.run(t, LaunchNormal, "n\nn\n")
	wantArgs(t, "template manquant, tout refusé", got, "")
}

// Refus non reposé pendant la même ouverture : la question n'existe qu'une fois dans le script, avant l'unique lancement
// de claude (aucune boucle qui la reposerait).
func TestStartSessionAskedOnlyOnce231(t *testing.T) {
	s := newStart231(t, true)
	s.template(t, TplOK)
	start, got := s.run(t, LaunchNormal, "n\nn\nn\n")
	if n := strings.Count(start, "lancer /start-session ?"); n != 1 {
		t.Errorf("question présente %d fois dans le script, attendu 1", n)
	}
	if len(got) != 1 {
		t.Errorf("claude lancé %d fois : %q", len(got), got)
	}
	wantArgs(t, "refus", got, "")
}

// ── core.TeamExists (contrat) ─────────────────────────────────────────────────────────

func TestTeamExists231(t *testing.T) {
	s := newStart231(t, true)
	if TeamExists(s.proj) {
		t.Error("aucune team : TeamExists doit être faux")
	}
	s.teamName(t, "alpha-team")
	if TeamExists(s.proj) {
		t.Error("team_name sans dossier de team : faux")
	}
	s.team(t, "alpha-team", ClaudePath(s.proj))
	if !TeamExists(s.proj) {
		t.Error("team_name avec teams/alpha-team/config.json : vrai")
	}
	// repli par cwd quand team_name est absent
	s2 := newStart231(t, true)
	s2.team(t, "x", ClaudePath(s2.proj))
	if !TeamExists(s2.proj) {
		t.Error("team dont le premier membre est dans le projet (sans team_name) : vrai")
	}
	s3 := newStart231(t, true)
	s3.team(t, "x", "/ailleurs/autre")
	if TeamExists(s3.proj) {
		t.Error("team d'un autre projet : faux")
	}
}
