# Development container for NovaTrail's api.
# Exact versions only: the base image is pinned by digest, every tool below by an ARG default.
# Anything a builder understands: no heredocs, no RUN --mount, no TARGETARCH — architecture is
# detected at RUN time via dpkg instead. A host that has Docker can build this, and that is the
# whole requirement.
#
# Two stages. `toolchain` holds what it takes to check the schemas and documents, and is the
# stage the automated checks run inside. `devcontainer` adds what a person needs on top of that
# and is the default stage, so building this file with no target gives the full environment.

# -- toolchain: checks the schemas and documents -----------------------------

FROM mcr.microsoft.com/devcontainers/base:3.0.8-noble@sha256:d7c468679f45a52ad3673d06656b5bf16e488990b17216a1fa295d9e1f89d724 AS toolchain

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

# Node.js LTS: the JSON Schema validators and the document linters are npm packages.
ARG NODE_VERSION=24.21.0
ARG JUST_VERSION=1.58.0

# Every archive comes through one helper, so that every download is retried the same way.
# --retry-all-errors is the point of it: a transfer that dies partway is not something a plain
# --retry repeats, and that is exactly the failure a small tunnel MTU produces. The file is
# renamed only once curl is happy, so a half-written archive is never unpacked as a whole one.
RUN arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) node_arch=x64; gnu_arch=x86_64 ;; \
        arm64) node_arch=arm64; gnu_arch=aarch64 ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    workdir="$(mktemp -d)"; \
    fetch() { \
        local url="$1" out="$2"; \
        if [[ ! -s "$out" ]]; then \
            curl -fsSL --proto "=https" --proto-redir "=https" --retry 3 --retry-all-errors \
                -o "$out.part" "$url"; \
            mv "$out.part" "$out"; \
        fi; \
    }; \
    \
    echo "node ${NODE_VERSION}"; \
    mkdir -p /usr/local/node; \
    fetch "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_arch}.tar.gz" \
        "$workdir/node-${NODE_VERSION}-${node_arch}.tar.gz"; \
    tar -C /usr/local/node --strip-components=1 -xzf "$workdir/node-${NODE_VERSION}-${node_arch}.tar.gz"; \
    \
    echo "just ${JUST_VERSION}"; \
    fetch "https://github.com/casey/just/releases/download/${JUST_VERSION}/just-${JUST_VERSION}-${gnu_arch}-unknown-linux-musl.tar.gz" \
        "$workdir/just-${JUST_VERSION}-${gnu_arch}.tar.gz"; \
    tar -C "$workdir" -xzf "$workdir/just-${JUST_VERSION}-${gnu_arch}.tar.gz" just; \
    install -m 0755 "$workdir/just" /usr/local/bin/just; \
    \
    rm -rf "$workdir"

ENV PATH="/usr/local/node/bin:/home/vscode/.local/bin:${PATH}"

# -- devcontainer: the toolchain, plus what a person works with --------------

FROM toolchain AS devcontainer

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]

# The GitHub CLI: pull requests, the runs of the checks and their alerts live only on
# github.com and are invisible from inside a container otherwise.
ARG GH_VERSION=2.101.0
# The language server for justfiles: the just extension in devcontainer.json starts it, and
# warns on every justfile it opens when it is missing.
ARG JUST_LSP_VERSION=0.10.0
# Claude Code CLI. The VS Code extension in devcontainer.json must have the same version.
ARG CLAUDE_CODE_VERSION=2.1.280

RUN arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
        amd64) gnu_arch=x86_64 ;; \
        arm64) gnu_arch=aarch64 ;; \
        *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    workdir="$(mktemp -d)"; \
    fetch() { \
        local url="$1" out="$2"; \
        if [[ ! -s "$out" ]]; then \
            curl -fsSL --proto "=https" --proto-redir "=https" --retry 3 --retry-all-errors \
                -o "$out.part" "$url"; \
            mv "$out.part" "$out"; \
        fi; \
    }; \
    \
    echo "gh ${GH_VERSION}"; \
    fetch "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${arch}.tar.gz" \
        "$workdir/gh-${GH_VERSION}-${arch}.tar.gz"; \
    tar -C "$workdir" --strip-components=2 \
        -xzf "$workdir/gh-${GH_VERSION}-${arch}.tar.gz" \
        "gh_${GH_VERSION}_linux_${arch}/bin/gh"; \
    install -m 0755 "$workdir/gh" /usr/local/bin/gh; \
    \
    echo "just-lsp ${JUST_LSP_VERSION}"; \
    fetch "https://github.com/terror/just-lsp/releases/download/${JUST_LSP_VERSION}/just-lsp-${JUST_LSP_VERSION}-${gnu_arch}-unknown-linux-musl.tar.gz" \
        "$workdir/just-lsp-${JUST_LSP_VERSION}-${gnu_arch}.tar.gz"; \
    tar -C "$workdir" -xzf "$workdir/just-lsp-${JUST_LSP_VERSION}-${gnu_arch}.tar.gz" ./just-lsp; \
    install -m 0755 "$workdir/just-lsp" /usr/local/bin/just-lsp; \
    \
    rm -rf "$workdir"

# Claude Code comes from the registry at an exact version, rather than from an installer
# script run off the network: what arrives is the package that version names, and npm checks
# the archive against the hash the registry publishes for it. That package's own install step
# is allowed by name — it is what puts the native binary in place of the stub the package
# ships with, and without it the CLI starts through a Node wrapper on every invocation. npm's
# download cache under root's home is cleared in the same layer, since the container runs as
# vscode.
RUN echo "claude code ${CLAUDE_CODE_VERSION}" \
    && npm install -g --allow-scripts=@anthropic-ai/claude-code \
        "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
    && npm cache clean --force

# The dirs the named volumes are mounted over are created here so the volumes inherit vscode
# ownership — a dir Docker makes for a mount belongs to root, and VS Code Server could not write
# to its own home — and the image is left as that user: nothing in it needs root once it is built.
USER vscode
RUN mkdir -p /home/vscode/.claude /home/vscode/.codex /home/vscode/.config/gh \
        /home/vscode/.vscode-server/extensions
