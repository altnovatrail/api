# api — sources

> The external facts the documents of `docs/` rest on: a provider's terms, a price, a standard, the documentation of a library or a service, and the texts of other NovaTrail and MathTrail repositories at named commits. Their records lie here — the address, the version or date of the page, the date read and a quote in the source's language — and [architecture.md](architecture.md) holds the claim and a reference to the record by its number, as to a section: `sources 2.1` (RULES.md, The bar; R143 spec).
>
> Until T62, 2026-11-13, the records stood in architecture 4, and a pointer remained there: the decision log and the archives of `docs/plan/` cite that section 4, and they are not rewritten. The text of the records was moved as it was. The heading became the first line of the reading — its name and date —, its parenthesis with sections and decisions became the first words of the record, "For …", and the references to this repository's sections, which in architecture 4 read as `раздел 3.2`, are here `architecture 3.2`. The two measurements of the binary's size that stood in the readings for R31 and R32 went to [measurements.md](measurements.md).

## 1. How to keep it

- **A record** is a subsection "N.M" of a topic: one reading of one or several sources for the sake of one decision. It holds the address, the version or date of the page, the date read and a quote in the source's language. What was not read in the primary source — a copy, a retelling, a page that did not open, a conclusion from code, a trial by a build — the record names as such.
- **Topics**, in the move, took the order of the sections of architecture 3.1–3.10, in order — topics 2–11 —, and topic 12 is a reading that several sections rest on at once. A new reading for a section that already has a topic is a new record at its end; a topic for another section or for several at once goes at the end of the file, after topic 12, and the order of the sections is broken by this, but the numbers are not.
- **Reference.** A document's claim refers to a record by its number, as to a section of any document: `(sources 7.1)`, and from another repository — `sources 7.1 api`. A record's number does not change: a new record goes at the end of its topic, a new topic at the end of the file.
- **A source read again** — an item with its own date is added to the record, and the earlier text is not edited: the decisions of its day rest on it. If the source has changed and the claim has diverged from it, that is an amendment to the decision or a question to the author, not an edit of the record.
- **What is not here:** quotes that an entry of the decision log gives itself — they stay in the entry (RULES.md, How we work: a decision is not rewritten); the results of measurements — they are in [measurements.md](measurements.md).

## 2. Sign-in and permissions

For architecture 3.1: the sign-in service, its Admin SDK and ID token, permissions and OpenFGA.

### 2.1. Firebase Auth, 2026-11-02

For architecture 3.1; R03. Read 2026-11-02:

