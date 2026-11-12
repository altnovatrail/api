---
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
  - "internal/**"
  - "cmd/**"
---

# The Go code of `api`

`just ci-lint`, `just ci-test`, `just ci-vuln` and `just ci-licenses` are the checks CI runs on it, as "Go / lint", "Go / test", "Go / vuln" and "Go / licenses" of the shared `go.yml` of `.github` (R27): gofmt and `go vet`, the licenses a release carries for every module the binary is built from (R28), and spec's two Go modules at the commit of the `spec` submodule in the index — after a move of the submodule, `git add spec` before `ci-lint`; the tests with the race detector, on a real PostgreSQL that `embedded-postgres` downloads on their first run, with the migrations of `internal/store/migrations/` applied, and their coverage in `coverage/go.out`; govulncheck, the standard library of its Go too; go-licenses, against spec's list of licenses (R12 spec). A task that touches Go code ends with the four green. Code comes with its tests, and a merge waits for `codecov/patch`: at least 90% of the changed lines covered (R48 spec; `.github/codecov.yml`).

A move of a library in `go.mod` — `tutor`, `engine`, `learner-model`, `module-kit` or spec's modules — runs `just ci-modules` too, whose test of the pinned release learns on its packages (`.claude/rules/modules.md`); a move of `learner-model` lays the `.proto` files the export carries again by `just learner-model-proto` (R52). `just dev-check` goes the whole way on the dev projects, with the configuration outside the repository and no paid call of AI (R39, and its 2026-11-12 amendment); the moves to a new tag of spec or of a library have run it too, as T53, T54 and T61 did.
