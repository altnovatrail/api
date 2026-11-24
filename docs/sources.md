# api — sources

> The facts of the outside world the repository's documents under `docs/` rest on and no run of ours checks: a law, a regulator, a standard's normative text, a provider's terms and policies, a price, a license, a page that changes in place with no version, a text outside the repositories. Here are their records — the address, the date it was read and a quote in the source's language — and [architecture.md](architecture.md) and the decision log hold the claim and a citation of its one record by its number, `sources 1.1`, not a chain. A record's heading names its topic, not a task.
>
> No record is needed for a library's documentation or code at a version or commit — a link beside the claim —, for a behaviour a test or a measurement checks — they are its evidence —, or for spec's tags and the files of NovaTrail's and MathTrail's repositories — git, at a commit.
>
> A record is a subsection "N.M" of a topic: one source, or one reading of several sources for one decision. A record's number never changes and is never given again: a new one goes at the end of its topic, a new topic at the end of the file, and a record removed leaves a gap. A source read again with the same text changes the record's date; with another text, the claims and decisions on it are rewritten in the same change and the record holds the new reading alone — where a decision waits on the author, an open question, and both readings stand until it is closed. The model is `spec/docs/sources.md`. The results of measurements are not here but in [measurements.md](measurements.md).

## 1. How to keep it

- **Topics** 2–11 follow the parts of the service in the order [architecture.md](architecture.md) describes them, and topic 12 is a reading that several parts rest on at once; a topic for another part or for several at once goes at the end of the file.
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

## 6. Showing the solution and hints

MathTrail's examples and the neighbouring repositories.

### 6.1. Showing the solution and hints in MathTrail, 2026-11-02

For showing the solution and a hint on request. Read 2026-11-02 through `gh api`:

- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0 — the latest on `main`:
  - the decision on the second call: it goes if «неверных ответов на холсте не больше трёх»; the model gets «ступень подсказки и прошлые вопросы этого холста»; «Дальше наставник предлагает позвать взрослого или начать заново. Ответ не показывается никогда»; rejected — «показывать ответ после третьей ступени»;
  - the decision on the hint ladder: «Лестница подсказок идёт по числу неверных ответов на холсте»; «После третьей ступени наставник предлагает позвать взрослого»;
  - the specification, on the route of a hint: «`UNREADABLE` и `UNCHECKED` ступень не меняют»; «**После третьей ступени** вызова 2 нет. Событие `hint` с `kind: offer_help` несёт фразу шаблона `offer_help` […] и кнопки «Ask a grown-up» и «Start again»»; the trap text is the rung template if it «совпадает с подсказкой, уже показанной на этом холсте, в том числе с вопросом модели». During the check: «записан `INCORRECT`, а подсказки нет — подсказкой становится то, что положено по маршруту […] без вызова 2: запасной текст […] на 1-м–3-м неверном ответе, `offer_help` с 4-го»;
  - the specification, on an abandoned canvas: «**Холст, к которому не возвращаются,** остаётся `active`: ежедневная очистка его не бросает»; «Брошенным холст делает только новый холст той же задачи».
