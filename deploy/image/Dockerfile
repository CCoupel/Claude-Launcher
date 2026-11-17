# Image du launcher du cluster (#167) — contrat : contracts/launcher-env.md « Image de conteneur ».
#
# Le binaire n'est PAS compilé ici : il est construit avant, par la même commande que la release
# (CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X claude-launcher-ng2/core.NgVersion=<tag>"),
# puis copié (BORE : une seule procédure de build). Contexte de construction = racine du dépôt
# (filtré par .dockerignore). Construction locale : deploy/image/build.sh.
#
#   BIN : chemin du binaire Linux dans le contexte (défaut, release multi-architecture : dist/claude-launcher-ng2-linux-<arch>)
FROM docker.io/library/ubuntu:26.04

# architecture cible : posée par buildah / docker buildx (--platform), ou --build-arg TARGETARCH
ARG TARGETARCH
ARG KUBECTL_VERSION=v1.36.5
# sommes figées ici (relevées sur dl.k8s.io le 2026-11-16), jamais téléchargées avec le binaire :
# changer KUBECTL_VERSION impose de les relever (curl …/kubectl.sha256, pour chaque architecture)
ARG KUBECTL_SHA256_amd64=33bc88a24c3b09cf55bfd59e6ca977881b61fc3f25ab65bc80c65b430248648e
ARG KUBECTL_SHA256_arm64=88fc1aca8fd0c1b44fe379f67c39bd6858bdfc50241dc55775f84d0f5b31da3a

# outils requis (contrat) : bash, git, gh, kubectl, certificats racine — ni claude, ni tmux.
# kubectl à la version du cluster, somme sha256 vérifiée ; couche stable (change rarement) avant le binaire.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends bash ca-certificates curl git gh; \
    curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl"; \
    case "${TARGETARCH}" in \
      amd64) sum="${KUBECTL_SHA256_amd64}" ;; \
      arm64) sum="${KUBECTL_SHA256_arm64}" ;; \
      *) echo "architecture ${TARGETARCH} : somme sha256 de kubectl non figée" >&2; exit 1 ;; \
    esac; \
    echo "${sum}  /usr/local/bin/kubectl" | sha256sum -c -; \
    chmod 755 /usr/local/bin/kubectl; \
    apt-get purge -y curl; apt-get autoremove -y; rm -rf /var/lib/apt/lists/*; \
    mkdir -p /home/launcher; chown 1000:1000 /home/launcher; chmod 1777 /home/launcher

# tout ce qui ne dépend pas du binaire, avant lui : avec le stockage vfs, chaque instruction recopie
# l'image entière — seules les deux dernières sont refaites quand le binaire change
COPY --chmod=755 deploy/image/entrypoint.sh /usr/local/bin/entrypoint.sh
ENV HOME=/home/launcher \
    CL_UPDATE_MODE=notify
# uid:gid du partage (1000:1000) ; le chart peut les remplacer (runAsUser, runAsGroup)
USER 1000:1000
WORKDIR /home/launcher
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# déclarés APRÈS ce qui précède : un ARG qui change invalide le cache des instructions qui le suivent
ARG BIN=dist/claude-launcher-ng2-linux-${TARGETARCH}
ARG VERSION=dev
COPY --chmod=755 ${BIN} /usr/local/bin/claude-launcher-ng2
LABEL org.opencontainers.image.title="claude-launcher" \
      org.opencontainers.image.source="https://github.com/CCoupel/Claude-Launcher" \
      org.opencontainers.image.licenses="GPL-3.0" \
      org.opencontainers.image.version="${VERSION}"
