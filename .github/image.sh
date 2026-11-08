#!/usr/bin/env bash
# What the job Image of ci.yml runs on the runner, which has Docker where the devcontainer has none
# (R28 in docs/decisions.md). It builds the release's files as the shared release does, from an
# archive of the commit in the toolchain stage, twice, and holds the two builds to the same bytes.
# Then it builds the image a deployment builds, the release's Dockerfile by --target release around
# the release's binary, and the image built from the source, and checks each: the version and
# commit it says, the user it runs as, its health check, and a stop on SIGTERM that it ends well.
set -euo pipefail

version=v0.0.0-ci
work="$(mktemp -d)"
containers=()
cleanup() {
    for container in "${containers[@]}"; do
        docker rm -f "$container" > /dev/null 2>&1 || true
    done
    rm -rf "$work"
}
trap cleanup EXIT

fail() {
    echo "::error::$*" >&2
    exit 1
}

docker build --target toolchain --tag toolchain - < .devcontainer/Dockerfile

tree="${work}/tree"
mkdir "$tree"
git archive HEAD | tar -x -C "$tree"
release_files() {
    docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp --volume "${tree}:/work" --workdir /work \
        toolchain just release-files "$version"
}
release_files
cp "${tree}/dist/SHA256SUMS" "${work}/first"
release_files
cmp "${work}/first" "${tree}/dist/SHA256SUMS" \
    || fail "two builds of the release's files from one commit differ: $(diff "${work}/first" "${tree}/dist/SHA256SUMS" | tr '\n' ' ')"
(cd "${tree}/dist" && sha256sum --check --strict SHA256SUMS)

read -r commit time < "${tree}/internal/version/archive.txt"
date="$(date -u -d "@${time}" +%Y-%m-%dT%H:%M:%SZ)"

# The image of a deployment: the release's Dockerfile with no context of its own, and the binary
# of the runner's platform in the context binary, as api.
mkdir "${work}/binary"
cp "${tree}/dist/api_${version}_linux_amd64" "${work}/binary/api"
docker build --target release --build-context "binary=${work}/binary" --tag api:release - < "${tree}/dist/Dockerfile"

# The image built from the source, as CI builds it.
docker build --build-arg "VERSION=${version}" --build-arg "COMMIT=${commit}" --build-arg "DATE=${date}" \
    --tag api:source "$tree"

check() {
    local image=$1
    echo "== ${image}"
    local said
    said="$(docker run --rm "$image" version)"
    [[ "$said" == "api ${version} ${commit} ${date} go"* ]] || fail "${image} says ${said}, not api ${version} ${commit} ${date}"

    local user
    user="$(docker image inspect --format '{{.Config.User}}' "$image")"
    [[ "$user" == 65532 ]] || fail "${image} runs as ${user:-root}, not nonroot, 65532"

    local container port
    container="$(docker run --detach --publish 127.0.0.1::8080 "$image")"
    containers+=("$container")
    port="$(docker port "$container" 8080/tcp | head -n 1 | sed 's/.*://')"
    local body=""
    for _ in $(seq 50); do
        if body="$(curl --silent --fail "http://127.0.0.1:${port}/health")"; then
            break
        fi
        sleep 0.2
    done
    [[ "$body" == '{"status":"ok"}' ]] || fail "${image} answers /health with ${body:-nothing}; its log: $(docker logs "$container" 2>&1)"

    docker stop --time 10 "$container" > /dev/null
    local code
    code="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
    [[ "$code" == 0 ]] || fail "${image} exits ${code} after SIGTERM; its log: $(docker logs "$container" 2>&1)"
    # The log is read once and matched as a whole: grep -q on a pipe would close it at the first
    # match, and docker logs, cut off mid-write, would fail the pipeline.
    local log
    log="$(docker logs "$container" 2>&1)"
    [[ "$log" == *'"message":"startup","version":"'"${version}"'"'* ]] || fail "${image} logs no startup line naming ${version}: ${log}"
    [[ "$log" == *'"message":"shutdown"'* ]] || fail "${image} logs no shutdown line: ${log}"
}
check api:release
check api:source
