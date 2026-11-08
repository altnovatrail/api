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

FROM golang:1.27.3-trixie@sha256:af1bfc2614ebb1dbd31af514b78740fd678850e76498762b425ecee4c5c6ea80 AS build

WORKDIR /src

# The whole context, then the build: the binary is built from the standard library and its own
# packages alone, so no layer of downloaded modules would survive a rebuild. Of spec's modules,
# which only the tests take, go fetches no more than their go.mod files.
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

# The image a deployment rolls out: the runtime around a binary of a release, which the
# deployment has checked before it gets here. The binary arrives in a build context of its own,
# named binary and holding it as api; downloaded, it has no execute bit, so it is given one here.
# A build that names no context binary fails here: BuildKit then looks for an image of that name,
# and the deployment always names the context.
FROM runtime AS release

COPY --from=binary --chmod=0555 api /api

# The image built from the source, which CI builds, starts and checks. It is the last stage, so a
# build that names no target gets it.
FROM runtime

COPY --from=build /api /api
