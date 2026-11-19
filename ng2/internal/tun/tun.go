// Package tun : variables de réglage globales (délais, crochets de test) lues par des goroutines de serveur, de flux ou de
// veille pendant que des tests les remplacent. Un accès brut est une course de données sous -race ; ici chaque lecture
// et chaque écriture passent par le verrou.
package tun

import "sync"

// V : une valeur partagée, lisible et remplaçable sans course.
type V[T any] struct {
	mu sync.RWMutex
	v  T
}

// Of : valeur initiale.
func Of[T any](v T) *V[T] { return &V[T]{v: v} }

// Get : la valeur courante.
func (t *V[T]) Get() T {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.v
}

// Set : remplace la valeur ; rend l'ancienne (restauration : defer x.Set(x.Set(nouvelle))).
func (t *V[T]) Set(v T) (old T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	old, t.v = t.v, v
	return old
}
