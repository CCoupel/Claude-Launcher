#!/bin/bash
# Construction locale de l'image du launcher du cluster (#167), sans démon (buildah).
#
#   deploy/image/build.sh <version>            ex. deploy/image/build.sh v6.3.0.1
#
# 1. binaire Linux statique, même commande que release.yml (BORE) :
#      build/candidate_<version>/claude-launcher-ng2-linux-amd64
# 2. image localhost/claude-launcher:<version> (contexte = racine du dépôt, .dockerignore)
# 3. archive OCI + empreinte à côté du binaire (tâche 45) si OCI_ARCHIVE=1
#
# Conteneur de dev : ni /dev/fuse ni CAP_SYS_ADMIN → stockage vfs, et la couche d'outils (RUN apt)
# exige root : BUILDAH="sudo buildah" (défaut si l'utilisateur n'est pas root). En CI GitHub, BUILDAH=buildah.
# Variables : BUILDAH, IMAGE (défaut localhost/claude-launcher), OCI_ARCHIVE (0/1), ARCH (défaut amd64).
set -euo pipefail
cd "$(dirname "$0")/../.."

version=${1:-}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || { echo "usage : $0 vX.Y.Z[.a] (reçu « $version »)" >&2; exit 2; }
arch=${ARCH:-amd64}
image=${IMAGE:-localhost/claude-launcher}
if [ -z "${BUILDAH:-}" ]; then
  if [ "$(id -u)" = 0 ]; then BUILDAH=buildah; else BUILDAH="sudo buildah"; fi
fi
command -v buildah >/dev/null || { echo "ECHEC : buildah absent (.claude-launcher/env.yaml)" >&2; exit 1; }

out=build/candidate_${version}
bin=$out/claude-launcher-ng2-linux-$arch
mkdir -p "$out"
t0=${EPOCHREALTIME/[.,]/}
( cd ng2 && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X claude-launcher-ng2/core.NgVersion=${version}" -o "../$bin" . )
t1=${EPOCHREALTIME/[.,]/}

# --storage-driver en option (pas seulement STORAGE_DRIVER) : buildah refuse de choisir si un
# dossier overlay d'un essai précédent existe
$BUILDAH --storage-driver vfs build --layers --isolation chroot \
  --build-arg TARGETARCH="$arch" --build-arg BIN="$bin" --build-arg VERSION="$version" \
  -f deploy/image/Dockerfile -t "$image:$version" .
t2=${EPOCHREALTIME/[.,]/}

if [ "${OCI_ARCHIVE:-0}" = 1 ]; then
  arc=$out/claude-launcher-image-${version}-linux-$arch.oci.tar
  $BUILDAH --storage-driver vfs push "$image:$version" "oci-archive:$arc:$image:$version"
  [ "$(id -u)" = 0 ] || sudo chown "$(id -u):$(id -g)" "$arc"
  (cd "$out" && sha256sum "$(basename "$arc")" "$(basename "$bin")" > SHA256SUMS)
fi
t3=${EPOCHREALTIME/[.,]/}
fmt() { local ds=$(( ($1 + 50000) / 100000 )); echo "$((ds / 10)),$((ds % 10)) s"; }
echo "image $image:$version — durées : binaire $(fmt $((t1 - t0))) · image $(fmt $((t2 - t1)))$([ "${OCI_ARCHIVE:-0}" = 1 ] && echo " · archive $(fmt $((t3 - t2)))") · total $(fmt $((t3 - t0)))"
