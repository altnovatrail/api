#!/usr/bin/env bash
# What the job Modules of ci.yml and the job Withdrawals of withdrawals.yml run on the runner (R07,
# R41, R53, R61 in docs/decisions.md): the recipe each names, just ci-modules or just
# ci-withdrawals, in the toolchain stage of the devcontainer, which has Node for release-verify, Go
# for cmd/modules-check, api packages and the test of the release on its packages, and curl and
# jq, with the network. The checkout is mounted with its spec submodule, and for ci-modules with
# the history of main, which the check reads main's modules.json from.
set -euo pipefail

case "${1:-}" in
    ci-modules | ci-withdrawals) recipe="$1" ;;
    *) echo "modules.sh: name the recipe, ci-modules or ci-withdrawals, not ${1:-none}" >&2; exit 2 ;;
esac

docker build --target toolchain --tag toolchain - < .devcontainer/Dockerfile
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp --volume "${PWD}:/work" --workdir /work \
    toolchain just "$recipe"
