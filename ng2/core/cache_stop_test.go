package core

import (
	"github.com/fsnotify/fsnotify"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Arrêt du gestionnaire d'état pendant une rafale d'évènements de fichiers, appelé en même temps de plusieurs endroits
// (App.Stop et le nettoyage d'un test) : ne se fige jamais (CI Windows : interblocage à l'arrêt, Close du surveillant
// bloqué sur un envoi d'évènement sans lecteur) et reste sans course (go test -race).
func TestStateMgrStopPendantUneRafale(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	m.SetProjects([]string{proj})
	// rafale d'évènements dans le projet surveillé
	stopWrites := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stopWrites:
				return
			default:
			}
			_ = os.WriteFile(filepath.Join(proj, "f"+strconv.Itoa(i%50)+".txt"), []byte("x"), 0o644)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		var sw sync.WaitGroup
		for i := 0; i < 4; i++ { // plusieurs arrêts simultanés : sans effet au-delà du premier
			sw.Add(1)
			go func() { defer sw.Done(); m.Stop() }()
		}
		sw.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("StateMgr.Stop figé pendant une rafale d'évènements")
	}
	close(stopWrites)
	wg.Wait()
	m.Stop() // et encore après coup
}

// drainClose : un Close qui ne rend la main que si Events est lu (surveillant Windows) finit ; un Close qui ne rend jamais
// la main n'empêche pas l'arrêt au-delà du délai.
func TestDrainClose(t *testing.T) {
	// Close bloqué tant que l'évènement en vol n'est pas lu
	events := make(chan fsnotify.Event)
	errs := make(chan error)
	closeFn := func() error { events <- fsnotify.Event{Name: "x"}; return nil } // l'envoi attend un lecteur
	start := time.Now()
	if !drainClose(closeFn, events, errs, 5*time.Second, "test") || time.Since(start) > 2*time.Second {
		t.Errorf("Close bloqué sur un envoi : non fini (%v)", time.Since(start))
	}
	// Close qui ne rend jamais la main : borné
	block := make(chan struct{})
	defer close(block)
	start = time.Now()
	if drainClose(func() error { <-block; return nil }, events, errs, 150*time.Millisecond, "test") || time.Since(start) > 3*time.Second {
		t.Errorf("Close sans réponse : non borné (%v)", time.Since(start))
	}
	// vrai surveillant : fermeture immédiate, deux fois sans effet
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	if !closeFsWatcher(w, "test") || !closeFsWatcher(nil, "test") {
		t.Error("fermeture d'un vrai surveillant")
	}
}

// WatchTreeUntil : l'arrêt (done fermé) ferme le surveillant sans se figer, même pendant des évènements.
func TestWatchTreeUntilArretSousEvenements(t *testing.T) {
	root := t.TempDir()
	done := make(chan struct{})
	c := WatchTreeUntil([]string{root}, done)
	stopWrites := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; ; i++ {
			select {
			case <-stopWrites:
				return
			default:
			}
			_ = os.MkdirAll(filepath.Join(root, "g"+strconv.Itoa(i%20)), 0o755)
			_ = os.Remove(filepath.Join(root, "g"+strconv.Itoa((i+7)%20)))
		}
	}()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
	}
	close(done)
	time.Sleep(300 * time.Millisecond)
	close(stopWrites)
	<-finished
}

// Un sous-dossier créé dans un projet surveillé est surveillé à son tour (par adder, pas par la goroutine d'évènements),
// et les évènements de son contenu déclenchent un recalcul.
func TestStateMgrSurveilleLesNouveauxSousDossiers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.SetProjects([]string{proj})
	sub := filepath.Join(proj, "nouveau")
	_ = os.MkdirAll(sub, 0o755)
	watched := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.dirOwner[sub]
		return ok
	}
	for end := time.Now().Add(5 * time.Second); !watched() && time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
	}
	if !watched() {
		t.Fatal("sous-dossier créé non surveillé")
	}
}

