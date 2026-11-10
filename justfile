# The recipes of api's code. Documents are checked by spec's own justfile:
# just -f spec/justfile docs-check-repo.

set shell := ["bash", "-euo", "pipefail", "-c"]

# Where the linker sets the build's identity, internal/version.
MODULE := "github.com/altnovatrail/api"
SYMBOLS := MODULE + "/internal/version"

# The tools only the checks need, at exact versions, as spec's justfile pins
# them (R30 spec). Each is a Go program run straight from its module, so
# nothing is installed for it: the Go module proxy serves that version and the
# checksum database vouches for it. Dependabot does not see them, so they are
# raised by hand.
GOVULNCHECK := "golang.org/x/vuln/cmd/govulncheck@v1.9.0"
GO_LICENSES := "github.com/google/go-licenses/v2@v2.0.1"

# List the recipes.
default:
    @{{quote(just_executable())}} --list

# The build's identity: a build between tags says how far past one it is, a
# tree with changes says so, and a repository with no tag yet names the commit.
# The binary is static, without cgo and without the paths of this machine, as a production image
# needs it (RULES.md, Production images).
# Build the binary into bin/api.
build:
    #!/usr/bin/env bash
    set -euo pipefail
    version=$(git describe --tags --match 'v[0-9]*' --always --dirty 2> /dev/null || echo dev)
    commit=$(git rev-parse --short HEAD 2> /dev/null || echo unknown)
    date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    CGO_ENABLED=0 go build -trimpath \
        -ldflags "-X {{SYMBOLS}}.Version=${version} -X {{SYMBOLS}}.Commit=${commit} -X {{SYMBOLS}}.Date=${date}" \
        -o bin/api ./cmd/api

# Serve on this machine, with the configuration of the environment.
run:
    go run ./cmd/api serve

# The connection string is API_MIGRATE_DATABASE_URL, or the file API_MIGRATE_DATABASE_URL_FILE
# names, outside the repository: the direct one, not the pooler's (R29).
# Apply the migrations of the database api.
migrate:
    go run ./cmd/api migrate

# What .github's shared release.yml runs to build a release, in the toolchain stage of the commit
# it releases, from that commit's tree with no git in it, and then signs every file (R28; R18
# .github). dist/ is made afresh and holds plain files alone: the binary for each platform below,
# static and named api_<version>_<os>_<arch>; the Dockerfile a deployment builds the image by
# (R105, R106 spec); the licenses a binary has to come with; and SHA256SUMS over the rest. The
# commit and its time come from internal/version/archive.txt, which git archive writes, so a build
# of the same commit gives the same bytes wherever it runs.
# Build a release's files into dist/, for a version such as v0.1.0.
release-files version:
    #!/usr/bin/env bash
    set -euo pipefail
    version={{ quote(version) }}
    if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
        echo "release-files: the version is v<major>.<minor>.<patch>, as v0.1.0, not ${version}" >&2
        exit 1
    fi
    read -r commit time < internal/version/archive.txt || true
    if ! [[ "${commit:-}" =~ ^[0-9a-f]{40}$ && "${time:-}" =~ ^[0-9]+$ ]]; then
        echo "release-files: internal/version/archive.txt names no commit and time: build from git archive of a commit, as the release does" >&2
        exit 1
    fi
    date="$(date -u -d "@${time}" +%Y-%m-%dT%H:%M:%SZ)"
    # Nothing of the caller's Go environment reaches the build: the binaries are the same wherever
    # the recipe runs, and a release can be built again here and compared with its SHA256SUMS.
    export GOENV=off GOFLAGS= GOEXPERIMENT=
    unset GOAMD64 GOARM64 GOOS GOARCH
    rm -rf dist
    mkdir dist
    for platform in linux/amd64 linux/arm64; do
        os="${platform%/*}"
        arch="${platform#*/}"
        # No VCS stamp, so that a build from a checkout matches one from the release's tree.
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
            -ldflags "-s -w -X {{SYMBOLS}}.Version=${version} -X {{SYMBOLS}}.Commit=${commit} -X {{SYMBOLS}}.Date=${date}" \
            -o "dist/api_${version}_${os}_${arch}" ./cmd/api
    done
    cp Dockerfile dist/Dockerfile
    # Apache-2.0 asks a copy of itself to go with the work, and Go's BSD license asks a binary
    # that holds its standard library to reproduce its notice; so do the licenses of the modules
    # the binaries are built from, which release-licenses names.
    cp LICENSE dist/LICENSE
    cp "$(go env GOROOT)/LICENSE" dist/LICENSE-go
    {{ quote(just_executable()) }} release-licenses dist
    (cd dist && sha256sum -- api_* Dockerfile LICENSE* $(ls NOTICE-* 2> /dev/null || true) > SHA256SUMS)

