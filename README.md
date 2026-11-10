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

A component of [NovaTrail](https://github.com/altnovatrail/spec/tree/v0.12.0), a universal open platform for adaptive learning, interactive lessons and AI tutoring. It belongs to stage 2, First slice with learners, with tutor, canvas, app, skill-map and infra.

## Status

Detailing is done. The first epic of [RUN.md](RUN.md) decided everything the component's card left open, and [docs/architecture.md](docs/architecture.md) now describes the service; the code epics it proposes are in RUN.md. The first, E2 — the skeleton, database, sign-in and deletion — is done, and has run on dev projects of Firebase and Neon: the service serves the contract on Connect, every request with a token passes the checks before learner data, an owner with an invite code creates the account and reads it, sets the PIN of the section for adults, whose requests repeat by their idempotency keys, creates the profiles — the owner's, and a child's by a consent — changes their settings, and deletes a profile or the whole account by a fresh sign-in; a daily job finishes the deletions a request left and deletes what is past its term; the other methods answer `unimplemented` until the tasks that build them. The second, E3, builds what the first app needs — its first build with app, which the author alone runs locally, on format 0.4 of modules. The packages of modules are in the code: `modules.json` pins a release of modules, CI checks the signature of its index, the binary lays its packages into the image, and serve reads them at its start and serves the catalog, the connected modules, the open views and the files of a module from them. An attempt of a task that a tap checks is in the code too: it begins by an engine's choice or again, gets its seed, instance and open view, takes the options tapped and gives their verdict by module-kit, closes the attempt "correct" with the task solved, and gives the techniques of a step. Admission to AI is in the code too: the binary reads the deployment's facts and the least stage of the admission of models, checks them by `api check` before the migrations, and a task of handwriting begins only where tutor's table admits models to read it, and then waits for tutor's reading; a task with a rubric is refused "not admitted". The pinned release, v0.1.4, holds no package of format 0.4 yet, so no module has a current version until modules moves to 0.4. No child learns here before verifiable parental consent, the DPIA and the contracts with AI providers are in place.

## Build and run

Inside the devcontainer, which pins Go and just:

```sh
just build              # a static binary, bin/api
./bin/api version       # its version, commit, build date and Go
just run                # serve on PORT, 8080 by default
```

`just ci-lint`, `just ci-test`, `just ci-vuln` and `just ci-licenses` are the checks CI runs: gofmt and `go vet`, the tests with the race detector and their coverage in `coverage/go.out`, govulncheck and go-licenses. `just ci-modules`, which needs the network, checks `modules.json`: it verifies the signature of the pinned release's index with `release-verify`, then `cmd/modules-check` holds its digest, a release no older than main's and no current version lost but by a withdrawal, and prints each module's current version (R41).

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
| `API_APP_ORIGIN` | — | the exact origin of app, which CORS answers when api has an origin of its own; on the platform's deployment, `https://<project>.web.app`, and not the project's `firebaseapp.com` |
| `API_PACKAGES_DIR` | `/packages` | the folder `api packages` laid, which `serve` reads at its start: the image's, or the first app's outside git |
| `API_ADMISSION_FACTS_FILE` | — | the absolute path of the deployment's file of facts for admission to AI, outside any git working tree; tutor reads it by the admission table of this release (R25, R45). Unset, no fact holds: the table closes platform AI and leaves an adult's own key open, where a model is admitted at the stage below |
| `API_ADMISSION_RUNG` | `full` | the least stage of tutor's rule of admission of models, `author` or `full`; `author` is the first app's alone, and refused when `K_SERVICE` or `CLOUD_RUN_JOB` is set (R45) |
| `K_SERVICE` | — | set by Cloud Run; a deployment is known by it |
| `CLOUD_RUN_JOB` | — | set by Cloud Run in a job; `sweep` deployed is known by it |

`api serve` reads the packages of modules in `API_PACKAGES_DIR` — the index by the digest `modules.json` pins, each package by module-kit by the chain from it — and opens the database `api`, the rights and the sign-in service before it takes its port, and fails to start when one does not hold or open. Its `startup` line names the release of modules and how many packages it read. It serves the contract `novatrail.api.v1` on Connect, in binary protobuf and in JSON with the protobuf names, and logs a line for each request: its method, code, cause and time, and nothing of who asked.

From those packages it serves the catalog of a profile and the modules connected to it, the four open views of a package by its digest — without a token, by Connect GET — and which digests the image still gives. The files of a package's device part — `LICENSE`, `NOTICE` and `media/…` — are no method of Connect: `GET /packages/sha256-<hex>/device/<path>`, without a token, answers `200` with the file, `404` for a path the device part does not have, `410` for a version withdrawn or out of the window, and `503` with `Retry-After: 30` for a digest the image's index does not know. A file and an open view are cached for a day, a file by its digest as its `ETag` too, and a refusal is never cached. A file's request logs its status and time, and nothing of its path.

`api migrate` applies the migrations of the database `api` and of `openfga`, the store of the rights, makes that store and writes its model when it must, and exits. It reads `API_LOG_LEVEL`, `API_MIGRATE_DATABASE_URL` and `API_MIGRATE_OPENFGA_DATABASE_URL`, the direct connection strings — not the pooler's — with `sslmode=verify-full`; `just migrate` runs it. The tests run on a real PostgreSQL that `embedded-postgres` downloads on their first run, with no Docker.

`api invite` makes one invite code of the closed slice and prints it on stdout, once: the database `api` keeps its SHA-256 alone, and the code lets one owner create an account. `-tariff` names the tariff the code passes to that account, `closed_slice` by default and the only one so far. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`, and logs a failure on stderr, so that stdout holds the code alone. Run it from a terminal of the operator's own: a job's output is kept in its log, the code with it.

`api countries list`, `api countries allow <XX>` and `api countries remove <XX>` keep the countries the deployment takes learners from, by ISO 3166-1 alpha-2. None is taken until it is allowed, and a country of the EU, the EEA or the United Kingdom is never allowed before the DPIA. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`.

`api packages <folder>` lays the packages of the release of modules that the embedded `modules.json` pins into the folder, which must be empty or not there, and exits: it fetches the index from the release by its public address and checks its digest, then fetches each package in the window of the format as its archive, unpacks it in memory — files of the package alone, no links — and has module-kit read it by the chain of digests. The folder holds `module-index.json`, the three texts of each package under `packages/sha256-<hex>/`, and each file of a device part once under `files/sha256-<hex>`. The build of the image runs it, and `just packages` lays them for the first app into `/home/vscode/.novatrail-dev/packages`, outside git; name that folder as `API_PACKAGES_DIR` in `dev.env`. It needs the network and reads `API_LOG_LEVEL`.

`api check` checks what the deployment declares for admission to AI — the file of facts by the admission table of this release, and the stage — and serves nothing: the rollout runs it as a job of the same image before the migrations, so that a dictionary of facts this release does not know stops the rollout before anything changed (R45; the amendment of 2026-11-08 to R106 spec). It writes one line, `admission`: the version of the table, whether a file of facts was given and the stage, and whether the stage was named; a refusal is printed as the configuration's are. `api serve` reads the same at its start and refuses them as it refuses its variables, and its `startup` line says the same four things.

`api sweep` is the daily job, one pass a run: it finishes the deletions of accounts a request left marked, deletes the accounts and profiles idle for more than 12 months, closes the attempts with no action for 7 days and ends the lesson runs with none for 30 days, deletes the users of the sign-in service with no account who have not been seen for 30 days, the dangling tuples, the minimal records of consents for children 5 years after their account or profile was deleted, and the records of idempotency keys and the changelog of `openfga` older than a day. `-for` is how long the pass may run, `9m` by default; it stops between items after that. It reads what `serve` opens, and `API_REQUEST_TIMEOUT`; it logs a line for each step with its counts and nothing it did them to, and exits 1 unless the pass was clean, which the next pass picks up. A deployment runs it on a schedule as a Cloud Run job.

### Sign-in in production

The Firebase project of the platform runs Firebase Authentication with Identity Platform, which `infra` sets up; api's own dev project stays on Firebase Auth without it (R03). What api expects of the project: sign-in by the Google window alone, ID tokens that carry `auth_time`, and users deleted by api alone. api reads no email. It makes these calls of the Identity Toolkit API, by the Admin SDK for Go v4.22.0, and no other:

| Call | What for | Permission |
|---|---|---|
| `accounts:lookup` | the revocation check of every ID token | `firebaseauth.users.get` |
| `accounts:batchGet` | the list of users that `sweep` reads | `firebaseauth.users.get` |
| `accounts:update` | the revocation of a user's refresh tokens, before the user is deleted | `firebaseauth.users.update` |
| `accounts:delete` | the deletion of the user | `firebaseauth.users.delete` |

They are the permissions of the role `signInUsers` that `infra` gives api's account. A release that makes a new call names it here, and spec hears of it first, so that the role grows before the release is rolled out (architecture 3.11 spec).

A secret — a key, a token, a connection string — comes from its variable or from a file outside the repository whose absolute path the variable with the suffix `_FILE` names; a file inside a git working tree is refused. The log is JSON on stdout, and writes only the events and fields of its table, `internal/logging/events.go`: no identifier, address or content of a learner's work.

## Run on the dev projects

The first run is on dev projects of Firebase, on the Spark plan with Google sign-in, and of Neon, on the Free plan in `aws-us-east-1` with the databases `api` and `openfga` (R39). Their configuration lives outside the repository, in `/home/vscode/.novatrail-dev/`, a Docker volume the devcontainer mounts, which survives a rebuild: the service account's key as `key.json`, and `dev.env`, one `API_NAME=VALUE` a line, read as written and never run; no other name is taken:

```sh
# The pooler's strings, then the direct ones, each with sslmode=verify-full.
API_DATABASE_URL=
API_OPENFGA_DATABASE_URL=
API_MIGRATE_DATABASE_URL=
API_MIGRATE_OPENFGA_DATABASE_URL=
API_FIREBASE_PROJECT_ID=
API_FIREBASE_CREDENTIALS_FILE=/home/vscode/.novatrail-dev/key.json
# The folder just packages lays the packages of modules into.
API_PACKAGES_DIR=/home/vscode/.novatrail-dev/packages
# Admission to AI: the first app's stage, and its file of facts, if it declares any (R25, R45).
API_ADMISSION_RUNG=author
API_ADMISSION_FACTS_FILE=/home/vscode/.novatrail-dev/facts.yaml
# The project's Web API key, which the recipe alone reads.
API_DEV_WEB_API_KEY=
```

A comment is a line of its own: whatever follows `=` is the value.

`just dev <command>` runs a command of the binary with that configuration — `just dev migrate`, `just dev invite`, `just dev serve`, `just dev sweep`. `just dev-check` goes the whole way: `api check`, migrations, the country `US`, the packages of modules into a folder of its own run, the server, an ID token of the test user `dev-author` by a custom token the service account's key signs, the account by an invite code, the owner's profile and the list of profiles, then a check that the log holds no UID, email, connection string or token. It prints no value of the configuration and puts none on a command line; a file inside a git working tree is refused. Both recipes build `bin/api` first.

## Documents

The documents are in Russian.

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links, what was decided and how the service is built.
- [docs/decisions.md](docs/decisions.md) — the decision log: what was decided, why, and what was rejected.
- [RUN.md](RUN.md) — the work plan, in epics.

The working rules, for people and coding agents alike, are in [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec/tree/v0.12.0), checked out here as a git submodule at `v0.12.0`: clone this repository with `git clone --recurse-submodules`.

## License

[Apache-2.0](LICENSE).
