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

A component of [NovaTrail](https://github.com/altnovatrail/spec/tree/v0.16.3), a universal open platform for adaptive learning, interactive lessons and AI tutoring. It belongs to stage 2, First slice with learners, with tutor, canvas, app, skill-map and infra.

## Status

Detailing is done. The first epic of [RUN.md](RUN.md) decided everything the component's card left open, and [docs/architecture.md](docs/architecture.md) now describes the service; the code epics it proposes are in RUN.md. The first, E2 — the skeleton, database, sign-in and deletion — is done, and has run on dev projects of Firebase and Neon: the service serves the contract on Connect, every request with a token passes the checks before learner data, an owner with an invite code creates the account and reads it, sets the PIN of the section for adults, whose requests repeat by their idempotency keys, creates the profiles — the owner's, and a child's by a consent — changes their settings, and deletes a profile or the whole account by a fresh sign-in; a daily job finishes the deletions a request left and deletes what is past its term; the other methods answer `unimplemented` until the tasks that build them. The second, E3, builds what the first app needs — its first build with app, which the author alone runs locally, on format 0.4 of modules. The packages of modules are in the code: `modules.json` pins a release of modules, CI checks the signature of its index, the binary lays its packages into the image, and serve reads them at its start and serves the catalog, the connected modules, the open views and the files of a module from them. An attempt of a task that a tap checks is in the code too: it begins by an engine's choice or again, gets its seed, instance and open view, takes the options tapped and gives their verdict by module-kit, closes the attempt "correct" with the task solved, and gives the techniques of a step. Admission to AI is in the code too: the binary reads the deployment's facts and the least stage of the admission of models, checks them by `api check` before the migrations, and a task of handwriting begins only where tutor's table admits models to read it and the platform holds their provider's key. Its submission, the crop of a handwriting sheet, is read by tutor's two readers, joined and judged by module-kit, and kept as its observation, never as the crop; a task with a rubric, or one on a workbook, is refused "not admitted". The hint ladder is in the code too: a hint before the first wrong answer gives the task's techniques and leading question, a wrong answer gives its rung — the tutor's words once they pass the leak check and the form check, or the rung's fallback text, which the attempt keeps until it closes —, "explain differently" words the rung anew once, and the solution is shown to an adult from the start and to a child after the ladder, by an adult's action while "ask before showing" is on. tutor v0.1.24 admits Opus 5.5 for its words at the author's stage, so the learner of the first app reads the tutor's question about their work, while words that fail a check, and the words after a correct answer, which tutor has no call for yet, stay the module's fallback texts (R47, R54). Lessons are in the code too: an engine's choice of a lesson opens one run of it on the package of the current version, its task segments begin in order on that package, an answer that closes a segment's attempt names the next segment, and "lesson done" ends the run "passed"; a run whose package is withdrawn, whose lesson no longer suits the learner, or that is idle for 30 days ends by itself (R48). The learner model is in the code too: each closed attempt that gives evidence has it appended to the journal, in the order the attempts closed, and learner-model v0.1.6 folds it into the cached skill state, recomputed when the version of learner-model, the release of modules or the window of the format changes; evidence of a skill a later major version retired goes to the skills that replaced it. The skill map gives the state of a module's skills and anchor skills, with the modules not connected that teach an anchor skill, and the progress the attempts of a profile, a page at a time, with the traps that repeat in them (R49). The engine's choice is in the code too: "something else" closes the open attempt and asks engine v0.1.8 for a lesson or a task, a correct answer or the solution shown outside a lesson and the end of a lesson run carry the next choice, a skill chosen on the skill map holds until the learner asks for something else, and each choice is kept with what a replay needs; the progress names when each attempt's task was chosen and why (R50). The jobs of a rollout are in the code too: the release names `api check`, `api migrate` and `api after`, which closes "unchecked" what is open on packages the image no longer gives once the new revision takes all traffic, and the daily job closes them so before it closes the abandoned ones (R51). Spec v0.15.0 is in the code too: the legal texts go with no token, the account and every consent name the current versions of their texts, a learner from the age of digital consent of their country consents themselves while a parent below it consents by a signed form whose scan the operator checks, the age is given again when the country raises its age, the storage of works is switched on and off by whoever consents for the learner, an adult reports a problem with a reply of the tutor, and the export gives `api.zip` by spec's part `export/` (R52). The pinned release, v0.2.5, holds the packages of format 0.4: olympiad mathematics, the module of the first app, whose every lesson begins, and two small modules whose workbook and rubric tasks do not begin yet; a leading question asked for and a time of day reach the learner model by their own fields (R53). CI lays the pinned packages and walks the first app's path on them. The author has walked that path in a browser with app, locally on the dev projects: sign-in through Google, lessons, handwriting read by tutor's two models, hints, the tutor's words by a real model once tutor admitted them, and the engine's choices; a deletion of the account by a fresh sign-in went through too. The quota of platform AI is in the code too: tariffs with caps a family day, a reserve before each call of platform AI by tutor's price and its settlement after by tutor's record of usage, a new task of handwriting only with room for a typical one, the engine's pool narrowed to tasks a tap checks — and an open lesson run whose next task segment a beginning would refuse kept from the engine, open, until the segment begins again —, the flag "platform limit" and `api reopen` (R55–R59). On 2026-11-12 the run with app saw it run out on the tariff `probe` and come back the next family day, and `api tariff` raised it (T52). No child learns here before verifiable parental consent, the DPIA and the contracts with AI providers are in place.

## Build and run

Inside the devcontainer, which pins Go and just:

```sh
just build              # a static binary, bin/api
./bin/api version       # its version, commit, build date and Go
just run                # serve on PORT, 8080 by default
```

`just ci-lint`, `just ci-test`, `just ci-vuln` and `just ci-licenses` are the checks CI runs: gofmt and `go vet`, the tests with the race detector and their coverage in `coverage/go.out`, govulncheck and go-licenses. `just fuzz`, by hand, fuzzes each fuzz test of the readers of foreign input in turn, for a minute each, or `just fuzz 5m FuzzRead` for one test five minutes; an input that fails is written to `testdata/fuzz/` of its package, and `just ci-test` runs that corpus with the seeds. `just ci-modules`, which needs the network, checks `modules.json`: it verifies the signature of the pinned release's index with `release-verify`, then `cmd/modules-check` holds its digest, a release no older than main's and no current version lost but by a withdrawal, and prints each module's current version (R41). `just ci-withdrawals`, which the scheduled job Withdrawals runs once a day, verifies the signature of the newest release of modules and fails while its index withdraws a version that the pinned release gives, until `modules.json` pins a release that marks it (R61).

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
| `API_REQUEST_TIMEOUT` | `30s` | the longest a request may run, under `API_HTTP_WRITE_TIMEOUT`; a submission that AI reads ends its reading 3 s before it, and a record of its key going older than it is of a request that fell; `sweep` leaves a mark or a tuple younger than that, so a job gets the value its service has |
| `API_DATABASE_URL` | — | the pooler's connection string of the database `api`, with `sslmode=verify-full`; required by `serve`, `invite`, `tariff`, `reopen`, `countries`, `sweep` and `after` |
| `API_OPENFGA_DATABASE_URL` | — | the pooler's connection string of `openfga`, the store of the rights; required by `serve` and `sweep` |
| `API_FIREBASE_PROJECT_ID` | — | the Firebase project whose ID tokens are accepted and whose users `sweep` lists; required by `serve` and `sweep` |
| `API_FIREBASE_CREDENTIALS` | — | a service account's key, required off a deployment and refused on one, where the service's own account is taken, with no key |
| `API_APP_ORIGIN` | — | the exact origin of app, which CORS answers when api has an origin of its own; on the platform's deployment, `https://<project>.web.app`, and not the project's `firebaseapp.com` |
| `API_PACKAGES_DIR` | `/packages` | the folder `api packages` laid, which `serve`, `sweep` and `after` read at their start: the image's, or the first app's outside git |
| `API_ADMISSION_FACTS_FILE` | — | the absolute path of the deployment's file of facts for admission to AI, outside any git working tree; tutor reads it by the admission table of this release (R25, R45). Unset, no fact holds: the table closes platform AI and leaves an adult's own key open, where a model is admitted at the stage below |
| `API_ADMISSION_RUNG` | `full` | the least stage of tutor's rule of admission of models, `author` or `full`; `author` is the first app's alone, and refused when `K_SERVICE` or `CLOUD_RUN_JOB` is set (R45) |
| `API_LEGAL_DIR` | — | a self-hosted copy's folder of its operator's legal texts, outside any git working tree: `VERSION`, `legal.yaml` and `texts/` alone, read at the start by the rules of spec's part `legal/`; unset, the texts of the tag of spec (R52) |
| `API_CONSENT_FORMS_BUCKET`, `API_CONSENT_FORMS_PREFIX` | — | the operator's closed bucket of Cloud Storage the scans of parents' forms of consent go to, and api's prefix in it, ending in a slash; on a deployment alone, by the service's own account, which creates and erases objects and reads none (R116 spec) |
| `API_CONSENT_FORMS_DIR` | — | a folder outside any git working tree the scans go to instead, off a deployment; with no store, `SendConsentForm` is refused unavailable |
| `API_ANTHROPIC_KEY` | — | the platform's key of Anthropic, a secret, or `API_ANTHROPIC_KEY_FILE`, the absolute path of a file outside git that holds it: on Cloud Run a volume of a secret of Secret Manager. Unset, platform AI has no route to Anthropic, whose models alone the admission table admits, for reading handwriting and for the tutor's words; Anthropic's is the one adapter api gives tutor (R46, R54) |
| `API_OPENAI_KEY` | — | refused when set, and so is `API_OPENAI_KEY_FILE`: api gives tutor no adapter of OpenAI, and its binary holds no SDK of it (R54) |
| `K_SERVICE` | — | set by Cloud Run; a deployment is known by it |
| `CLOUD_RUN_JOB` | — | set by Cloud Run in a job; `sweep` deployed is known by it |

`api serve` reads the packages of modules in `API_PACKAGES_DIR` — the index by the digest `modules.json` pins, each package by module-kit by the chain from it — and opens the database `api`, the rights and the sign-in service before it takes its port, and fails to start when one does not hold or open. Its `startup` line names the release of modules and how many packages it read. It serves the contract `novatrail.api.v1` on Connect, in binary protobuf and in JSON with the protobuf names, reads no message larger than 11 MiB, room for the scan of a parent's form of consent — a larger one is refused `resource_exhausted` — and logs a line for each request: its method, code, cause and time, and nothing of who asked. A request that calls AI logs beside it a line `ai_usage` for each model: the provider, the model, its tokens, how the call ended and how long it took — never the work, what was read or a key (R46).

From those packages it serves the catalog of a profile and the modules connected to it, the four open views of a package by its digest — without a token, by Connect GET — and which digests the image still gives. The files of a package's device part — `LICENSE`, `NOTICE` and `media/…` — are no method of Connect: `GET /packages/sha256-<hex>/device/<path>`, without a token, answers `200` with the file — or, as `http.ServeContent` answers, `206` for a range and `304` for a request that names its `ETag` —, `404` for a path the device part does not have, `410` for a version withdrawn or out of the window, and `503` with `Retry-After: 30` for a digest the image's index does not know. A file and an open view are cached for a day, a file by its digest as its `ETag` too, and a refusal is never cached. A file's request logs its status and time, and nothing of its path.

`api migrate` applies the migrations of the database `api` and of `openfga`, the store of the rights, makes that store and writes its model when it must, and exits. It reads `API_LOG_LEVEL`, `API_MIGRATE_DATABASE_URL` and `API_MIGRATE_OPENFGA_DATABASE_URL`, the direct connection strings — not the pooler's — with `sslmode=verify-full`; `just migrate` runs it. The tests run on a real PostgreSQL that `embedded-postgres` downloads on their first run, with no Docker.

`api invite` makes one invite code of the closed slice and prints it on stdout, once: the database `api` keeps its SHA-256 alone, and the code lets one owner create an account. `-tariff` names the tariff the code passes to that account, `closed_slice` by default, or `probe`, a few cents a day for a run that sees the quota of platform AI run out. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`, and logs a failure on stderr, so that stdout holds the code alone. Run it from a terminal of the operator's own: a job's output is kept in its log, the code with it.

`api tariff -tariff <tariff> <code>` gives the account that redeemed an invite code another tariff, found by its code, so that the account's ID never leaves the platform; it prints the tariff the account has now, and a code no account redeemed, or one of an account being deleted, is refused. The caps of each tariff for a family day are the configuration of `api`, in `internal/quota`, which a pull request changes (R11). It reads `API_LOG_LEVEL` and `API_DATABASE_URL`.

`api reopen` opens platform AI again for all: a refusal of its provider by a limit on the platform's key closes it to every learner, with a `platform_limit` warning in the log, until this command, once the operator has lifted the limit at the provider (R11). It prints whether platform AI was closed, and reads `API_LOG_LEVEL` and `API_DATABASE_URL`.

`api countries list`, `api countries allow <XX>` and `api countries remove <XX>` keep the countries the deployment takes learners from, by ISO 3166-1 alpha-2. None is taken until it is allowed, and a country of the EU, the EEA or the United Kingdom is never allowed before the DPIA. It reads `API_LOG_LEVEL` and `API_DATABASE_URL`.

`api consents list`, `api consents confirm <code>` and `api consents reject <code>` are the operator's check of the parents' signed forms (R116 spec): the forms sent, by the code of each request and the object its scan lies at; a consent confirmed when its scan matches its code; or a form rejected, whose adult sends a new one. `api reports list` and `api reports review <id>` are the reading of the adults' problem reports on the tutor's replies, which the operator reviews within 3 days (R123 spec). Both read `API_LOG_LEVEL` and `API_DATABASE_URL`, and print to the operator's terminal, not to a log.

`api packages <folder>` lays the packages of the release of modules that the embedded `modules.json` pins into the folder, which must be empty or not there, and exits: it fetches the index from the release by its public address and checks its digest, then fetches each package in the window of the format as its archive, unpacks it in memory — files of the package alone, no links — and has module-kit read it by the chain of digests. The folder holds `module-index.json`, the three texts of each package under `packages/sha256-<hex>/`, and each file of a device part once under `files/sha256-<hex>`. The build of the image runs it, and `just packages` lays them for the first app into `/home/vscode/.novatrail-dev/packages`, outside git; name that folder as `API_PACKAGES_DIR` in `dev.env`. It needs the network and reads `API_LOG_LEVEL`.

`api check` checks what the deployment declares for admission to AI — the file of facts by the admission table of this release, and the stage — and serves nothing: the rollout runs it as a job of the same image before the migrations, so that a dictionary of facts this release does not know stops the rollout before anything changed (R45; the amendment of 2026-11-08 to R106 spec). It writes one line, `admission`: the version of the table, whether a file of facts was given and the stage, and whether the stage was named; a refusal is printed as the configuration's are. `api serve` reads the same at its start and refuses them as it refuses its variables, and its `startup` line says the same four things.

`api sweep` is the daily job, one pass a run: it finishes the deletions of accounts a request left marked, deletes the accounts and profiles idle for more than 12 months, closes "unchecked" the attempts and lesson runs on packages the image no longer gives, as `api after` does, closes the attempts with no action for 7 days, writes the evidence the closed attempts owe and ends the lesson runs with none for 30 days, deletes the users of the sign-in service with no account who have not been seen for 30 days, the dangling tuples, the minimal records of consents for children 5 years after their account or profile was deleted, the scans of forms past their term, the problem reports reviewed more than a year ago, the counters of the quota of platform AI a day after their family day ended, and the records of idempotency keys and the changelog of `openfga` older than a day. `-for` is how long the pass may run, `9m` by default; it stops between items after that. It reads what `serve` opens but the origin of app — the packages in `API_PACKAGES_DIR` too, of which the evidence is built — and `API_REQUEST_TIMEOUT`; it logs a line for each step with its counts and nothing it did them to, and exits 1 unless the pass was clean, which the next pass picks up. A deployment runs it on a schedule as a Cloud Run job.

`api after` is the job after a rollout, one pass a run: it closes "unchecked" each open attempt and lesson run on a package the index of the image knows but no longer gives — a version withdrawn, or out of the window — one at a time, holding the rows as a request holds them, and writes the evidence of those whose package the image holds; what is on a package the index does not know, it leaves to the image before. `-for` is how long the pass may run, `9m` by default. It reads `API_LOG_LEVEL`, `API_DATABASE_URL`, the pooler's string, and the packages in `API_PACKAGES_DIR`, and opens neither `openfga` nor the sign-in service; it logs a line for each step with its counts and exits 1 unless the pass was clean, as `sweep` does (R51).

A rollout runs these as Cloud Run jobs of the image it deploys, by the part each plays in it (the amendment of 2026-11-08 to R106 spec):

| Part | Command | When | Reads |
|---|---|---|---|
| `check` | `api check` | before the migrations | `API_ADMISSION_FACTS_FILE`, `API_ADMISSION_RUNG` |
| `migrate` | `api migrate` | before the revision takes requests | `API_MIGRATE_DATABASE_URL`, `API_MIGRATE_OPENFGA_DATABASE_URL` |
| `after` | `api after` | once the revision takes all traffic | `API_DATABASE_URL`, `API_PACKAGES_DIR` |
| by the clock | `api sweep` | daily | what `serve` opens but `API_APP_ORIGIN`, and `API_REQUEST_TIMEOUT` |

Each reads `API_LOG_LEVEL` too, and a secret by its `_FILE` twin as `serve` does.

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

The first run is on dev projects of Firebase, on the Spark plan with Google sign-in — the same project app runs locally with, where the author adds a Web App and `127.0.0.1` among its authorized domains (Д178 of spec's RUN.md) —, and of Neon, on the Free plan in `aws-us-east-1` with the databases `api` and `openfga` (R39). Their configuration lives outside the repository, in `/home/vscode/.novatrail-dev/`, a Docker volume the devcontainer mounts, which survives a rebuild: the service account's key as `key.json`, and `dev.env`, one `API_NAME=VALUE` a line, read as written and never run; no other name is taken:

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
# The platform's key of Anthropic, in a file of its own outside the repository (R46).
API_ANTHROPIC_KEY_FILE=/home/vscode/.novatrail-dev/anthropic.key
API_ADMISSION_FACTS_FILE=/home/vscode/.novatrail-dev/facts.yaml
# The origin of app on loopback, http://127.0.0.1 and app's port, whose server proxies the paths of
# api (Д177 of spec's RUN.md).
API_APP_ORIGIN=
# Where the scans of parents' forms of consent go off a deployment (R52).
API_CONSENT_FORMS_DIR=/home/vscode/.novatrail-dev/consent-forms
# The project's Web API key, which the recipe alone reads.
API_DEV_WEB_API_KEY=
```

A comment is a line of its own: whatever follows `=` is the value.

`just dev <command>` runs a command of the binary with that configuration — `just dev migrate`, `just dev invite`, `just dev serve`, `just dev sweep`. `just dev-check` goes the whole way: `api check`, migrations, the country `US`, the packages of modules into a folder of its own run, `api reopen`, the server, an ID token of the test user `dev-author` by a custom token the service account's key signs, the account by an invite code, the owner's profile and the list of profiles, its progress and "something else", a reading of handwriting with the reserve and the settlement of the quota of platform AI, which the export shows, the legal texts and a parent's consent by its form, taken back after, then `api after` on the same database, and a check that each log holds no UID, email, connection string, key or token. Its server has a key of platform AI that Anthropic refuses, not the one of the dev file: the reading goes to the provider and back, free, before any model reads it, and the reserve is kept as spent. It prints no value of the configuration and puts none on a command line; a file inside a git working tree is refused. Both recipes build `bin/api` first.

With app: `just packages` lays the pinned packages into the folder `API_PACKAGES_DIR` names. Then `just dev migrate`, `just dev invite` for the author's invite code — `just dev invite -tariff probe` for a run that sees the quota run out, and `just dev tariff -tariff closed_slice <code>` after it —, and `just dev serve`, which listens on port 8080. `hostname -i` gives this container's address on the network `novatrail-api`, and app, whose container joins that network, runs `NOVATRAIL_API=http://<address>:8080 just dev`. `deployment.local.json` of app names the dev project's Web App, whose `apiKey` is `API_DEV_WEB_API_KEY`, and the browser opens `http://127.0.0.1:5173`, the origin `API_APP_ORIGIN` names.

## Contributing

Report a bug or propose a change in an [issue](https://github.com/altnovatrail/api/issues), in English or Russian, and open one before a pull request unless the pull request fixes a typo or a broken link. How a change comes in, with every commit signed off, and what every change keeps to are in the organization's [CONTRIBUTING.md](https://github.com/altnovatrail/.github/blob/main/CONTRIBUTING.md); the rules every NovaTrail repository shares are in spec's [RULES.md](https://github.com/altnovatrail/spec/blob/v0.16.3/RULES.md): code comes with its tests, and no learner's data, not even in examples, which are synthetic. Report a vulnerability privately, as the [security policy](https://github.com/altnovatrail/api/security/policy) says, never in a public issue.

Work in the devcontainer: open the repository in VS Code and choose "Reopen in Container". It holds every tool at the version the repository pins. The checks are the recipes of the [justfile](justfile) that [Build and run](#build-and-run) names, and `just --list` names each with its arguments. A check exits with a non-zero status when it finds a problem. CI runs them all on every pull request and on `main` — all but `just ci-withdrawals`, which the job Withdrawals runs once a day —, builds and checks the image, and a pull request merges only when they pass and its changed lines are at least 90% covered. Documents are checked by `just -f spec/justfile docs-check-repo`. The answers of the project to the criteria of OpenSSF Best Practices are in [.bestpractices.json](.bestpractices.json).

## Documents

The documents are in English; the work plan, its technical debt and the archives of its epics stay in Russian.

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links, what was decided and how the service is built.
- [docs/sources.md](docs/sources.md) — the external facts the documents rest on: each source with its address, the date it was read and a quote.
- [docs/measurements.md](docs/measurements.md) — the results of measurements: what was measured, how, and the numbers.
- [docs/decisions.md](docs/decisions.md) — the decision log: what was decided, why, and what was rejected.
- [RUN.md](RUN.md) — the work plan, in epics.
- [DEBT.md](DEBT.md) — the technical debt, in Russian: table 4 of the work plan.

The working rules, for people and coding agents alike, are in [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec/tree/v0.16.3), checked out here as a git submodule at `v0.16.3`: clone this repository with `git clone --recurse-submodules`.

## License

[Apache-2.0](LICENSE).