- [mathtrail/mathtrail-standalone, `docs/decisions.md`](https://github.com/mathtrail/mathtrail-standalone/blob/89986bc/docs/decisions.md), commit 89986bc — the latest on `main`:
  - the decision on "I don't know": ""I don't know" is an answer, and a wrong one"; "a child who has seen the solution has not solved the task; counting it as wrong keeps the rating an honest estimate"; rejected — "a skip that leaves the rating alone, which would need a third outcome in the profile and the formula";
  - the decision on a skipped task: "When `next_task` is asked for a new task while the current one has no answer, the current one is recorded as skipped"; "Nothing that reads answers reads it — not the rating"; "punishing that would punish a stray tap; but leafing through hard tasks until an easy one comes shows only in what the parent can see, and a skip that left no trace would hide it".
- the NovaTrail prototype — `spec/draft/README.md` and `spec/draft/NovaTrail Widget.html` in `spec` v0.6.2: ""Show solution" ends the task as skipped, with no change to the rating | Open, product О-7"; "A quick reply "Give me a hint" at any moment, before the first answer too". The adult's switch "Ask before “Show solution”" with the caption "The coach first offers one more hint" is off by default — `S['t_' + k] ?? (k === 'week')`, — and there is no string `t_ask` in the code: only the switch itself reads its value.

### 6.2. The neighbouring repositories on showing the solution and the hint, 2026-11-02

For showing the solution and a hint on request. Read 2026-11-02 through `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), commit a42a7e1, the `spec` submodule at v0.6.2. In `docs/architecture.md`: ««Подсказка» — всегда, в обоих режимах, с начала задачи. Работу она не шлёт […], а запрос всегда идёт в `api`»; «Попытка кончилась, когда `api` сказал, что задача кончилась, — верный ответ, показ решения задачи, попытка закрыта «без проверки» на снятой версии […], — или когда ученик начал ту же задачу заново». `app` is silent on the setting "ask before showing";
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, the `spec` submodule at v0.6.2. The hint request is `func Request(t *sample.Task, sh *reading.Sheet, rung Rung, lang sample.Lang)` in `internal/hint/hint.go`, without the past questions and the matched trap. In `docs/architecture.md`: «Поле у запроса подсказки появится, если О-21 решат по рекомендации»; on the feedback and the hint on a failure — «запасной текст ступени… Как это сочетается с нейтральной фразой при сбое ИИ […], решает детализация `api`». The form and leak checks lie in `internal/`, and the library for `api` comes from the epic E5 «Код библиотеки `tutor`» in table 2 of its RUN.md;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/9f338ae), commit 9f338ae, the `spec` submodule at v0.2.9. In RUN.md the task «Веса свидетельств» is `[ ]`, and it is to decide: «меняет ли подсказка вес свидетельства или только то, ставится ли «освоено»; «без проверки» — свидетельство с нулевым весом или не свидетельство вовсе».

## 7. Platform AI quota

The provider's prices, the usage report of `tutor` and MathTrail's examples.

### 7.1. Platform AI quota, 2026-11-02

For the quota in dollars and its reserve. Read 2026-11-02:

- Anthropic's prices — [Pricing](https://platform.claude.com/docs/en/about-claude/pricing), no date on the page: per million input and output tokens — "Claude Opus 5.5 … $4 / MTok … $20 / MTok", "Claude Sonnet 5.5 … $2 / MTok … $10 / MTok", "Claude Haiku 5.5 … $0.10 / MTok for prompts up to 100,000 tokens … $0.50 / MTok", "Claude Fable 5.1 … $10 / MTok … $50 / MTok". [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/thinking), no date: "Thinking has a cost: the tokens Claude spends reasoning are billed as output tokens, even when the thinking text isn’t returned to you". [Vision](https://platform.claude.com/docs/en/build-with-claude/vision), no date: "An image, therefore, costs ⌈width / 28⌉ × ⌈height / 28⌉ visual tokens". The prices are the same as in the table of `tutor`, read 2026-10-23;
- payment for a cut-off request — [How do I pay for my Claude API usage?](https://support.claude.com/en/articles/8977456-how-do-i-pay-for-my-claude-api-usage), no date on the page: "You're billed only for successful API calls and completed tasks. Failed requests aren't charged"; "If your client disconnects or times out in the middle of a request that was on track to succeed, that request is still charged";
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, through `gh api`:
  - on the usage report: «по записи на каждый вызов модели: у чтения рукописи их две, по читателю […], а у чтения сочинения — одна…»; in the record — «поставщик и модель, как её назвал поставщик в ответе», the tokens, «оценка в долларах — по таблице цен `tutor`»; «Ответ, оборванный до подсчёта токенов, и вызов, прерванный до счёта, считаются по худшему случаю»; «ни содержания, ни идентификаторов в сообщении нет, и его можно писать в журналы […]»; «по нему `api` считает квоты ИИ платформы […]. На своём ключе `api` его не копит». On the two readers: «Если хоть одному читателю рукописи поставщик отказал по лимиту, чтение отдаёт исход «лимит исчерпан» целиком»; «Но вызов читателя, который ответил, оплачен, и его запись остаётся в сообщении о расходе». On the outcome "limit reached": «На своём ключе это повод сказать владельцу ключа, а на ИИ платформы — сигнал оператору: отказ — ключу платформы»; Anthropic's tier limit — «до 00:00 UTC первого числа следующего месяца». There is no code of the report: «Кода продукта ещё нет: его строит эпик после […]»;
  - on the worst case: «Худший случай одного ответа — 4 000 токенов входа и 16 000 выхода: от $0,0084 у Haiku 5.5 до $0,84 у Fable 5.1». For Opus 5.5, by the prices above, that is about $0.34 — our count;
  - «Итог двух вызовов сочинений»: the price of an essay on Opus 5.5 · `high` — «$0,030», on Haiku 5.5 · `low` — «$0,0006»;
  - the series of the judge: «Claude Opus 5.5 · `high` | 4 из 4 | 5 | 0 | $0,0098 | 5,7 с» — an answer with feedback. This is a judge's answer, not a call for words, and the estimate does not take this price for the words of a rung;
- [altnovatrail/tutor, `docs/architecture.md`](https://github.com/altnovatrail/tutor/blob/9f85613/docs/architecture.md), commit 9f85613 — the latest on `main`, read 2026-11-08 through `gh api`: «Итог замера подсказок» of 2026-10-28, the row «Opus 5.5 · `high`» — p50 «5,1» s, «Цена вызова» — «$0,0152». The prices of the day — «Opus 5.5 — $4 и $20 за 1 млн токенов […]»;
  - on admission: «Если ИИ, который нужен проверке задачи, ученику закрыт, `api` даёт задачи, которые проверяются без ИИ ([…]; О-5): ученик не пишет работу, которую некому прочитать. Какие это задачи, решают `api` и `engine` — строка В64»; «сам ключ приходит только с работой […]»; an admission refusal — «исход «не допущен», отдельный от сбоя ИИ и от «лимит исчерпан», но ученик видит то же, что при исчерпанном лимите»; on the first app — «Ключ автора — это ключ взрослого, который сам и ученик, и владелец ключа: вызов идёт от его имени […], и путь «свой ключ» для `adult` открыт без фактов»;
  - the decision on the estimate in dollars: «С оценкой в долларах `api` считает квоты и юнит-экономику (О-14), не зная цен моделей»; rejected — «лимит в вызовах или токенах — вызов чтения и вызов разбора стоят по-разному, а токены разных моделей — тоже». This is an argument about the own key limit, and it bears on the quota of `api` by the same argument;
- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0 — the latest on `main`, through `gh api`:
  - the decision on reserving the counters: «Счётчики резервируются в начале проверки, вместе с арендой…»; «Резерв возвращается, если его вызов модели не понадобился или не был сделан»; «Ребёнку об этом сказано мягко: «I've helped all I can for now. Keep going on your own!» — на каждое нажатие Check. Времени фраза не обещает: сутки лимитов — UTC, и для семьи в США они кончаются ещё днём»;
  - the decision on tariffs: «После MVP — три тарифа»: the free one — «дневная квота помощи наставника, как в бете», «За $10 в месяц — квоты больше», «За $20 в месяц — квоты ещё больше и ИИ-наставник с голосом»;
  - the specification, on the day of the limits: «**Сутки** для лимитов — UTC»; the daily cleanup «удаляет счётчики старше 30 дней и истёкшие инвайты»;
  - the specification, on the limits and the worst day: «Чтений ответа на ребёнка в день | 60», «Разборов черновика на ребёнка в день | 20», «Чтений и разборов на родителя в день, по всем детям | 180 / 60»; «**Худший день.** При средней цене вызова ≈ $0.14 на ребёнка и ≈ $0.41 на родителя, а потолок — если каждый вызов дойдёт до своего лимита выходных токенов […] — ≈ $0.52 и ≈ $1.55»;
  - the privacy document, its table of data: «| Дневные счётчики | ребёнка и родителя | Neon, `usage_daily`, `parent_usage_daily` | лимиты | 30 дней | нет |»;
- [mathtrail/mathtrail-standalone](https://github.com/mathtrail/mathtrail-standalone/tree/89986bc), commit 89986bc — the latest on `main`, through `gh api`:
  - the decision on the family's day: "The day of the limits is the family's: it ends at the family's midnight, by the clock the card tells with each answer"; "an offset no time zone keeps, more than twelve hours behind or fourteen ahead or off the quarters of an hour, is passed over"; "Until a card has told a clock, the day is the UTC day, as before. A clock counts from the next day on"; "No line of the log carries the offset"; why — the day of the limits ended "at seven in the evening" for a family five hours behind UTC; rejected are "a rolling day, so many tasks in any twenty-four hours, which needs no clock but makes "tomorrow" the hour the day began" and "the UTC day as it was";
  - the specification, on refusals: "No refusal ever names a limit's number, because a number invites arithmetic rather than a lesson";
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), commit a42a7e1, `docs/architecture.md`: «тариф и квота ИИ платформы в разделе для взрослых — О-14»; «Сбой ИИ или исчерпанная квота — нейтральная фраза […]».
- Anthropic's prices were checked again 2026-11-11, for the price of a check on the first app (measurements 2.1), and 2026-11-12, for the run of the quota (measurements 3). This item comes from the bodies of those tasks in the archives [docs/plan/E3.md](plan/E3.md) and [docs/plan/E6.md](plan/E6.md), not from the primary source, and there are no quotes there: the task of the check's price has only «По ценам, сверенным 2026-11-11», with no address, and the task of the quota's run has a retelling: «Цены Anthropic сверены 2026-11-12 ([Pricing](https://platform.claude.com/docs/en/about-claude/pricing)): Opus 5.5 — $4 и $20 за MTok входа и выхода, Haiku 5.5 — $0,10 и $0,50». These are the same prices as 2026-11-02 above, and by them comes the $0.138 of measurements 2.1.

## 8. Lesson

What the neighbours decided about the lesson.

### 8.1. The lesson at the neighbours, 2026-11-02

For the lesson run. Read 2026-11-02 through `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/3ce4cdb), commit 3ce4cdb, the `spec` submodule at v0.6.2:
  - the decision on the place in a lesson: «Место в уроке и состояние показа его примеров принадлежат прохождению урока на его пакете — ключ — ID урока и дайджест пакета»; «Обычный конец — всегда «Продолжить» на последнем звене, какого бы вида оно ни было: `app` говорит `api`, что урок пройден»; «Сам `api` кончает урок только необычным путём — досрочно или «без проверки»»; rejected — «место в уроке на сервере: каждое «Продолжить» — запрос и новая строка данных ученика в […]»; it does not decide — «когда урок пройден и что `api` о нём записывает, и продолжает ли урок, начатый на другом устройстве, то же прохождение — […]»;
  - on leaving a lesson: «Уход посреди урока ничего не шлёт: состояние урока лежит в базе профиля, пока его не сотрёт срок в 30 дней без изменений […]. Когда `api` снова даёт этот урок на том же пакете, `app` открывает его на месте и с показами, как они были.»; «Урок открывается на более позднем из двух мест: своём, из состояния урока, и том, что идёт после последней кончившейся задачи по ответу `api`.»; «Звено задачи, которая кончилась, полоса показывает пройденным, но не открывает»;
  - the row В15 of table 3 of RUN.md of `app`: «Состояние показа `api` знать не нужно — это ответ на вопрос […]»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/20ae268), commit 20ae268, `docs/architecture.md`: «Пример с разбором в уроке — не задача и свидетельством не служит, а задача урока — такая же попытка, как вне урока […]»;
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, `docs/decisions.md`: «Вызова `tutor` урок не добавляет»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), commit 08bc48a, the `spec` submodule at v0.2.9: at the engine's input — «уроки и пул задач»; in RUN.md, in a task that has not started, it is to decide — «когда движок даёт урок, а когда задачу вне урока […]»;
- [altnovatrail/engine, `docs/decisions.md`](https://github.com/altnovatrail/engine/blob/9751ba4/docs/decisions.md), commit 9751ba4 — the latest on `main`, read 2026-11-08 through `gh api` for the move to `spec` v0.9.11, whose decision log took it: on the lesson — «Закрепления в уроке и досрочного конца в первом срезе нет»; on the learner's data in a call — «данных ученика в вызове столько, сколько правило читает […]: прохождения без мест звеньев, попытки с рубрикой — только «Готово»»; on a task whose solution was seen — «Задача с виденным решением — 24 часа не кандидат, а потом — после всех задач навыка без виденного решения»;
- [altnovatrail/skill-map](https://github.com/altnovatrail/skill-map/tree/0295ce8), commit 0295ce8, `docs/architecture.md`: «не выбирает следующий урок или задачу: это делает движок […]»; the package draws the graph and the state of the learner model «по готовым данным от `api` — ярусы, пререквизиты, владение»;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/a0db6e9), commit a0db6e9: in the tree — documents and settings, neither module folders nor `lessons/`;
- `spec` [v0.6.4](https://github.com/altnovatrail/spec/blob/v0.6.4/RUN.md) — then a tag newer than the `api` submodule —, RUN.md, «Что `app` нужно от контракта в E3»: on «урок пройден» — «Как `api` сверяет его с попытками урока и что записывает, решает […]»; on «что известно и что найти» — «`api` их не записывает, пока О-21 не решит, что они входят в свидетельство».

## 9. Task with a rubric

What the neighbours decided about the essay.

### 9.1. The task with a rubric at the neighbours, 2026-11-02

For the attempt of a task with a rubric and what of it is stored. Read 2026-11-02 through `gh api`:

- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5, the `spec` submodule at v0.6.2, `docs/architecture.md`:
  - the output of the assessment: «по каждому критерию — номер, обоснование и выполнен ли он»; «критерий, которого нет в ответе или который назван дважды, `tutor` отдаёт без оценки, как в замере […], а отзыв — как есть, через правило отзыва. Повторять ли тогда вызов, решает `api`. Сбой вызова оставляет без оценки все критерии, и ученик видит нейтральную фразу»; «видит ли ученик оценки по критериям, решают `api` и `app`: в открытом виде критериев нет»; «У критерия в формате `spec` есть только текст, без ID, поэтому `tutor` нумерует критерии по порядку в рубрике»;
  - the reading: «сочинение, которое не прочитать, до оценки не доходит… так что как `api` это показывает и записывает, — его детализация… Такого выхода у промпта замера нет: синтетика вся разборчива. Что делать с отдельным неразборчивым словом, покажет замер на сочинениях добровольцев»; in the table of request types the reading of an essay returns «текст, как написан, и можно ли его прочитать»;
  - the feedback rule: the function «получает отзыв, прочитанный текст и язык объяснений. Отвечает «прошёл» или называет, какие части правила нарушены: предложения, слова, цитата, письмо»; «проверка утечки ищет в отзыве только фразы-утечки задачи, если автор их дал»; «что ученик видит вместо отзыва, который правило не прошёл, решает детализация `api` […]»; the logs — «без содержания: ни прочитанного текста, ни обоснований, ни отзыва, хотя отзыв может цитировать сочинение»;
  - «Итог двух вызовов сочинений»: on Opus 5.5 · `high` — «Правило отзыва […] нарушено» «2 из 45», the price of an essay «$0,030», the time p50 «11,9» s;
  - the own key limit reached: «на оценке по рубрике — нейтральную фразу, а критерии остаются без оценки, как при сбое вызова»;
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/14f5a54), commit 14f5a54, the `spec` submodule at v0.6.2:
  - the decision on the essay: «сочинение с отзывом по рубрике — артефакт и живёт, пока его не сотрут»; in the table of what the device keeps: «сочинение — с начала попытки, законченное и нет: история письма, прочитанное, отзыв по рубрике | база профиля | как история попытки | когда его сотрёт взрослый на странице «Это устройство»; ни конец попытки, ни срок его не стирают»;
  - on the kinds of tasks: «Сочинение — чтение и отзыв по рубрике, без вердикта […]; что идёт после отзыва, решает `api`»;
  - on the lesson: «На задаче «Продолжить» появляется, только когда `api` сказал, что задача кончилась»; «Сочинение с отзывом по рубрике остаётся на устройстве, но из полосы урока не открывается»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/28433fa), commit 28433fa, `docs/architecture.md`: «попытка задачи с рубрикой в первом срезе его не даёт (О-17)»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), commit 08bc48a, the `spec` submodule at v0.2.9, `docs/architecture.md`: «навыки с рубрикой и без оценки идут в порядке автора»; the task of its plan that decides this has not started;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/97c20e2), commit 97c20e2: in the tree — documents and settings, no modules yet;
