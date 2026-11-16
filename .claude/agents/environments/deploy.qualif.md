# Adaptations projet — deploy QUALIF (Claude-Launcher)

QUALIF = binaires de test pour les essais réels de l'utilisateur, déposés à côté du binaire du hub, **sans le remplacer**.

- Compiler depuis le candidat : Linux `claude-launcher-ng2-<X.Y.Z.a>` et Windows `claude-launcher-ng2-<X.Y.Z.a>.exe`
  (`-tags desktop,production -H windowsgui`, voir `scripts/ci.sh`).
- Racine : `ROOT_BASE=${CL_ROOT_BASE:-/share/MFS/ROOT/CLAUDE_PROJETS}` (`CL_ROOT_BASE` de `qualif.env` si posée, #166).
- Déposer dans `$ROOT_BASE/` (cible déclarée), jamais par-dessus `claude-launcher-ng2` ni `.exe`.
- Donner à l'utilisateur les chemins exacts et ce qu'il doit tester ; la validation se fait sur ses essais Windows/Linux.
- Après la release PROD, supprimer les binaires de test QUALIF de ce dossier (garder seulement les binaires de prod et le `.bak`).
