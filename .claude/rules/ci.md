---
paths:
  - ".github/**"
  - ".devcontainer/**"
  - "justfile"
  - "Dockerfile"
  - ".dockerignore"
  - "scripts/**"
---

# CI, the release and the devcontainer

**CI**, `.github/workflows/ci.yml`, runs on every pull request and on `main`: the checks every NovaTrail repository shares — Secrets, DCO, Documents (`docs-check-repo`) and Go (`.claude/rules/go.md`) — from `.github`'s shared workflows pinned by SHA to a release in the `uses:` lines of `.github/workflows/`, which Dependabot raises as one group, `shared-workflows`, and whose "Go" and "Documents" take the image of a stage from the cache of GitHub Actions by the hash of `.devcontainer/Dockerfile` — `toolchain` for "Go / test", `toolchain-core` for the rest (R47 .github) —, and two jobs of its own, which take an image of the same entries, read only, by `.github/toolchain.sh` and the stage `TOOLCHAIN_STAGE` names (R27): "Image", `.github/image.sh`, in `toolchain-core`, which builds the release's files, then the image a deployment builds around the release's binary and the image from the source, holds the binary of the second to the release's `linux/amd64` one byte for byte, and checks each image — its version, user, migrations, health, a file of a package it does not know, its size and a stop (R28) —, and "Modules", `.github/modules.sh ci-modules`, in `toolchain`, whose test starts PostgreSQL (`.claude/rules/modules.md`). CodeQL and Scorecard run beside it, and rule 12 of RUN.md has their findings closed before the next task. "Image" needs Docker, which the devcontainer has not: a change to the `Dockerfile`, `.dockerignore`, `image.sh` or `toolchain.sh` is checked by those jobs on its pull request.

**The release** comes out by itself once CI passes on `main`, by the shared `release.yml` of `.github`, in the stage `toolchain-core`: `just release-files <version>` builds static binaries for `linux/amd64` and `linux/arm64`, the `Dockerfile`, the licenses and `SHA256SUMS`, each file signed, reproducibly from `git archive` (R28). NovaTrail publishes no image (RULES.md, Production images).

**The devcontainer** is spec's container of the kind `go`, as `just -f spec/justfile new-repo-draft api go 3.5 <tag>` builds it, with three differences of `api`'s own: `libnss-wrapper` from a source of a snapshot of the Ubuntu archive alone in the `toolchain` stage, over `toolchain-core`, which holds nothing of `api`'s own, for the tests of the database (R30), the volume `novatrail-api-dev` of the dev projects' configuration (R39), and the volume `novatrail-dev-token`, shared with app's container, of the custom token of the local build of app (R67). A move to a newer tag of spec compares `.devcontainer/` with that draft and keeps the three. A change to it takes effect after "Rebuild Container".

`scripts/dev.sh` runs `just dev` — on the stub of tutor (R66) —, `just dev-ai` — on the models —, `just dev-check` on the dev projects and `just dev-app`, which keeps api up for the local build of app with a custom token in that shared volume; its test is `scripts/dev_test.go`, run by `ci-test`.
