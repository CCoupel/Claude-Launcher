package core

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// progressBar : barre affichée sur le terminal (avant l'attache tmux) pour une
// étape de démarrage, seulement si elle dure plus de progressDelay — sur un
// stockage réseau, une étape d'ordinaire instantanée peut prendre des secondes.
type progressBar struct {
	label       string
	start       time.Time
	mu          sync.Mutex
	done, total int
	shown       bool
	stop        chan struct{}
	wg          sync.WaitGroup
}

const progressDelay = 500 * time.Millisecond

func StartProgress(label string) *progressBar {
	p := &progressBar{label: label, start: time.Now(), stop: make(chan struct{})}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				if time.Since(p.start) >= progressDelay {
					p.draw()
				}
			}
		}
	}()
	return p
}

// Step : avancement (done sur total).
func (p *progressBar) Step(done, total int) {
	p.mu.Lock()
	p.done, p.total = done, total
	p.mu.Unlock()
}

func (p *progressBar) draw() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shown = true
	fmt.Fprint(os.Stderr, "\r\033[K"+progressLine(p.label, p.done, p.total, time.Since(p.start)))
}

// Done : arrête la barre et efface sa ligne.
func (p *progressBar) Done() {
	close(p.stop)
	p.wg.Wait()
	if p.shown {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

func progressLine(label string, done, total int, el time.Duration) string {
	const width = 24
	fill := 0
	if total > 0 {
		fill = min(width, done*width/total)
	}
	return fmt.Sprintf("  %s  %s%s  %d/%d  %ds", label,
		strings.Repeat("█", fill), strings.Repeat("░", width-fill), done, total, int(el.Seconds()))
}
