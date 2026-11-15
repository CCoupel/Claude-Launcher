package core

import "encoding/json"

// jsonq : chaîne JSON valide (guillemets compris) — un chemin Windows (C:\Users\…) concaténé
// tel quel dans du JSON contient des séquences d'échappement invalides.
func jsonq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
