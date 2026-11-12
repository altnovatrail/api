#!/usr/bin/env bash
# What the job Modules of ci.yml runs on the runner (R07, R41, R53 in docs/decisions.md): just
# ci-modules in the toolchain stage of the devcontainer, which has Node for release-verify, Go for
# cmd/modules-check, api packages and the test of the release on its packages, and curl and jq,
# with the network. The checkout is mounted with its spec submodule and the history of main, which
# the check reads main's modules.json from.
set -euo pipefail

docker build --target toolchain --tag toolchain - < .devcontainer/Dockerfile
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp --volume "${PWD}:/work" --workdir /work \
    toolchain just ci-modules
