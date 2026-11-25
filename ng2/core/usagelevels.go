package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// usage-levels.json (#237, contracts/usage-levels.md) : niveaux d'épuisement des
// quotas, calculés ici (seule source) et lus par le panneau milestone du template.

const usageLevelsSchema = 1

// usageLevelsPath : voisin de usage.json, dans le cache partagé.
func usageLevelsPath() string { return filepath.Join(sharedUsageDir(), "usage-levels.json") }

// levelTime : RFC3339 UTC à la milliseconde (…T13:51:32.226Z).
func levelTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

type usageLevelsDoc struct {
	Schema      int           `json:"schema"`
	ComputedAt  string        `json:"computed_at"`
	SourceMtime string        `json:"source_mtime"`
	Windows     []levelWindow `json:"windows"`
}

type levelWindow struct {
	Kind       string          `json:"kind"`
	Scope      json.RawMessage `json:"scope"`
	ResetsAt   *string         `json:"resets_at"`
	Percent    json.Number     `json:"percent"`
	ExhaustsAt *string         `json:"exhausts_at"`
	Approx     bool            `json:"approx"`
	Level      *string         `json:"level"`
	Alert      *string         `json:"alert"`
}

// SyncUsageLevels : (re)calcule usage-levels.json si son source_mtime diffère de
// celui de usage.json (stat seul sinon : aucune écriture périodique sur le
// stockage partagé). Rien n'est écrit si usage.json est absent ou inexploitable.
func SyncUsageLevels(now time.Time) {
	st, err := os.Stat(usageCachePath())
	if err != nil {
		return
	}
	src := levelTime(st.ModTime())
	if b, err := os.ReadFile(usageLevelsPath()); err == nil {
		var cur struct {
			Schema      int    `json:"schema"`
			SourceMtime string `json:"source_mtime"`
		}
		if json.Unmarshal(b, &cur) == nil && cur.Schema == usageLevelsSchema && cur.SourceMtime == src {
			return
		}
	}
	b, err := os.ReadFile(usageCachePath())
	if err != nil {
		return
	}
	var doc struct {
		Limits []struct {
			Kind     string          `json:"kind"`
			Scope    json.RawMessage `json:"scope"`
			ResetsAt *string         `json:"resets_at"`
			Percent  json.Number     `json:"percent"`
		} `json:"limits"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&doc) != nil || len(doc.Limits) == 0 {
		return
	}
	out := usageLevelsDoc{Schema: usageLevelsSchema, ComputedAt: levelTime(now), SourceMtime: src, Windows: []levelWindow{}}
	for _, l := range doc.Limits {
		if l.Kind != "session" && l.Kind != "weekly_all" && l.Kind != "weekly_scoped" {
			continue
		}
		w := levelWindow{Kind: l.Kind, Scope: l.Scope, ResetsAt: l.ResetsAt, Percent: l.Percent}
		if len(bytes.TrimSpace(w.Scope)) == 0 {
			w.Scope = json.RawMessage("null")
		}
		if w.Percent == "" {
			w.Percent = "0"
		}
		pct, _ := w.Percent.Float64()
		reset := ""
		if l.ResetsAt != nil {
			reset = *l.ResetsAt
		}
		if e := ProjectExhaustion(l.Kind, pct, reset, now); e.Level != "" || e.Approx {
			at := levelTime(e.Exhausts)
			w.ExhaustsAt, w.Approx = &at, e.Approx
			if e.Level != "" {
				lv := e.Level
				w.Level = &lv
			}
			if a := e.Alert(now); a != "" {
				w.Alert = &a
			}
		}
		out.Windows = append(out.Windows, w)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = WriteFileAtomic(usageLevelsPath(), append(data, '\n'), 0o644)
}
