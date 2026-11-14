package core

import (
	"fmt"
	"sync"
	"time"
)

// États des backends et charge des pools pour l'interface de configuration
// (#118) : lus dans le CACHE du diagnostic (DiagnoseCached, une minute), sans
// bloquer l'affichage. Un backend jamais diagnostiqué est « en cours » et son
// diagnostic est lancé en tâche de fond ; l'interface relit un peu plus tard.

// BackendState : état d'un backend pour la liste.
type BackendState struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Pending bool   `json:"pending,omitempty"` // diagnostic en cours
}

// peekDiagnose : diagnostic en cache et encore valide.
func peekDiagnose(b BackendConf) ([]Problem, bool) {
	key := fmt.Sprintf("%+v", b)
	diagCache.Lock()
	defer diagCache.Unlock()
	e, ok := diagCache.m[key]
	if !ok || time.Since(e.at) >= time.Minute {
		return nil, false
	}
	return e.ps, true
}

// diagnoseBg : diagnostic lancé en tâche de fond (remplacé dans les tests : aucun
// accès au réseau ni aux moteurs).
var diagnoseBg = func(b BackendConf) { DiagnoseCached(b) }

var diagInflight = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

// BackendStates : état de chaque backend depuis le cache du diagnostic. Les
// absents du cache sont diagnostiqués en tâche de fond (un seul calcul à la
// fois par backend) et rendus « en cours ». Le backend host n'a rien à vérifier.
func BackendStates(bs []BackendConf) map[string]BackendState {
	out := map[string]BackendState{}
	for _, b := range bs {
		if b.Type == "host" {
			out[b.Name] = BackendState{OK: true, Text: "hôte local"}
			continue
		}
		if ps, ok := peekDiagnose(b); ok {
			if len(ps) == 0 {
				out[b.Name] = BackendState{OK: true, Text: "joignable"}
			} else {
				out[b.Name] = BackendState{Text: ps[0].What}
			}
			continue
		}
		out[b.Name] = BackendState{Pending: true, Text: "vérification…"}
		key := fmt.Sprintf("%+v", b)
		diagInflight.Lock()
		busy := diagInflight.m[key]
		diagInflight.m[key] = true
		diagInflight.Unlock()
		if !busy {
			go func(b BackendConf, key string) {
				diagnoseBg(b)
				diagInflight.Lock()
				delete(diagInflight.m, key)
				diagInflight.Unlock()
			}(b, key)
		}
	}
	return out
}

// PoolLoads : conteneurs en marche par machine de chaque pool, tous les
// backends du pool confondus (charge commune, cf. AddMates). founds : conteneurs
// connus (CtrView.Found). Chaque machine du pool figure, à 0 si elle est libre.
func PoolLoads(bs []BackendConf, founds []Found) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, b := range bs {
		if b.Pool == "" {
			continue
		}
		if out[b.Pool] == nil {
			out[b.Pool] = map[string]int{}
		}
		for _, m := range b.Machines {
			if _, ok := out[b.Pool][m]; !ok {
				out[b.Pool][m] = 0
			}
		}
	}
	for _, f := range founds {
		if !f.Running {
			continue
		}
		for _, b := range bs {
			if b.Name != f.Backend || b.Pool == "" {
				continue
			}
			if m, ok := b.Machines[f.Server]; ok {
				out[b.Pool][m]++
			}
		}
	}
	return out
}
