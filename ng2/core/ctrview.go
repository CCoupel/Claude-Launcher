package core

import "sync"

// ── Conteneurs vus par le navigateur ──────────────────────────────────────

// CtrView : ce que le navigateur sait des backends, relu toutes les 10 s.
type CtrView struct {
	Env    map[string]bool      // projet -> a un env.yaml ou un env-template.yaml
	Assign map[string]string    // projet -> backend (sans entrée : host, ou « prefer »)
	Found  map[string]Found     // projet -> son conteneur, s'il existe
	Errs   map[string]string    // backend -> erreur de listing (moteur absent…)
	Diag   map[string][]Problem // backend affecté à un projet -> ce qui l'empêche de marcher
	Bad    map[string]string    // projet -> configuration invalide (launchBackend)
	Live   map[string]LiveSess  // projet -> session tmux vivante dans son conteneur
	Dups   map[string][]string  // projet -> serveurs où il a un conteneur (groupe, en double)
	Down   map[string]string    // backend (groupe) -> serveurs injoignables
}

// ReadCtrView : état des conteneurs des projets paths, sur tous les backends.
func ReadCtrView(paths []string) CtrView {
	v := CtrView{Env: map[string]bool{}, Assign: map[string]string{}, Found: map[string]Found{}, Errs: map[string]string{}, Diag: map[string][]Problem{}, Bad: map[string]string{}, Live: map[string]LiveSess{},
		Dups: map[string][]string{}, Down: map[string]string{}}
	any := false
	asg := ReadAssignments()
	for _, p := range paths {
		if HasEnv(p) {
			v.Env[p], any = true, true
			v.Assign[p] = ProjectBackend(p).Name
		}
		if name, ok := asg[p]; ok && name != "host" {
			v.Assign[p] = name
			if _, err := LaunchBackend(p); err != nil {
				v.Bad[p] = err.Error()
			}
		}
	}
	bs, _ := LoadBackends()
	for _, b := range bs {
		e, ok := EngineOf(b)
		if !ok || !any && !usedBackend(b.Name) {
			continue
		}
		if assignedTo(v.Assign, b.Name) {
			if ps := DiagnoseCached(b); len(ps) > 0 {
				v.Diag[b.Name] = ps
				continue
			}
		}
		var fs []Found
		var down map[string]error
		if b.K8s() {
			var err error
			if fs, err = e.K8sList(); err != nil {
				down = map[string]error{"": err}
			}
		} else {
			fs, down = e.ListPool() // groupe : tous ses serveurs en parallèle (#61)
		}
		if len(fs) == 0 && len(down) > 0 && len(down) == len(b.Servers()) {
			for _, err := range down {
				v.Errs[b.Name] = err.Error()
			}
			continue
		}
		if len(down) > 0 {
			v.Down[b.Name] = DownText(down)
		}
		for _, f := range fs {
			if o, dup := v.Found[f.Project]; dup && o.Backend == f.Backend && o.Server != f.Server {
				v.Dups[f.Project] = append(v.Dups[f.Project], f.Server)
				if len(v.Dups[f.Project]) == 1 {
					v.Dups[f.Project] = []string{o.Server, f.Server}
				}
			}
			v.Found[f.Project] = f
		}
		// sessions des conteneurs en marche, en parallèle (un exec chacun ; k8s ~0,5 s)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, f := range fs {
			if !f.Running {
				continue
			}
			wg.Add(1)
			go func(f Found) {
				defer wg.Done()
				if l, ok := e.Of(f).ReadLive(f.Name); ok {
					mu.Lock()
					v.Live[f.Project] = l
					mu.Unlock()
				}
			}(f)
		}
		wg.Wait()
	}
	return v
}

func assignedTo(assign map[string]string, name string) bool {
	for _, b := range assign {
		if b == name {
			return true
		}
	}
	return false
}

func usedBackend(name string) bool {
	for _, b := range ReadAssignments() {
		if b == name {
			return true
		}
	}
	return false
}
