package core

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Réglage du modèle et de l'effort d'un teammate depuis la carte TEAM (#250). Les valeurs vont dans le frontmatter
// du compagnon .claude/agents/<agent>.md (jamais dans le .template.md) ; règle du template : MODEL_EFFORT.md §7.
// Contrat : contracts/desktop-dispatch.md « v6.9.0 » (teamAgentSet).

// AgentOverrideResult : résultat de SetAgentOverride (JSON de teamAgentSet).
type AgentOverrideResult struct {
	OK             bool             `json:"ok"`
	Model          string           `json:"model,omitempty"`
	Effort         string           `json:"effort,omitempty"`
	ModelSrc       string           `json:"modelSrc,omitempty"`
	EffortSrc      string           `json:"effortSrc,omitempty"`
	File           string           `json:"file,omitempty"`
	Removed        bool             `json:"removed"`
	EffortAdjusted bool             `json:"effortAdjusted"`
	NextSpawn      bool             `json:"nextSpawn"`
	Note           string           `json:"note,omitempty"`
	Settings       *SettingsWritten `json:"settings,omitempty"`
	Msg            string           `json:"msg,omitempty"`
	Timeout        bool             `json:"timeout,omitempty"`
	Partial        string           `json:"partial,omitempty"`
}

// SettingsWritten : valeurs écrites dans settings.json (teamleader).
type SettingsWritten struct {
	File        string `json:"file"`
	Model       string `json:"model"`
	EffortLevel string `json:"effortLevel"`
}

var agentNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// DefaultValue : valeur de requête = entrée « Défaut (template) » (clé retirée).
const DefaultValue = "default"

// teamleaderAgent : entrée du registre dont la session suit settings.json (P4).
const teamleaderAgent = "teamleader"

// SetAgentOverride : fixe le modèle et/ou l'effort de l'agent (nom d'entrée du registre ou d'instance générique) de
// proj. model / effort nil : inchangé ; « default » : clé retirée ; une valeur égale à la référence retire aussi la
// clé. Les écritures sont atomiques, sous verrou fichier, bornées par SHARE_WRITE_TIMEOUT. Refus : erreur ; le
// résultat porte alors Msg, et Timeout / Partial le cas échéant.
func SetAgentOverride(proj, agent string, model, effort *string) (AgentOverrideResult, error) {
	var res AgentOverrideResult
	fail := func(format string, a ...any) (AgentOverrideResult, error) {
		res.OK = false
		res.Msg = fmt.Sprintf(format, a...)
		return res, errors.New(res.Msg)
	}
	if !agentNameRe.MatchString(agent) || strings.Contains(agent, "..") {
		return fail("agent invalide : %q", agent)
	}
	if model != nil && *model != DefaultValue && !slices.Contains(Models, *model) {
		return fail("modèle invalide : %q", *model)
	}
	if effort != nil && *effort != DefaultValue && !slices.Contains(Efforts, *effort) {
		return fail("effort invalide : %q", *effort)
	}
	if model == nil && effort == nil {
		return fail("rien à changer")
	}
	var tm *Teammate
	for _, t := range ReadTeammates(proj) {
		if t.Name == agent {
			t := t
			tm = &t
			break
		}
	}
	if tm == nil {
		return fail("agent hors registre : %s", agent)
	}
	if !tm.Editable {
		return fail("%s : ni model ni effort dans son template, réglage impossible", agent)
	}
	agentsDir := filepath.Join(proj, ".claude", "agents")
	comp := filepath.Join(agentsDir, tm.CompFile)
	if strings.HasSuffix(tm.CompFile, ".template.md") || filepath.Dir(filepath.Clean(comp)) != filepath.Clean(agentsDir) {
		return fail("fichier refusé : %s", tm.CompFile)
	}
	if err := checkNoLinks(proj, comp); err != nil {
		return fail("%v", err)
	}

	// premier calcul (valeurs lues hors verrou) : refus avant toute écriture ; recalculé sous verrou
	pl, err := planOverride(*tm, tm.CompModel, tm.CompEffort, model, effort)
	if err != nil {
		return fail("%v", err)
	}
	res.File = tm.CompFile

	isLeader := agent == teamleaderAgent
	var settingsPath string
	var settingsOld []byte
	var settingsMode os.FileMode = 0o644
	settingsKV := func(p overridePlan) ([]string, map[string]string) {
		vals := map[string]string{"model": p.resM}
		keys := []string{"model"}
		if p.resE != "" {
			keys, vals["effortLevel"] = append(keys, "effortLevel"), p.resE
		}
		return keys, vals
	}
	if isLeader {
		settingsPath = filepath.Join(proj, ".claude", "settings.json")
		if err := checkNoLinks(proj, settingsPath); err != nil {
			return fail("%v", err)
		}
		if st, err := os.Lstat(settingsPath); err == nil {
			settingsMode = st.Mode().Perm()
			if settingsOld, err = os.ReadFile(settingsPath); err != nil {
				return fail("settings.json illisible : %v", err)
			}
		}
		keys, vals := settingsKV(pl)
		if _, err := settingsSet(settingsOld, keys, vals); err != nil {
			return fail("settings.json illisible : %v", err)
		}
	}
	// contenu du compagnon calculé d'abord : un compagnon illisible refuse tout avant écriture
	if _, _, err := rewriteCompanion(comp, pl.ptrM(model), pl.ptrE(effort)); err != nil {
		return fail("%v", err)
	}

	defer ResetTplInfo(proj) // carte relue : le compagnon (« + ») a pu apparaître ou disparaître
	timeout := ShareWriteTimeoutNow()
	var final overridePlan
	werr := writeShared(timeout, comp, func() error {
		// sous le verrou : état courant relu, seules les clés demandées sont touchées, référence et P5 recalculés
		if err := checkNoLinks(proj, comp); err != nil {
			return err
		}
		curM, curE := frontKeys(comp)
		p, err := planOverride(*tm, curM, curE, model, effort)
		if err != nil {
			return err
		}
		final = p
		removed, _, err := applyCompanion(comp, p.ptrM(model), p.ptrE(effort))
		res.Removed = removed
		return err
	})
	if werr != nil {
		return writeFailure(res, tm.CompFile, werr)
	}
	pl = final
	resM, resE := pl.resM, pl.resE
	res.Model, res.Effort = resM, resE
	res.ModelSrc, res.EffortSrc = srcOf(pl.newM), srcOf(pl.newE)
	res.EffortAdjusted = pl.effortAdjusted
	adjusted := pl.adjusted
	keys, vals := settingsKV(pl)
	if isLeader {
		serr := writeShared(timeout, settingsPath, func() error {
			if err := checkNoLinks(proj, settingsPath); err != nil {
				return err
			}
			cur, err := os.ReadFile(settingsPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			out, err := settingsSet(cur, keys, vals)
			if err != nil {
				return err
			}
			if bytes.Equal(out, cur) {
				return nil
			}
			return WriteFileAtomic(settingsPath, out, settingsMode)
		})
		if serr != nil {
			r, err := writeFailure(res, "settings.json", serr)
			r.Partial = "companion"
			return r, err
		}
		res.Settings = &SettingsWritten{File: ".claude/settings.json", Model: resM, EffortLevel: resE}
		res.Note = adjusted + "teamleader.md et settings.json écrits — la session en cours garde son modèle (/model, /effort)"
	} else {
		res.Note = adjusted + "appliqué au prochain spawn"
	}
	res.OK = true
	return res, nil
}

func srcOf(explicit string) string {
	if explicit != "" {
		return "companion"
	}
	return "template"
}

func writeFailure(res AgentOverrideResult, file string, err error) (AgentOverrideResult, error) {
	res.OK = false
	var st *ShareTimeout
	if errors.As(err, &st) {
		res.Timeout = true
		if st.Busy {
			res.Msg = fmt.Sprintf("partage injoignable : %s non écrit, écriture précédente toujours bloquée", file)
		} else {
			res.Msg = fmt.Sprintf("partage injoignable : %s non écrit après %d s", file, int(st.After.Seconds()))
		}
	} else {
		res.Msg = fmt.Sprintf("%s non écrit : %v", file, err)
	}
	return res, errors.New(res.Msg)
}

// writeShared : fn sous le verrou fichier de target, dans l'attente bornée du partage.
func writeShared(d time.Duration, target string, fn func() error) error {
	// pas WithinShare : son créneau unique (écriture de config.conf) refuserait deux réglages simultanés ; le verrou
	// fichier les sérialise. Même borne SHARE_WRITE_TIMEOUT : au-delà, rendu tout de suite, l'écriture bloquée se
	// termine en tâche de fond.
	res := make(chan error, 1)
	go func() { res <- WithFileLock(target, fn) }()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case err := <-res:
		return err
	case <-t.C:
		return &ShareTimeout{After: d}
	}
}

// applyCompanion : relit le compagnon (sous verrou), applique les valeurs et l'écrit (ou le supprime s'il ne reste
// rien). removed : compagnon supprimé.
func applyCompanion(path string, model, effort *string) (removed, changed bool, err error) {
	out, del, err := rewriteCompanion(path, model, effort)
	if err != nil {
		return false, false, err
	}
	cur, rerr := os.ReadFile(path)
	exists := rerr == nil
	switch {
	case del && exists:
		if err := guardWrite(path); err != nil {
			return false, false, err
		}
		return true, true, removeRetry(path)
	case del, exists && bytes.Equal(out, cur):
		return false, false, nil
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return false, true, WriteFileAtomic(path, out, mode)
}

// rewriteCompanion : nouveau contenu du compagnon. del : plus rien à garder (fichier supprimé, ou jamais créé).
// model / effort : valeur explicite à écrire, "" pour retirer la clé.
func rewriteCompanion(path string, model, effort *string) (out []byte, del bool, err error) {
	src, rerr := os.ReadFile(path)
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return nil, false, rerr
	}
	out, err = rewriteFront(src, model, effort)
	if err != nil {
		return nil, false, fmt.Errorf("%s : %w", filepath.Base(path), err)
	}
	return out, len(bytes.TrimSpace(out)) == 0, nil
}

var frontKeyRe = regexp.MustCompile(`^(model|effort)[ \t]*:`)

// rewriteFront : réécrit les lignes model: / effort: du frontmatter de src ; le reste est conservé octet pour octet
// (CRLF, BOM compris). Sans frontmatter : bloc ajouté en tête si une valeur est à écrire. Bloc devenu vide : retiré.
func rewriteFront(src []byte, model, effort *string) ([]byte, error) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	var pre []byte
	if bytes.HasPrefix(src, bom) {
		pre, src = bom, src[len(bom):]
	}
	eol := "\n"
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		eol = "\r\n"
	}
	want := func() []string {
		var l []string
		if model != nil && *model != "" {
			l = append(l, "model: "+*model)
		}
		if effort != nil && *effort != "" {
			l = append(l, "effort: "+*effort)
		}
		return l
	}
	lines := splitKeepEOL(src)
	if len(lines) == 0 || strings.TrimSpace(trimEOL(lines[0])) != "---" {
		w := want()
		if len(w) == 0 {
			return append(pre, src...), nil
		}
		blk := "---" + eol + strings.Join(w, eol) + eol + "---" + eol
		return append(append(pre, blk...), src...), nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(trimEOL(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, errors.New("frontmatter non fermé, réglage refusé")
	}
	vals := map[string]*string{"model": model, "effort": effort} // nil : clé non touchée
	done := map[string]bool{}
	var body []string // lignes conservées du frontmatter
	for _, l := range lines[1:end] {
		if g := frontKeyRe.FindStringSubmatch(l); g != nil {
			k := g[1]
			if vals[k] == nil { // clé non demandée : ligne conservée telle quelle
				done[k] = true
				body = append(body, l)
				continue
			}
			if done[k] || *vals[k] == "" { // doublon, ou clé retirée
				continue
			}
			done[k] = true
			body = append(body, k+": "+*vals[k]+eolOf(l, eol))
			continue
		}
		body = append(body, l)
	}
	for _, k := range []string{"model", "effort"} {
		if vals[k] != nil && *vals[k] != "" && !done[k] {
			line := k + ": " + *vals[k] + eol
			at := len(body)
			if k == "model" { // model avant effort, même ajouté après coup
				for n, l := range body {
					if frontKeyRe.MatchString(l) && strings.HasPrefix(l, "effort") {
						at = n
						break
					}
				}
			}
			body = append(body[:at], append([]string{line}, body[at:]...)...)
		}
	}
	rest := lines[end+1:]
	empty := true
	for _, l := range body {
		if strings.TrimSpace(l) != "" {
			empty = false
		}
	}
	var b bytes.Buffer
	b.Write(pre)
	if !empty {
		b.WriteString(lines[0])
		for _, l := range body {
			b.WriteString(l)
		}
		b.WriteString(lines[end])
	}
	for _, l := range rest {
		b.WriteString(l)
	}
	return b.Bytes(), nil
}

func splitKeepEOL(b []byte) []string {
	var out []string
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			out = append(out, string(b))
			break
		}
		out = append(out, string(b[:i+1]))
		b = b[i+1:]
	}
	return out
}

