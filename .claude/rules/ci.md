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

**CI**, `.github/workflows/ci.yml`, runs on every pull request and on `main`: the checks every NovaTrail repository shares — Secrets, DCO, Documents (`docs-check-repo`) and Go (`.claude/rules/go.md`) — from `.github`'s shared workflows pinned by SHA to a release (R27), and two jobs of its own: "Image", `.github/image.sh`, which builds the release's files twice to the same bytes, then the image a deployment builds around the release's binary and the image from the source, and checks each — its version, user, migrations, health, a file of a package it does not know, its size and a stop (R28) —, and "Modules", `.github/modules.sh ci-modules` (`.claude/rules/modules.md`). CodeQL and Scorecard run beside it, and rule 12 of RUN.md has their findings closed before the next task. "Image" needs Docker, which the devcontainer has not: a change to the `Dockerfile`, `.dockerignore` or `image.sh` is checked by that job on its pull request.

**The release** comes out by itself once CI passes on `main`, by the shared `release.yml` of `.github`: `just release-files <version>` builds static binaries for `linux/amd64` and `linux/arm64`, the `Dockerfile`, the licenses and `SHA256SUMS`, each file signed, reproducibly from `git archive` (R28). NovaTrail publishes no image (RULES.md, Production images).

**The devcontainer** is spec's container of the kind `go`, as `just -f spec/justfile new-repo-draft api go 3.5 <tag>` builds it, with two differences of `api`'s own: `libnss-wrapper` from a snapshot of the Ubuntu archive in the `toolchain` stage, for the tests of the database (R30), and the volume `novatrail-api-dev` of the dev projects' configuration (R39). A move to a newer tag of spec compares `.devcontainer/` with that draft and keeps the two (R30). A change to it takes effect after "Rebuild Container".

`scripts/dev.sh` runs `just dev` — on the stub of tutor (R66) —, `just dev-ai` — on the models — and `just dev-check` on the dev projects (R39); its test is `scripts/dev_test.go`, run by `ci-test`.
