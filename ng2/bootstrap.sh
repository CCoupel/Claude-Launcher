#!/bin/sh
# Bootstrap d'un conteneur vanilla de projet (Claude-Launcher-NG2).
# Envoyé par stdin (exec -i -u 0 … sh -s), précédé des variables CL_* :
#   CL_UID CL_GID CL_USER CL_HOME CL_PROJ CL_HASH CL_SKIP_CLAUDE
#   CL_PKGS (génériques) CL_PKGS_APT CL_PKGS_DNF CL_PKGS_APK CL_PKGS_ZYPPER
#   CL_SETUP (étapes du projet, en base64)
# Idempotent : rien n'est refait si le marqueur porte déjà CL_HASH.
set -e
MARK=/var/lib/cl/bootstrap
if [ "$(cat "$MARK" 2>/dev/null)" = "$CL_HASH" ]; then
  echo "  ✓ bootstrap à jour"
  exit 0
fi
step() { printf '\n\033[1;36m▸ %s\033[0m\n' "$1"; }

step "paquets système"
BASE="tmux git bash sudo curl ca-certificates"
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends $BASE procps $CL_PKGS $CL_PKGS_APT >/dev/null
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y -q $BASE procps-ng $CL_PKGS $CL_PKGS_DNF
elif command -v microdnf >/dev/null 2>&1; then
  microdnf install -y $BASE procps-ng $CL_PKGS $CL_PKGS_DNF
elif command -v yum >/dev/null 2>&1; then
  yum install -y -q $BASE procps-ng $CL_PKGS $CL_PKGS_DNF
elif command -v apk >/dev/null 2>&1; then
  # claude (variante musl) a besoin de libgcc, libstdc++ et ripgrep
  apk add -q --no-cache $BASE procps libgcc libstdc++ ripgrep shadow $CL_PKGS $CL_PKGS_APK
elif command -v zypper >/dev/null 2>&1; then
  zypper -nq install $BASE procps $CL_PKGS $CL_PKGS_ZYPPER
else
  echo "aucun gestionnaire de paquets connu (apt, dnf, yum, apk, zypper)" >&2
  exit 2
fi
command -v tmux >/dev/null || { echo "tmux introuvable après installation (dépôts de l'image ?)" >&2; exit 2; }

step "utilisateur $CL_USER ($CL_UID:$CL_GID)"
if ! getent group "$CL_GID" >/dev/null; then
  groupadd -g "$CL_GID" "$CL_USER" 2>/dev/null || addgroup -g "$CL_GID" "$CL_USER"
fi
if ! getent passwd "$CL_UID" >/dev/null; then
  useradd -u "$CL_UID" -g "$CL_GID" -d "$CL_HOME" -M -s /bin/bash "$CL_USER" 2>/dev/null ||
    adduser -u "$CL_UID" -G "$(getent group "$CL_GID" | cut -d: -f1)" -h "$CL_HOME" -H -D -s /bin/bash "$CL_USER"
fi
NAME=$(getent passwd "$CL_UID" | cut -d: -f1)
mkdir -p "$CL_HOME" /etc/sudoers.d
chown "$CL_UID:$CL_GID" "$CL_HOME"
echo "$NAME ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/cl
chmod 440 /etc/sudoers.d/cl
asuser() { su -s /bin/sh "$NAME" -c "export HOME='$CL_HOME' CLAUDE_CONFIG_DIR='$CLAUDE_CONFIG_DIR' LANG=C.UTF-8 PATH=\"$CL_HOME/.local/bin:\$PATH\"; $1"; }

step "tmux du projet"
# préfixe Ctrl+B transmis par le hub ; double-clic = zoom ; R = relayout demandé au
# launcher (compteur surveillé par layout-watch) ; bash : les teammates sont exec-és
cat > /etc/cl-tmux.conf <<'CONF'
set -g default-shell /bin/bash
set -g default-terminal tmux-256color
set -g prefix C-b
set -g status off
set -g mouse on
set -g escape-time 10
set -g history-limit 50000
bind-key -n DoubleClick1Pane select-pane -t = \; resize-pane -Z -t =
bind-key R set -wF @cl_relayout "#{e|+:#{?@cl_relayout,#{@cl_relayout},0},1}" \; display-message "relayout demandé"
CONF

if [ "$CL_SKIP_CLAUDE" != 1 ]; then
  if asuser 'command -v claude' >/dev/null 2>&1; then
    step "claude déjà installé : $(asuser 'claude --version' 2>/dev/null)"
  else
    step "installation de claude"
    asuser 'curl -fsSL https://claude.ai/install.sh | bash'
  fi
fi

if [ -n "$CL_SETUP" ]; then
  step "étapes du projet (env.yaml : setup)"
  printf '%s' "$CL_SETUP" | base64 -d > /tmp/cl-setup.sh
  chmod 755 /tmp/cl-setup.sh
  asuser "cd '$CL_PROJ' && bash -e /tmp/cl-setup.sh"
fi

mkdir -p /var/lib/cl
echo "$CL_HASH" > "$MARK"
step "bootstrap terminé"