# The modules besides the standard library that the binary is built from, each with the name its
# license takes in a release: LICENSE-<name>, and NOTICE-<name> beside it when the module has a
# NOTICE, as module-kit names them (R38 module-kit): pgx and goose (R29), OpenFGA and the modules
# they take (R31), the Admin SDK for Go and the modules it takes (R32), Connect and spec's module
# of contracts (R33), module-kit and spec's own module (R41), tutor (R45), and the SDKs of its
# adapters with the modules they take (R46). A module the binary takes beyond these fails
# release-licenses, and ci-lint with it, until it is named here. Each entry is
# <module path>=<name>, one a line, the name the path without its host, its slashes dashes; where
# that name would read as a license the release has of its own — go, Go's, and api, api's — the
# host stays in front of it, and release-licenses refuses a name twice or either of those.
THIRD_PARTY := """
    cel.dev/expr=expr
    cloud.google.com/go=cloud.google.com-go
    cloud.google.com/go/auth=go-auth
    cloud.google.com/go/auth/oauth2adapt=go-auth-oauth2adapt
    cloud.google.com/go/compute/metadata=go-compute-metadata
    cloud.google.com/go/firestore=go-firestore
    cloud.google.com/go/iam=go-iam
    cloud.google.com/go/longrunning=go-longrunning
    cloud.google.com/go/monitoring=go-monitoring
    cloud.google.com/go/storage=go-storage
    connectrpc.com/connect/v2=connect-v2
    firebase.google.com/go/v4=go-v4
    github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp=GoogleCloudPlatform-opentelemetry-operations-go-detectors-gcp
    github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric=GoogleCloudPlatform-opentelemetry-operations-go-exporter-metric
    github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping=GoogleCloudPlatform-opentelemetry-operations-go-internal-resourcemapping
    github.com/IBM/pgxpoolprometheus=IBM-pgxpoolprometheus
    github.com/Masterminds/squirrel=Masterminds-squirrel
    github.com/MicahParks/keyfunc=MicahParks-keyfunc
    github.com/Yiling-J/theine-go=Yiling-J-theine-go
    github.com/altnovatrail/module-kit=altnovatrail-module-kit
    github.com/altnovatrail/spec=altnovatrail-spec
    github.com/altnovatrail/spec/contracts=altnovatrail-spec-contracts
    github.com/altnovatrail/tutor=altnovatrail-tutor
    github.com/anthropics/anthropic-sdk-go=anthropics-anthropic-sdk-go
    github.com/antlr4-go/antlr/v4=antlr4-go-antlr-v4
    github.com/bahlo/generic-list-go=bahlo-generic-list-go
    github.com/beorn7/perks=beorn7-perks
    github.com/buger/jsonparser=buger-jsonparser
    github.com/cenkalti/backoff/v4=cenkalti-backoff-v4
    github.com/cenkalti/backoff/v5=cenkalti-backoff-v5
    github.com/cespare/xxhash/v2=cespare-xxhash-v2
    github.com/cncf/xds/go=cncf-xds-go
    github.com/coder/websocket=coder-websocket
    github.com/emirpasic/gods=emirpasic-gods
    github.com/envoyproxy/go-control-plane/envoy=envoyproxy-go-control-plane-envoy
    github.com/envoyproxy/protoc-gen-validate=envoyproxy-protoc-gen-validate
    github.com/felixge/httpsnoop=felixge-httpsnoop
    github.com/fsnotify/fsnotify=fsnotify-fsnotify
    github.com/go-jose/go-jose/v4=go-jose-go-jose-v4
    github.com/go-logr/logr=go-logr-logr
    github.com/go-logr/stdr=go-logr-stdr
    github.com/go-viper/mapstructure/v2=go-viper-mapstructure-v2
    github.com/golang-jwt/jwt/v4=golang-jwt-jwt-v4
    github.com/google/cel-go=google-cel-go
    github.com/google/s2a-go=google-s2a-go
    github.com/google/uuid=google-uuid
    github.com/googleapis/enterprise-certificate-proxy=googleapis-enterprise-certificate-proxy
    github.com/googleapis/gax-go/v2=googleapis-gax-go-v2
    github.com/grpc-ecosystem/go-grpc-middleware=grpc-ecosystem-go-grpc-middleware
    github.com/grpc-ecosystem/go-grpc-middleware/v2=grpc-ecosystem-go-grpc-middleware-v2
    github.com/grpc-ecosystem/grpc-gateway/v2=grpc-ecosystem-grpc-gateway-v2
    github.com/invopop/jsonschema=invopop-jsonschema
    github.com/jackc/pgpassfile=jackc-pgpassfile
    github.com/jackc/pgservicefile=jackc-pgservicefile
    github.com/jackc/pgx/v5=jackc-pgx-v5
    github.com/jackc/puddle/v2=jackc-puddle-v2
    github.com/klauspost/cpuid/v2=klauspost-cpuid-v2
    github.com/lann/builder=lann-builder
    github.com/lann/ps=lann-ps
    github.com/mfridman/interpolate=mfridman-interpolate
    github.com/munnerz/goautoneg=munnerz-goautoneg
    github.com/natefinch/wrap=natefinch-wrap
    github.com/oklog/ulid/v2=oklog-ulid-v2
    github.com/openai/openai-go/v3=openai-openai-go-v3
    github.com/openfga/api/proto=openfga-api-proto
    github.com/openfga/language/pkg/go=openfga-language-pkg-go
    github.com/openfga/openfga=openfga-openfga
    github.com/pb33f/ordered-map/v2=pb33f-ordered-map-v2
    github.com/pelletier/go-toml/v2=pelletier-go-toml-v2
    github.com/pressly/goose/v3=pressly-goose-v3
    github.com/prometheus/client_golang=prometheus-client_golang
    github.com/prometheus/client_model=prometheus-client_model
    github.com/prometheus/common=prometheus-common
    github.com/prometheus/procfs=prometheus-procfs
    github.com/sagikazarmark/locafero=sagikazarmark-locafero
    github.com/santhosh-tekuri/jsonschema/v6=santhosh-tekuri-jsonschema-v6
    github.com/sethvargo/go-retry=sethvargo-go-retry
    github.com/sourcegraph/conc=sourcegraph-conc
    github.com/spf13/afero=spf13-afero
    github.com/spf13/cast=spf13-cast
    github.com/spf13/pflag=spf13-pflag
    github.com/spf13/viper=spf13-viper
    github.com/spiffe/go-spiffe/v2=spiffe-go-spiffe-v2
    github.com/standard-webhooks/standard-webhooks/libraries=standard-webhooks-standard-webhooks-libraries
    github.com/stretchr/testify=stretchr-testify
    github.com/subosito/gotenv=subosito-gotenv
    github.com/tidwall/gjson=tidwall-gjson
    github.com/tidwall/match=tidwall-match
    github.com/tidwall/pretty=tidwall-pretty
    github.com/tidwall/sjson=tidwall-sjson
    github.com/zeebo/xxh3=zeebo-xxh3
    go.opentelemetry.io/auto/sdk=auto-sdk
    go.opentelemetry.io/contrib/detectors/gcp=contrib-detectors-gcp
    go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc=contrib-instrumentation-google.golang.org-grpc-otelgrpc
    go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp=contrib-instrumentation-net-http-otelhttp
    go.opentelemetry.io/otel=otel
    go.opentelemetry.io/otel/exporters/otlp/otlptrace=otel-exporters-otlp-otlptrace
    go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc=otel-exporters-otlp-otlptrace-otlptracegrpc
    go.opentelemetry.io/otel/log=otel-log
    go.opentelemetry.io/otel/metric=otel-metric
    go.opentelemetry.io/otel/sdk=otel-sdk
    go.opentelemetry.io/otel/sdk/metric=otel-sdk-metric
    go.opentelemetry.io/otel/trace=otel-trace
    go.opentelemetry.io/proto/otlp=proto-otlp
    go.starlark.net=go.starlark.net
    go.uber.org/mock=mock
    go.uber.org/multierr=multierr
    go.uber.org/zap=zap
    go.yaml.in/yaml/v3=yaml-v3
    go.yaml.in/yaml/v4=yaml-v4
    golang.org/x/crypto=x-crypto
    golang.org/x/exp=x-exp
    golang.org/x/net=x-net
    golang.org/x/oauth2=x-oauth2
    golang.org/x/sync=x-sync
    golang.org/x/sys=x-sys
    golang.org/x/text=x-text
    golang.org/x/time=x-time
    gonum.org/v1/gonum=v1-gonum
    google.golang.org/api=google.golang.org-api
    google.golang.org/genproto=genproto
    google.golang.org/genproto/googleapis/api=genproto-googleapis-api
    google.golang.org/genproto/googleapis/rpc=genproto-googleapis-rpc
    google.golang.org/grpc=grpc
    google.golang.org/protobuf=protobuf
    gopkg.in/yaml.v3=yaml.v3
"""

