#!/usr/bin/env bash
# The image of the toolchain stage of .devcontainer/Dockerfile, tagged novatrail-toolchain, for
# image.sh and modules.sh (R27 in docs/decisions.md). The jobs Image and Modules of ci.yml restore
# the file of docker save that the shared workflows of .github save on main, by the same key and
# path, and name it in TOOLCHAIN_TAR; the file holds the image novatrail-toolchain, the name
# spec/tools/toolchain/toolchain.sh gives (R42 .github). The Dockerfile pins everything it
# installs, so the image from the file is the one it would build. With no file, as in
# withdrawals.yml, or one that does not load as the image novatrail-toolchain, the image is built
# from the Dockerfile: the cache costs a run its speed, never its verdict. The file is deleted once
# read, so that nothing of it is left on the runner.
set -euo pipefail

tar="${TOOLCHAIN_TAR:-}"
if [[ -z "$tar" ]]; then
    :
elif [[ ! -f "$tar" ]]; then
    # A miss is said, so that a key or a path that no longer matches the entry of "Go" shows in
    # the log.
    echo "toolchain.sh: the cache gave no image novatrail-toolchain for this Dockerfile: it is built from it"
elif docker load --quiet --input "$tar" && docker image inspect novatrail-toolchain > /dev/null; then
    rm -f -- "$tar"
    echo "toolchain.sh: the image novatrail-toolchain is loaded from the cache"
    exit 0
else
    echo "::warning::The cache's file of the toolchain stage does not load as the image novatrail-toolchain: it is built from the Dockerfile, on every run until the entry is deleted (gh cache delete) or the Dockerfile changes."
    rm -f -- "$tar"
fi
docker build --target toolchain --tag novatrail-toolchain - < .devcontainer/Dockerfile
