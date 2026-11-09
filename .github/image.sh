#!/usr/bin/env bash
# What the job Image of ci.yml runs on the runner, which has Docker where the devcontainer has none
# (R28 in docs/decisions.md). It builds the release's files as the shared release does, from an
# archive of the commit in the toolchain stage, twice, and holds the two builds to the same bytes.
# Then it builds the image a deployment builds, the release's Dockerfile by --target release around
# the release's binary, and the image built from the source, and checks each: the version and
# commit it says, the user it runs as, its migrations, its health check, the release of modules
# whose packages it read, its size, and a stop on SIGTERM
# that it ends well. serve opens its databases and the sign-in service before it starts (R33): the
# databases are a PostgreSQL of the runner's, 17.11 as the tests' is, pinned by its digest, and
# the sign-in service takes a key made here for the run, which no call ever uses.
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

# PostgreSQL on the runner's network, on a port of its own, with the databases api and openfga.
# A string to the loopback needs no TLS (R29).
postgres_image=postgres:17.11-bookworm@sha256:665dc7b37ac52a17e298511414c21d23ffa4e6a6ef6225b3e5d59c7afb35e994
pg_port=55432
# A password of this run alone, so that none is written here.
pg_password="$(openssl rand -hex 16)"
postgres="$(docker run --detach --network host --env "POSTGRES_PASSWORD=${pg_password}" "$postgres_image" -p "$pg_port")"
containers+=("$postgres")
for _ in $(seq 100); do
    docker exec "$postgres" pg_isready --quiet --host 127.0.0.1 --port "$pg_port" && break
    sleep 0.3
done
for database in api openfga; do
    docker exec --env "PGPASSWORD=${pg_password}" "$postgres" createdb --host 127.0.0.1 --port "$pg_port" --username postgres "$database"
done
database_url() { echo "postgres://postgres:${pg_password}@127.0.0.1:${pg_port}/$1?sslmode=disable"; }

# A service account's key of no project, made for this run: serve takes it, and calls nothing.
key="$(jq --null-input --arg key "$(openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 2> /dev/null)" '{
    type: "service_account", project_id: "novatrail-ci", private_key_id: "ci", private_key: $key,
    client_email: "api@novatrail-ci.iam.gserviceaccount.com", client_id: "1",
    token_uri: "https://oauth2.googleapis.com/token"}')"

# The release's migrate brings both databases up, the store of the rights with its model.
docker run --rm --network host \
    --env "API_MIGRATE_DATABASE_URL=$(database_url api)" --env "API_MIGRATE_OPENFGA_DATABASE_URL=$(database_url openfga)" \
    api:release migrate > "${work}/migrate.log" 2>&1 || fail "api:release migrate fails: $(cat "${work}/migrate.log")"

serve_port=18080
check() {
    local image=$1
    echo "== ${image}"
    local said
    said="$(docker run --rm "$image" version)"
    [[ "$said" == "api ${version} ${commit} ${date} go"* ]] || fail "${image} says ${said}, not api ${version} ${commit} ${date}"

    local user
    user="$(docker image inspect --format '{{.Config.User}}' "$image")"
    [[ "$user" == 65532 ]] || fail "${image} runs as ${user:-root}, not nonroot, 65532"

    local container port=$serve_port
    serve_port=$((serve_port + 1))
    container="$(docker run --detach --network host --env "PORT=${port}" \
        --env "API_DATABASE_URL=$(database_url api)" --env "API_OPENFGA_DATABASE_URL=$(database_url openfga)" \
        --env API_FIREBASE_PROJECT_ID=novatrail-ci --env "API_FIREBASE_CREDENTIALS=${key}" "$image")"
    containers+=("$container")
    local body=""
    for _ in $(seq 150); do
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
    # serve read the packages of the release modules.json pins, which the image's build laid (R41).
    [[ "$log" == *'"modules_release":"'"${modules_release}"'"'* ]] || fail "${image} read no packages of modules ${modules_release}: ${log}"
    # The size of the image, which R07 left to measure (Д8 in RUN.md).
    echo "${image}: $(docker image inspect --format '{{.Size}}' "$image") bytes"
}
modules_release="$(jq --exit-status --raw-output .release "${tree}/modules.json")"
check api:release
check api:source