# It fails when the modules are not those THIRD_PARTY names.
# Copy the licenses of the modules the binary is built from into a folder.
release-licenses folder:
    #!/usr/bin/env bash
    set -euo pipefail
    folder={{ quote(folder) }}
    export GOENV=off GOFLAGS= GOEXPERIMENT= CGO_ENABLED=0
    unset GOAMD64 GOARM64
    built="$(for platform in linux/amd64 linux/arm64; do
        GOOS="${platform%/*}" GOARCH="${platform#*/}" go list -deps -f '{{{{with .Module}}{{{{.Path}}{{{{end}}' ./cmd/api
    done | sort -u | grep -v -x -F {{ quote(MODULE) }} || true)"
    entries={{ quote(THIRD_PARTY) }}
    named="$(for entry in $entries; do echo "${entry%%=*}"; done | sort -u)"
    names="$(for entry in $entries; do echo "${entry#*=}"; done | sort)"
    taken="$( (echo "$names" | uniq -d; echo "$names" | grep -x -E 'go|api') || true)"
    if [ -n "$taken" ]; then
        echo "release-licenses: these names are taken twice, or by a license the release has of its own:" >&2
        echo "$taken" >&2
        exit 1
    fi
    if [ "$built" != "$named" ]; then
        echo "release-licenses: the binary is built from these modules besides the standard library:" >&2
        echo "${built:-none}" >&2
        echo "and the justfile names these, whose licenses go with a release:" >&2
        echo "${named:-none}" >&2
        echo "Name each module the binary is built from in THIRD_PARTY, with its license (R28)." >&2
        exit 1
    fi
    mkdir -p "$folder"
    for entry in $entries; do
        dir="$(go list -m -f '{{{{.Dir}}' "${entry%%=*}")"
        # The license is LICENSE, or LICENSE.txt, LICENSE.md or COPYING as some modules name it.
        license=""
        for name in LICENSE LICENSE.txt LICENSE.md COPYING; do
            if [ -f "$dir/$name" ]; then
                license="$dir/$name"
                break
            fi
        done
        if [ -z "$license" ]; then
            echo "release-licenses: ${entry%%=*} has no LICENSE, LICENSE.txt, LICENSE.md or COPYING" >&2
            exit 1
        fi
        cp "$license" "$folder/LICENSE-${entry#*=}"
        # Apache-2.0 asks that a NOTICE go with the work: NOTICE, or NOTICE.txt as grpc names it.
        for name in NOTICE NOTICE.txt NOTICE.md; do
            if [ -f "$dir/$name" ]; then
                cp "$dir/$name" "$folder/NOTICE-${entry#*=}"
                break
            fi
        done
    done

# The ci- recipes are what .github's shared go.yml runs, one job each, in the
# toolchain stage of .devcontainer/Dockerfile: "Go / lint", "Go / test",
# "Go / vuln" and "Go / licenses" (R30 spec). The job checks out no submodule,
# so none of them reads spec/.

# The code takes spec's files from its Go modules, and the documents from the submodule spec/,
# and the two move together, to one tag (R64 spec; R27): the commit the index records for the
# submodule is the one the Go module proxy names as the origin of each module's version, and the
# contracts carry the number of spec's tag. The versions are the ones the build selects, so a
# library that requires a newer spec fails here too, and the fix is the move to that tag. The
# index is the commit's tree in CI, which checks out no submodule.
# It fails on Go code gofmt would rewrite, on what go vet reports, on a module of the binary whose
# license a release would not carry, and on a version of spec's Go modules that is not the tag of
# the spec submodule.
# Check the format of the Go code, go vet, the licenses of a release and spec's modules.
ci-lint:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(git ls-files -z --cached --others --exclude-standard -- '*.go' | xargs -0 -r gofmt -l)"
    if [ -n "$unformatted" ]; then
        echo "ci-lint: gofmt would rewrite these files; run gofmt -w on them:" >&2
        echo "$unformatted" >&2
        exit 1
    fi
    go vet ./...
    # The modules the binary is built from are the ones whose licenses a release carries (R28).
    licenses="$(mktemp -d)"
    trap 'rm -rf "$licenses"' EXIT
    {{ quote(just_executable()) }} release-licenses "$licenses"
    submodule="$(git ls-files --stage -- spec | awk '$1 == "160000" { print $2 }')"
    spec_version="$(go list -m -f '{{{{.Version}}' github.com/altnovatrail/spec)"
    contracts_version="$(go list -m -f '{{{{.Version}}' github.com/altnovatrail/spec/contracts)"
    # The origin of a version is what the Go module proxy gives with it; a proxy that gives none
    # fails the check, by name.
    origin() {
        go mod download -json "$1" | sed -n 's/^[[:space:]]*"Hash": "\([0-9a-f]\{40\}\)",\{0,1\}$/\1/p'
    }
    spec_origin="$(origin "github.com/altnovatrail/spec@${spec_version}")"
    contracts_origin="$(origin "github.com/altnovatrail/spec/contracts@${contracts_version}")"
    if [ -z "$submodule" ] || [ "$spec_origin" != "$submodule" ] || [ "$contracts_origin" != "$submodule" ] \
        || [ "$contracts_version" != "$spec_version" ]; then
        echo "ci-lint: the submodule spec/ is at ${submodule:-no commit}, while the build takes" \
            "github.com/altnovatrail/spec ${spec_version}, from ${spec_origin:-a commit the Go module proxy does not give}," \
            "and github.com/altnovatrail/spec/contracts ${contracts_version}, from" \
            "${contracts_origin:-a commit the Go module proxy does not give}; move them to one tag together" \
            "(R64 spec; R27)" >&2
        exit 1
    fi

# The profile goes to coverage/go.out, one profile with one mode line, which
# the job coverage of go.yml sends to Codecov (R48 spec). A package that no test
# runs is in it too, its statements at zero.
# Run the tests, with the race detector, and write their coverage.
ci-test:
    rm -f coverage/go.out
    mkdir -p coverage
    go test -race -count=1 -covermode=atomic -coverprofile=coverage/go.out ./...

# It fails on a known vulnerability in code the module reaches, in the standard library of its Go
# too.
# Look for known vulnerabilities with govulncheck.
ci-vuln:
    go run {{GOVULNCHECK}} ./...

# The list is ALLOWED_LICENSES, SPDX identifiers separated by commas, which
# go.yml sets from spec's list; without it, as on this machine, the recipe takes
# that list from spec itself, as its licenses-allowed prints it (R12 spec).
# Fail if a package the module is built from carries a license outside that list.
ci-licenses:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "${ALLOWED_LICENSES:-}" ]; then
        allowed=$ALLOWED_LICENSES
    elif [ -e spec/justfile ]; then
        allowed=$({{quote(just_executable())}} -f spec/justfile licenses-allowed | paste -sd , -)
    else
        echo "ci-licenses: ALLOWED_LICENSES is not set and spec/ is not checked out: run git submodule update --init" >&2
        exit 1
    fi
    go run {{GO_LICENSES}} check ./... --allowed_licenses="$allowed"

