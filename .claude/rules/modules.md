---
paths:
  - "modules.json"
  - "modules.go"
  - "modules_test.go"
  - "cmd/modules-check/**"
  - "internal/packages/**"
  - ".github/modules.sh"
  - ".github/workflows/withdrawals.yml"
---

# The packages of modules: `modules.json`

`modules.json` pins the release of `modules` whose packages the image holds, and the digest of its index (R07, R41, R53). `just ci-modules`, which needs the network, is the job "Modules" of CI: it verifies the signature of the pinned release's index with `just -f spec/justfile release-verify`, then `cmd/modules-check` holds its digest, a release no older than main's and no current version lost but by a withdrawal (R07, R41); it lays the packages by `api packages` and runs the test of the pinned release, `TestThePinnedReleaseIsLearnedOn` of `internal/learning`, on them (R53); and it checks the `.proto` files of `learner-model` the export carries (R52). A task that touches `modules.json`, the reading or the laying of packages, or `cmd/modules-check` ends with it green.

`just ci-withdrawals` is the daily job "Withdrawals": it verifies the signature of the newest release of `modules` and fails while its index withdraws a version that the pinned release gives, until `modules.json` pins a release that marks it (R61). A task that touches it runs it once by hand.
