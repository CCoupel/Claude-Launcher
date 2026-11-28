package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Démarrage ROOT du conteneur d'un projet d'utilisateur, traitement standard SANS root (LOT-3.2, D3 révisée P28, P30 ; contrat §5 bis) :
// le bootstrap (pid1, root) installe les paquets, donne UNE FOIS le stockage à l'uid du traitement (marqueur /space/.claude-launcher/owner),
// contrôle l'inscriptibilité sous cet uid, puis passe DÉFINITIVEMENT à l'uid du traitement par setpriv. Chaque exec du launcher se fait sous
// cet uid, jamais en root.

// SetprivPrefix : bascule définitive vers uid:gid — sans groupes hérités, sans nouveaux privilèges, sans capacité héritable. Pas de « --bounding-set » :
// le réduire exige CAP_SETPCAP, hors des cinq capacités accordées ; le conteneur démarre déjà avec « drop ALL » + ces cinq (son bounding set en est donc
// limité), le passage de l'uid 0 à l'uid du traitement vide les capacités permises et effectives, et no_new_privs interdit tout regain (setuid, capacités de fichier).
func SetprivPrefix(uid, gid int64) string {
	return fmt.Sprintf("setpriv --reuid=%d --regid=%d --clear-groups --no-new-privs --inh-caps=-all", uid, gid)
}

// userEnvPairs : environnement fixé par le launcher pour le traitement.
var userEnvPairs = []string{"HOME=/space/.claude/home", "CLAUDE_CONFIG_DIR=/space/.claude", "XDG_CONFIG_HOME=/space/.claude/home/.config"}

// UserBootstrapScript : script « sh -c » du conteneur (pid1). uid et gid sont des entiers non nuls (sinon le script refuse de démarrer) ; aucune
// valeur de l'utilisateur n'y figure.
func UserBootstrapScript(uid, gid int) string {
	if !validID(int64(uid)) || !validID(int64(gid)) {
		return `echo "identité du traitement invalide (jamais root)" >&2; exit 1`
	}
	u, g := int64(uid), int64(gid)
	refusal := fmt.Sprintf("Le stockage n'est pas accessible en écriture à l'utilisateur %d:%d et le launcher n'a pas pu en changer le propriétaire. "+
		"Exécute sur ton stockage : chown -R %d:%d <dossier> (procédure dans la documentation).", u, g, u, g)
	return fmt.Sprintf(`D=/space; M=/space/.claude-launcher/owner; R=/space/.claude-launcher/ready; U=%[1]d; G=%[2]d
[ -L "$D/.claude-launcher" ] && rm -f "$D/.claude-launcher"; [ -L "$M" ] && rm -f "$M"
rm -f "$R" 2>/dev/null
need=""
for c in tmux git curl; do command -v $c >/dev/null 2>&1 || need="$need $c"; done
if [ -n "$need" ]; then
  if command -v apt-get >/dev/null 2>&1; then DEBIAN_FRONTEND=noninteractive apt-get update -qq >/dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $need >/dev/null 2>&1
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache $need >/dev/null 2>&1
  elif command -v dnf >/dev/null 2>&1; then dnf install -y -q $need >/dev/null 2>&1
  fi
fi
cur=$(cat "$M" 2>/dev/null); own=$(stat -c %%u:%%g "$D" 2>/dev/null)
if [ "$cur" != "$U:$G" ] || [ "$own" != "$U:$G" ]; then
  if chown -R -h %[1]d:%[2]d /space 2>/dev/null; then mkdir -p $D/.claude-launcher && rm -f "$M" && (set -C; printf '%%s' "$U:$G" > "$M") && chown -h %[1]d:%[2]d $D/.claude-launcher "$M"; fi
fi
if ! E=$(%[3]s true 2>&1); then echo "échec de l'abandon des privilèges : $(printf %%s "$E" | head -c 300)" >&2; exit 1; fi
%[3]s sh -c 'test -w /space && mkdir -p /space/.claude/home/.config' || { echo "%[4]s" >&2; exit 1; }
%[3]s sh -c ': > /space/.claude-launcher/ready' 2>/dev/null || { mkdir -p /space/.claude-launcher && : > "$R"; }
P='trap "exit 0" TERM INT; while :; do sleep 3600 & wait $!; done'
exec %[3]s env %[5]s sh -c "$P"
`, u, g, SetprivPrefix(u, g), refusal, strings.Join(userEnvPairs, " "))
}

// StorageChownNeeded : le chown du stockage est-il nécessaire ? Marqueur « <uid>:<gid> » ET propriétaire de /space égaux à l'identité du traitement :
// non ; tout autre cas (premier démarrage, marqueur illisible, propriétaire changé de l'extérieur, uid du projet changé) : oui.
func StorageChownNeeded(marker string, ownerUID, ownerGID, uid, gid int) bool {
	want := fmt.Sprintf("%d:%d", uid, gid)
	return strings.TrimSpace(marker) != want || ownerUID != uid || ownerGID != gid
}

var safeDirRE = regexp.MustCompile(`^[A-Za-z0-9/_.,:@+-]+$`)

// StorageRefusal : refus nommé quand le stockage n'est pas inscriptible à l'uid du traitement et que le chown a échoué. Le dossier n'est jamais
// recopié tel quel dans la commande proposée s'il comporte autre chose que des caractères sûrs.
func StorageRefusal(uid, gid int, dir string) string {
	if !safeDirRE.MatchString(dir) || strings.Contains(dir, "..") {
		dir = "<dossier>"
	}
	return fmt.Sprintf("Le stockage n'est pas accessible en écriture à l'utilisateur %d:%d et le launcher n'a pas pu en changer le propriétaire. "+
		"Exécute sur ton stockage : chown -R %d:%d %s (procédure dans la documentation).", uid, gid, uid, gid, dir)
}

// ExplainApplyError : traduit un refus d'admission de kubectl en message nommé — PodSecurity « restricted » (le bootstrap root exige baseline).
func ExplainApplyError(stderr string) error {
	if strings.Contains(stderr, `violates PodSecurity "restricted`) {
		return errors.New("le namespace impose le profil restricted : le démarrage d'un projet exige baseline")
	}
	return nil
}