# The check of modules.json (R07, R41), which the job Modules of ci.yml runs in the toolchain
# stage, with the network, on every pull request and push: the index of the pinned release and its
# signature, fetched by the public address with no token; release-verify, which accepts only the
# shared release.yml of .github, v0.1.7 or later, at the tag's commit (R77 spec); then
# cmd/modules-check, against main's modules.json when main has one: the index by its digest, a
# release no older than main's, no current version lost but by a withdrawal, and each module's
# current version printed.
# Check modules.json: the signature of its index and the rules of R07.
ci-modules:
    #!/usr/bin/env bash
    set -euo pipefail
    base=https://github.com/altnovatrail/modules/releases/download
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT
    fetch() {
        mkdir -p "$1"
        curl --fail --silent --show-error --location --retry 3 --output "$1/module-index.json" "${base}/$2/module-index.json"
    }
    release="$(jq --exit-status --raw-output .release modules.json)"
    fetch "$work/current" "$release"
    curl --fail --silent --show-error --location --retry 3 \
        --output "$work/current/module-index.json.sigstore.json" "${base}/${release}/module-index.json.sigstore.json"
    {{ quote(just_executable()) }} -f spec/justfile release-verify modules "$release" "$work/current/module-index.json"
    # main must be there to compare with: a git that cannot say is a failure, not a main with no
    # modules.json, which would let an older release or a lost version through.
    git rev-parse --verify --quiet origin/main^{commit} > /dev/null \
        || { echo "ci-modules: origin/main is not there to compare with: fetch it" >&2; exit 1; }
    main=()
    if git cat-file -e origin/main:modules.json 2> /dev/null; then
        git show origin/main:modules.json > "$work/main.json"
        fetch "$work/main" "$(jq --exit-status --raw-output .release "$work/main.json")"
        main=(-main-config "$work/main.json" -main-index "$work/main/module-index.json")
    fi
    go run ./cmd/modules-check -config modules.json -index "$work/current/module-index.json" "${main[@]}"

