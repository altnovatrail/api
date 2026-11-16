#!/usr/bin/env bash
# What the job Modules of ci.yml and the job Withdrawals of withdrawals.yml run on the runner (R07,
# R41, R53, R61 in docs/decisions.md): the recipe each names, just ci-modules or just
# ci-withdrawals, in a stage of the devcontainer, whose image toolchain.sh gives — toolchain, or
# toolchain-core where the job names it in TOOLCHAIN_STAGE, as Withdrawals does. Each has Node for
# release-verify, Go for cmd/modules-check and api packages, and curl and jq, with the network;
# toolchain adds the nss_wrapper that the test of the release on its packages needs for PostgreSQL
# (R30). The checkout is mounted with its spec submodule, and for ci-modules with the history of
# main, which the check reads main's modules.json from.
set -euo pipefail

case "${1:-}" in
    ci-modules | ci-withdrawals) recipe="$1" ;;
    *) echo "modules.sh: name the recipe, ci-modules or ci-withdrawals, not ${1:-none}" >&2; exit 2 ;;
esac

.github/toolchain.sh
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp --volume "${PWD}:/work" --workdir /work \
    novatrail-toolchain just "$recipe"
