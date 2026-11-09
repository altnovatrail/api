# api

[![CI](https://github.com/altnovatrail/api/actions/workflows/ci.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/ci.yml)
[![Release](https://github.com/altnovatrail/api/actions/workflows/release.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/release.yml)
[![CodeQL](https://github.com/altnovatrail/api/actions/workflows/codeql.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/codeql.yml)
[![Latest release](https://img.shields.io/github/v/release/altnovatrail/api)](https://github.com/altnovatrail/api/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/altnovatrail/api)](go.mod)
[![Coverage](https://codecov.io/gh/altnovatrail/api/branch/main/graph/badge.svg)](https://codecov.io/gh/altnovatrail/api)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/altnovatrail/api/badge)](https://scorecard.dev/viewer/?uri=github.com/altnovatrail/api)
[![License](https://img.shields.io/github/license/altnovatrail/api)](LICENSE)

[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)

The NovaTrail platform service, the only service of the first slice. It checks the account owner's sign-in and keeps the learners, their consent and their attempts in its database — among the platform's parts, the one place where learners' data is stored on the server, and the learner model lives there too, as an append-only log of evidence. It runs each check — tutor reads handwritten work and module-kit gives the verdict, while a choice tapped among options is checked without AI — then has learner-model weigh the attempt's evidence and engine pick the next lesson or task with a reason code: all four are libraries in its own process, and it reaches AI providers only through tutor, without the learner's identifiers.

A component of [NovaTrail](https://github.com/altnovatrail/spec/tree/v0.9.11), a universal open platform for adaptive learning, interactive lessons and AI tutoring. It belongs to stage 2, First slice with learners, with tutor, canvas, app, skill-map and infra.

## Status

Detailing is done. The first epic of [RUN.md](RUN.md) decided everything the component's card left open, and [docs/architecture.md](docs/architecture.md) now describes the service; the code epics it proposes are in RUN.md. The first, E2 — the skeleton, database, sign-in and deletion — has begun: the service serves the contract on Connect, every request with a token passes the checks before learner data, an owner with an invite code creates the account and reads it, sets the PIN of the section for adults, whose requests repeat by their idempotency keys, creates the profiles — the owner's, and a child's by a consent — changes their settings, and deletes a profile or the whole account by a fresh sign-in; a daily job finishes the deletions a request left and deletes what is past its term; the other methods answer `unimplemented` until the tasks that build them. Its first build with app, the first app, which the author alone runs locally, waits for this repository's skeleton, app's code and tutor's first code: the module format's code and content, the learner model and engine have released, and spec's contract between app and api is already out. E2 needs none of these, and spec's cross-repository plan lets it run now (RUN.md, table 1). No child learns here before verifiable parental consent and the contracts with AI providers are in place.

## Build and run

Inside the devcontainer, which pins Go and just:

```sh
just build              # a static binary, bin/api
./bin/api version       # its version, commit, build date and Go
just run                # serve on PORT, 8080 by default
```

`just ci-lint`, `just ci-test`, `just ci-vuln` and `just ci-licenses` are the checks CI runs: gofmt and `go vet`, the tests with the race detector and their coverage in `coverage/go.out`, govulncheck and go-licenses.

A release comes out by itself once CI passes on `main`, from `just release-files`: static binaries for `linux/amd64` and `linux/arm64`, the `Dockerfile`, the licenses the binaries come with and `SHA256SUMS`, each file signed. NovaTrail publishes no image. Whoever deploys checks the files with `just -f spec/justfile release-verify api <tag> <file>` and builds the image around the binary: `docker build --target release --build-context binary=<a folder holding the binary as api> .`, with the release's `Dockerfile`.

The binary reads its configuration from the environment, and only these variables:

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | the port to listen on; `0`, off a deployment, asks the kernel for a free one, which the `startup` line names |
| `API_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `API_HTTP_READ_HEADER_TIMEOUT` | `5s` | how long a client may take to send its headers |
| `API_HTTP_READ_TIMEOUT` | `30s` | how long a client may take to send its whole request |
| `API_HTTP_WRITE_TIMEOUT` | `60s` | how long the server may take to answer |
| `API_HTTP_IDLE_TIMEOUT` | `120s` | how long an idle connection is kept |
| `API_SHUTDOWN_TIMEOUT` | `9s` | the whole way out after a stop is asked for; under `10s` when deployed |
| `API_REQUEST_TIMEOUT` | `30s` | the longest a request may run, under `API_HTTP_WRITE_TIMEOUT`; `sweep` leaves a mark or a tuple younger than that, so a job gets the value its service has |
| `API_DATABASE_URL` | — | the pooler's connection string of the database `api`, with `sslmode=verify-full`; required by `serve`, `invite`, `countries` and `sweep` |
| `API_OPENFGA_DATABASE_URL` | — | the pooler's connection string of `openfga`, the store of the rights; required by `serve` and `sweep` |
| `API_FIREBASE_PROJECT_ID` | — | the Firebase project whose ID tokens are accepted and whose users `sweep` lists; required by `serve` and `sweep` |
| `API_FIREBASE_CREDENTIALS` | — | a service account's key, required off a deployment and refused on one, where the service's own account is taken, with no key |
| `API_APP_ORIGIN` | — | the exact origin of app, which CORS answers when api has an origin of its own |
| `K_SERVICE` | — | set by Cloud Run; a deployment is known by it |
| `CLOUD_RUN_JOB` | — | set by Cloud Run in a job; `sweep` deployed is known by it |

`api serve` opens the database `api`, the rights and the sign-in service before it takes its port, and fails to start when one does not open. It serves the contract `novatrail.api.v1` on Connect, in binary protobuf and in JSON with the protobuf names, and logs a line for each request: its method, code, cause and time, and nothing of who asked.

`api migrate` applies the migrations of the database `api` and of `openfga`, the store of the rights, makes that store and writes its model when it must, and exits. It reads `API_LOG_LEVEL`, `API_MIGRATE_DATABASE_URL` and `API_MIGRATE_OPENFGA_DATABASE_URL`, the direct connection strings — not the pooler's — with `sslmode=verify-full`; `just migrate` runs it. The tests run on a real PostgreSQL that `embedded-postgres` downloads on their first run, with no Docker.

`api invite` makes one invite code of the closed slice and prints it on stdout, once: the database `api` keeps its SHA-256 alone, and the code lets one owner create an account. `-tariff` names the tariff the code passes to that account, `closed_slice` by default and the only one so far. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`, and logs a failure on stderr, so that stdout holds the code alone. Run it from a terminal of the operator's own: a job's output is kept in its log, the code with it.

`api countries list`, `api countries allow <XX>` and `api countries remove <XX>` keep the countries the deployment takes learners from, by ISO 3166-1 alpha-2. None is taken until it is allowed, and a country of the EU, the EEA or the United Kingdom is never allowed before the DPIA. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`.

`api sweep` is the daily job, one pass a run: it finishes the deletions of accounts a request left marked, deletes the accounts and profiles idle for more than 12 months, the users of the sign-in service with no account who have not been seen for 30 days, the dangling tuples, and the records of idempotency keys and the changelog of `openfga` older than a day. `-for` is how long the pass may run, `9m` by default; it stops between items after that. It reads what `serve` opens, and `API_REQUEST_TIMEOUT`; it logs a line for each step with its counts and nothing it did them to, and exits 1 unless the pass was clean, which the next pass picks up. A deployment runs it on a schedule as a Cloud Run job.

A secret — a key, a token, a connection string — comes from its variable or from a file outside the repository whose absolute path the variable with the suffix `_FILE` names; a file inside a git working tree is refused. The log is JSON on stdout, and writes only the events and fields of its table, `internal/logging/events.go`: no identifier, address or content of a learner's work.

## Documents

The documents are in Russian.

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links, what was decided and how the service is built.
- [docs/decisions.md](docs/decisions.md) — the decision log: what was decided, why, and what was rejected.
- [RUN.md](RUN.md) — the work plan, in epics.

The working rules, for people and coding agents alike, are in [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec/tree/v0.9.11), checked out here as a git submodule at `v0.9.11`: clone this repository with `git clone --recurse-submodules`.

## License

[Apache-2.0](LICENSE).
