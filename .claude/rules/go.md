---
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
  - "internal/**"
  - "cmd/**"
---

# The Go code of `api`

`just ci-lint`, `just ci-test`, `just ci-vuln` and `just ci-licenses` are the checks CI runs on it, as "Go / lint", "Go / test", "Go / vuln" and "Go / licenses" of the shared `go.yml` of `.github`: gofmt and `go vet`, the licenses a release carries for every module the binary is built from, and spec's two Go modules at the commit of the `spec` submodule in the index — after a move of the submodule, `git add spec` before `ci-lint`; the tests with the race detector, on a real PostgreSQL that `embedded-postgres` downloads on their first run, with the migrations of `internal/store/migrations/` applied, and their coverage in `coverage/go.out`; govulncheck, the standard library of its Go too; go-licenses, against spec's list of licenses, those of the ASF's category A. A task that touches Go code ends with the four green. Code comes with its tests, and a merge waits for `codecov/patch`: at least 90% of the changed lines covered, by `.github/codecov.yml`.

A move of a library in `go.mod` — `tutor`, `engine`, `learner-model`, `module-kit` or spec's modules — runs `just ci-modules` too, whose test of the pinned release learns on its packages (`.claude/rules/modules.md`); a move of `learner-model` lays the `.proto` files the export carries again by `just learner-model-proto`. `just dev-check` goes the whole way on the dev projects, with the configuration outside the repository and no paid call of AI; a move to a new tag of spec or of a library runs it too. `just dev` runs on the stub of tutor, which calls no provider; `just dev-ai` runs on the models, only for a task that checks the words of a model, since its calls of a provider are paid.

The readers of foreign input have fuzz tests — `func Fuzz…` in `internal/invite`, `internal/packages`, `internal/learning` and `internal/server`; `docs/decisions.md` lists the readers given none, and why. `ci-test` runs their seeds, `f.Add` and `testdata/fuzz/`. A task that changes such a reader, or adds one, runs `just fuzz 5m <test>` for each of its fuzz tests and names the time in its pull request, or in its report where the author commits after review; a new reader of foreign input comes with a fuzz test. An input that fails is written to `testdata/fuzz/` and is committed with the fix.
