package tmux

import (
	"strings"
	"testing"
)

// #223 coque : la vignette porte « ❓ en attente » et la question quand @cl_attn est posée ; rien sinon.
func TestMirrorFrameAttention223(t *testing.T) {
	frame := []string{"ligne 1", "ligne 2"}
	plain := mirrorFrameAttn("proj", "17", "", 10, 60, false, 0, 1, frame)
	if strings.Contains(plain, "en attente") || plain != mirrorFrame("proj", "17", 10, 60, false, 0, 1, frame) {
		t.Errorf("sans attente : rendu inchangé attendu")
	}
	w := mirrorFrameAttn("proj", "17", "question|Valider le plan ?", 10, 60, false, 0, 1, frame)
	for _, want := range []string{"❓ en attente", "❓ question : Valider le plan ?", "ligne 2"} {
		if !strings.Contains(w, want) {
			t.Errorf("%q manquant dans le rendu en attente", want)
		}
	}
}

func TestParseAttnOption223(t *testing.T) {
	if _, _, ok := parseAttnOption("  "); ok {
		t.Error("option vide : pas d'attente")
	}
	k, txt, ok := parseAttnOption("permission|Bash | rm")
	if !ok || k != "permission" || txt != "Bash | rm" {
		t.Errorf("parse = %q %q %v", k, txt, ok)
	}
}
