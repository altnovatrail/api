# The image of api in production (R105, R106 in spec; R28 in docs/decisions.md). NovaTrail publishes
# no image: a release of api carries its signed binaries and this file, and whoever deploys builds
# the image around the binary that release-verify accepted, by --target release:
#
#   docker build --target release --build-context binary=<a folder holding the binary as api> .
#
# Exact versions only. The builder is pinned by its exact tag and its digest, with the Go go.mod
# names and the tests run on; distroless names no versions in its tags, so the runtime is pinned
# by its digest and the tag names the variant taken. Both digests were read from their registries
# on 2026-11-08, and Dependabot raises them.

FROM golang:1.27.4-trixie@sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9 AS build

WORKDIR /src

# The whole context, then the build: modules.json, which the binary embeds, comes with it (R41).
# The modules the binary is built from are fetched by the build itself, as go.sum pins them.
COPY . .

# The build's identity comes in as arguments rather than being read from git: the context has no
# .git, and a version the image guesses is worse than one it is told (internal/version).
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Static, with no paths of the machine that built it and no VCS stamp, as release-files builds it.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w \
      -X github.com/altnovatrail/api/internal/version.Version=${VERSION} \
      -X github.com/altnovatrail/api/internal/version.Commit=${COMMIT} \
      -X github.com/altnovatrail/api/internal/version.Date=${DATE}" \
    -o /api ./cmd/api

# The runtime holds the binary and nothing of its own beyond certificates, tzdata and /tmp: no
# shell, no package manager, and the user nonroot, uid 65532. Every job of the operator is a
# command of the same binary, so the image serves by default and runs any other command named.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS runtime

EXPOSE 8080
ENTRYPOINT ["/api"]
CMD ["serve"]

# The packages of modules the image holds (R07, R41 in docs/decisions.md): the binary lays them
# itself, by its command packages, from the release of modules that the modules.json embedded in
# it pins — the index by its digest, each package by module-kit by the chain from it — so the
# build needs the network, and no file of the main context. The runtime has no shell, so the RUN
# is of the exec form; it runs as nonroot, who may write /tmp alone.
FROM runtime AS release-packages

COPY --from=binary --chmod=0555 api /api
RUN ["/api", "packages", "/tmp/packages"]

# The image a deployment rolls out: the runtime around a binary of a release, which the
# deployment has checked before it gets here, and the packages that binary laid, owned by root so
# that the service, nonroot, cannot change them. The binary arrives in a build context of its own,
# named binary and holding it as api; downloaded, it has no execute bit, so it is given one here.
# A build that names no context binary fails here: BuildKit then looks for an image of that name,
# and the deployment always names the context.
FROM runtime AS release

COPY --from=binary --chmod=0555 api /api
COPY --from=release-packages --chown=0:0 /tmp/packages /packages

# The packages of the image built from the source, laid as the release's are.
FROM runtime AS source-packages

COPY --from=build /api /api
RUN ["/api", "packages", "/tmp/packages"]

# The image built from the source, which CI builds, starts and checks. It is the last stage, so a
# build that names no target gets it.
FROM runtime

COPY --from=build /api /api
COPY --from=source-packages --chown=0:0 /tmp/packages /packages
