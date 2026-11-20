package core

import (
	"strings"
	"testing"
)

// #209 : PROJECT_HIDE_IDLE de config.conf (contracts/desktop-dispatch.md « v6.5.0 ») — mêmes formes que PROJECT_LAYOUTS.
func TestValidateProjectHideIdle209(t *testing.T) {
	for _, ok := range []string{
		"declare -A PROJECT_HIDE_IDLE=([\"alpha\"]=\"auto\" [\"beta\"]=\"none\")\n",
		"declare -A PROJECT_HIDE_IDLE=()\n",
		"# declare -A PROJECT_HIDE_IDLE=([\"alpha\"]=\"auto\")\n",
	} {
		if got := problemsMentioning(ValidateConfig(ConfConf, []byte(ok)), "PROJECT_HIDE_IDLE"); len(got) != 0 {
			t.Errorf("%q : problème inattendu %v", ok, got)
		}
	}
	for _, bad := range []string{
		"declare -A PROJECT_HIDE_IDLE=([\"alpha\"]=\"always\")\n",
		"declare -A PROJECT_HIDE_IDLE=([\"alpha\"]=\"default\")\n", // « default » = entrée absente, jamais écrite
		"declare -A PROJECT_HIDE_IDLE=([\"alpha\"]=\"\")\n",
	} {
		if len(problemsMentioning(ValidateConfig(ConfConf, []byte(bad)), "PROJECT_HIDE_IDLE")) == 0 {
			t.Errorf("%q : problème de validation attendu (auto|none)", bad)
		}
	}
}

// La clé figure en commentaire dans le modèle par défaut (defaultConfig) et ne change pas la validité d'un fichier existant.
func TestDefaultConfigMentionsProjectHideIdle209(t *testing.T) {
	if !strings.Contains(string(ConfigTemplate(ConfConf)), "PROJECT_HIDE_IDLE") {
		t.Error("PROJECT_HIDE_IDLE absent du modèle config.conf par défaut (en commentaire attendu)")
	}
}
