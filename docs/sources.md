# api — sources

> The facts of the outside world the repository's documents under `docs/` rest on and no run of ours checks: a law, a regulator, a standard's normative text, a provider's terms and policies, a price, a license, a page that changes in place with no version, a text outside the repositories. Here are their records — the address, the date it was read and a quote in the source's language — and [architecture.md](architecture.md) and the decision log hold the claim and a citation of its one record by its number, `sources 1.1`, not a chain. A record's heading names its topic, not a task.
>
> No record is needed for a library's documentation or code at a version or commit — a link beside the claim —, for a behaviour a test or a measurement checks — they are its evidence —, or for spec's tags and the files of NovaTrail's and MathTrail's repositories — git, at a commit.
>
> A record is a subsection "N.M" of a topic: one source, or one reading of several sources for one decision. A record's number never changes and is never given again: a new one goes at the end of its topic, a new topic at the end of the file, and a record removed leaves a gap. A source read again with the same text changes the record's date; with another text, the claims and decisions on it are rewritten in the same change and the record holds the new reading alone — where a decision waits on the author, an open question, and both readings stand until it is closed. The model is `spec/docs/sources.md`. The results of measurements are not here but in [measurements.md](measurements.md).

## 1. How to keep it

- **Topics** 2–11 follow the parts of the service in the order [architecture.md](architecture.md) describes them; a topic for another part or for several at once goes at the end of the file.
- **What was not read in the primary source** — a copy, a retelling, a page that did not open — the record names as such.
- **From another repository** a record is cited with the repository's name: `sources 7.1 api`.
- **A citation inside a quote** — of a decision, a section, a document or a task of the source — is left out as «[…]»: the quote is found in its source by its words.
- **What is not here:** quotes that an entry of the decision log gives itself — they stay in the entry.

## 2. Sign-in and permissions

The sign-in service and its ID token, Apple's program, OpenFGA, a shared volume and the hash of the PIN.

### 2.1. Firebase Auth, 2026-11-02

For the choice of the sign-in service. Read 2026-11-02:

- [privacy in Firebase](https://firebase.google.com/support/privacy), updated 2026-10-29: "The Firebase Authentication service is run only from US data centers. As a result, Firebase Authentication processes data exclusively in the United States"; "Firebase Authentication keeps logged IP addresses for a few weeks"; after a user is deleted "data is removed from live and backup systems within 180 days"; among the data it processes — "User agents" and "IP addresses";
- [Firebase Authentication](https://firebase.google.com/docs/auth), updated 2026-10-20: "Firebase Authentication with Identity Platform is an optional upgrade that adds several new features to Firebase Authentication"; "When upgraded, no-cost (Spark) plan projects will be limited to 3,000 daily active users, and pay-as-you-go (Blaze) plan projects will be charged for usage beyond the free tier of 50,000 monthly active users";
- [Firebase pricing](https://firebase.google.com/pricing), no date on the page: the row "Other Authentication services" is marked for both the Spark plan and Blaze, with no price. The rows of active users stand under "With Identity Platform": on Blaze — "No-cost up to 50K MAUs", "Then Google Cloud pricing";
- [verifying an ID token](https://firebase.google.com/docs/auth/admin/verify-id-tokens), updated 2026-10-20: `auth_time` — "Must be in the past. The time when the user authenticated";
- [user sessions](https://firebase.google.com/docs/auth/admin/manage-sessions), updated 2026-10-20: the revocation check on the server — "an expensive operation, requiring an extra network round trip";
- [the Authentication emulator](https://firebase.google.com/docs/emulator-suite/connect_auth), no date: "For security reasons, the Authentication emulator issues unsigned ID tokens"; "Please make sure not to set the environment variable in production";
- [sign-in with Apple](https://firebase.google.com/docs/auth/web/apple), no date: "Sign In with Apple can only be configured by members of the Apple Developer Program".

### 2.2. Apple Developer Program, 2026-11-02

For sign-in with Apple; Д3. [the enrollment terms](https://developer.apple.com/programs/enroll/), no date, read 2026-11-02: "The Apple Developer Program is 99 USD per membership year".

This record is a reading of 2026-11-02, not a fact a decision of `api` rests on now: Т-41 of `spec` says "Apple's pages of the program did not give the price, read 2026-11-14", and that the price and terms are read from Apple, into a record of sources, when the question is decided. `api` has not read Apple's pages again.

### 2.4. OpenFGA as a library, 2026-11-08

For OpenFGA in the process of `api`. Read 2026-11-08:

- [on consistency](https://openfga.dev/docs/interacting/consistency), no date: "caching is disabled by default. When caching is disabled, all queries will have strong consistency regardless of the consistency mode specified";
- the license of the MySQL driver that OpenFGA's package of migrations imports: [go-sql-driver/mysql v1.10.1](https://github.com/go-sql-driver/mysql/blob/v1.10.1/LICENSE) has "Mozilla Public License Version 2.0".

### 2.6. An ID token without `app`, 2026-11-09

For the first run on the dev projects. Read 2026-11-09:

- [the Firebase Auth REST API](https://firebase.google.com/docs/reference/rest/auth), no date: "Exchange custom token for an ID and refresh token" — `POST https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=[API_KEY]`, the body `token` and `returnSecureToken`, in the response `idToken`, `refreshToken` and `expiresIn`; "API_KEY refers to the Web API Key, which can be obtained on the project settings page in your admin console";
- [custom token](https://firebase.google.com/docs/auth/admin/create-custom-tokens), no date: the claims of a token made by a third-party JWT library — `alg` `"RS256"`, `iss` and `sub` — "Your project's service account email address", `aud` — `"https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"`, `exp` — "a maximum of 3600 seconds later than the `iat`", `uid` — "between 1-128 characters long, inclusive".

### 2.7. A volume two devcontainers share, 2026-11-13

For the local build of `app` beside `api`. Read 2026-11-13, [Volumes](https://docs.docker.com/engine/storage/volumes/) of the Docker documentation, no date:

- "Multiple containers can mount the same volume. You can simultaneously mount a single volume as `read-write` for some containers and as `read-only` for others.";
- the option `readonly` of `--mount`: "If present, causes the volume to be mounted into the container as read-only.";
- "If you start a container which creates a new volume, and the container has files or directories in the directory to be mounted such as `/app/`, Docker copies the directory's contents into the volume." Whether the owner and the mode of the directory itself pass to the volume, the page does not say.

### 2.8. OWASP on the parameters of argon2id, 2026-11-23

For the hash of the PIN. The [Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) of OWASP, no date on the page, read 2026-11-23: "Use Argon2id with a minimum configuration of 19 MiB of memory, an iteration count of 2, and 1 degree of parallelism.", and among the minimum configurations "m=19456 (19 MiB), t=2, p=1 (Do not use with Argon2i)".

## 3. Database and learner model

Neon, its pooler and the rejected Cloud SQL.

### 3.1. Neon, 2026-11-02

For the choice of the database. Read 2026-11-02:

- [regions](https://neon.com/docs/introduction/regions), the IP list updated 2026-10-28: "Neon offers project deployment in multiple AWS regions"; among them `aws-us-east-1` (N. Virginia), `aws-us-east-2` (Ohio), `aws-us-west-2`, `aws-eu-central-1` (Frankfurt) and `aws-eu-west-2` (London); "You can no longer create new projects in Azure regions". There are no Google Cloud regions in the list;
- [pricing](https://neon.com/pricing), "Last updated on October 31, 2026": Free has "100 CU-hours Per Project", "1 GB per Project", the window — "Up to 6 hours or 1 GB of data changes"; Launch has "$0.106 per CU-hour", "$0.35 per GB-month", history — "$0.20 per GB-month", the window "Up to 7 days", suspension — "After 5 minutes can be disabled", and the spending estimate — "Typical spend: $ 15 /mo Based on: Intermittent load , 1 GB". The answer to the question about the Free plan: "When a database is idle, its compute scales to zero after 5 minutes and starts again on the next query, typically within a few hundred milliseconds" and "Compute is suspended only if a project uses up its monthly 100 CU-hours or 5 GB of network transfer";
- [the history window](https://neon.com/docs/postgres/backup-restore/history-window), no date: the window "sets how long that change history is kept, which defines how far back instant restore and the other features can reach"; the default and the largest — "Free 6 hours 6 hours (capped at 1 GB of history) Launch 1 day 7 days Scale 1 day 30 days";
- [how to reduce costs](https://neon.com/docs/introduction/cost-optimization), no date: "Be aware that delete operations generate WAL records that temporarily add to your instant restore storage until they age out of your history window";
- [databases](https://neon.com/docs/manage/databases), no date: "In the Neon object model, a database exists within a branch of a project. There is a limit of 500 databases per branch";
- [security](https://neon.com/docs/security/security-overview), no date: "Neon requires that all connections use SSL/TLS encryption";
- [compliance](https://neon.com/docs/security/compliance), no date: "We have completed audits for SOC 2 Type 1 and Type 2, SOC 3, ISO 27001, and ISO 27701, and we adhere to GDPR and CCPA regulations";
- [Neon's terms](https://neon.com/platform-terms) — the address `neon.com/dpa` leads here, "Last Updated: October 14, 2026": "This Product Specific Schedule (Neon) … is entered into as of the Effective Date between Databricks, Inc., the parent company of Neon, LLC ("Databricks"), and Customer"; the schedule changes sections of the Databricks DPA. It says nothing about children or minors;
- [subprocessors](https://www.databricks.com/legal/databricks-subprocessors) — `neon.com/subprocessors` leads here, "Last Updated: June 9, 2026": "Amazon Web Services, Inc. Cloud Service Provider United States Customer Selected".

### 3.2. Cloud SQL for PostgreSQL, 2026-11-02

For the choice of the database — the option it rejected. Read 2026-11-02:

- [pricing](https://cloud.google.com/sql/pricing), no date: "Instance pricing is charged for every second that the instance is running (the activation policy is set to ALWAYS)";
- [starting and stopping](https://docs.cloud.google.com/sql/docs/postgres/start-stop-restart-instance), "Last updated 2026-10-26 UTC": "If you are not using your instance, you can set its activation policy to NEVER to avoid instance charges" — that is, stopping by hand, not when idle;
- [the free trial instance](https://docs.cloud.google.com/sql/docs/postgres/free-trial-instance), "Last updated 2026-10-26 UTC": "A Cloud SQL free trial instance lets you test the majority of Cloud SQL features for up to 30 days without any financial commitment".

### 3.5. Neon's pooler, 2026-11-08

For the driver of the database and the migrations. [Connection pooling](https://neon.com/docs/connect/connection-pooling) of Neon, updated 2026-10-19, read 2026-11-08: "Neon uses PgBouncer in transaction mode ( pool_mode=transaction )"; "max_prepared_statements=1000 … Maximum protocol-level prepared statements per connection"; "Not supported with pooled connections: SET / RESET (session variables) LISTEN / NOTIFY WITH HOLD CURSOR PREPARE / DEALLOCATE (SQL-level prepared statements)", and also "Session-level advisory locks"; in the table of choice — "Schema migrations \| Direct \| Tools may not support transaction pooling".

## 4. Module versions and packages in the image

The checks of CI and a scheduled job of GitHub Actions.

### 4.2. Required checks and the path filter, 2026-11-02

For the checks of CI on the module packages. [GitHub Actions workflow syntax](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions), no date on the page, read 2026-11-02: "If a workflow is skipped due to path filtering, branch filtering, or a commit message, then checks associated with that workflow will remain in a 'Pending' state. A pull request that requires those checks to be successful will be blocked from merging".

### 4.4. The scheduled workflow and its signal, 2026-11-12

For the job that notices a withdrawn version. Read 2026-11-12, no date on the pages:

- [workflow events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows), `schedule`: "High load times include the start of every hour"; "Scheduled workflows run on the latest commit on the default branch"; "In a public repository, scheduled workflows are automatically disabled when no repository activity has occurred in 60 days"; on a pull request and other events from `GITHUB_TOKEN` — "other `GITHUB_TOKEN`-triggered events do not create workflow runs at all";
- [notifications for runs](https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs): "Notifications for scheduled workflows are sent to the user who initially created the workflow"; if someone else changes the cron line, "subsequent notifications will be sent to that user instead", and if the workflow was disabled and enabled again, the notifications go to "the user who re-enabled the workflow"; "You can also choose to receive a notification only when a workflow run has failed";
- [the repository's Actions settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository): "You can choose to allow or prevent GitHub Actions workflows from creating or approving pull requests". `api` has `default_workflow_permissions: read` and `can_approve_pull_request_reviews: false` (`gh api repos/altnovatrail/api/actions/permissions/workflow`);
- [the latest release](https://docs.github.com/en/rest/releases/releases#get-the-latest-release): "The latest release is the most recent non-prerelease, non-draft release, sorted by the created_at attribute"; when creating a release, `make_latest` defaults to `true`; the public `https://github.com/altnovatrail/modules/releases/latest` redirects to `…/releases/tag/v0.2.5` (`curl -sI`);
- [Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions): "The following usage remains free independently and does not reduce the included compute usage: Standard GitHub-hosted runners in public repositories".

## 7. Platform AI quota

The provider's prices and what it charges for.

### 7.1. Platform AI quota, 2026-11-02

For the quota in dollars and its reserve. Read 2026-11-02:

- Anthropic's prices — [Pricing](https://platform.claude.com/docs/en/about-claude/pricing), no date on the page: per million input and output tokens — "Claude Opus 5.5 … $4 / MTok … $20 / MTok", "Claude Sonnet 5.5 … $2 / MTok … $10 / MTok", "Claude Haiku 5.5 … $0.10 / MTok for prompts up to 100,000 tokens … $0.50 / MTok", "Claude Fable 5.1 … $10 / MTok … $50 / MTok". [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/thinking), no date: "Thinking has a cost: the tokens Claude spends reasoning are billed as output tokens, even when the thinking text isn’t returned to you". [Vision](https://platform.claude.com/docs/en/build-with-claude/vision), no date: "An image, therefore, costs ⌈width / 28⌉ × ⌈height / 28⌉ visual tokens";
- payment for a cut-off request — [How do I pay for my Claude API usage?](https://support.claude.com/en/articles/8977456-how-do-i-pay-for-my-claude-api-usage), no date on the page: "You're billed only for successful API calls and completed tasks. Failed requests aren't charged"; "If your client disconnects or times out in the middle of a request that was on track to succeed, that request is still charged";
- the prices were checked again 2026-11-11 and 2026-11-12, for measurements 2.1 and 3: the body of the task of 2026-11-12 retells the same prices, and that of 2026-11-11 names none. This is known not from the primary source but from the bodies of those tasks in [docs/plan/E3.md](plan/E3.md) and [docs/plan/E6.md](plan/E6.md), with no quote.

## 10. Protocol

Connect, protobuf, buf, Fetch and CORS, the check of an own key at Anthropic and the paths Cloud Run keeps.

### 10.1. Connect, protobuf and buf, 2026-11-02

For the protocol and what goes on the wire. Read 2026-11-02:

- [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page: on GET — "These requests do not require any special HTTP headers, and can be viewed in a browser tab. They are designed to be easy to make and easy to cache in browsers, proxies, and CDNs"; the error detail — "the "value" field contains unpadded, base64-encoded binary Protobuf data"; the codes — `invalid_argument` and `failed_precondition` — 400, `not_found` — 404, `permission_denied` — 403, `unauthenticated` — 401, `resource_exhausted` — 429, `unavailable` — 503, `unimplemented` — 501, and "there are no user-defined codes";
- [Connect, CORS](https://connectrpc.com/docs/cors/), no date: "For side-effect-free RPCs such as simple queries, Connect supports GET requests that do not require CORS preflights", if "No request headers must be set"; "If your application uses custom request headers, you must explicitly allow them. This includes commonly used headers such as Authorization";
- [ProtoJSON](https://protobuf.dev/programming-guides/json/), no date: "Adding values to an enum is safe if the "Emit enum values as integers" is set on all relevant clients"; [the behaviour of enums](https://protobuf.dev/programming-guides/enum/), no date: "Proto3 and editions use *open* enums specifically because of the unexpected behavior that *closed* enums cause";
- [buf breaking rules](https://buf.build/docs/breaking/rules/), no date: `RPC_SAME_IDEMPOTENCY_LEVEL` — in the categories `FILE`, `PACKAGE`, `WIRE_JSON` and `WIRE`, "This checks that the idempotency_level RPC option doesn't change. Doing so can result in different HTTP verbs being used".

### 10.2. Fetch, CORS and `<img>`, 2026-11-02

For the token in a header and what goes on the wire. Read 2026-11-02:

- [Fetch Standard](https://fetch.spec.whatwg.org/), "Last Updated 19 October 2026": a request's cache mode — "Unless stated otherwise, it is "default"", and ""default": Fetch will inspect the HTTP cache on the way to the network"; `fetch()` without its own `cache` does not change the mode. `Authorization` is not in the safelist of request headers, and "A CORS non-wildcard request-header name is a header name that is a byte-case-insensitive match for `Authorization`". `Access-Control-Max-Age` — "Indicates the number of seconds (5 by default)", and an entry of the preflight cache holds the URL, and a match requires that "entry's URL equals request's current URL";
- [HTML Standard, "Updating the image data"](https://html.spec.whatwg.org/multipage/images.html#updating-the-image-data), "Last Updated 30 October 2026": the request for an image — "the result of creating a potential-CORS request given urlString, "image", and the current state of element's crossorigin content attribute", and the page's headers are not in it.

### 10.7. The check of an own key at Anthropic, 2026-11-14

For the check of an own key. Read 2026-11-14, by the `.md` versions of the pages, before the real check that calls `count_tokens` with the author's dev key:
- [Token counting](https://platform.claude.com/docs/en/build-with-claude/token-counting), its section "Pricing and rate limits": "Token counting is **free to use** but subject to requests per minute rate limits based on your [usage tier]", the table — "| Start | 5,000 |", "| Build | 10,000 |", "| Scale | 20,000 |" requests per minute —, and "Token counting and message creation have separate and independent rate limits. Usage of one does not count against the limits of the other.";
- [Errors](https://platform.claude.com/docs/en/api/errors): "401 - `authentication_error`: There’s an issue with your [API key] (for example, it’s malformed, revoked, or expired …)"; "403 - `permission_error`: Your API key does not have permission to use the specified resource."; and on a limit one sets oneself: "The API also returns a 400 when usage reaches an organization or workspace [spend limit you set]". Whether `count_tokens` meets this 400 the page does not say.

### 10.8. The paths Cloud Run keeps, 2026-11-23

For the path of the health check. [Cloud Run known issues](https://docs.cloud.google.com/run/docs/known-issues), "Reserved URL paths", no date on the page, read 2026-11-23: "You can't use the following URL paths: Paths starting with /_ah/; Some paths ending with z. To prevent conflicts with reserved paths, we recommend avoiding all paths that end in z".

## 11. Deletion and export

The sign-in service on deletion and fresh sign-in, RFC 9470, the codes of Connect, Cloud Run jobs and the size of a response of Cloud Run.

### 11.1. Firebase Auth on deletion and fresh sign-in, 2026-11-02

For the fresh sign-in and the deletion of a profile. Read 2026-11-02:

- [the Firebase Auth REST API](https://firebase.google.com/docs/reference/rest/auth), updated 2026-10-20, exchanging a refresh token for an ID token: "USER_NOT_FOUND: The user corresponding to the refresh token was not found. It is likely the user was deleted.";
- [managing users](https://firebase.google.com/docs/auth/web/manage-users), updated 2026-10-21: "Some security-sensitive actions—such as deleting an account, setting a primary email address, and changing a password—require that the user has recently signed in";
- [session cookies](https://firebase.google.com/docs/auth/admin/manage-cookies), updated 2026-10-20: "For sensitive applications, the auth_time should be checked before issuing the session cookie", and in the sample — `new Date().getTime() / 1000 - decodedIdToken.auth_time < 5 * 60`, otherwise 401;
- [user sessions](https://firebase.google.com/docs/auth/admin/manage-sessions), updated 2026-10-20: "Firebase ID tokens are short lived and last for an hour", and a refresh token stops working when "The user is deleted";
- [connections of a Google account with third-party apps](https://support.google.com/accounts/answer/13533235), no date on the page: "If you delete a link, Google stops automatic sign-in to the app. This doesn't delete your data on the app". Whether deleting the user at the app removes the connection, the page does not say.

### 11.2. RFC 9470, 2026-11-02

For the fresh sign-in. [OAuth 2.0 Step Up Authentication Challenge Protocol](https://www.rfc-editor.org/rfc/rfc9470), September 2023, read 2026-11-02: "insufficient_user_authentication: The authentication event associated with the access token presented with the request does not meet the authentication requirements of the protected resource"; "max_age: This value indicates the allowable elapsed time in seconds since the last active authentication event associated with the access token"; in the sample response — `HTTP/1.1 401 Unauthorized` with `max_age="5"`.

### 11.3. Connect codes, 2026-11-02

For the fresh sign-in. [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page, read 2026-11-02: `unauthenticated` — "Caller doesn't have valid authentication credentials for the operation", and "use `unauthenticated` if the user can't be identified or presents invalid credentials"; `failed_precondition` — "if the client must explicitly fix the system state before retrying".

### 11.6. Cloud Run jobs, 2026-11-09

For the daily retention job. Read 2026-11-09:

- [the container contract](https://docs.cloud.google.com/run/docs/container-contract), no date: for a Cloud Run job it sets `CLOUD_RUN_JOB` — "The name of the Cloud Run job being run", and for a service `K_SERVICE`, "The name of the Cloud Run service being run";
- [the task timeout](https://docs.cloud.google.com/run/docs/configuring/task-timeout), no date: "By default, each task runs for a maximum of 10 minutes", and the largest — "up to 168 hours (7 days)"; "If the task attempt does not complete within this time, it will be stopped".

### 11.8. The size of a response of Cloud Run, 2026-11-14

For the export and the stored works. [Cloud Run quotas and limits](https://docs.cloud.google.com/run/quotas), read 2026-11-14: "Maximum HTTP/1 response size: 32 MiB per response. Limit applies if not using Transfer-Encoding: chunked or streaming". The export of `api` is one unary response of Connect, whose `api.zip` holds the profile's stored works, each up to 4 MiB.

## 13. Checks of the code

The checks of CI on the Go code.

### 13.1. Go's fuzzing and Scorecard's check Fuzzing, 2026-11-13

For the fuzz tests in CI. Read 2026-11-13, no date on the pages:

- [Go Fuzzing](https://go.dev/doc/security/fuzz/): "Fuzz tests are run much like a unit test by default. Each seed corpus entry will be tested against the fuzz target, reporting any failures before exiting"; "To enable fuzzing, run go test with the -fuzz flag, providing a regex matching a single fuzz test"; "The fuzzing engine wrote this failing input to the seed corpus for that fuzz test, and it will now be run by default with go test, serving as a regression test once the bug has been fixed"; the seed corpus "is composed of the corpus entries provided by f.Add calls within the fuzz test, and the files in the testdata/fuzz/{FuzzTestName} directory within the package. These entries are run by default with go test, whether fuzzing or not";
- [Scorecard's checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md#fuzzing), the branch `main`: "This check tries to determine if the project uses fuzzing by checking: … 3. if there are user-defined language-specified fuzzing functions in the repository. - currently only supports Go fuzzing", beside OSS-Fuzz and ClusterFuzzLite; "Regular fuzzing is important to detect vulnerabilities that may be exploited by others".

### 13.2. The project of `api` on bestpractices.dev and Scorecard's check CII-Best-Practices, 2026-11-13

For the badge of best practices. Read 2026-11-13, 15:20 UTC:

- [the project 16661](https://www.bestpractices.dev/projects/16661.json): `"repo_url":"https://github.com/altnovatrail/api"`, `"badge_level":"passing"`, `"badge_percentage_0":100`, `"created_at":"2026-11-13T15:18:40.577Z"`, `"achieved_passing_at":"2026-11-13T15:18:52.292Z"`. Its 67 statuses of the level passing and 63 justifications are those of `.bestpractices.json`; `floss_license_justification` and `floss_license_osi_justification` are "The Apache-2.0 license is approved by the Open Source Initiative (OSI).", `repo_track_justification` is "Repository on GitHub, which uses git. git can track the changes, who made them, and when they were made.", and `repo_distributed_justification` "Repository on GitHub, which uses git. git is distributed.";
- [the projects by the address](https://www.bestpractices.dev/en/projects.json?url=https%3A%2F%2Fgithub.com%2Faltnovatrail%2Fapi): `[{"id":16661,"badge_level":"passing","repo_url":"https://github.com/altnovatrail/api"}]`;
- [Scorecard's checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md#cii-best-practices), the branch `main`: "The check uses the URL for the Git repo and the OpenSSF Best Practices badge API"; "[passing badge](https://www.bestpractices.dev/criteria/0): 5";
- [Scorecard's run of `api`](https://api.scorecard.dev/projects/github.com/altnovatrail/api): `"date":"2026-11-13T15:17:59Z"`, the commit 5c7caec, `CII-Best-Practices` — `"score":0`, "no effort to earn an OpenSSF best practices badge detected"; `Fuzzing` — `"score":10`, "project is fuzzed";
- the open findings of code scanning (`gh api 'repos/altnovatrail/<repository>/code-scanning/alerts'`): of Scorecard at `api` — `CIIBestPracticesID`, `CodeReviewID`, `MaintainedID`, `PinnedDependenciesID` and `BranchProtectionID`, `FuzzingID` no longer among them; at `spec`, `app`, `module-kit` and `tutor`, whose projects have the level passing and the score 5, `CIIBestPracticesID` is fixed.