// Rafale de sous-dossiers avec une file minuscule (npm install, git clone) : adder est retenu pour que la file déborde à
// coup sûr, le projet est reparcouru (compteur de reparcours > 0), et TOUS les dossiers finissent surveillés.
func TestStateMgrRafaleDeDossiersFileDebordee(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	oldQ := addQueueSize
	addQueueSize = 1
	defer func() { addQueueSize = oldQ }()
	defer rescanGap.Set(rescanGap.Set(50 * time.Millisecond))
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	defer open()
	defer adderHook.Set(adderHook.Set(func() { <-gate })) // adder retenu : la file (1) ne se vide pas
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	m.SetProjects([]string{proj})
	const n = 1100 // plus que la file d'origine (1024)
	dirs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		d := filepath.Join(proj, "g"+strconv.Itoa(i/100), "d"+strconv.Itoa(i))
		_ = os.MkdirAll(d, 0o755)
		dirs = append(dirs, d)
	}
	// le débordement est marqué pendant qu'adder est retenu
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		m.mu.Lock()
		marked := m.rescan[proj]
		m.mu.Unlock()
		if marked {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m.mu.Lock()
	marked := m.rescan[proj]
	m.mu.Unlock()
	if !marked {
		t.Fatal("la file n'a pas débordé : le projet n'est pas marqué à reparcourir")
	}
	open() // adder libéré : file vidée puis reparcours
	missing := func() int {
		m.mu.Lock()
		defer m.mu.Unlock()
		k := 0
		for _, d := range dirs {
			if _, ok := m.dirOwner[d]; !ok {
				k++
			}
		}
		return k
	}
	end = time.Now().Add(30 * time.Second)
	for (missing() > 0 || m.Rescans() == 0) && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	if k := missing(); k > 0 {
		t.Errorf("%d dossiers sur %d jamais surveillés après la rafale", k, n)
	}
	if m.Rescans() == 0 {
		t.Error("aucun reparcours : le chemin de débordement n'a pas été pris")
	}
}

// Débordements répétés : l'intervalle minimal entre deux reparcours d'un même projet est respecté.
func TestStateMgrReparcoursEspaces(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	defer rescanGap.Set(rescanGap.Set(700 * time.Millisecond))
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	for i := 0; i < 5; i++ { // cinq débordements à la suite
		m.mu.Lock()
		m.rescan[proj] = true
		m.mu.Unlock()
		m.rescanCh <- struct{}{}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if n := m.Rescans(); n != 1 {
		t.Errorf("%d reparcours en moins d'une seconde, attendu 1 (intervalle minimal)", n)
	}
	for end := time.Now().Add(5 * time.Second); m.Rescans() < 2 && time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
	}
	if n := m.Rescans(); n != 2 {
		t.Errorf("après l'intervalle : %d reparcours, attendu 2 (le débordement noté est repris une fois)", n)
	}
}

// L'arrêt interrompt un parcours : walkAdd lancé après Stop ne visite rien.
func TestStateMgrWalkAddInterrompuParLArret(t *testing.T) {
	t.Setenv(CacheDirVar, t.TempDir())
	proj := t.TempDir()
	for i := 0; i < 300; i++ {
		_ = os.MkdirAll(filepath.Join(proj, "d"+strconv.Itoa(i), "s"), 0o755)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	m.Stop()
	m.walkAdd(proj, proj)
	if n := m.walked.Load(); n != 0 {
		t.Errorf("%d dossiers visités après l'arrêt, attendu 0", n)
	}
}

// Un Add du surveillant qui ne répond jamais (fsnotify sous Windows quand Close passe avant la goroutine de lecture :
// dump CI, adder figé sur backend_windows.go « <-in.reply ») ne fige pas Stop.
func TestStateMgrStopAvecUnAddQuiNeRepondJamais(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	defer fsCallGrace.Set(fsCallGrace.Set(100 * time.Millisecond))
	block := make(chan struct{})
	defer close(block)
	defer fsAdd.Set(fsAdd.Set(func(w *fsnotify.Watcher, dir string) error {
		if filepath.Base(dir) == "nouveau" { // seul ce Add ne répond jamais ; les autres sont de vrais Add
			<-block
			return nil
		}
		return w.Add(dir)
	}))
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	m.SetProjects([]string{proj})
	_ = os.MkdirAll(filepath.Join(proj, "nouveau"), 0o755) // évènement → adder → Add qui ne répond jamais
	time.Sleep(500 * time.Millisecond)
	done := make(chan struct{})
	go func() { m.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop figé par un Add qui ne répond jamais")
	}
}

// waitGroupTimeout : borné, et rend la main avec la pile des goroutines quand une tâche ne finit jamais.
func TestWaitGroupTimeout(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	start := time.Now()
	if waitGroupTimeout(&wg, 150*time.Millisecond, "test") || time.Since(start) > 3*time.Second {
		t.Errorf("attente non bornée (%v)", time.Since(start))
	}
	wg.Done()
	if !waitGroupTimeout(&wg, time.Second, "test") {
		t.Error("groupe fini : attendu vrai")
	}
}