# The packages of modules for the first app (R07, R41): the command the image's build runs,
# into a folder outside any git working tree, which serve then takes as API_PACKAGES_DIR. The
# folder must be empty or not there.
# Lay the packages of the pinned release of modules into a folder outside git.
packages dir="/home/vscode/.novatrail-dev/packages":
    #!/usr/bin/env bash
    set -euo pipefail
    dir={{ quote(dir) }}
    case "$dir" in
        /*) ;;
        *) echo "packages: name the folder by its absolute path, not ${dir}" >&2; exit 1 ;;
    esac
    # A folder is inside a working tree when it or a folder above it holds .git, as scripts/dev.sh
    # and internal/config tell it: by the files, so that no failure of git lets it through.
    at="$dir"
    while :; do
        if [ -e "$at/.git" ]; then
            echo "packages: ${dir} is inside a git working tree; lay the packages outside it" >&2
            exit 1
        fi
        [ "$at" = / ] && break
        at="$(dirname "$at")"
    done
    go run ./cmd/api packages "$dir"

# The configuration is a file outside any git working tree, API_DEV_CONFIG, by default
# /home/vscode/.novatrail-dev/dev.env in the volume the devcontainer mounts (R39); README.md shows
# its lines. Nothing of it is printed.
# Run a command of api on the dev projects, as `just dev migrate` or `just dev serve`.
[positional-arguments]
dev *args:
    bash scripts/dev.sh run "$@"

# The account of the dev project is a test one, dev-author, signed in by a custom token (R39).
# Go the whole way on the dev projects and check the answers and the log.
dev-check:
    bash scripts/dev.sh check