func trimEOL(l string) string { return strings.TrimRight(l, "\r\n") }

// eolOf : fin de ligne de l (celle de la ligne remplacée), def si elle n'en a pas.
func eolOf(l, def string) string {
	switch {
	case strings.HasSuffix(l, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(l, "\n"):
		return "\n"
	}
	return def
}

// overridePlan : résultat du calcul d'un réglage sur l'état courant du compagnon.
type overridePlan struct {
	newM, newE     string // valeurs explicites du compagnon après réglage ("" : clé absente)
	resM, resE     string // valeurs résolues
	adjusted       string // préfixe de note quand l'effort a été ajusté
	effortAdjusted bool
	touchE         bool // l'effort est réécrit (demandé, ou ajusté à un changement de modèle)
}

func (p overridePlan) ptrM(req *string) *string {
	if req == nil {
		return nil
	}
	return &p.newM
}

func (p overridePlan) ptrE(req *string) *string {
	if req == nil && !p.touchE {
		return nil
	}
	return &p.newE
}

// planOverride : valeurs du compagnon (curM, curE : état courant) après la demande ; référence du template et
// compatibilité modèle/effort appliquées. Erreur : refus.
func planOverride(tm Teammate, curM, curE string, model, effort *string) (overridePlan, error) {
	p := overridePlan{newM: curM, newE: curE}
	if model != nil {
		p.newM = ""
		if *model != DefaultValue {
			p.newM = *model
		}
		if p.newM == tm.TplModel {
			p.newM = ""
		}
	}
	if effort != nil {
		p.newE = ""
		if *effort != DefaultValue {
			p.newE = *effort
		}
		if p.newE == tm.TplEffort {
			p.newE = ""
		}
	}
	p.resM = cmp.Or(p.newM, tm.TplModel)
	p.resE = cmp.Or(p.newE, tm.TplEffort)
	if effort != nil && *effort != DefaultValue && !EffortAccepted(p.resM, *effort) {
		return p, fmt.Errorf("effort %s non pris en charge par %s", *effort, p.resM)
	}
	if effort == nil && model != nil && p.resE != "" && !EffortAccepted(p.resM, p.resE) {
		acc, _ := ModelEfforts(p.resM)
		switch {
		case slices.Contains(acc, tm.TplEffort), len(acc) == 0:
			p.newE = ""
		default:
			p.newE = highestEffort(acc)
		}
		p.touchE = true
		old := p.resE
		p.resE = cmp.Or(p.newE, tm.TplEffort)
		if len(acc) == 0 {
			p.resE = ""
		}
		p.effortAdjusted = p.resE != old
		if p.effortAdjusted {
			p.adjusted = fmt.Sprintf("effort ajusté à %s (%s ne prend pas %s) — ", cmp.Or(p.resE, "aucun"), p.resM, old)
		}
	}
	return p, nil
}

// checkNoLinks : ni .claude, ni .claude/agents, ni le fichier ne sont des liens symboliques (défense en profondeur,
// contrôlé aussi sous le verrou).
func checkNoLinks(proj, file string) error {
	for _, p := range []string{filepath.Join(proj, ".claude"), filepath.Join(proj, ".claude", "agents"), file} {
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s est un lien symbolique : réglage refusé", filepath.Base(p))
		}
	}
	return nil
}