- `spec` [v0.6.5](https://github.com/altnovatrail/spec/blob/v0.6.5/docs/privacy.md) — then a tag newer than the `api` submodule, commit 1a99391. П-19 there is the same, and П-8 was supplemented 2026-11-02 by В9 of `app`: «хранение сочинений на сервере по выбору владельца аккаунта приходит с модулем сочинений, до первого ребёнка: иначе единственная копия работы ученика зависит от правил браузера». The row 6.2 of its `COORDINATION.md` already names this path: «путь задачи с рубрикой ([…], …)»;
- the NovaTrail prototype — `spec/draft/README.md` in `spec` v0.6.2: "An essay, such as "My best day", goes Plan, Write, Improve and ends with "I'm done""; in the table of differences — "A short essay ends with "I'm done", and nothing checks it | An essay gets feedback by the module author's rubric: feedback, not a verdict".

## 10. Protocol

Connect, protobuf, buf, Fetch and CORS, the check time of `tutor` and the channel of canvas-api.

### 10.1. Connect, protobuf and buf, 2026-11-02

For the protocol and what goes on the wire. Read 2026-11-02 — the source code at the latest releases, cloned into the session's temporary folder, and the pages of documentation, checked against their source code:

- connect-es [v2.3.0](https://github.com/connectrpc/connect-es/tree/v2.3.0) of 2026-10-27 — the latest, commit 6b7c593: `packages/connect-web/src/connect-transport.ts` — "By default, connect-web clients use the JSON format" and `const useBinaryFormat = options.useBinaryFormat ?? false;`, and `useHttpGet` has "Controls whether or not Connect GET requests should be used when available, on side-effect free methods. Defaults to false"; `packages/connect/src/protocol/serialization.ts` — "In connect-es, unknown JSON fields are ignored by default" and `o.ignoreUnknownFields ??= true;`. `connect-node`, by contrast, has "By default, connect-node clients use the binary format";
- protobuf-es [v2.16.0](https://github.com/bufbuild/protobuf-es/tree/v2.16.0) of 2026-09-29 — the latest, commit b3b232f: `packages/protobuf/src/from-json.ts` — `ignoreUnknownFields` has "This option ignores unknown fields in parsing, as well as unrecognized enum string representations"; the reference of the generated code — "Open enums can contain numeric values that are not declared in the generated TypeScript enum, so code that receives data from newer schemas should handle unknown enum numbers"; `bytes` in JSON — "JSON value will be the data encoded as a string using standard base64 encoding with paddings"; `useProtoFieldName` defaults to `false`;
- connect-go [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) of 2026-10-21 — the latest, commit 8f69fe4, the module `connectrpc.com/connect/v2`: the JSON codec in `connectproto/connectproto.go` — `MarshalOptions: protojson.MarshalOptions{Resolver: resolved.resolver}` without `UseProtoNames` and `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, and "Other settings can be configured by mutating the exported fields before use"; `connecthttp/protocol_connect.go` — GET is allowed only for `StreamTypeUnary` with `IdempotencyNoSideEffects`; `WithHTTPGet` has "Note, however, that servers don't automatically set any cache headers";
- [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page: on GET — "These requests do not require any special HTTP headers, and can be viewed in a browser tab. They are designed to be easy to make and easy to cache in browsers, proxies, and CDNs"; the error detail — "the "value" field contains unpadded, base64-encoded binary Protobuf data"; the codes — `invalid_argument` and `failed_precondition` — 400, `not_found` — 404, `permission_denied` — 403, `unauthenticated` — 401, `resource_exhausted` — 429, `unavailable` — 503, `unimplemented` — 501, and "there are no user-defined codes";
- [Connect, CORS](https://connectrpc.com/docs/cors/), no date: "For side-effect-free RPCs such as simple queries, Connect supports GET requests that do not require CORS preflights", if "No request headers must be set"; "If your application uses custom request headers, you must explicitly allow them. This includes commonly used headers such as Authorization";
- [ProtoJSON](https://protobuf.dev/programming-guides/json/), no date: "Adding values to an enum is safe if the "Emit enum values as integers" is set on all relevant clients"; [the behaviour of enums](https://protobuf.dev/programming-guides/enum/), no date: "Proto3 and editions use *open* enums specifically because of the unexpected behavior that *closed* enums cause";
- [buf breaking rules](https://buf.build/docs/breaking/rules/), no date: `RPC_SAME_IDEMPOTENCY_LEVEL` — in the categories `FILE`, `PACKAGE`, `WIRE_JSON` and `WIRE`, "This checks that the idempotency_level RPC option doesn't change. Doing so can result in different HTTP verbs being used".

### 10.2. Fetch, CORS and `<img>`, 2026-11-02

For the token in a header and what goes on the wire. Read 2026-11-02:

- [Fetch Standard](https://fetch.spec.whatwg.org/), "Last Updated 19 October 2026": a request's cache mode — "Unless stated otherwise, it is "default"", and ""default": Fetch will inspect the HTTP cache on the way to the network"; `fetch()` without its own `cache` does not change the mode. `Authorization` is not in the safelist of request headers, and "A CORS non-wildcard request-header name is a header name that is a byte-case-insensitive match for `Authorization`". `Access-Control-Max-Age` — "Indicates the number of seconds (5 by default)", and an entry of the preflight cache holds the URL, and a match requires that "entry's URL equals request's current URL";
- [HTML Standard, "Updating the image data"](https://html.spec.whatwg.org/multipage/images.html#updating-the-image-data), "Last Updated 30 October 2026": the request for an image — "the result of creating a potential-CORS request given urlString, "image", and the current state of element's crossorigin content attribute", and the page's headers are not in it;
- Chromium, [`services/network/cors/preflight_result.cc`](https://github.com/chromium/chromium/blob/98b9eab7ac9e36a7228c5143db756f699c8ec4e1/services/network/cors/preflight_result.cc), commit 98b9eab: "constexpr base::TimeDelta kMaxTimeout = base::Hours(2);"; `preflight_cache.h` — "static constexpr size_t kMaxEntriesPerTopFrameSite = 32u;". Firefox, [`netwerk/protocol/http/nsCORSListenerProxy.cpp`](https://github.com/mozilla-firefox/firefox/blob/abbeff4fd9e02e3f6195153493fc0825eafe5d6d/netwerk/protocol/http/nsCORSListenerProxy.cpp), commit abbeff4: "// Cap at 24 hours.".

### 10.3. The check time of `tutor`, 2026-11-02

For the real-time channel. [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), commit be607e5 — the latest on `main` on 2026-11-02, `docs/architecture.md`, read at the same time through `git`:

- «Итог замера подсказок»: in the column «p50, с» — «| Opus 5.5 · `high` | … | 5,1 | $0,0152 |»;
- in the same place, «Итог двух вызовов сочинений»: «| Opus 5.5 · `high` | 0 из 198 | 0 из 197 | 1,00 | 2 из 45 | $0,030 | 11,9 |», the last column — «Время p50, с»;
- the documents have no time for the reading call and the pair of readers: the reading report counts «**время** p50 и p95 и цена вызова», but the runs lie outside git, and the pair is counted by the logs of the separate readers — coverage and errors. The documents do not name the order in which the readers of the pair go in the product;
- the judge with the key is the same call with the picture of the work: the series of the judge with the key, the medians — «Opus 5.5 · `high` | 4 | 4 из 4 | 2,5 с» and «Haiku 5.5 · `low` | 9 | 7 из 8 | 2,7 с»; the measurements of the check — «**Время ответа**, p50: Opus — 2,5–3,2 с, Haiku · `low` — 2,2–2,8 с» and «Вызов Opus — $0,012 в форме A и $0,020 в форме D, p50 — 3,0–3,9 с»;
- on the order of the calls: «Вердикт ждёт только вызова чтения, без ключа, а сильная модель — разбор и подсказка, с ключом — идёт только после «пока нет»».

### 10.4. canvas-api on the channel, 2026-11-02

For the real-time channel. [mathtrail/canvas-api, `docs/decisions.md`](https://github.com/mathtrail/canvas-api/blob/0901af0/docs/decisions.md), commit 0901af0 — the latest on `main`, read 2026-11-02 through `gh api`: the decision on the processor of Cloud Run — «При оплате Cloud Run за запросы CPU выделяется только на время обработки запроса, и фоновая работа после ответа почти не получает процессора. Поэтому всё остаётся внутри запроса», and its later text — «Всё по-прежнему делается внутри запроса — довод о процессоре после ответа в силе»; the decision on Centrifugo and OpenFGA — «Centrifugo и OpenFGA из стека не уходят: … они приходят в E15. В E2 их нет не потому, что «проверка — один запрос и один ответ», — этот довод … отверг, — а потому, что в E2 нет беты».

### 10.5. Connect v2, 2026-11-09

For Connect v2 in `serve`. Read 2026-11-09:

- the module of contracts `contracts/v0.9.11`, and `contracts/v0.12.0` after it, where `go.mod` and `apiv1connect/account.connect.go` are the same: `go.mod` requires `connectrpc.com/connect/v2 v2.0.2`, and `apiv1connect/account.connect.go` has `func RegisterAccountServiceHandler(server *connect.Server, svc AccountServiceHandler)`, each method has `if err := stream.Receive(&req); err != nil { return err }` before calling the handler, and `UnimplementedAccountServiceHandler` has `connect.NewError(connect.CodeUnimplemented, …)`;
- `connectrpc.com/connect/v2` [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) — `go 1.26.0`, license Apache-2.0, from the module through `go mod download`; `docs/v2-guide.md`: "an interceptor can reject a call using only its headers, before any payload work, which is what makes authn be able to be an interceptor in v2 rather than HTTP middleware"; "an interceptor can observe individual messages by wrapping the stream before calling `next`"; "only a locally authored `*connect.Error` is serialized to the wire. Any other error reaches the client as a bare code with no message"; "`Mount` installs one `http.Handler` per registered procedure onto a mux"; `connectproto/connectproto.go`: `NewJSONCodec` has `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, and `MarshalOptions` is a field that can be set "before use".

### 10.6. Redirects of the standard library's mux, 2026-11-13

For the routes of `serve`. Read 2026-11-13 in the sources of the module, through `go list -m -f '{{.Dir}}'` and `go env GOROOT`:

- Go 1.27.4, `src/net/http/server.go`, the comment of `ServeMux`, "Trailing-slash redirection": "If the ServeMux receives a request for the subtree root without a trailing slash, it redirects the request by adding the trailing slash"; the comment of `ServeMux.Handler`: "If the path is not in its canonical form, the handler will be an internally-generated handler that redirects to the canonical path". The comment does not name the trailing slash there; the code does: `findHandler` returns `RedirectHandler(u.String(), StatusTemporaryRedirect)` for both, and for the clean path `u` keeps `r.URL.RawQuery`. This is a conclusion from the code, which the fuzz test `FuzzRoutes` of `internal/server` holds;
- `connectrpc.com/connect/v2` v2.0.2, `connecthttp/connecthttp.go`, `Mount`: `mux.Handle(spec.Procedure, …)` for each method, and beside them `// "/package.Service/Method" -> subtree pattern "/package.Service/".` with `mux.Handle(service, newUnknownMethodHandler(server, &opts))`; `newUnknownMethodHandler` "routes RPC-shaped POST requests through [connect.Server.Call] and 404s everything else", by `http.NotFound(responseWriter, request)`, whose body in `server.go` of Go is "404 page not found".

### 10.7. The check of an own key at Anthropic, 2026-11-14

For the check of an own key. Read 2026-11-14, by the `.md` versions of the pages, before the real check that calls `count_tokens` with the author's dev key:
- [Token counting](https://platform.claude.com/docs/en/build-with-claude/token-counting), its section "Pricing and rate limits": "Token counting is **free to use** but subject to requests per minute rate limits based on your [usage tier]", the table — "| Start | 5,000 |", "| Build | 10,000 |", "| Scale | 20,000 |" requests per minute —, and "Token counting and message creation have separate and independent rate limits. Usage of one does not count against the limits of the other." The same words as sources 1.16 `tutor` read 2026-11-11;
- [Errors](https://platform.claude.com/docs/en/api/errors): "401 - `authentication_error`: There’s an issue with your [API key] (for example, it’s malformed, revoked, or expired …)"; "403 - `permission_error`: Your API key does not have permission to use the specified resource."; and on a limit one sets oneself: "The API also returns a 400 when usage reaches an organization or workspace [spend limit you set]". Whether `count_tokens` meets this 400 the page does not say. `claude.Refusal` of `tutor` v0.1.33 reads a 400 `invalid_request_error` of the owner's or the workspace's limit as a limit, and `Tutor.CheckKey` takes a limit for a key accepted — a conclusion from the code, not tried against the provider.

### 10.8. The paths Cloud Run keeps, 2026-11-23

For the path of the health check. [Cloud Run known issues](https://docs.cloud.google.com/run/docs/known-issues), "Reserved URL paths", no date on the page, read 2026-11-23: "You can't use the following URL paths: Paths starting with /_ah/; Some paths ending with z. To prevent conflicts with reserved paths, we recommend avoiding all paths that end in z".

## 11. Deletion and export

The sign-in service on deletion and fresh sign-in, RFC 9470, the codes of Connect, canvas-api and `app`, Cloud Run jobs and the `changelog` of OpenFGA.

### 11.1. Firebase Auth on deletion and fresh sign-in, 2026-11-02

For the fresh sign-in and the deletion of a profile. Read 2026-11-02:

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

  That re-sign-in sets a new `auth_time` is said directly nowhere: it is a conclusion from `reauthenticateWithPopup` being a new sign-in at the sign-in service. On the dev project on 2026-11-12 a sign-out and a new sign-in by a popup changed the user's last sign-in time at the sign-in service — an indirect check, not the token itself;
- [the Firebase Auth REST API](https://firebase.google.com/docs/reference/rest/auth), updated 2026-10-20, exchanging a refresh token for an ID token: "USER_NOT_FOUND: The user corresponding to the refresh token was not found. It is likely the user was deleted.";
- [managing users](https://firebase.google.com/docs/auth/web/manage-users), updated 2026-10-21: "Some security-sensitive actions—such as deleting an account, setting a primary email address, and changing a password—require that the user has recently signed in";
- [session cookies](https://firebase.google.com/docs/auth/admin/manage-cookies), updated 2026-10-20: "For sensitive applications, the auth_time should be checked before issuing the session cookie", and in the sample — `new Date().getTime() / 1000 - decodedIdToken.auth_time < 5 * 60`, otherwise 401;
- [user sessions](https://firebase.google.com/docs/auth/admin/manage-sessions), updated 2026-10-20: "Firebase ID tokens are short lived and last for an hour", and a refresh token stops working when "The user is deleted";
- [connections of a Google account with third-party apps](https://support.google.com/accounts/answer/13533235), no date on the page: "If you delete a link, Google stops automatic sign-in to the app. This doesn't delete your data on the app". Whether deleting the user at the app removes the connection, the page does not say.

### 11.2. RFC 9470, 2026-11-02

For the fresh sign-in. [OAuth 2.0 Step Up Authentication Challenge Protocol](https://www.rfc-editor.org/rfc/rfc9470), September 2023, read 2026-11-02: "insufficient_user_authentication: The authentication event associated with the access token presented with the request does not meet the authentication requirements of the protected resource"; "max_age: This value indicates the allowable elapsed time in seconds since the last active authentication event associated with the access token"; in the sample response — `HTTP/1.1 401 Unauthorized` with `max_age="5"`.

### 11.3. Connect codes, 2026-11-02

For the fresh sign-in. [the Connect protocol](https://connectrpc.com/docs/protocol), no date on the page, read 2026-11-02: `unauthenticated` — "Caller doesn't have valid authentication credentials for the operation", and "use `unauthenticated` if the user can't be identified or presents invalid credentials"; `failed_precondition` — "if the client must explicitly fix the system state before retrying".

### 11.4. Deletion in canvas-api, 2026-11-02

For the deletion of a profile. [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), commit 0901af0, read 2026-11-02 through `gh api`:

- the decision on deletion: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен», «Кортежи удаляются пачками до 100, отсутствующие пропускаются», «Аккаунт удаляется в порядке «дети → `parents` → отзыв токенов и `DeleteUser`»»;
- the specification, on deleting an account: «Повтор после сбоя на `DeleteUser` видит, что строки `parents` уже нет, и просто доводит удаление в Firebase»; «Другие устройства родителя стирают черновики, когда там откроется SPA и узнает об удалении»;
- the specification, on the fresh sign-in: «Иначе ответ `REAUTH_REQUIRED`», and in the table of codes — «`REAUTH_REQUIRED` | 401 | нужен свежий вход: `auth_time` старше 5 минут»;
- the privacy document, on deletion: «Удаление требует свежего входа: PIN не подходит, ребёнок может его знать». Its open question П-7 — «Хранить ли доказательство согласия после удаления аккаунта», and «решает юрист».

There is neither a job that completes the deletion nor an answer to the other devices there: the retry was led by the client.

### 11.5. `app` on erasing on the device, 2026-11-02

For the fresh sign-in. [altnovatrail/app, `docs/architecture.md`](https://github.com/altnovatrail/app/blob/375a09b/docs/architecture.md), commit 375a09b, read 2026-11-02 through `gh api`. In «Когда что стирается»: «выход владельца аккаунта — всё аккаунта: базы всех профилей, общий кеш, реестр и сессия»; «вход другого аккаунта — если в реестре другой ID аккаунта, `app` стирает всё прежнее раньше, чем что-либо откроет … Так бывает, когда прежний вход кончился без выхода»; «удаление аккаунта — `api` отвечает, что аккаунта больше нет, и `app` стирает всё аккаунта и выходит».

### 11.6. Cloud Run jobs, 2026-11-09

For the daily retention job. Read 2026-11-09:

- [the container contract](https://docs.cloud.google.com/run/docs/container-contract), no date: for a Cloud Run job it sets `CLOUD_RUN_JOB` — "The name of the Cloud Run job being run", and for a service `K_SERVICE`, "The name of the Cloud Run service being run";
- [the task timeout](https://docs.cloud.google.com/run/docs/configuring/task-timeout), no date: "By default, each task runs for a maximum of 10 minutes", and the largest — "up to 168 hours (7 days)"; "If the task attempt does not complete within this time, it will be stopped".

### 11.7. The `changelog` of OpenFGA, 2026-11-09

For the daily retention job. `assets/migrations/postgres/001_initialize_schema.sql` in [openfga/openfga v1.22.1](https://github.com/openfga/openfga/blob/v1.22.1/assets/migrations/postgres/001_initialize_schema.sql), read 2026-11-09: `CREATE TABLE changelog` with the fields `store`, `object_type`, `object_id`, `relation`, `_user`, `operation`, `ulid`, `inserted_at TIMESTAMPTZ NOT NULL` and the key `PRIMARY KEY (store, ulid, object_type)`; there is no index by time.

### 11.8. The size of a response of Cloud Run, 2026-11-14

For the export and the stored works. [Cloud Run quotas and limits](https://docs.cloud.google.com/run/quotas), read 2026-11-14: "Maximum HTTP/1 response size: 32 MiB per response. Limit applies if not using Transfer-Encoding: chunked or streaming". The export of `api` is one unary response of Connect, whose `api.zip` holds the profile's stored works, each up to 4 MiB.

## 12. canvas-api: decisions and specification

A reading that several parts of the service rest on at once: sign-in and permissions, the database, the quota, the protocol, deletion.

### 12.1. The decisions and specification of canvas-api, 2026-10-19

[mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), the branch `main`, commit 0901af0, read 2026-10-19. The repository was cloned into the session's temporary folder, not into a NovaTrail repository. The quotes are word for word:

- the decision on OpenFGA: «OpenFGA работает сайдкар-контейнером в сервисе canvas-api», «хранилище OpenFGA — отдельная база в том же проекте Neon»;
- the decision on the token check: «Все запросы, кроме горячего пути — выдачи токенов Centrifugo, — проверяют токен с отзывом (`VerifyIDTokenAndCheckRevoked`)»; «Перед `DeleteUser` вызывается `RevokeRefreshTokens`»; «Необратимые действия требуют свежего входа (`auth_time` ≤ 5 минут)»;
- the specification, on the fresh sign-in: «**Свежий вход** — `auth_time` не старше 5 минут — нужен для удаления профиля ребёнка, удаления аккаунта и смены забытого PIN. Иначе ответ `REAUTH_REQUIRED`, и SPA просит войти заново»;
- the specification, on the PIN: «PIN — барьер интерфейса, а не граница безопасности»; «Поэтому необратимые действия требуют свежего входа, а не PIN»;
- the decision on deletion: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен»;
- the specification, on deleting a profile: «Дневные счётчики родителя остаются: иначе удаление и новый профиль снимали бы потолок трат»;
- the decision on access to the beta: «Доступ в бету — по одноразовым инвайт-кодам», «Email canvas-api не читает»;
- the decision on the daily limit: «Исчерпанный дневной лимит не прерывает занятие: ребёнок решает дальше сам, без проверки»; «Сделанный вызов засчитывается, даже если он не удался или вышел по тайм-ауту»;
- the decision on the HTTP response: «HTTP-ответ приходит последним и повторяет все события на случай разрыва WebSocket»;
- the decision on the early verdict: «Разбор черновика идёт секунды, поэтому ранний `verdict` по WebSocket по-прежнему нужен»; rejected — «убрать Centrifugo и Upstash и отдавать всё в HTTP-ответе», «убрать OpenFGA и проверять права кодом в Postgres»; «Довод «проверка теперь — один запрос и один ответ» без новых фактов повторно не предлагать».
- read 2026-11-02 for sign-in and permissions, the same commit, through `gh api`:
  - the decision on the OpenFGA model: «Модель OpenFGA заранее описывает учителя и класс, но кортежей для них MVP не пишет»;
  - the decision on the subject of access: «Субъект доступа — всегда родитель: запросы идут с его Firebase ID token, профиль ребёнка передаётся параметром»;
  - the decision on the stack: «система сразу готова к realtime и B2B»;
  - the decision on the status `verified`: «Статус `verified` ставит оператор командой `cmd/admin` после того, как получил подписанную форму (П-1)»; «До `verified` профили детей и холсты не создаются»;
  - the decision on the OpenFGA store: «Сервис находит или создаёт store `canvas`, сравнивает последнюю модель со встроенной и при расхождении записывает новую»;
  - the specification, on the tuple: «пишется внутри транзакции Neon, до `COMMIT`» — about the OpenFGA tuple; «Он безвреден: права на пустой объект ничего не дают. Его удаляет ежедневная очистка» — about a "dangling" tuple;
  - the specification, on sign-in: «Firebase Auth, провайдеры Google и Apple. Для Apple нужно членство в Apple Developer Program»;
  - the specification, on the PIN: «не больше 5 попыток за 15 минут»; «На сервере хранится только хеш argon2id»; «меняется после свежего входа, без старого PIN» — about a forgotten PIN.
- read 2026-11-02 for the database, the same commit, through `gh api`:
  - the decision on the stack: in the stack — «Neon Postgres, Upstash Redis»; rejected — «Firestore вместо Postgres»;
  - the decision on the region: «Neon: `aws-us-east-1` — та же агломерация, задержка до базы около 1–2 мс. У Neon регионы только в AWS (документация Neon, прочитана 2026-10-14)» — about the pairing with Cloud Run in `us-east4`; rejected — `us-central1`: «ближайший Neon — `aws-us-east-2` (Огайо), около 15–20 мс на каждый запрос к базе»;
  - the decision on deletion: «`changelog` OpenFGA сам не чистится и хранил бы связи «uid ↔ ребёнок» бессрочно»; «Ежедневная очистка удаляет «висящие» кортежи и таблицу `changelog` OpenFGA старше суток»;
  - the specification, on the stack: migrations — «`github.com/pressly/goose/v3` | v3.28.0»; on the migration job: «Она запускается как Cloud Run job с образом нового релиза до выкладки сервиса»; on changing the schema: migrations — «по схеме «сначала добавить, потом убрать»»;
  - the specification, on the plans of services: «Neon | Free | 0,5 ГБ на проект, 100 CU-часов в месяц, автопауза через 5 мин (не отключается), восстановление на момент времени — 6 часов»;
  - the privacy document, on deletion: the database `canvas` is deleted «сразу; из истории восстановления (PITR) — через 6 часов», and in the database `openfga` «`changelog` — ежедневной очисткой, до суток; из PITR — через 6 часов после этого»;
  - the privacy document, on retention: a child's profile with no activity for 12 months «удаляется автоматически, без отдельного уведомления. Этот срок заранее указан в уведомлении при согласии: email у нас нет, писать некуда (П-6)».

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