- [privacy in Firebase](https://firebase.google.com/support/privacy), updated 2026-10-29: "The Firebase Authentication service is run only from US data centers. As a result, Firebase Authentication processes data exclusively in the United States"; "Firebase Authentication keeps logged IP addresses for a few weeks"; after a user is deleted "data is removed from live and backup systems within 180 days"; among the data it processes — "User agents" and "IP addresses";
- [Firebase Authentication](https://firebase.google.com/docs/auth), updated 2026-10-20: "Firebase Authentication with Identity Platform is an optional upgrade that adds several new features to Firebase Authentication"; "When upgraded, no-cost (Spark) plan projects will be limited to 3,000 daily active users, and pay-as-you-go (Blaze) plan projects will be charged for usage beyond the free tier of 50,000 monthly active users";
- [Firebase pricing](https://firebase.google.com/pricing), no date on the page: the row "Other Authentication services" is marked for both the Spark plan and Blaze, with no price. The rows of active users stand under "With Identity Platform": on Blaze — "No-cost up to 50K MAUs", "Then Google Cloud pricing";
- [verifying an ID token](https://firebase.google.com/docs/auth/admin/verify-id-tokens), updated 2026-10-20: `auth_time` — "Must be in the past. The time when the user authenticated";
- [user sessions](https://firebase.google.com/docs/auth/admin/manage-sessions), updated 2026-10-20: the revocation check on the server — "an expensive operation, requiring an extra network round trip";
- the Admin SDK for Go, [v4.22.0](https://github.com/firebase/firebase-admin-go/releases/tag/v4.22.0) of 2026-09-23 — the latest release; the comments on the functions in `auth/auth.go` and `auth/user_mgt.go`: on `VerifyIDToken` — "In non-emulator mode, this function does not make any RPC calls most of the time"; on `VerifyIDTokenAndCheckRevoked` — "this function must make an RPC call to perform the revocation check"; on `RevokeRefreshTokens` — "existing ID tokens may remain active until their natural expiration (one hour)";
- [the Authentication emulator](https://firebase.google.com/docs/emulator-suite/connect_auth), no date: "For security reasons, the Authentication emulator issues unsigned ID tokens"; "Please make sure not to set the environment variable in production". `firebase-tools` [v15.0.0](https://github.com/firebase/firebase-tools/releases/tag/v15.0.0) of 2025-12-10: "Removed support for running emulators with Java versions prior to 21";
- [sign-in with Apple](https://firebase.google.com/docs/auth/web/apple), no date: "Sign In with Apple can only be configured by members of the Apple Developer Program".

### 2.2. Apple Developer Program, 2026-11-02

For R03; Д3. [the enrollment terms](https://developer.apple.com/programs/enroll/), no date, read 2026-11-02: "The Apple Developer Program is 99 USD per membership year".

### 2.3. OpenFGA, 2026-11-02

For architecture 3.1; R04. [openfga/openfga](https://github.com/openfga/openfga/tree/v1.22.1), release v1.22.1 of 2026-10-28 — the latest, commit 44054f5, license Apache-2.0, read 2026-11-02:

- README: "Can also be embedded as a Go library";
- `pkg/storage/migrate/migrate.go`, the comment on `RunMigrations`: "This function is exposed to allow embedding openFGA into applications and manage OpenFGA's database schema migrations directly"; the stores in `pkg/storage` — `postgres`, `mysql`, `sqlite` and `memory`;
- `.config-schema.json` — `changelog` has one setting, `changelogHorizonOffset`: "Changes that occur after this offset will not be included in the response of ReadChanges". There is no retention period for the records of `changelog` there (architecture 3.2; R05).

### 2.4. OpenFGA as a library, 2026-11-08

For R31. Read 2026-11-08, by v1.22.1 — still the latest release:

- [`pkg/server/server.go`](https://github.com/openfga/openfga/blob/v1.22.1/pkg/server/server.go): `NewServerWithOpts` — "You must call Close on it after you are done using it", and `Close` closes the storage too; the methods `Check`, `Write`, `Read`, `ListStores`, `CreateStore` and `WriteAuthorizationModel` are called directly, with `openfgav1` requests, and validate them themselves;
- [`pkg/storage/migrate/migrate.go`](https://github.com/openfga/openfga/blob/v1.22.1/pkg/storage/migrate/migrate.go) imports `github.com/go-sql-driver/mysql`, and [go-sql-driver/mysql v1.10.1](https://github.com/go-sql-driver/mysql/blob/v1.10.1/LICENSE) has "Mozilla Public License Version 2.0"; the migration files — `assets.EmbedMigrations`, `PostgresMigrationDir = "migrations/postgres"`;
- [on consistency](https://openfga.dev/docs/interacting/consistency): "caching is disabled by default. When caching is disabled, all queries will have strong consistency regardless of the consistency mode specified";
- [openfga/language `pkg/go` v0.3.2](https://github.com/openfga/language/tree/pkg/go/v0.3.2/pkg/go), license Apache-2.0: `transformer.TransformDSLToProto`;
- the size of the `api` binary with OpenFGA — a measurement, measurements 1.1.

### 2.5. The Admin SDK for Go and its tests, 2026-11-09

For architecture 3.1 and 3.10; R32. Read 2026-11-09:

- the Admin SDK for Go, [v4.22.0](https://github.com/firebase/firebase-admin-go/tree/v4.22.0) of 2026-09-23 — still the latest release, commit 66a72c3, `go 1.25.0`, Apache-2.0; the sources through `gh api`:
  - `firebase.go`: the root package imports `cloud.google.com/go/firestore`, `firebase.google.com/go/v4/storage`, `db`, `messaging` and others; `NewApp` with a given configuration returns no error — only when it reads `FIREBASE_CONFIG`; `App.Auth` calls `auth.NewClient(ctx, &internal.AuthConfig{…})`, and `NewClient` has "This function can only be invoked from within the SDK";
  - `auth/auth.go`: `verifyIDToken` checks revocation when `if c.isEmulator || checkRevokedOrDisabled`, so in emulator mode — `FIREBASE_AUTH_EMULATOR_HOST` — `VerifyIDToken` checks it too, and Identity Toolkit is `fmt.Sprintf("http://%s/identitytoolkit.googleapis.com", authEmulatorHost)`; without a key the SDK signs with `newIAMSigner`, which asks the metadata server for the service account's email only when signing;
  - `auth/token_verifier.go`: the keys — by `idTokenCertURL`, «https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com», with the client `transport.NewHTTPClient(ctx, option.WithoutAuthentication())`; `verifyTimestamps` checks `iat` and `exp` with `clockSkewSeconds = 300`, and checks `auth_time` nowhere; in emulator mode — "skip signature verification"; keys that cannot be fetched are the code `CERTIFICATE_FETCH_FAILED`, not `idTokenInvalid`;
  - `auth/user_mgt.go`: the error text names the UID — `fmt.Sprintf("no user exists with the %s", query.description())`;
  - `internal/internal.go`: `FirebaseScopes` — "the set of OAuth2 scopes used by the Admin SDK": `cloud-platform`, `datastore`, `devstorage.full_control`, `firebase`, `identitytoolkit` and `userinfo.email` from `https://www.googleapis.com/auth/`;
  - `internal/http_client.go`: `WithDefaultRetryConfig` — `MaxRetries: 4`, retries on network errors and 503, `MaxDelay` — two minutes;
- `google.golang.org/api` [v0.279.0](https://github.com/googleapis/google-api-go-client/tree/v0.279.0), which the SDK requires, the source code through `gh api`: `transport/http/dial.go` — `defaultBaseTransport` "Copy http.DefaultTransport except for MaxIdleConnsPerHost setting", through `clonedTransport(http.DefaultTransport)`; `option/option.go` — `WithCredentialsFile` and `WithCredentialsJSON` have "Deprecated: This function is being deprecated because of a potential security risk", and "you can use [WithAuthCredentialsFile] with [ServiceAccount]";
- a trial in a copy of the module outside the repository. `cmd/api`, which imports the port, pulls in 32 new modules — `cloud.google.com/go` with `firestore`, `storage`, `monitoring`, `auth` and `iam`, `google.golang.org/api`, `golang.org/x/oauth2`, otel, s2a-go, go-jose and others. Each one's LICENSE is Apache-2.0, MIT or BSD-3, and none has a NOTICE. `go-licenses check` by the allowed list passed with no refusals, and govulncheck v1.9.0 said: "Your code is affected by 0 vulnerabilities". The size of the binary with the port — a measurement, measurements 1.2. The real SDK against a fake Google: a live token passes with no refusal; `IsIDTokenRevoked`, `IsUserDisabled` and `IsUserNotFound` are told apart; a 500 response is recognized by none of the helpers `Is…`; a signature with a foreign key gives `IsIDTokenInvalid` without `IsIDTokenExpired`; `VerifyIDToken` returns the UID of a revoked token without a request.

### 2.6. An ID token without `app`, 2026-11-09

For R39. Read 2026-11-09:

- [the Firebase Auth REST API](https://firebase.google.com/docs/reference/rest/auth), no date: "Exchange custom token for an ID and refresh token" — `POST https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=[API_KEY]`, the body `token` and `returnSecureToken`, in the response `idToken`, `refreshToken` and `expiresIn`; "API_KEY refers to the Web API Key, which can be obtained on the project settings page in your admin console";
- [custom token](https://firebase.google.com/docs/auth/admin/create-custom-tokens), no date: the claims of a token made by a third-party JWT library — `alg` `"RS256"`, `iss` and `sub` — "Your project's service account email address", `aud` — `"https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"`, `exp` — "a maximum of 3600 seconds later than the `iat`", `uid` — "between 1-128 characters long, inclusive".

### 2.7. A volume two devcontainers share, 2026-11-13

For R67. Read 2026-11-13, [Volumes](https://docs.docker.com/engine/storage/volumes/) of the Docker documentation, no date:

- "Multiple containers can mount the same volume. You can simultaneously mount a single volume as `read-write` for some containers and as `read-only` for others.";
- the option `readonly` of `--mount`: "If present, causes the volume to be mounted into the container as read-only.";
- "If you start a container which creates a new volume, and the container has files or directories in the directory to be mounted such as `/app/`, Docker copies the directory's contents into the volume." Whether the owner and the mode of the directory itself pass to the volume, the page does not say.

## 3. Database and learner model

For architecture 3.2: Neon and the rejected Cloud SQL and the profile on Drive, the database of the tests and the migrations.

### 3.1. Neon, 2026-11-02

For architecture 3.2; R05. Read 2026-11-02:

- [regions](https://neon.com/docs/introduction/regions), the IP list updated 2026-10-28: "Neon offers project deployment in multiple AWS regions"; among them `aws-us-east-1` (N. Virginia), `aws-us-east-2` (Ohio), `aws-us-west-2`, `aws-eu-central-1` (Frankfurt) and `aws-eu-west-2` (London); "You can no longer create new projects in Azure regions". There are no Google Cloud regions in the list;
- [pricing](https://neon.com/pricing), "Last updated on October 31, 2026": Free has "100 CU-hours Per Project", "1 GB per Project", the window — "Up to 6 hours or 1 GB of data changes"; Launch has "$0.106 per CU-hour", "$0.35 per GB-month", history — "$0.20 per GB-month", the window "Up to 7 days", suspension — "After 5 minutes can be disabled", and the spending estimate — "Typical spend: $ 15 /mo Based on: Intermittent load , 1 GB". The answer to the question about the Free plan: "When a database is idle, its compute scales to zero after 5 minutes and starts again on the next query, typically within a few hundred milliseconds" and "Compute is suspended only if a project uses up its monthly 100 CU-hours or 5 GB of network transfer";
- [the history window](https://neon.com/docs/postgres/backup-restore/history-window), no date: the window "sets how long that change history is kept, which defines how far back instant restore and the other features can reach"; the default and the largest — "Free 6 hours 6 hours (capped at 1 GB of history) Launch 1 day 7 days Scale 1 day 30 days";
- [how to reduce costs](https://neon.com/docs/introduction/cost-optimization), no date: "Be aware that delete operations generate WAL records that temporarily add to your instant restore storage until they age out of your history window";
- [databases](https://neon.com/docs/manage/databases), no date: "In the Neon object model, a database exists within a branch of a project. There is a limit of 500 databases per branch";
- [connection pooling](https://neon.com/docs/connect/connection-pooling), no date: "Neon uses PgBouncer in transaction mode ( pool_mode=transaction )"; "Not supported with pooled connections: SET / RESET (session variables) LISTEN / NOTIFY WITH HOLD CURSOR PREPARE / DEALLOCATE (SQL-level prepared statements)", and also "Session-level advisory locks";
- [security](https://neon.com/docs/security/security-overview), no date: "Neon requires that all connections use SSL/TLS encryption";
- [compliance](https://neon.com/docs/security/compliance), no date: "We have completed audits for SOC 2 Type 1 and Type 2, SOC 3, ISO 27001, and ISO 27701, and we adhere to GDPR and CCPA regulations";
- [Neon's terms](https://neon.com/platform-terms) — the address `neon.com/dpa` leads here, "Last Updated: October 14, 2026": "This Product Specific Schedule (Neon) … is entered into as of the Effective Date between Databricks, Inc., the parent company of Neon, LLC ("Databricks"), and Customer"; the schedule changes sections of the Databricks DPA. It says nothing about children or minors;
- [subprocessors](https://www.databricks.com/legal/databricks-subprocessors) — `neon.com/subprocessors` leads here, "Last Updated: June 9, 2026": "Amazon Web Services, Inc. Cloud Service Provider United States Customer Selected".

### 3.2. Cloud SQL for PostgreSQL, 2026-11-02

For R05 — the option it rejected. Read 2026-11-02:

- [pricing](https://cloud.google.com/sql/pricing), no date: "Instance pricing is charged for every second that the instance is running (the activation policy is set to ALWAYS)";
- [starting and stopping](https://docs.cloud.google.com/sql/docs/postgres/start-stop-restart-instance), "Last updated 2026-10-26 UTC": "If you are not using your instance, you can set its activation policy to NEVER to avoid instance charges" — that is, stopping by hand, not when idle;
- [the free trial instance](https://docs.cloud.google.com/sql/docs/postgres/free-trial-instance), "Last updated 2026-10-26 UTC": "A Cloud SQL free trial instance lets you test the majority of Cloud SQL features for up to 30 days without any financial commitment".

### 3.3. mathtrail-standalone, 2026-11-02

For R06 — the option it rejected. [docs/architecture/](https://github.com/mathtrail/mathtrail-standalone/tree/c6cbeb2/docs/architecture), commit c6cbeb2, read 2026-11-02 through `gh api`:

- `04-profile.md`: "everything the service knows sits in a JSON file in a visible folder of the parent's Drive (О-5). The service keeps nothing of its own between requests, so this file is not a cache of some server-side truth — it **is** the truth"; `recent` — "the last **20** entries"; the choice rule is computed "from the summary and the window alone — never from the full history, which we do not keep";
- `05-storage.md`: "**There is no conditional write.** … Whoever uploads last wins, silently"; "If the parent empties the Drive bin, the profile is gone — no server-side copy exists, by design".

### 3.4. embedded-postgres, 2026-11-02

For Д6. [fergusstrange/embedded-postgres](https://github.com/fergusstrange/embedded-postgres), release v2.0.1 of 2026-10-19 — the latest, license MIT, read 2026-11-02. README: "Run real PostgreSQL in your tests"; "The first start downloads PostgreSQL and the matching supervisor; later starts reuse the cache"; "Requires Go 1.26 or newer"; "Root callers must select an existing non-root Unix identity". In `internal/binaries/manifest.json` of v2.0.1 each Postgres archive has a `sha256` and an address in the releases of `theseus-rs/postgresql-binaries`. In `supervisor_binary.go` the downloaded supervisor is checked against `checksums.txt` of the same release, which `cacheSupervisor` downloads at start-up, and without a published version the library asks for "Config.Supervisor(path) or EP_SUPERVISOR". The devcontainer's user is `vscode`, not root.

### 3.5. Database and migrations, 2026-11-08

For R29. Read 2026-11-08:

- [`embedded-postgres` v2.0.1](https://github.com/fergusstrange/embedded-postgres/tree/v2.0.1) — still the latest release; the module's `go.mod` has no requirements, and README: "The production Go packages have zero third-party dependencies". In `config.go` — `V17 PostgresVersion = "17.11.1"`, and `Supervisor(path)` and `Port(0)` are builders of `Config`. README: "An independent supervisor watches a dedicated owner pipe. An unrecovered panic, `os.Exit`, or forced owner termination closes that pipe and triggers PostgreSQL shutdown";
- [pgx v5.12.0](https://github.com/jackc/pgx/blob/v5.12.0/CHANGELOG.md), license MIT: CHANGELOG closes GHSA-95fj-3fxw-g62g — in v5.9.0–v5.11.0 `require_auth` and `channel_binding=require` were bypassed without `sslmode=verify-full`; in `pgconn/config.go` `sslmode=require` without `sslrootcert` sets `InsecureSkipVerify`. On the default query mode `ConnConfig.DefaultQueryExecMode` it says: "may be incompatible with proxies such as PgBouncer unless they are configured to support protocol-level prepared statements";
- [goose v3.29.0](https://github.com/pressly/goose/tree/v3.29.0), license MIT: `NewProvider` with `WithSessionLocker` and `WithDisableGlobalRegistry`; `lock/postgres.go` — `pg_try_advisory_lock`, a session lock; README: "By default, all migrations are run within a transaction";
- [Neon, connection pooling](https://neon.com/docs/connect/connection-pooling), updated 2026-10-19: "pool_mode=transaction", "max_prepared_statements=1000 … Maximum protocol-level prepared statements per connection"; among what the pooler does not hold — "Session-level advisory locks"; in the table of choice — "Schema migrations \| Direct \| Tools may not support transaction pooling";
- [OpenFGA v1.22.1](https://github.com/openfga/openfga/blob/v1.22.1/pkg/storage/postgres/postgres.go): the Postgres store opens `pgxpool.ParseConfig(uri)` and sets no query mode, and the store's migrations go through goose without a lock (`pkg/storage/migrate/migrate.go`).
- [tern v2.4.3](https://github.com/jackc/tern/blob/v2.4.3/migrate/migrate.go), license MIT: the package `migrate` imports `github.com/Masterminds/sprig/v3`; [golang-migrate v4.20.1](https://github.com/golang-migrate/migrate/blob/v4.20.1/go.mod), license MIT: the root `go.mod` requires drivers of many databases and cloud SDKs — Spanner, Cloud Storage, AWS, MongoDB, Snowflake.

## 4. Module versions and packages in the image

For architecture 3.3: `modules` and `module-kit`, the checks of CI, embedding a file and a scheduled job of GitHub Actions.

### 4.1. `modules` and `module-kit`, 2026-11-02

For architecture 3.3; R07. Read 2026-11-02 through `gh`:

- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/105fa72), commit 105fa72 on `main`: `gh release list -R altnovatrail/modules` — empty, no tags, so there is no `module-index.json` in any release. In RUN.md the task T01 «Раскладка модулей в репозитории» is `[ ]`, and it says: «индекс модулей один на все модули: как его собирает релиз одного модуля, чтобы индекс называл и пакеты остальных. На этапе 2 `api` берёт дайджесты из индекса релиза (architecture 3.10 spec)»;
- [altnovatrail/module-kit](https://github.com/altnovatrail/module-kit/tree/1a54028), commit 1a54028 on `main`: in `docs/architecture.md` — «Пока он отвечает только на `--version`; проверка модуля, сборка пакета и индекса придут с форматом v0». The only release is v0.1.0 of 2026-10-21; it was signed by the shared `release.yml` of `.github` v0.1.4, and `release-verify` rejects it (R77 spec).
- read 2026-11-07 for the move to `spec` v0.9.4 (R23): [altnovatrail/module-kit, `docs/architecture.md`](https://github.com/altnovatrail/module-kit/blob/e069943/docs/architecture.md), commit e069943 on `main`, the latest release — `v0.2.3`; the first release with the code of the format that others take is `v0.2.0` (row 3.3 of the closed rows of `COORDINATION.md` of `spec` v0.9.4). The table of Go packages: `modulepackage` — «канонический вид, сборка пакета, чтение по цепочке дайджестов — с `device/` и без», called by `api` too; `moduleindex` — «сборка и чтение индекса, версия новой попытки», called by the CLI and `api`; `observation` — «годность наблюдения, сведение двух чтений, вердикт по наблюдению с выбором касанием, исход сочинения», called by `api`.
- read and built 2026-11-09 for the move to `spec` v0.12.0 (R40): the latest releases — `module-kit` [`v0.2.6`](https://github.com/altnovatrail/module-kit/tree/v0.2.6) of 2026-11-08, its `go.mod` — `github.com/altnovatrail/spec v0.9.9`; `modules` [`v0.1.4`](https://github.com/altnovatrail/modules/releases/tag/v0.1.4) of 2026-11-09; `tutor` [`v0.1.21`](https://github.com/altnovatrail/tutor/tree/v0.1.21) of 2026-11-09, its `go.mod` — `module-kit v0.2.3` and `spec v0.9.16`. `module/check.go` of `v0.2.6`: "const FormatVersion = "0.3"". An empty module in a temporary folder with `module-kit` `v0.2.6` and `spec` v0.12.0 — `go build` of the package `modulepackage`: "module/tasks.go:236:14: cannot use part.Picture (variable of type *formatv0.TaskPicturePath) as *formatv0.PicturePath value in assignment" and "module/tasks.go:290:11: formatv0.ExplanationPart does not satisfy formatv0.StatementPart | formatv0.DemoPart". By package: `observation`, `verdict` and `openview` build, `moduleindex` and `moduleversion` do not. An empty module with `tutor` `v0.1.21` and `spec` v0.12.0 — the packages `tutor`, `tutor/admission`, `tutor/words` and `observation` with `verdict` of `module-kit` build;

### 4.2. Required checks and the path filter, 2026-11-02

For architecture 3.3; R07. [GitHub Actions workflow syntax](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions), no date on the page, read 2026-11-02: "If a workflow is skipped due to path filtering, branch filtering, or a commit message, then checks associated with that workflow will remain in a 'Pending' state. A pull request that requires those checks to be successful will be blocked from merging".

### 4.3. Embedding a file in Go, 2026-11-02

For architecture 3.3; R07. [the package `embed`](https://pkg.go.dev/embed), documentation for go1.27.3, read 2026-11-02: "Patterns may not contain '.' or '..' or empty path elements, nor may they begin or end with a slash"; "The patterns are interpreted relative to the package directory containing the source file".

### 4.4. The scheduled workflow and its signal, 2026-11-12

For architecture 3.3; R61. Read 2026-11-12, no date on the pages:

- [workflow events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows), `schedule`: "High load times include the start of every hour"; "Scheduled workflows run on the latest commit on the default branch"; "In a public repository, scheduled workflows are automatically disabled when no repository activity has occurred in 60 days"; on a pull request and other events from `GITHUB_TOKEN` — "other `GITHUB_TOKEN`-triggered events do not create workflow runs at all";
- [notifications for runs](https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs): "Notifications for scheduled workflows are sent to the user who initially created the workflow"; if someone else changes the cron line, "subsequent notifications will be sent to that user instead", and if the workflow was disabled and enabled again, the notifications go to "the user who re-enabled the workflow"; "You can also choose to receive a notification only when a workflow run has failed";
- [the repository's Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository): "You can choose to allow or prevent GitHub Actions workflows from creating or approving pull requests". `api` has `default_workflow_permissions: read` and `can_approve_pull_request_reviews: false` (`gh api repos/altnovatrail/api/actions/permissions/workflow`);
- [the latest release](https://docs.github.com/en/rest/releases/releases#get-the-latest-release): "The latest release is the most recent non-prerelease, non-draft release, sorted by the created_at attribute"; when creating a release, `make_latest` defaults to `true`; the public `https://github.com/altnovatrail/modules/releases/latest` redirects to `…/releases/tag/v0.2.5` (`curl -sI`);
- [Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions): "The following usage remains free independently and does not reduce the included compute usage: Standard GitHub-hosted runners in public repositories".

### 4.5. Sparse files in `archive/tar`, 2026-11-13

For architecture 3.3; R41. Read 2026-11-13 in `src/archive/tar/` of Go 1.27.4, through `go env GOROOT`, and tried by a test:

- `common.go`: `// Type 'S' indicates a sparse file in the GNU format.` and `paxGNUSparse = "GNU.sparse."`, the prefix of the keywords of a sparse file in a PAX extended header;
- `reader.go`: `readGNUSparsePAXHeaders` — "checks the PAX headers for GNU sparse headers. If they are found, then this function reads the sparse map and returns it", and the size of the entry becomes `GNU.sparse.size` or `GNU.sparse.realsize`; the old GNU format keeps its type `S`, and `Next` gives an entry of PAX sparse with the type of a regular file. Reading it gives the zeros of its holes;
- the trial, a conclusion from the code that a test confirmed: a tar of 3,584 bytes, 193 after gzip, with the records `GNU.sparse.major=1`, `minor=0`, `name=inventory.json` and `realsize=500000000`, was unpacked by `unpack` of `internal/packages` to a file of 500,000,000 bytes, before T59 refused such an entry. The input is `testdata/fuzz/FuzzUnpack/sparse-file`.

## 5. Carrying progress over

For architecture 3.4.

### 5.1. `learner-model` and `module-kit` on carrying progress over, 2026-11-02

For architecture 3.4; R08. Read 2026-11-02 through `gh api`:

- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/9f338ae), commit 9f338ae on `main`, the `spec` submodule at v0.2.9. In RUN.md the task T04 «Журнал свидетельств (Т-22)» is `[ ]`, and it says: «перенос по таблице соответствия старых ID навыков новым (concepts 3 spec): что делает библиотека, а что `api` — перенос есть в обеих карточках (architecture 3.5, 3.8 spec)». In `docs/architecture.md`, in the subsection «Журнал свидетельств»: «Журнал только на дозапись свидетельств не переписывает: при нём старые ID остаются в свидетельствах, и перенос идёт через таблицу при пересчёте. Как он устроен и чья это работа, не решено»;
- [altnovatrail/module-kit](https://github.com/altnovatrail/module-kit/tree/1a54028), commit 1a54028 on `main`. In RUN.md the task T04 «Правила semver для `diff`» is `[ ]`, and it says: «таблица соответствия старых ID навыков новым: где она лежит и что сравнение у неё проверяет — покрыт ли каждый удалённый навык, не переиспользован ли опубликованный ID» and «откуда CI `modules` берёт прошлую версию».

## 6. Showing the solution and hints

For architecture 3.5: MathTrail's examples and the neighbouring repositories.

### 6.1. Showing the solution and hints in MathTrail, 2026-11-02

For architecture 3.5; R09, R10. Read 2026-11-02 through `gh api`:

- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0 — the latest on `main`:
  - R39 canvas-api: call 2 goes if «неверных ответов на холсте не больше трёх»; the model gets «ступень подсказки и прошлые вопросы этого холста»; «Дальше наставник предлагает позвать взрослого или начать заново. Ответ не показывается никогда»; rejected — «показывать ответ после третьей ступени»;
  - R24 canvas-api: «Лестница подсказок идёт по числу неверных ответов на холсте»; «После третьей ступени наставник предлагает позвать взрослого»;
  - spec 6.3 canvas-api: «`UNREADABLE` и `UNCHECKED` ступень не меняют»; «**После третьей ступени** вызова 2 нет. Событие `hint` с `kind: offer_help` несёт фразу шаблона `offer_help` (7.3) и кнопки «Ask a grown-up» и «Start again»»; the trap text is the rung template if it «совпадает с подсказкой, уже показанной на этом холсте, в том числе с вопросом модели». During the check: «записан `INCORRECT`, а подсказки нет — подсказкой становится то, что положено по маршруту 6.1 без вызова 2: запасной текст (6.3) на 1-м–3-м неверном ответе, `offer_help` с 4-го»;
  - spec 9.7 canvas-api: «**Холст, к которому не возвращаются,** остаётся `active`: ежедневная очистка его не бросает»; «Брошенным холст делает только новый холст той же задачи».
- [mathtrail/mathtrail-standalone, `docs/decisions.md`](https://github.com/mathtrail/mathtrail-standalone/blob/89986bc/docs/decisions.md), commit 89986bc — the latest on `main`:
  - R93 standalone: ""I don't know" is an answer, and a wrong one"; "a child who has seen the solution has not solved the task; counting it as wrong keeps the rating an honest estimate"; rejected — "a skip that leaves the rating alone, which would need a third outcome in the profile and the formula";
  - R98 standalone: "When `next_task` is asked for a new task while the current one has no answer, the current one is recorded as skipped"; "Nothing that reads answers reads it — not the rating"; "punishing that would punish a stray tap; but leafing through hard tasks until an easy one comes shows only in what the parent can see, and a skip that left no trace would hide it".
- the NovaTrail prototype — `spec/draft/README.md` and `spec/draft/NovaTrail Widget.html` in `spec` v0.6.2: ""Show solution" ends the task as skipped, with no change to the rating | Open, product О-7"; "A quick reply "Give me a hint" at any moment, before the first answer too". The adult's switch "Ask before “Show solution”" with the caption "The coach first offers one more hint" is off by default — `S['t_' + k] ?? (k === 'week')`, — and there is no string `t_ask` in the code: only the switch itself reads its value.

### 6.2. The neighbouring repositories on showing the solution and the hint, 2026-11-02

For architecture 3.5; R09, R10. Read 2026-11-02 through `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), commit a42a7e1, the `spec` submodule at v0.6.2. In `docs/architecture.md`: ««Подсказка» — всегда, в обоих режимах, с начала задачи. Работу она не шлёт (R69 spec), а запрос всегда идёт в `api`» (architecture 3.3 app); «Попытка кончилась, когда `api` сказал, что задача кончилась, — верный ответ, показ решения задачи, попытка закрыта «без проверки» на снятой версии (R63 spec), — или когда ученик начал ту же задачу заново» (architecture 3.2 app). `app` is silent on the setting "ask before showing";
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, the `spec` submodule at v0.6.2. The hint request is `func Request(t *sample.Task, sh *reading.Sheet, rung Rung, lang sample.Lang)` in `internal/hint/hint.go`, without the past questions and the matched trap. In `docs/architecture.md`: «Поле у запроса подсказки появится, если О-21 решат по рекомендации» (architecture 3.10 tutor); on the feedback and the hint on a failure — «запасной текст ступени… Как это сочетается с нейтральной фразой при сбое ИИ (architecture 4.1 spec), решает детализация `api`» (architecture 3.7 tutor). The form and leak checks lie in `internal/`, and the library for `api` comes from the epic E5 «Код библиотеки `tutor`» in table 2 of its RUN.md;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/9f338ae), commit 9f338ae, the `spec` submodule at v0.2.9. In RUN.md the task T03 «Веса свидетельств» is `[ ]`, and it is to decide: «меняет ли подсказка вес свидетельства или только то, ставится ли «освоено»; «без проверки» — свидетельство с нулевым весом или не свидетельство вовсе».

## 7. Platform AI quota

For architecture 3.6: the provider's prices, the usage report of `tutor` and MathTrail's examples.

### 7.1. Platform AI quota, 2026-11-02

For architecture 3.6; R11, R12. Read 2026-11-02:

- Anthropic's prices — [Pricing](https://platform.claude.com/docs/en/about-claude/pricing), no date on the page: per million input and output tokens — "Claude Opus 5.5 … $4 / MTok … $20 / MTok", "Claude Sonnet 5.5 … $2 / MTok … $10 / MTok", "Claude Haiku 5.5 … $0.10 / MTok for prompts up to 100,000 tokens … $0.50 / MTok", "Claude Fable 5.1 … $10 / MTok … $50 / MTok". [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/thinking), no date: "Thinking has a cost: the tokens Claude spends reasoning are billed as output tokens, even when the thinking text isn’t returned to you". [Vision](https://platform.claude.com/docs/en/build-with-claude/vision), no date: "An image, therefore, costs ⌈width / 28⌉ × ⌈height / 28⌉ visual tokens". The prices are the same as in the table of `tutor`, read 2026-10-23 (architecture 3.3 tutor);
- payment for a cut-off request — [How do I pay for my Claude API usage?](https://support.claude.com/en/articles/8977456-how-do-i-pay-for-my-claude-api-usage), no date on the page: "You're billed only for successful API calls and completed tasks. Failed requests aren't charged"; "If your client disconnects or times out in the middle of a request that was on track to succeed, that request is still charged";
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, through `gh api`:
  - architecture 3.7 tutor: in the usage report «по записи на каждый вызов модели: у чтения рукописи их две, по читателю (R68 spec), а у чтения сочинения — одна…»; in the record — «поставщик и модель, как её назвал поставщик в ответе», the tokens, «оценка в долларах — по таблице цен `tutor`»; «Ответ, оборванный до подсчёта токенов, и вызов, прерванный до счёта, считаются по худшему случаю»; «ни содержания, ни идентификаторов в сообщении нет, и его можно писать в журналы (R09 spec)»; «по нему `api` считает квоты ИИ платформы (architecture 3.5 spec). На своём ключе `api` его не копит». On the two readers: «Если хоть одному читателю рукописи поставщик отказал по лимиту, чтение отдаёт исход «лимит исчерпан» целиком»; «Но вызов читателя, который ответил, оплачен, и его запись остаётся в сообщении о расходе». On the outcome "limit reached": «На своём ключе это повод сказать владельцу ключа, а на ИИ платформы — сигнал оператору: отказ — ключу платформы»; Anthropic's tier limit — «до 00:00 UTC первого числа следующего месяца». There is no code of the report: «Кода продукта ещё нет: его строит эпик после T06»;
  - architecture 3.3 tutor: «Худший случай одного ответа — 4 000 токенов входа и 16 000 выхода: от $0,0084 у Haiku 5.5 до $0,84 у Fable 5.1». For Opus 5.5, by the prices above, that is about $0.34 — our count;
  - architecture 3.4 tutor, «Итог двух вызовов сочинений» (T92): the price of an essay on Opus 5.5 · `high` — «$0,030», on Haiku 5.5 · `low` — «$0,0006»;
  - architecture 3.12 tutor, the series T40: «Claude Opus 5.5 · `high` | 4 из 4 | 5 | 0 | $0,0098 | 5,7 с» — an answer with feedback. Until the 2026-11-08 amendment to R11 the estimate took this price for the words of a rung, and this is a judge's answer, not a call for words;
- [altnovatrail/tutor, `docs/architecture.md`](https://github.com/altnovatrail/tutor/blob/9f85613/docs/architecture.md), commit 9f85613 — the latest on `main`, read 2026-11-08 through `gh api`: architecture 3.4 tutor, «Итог замера подсказок» (T76, 2026-10-28), the row «Opus 5.5 · `high`» — p50 «5,1» s, «Цена вызова» — «$0,0152». The prices of the day — «Opus 5.5 — $4 и $20 за 1 млн токенов, как в разделе 4»;
  - architecture 3.9 tutor: «Если ИИ, который нужен проверке задачи, ученику закрыт, `api` даёт задачи, которые проверяются без ИИ (architecture 4.2 spec; О-5): ученик не пишет работу, которую некому прочитать. Какие это задачи, решают `api` и `engine` — строка В64»; «сам ключ приходит только с работой (architecture 4.3 spec)»; an admission refusal — «исход «не допущен», отдельный от сбоя ИИ и от «лимит исчерпан», но ученик видит то же, что при исчерпанном лимите»; on the first app — «Ключ автора — это ключ взрослого, который сам и ученик, и владелец ключа: вызов идёт от его имени (privacy 5.2 spec), и путь «свой ключ» для `adult` открыт без фактов»;
  - R55 tutor: «С оценкой в долларах `api` считает квоты и юнит-экономику (О-14), не зная цен моделей»; rejected — «лимит в вызовах или токенах — вызов чтения и вызов разбора стоят по-разному, а токены разных моделей — тоже». This is an argument about the own key limit, and it bears on the quota of `api` by the same argument;
- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0 — the latest on `main`, through `gh api`:
  - R35 canvas-api: «Счётчики резервируются в начале проверки, вместе с арендой…»; «Резерв возвращается, если его вызов модели не понадобился или не был сделан»; «Ребёнку об этом сказано мягко: «I've helped all I can for now. Keep going on your own!» — на каждое нажатие Check. Времени фраза не обещает: сутки лимитов — UTC, и для семьи в США они кончаются ещё днём»;
  - R36 canvas-api: «После MVP — три тарифа»: the free one — «дневная квота помощи наставника, как в бете», «За $10 в месяц — квоты больше», «За $20 в месяц — квоты ещё больше и ИИ-наставник с голосом»;
  - spec 11 canvas-api: «**Сутки** для лимитов — UTC»; the daily cleanup «удаляет счётчики старше 30 дней и истёкшие инвайты»;
  - spec 14.5 canvas-api: «Чтений ответа на ребёнка в день | 60», «Разборов черновика на ребёнка в день | 20», «Чтений и разборов на родителя в день, по всем детям | 180 / 60»; «**Худший день.** При средней цене вызова ≈ $0.14 на ребёнка и ≈ $0.41 на родителя, а потолок — если каждый вызов дойдёт до своего лимита выходных токенов (7.1) — ≈ $0.52 и ≈ $1.55»;
  - privacy 4 canvas-api: «| Дневные счётчики | ребёнка и родителя | Neon, `usage_daily`, `parent_usage_daily` | лимиты | 30 дней | нет |»;
- [mathtrail/mathtrail-standalone](https://github.com/mathtrail/mathtrail-standalone/tree/89986bc), commit 89986bc — the latest on `main`, through `gh api`:
  - R293 standalone: "The day of the limits is the family's: it ends at the family's midnight, by the clock the card tells with each answer"; "an offset no time zone keeps, more than twelve hours behind or fourteen ahead or off the quarters of an hour, is passed over"; "Until a card has told a clock, the day is the UTC day, as before. A clock counts from the next day on"; "No line of the log carries the offset"; why — the day of the limits ended "at seven in the evening" for a family five hours behind UTC; rejected are "a rolling day, so many tasks in any twenty-four hours, which needs no clock but makes "tomorrow" the hour the day began" and "the UTC day as it was";
  - SPEC 10.3 standalone: "No refusal ever names a limit's number, because a number invites arithmetic rather than a lesson";
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), commit a42a7e1, `docs/architecture.md`: «тариф и квота ИИ платформы в разделе для взрослых — О-14»; «Сбой ИИ или исчерпанная квота — нейтральная фраза (R04 spec)».
- Anthropic's prices were checked again 2026-11-11, for the price of a check on the first app (T42; measurements 2.1), and 2026-11-12, for the run of the quota (T52; measurements 3). Added by T62 from the bodies of these tasks in the archives [docs/plan/E3.md](plan/E3.md) and [docs/plan/E6.md](plan/E6.md), not from the primary source, and there are no quotes there: T42 has only «По ценам, сверенным 2026-11-11», with no address, and T52 has a retelling: «Цены Anthropic сверены 2026-11-12 ([Pricing](https://platform.claude.com/docs/en/about-claude/pricing)): Opus 5.5 — $4 и $20 за MTok входа и выхода, Haiku 5.5 — $0,10 и $0,50». These are the same prices as 2026-11-02 above, and by them comes the $0.138 of the measurement T42.

## 8. Lesson

For architecture 3.7: what the neighbours decided about the lesson.

### 8.1. The lesson at the neighbours, 2026-11-02

For architecture 3.7; R13. Read 2026-11-02 through `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/3ce4cdb), commit 3ce4cdb, the `spec` submodule at v0.6.2:
  - R06 app: «Место в уроке и состояние показа его примеров принадлежат прохождению урока на его пакете — ключ — ID урока и дайджест пакета»; «Обычный конец — всегда «Продолжить» на последнем звене, какого бы вида оно ни было: `app` говорит `api`, что урок пройден»; «Сам `api` кончает урок только необычным путём — досрочно или «без проверки»»; rejected — «место в уроке на сервере: каждое «Продолжить» — запрос и новая строка данных ученика в privacy 4 spec»; it does not decide — «когда урок пройден и что `api` о нём записывает, и продолжает ли урок, начатый на другом устройстве, то же прохождение — T12 `api`»;
  - architecture 3.4 app: «Уход посреди урока ничего не шлёт: состояние урока лежит в базе профиля, пока его не сотрёт срок в 30 дней без изменений (раздел 3.2). Когда `api` снова даёт этот урок на том же пакете, `app` открывает его на месте и с показами, как они были.»; «Урок открывается на более позднем из двух мест: своём, из состояния урока, и том, что идёт после последней кончившейся задачи по ответу `api`.»; «Звено задачи, которая кончилась, полоса показывает пройденным, но не открывает»;
  - the row В15 of table 3 of RUN.md of `app`: «Состояние показа `api` знать не нужно — это ответ на вопрос T12 api»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/20ae268), commit 20ae268, `docs/architecture.md`: «Пример с разбором в уроке — не задача и свидетельством не служит, а задача урока — такая же попытка, как вне урока (R58 spec; architecture 3.5 spec)»;
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, `docs/decisions.md`: «Вызова `tutor` урок не добавляет»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), commit 08bc48a, the `spec` submodule at v0.2.9: at the engine's input — «уроки и пул задач»; in RUN.md, in the task T01, which has not started, it is to decide — «когда движок даёт урок, а когда задачу вне урока (concepts 5 spec)»;
- [altnovatrail/engine, `docs/decisions.md`](https://github.com/altnovatrail/engine/blob/9751ba4/docs/decisions.md), commit 9751ba4 — the latest on `main`, read 2026-11-08 through `gh api` for the move to `spec` v0.9.11 (R24), and in `spec` its outcome is R107 spec: R05 engine — «Закрепления в уроке и досрочного конца в первом срезе нет»; R09 engine — «данных ученика в вызове столько, сколько правило читает (privacy 1 spec): прохождения без мест звеньев, попытки с рубрикой — только «Готово»»; R03 engine — «Задача с виденным решением — 24 часа не кандидат, а потом — после всех задач навыка без виденного решения»;
- [altnovatrail/skill-map](https://github.com/altnovatrail/skill-map/tree/0295ce8), commit 0295ce8, `docs/architecture.md`: «не выбирает следующий урок или задачу: это делает движок (concepts 12 spec)»; the package draws the graph and the state of the learner model «по готовым данным от `api` — ярусы, пререквизиты, владение»;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/a0db6e9), commit a0db6e9: in the tree — documents and settings, neither module folders nor `lessons/`;
- `spec` [v0.6.4](https://github.com/altnovatrail/spec/blob/v0.6.4/RUN.md) — then a tag newer than the `api` submodule, and since T14 the submodule is at v0.6.6 (R16) —, RUN.md, «Что `app` нужно от контракта в E3»: on «урок пройден» — «Как `api` сверяет его с попытками урока и что записывает, решает T12 `api`»; on «что известно и что найти» — «`api` их не записывает, пока О-21 не решит, что они входят в свидетельство».

## 9. Task with a rubric

For architecture 3.8: what the neighbours decided about the essay.

### 9.1. The task with a rubric at the neighbours, 2026-11-02

For architecture 3.8; R14, R15. Read 2026-11-02 through `gh api`:

- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, the `spec` submodule at v0.6.2, `docs/architecture.md`:
  - architecture 3.5 tutor, the output of the assessment: «по каждому критерию — номер, обоснование и выполнен ли он»; «критерий, которого нет в ответе или который назван дважды, `tutor` отдаёт без оценки, как в замере (раздел 3.4, «Оценка по рубрике»), а отзыв — как есть, через правило отзыва. Повторять ли тогда вызов, решает `api`. Сбой вызова оставляет без оценки все критерии, и ученик видит нейтральную фразу»; «видит ли ученик оценки по критериям, решают `api` и `app`: в открытом виде критериев нет»; «У критерия в формате `spec` есть только текст, без ID, поэтому `tutor` нумерует критерии по порядку в рубрике»;
  - architecture 3.5 tutor, the reading: «сочинение, которое не прочитать, до оценки не доходит… так что как `api` это показывает и записывает, — его детализация… Такого выхода у промпта замера нет: синтетика вся разборчива. Что делать с отдельным неразборчивым словом, покажет замер на сочинениях добровольцев»; in the table of request types — architecture 3.10 tutor — the reading of an essay returns «текст, как написан, и можно ли его прочитать»;
  - architecture 3.5 tutor, the feedback rule: the function «получает отзыв, прочитанный текст и язык объяснений. Отвечает «прошёл» или называет, какие части правила нарушены: предложения, слова, цитата, письмо»; «проверка утечки ищет в отзыве только фразы-утечки задачи, если автор их дал»; «что ученик видит вместо отзыва, который правило не прошёл, решает детализация `api` (R70 spec)»; the logs — «без содержания: ни прочитанного текста, ни обоснований, ни отзыва, хотя отзыв может цитировать сочинение»;
  - architecture 3.4 tutor, «Итог двух вызовов сочинений» (T92): on Opus 5.5 · `high` — «Правило отзыва по R70 spec нарушено» «2 из 45», the price of an essay «$0,030», the time p50 «11,9» s;
  - architecture 3.7 tutor, the own key limit reached: «на оценке по рубрике — нейтральную фразу, а критерии остаются без оценки, как при сбое вызова»;
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/14f5a54), commit 14f5a54, the `spec` submodule at v0.6.2:
  - R04 app: «сочинение с отзывом по рубрике — артефакт и живёт, пока его не сотрут»; in the table of architecture 3.2 app: «сочинение — с начала попытки, законченное и нет: история письма, прочитанное, отзыв по рубрике | база профиля | как история попытки | когда его сотрёт взрослый на странице «Это устройство»; ни конец попытки, ни срок его не стирают»;
  - architecture 3.3 app: «Сочинение — чтение и отзыв по рубрике, без вердикта (R75 spec); что идёт после отзыва, решает `api`»;
  - architecture 3.4 app: «На задаче «Продолжить» появляется, только когда `api` сказал, что задача кончилась»; «Сочинение с отзывом по рубрике остаётся на устройстве, но из полосы урока не открывается»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/28433fa), commit 28433fa, `docs/architecture.md`: «попытка задачи с рубрикой в первом срезе его не даёт (О-17)»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), commit 08bc48a, the `spec` submodule at v0.2.9, `docs/architecture.md`: «навыки с рубрикой и без оценки идут в порядке автора»; its T01, where this is decided, has not started;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/97c20e2), commit 97c20e2: in the tree — documents and settings, no modules yet;
- `spec` [v0.6.5](https://github.com/altnovatrail/spec/blob/v0.6.5/docs/privacy.md) — then a tag newer than the `api` submodule, and since T14 the submodule is at v0.6.6 (R16), commit 1a99391. П-19 there is the same, and П-8 was supplemented 2026-11-02 by В9 of `app`: «хранение сочинений на сервере по выбору владельца аккаунта приходит с модулем сочинений, до первого ребёнка: иначе единственная копия работы ученика зависит от правил браузера». In the row 6.2 of its `COORDINATION.md` T13 already stands: «путь задачи с рубрикой (T13, …)»;
- the NovaTrail prototype — `spec/draft/README.md` in `spec` v0.6.2: "An essay, such as "My best day", goes Plan, Write, Improve and ends with "I'm done""; in the table of differences — "A short essay ends with "I'm done", and nothing checks it | An essay gets feedback by the module author's rubric: feedback, not a verdict".

## 10. Protocol

For architecture 3.9: Connect, protobuf, buf, Fetch and CORS, the check time of `tutor` and the channel of canvas-api.

### 10.1. Connect, protobuf and buf, 2026-11-02

For architecture 3.9; R17, R18. Read 2026-11-02 — the source code at the latest releases, cloned into the session's temporary folder, and the pages of documentation, checked against their source code:

- connect-es [v2.3.0](https://github.com/connectrpc/connect-es/tree/v2.3.0) of 2026-10-27 — the latest, commit 6b7c593: `packages/connect-web/src/connect-transport.ts` — "By default, connect-web clients use the JSON format" and `const useBinaryFormat = options.useBinaryFormat ?? false;`, and `useHttpGet` has "Controls whether or not Connect GET requests should be used when available, on side-effect free methods. Defaults to false"; `packages/connect/src/protocol/serialization.ts` — "In connect-es, unknown JSON fields are ignored by default" and `o.ignoreUnknownFields ??= true;`. `connect-node`, by contrast, has "By default, connect-node clients use the binary format";
- protobuf-es [v2.16.0](https://github.com/bufbuild/protobuf-es/tree/v2.16.0) of 2026-09-29 — the latest, commit b3b232f: `packages/protobuf/src/from-json.ts` — `ignoreUnknownFields` has "This option ignores unknown fields in parsing, as well as unrecognized enum string representations"; the reference of the generated code — "Open enums can contain numeric values that are not declared in the generated TypeScript enum, so code that receives data from newer schemas should handle unknown enum numbers"; `bytes` in JSON — "JSON value will be the data encoded as a string using standard base64 encoding with paddings"; `useProtoFieldName` defaults to `false`;
- connect-go [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) of 2026-10-21 — the latest, commit 8f69fe4, the module `connectrpc.com/connect/v2`: the JSON codec in `connectproto/connectproto.go` — `MarshalOptions: protojson.MarshalOptions{Resolver: resolved.resolver}` without `UseProtoNames` and `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, and "Other settings can be configured by mutating the exported fields before use"; `connecthttp/protocol_connect.go` — GET is allowed only for `StreamTypeUnary` with `IdempotencyNoSideEffects`; `WithHTTPGet` has "Note, however, that servers don't automatically set any cache headers";
- [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page: on GET — "These requests do not require any special HTTP headers, and can be viewed in a browser tab. They are designed to be easy to make and easy to cache in browsers, proxies, and CDNs"; the error detail — "the "value" field contains unpadded, base64-encoded binary Protobuf data"; the codes — `invalid_argument` and `failed_precondition` — 400, `not_found` — 404, `permission_denied` — 403, `unauthenticated` — 401, `resource_exhausted` — 429, `unavailable` — 503, `unimplemented` — 501, and "there are no user-defined codes";
- [Connect, CORS](https://connectrpc.com/docs/cors/), no date: "For side-effect-free RPCs such as simple queries, Connect supports GET requests that do not require CORS preflights", if "No request headers must be set"; "If your application uses custom request headers, you must explicitly allow them. This includes commonly used headers such as Authorization";
- [ProtoJSON](https://protobuf.dev/programming-guides/json/), no date: "Adding values to an enum is safe if the "Emit enum values as integers" is set on all relevant clients"; [the behaviour of enums](https://protobuf.dev/programming-guides/enum/), no date: "Proto3 and editions use *open* enums specifically because of the unexpected behavior that *closed* enums cause";
- [buf breaking rules](https://buf.build/docs/breaking/rules/), no date: `RPC_SAME_IDEMPOTENCY_LEVEL` — in the categories `FILE`, `PACKAGE`, `WIRE_JSON` and `WIRE`, "This checks that the idempotency_level RPC option doesn't change. Doing so can result in different HTTP verbs being used".

### 10.2. Fetch, CORS and `<img>`, 2026-11-02

For architecture 3.1 and 3.9; R03, R18. Read 2026-11-02:

- [Fetch Standard](https://fetch.spec.whatwg.org/), "Last Updated 19 October 2026": a request's cache mode — "Unless stated otherwise, it is "default"", and ""default": Fetch will inspect the HTTP cache on the way to the network"; `fetch()` without its own `cache` does not change the mode. `Authorization` is not in the safelist of request headers, and "A CORS non-wildcard request-header name is a header name that is a byte-case-insensitive match for `Authorization`". `Access-Control-Max-Age` — "Indicates the number of seconds (5 by default)", and an entry of the preflight cache holds the URL, and a match requires that "entry's URL equals request's current URL";
- [HTML Standard, "Updating the image data"](https://html.spec.whatwg.org/multipage/images.html#updating-the-image-data), "Last Updated 30 October 2026": the request for an image — "the result of creating a potential-CORS request given urlString, "image", and the current state of element's crossorigin content attribute", and the page's headers are not in it;
- Chromium, [`services/network/cors/preflight_result.cc`](https://github.com/chromium/chromium/blob/98b9eab7ac9e36a7228c5143db756f699c8ec4e1/services/network/cors/preflight_result.cc), commit 98b9eab: "constexpr base::TimeDelta kMaxTimeout = base::Hours(2);"; `preflight_cache.h` — "static constexpr size_t kMaxEntriesPerTopFrameSite = 32u;". Firefox, [`netwerk/protocol/http/nsCORSListenerProxy.cpp`](https://github.com/mozilla-firefox/firefox/blob/abbeff4fd9e02e3f6195153493fc0825eafe5d6d/netwerk/protocol/http/nsCORSListenerProxy.cpp), commit abbeff4: "// Cap at 24 hours.".

### 10.3. The check time of `tutor`, 2026-11-02

For architecture 3.9; R19. [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5 — the latest on `main` on 2026-11-02, `docs/architecture.md`, read at the same time through `git`:

- architecture 3.4 tutor, «Итог замера подсказок» (T76): in the column «p50, с» — «| Opus 5.5 · `high` | … | 5,1 | $0,0152 |»;
- in the same place, «Итог двух вызовов сочинений» (T92): «| Opus 5.5 · `high` | 0 из 198 | 0 из 197 | 1,00 | 2 из 45 | $0,030 | 11,9 |», the last column — «Время p50, с»;
- the documents have no time for the reading call and the pair of readers: the reading report counts «**время** p50 и p95 и цена вызова», but the runs lie outside git, and the pair is counted by the logs of the separate readers — coverage and errors. The documents do not name the order in which the readers of the pair go in the product;
- the judge with the key is the same call with the picture of the work: architecture 3.12 tutor, the series T45, the medians — «Opus 5.5 · `high` | 4 | 4 из 4 | 2,5 с» and «Haiku 5.5 · `low` | 9 | 7 из 8 | 2,7 с»; architecture 3.4 tutor — «**Время ответа**, p50: Opus — 2,5–3,2 с, Haiku · `low` — 2,2–2,8 с» (T66) and «Вызов Opus — $0,012 в форме A и $0,020 в форме D, p50 — 3,0–3,9 с» (T77);
- architecture 3.12 tutor: «Вердикт ждёт только вызова чтения, без ключа, а сильная модель — разбор и подсказка, с ключом — идёт только после «пока нет»».

### 10.4. canvas-api on the channel, 2026-11-02

For architecture 3.9; R19. [mathtrail/canvas-api, `docs/decisions.md`](https://github.com/mathtrail/canvas-api/blob/0901af0/docs/decisions.md), commit 0901af0 — the latest on `main`, read 2026-11-02 through `gh api`: R23 canvas-api — «При оплате Cloud Run за запросы CPU выделяется только на время обработки запроса, и фоновая работа после ответа почти не получает процессора. Поэтому всё остаётся внутри запроса», and the 2026-10-15 amendment — «Всё по-прежнему делается внутри запроса — довод о процессоре после ответа в силе»; R60 canvas-api — «Centrifugo и OpenFGA из стека не уходят: … они приходят в E15. В E2 их нет не потому, что «проверка — один запрос и один ответ», — этот довод … отверг, — а потому, что в E2 нет беты».

### 10.5. Connect v2, 2026-11-09

For architecture 3.9; R33. Read 2026-11-09:

- the module of contracts `contracts/v0.9.11`, and since R40 `contracts/v0.12.0`, where `go.mod` and `apiv1connect/account.connect.go` are the same: `go.mod` requires `connectrpc.com/connect/v2 v2.0.2`, and `apiv1connect/account.connect.go` has `func RegisterAccountServiceHandler(server *connect.Server, svc AccountServiceHandler)`, each method has `if err := stream.Receive(&req); err != nil { return err }` before calling the handler, and `UnimplementedAccountServiceHandler` has `connect.NewError(connect.CodeUnimplemented, …)`;
- `connectrpc.com/connect/v2` [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) — `go 1.26.0`, license Apache-2.0, from the module through `go mod download`; `docs/v2-guide.md`: "an interceptor can reject a call using only its headers, before any payload work, which is what makes authn be able to be an interceptor in v2 rather than HTTP middleware"; "an interceptor can observe individual messages by wrapping the stream before calling `next`"; "only a locally authored `*connect.Error` is serialized to the wire. Any other error reaches the client as a bare code with no message"; "`Mount` installs one `http.Handler` per registered procedure onto a mux"; `connectproto/connectproto.go`: `NewJSONCodec` has `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, and `MarshalOptions` is a field that can be set "before use".

### 10.6. Redirects of the standard library's mux, 2026-11-13

For architecture 3.9; R26. Read 2026-11-13 in the sources of the module, through `go list -m -f '{{.Dir}}'` and `go env GOROOT`:

- Go 1.27.4, `src/net/http/server.go`, the comment of `ServeMux`, "Trailing-slash redirection": "If the ServeMux receives a request for the subtree root without a trailing slash, it redirects the request by adding the trailing slash"; the comment of `ServeMux.Handler`: "If the path is not in its canonical form, the handler will be an internally-generated handler that redirects to the canonical path". The comment does not name the trailing slash there; the code does: `findHandler` returns `RedirectHandler(u.String(), StatusTemporaryRedirect)` for both, and for the clean path `u` keeps `r.URL.RawQuery`. This is a conclusion from the code, which the fuzz test `FuzzRoutes` of `internal/server` holds;
- `connectrpc.com/connect/v2` v2.0.2, `connecthttp/connecthttp.go`, `Mount`: `mux.Handle(spec.Procedure, …)` for each method, and beside them `// "/package.Service/Method" -> subtree pattern "/package.Service/".` with `mux.Handle(service, newUnknownMethodHandler(server, &opts))`; `newUnknownMethodHandler` "routes RPC-shaped POST requests through [connect.Server.Call] and 404s everything else", by `http.NotFound(responseWriter, request)`, whose body in `server.go` of Go is "404 page not found".

## 11. Deletion and export

For architecture 3.10: the sign-in service on deletion and fresh sign-in, RFC 9470, the codes of Connect, canvas-api and `app`, Cloud Run jobs and the `changelog` of OpenFGA.

### 11.1. Firebase Auth on deletion and fresh sign-in, 2026-11-02

For architecture 3.10; R20, R21. Read 2026-11-02:

- the Admin SDK for Go [v4.22.0](https://github.com/firebase/firebase-admin-go/tree/v4.22.0) — the latest release, commit 66a72c3, the sources through `gh api`:
  - `auth/auth.go`: `Token` has ``AuthTime int64 `json:"auth_time"` ``; `VerifyIDTokenAndCheckRevoked` "verifies the provided ID token, and additionally checks that the token has not been revoked or disabled" and on each refusal returns `nil, err`; `checkRevokedOrDisabled` calls `GetUser` and compares `token.IssuedAt*1000 < user.TokensValidAfterMillis`; `hasAuthErrorCode` — `fe, ok := err.(*internal.FirebaseError)`, without `errors.As`;
  - `auth/token_verifier.go`: `clockSkewSeconds = 300`, and `IsIDTokenInvalid` — "An ID token is considered invalid when it is malformed (i.e. contains incorrect data), expired or revoked", without "no such user";
  - `auth/user_mgt.go`: a user that does not exist — "no user exists with the …" with the code `USER_NOT_FOUND`, and `IsUserNotFound` recognizes it; `RevokeRefreshTokens` "updates the user's TokensValidAfterMillis to the current UTC second", and "existing ID tokens may remain active until their natural expiration (one hour)"; `UserMetadata` — "Timestamps are in milliseconds since epoch", and `LastRefreshTimestamp` — "The time at which the user was last active (ID token refreshed), or 0 if the user was never active";
  - `auth/export_users.go`: `Users(ctx, nextPageToken)` iterates over the project's users;
  - `integration/auth/user_mgt_test.go`: `TestDeleteNonExistingUser` — `DeleteUser` of a user that does not exist answers with an error that `IsUserNotFound` recognizes; `integration/auth/auth_test.go`: "If a token was issued, and then in the same second tokens were revoked, the token will have the same timestamp as the tokensValidAfterMillis, and will therefore not be considered revoked";
- the Firebase SDK for the web, `@firebase/auth` [1.13.8](https://github.com/firebase/firebase-js-sdk/tree/@firebase/auth@1.13.8) — commit cc7ead4, the same as `firebase` 13.1.0, the sources through `gh api`:
  - `packages/auth/src/api/errors.ts`: `[ServerError.USER_NOT_FOUND]: AuthErrorCode.TOKEN_EXPIRED`;
  - `packages/auth/src/core/errors.ts`: `TOKEN_EXPIRED = 'user-token-expired'` and "The user's credential is no longer valid. The user must sign in again.";
  - `packages/auth/src/core/user/invalidation.ts`: on `auth/user-disabled` and `auth/user-token-expired` the SDK calls `signOut()` and throws the error on;
  - `packages/auth/src/core/user/reauthenticate.ts`: re-sign-in checks `user.uid === localId`, otherwise `auth/user-mismatch`, and turns `auth/user-not-found` into the same;
  - `packages/auth/src/model/public_types.ts`: `authTime` — "This is the time the user authenticated (signed in) and not the time the token was refreshed".

  That re-sign-in sets a new `auth_time` is said directly nowhere: it is a conclusion from `reauthenticateWithPopup` being a new sign-in at the sign-in service. On the dev project on 2026-11-12 a sign-out and a new sign-in by a popup changed the user's last sign-in time at the sign-in service — an indirect check, not the token itself (T42; the 2026-11-12 amendment to R20);
- [the Firebase Auth REST API](https://firebase.google.com/docs/reference/rest/auth), updated 2026-10-20, exchanging a refresh token for an ID token: "USER_NOT_FOUND: The user corresponding to the refresh token was not found. It is likely the user was deleted.";
- [managing users](https://firebase.google.com/docs/auth/web/manage-users), updated 2026-10-21: "Some security-sensitive actions—such as deleting an account, setting a primary email address, and changing a password—require that the user has recently signed in";
- [session cookies](https://firebase.google.com/docs/auth/admin/manage-cookies), updated 2026-10-20: "For sensitive applications, the auth_time should be checked before issuing the session cookie", and in the sample — `new Date().getTime() / 1000 - decodedIdToken.auth_time < 5 * 60`, otherwise 401;
- [user sessions](https://firebase.google.com/docs/auth/admin/manage-sessions), updated 2026-10-20: "Firebase ID tokens are short lived and last for an hour", and a refresh token stops working when "The user is deleted";
- [connections of a Google account with third-party apps](https://support.google.com/accounts/answer/13533235), no date on the page: "If you delete a link, Google stops automatic sign-in to the app. This doesn't delete your data on the app". Whether deleting the user at the app removes the connection, the page does not say.

### 11.2. RFC 9470, 2026-11-02

For architecture 3.10; R20. [OAuth 2.0 Step Up Authentication Challenge Protocol](https://www.rfc-editor.org/rfc/rfc9470), September 2023, read 2026-11-02: "insufficient_user_authentication: The authentication event associated with the access token presented with the request does not meet the authentication requirements of the protected resource"; "max_age: This value indicates the allowable elapsed time in seconds since the last active authentication event associated with the access token"; in the sample response — `HTTP/1.1 401 Unauthorized` with `max_age="5"`.

### 11.3. Connect codes, 2026-11-02

For architecture 3.10; R20. [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page, read 2026-11-02: `unauthenticated` — "Caller doesn't have valid authentication credentials for the operation", and "use `unauthenticated` if the user can't be identified or presents invalid credentials"; `failed_precondition` — "if the client must explicitly fix the system state before retrying".

### 11.4. Deletion in canvas-api, 2026-11-02

For architecture 3.10; R21. [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0, read 2026-11-02 through `gh api`:

- R27 canvas-api: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен», «Кортежи удаляются пачками до 100, отсутствующие пропускаются», «Аккаунт удаляется в порядке «дети → `parents` → отзыв токенов и `DeleteUser`»»;
- spec 9.8 canvas-api: «Повтор после сбоя на `DeleteUser` видит, что строки `parents` уже нет, и просто доводит удаление в Firebase»; «Другие устройства родителя стирают черновики, когда там откроется SPA и узнает об удалении»;
- spec 12.1 canvas-api: «Иначе ответ `REAUTH_REQUIRED`», and in the table of codes — «`REAUTH_REQUIRED` | 401 | нужен свежий вход: `auth_time` старше 5 минут»;
- privacy 7 canvas-api: «Удаление требует свежего входа: PIN не подходит, ребёнок может его знать». П-7 canvas-api — «Хранить ли доказательство согласия после удаления аккаунта», and «решает юрист».

There is neither a job that completes the deletion nor an answer to the other devices there: the retry was led by the client.

### 11.5. `app` on erasing on the device, 2026-11-02

For architecture 3.10; R20. [altnovatrail/app, `docs/architecture.md`](https://github.com/altnovatrail/app/blob/375a09b/docs/architecture.md), commit 375a09b, read 2026-11-02 through `gh api`. In architecture 3.2 app, «Когда что стирается»: «выход владельца аккаунта — всё аккаунта: базы всех профилей, общий кеш, реестр и сессия»; «вход другого аккаунта — если в реестре другой ID аккаунта, `app` стирает всё прежнее раньше, чем что-либо откроет … Так бывает, когда прежний вход кончился без выхода»; «удаление аккаунта — `api` отвечает, что аккаунта больше нет, и `app` стирает всё аккаунта и выходит».

### 11.6. Cloud Run jobs, 2026-11-09

For architecture 3.10; R38. Read 2026-11-09:

- [the container contract](https://docs.cloud.google.com/run/docs/container-contract), no date: for a Cloud Run job it sets `CLOUD_RUN_JOB` — "The name of the Cloud Run job being run", and for a service `K_SERVICE`, "The name of the Cloud Run service being run";
- [the task timeout](https://docs.cloud.google.com/run/docs/configuring/task-timeout), no date: "By default, each task runs for a maximum of 10 minutes", and the largest — "up to 168 hours (7 days)"; "If the task attempt does not complete within this time, it will be stopped".

### 11.7. The `changelog` of OpenFGA, 2026-11-09

For architecture 3.10; R38. `assets/migrations/postgres/001_initialize_schema.sql` in [openfga/openfga v1.22.1](https://github.com/openfga/openfga/blob/v1.22.1/assets/migrations/postgres/001_initialize_schema.sql), read 2026-11-09: `CREATE TABLE changelog` with the fields `store`, `object_type`, `object_id`, `relation`, `_user`, `operation`, `ulid`, `inserted_at TIMESTAMPTZ NOT NULL` and the key `PRIMARY KEY (store, ulid, object_type)`; there is no index by time.

## 12. canvas-api: decisions and specification

A reading that several sections rest on at once — architecture 3.1, 3.2, 3.6, 3.9 and 3.10.

### 12.1. The decisions and specification of canvas-api, 2026-10-19

For architecture 3.1, 3.2, 3.6, 3.9 and 3.10. [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), the branch `main`, commit 0901af0, read 2026-10-19. The repository was cloned into the session's temporary folder (RULES.md, Citations). The quotes are word for word:

- R09 canvas-api: «OpenFGA работает сайдкар-контейнером в сервисе canvas-api», «хранилище OpenFGA — отдельная база в том же проекте Neon»;
- R14 canvas-api: «Все запросы, кроме горячего пути — выдачи токенов Centrifugo, — проверяют токен с отзывом (`VerifyIDTokenAndCheckRevoked`)»; «Перед `DeleteUser` вызывается `RevokeRefreshTokens`»; «Необратимые действия требуют свежего входа (`auth_time` ≤ 5 минут)»;
- spec 12.1 canvas-api: «**Свежий вход** — `auth_time` не старше 5 минут — нужен для удаления профиля ребёнка, удаления аккаунта и смены забытого PIN. Иначе ответ `REAUTH_REQUIRED`, и SPA просит войти заново»;
- spec 12.2 canvas-api: «PIN — барьер интерфейса, а не граница безопасности»; «Поэтому необратимые действия требуют свежего входа, а не PIN»;
- R27 canvas-api: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен»;
- spec 9.8 canvas-api: «Дневные счётчики родителя остаются: иначе удаление и новый профиль снимали бы потолок трат»;
- R29 canvas-api: «Доступ в бету — по одноразовым инвайт-кодам», «Email canvas-api не читает»;
- R35 canvas-api: «Исчерпанный дневной лимит не прерывает занятие: ребёнок решает дальше сам, без проверки»; «Сделанный вызов засчитывается, даже если он не удался или вышел по тайм-ауту»;
- R23 canvas-api: «HTTP-ответ приходит последним и повторяет все события на случай разрыва WebSocket»;
- R41 canvas-api: «Разбор черновика идёт секунды, поэтому ранний `verdict` по WebSocket по-прежнему нужен»; rejected by the author on 2026-10-11 — «убрать Centrifugo и Upstash и отдавать всё в HTTP-ответе», «убрать OpenFGA и проверять права кодом в Postgres»; «Довод «проверка теперь — один запрос и один ответ» без новых фактов повторно не предлагать».
- read 2026-11-02 for architecture 3.1, the same commit, through `gh api`:
  - R06 canvas-api: «Модель OpenFGA заранее описывает учителя и класс, но кортежей для них MVP не пишет»;
  - R07 canvas-api: «Субъект доступа — всегда родитель: запросы идут с его Firebase ID token, профиль ребёнка передаётся параметром»;
  - R08 canvas-api: «система сразу готова к realtime и B2B»;
  - R26 canvas-api: «Статус `verified` ставит оператор командой `cmd/admin` после того, как получил подписанную форму (П-1)»; «До `verified` профили детей и холсты не создаются»;
  - R28 canvas-api: «Сервис находит или создаёт store `canvas`, сравнивает последнюю модель со встроенной и при расхождении записывает новую»;
  - spec 9.1 canvas-api: «пишется внутри транзакции Neon, до `COMMIT`» — about the OpenFGA tuple; «Он безвреден: права на пустой объект ничего не дают. Его удаляет ежедневная очистка» — about a "dangling" tuple;
  - spec 12.1 canvas-api: «Firebase Auth, провайдеры Google и Apple. Для Apple нужно членство в Apple Developer Program»;
  - spec 12.2 canvas-api: «не больше 5 попыток за 15 минут»; «На сервере хранится только хеш argon2id»; «меняется после свежего входа, без старого PIN» — about a forgotten PIN.
- read 2026-11-02 for architecture 3.2, the same commit, through `gh api`:
  - R08 canvas-api: in the stack — «Neon Postgres, Upstash Redis»; rejected — «Firestore вместо Postgres»;
  - R54 canvas-api: «Neon: `aws-us-east-1` — та же агломерация, задержка до базы около 1–2 мс. У Neon регионы только в AWS (документация Neon, прочитана 2026-10-14)» — about the pairing with Cloud Run in `us-east4`; rejected — `us-central1`: «ближайший Neon — `aws-us-east-2` (Огайо), около 15–20 мс на каждый запрос к базе»;
  - R27 canvas-api: «`changelog` OpenFGA сам не чистится и хранил бы связи «uid ↔ ребёнок» бессрочно»; «Ежедневная очистка удаляет «висящие» кортежи и таблицу `changelog` OpenFGA старше суток»;
  - spec 3.2 canvas-api: migrations — «`github.com/pressly/goose/v3` | v3.28.0»; spec 11 canvas-api: «Она запускается как Cloud Run job с образом нового релиза до выкладки сервиса»; spec 15.5 canvas-api: migrations — «по схеме «сначала добавить, потом убрать»»;
  - spec 3.3 canvas-api: «Neon | Free | 0,5 ГБ на проект, 100 CU-часов в месяц, автопауза через 5 мин (не отключается), восстановление на момент времени — 6 часов»;
  - privacy 7 canvas-api: the database `canvas` is deleted «сразу; из истории восстановления (PITR) — через 6 часов», and in the database `openfga` «`changelog` — ежедневной очисткой, до суток; из PITR — через 6 часов после этого»;
  - privacy 8 canvas-api: a child's profile with no activity for 12 months «удаляется автоматически, без отдельного уведомления. Этот срок заранее указан в уведомлении при согласии: email у нас нет, писать некуда (П-6)».

## 13. Checks of the code

For architecture 1, where the checks of CI are named, and R27.

### 13.1. Go's fuzzing and Scorecard's check Fuzzing, 2026-11-13

For architecture 1; R27. Read 2026-11-13, no date on the pages:

- [Go Fuzzing](https://go.dev/doc/security/fuzz/): "Fuzz tests are run much like a unit test by default. Each seed corpus entry will be tested against the fuzz target, reporting any failures before exiting"; "To enable fuzzing, run go test with the -fuzz flag, providing a regex matching a single fuzz test"; "The fuzzing engine wrote this failing input to the seed corpus for that fuzz test, and it will now be run by default with go test, serving as a regression test once the bug has been fixed"; the seed corpus "is composed of the corpus entries provided by f.Add calls within the fuzz test, and the files in the testdata/fuzz/{FuzzTestName} directory within the package. These entries are run by default with go test, whether fuzzing or not";
- [Scorecard's checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md#fuzzing), the branch `main`: "This check tries to determine if the project uses fuzzing by checking: … 3. if there are user-defined language-specified fuzzing functions in the repository. - currently only supports Go fuzzing", beside OSS-Fuzz and ClusterFuzzLite; "Regular fuzzing is important to detect vulnerabilities that may be exploited by others".

### 13.2. The project of `api` on bestpractices.dev and Scorecard's check CII-Best-Practices, 2026-11-13

For R27. Read 2026-11-13, 15:20 UTC:

- [the project 16661](https://www.bestpractices.dev/projects/16661.json): `"repo_url":"https://github.com/altnovatrail/api"`, `"badge_level":"passing"`, `"badge_percentage_0":100`, `"created_at":"2026-11-13T15:18:40.577Z"`, `"achieved_passing_at":"2026-11-13T15:18:52.292Z"`. Its 67 statuses of the level passing and 63 justifications are those of `.bestpractices.json`; `floss_license_justification` and `floss_license_osi_justification` are "The Apache-2.0 license is approved by the Open Source Initiative (OSI).", `repo_track_justification` is "Repository on GitHub, which uses git. git can track the changes, who made them, and when they were made.", and `repo_distributed_justification` "Repository on GitHub, which uses git. git is distributed.";
- [the projects by the address](https://www.bestpractices.dev/en/projects.json?url=https%3A%2F%2Fgithub.com%2Faltnovatrail%2Fapi): `[{"id":16661,"badge_level":"passing","repo_url":"https://github.com/altnovatrail/api"}]`;
- [Scorecard's checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md#cii-best-practices), the branch `main`: "The check uses the URL for the Git repo and the OpenSSF Best Practices badge API"; "[passing badge](https://www.bestpractices.dev/criteria/0): 5";
- [Scorecard's run of `api`](https://api.scorecard.dev/projects/github.com/altnovatrail/api): `"date":"2026-11-13T15:17:59Z"`, the commit 5c7caec, `CII-Best-Practices` — `"score":0`, "no effort to earn an OpenSSF best practices badge detected"; `Fuzzing` — `"score":10`, "project is fuzzed";
- the open findings of code scanning (`gh api 'repos/altnovatrail/<repository>/code-scanning/alerts'`): of Scorecard at `api` — `CIIBestPracticesID`, `CodeReviewID`, `MaintainedID`, `PinnedDependenciesID` and `BranchProtectionID`, `FuzzingID` no longer among them; at `spec`, `app`, `module-kit` and `tutor`, whose projects have the level passing and the score 5, `CIIBestPracticesID` is fixed.
