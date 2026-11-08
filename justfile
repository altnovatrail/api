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
# Build the binary into bin/api: static, without cgo and without the paths of
# this machine, as a production image needs it (RULES.md, Production images).
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

# The ci- recipes are what .github's shared go.yml runs, one job each, in the
# toolchain stage of .devcontainer/Dockerfile: "Go / lint", "Go / test",
# "Go / vuln" and "Go / licenses" (R30 spec). The job checks out no submodule,
# so none of them reads spec/.

# Fail on Go code gofmt would rewrite, and on what go vet reports.
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

# The profile goes to coverage/go.out, one profile with one mode line, which
# the job coverage of go.yml sends to Codecov (R48 spec). A package that no test
# runs is in it too, its statements at zero.
# Run the tests, with the race detector, and write their coverage.
ci-test:
    rm -f coverage/go.out
    mkdir -p coverage
    go test -race -count=1 -covermode=atomic -coverprofile=coverage/go.out ./...

# Fail on a known vulnerability in code the module reaches, in the standard
# library of its Go too.
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
