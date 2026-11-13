# api — источники

> Внешние факты, на которых стоят документы `docs/`: условия поставщика, цена, стандарт, документация библиотеки или сервиса, а также тексты других репозиториев NovaTrail и MathTrail на названных коммитах. Здесь лежат их записи — адрес, версия или дата страницы, дата прочтения и цитата на языке источника, — а [architecture.md](architecture.md) держит утверждение и ссылку на запись по её номеру, как на раздел: `sources 2.1` (RULES.md, The bar; R143 spec).
>
> До T62, 2026-11-13, записи стояли в architecture 4, и там остался указатель: на раздел 4 ссылаются журнал решений и архивы `docs/plan/`, а их не переписывают. Текст записей перенесён как был. Заголовком стала первая строка чтения — его название и дата, — её скобка с разделами и решениями стала первыми словами записи, «Для …», а ссылки на разделы этого репозитория, которые в architecture 4 читались как `раздел 3.2`, здесь — `architecture 3.2`. Два замера размера бинарника, что стояли в чтениях для R31 и R32, ушли в [measurements.md](measurements.md). Файл — на русском, пока его не переведёт T63.

## 1. Как вести

- **Запись** — подраздел «N.M» темы: одно чтение одного или нескольких источников ради одного решения. В ней — адрес, версия или дата страницы, дата прочтения и цитата на языке источника. Что прочитано не в первоисточнике — копия, пересказ, страница, которая не открылась, вывод из кода, проба сборкой, — запись так и называет.
- **Темы** при переносе встали по разделам architecture 3.1–3.10, по порядку — темы 2–11, — а тема 12 — чтение, на котором стоят сразу несколько разделов. Новое чтение для раздела, у которого тема уже есть, — новая запись в её конце; тема для другого раздела или для нескольких сразу встаёт в конец файла, после темы 12, и порядок разделов этим нарушается, а номера — нет.
- **Ссылка.** Утверждение документа ссылается на запись её номером, как на раздел любого документа: `(sources 7.1)`, а из другого репозитория — `sources 7.1 api`. Номер записи не меняется: новая запись встаёт в конец своей темы, новая тема — в конец файла.
- **Источник прочитан снова** — в запись добавляется пункт со своей датой, а прежний текст не правится: на нём стоят решения своего дня. Если источник изменился и утверждение с ним разошлось, это поправка к решению или вопрос автору, а не правка записи.
- **Чего здесь нет:** цитат, которые запись журнала решений приводит сама, — они остаются в записи (RULES.md, How we work: решение не переписывается); итогов замеров — они в [measurements.md](measurements.md).

## 2. Вход и права

Для architecture 3.1: сервис входа, его Admin SDK и ID token, права и OpenFGA.

### 2.1. Firebase Auth, 2026-11-02

Для architecture 3.1; R03. Прочитано 2026-11-02:

- [приватность в Firebase](https://firebase.google.com/support/privacy), обновлено 2026-10-29: «The Firebase Authentication service is run only from US data centers. As a result, Firebase Authentication processes data exclusively in the United States»; «Firebase Authentication keeps logged IP addresses for a few weeks»; после удаления пользователя «data is removed from live and backup systems within 180 days»; среди данных, которые он обрабатывает, — «User agents» и «IP addresses»;
- [Firebase Authentication](https://firebase.google.com/docs/auth), обновлено 2026-10-20: «Firebase Authentication with Identity Platform is an optional upgrade that adds several new features to Firebase Authentication»; «When upgraded, no-cost (Spark) plan projects will be limited to 3,000 daily active users, and pay-as-you-go (Blaze) plan projects will be charged for usage beyond the free tier of 50,000 monthly active users»;
- [цены Firebase](https://firebase.google.com/pricing), даты на странице нет: строка «Other Authentication services» отмечена и у плана Spark, и у Blaze, без цены. Строки активных пользователей стоят под «With Identity Platform»: на Blaze — «No-cost up to 50K MAUs», «Then Google Cloud pricing»;
- [проверка ID token](https://firebase.google.com/docs/auth/admin/verify-id-tokens), обновлено 2026-10-20: `auth_time` — «Must be in the past. The time when the user authenticated»;
- [сеансы пользователя](https://firebase.google.com/docs/auth/admin/manage-sessions), обновлено 2026-10-20: проверка отзыва на сервере — «an expensive operation, requiring an extra network round trip»;
- Admin SDK для Go, [v4.22.0](https://github.com/firebase/firebase-admin-go/releases/tag/v4.22.0) от 2026-09-23 — последний релиз; комментарии к функциям в `auth/auth.go` и `auth/user_mgt.go`: у `VerifyIDToken` — «In non-emulator mode, this function does not make any RPC calls most of the time»; у `VerifyIDTokenAndCheckRevoked` — «this function must make an RPC call to perform the revocation check»; у `RevokeRefreshTokens` — «existing ID tokens may remain active until their natural expiration (one hour)»;
- [эмулятор Authentication](https://firebase.google.com/docs/emulator-suite/connect_auth), даты нет: «For security reasons, the Authentication emulator issues unsigned ID tokens»; «Please make sure not to set the environment variable in production». `firebase-tools` [v15.0.0](https://github.com/firebase/firebase-tools/releases/tag/v15.0.0) от 2025-12-10: «Removed support for running emulators with Java versions prior to 21»;
- [вход через Apple](https://firebase.google.com/docs/auth/web/apple), даты нет: «Sign In with Apple can only be configured by members of the Apple Developer Program».

### 2.2. Apple Developer Program, 2026-11-02

Для R03; Д3. [условия вступления](https://developer.apple.com/programs/enroll/), даты нет, прочитано 2026-11-02: «The Apple Developer Program is 99 USD per membership year».

### 2.3. OpenFGA, 2026-11-02

Для architecture 3.1; R04. [openfga/openfga](https://github.com/openfga/openfga/tree/v1.22.1), релиз v1.22.1 от 2026-10-28 — последний, коммит 44054f5, лицензия Apache-2.0, прочитано 2026-11-02:

- README: «Can also be embedded as a Go library»;
- `pkg/storage/migrate/migrate.go`, комментарий к `RunMigrations`: «This function is exposed to allow embedding openFGA into applications and manage OpenFGA's database schema migrations directly»; хранилища в `pkg/storage` — `postgres`, `mysql`, `sqlite` и `memory`;
- `.config-schema.json` — у `changelog` одна настройка, `changelogHorizonOffset`: «Changes that occur after this offset will not be included in the response of ReadChanges». Срока хранения записей `changelog` там нет (architecture 3.2; R05).

### 2.4. OpenFGA как библиотека, 2026-11-08

Для R31. Прочитано 2026-11-08, по v1.22.1 — всё ещё последнему релизу:

- [`pkg/server/server.go`](https://github.com/openfga/openfga/blob/v1.22.1/pkg/server/server.go): `NewServerWithOpts` — «You must call Close on it after you are done using it», а `Close` закрывает и хранилище; методы `Check`, `Write`, `Read`, `ListStores`, `CreateStore` и `WriteAuthorizationModel` вызываются прямо, с запросами `openfgav1`, и сами их проверяют;
- [`pkg/storage/migrate/migrate.go`](https://github.com/openfga/openfga/blob/v1.22.1/pkg/storage/migrate/migrate.go) импортирует `github.com/go-sql-driver/mysql`, а у [go-sql-driver/mysql v1.10.1](https://github.com/go-sql-driver/mysql/blob/v1.10.1/LICENSE) — «Mozilla Public License Version 2.0»; файлы миграций — `assets.EmbedMigrations`, `PostgresMigrationDir = "migrations/postgres"`;
- [о согласованности](https://openfga.dev/docs/interacting/consistency): «caching is disabled by default. When caching is disabled, all queries will have strong consistency regardless of the consistency mode specified»;
- [openfga/language `pkg/go` v0.3.2](https://github.com/openfga/language/tree/pkg/go/v0.3.2/pkg/go), лицензия Apache-2.0: `transformer.TransformDSLToProto`;
- размер бинарника `api` с OpenFGA — замер, measurements 1.1.

### 2.5. Admin SDK для Go и его тесты, 2026-11-09

Для architecture 3.1 и 3.10; R32. Прочитано 2026-11-09:

- Admin SDK для Go, [v4.22.0](https://github.com/firebase/firebase-admin-go/tree/v4.22.0) от 2026-09-23 — всё ещё последний релиз, коммит 66a72c3, `go 1.25.0`, Apache-2.0; исходники через `gh api`:
  - `firebase.go`: корневой пакет импортирует `cloud.google.com/go/firestore`, `firebase.google.com/go/v4/storage`, `db`, `messaging` и другие; `NewApp` с заданной конфигурацией ошибки не возвращает — только когда читает `FIREBASE_CONFIG`; `App.Auth` зовёт `auth.NewClient(ctx, &internal.AuthConfig{…})`, а у `NewClient` — «This function can only be invoked from within the SDK»;
  - `auth/auth.go`: `verifyIDToken` проверяет отзыв при `if c.isEmulator || checkRevokedOrDisabled`, так что в режиме эмулятора — `FIREBASE_AUTH_EMULATOR_HOST` — его проверяет и `VerifyIDToken`, а Identity Toolkit — `fmt.Sprintf("http://%s/identitytoolkit.googleapis.com", authEmulatorHost)`; без ключа SDK подписывает `newIAMSigner`, который спрашивает сервер метаданных об email сервисного аккаунта только при подписи;
  - `auth/token_verifier.go`: ключи — по `idTokenCertURL`, «https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com», клиентом `transport.NewHTTPClient(ctx, option.WithoutAuthentication())`; `verifyTimestamps` сверяет `iat` и `exp` с `clockSkewSeconds = 300`, а `auth_time` не проверяет нигде; в режиме эмулятора — «skip signature verification»; ключи, которых не получить, — код `CERTIFICATE_FETCH_FAILED`, а не `idTokenInvalid`;
  - `auth/user_mgt.go`: текст ошибки называет UID — `fmt.Sprintf("no user exists with the %s", query.description())`;
  - `internal/internal.go`: `FirebaseScopes` — «the set of OAuth2 scopes used by the Admin SDK»: `cloud-platform`, `datastore`, `devstorage.full_control`, `firebase`, `identitytoolkit` и `userinfo.email` из `https://www.googleapis.com/auth/`;
  - `internal/http_client.go`: `WithDefaultRetryConfig` — `MaxRetries: 4`, повторы на сетевые ошибки и 503, `MaxDelay` — две минуты;
- `google.golang.org/api` [v0.279.0](https://github.com/googleapis/google-api-go-client/tree/v0.279.0), который требует SDK, исходники через `gh api`: `transport/http/dial.go` — `defaultBaseTransport` «Copy http.DefaultTransport except for MaxIdleConnsPerHost setting», через `clonedTransport(http.DefaultTransport)`; `option/option.go` — у `WithCredentialsFile` и `WithCredentialsJSON` «Deprecated: This function is being deprecated because of a potential security risk», и «you can use [WithAuthCredentialsFile] with [ServiceAccount]»;
- проба в копии модуля вне репозитория. `cmd/api`, который импортирует порт, тянет 32 новых модуля — `cloud.google.com/go` с `firestore`, `storage`, `monitoring`, `auth` и `iam`, `google.golang.org/api`, `golang.org/x/oauth2`, otel, s2a-go, go-jose и другие. У каждого LICENSE — Apache-2.0, MIT или BSD-3, NOTICE нет ни у одного. `go-licenses check` по разрешённому списку прошёл без отказов, а govulncheck v1.9.0 сказал: «Your code is affected by 0 vulnerabilities». Размер бинарника с портом — замер, measurements 1.2. Настоящий SDK против подставного Google: живой токен проходит без отказа; `IsIDTokenRevoked`, `IsUserDisabled` и `IsUserNotFound` различаются; ответ 500 не узнаёт ни один помощник `Is…`; подпись чужим ключом даёт `IsIDTokenInvalid` без `IsIDTokenExpired`; `VerifyIDToken` отдаёт UID отозванного токена без запроса.

### 2.6. ID token без `app`, 2026-11-09

Для R39. Прочитано 2026-11-09:

- [REST API Firebase Auth](https://firebase.google.com/docs/reference/rest/auth), даты нет: «Exchange custom token for an ID and refresh token» — `POST https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=[API_KEY]`, тело `token` и `returnSecureToken`, в ответе `idToken`, `refreshToken` и `expiresIn`; «API_KEY refers to the Web API Key, which can be obtained on the project settings page in your admin console»;
- [custom token](https://firebase.google.com/docs/auth/admin/create-custom-tokens), даты нет: claims токена, сделанного сторонней библиотекой JWT, — `alg` `"RS256"`, `iss` и `sub` — «Your project's service account email address», `aud` — `"https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"`, `exp` — «a maximum of 3600 seconds later than the `iat`», `uid` — «between 1-128 characters long, inclusive».

## 3. База и модель ученика

Для architecture 3.2: Neon и отвергнутые Cloud SQL и профиль на Диске, база тестов и миграции.

### 3.1. Neon, 2026-11-02

Для architecture 3.2; R05. Прочитано 2026-11-02:

- [регионы](https://neon.com/docs/introduction/regions), список IP обновлён 2026-10-28: «Neon offers project deployment in multiple AWS regions»; среди них `aws-us-east-1` (N. Virginia), `aws-us-east-2` (Ohio), `aws-us-west-2`, `aws-eu-central-1` (Frankfurt) и `aws-eu-west-2` (London); «You can no longer create new projects in Azure regions». Регионов Google Cloud в списке нет;
- [цены](https://neon.com/pricing), «Last updated on October 31, 2026»: у Free — «100 CU-hours Per Project», «1 GB per Project», окно — «Up to 6 hours or 1 GB of data changes»; у Launch — «$0.106 per CU-hour», «$0.35 per GB-month», история — «$0.20 per GB-month», окно «Up to 7 days», засыпание — «After 5 minutes can be disabled», а оценка трат — «Typical spend: $ 15 /mo Based on: Intermittent load , 1 GB». Ответ на вопрос о плане Free: «When a database is idle, its compute scales to zero after 5 minutes and starts again on the next query, typically within a few hundred milliseconds» и «Compute is suspended only if a project uses up its monthly 100 CU-hours or 5 GB of network transfer»;
- [окно истории](https://neon.com/docs/postgres/backup-restore/history-window), даты нет: окно «sets how long that change history is kept, which defines how far back instant restore and the other features can reach»; по умолчанию и наибольшее — «Free 6 hours 6 hours (capped at 1 GB of history) Launch 1 day 7 days Scale 1 day 30 days»;
- [как снизить расходы](https://neon.com/docs/introduction/cost-optimization), даты нет: «Be aware that delete operations generate WAL records that temporarily add to your instant restore storage until they age out of your history window»;
- [базы](https://neon.com/docs/manage/databases), даты нет: «In the Neon object model, a database exists within a branch of a project. There is a limit of 500 databases per branch»;
- [пул соединений](https://neon.com/docs/connect/connection-pooling), даты нет: «Neon uses PgBouncer in transaction mode ( pool_mode=transaction )»; «Not supported with pooled connections: SET / RESET (session variables) LISTEN / NOTIFY WITH HOLD CURSOR PREPARE / DEALLOCATE (SQL-level prepared statements)», а также «Session-level advisory locks»;
- [безопасность](https://neon.com/docs/security/security-overview), даты нет: «Neon requires that all connections use SSL/TLS encryption»;
- [соответствие нормам](https://neon.com/docs/security/compliance), даты нет: «We have completed audits for SOC 2 Type 1 and Type 2, SOC 3, ISO 27001, and ISO 27701, and we adhere to GDPR and CCPA regulations»;
- [условия Neon](https://neon.com/platform-terms) — сюда ведёт адрес `neon.com/dpa`, «Last Updated: October 14, 2026»: «This Product Specific Schedule (Neon) … is entered into as of the Effective Date between Databricks, Inc., the parent company of Neon, LLC ("Databricks"), and Customer»; приложение меняет разделы DPA Databricks. О детях и несовершеннолетних в нём ничего нет;
- [субобработчики](https://www.databricks.com/legal/databricks-subprocessors) — сюда ведёт `neon.com/subprocessors`, «Last Updated: June 9, 2026»: «Amazon Web Services, Inc. Cloud Service Provider United States Customer Selected».

### 3.2. Cloud SQL для PostgreSQL, 2026-11-02

Для R05 — вариант, который он отверг. Прочитано 2026-11-02:

- [цены](https://cloud.google.com/sql/pricing), даты нет: «Instance pricing is charged for every second that the instance is running (the activation policy is set to ALWAYS)»;
- [запуск и остановка](https://docs.cloud.google.com/sql/docs/postgres/start-stop-restart-instance), «Last updated 2026-10-26 UTC»: «If you are not using your instance, you can set its activation policy to NEVER to avoid instance charges» — то есть остановка вручную, а не по простою;
- [пробный экземпляр](https://docs.cloud.google.com/sql/docs/postgres/free-trial-instance), «Last updated 2026-10-26 UTC»: «A Cloud SQL free trial instance lets you test the majority of Cloud SQL features for up to 30 days without any financial commitment».

### 3.3. mathtrail-standalone, 2026-11-02

Для R06 — вариант, который он отверг. [docs/architecture/](https://github.com/mathtrail/mathtrail-standalone/tree/c6cbeb2/docs/architecture), коммит c6cbeb2, прочитано 2026-11-02 через `gh api`:

- `04-profile.md`: «everything the service knows sits in a JSON file in a visible folder of the parent's Drive (О-5). The service keeps nothing of its own between requests, so this file is not a cache of some server-side truth — it **is** the truth»; `recent` — «the last **20** entries»; правило выбора считается «from the summary and the window alone — never from the full history, which we do not keep»;
- `05-storage.md`: «**There is no conditional write.** … Whoever uploads last wins, silently»; «If the parent empties the Drive bin, the profile is gone — no server-side copy exists, by design».

### 3.4. embedded-postgres, 2026-11-02

Для Д6. [fergusstrange/embedded-postgres](https://github.com/fergusstrange/embedded-postgres), релиз v2.0.1 от 2026-10-19 — последний, лицензия MIT, прочитано 2026-11-02. README: «Run real PostgreSQL in your tests»; «The first start downloads PostgreSQL and the matching supervisor; later starts reuse the cache»; «Requires Go 1.26 or newer»; «Root callers must select an existing non-root Unix identity». В `internal/binaries/manifest.json` v2.0.1 у каждого архива Postgres — `sha256` и адрес в релизах `theseus-rs/postgresql-binaries`. В `supervisor_binary.go` скачанный супервизор сверяется с `checksums.txt` того же релиза, который `cacheSupervisor` скачивает во время запуска, а без опубликованной версии библиотека просит «Config.Supervisor(path) or EP_SUPERVISOR». Пользователь devcontainer — `vscode`, не root.

### 3.5. База и миграции, 2026-11-08

Для R29. Прочитано 2026-11-08:

- [`embedded-postgres` v2.0.1](https://github.com/fergusstrange/embedded-postgres/tree/v2.0.1) — всё ещё последний релиз; в `go.mod` модуля требований нет, а README: «The production Go packages have zero third-party dependencies». В `config.go` — `V17 PostgresVersion = "17.11.1"`, а `Supervisor(path)` и `Port(0)` — построители `Config`. README: «An independent supervisor watches a dedicated owner pipe. An unrecovered panic, `os.Exit`, or forced owner termination closes that pipe and triggers PostgreSQL shutdown»;
- [pgx v5.12.0](https://github.com/jackc/pgx/blob/v5.12.0/CHANGELOG.md), лицензия MIT: CHANGELOG закрывает GHSA-95fj-3fxw-g62g — в v5.9.0–v5.11.0 `require_auth` и `channel_binding=require` обходились без `sslmode=verify-full`; в `pgconn/config.go` `sslmode=require` без `sslrootcert` ставит `InsecureSkipVerify`. О режиме запросов по умолчанию `ConnConfig.DefaultQueryExecMode` говорит: «may be incompatible with proxies such as PgBouncer unless they are configured to support protocol-level prepared statements»;
- [goose v3.29.0](https://github.com/pressly/goose/tree/v3.29.0), лицензия MIT: `NewProvider` с `WithSessionLocker` и `WithDisableGlobalRegistry`; `lock/postgres.go` — `pg_try_advisory_lock`, блокировка сеанса; README: «By default, all migrations are run within a transaction»;
- [Neon, connection pooling](https://neon.com/docs/connect/connection-pooling), обновлено 2026-10-19: «pool_mode=transaction», «max_prepared_statements=1000 … Maximum protocol-level prepared statements per connection»; среди того, что пулер не держит, — «Session-level advisory locks»; в таблице выбора — «Schema migrations \| Direct \| Tools may not support transaction pooling»;
- [OpenFGA v1.22.1](https://github.com/openfga/openfga/blob/v1.22.1/pkg/storage/postgres/postgres.go): хранилище Postgres открывает `pgxpool.ParseConfig(uri)` и режима запросов не задаёт, а миграции хранилища идут через goose без блокировки (`pkg/storage/migrate/migrate.go`).
- [tern v2.4.3](https://github.com/jackc/tern/blob/v2.4.3/migrate/migrate.go), лицензия MIT: пакет `migrate` импортирует `github.com/Masterminds/sprig/v3`; [golang-migrate v4.20.1](https://github.com/golang-migrate/migrate/blob/v4.20.1/go.mod), лицензия MIT: корневой `go.mod` требует драйверы многих баз и облачные SDK — Spanner, Cloud Storage, AWS, MongoDB, Snowflake.

## 4. Версии модулей и пакеты в образе

Для architecture 3.3: `modules` и `module-kit`, проверки CI, встраивание файла и задание по расписанию GitHub Actions.

### 4.1. `modules` и `module-kit`, 2026-11-02

Для architecture 3.3; R07. Прочитано 2026-11-02 через `gh`:

- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/105fa72), коммит 105fa72 на `main`: `gh release list -R altnovatrail/modules` — пусто, тегов нет, так что `module-index.json` нет ни в одном релизе. В RUN.md задача T01 «Раскладка модулей в репозитории» — `[ ]`, и в ней: «индекс модулей один на все модули: как его собирает релиз одного модуля, чтобы индекс называл и пакеты остальных. На этапе 2 `api` берёт дайджесты из индекса релиза (architecture 3.10 spec)»;
- [altnovatrail/module-kit](https://github.com/altnovatrail/module-kit/tree/1a54028), коммит 1a54028 на `main`: в `docs/architecture.md` — «Пока он отвечает только на `--version`; проверка модуля, сборка пакета и индекса придут с форматом v0». Единственный релиз — v0.1.0 от 2026-10-21; его подписал общий `release.yml` `.github` v0.1.4, и `release-verify` его отклоняет (R77 spec).
- прочитано 2026-11-07 для перехода на `spec` v0.9.4 (R23): [altnovatrail/module-kit, `docs/architecture.md`](https://github.com/altnovatrail/module-kit/blob/e069943/docs/architecture.md), коммит e069943 на `main`, последний релиз — `v0.2.3`; первый релиз с кодом формата, который берут другие, — `v0.2.0` (строка 3.3 закрытых строк `COORDINATION.md` `spec` v0.9.4). Таблица пакетов Go: `modulepackage` — «канонический вид, сборка пакета, чтение по цепочке дайджестов — с `device/` и без», зовёт и `api`; `moduleindex` — «сборка и чтение индекса, версия новой попытки», зовут CLI и `api`; `observation` — «годность наблюдения, сведение двух чтений, вердикт по наблюдению с выбором касанием, исход сочинения», зовёт `api`.
- прочитано и собрано 2026-11-09 для перехода на `spec` v0.12.0 (R40): последние релизы — `module-kit` [`v0.2.6`](https://github.com/altnovatrail/module-kit/tree/v0.2.6) от 2026-11-08, его `go.mod` — `github.com/altnovatrail/spec v0.9.9`; `modules` [`v0.1.4`](https://github.com/altnovatrail/modules/releases/tag/v0.1.4) от 2026-11-09; `tutor` [`v0.1.21`](https://github.com/altnovatrail/tutor/tree/v0.1.21) от 2026-11-09, его `go.mod` — `module-kit v0.2.3` и `spec v0.9.16`. `module/check.go` `v0.2.6`: «const FormatVersion = "0.3"». Пустой модуль во временной папке с `module-kit` `v0.2.6` и `spec` v0.12.0 — `go build` пакета `modulepackage`: «module/tasks.go:236:14: cannot use part.Picture (variable of type *formatv0.TaskPicturePath) as *formatv0.PicturePath value in assignment» и «module/tasks.go:290:11: formatv0.ExplanationPart does not satisfy formatv0.StatementPart | formatv0.DemoPart». По пакетам: `observation`, `verdict` и `openview` собираются, `moduleindex` и `moduleversion` — нет. Пустой модуль с `tutor` `v0.1.21` и `spec` v0.12.0 — пакеты `tutor`, `tutor/admission`, `tutor/words` и `observation` с `verdict` `module-kit` собираются;

### 4.2. Обязательные проверки и фильтр путей, 2026-11-02

Для architecture 3.3; R07. [синтаксис workflow GitHub Actions](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions), даты на странице нет, прочитано 2026-11-02: «If a workflow is skipped due to path filtering, branch filtering, or a commit message, then checks associated with that workflow will remain in a 'Pending' state. A pull request that requires those checks to be successful will be blocked from merging».

### 4.3. Встраивание файла в Go, 2026-11-02

Для architecture 3.3; R07. [пакет `embed`](https://pkg.go.dev/embed), документация для go1.27.3, прочитано 2026-11-02: «Patterns may not contain '.' or '..' or empty path elements, nor may they begin or end with a slash»; «The patterns are interpreted relative to the package directory containing the source file».

### 4.4. Задание по расписанию и его сигнал, 2026-11-12

Для architecture 3.3; R61. Прочитано 2026-11-12, даты на страницах нет:

- [события workflow](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows), `schedule`: «High load times include the start of every hour»; «Scheduled workflows run on the latest commit on the default branch»; «In a public repository, scheduled workflows are automatically disabled when no repository activity has occurred in 60 days»; о pull request и других событиях от `GITHUB_TOKEN` — «other `GITHUB_TOKEN`-triggered events do not create workflow runs at all»;
- [уведомления о прогонах](https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs): «Notifications for scheduled workflows are sent to the user who initially created the workflow»; если строку cron меняет другой, «subsequent notifications will be sent to that user instead», а если задание отключили и включили снова, уведомления идут «the user who re-enabled the workflow»; «You can also choose to receive a notification only when a workflow run has failed»;
- [настройки Actions репозитория](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository): «You can choose to allow or prevent GitHub Actions workflows from creating or approving pull requests». У `api` — `default_workflow_permissions: read` и `can_approve_pull_request_reviews: false` (`gh api repos/altnovatrail/api/actions/permissions/workflow`);
- [новейший релиз](https://docs.github.com/en/rest/releases/releases#get-the-latest-release): «The latest release is the most recent non-prerelease, non-draft release, sorted by the created_at attribute»; у создания релиза `make_latest` по умолчанию — `true`; публичный `https://github.com/altnovatrail/modules/releases/latest` переадресует на `…/releases/tag/v0.2.5` (`curl -sI`);
- [оплата Actions](https://docs.github.com/en/billing/concepts/product-billing/github-actions): «The following usage remains free independently and does not reduce the included compute usage: Standard GitHub-hosted runners in public repositories».

## 5. Перенос прогресса

Для architecture 3.4.

### 5.1. `learner-model` и `module-kit` о переносе, 2026-11-02

Для architecture 3.4; R08. Прочитано 2026-11-02 через `gh api`:

- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/9f338ae), коммит 9f338ae на `main`, подмодуль `spec` — на v0.2.9. В RUN.md задача T04 «Журнал свидетельств (Т-22)» — `[ ]`, и в ней: «перенос по таблице соответствия старых ID навыков новым (concepts 3 spec): что делает библиотека, а что `api` — перенос есть в обеих карточках (architecture 3.5, 3.8 spec)». В `docs/architecture.md`, в подразделе «Журнал свидетельств»: «Журнал только на дозапись свидетельств не переписывает: при нём старые ID остаются в свидетельствах, и перенос идёт через таблицу при пересчёте. Как он устроен и чья это работа, не решено»;
- [altnovatrail/module-kit](https://github.com/altnovatrail/module-kit/tree/1a54028), коммит 1a54028 на `main`. В RUN.md задача T04 «Правила semver для `diff`» — `[ ]`, и в ней: «таблица соответствия старых ID навыков новым: где она лежит и что сравнение у неё проверяет — покрыт ли каждый удалённый навык, не переиспользован ли опубликованный ID» и «откуда CI `modules` берёт прошлую версию».

## 6. Показ решения и подсказки

Для architecture 3.5: образцы MathTrail и соседние репозитории.

### 6.1. Показ решения и подсказки в MathTrail, 2026-11-02

Для architecture 3.5; R09, R10. Прочитано 2026-11-02 через `gh api`:

- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), коммит 0901af0 — последний на `main`:
  - R39 canvas-api: вызов 2 идёт, если «неверных ответов на холсте не больше трёх»; модель получает «ступень подсказки и прошлые вопросы этого холста»; «Дальше наставник предлагает позвать взрослого или начать заново. Ответ не показывается никогда»; отвергнуто — «показывать ответ после третьей ступени»;
  - R24 canvas-api: «Лестница подсказок идёт по числу неверных ответов на холсте»; «После третьей ступени наставник предлагает позвать взрослого»;
  - spec 6.3 canvas-api: «`UNREADABLE` и `UNCHECKED` ступень не меняют»; «**После третьей ступени** вызова 2 нет. Событие `hint` с `kind: offer_help` несёт фразу шаблона `offer_help` (7.3) и кнопки «Ask a grown-up» и «Start again»»; текст ловушки — шаблон ступени, если он «совпадает с подсказкой, уже показанной на этом холсте, в том числе с вопросом модели». В ходе проверки: «записан `INCORRECT`, а подсказки нет — подсказкой становится то, что положено по маршруту 6.1 без вызова 2: запасной текст (6.3) на 1-м–3-м неверном ответе, `offer_help` с 4-го»;
  - spec 9.7 canvas-api: «**Холст, к которому не возвращаются,** остаётся `active`: ежедневная очистка его не бросает»; «Брошенным холст делает только новый холст той же задачи».
- [mathtrail/mathtrail-standalone, `docs/decisions.md`](https://github.com/mathtrail/mathtrail-standalone/blob/89986bc/docs/decisions.md), коммит 89986bc — последний на `main`:
  - R93 standalone: «"I don't know" is an answer, and a wrong one»; «a child who has seen the solution has not solved the task; counting it as wrong keeps the rating an honest estimate»; отвергнуто — «a skip that leaves the rating alone, which would need a third outcome in the profile and the formula»;
  - R98 standalone: «When `next_task` is asked for a new task while the current one has no answer, the current one is recorded as skipped»; «Nothing that reads answers reads it — not the rating»; «punishing that would punish a stray tap; but leafing through hard tasks until an easy one comes shows only in what the parent can see, and a skip that left no trace would hide it».
- прототип NovaTrail — `spec/draft/README.md` и `spec/draft/NovaTrail Widget.html` в `spec` v0.6.2: «"Show solution" ends the task as skipped, with no change to the rating | Open, product О-7»; «A quick reply "Give me a hint" at any moment, before the first answer too». Переключатель взрослого «Ask before “Show solution”» с подписью «The coach first offers one more hint» по умолчанию выключен — `S['t_' + k] ?? (k === 'week')`, — а строки `t_ask` в коде нет: его значение читает только сам переключатель.

### 6.2. Соседние репозитории о показе решения и подсказке, 2026-11-02

Для architecture 3.5; R09, R10. Прочитано 2026-11-02 через `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), коммит a42a7e1, подмодуль `spec` — на v0.6.2. В `docs/architecture.md`: ««Подсказка» — всегда, в обоих режимах, с начала задачи. Работу она не шлёт (R69 spec), а запрос всегда идёт в `api`» (architecture 3.3 app); «Попытка кончилась, когда `api` сказал, что задача кончилась, — верный ответ, показ решения задачи, попытка закрыта «без проверки» на снятой версии (R63 spec), — или когда ученик начал ту же задачу заново» (architecture 3.2 app). О настройке «спросить перед показом» `app` молчит;
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), коммит be607e5, подмодуль `spec` — на v0.6.2. Запрос подсказки — `func Request(t *sample.Task, sh *reading.Sheet, rung Rung, lang sample.Lang)` в `internal/hint/hint.go`, без прошлых вопросов и совпавшей ловушки. В `docs/architecture.md`: «Поле у запроса подсказки появится, если О-21 решат по рекомендации» (architecture 3.10 tutor); на разборе и подсказке при сбое — «запасной текст ступени… Как это сочетается с нейтральной фразой при сбое ИИ (architecture 4.1 spec), решает детализация `api`» (architecture 3.7 tutor). Проверки формы и утечки лежат в `internal/`, а библиотеку для `api` даёт эпик E5 «Код библиотеки `tutor`» в таблице 2 его RUN.md;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/9f338ae), коммит 9f338ae, подмодуль `spec` — на v0.2.9. В RUN.md задача T03 «Веса свидетельств» — `[ ]`, и в ней решить: «меняет ли подсказка вес свидетельства или только то, ставится ли «освоено»; «без проверки» — свидетельство с нулевым весом или не свидетельство вовсе».

## 7. Квота ИИ платформы

Для architecture 3.6: цены поставщика, сообщение о расходе `tutor` и образцы MathTrail.

### 7.1. Квота ИИ платформы, 2026-11-02

Для architecture 3.6; R11, R12. Прочитано 2026-11-02:

- цены Anthropic — [Pricing](https://platform.claude.com/docs/en/about-claude/pricing), даты на странице нет: за миллион токенов входа и выхода — «Claude Opus 5.5 … $4 / MTok … $20 / MTok», «Claude Sonnet 5.5 … $2 / MTok … $10 / MTok», «Claude Haiku 5.5 … $0.10 / MTok for prompts up to 100,000 tokens … $0.50 / MTok», «Claude Fable 5.1 … $10 / MTok … $50 / MTok». [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/thinking), даты нет: «Thinking has a cost: the tokens Claude spends reasoning are billed as output tokens, even when the thinking text isn’t returned to you». [Vision](https://platform.claude.com/docs/en/build-with-claude/vision), даты нет: «An image, therefore, costs ⌈width / 28⌉ × ⌈height / 28⌉ visual tokens». Цены те же, что в таблице `tutor`, прочитанной 2026-10-23 (architecture 3.3 tutor);
- оплата оборванного запроса — [How do I pay for my Claude API usage?](https://support.claude.com/en/articles/8977456-how-do-i-pay-for-my-claude-api-usage), даты на странице нет: «You're billed only for successful API calls and completed tasks. Failed requests aren't charged»; «If your client disconnects or times out in the middle of a request that was on track to succeed, that request is still charged»;
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), коммит be607e5, через `gh api`:
  - architecture 3.7 tutor: в сообщении о расходе «по записи на каждый вызов модели: у чтения рукописи их две, по читателю (R68 spec), а у чтения сочинения — одна…»; в записи — «поставщик и модель, как её назвал поставщик в ответе», токены, «оценка в долларах — по таблице цен `tutor`»; «Ответ, оборванный до подсчёта токенов, и вызов, прерванный до счёта, считаются по худшему случаю»; «ни содержания, ни идентификаторов в сообщении нет, и его можно писать в журналы (R09 spec)»; «по нему `api` считает квоты ИИ платформы (architecture 3.5 spec). На своём ключе `api` его не копит». О двух читателях: «Если хоть одному читателю рукописи поставщик отказал по лимиту, чтение отдаёт исход «лимит исчерпан» целиком»; «Но вызов читателя, который ответил, оплачен, и его запись остаётся в сообщении о расходе». Об исходе «лимит исчерпан»: «На своём ключе это повод сказать владельцу ключа, а на ИИ платформы — сигнал оператору: отказ — ключу платформы»; предел тира Anthropic — «до 00:00 UTC первого числа следующего месяца». Кода сообщения нет: «Кода продукта ещё нет: его строит эпик после T06»;
  - architecture 3.3 tutor: «Худший случай одного ответа — 4 000 токенов входа и 16 000 выхода: от $0,0084 у Haiku 5.5 до $0,84 у Fable 5.1». У Opus 5.5 по ценам выше это около $0,34 — наш счёт;
  - architecture 3.4 tutor, «Итог двух вызовов сочинений» (T92): цена сочинения у Opus 5.5 · `high` — «$0,030», у Haiku 5.5 · `low` — «$0,0006»;
  - architecture 3.12 tutor, серия T40: «Claude Opus 5.5 · `high` | 4 из 4 | 5 | 0 | $0,0098 | 5,7 с» — ответ с разбором. До поправки 2026-11-08 к R11 оценка брала эту цену за слова ступени, а это ответ судьи, не вызов слов;
- [altnovatrail/tutor, `docs/architecture.md`](https://github.com/altnovatrail/tutor/blob/9f85613/docs/architecture.md), коммит 9f85613 — последний на `main`, прочитано 2026-11-08 через `gh api`: architecture 3.4 tutor, «Итог замера подсказок» (T76, 2026-10-28), строка «Opus 5.5 · `high`» — p50 «5,1» с, «Цена вызова» — «$0,0152». Цены дня — «Opus 5.5 — $4 и $20 за 1 млн токенов, как в разделе 4»;
  - architecture 3.9 tutor: «Если ИИ, который нужен проверке задачи, ученику закрыт, `api` даёт задачи, которые проверяются без ИИ (architecture 4.2 spec; О-5): ученик не пишет работу, которую некому прочитать. Какие это задачи, решают `api` и `engine` — строка В64»; «сам ключ приходит только с работой (architecture 4.3 spec)»; отказ допуска — «исход «не допущен», отдельный от сбоя ИИ и от «лимит исчерпан», но ученик видит то же, что при исчерпанном лимите»; о первом приложении — «Ключ автора — это ключ взрослого, который сам и ученик, и владелец ключа: вызов идёт от его имени (privacy 5.2 spec), и путь «свой ключ» для `adult` открыт без фактов»;
  - R55 tutor: «С оценкой в долларах `api` считает квоты и юнит-экономику (О-14), не зная цен моделей»; отвергнуто — «лимит в вызовах или токенах — вызов чтения и вызов разбора стоят по-разному, а токены разных моделей — тоже». Это довод о лимите своего ключа, а квоту `api` он касается тем же доводом;
- [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), коммит 0901af0 — последний на `main`, через `gh api`:
  - R35 canvas-api: «Счётчики резервируются в начале проверки, вместе с арендой…»; «Резерв возвращается, если его вызов модели не понадобился или не был сделан»; «Ребёнку об этом сказано мягко: «I've helped all I can for now. Keep going on your own!» — на каждое нажатие Check. Времени фраза не обещает: сутки лимитов — UTC, и для семьи в США они кончаются ещё днём»;
  - R36 canvas-api: «После MVP — три тарифа»: бесплатный — «дневная квота помощи наставника, как в бете», «За $10 в месяц — квоты больше», «За $20 в месяц — квоты ещё больше и ИИ-наставник с голосом»;
  - spec 11 canvas-api: «**Сутки** для лимитов — UTC»; ежедневная очистка «удаляет счётчики старше 30 дней и истёкшие инвайты»;
  - spec 14.5 canvas-api: «Чтений ответа на ребёнка в день | 60», «Разборов черновика на ребёнка в день | 20», «Чтений и разборов на родителя в день, по всем детям | 180 / 60»; «**Худший день.** При средней цене вызова ≈ $0.14 на ребёнка и ≈ $0.41 на родителя, а потолок — если каждый вызов дойдёт до своего лимита выходных токенов (7.1) — ≈ $0.52 и ≈ $1.55»;
  - privacy 4 canvas-api: «| Дневные счётчики | ребёнка и родителя | Neon, `usage_daily`, `parent_usage_daily` | лимиты | 30 дней | нет |»;
- [mathtrail/mathtrail-standalone](https://github.com/mathtrail/mathtrail-standalone/tree/89986bc), коммит 89986bc — последний на `main`, через `gh api`:
  - R293 standalone: «The day of the limits is the family's: it ends at the family's midnight, by the clock the card tells with each answer»; «an offset no time zone keeps, more than twelve hours behind or fourteen ahead or off the quarters of an hour, is passed over»; «Until a card has told a clock, the day is the UTC day, as before. A clock counts from the next day on»; «No line of the log carries the offset»; почему — день лимитов кончался «at seven in the evening» для семьи на пять часов позади UTC; отвергнуты «a rolling day, so many tasks in any twenty-four hours, which needs no clock but makes "tomorrow" the hour the day began» и «the UTC day as it was»;
  - SPEC 10.3 standalone: «No refusal ever names a limit's number, because a number invites arithmetic rather than a lesson»;
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/a42a7e1), коммит a42a7e1, `docs/architecture.md`: «тариф и квота ИИ платформы в разделе для взрослых — О-14»; «Сбой ИИ или исчерпанная квота — нейтральная фраза (R04 spec)».
- цены Anthropic сверены снова 2026-11-11, для цены проверки на первом приложении (T42; measurements 2.1), и 2026-11-12, для прогона квоты (T52; measurements 3). Добавлено T62 по телам этих задач в архивах [docs/plan/E3.md](plan/E3.md) и [docs/plan/E6.md](plan/E6.md), а не по первоисточнику, и цитат там нет: у T42 — только «По ценам, сверенным 2026-11-11», без адреса, а у T52 — пересказ: «Цены Anthropic сверены 2026-11-12 ([Pricing](https://platform.claude.com/docs/en/about-claude/pricing)): Opus 5.5 — $4 и $20 за MTok входа и выхода, Haiku 5.5 — $0,10 и $0,50». Это те же цены, что 2026-11-02 выше, и по ним выходит $0,138 замера T42.

## 8. Урок

Для architecture 3.7: что об уроке решили соседи.

### 8.1. Урок у соседей, 2026-11-02

Для architecture 3.7; R13. Прочитано 2026-11-02 через `gh api`:

- [altnovatrail/app](https://github.com/altnovatrail/app/tree/3ce4cdb), коммит 3ce4cdb, подмодуль `spec` — на v0.6.2:
  - R06 app: «Место в уроке и состояние показа его примеров принадлежат прохождению урока на его пакете — ключ — ID урока и дайджест пакета»; «Обычный конец — всегда «Продолжить» на последнем звене, какого бы вида оно ни было: `app` говорит `api`, что урок пройден»; «Сам `api` кончает урок только необычным путём — досрочно или «без проверки»»; отвергнуто — «место в уроке на сервере: каждое «Продолжить» — запрос и новая строка данных ученика в privacy 4 spec»; не решает — «когда урок пройден и что `api` о нём записывает, и продолжает ли урок, начатый на другом устройстве, то же прохождение — T12 `api`»;
  - architecture 3.4 app: «Уход посреди урока ничего не шлёт: состояние урока лежит в базе профиля, пока его не сотрёт срок в 30 дней без изменений (раздел 3.2). Когда `api` снова даёт этот урок на том же пакете, `app` открывает его на месте и с показами, как они были.»; «Урок открывается на более позднем из двух мест: своём, из состояния урока, и том, что идёт после последней кончившейся задачи по ответу `api`.»; «Звено задачи, которая кончилась, полоса показывает пройденным, но не открывает»;
  - строка В15 таблицы 3 RUN.md `app`: «Состояние показа `api` знать не нужно — это ответ на вопрос T12 api»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/20ae268), коммит 20ae268, `docs/architecture.md`: «Пример с разбором в уроке — не задача и свидетельством не служит, а задача урока — такая же попытка, как вне урока (R58 spec; architecture 3.5 spec)»;
- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), коммит be607e5, `docs/decisions.md`: «Вызова `tutor` урок не добавляет»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), коммит 08bc48a, подмодуль `spec` — на v0.2.9: на входе движка — «уроки и пул задач»; в RUN.md, в задаче T01, которая не начата, решить — «когда движок даёт урок, а когда задачу вне урока (concepts 5 spec)»;
- [altnovatrail/engine, `docs/decisions.md`](https://github.com/altnovatrail/engine/blob/9751ba4/docs/decisions.md), коммит 9751ba4 — последний на `main`, прочитано 2026-11-08 через `gh api` для перехода на `spec` v0.9.11 (R24), а в `spec` его итог — R107 spec: R05 engine — «Закрепления в уроке и досрочного конца в первом срезе нет»; R09 engine — «данных ученика в вызове столько, сколько правило читает (privacy 1 spec): прохождения без мест звеньев, попытки с рубрикой — только «Готово»»; R03 engine — «Задача с виденным решением — 24 часа не кандидат, а потом — после всех задач навыка без виденного решения»;
- [altnovatrail/skill-map](https://github.com/altnovatrail/skill-map/tree/0295ce8), коммит 0295ce8, `docs/architecture.md`: «не выбирает следующий урок или задачу: это делает движок (concepts 12 spec)»; пакет рисует граф и состояние модели ученика «по готовым данным от `api` — ярусы, пререквизиты, владение»;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/a0db6e9), коммит a0db6e9: в дереве — документы и настройки, ни папок модулей, ни `lessons/`;
- `spec` [v0.6.4](https://github.com/altnovatrail/spec/blob/v0.6.4/RUN.md) — тогда тег новее подмодуля `api`, а с T14 подмодуль на v0.6.6 (R16), — RUN.md, «Что `app` нужно от контракта в E3»: о «урок пройден» — «Как `api` сверяет его с попытками урока и что записывает, решает T12 `api`»; о «что известно и что найти» — «`api` их не записывает, пока О-21 не решит, что они входят в свидетельство».

## 9. Задача с рубрикой

Для architecture 3.8: что о сочинении решили соседи.

### 9.1. Задача с рубрикой у соседей, 2026-11-02

Для architecture 3.8; R14, R15. Прочитано 2026-11-02 через `gh api`:

- [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), коммит be607e5, подмодуль `spec` — на v0.6.2, `docs/architecture.md`:
  - architecture 3.5 tutor, выход оценки: «по каждому критерию — номер, обоснование и выполнен ли он»; «критерий, которого нет в ответе или который назван дважды, `tutor` отдаёт без оценки, как в замере (раздел 3.4, «Оценка по рубрике»), а отзыв — как есть, через правило отзыва. Повторять ли тогда вызов, решает `api`. Сбой вызова оставляет без оценки все критерии, и ученик видит нейтральную фразу»; «видит ли ученик оценки по критериям, решают `api` и `app`: в открытом виде критериев нет»; «У критерия в формате `spec` есть только текст, без ID, поэтому `tutor` нумерует критерии по порядку в рубрике»;
  - architecture 3.5 tutor, чтение: «сочинение, которое не прочитать, до оценки не доходит… так что как `api` это показывает и записывает, — его детализация… Такого выхода у промпта замера нет: синтетика вся разборчива. Что делать с отдельным неразборчивым словом, покажет замер на сочинениях добровольцев»; в таблице типов запросов — architecture 3.10 tutor — чтение сочинения отдаёт «текст, как написан, и можно ли его прочитать»;
  - architecture 3.5 tutor, правило отзыва: функция «получает отзыв, прочитанный текст и язык объяснений. Отвечает «прошёл» или называет, какие части правила нарушены: предложения, слова, цитата, письмо»; «проверка утечки ищет в отзыве только фразы-утечки задачи, если автор их дал»; «что ученик видит вместо отзыва, который правило не прошёл, решает детализация `api` (R70 spec)»; журналы — «без содержания: ни прочитанного текста, ни обоснований, ни отзыва, хотя отзыв может цитировать сочинение»;
  - architecture 3.4 tutor, «Итог двух вызовов сочинений» (T92): у Opus 5.5 · `high` — «Правило отзыва по R70 spec нарушено» «2 из 45», цена сочинения «$0,030», время p50 «11,9» с;
  - architecture 3.7 tutor, исчерпанный лимит своего ключа: «на оценке по рубрике — нейтральную фразу, а критерии остаются без оценки, как при сбое вызова»;
- [altnovatrail/app](https://github.com/altnovatrail/app/tree/14f5a54), коммит 14f5a54, подмодуль `spec` — на v0.6.2:
  - R04 app: «сочинение с отзывом по рубрике — артефакт и живёт, пока его не сотрут»; в таблице architecture 3.2 app: «сочинение — с начала попытки, законченное и нет: история письма, прочитанное, отзыв по рубрике | база профиля | как история попытки | когда его сотрёт взрослый на странице «Это устройство»; ни конец попытки, ни срок его не стирают»;
  - architecture 3.3 app: «Сочинение — чтение и отзыв по рубрике, без вердикта (R75 spec); что идёт после отзыва, решает `api`»;
  - architecture 3.4 app: «На задаче «Продолжить» появляется, только когда `api` сказал, что задача кончилась»; «Сочинение с отзывом по рубрике остаётся на устройстве, но из полосы урока не открывается»;
- [altnovatrail/learner-model](https://github.com/altnovatrail/learner-model/tree/28433fa), коммит 28433fa, `docs/architecture.md`: «попытка задачи с рубрикой в первом срезе его не даёт (О-17)»;
- [altnovatrail/engine](https://github.com/altnovatrail/engine/tree/08bc48a), коммит 08bc48a, подмодуль `spec` — на v0.2.9, `docs/architecture.md`: «навыки с рубрикой и без оценки идут в порядке автора»; его T01, где это решается, не начата;
- [altnovatrail/modules](https://github.com/altnovatrail/modules/tree/97c20e2), коммит 97c20e2: в дереве — документы и настройки, модулей ещё нет;
- `spec` [v0.6.5](https://github.com/altnovatrail/spec/blob/v0.6.5/docs/privacy.md) — тогда тег новее подмодуля `api`, а с T14 подмодуль на v0.6.6 (R16), коммит 1a99391. П-19 там тот же, а П-8 дополнен 2026-11-02 по В9 `app`: «хранение сочинений на сервере по выбору владельца аккаунта приходит с модулем сочинений, до первого ребёнка: иначе единственная копия работы ученика зависит от правил браузера». В строке 6.2 его `COORDINATION.md` T13 уже стоит: «путь задачи с рубрикой (T13, …)»;
- прототип NovaTrail — `spec/draft/README.md` в `spec` v0.6.2: «An essay, such as "My best day", goes Plan, Write, Improve and ends with "I'm done"»; в таблице расхождений — «A short essay ends with "I'm done", and nothing checks it | An essay gets feedback by the module author's rubric: feedback, not a verdict».

## 10. Протокол

Для architecture 3.9: Connect, protobuf, buf, Fetch и CORS, время проверки у `tutor` и канал у canvas-api.

### 10.1. Connect, protobuf и buf, 2026-11-02

Для architecture 3.9; R17, R18. Прочитано 2026-11-02 — исходники по последним релизам, склонированные во временную папку сессии, и страницы документации, сверенные с их исходниками:

- connect-es [v2.3.0](https://github.com/connectrpc/connect-es/tree/v2.3.0) от 2026-10-27 — последний, коммит 6b7c593: `packages/connect-web/src/connect-transport.ts` — «By default, connect-web clients use the JSON format» и `const useBinaryFormat = options.useBinaryFormat ?? false;`, а у `useHttpGet` — «Controls whether or not Connect GET requests should be used when available, on side-effect free methods. Defaults to false»; `packages/connect/src/protocol/serialization.ts` — «In connect-es, unknown JSON fields are ignored by default» и `o.ignoreUnknownFields ??= true;`. У `connect-node`, напротив, «By default, connect-node clients use the binary format»;
- protobuf-es [v2.16.0](https://github.com/bufbuild/protobuf-es/tree/v2.16.0) от 2026-09-29 — последний, коммит b3b232f: `packages/protobuf/src/from-json.ts` — у `ignoreUnknownFields` «This option ignores unknown fields in parsing, as well as unrecognized enum string representations»; справочник сгенерированного кода — «Open enums can contain numeric values that are not declared in the generated TypeScript enum, so code that receives data from newer schemas should handle unknown enum numbers»; `bytes` в JSON — «JSON value will be the data encoded as a string using standard base64 encoding with paddings»; у `useProtoFieldName` по умолчанию `false`;
- connect-go [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) от 2026-10-21 — последний, коммит 8f69fe4, модуль `connectrpc.com/connect/v2`: кодек JSON в `connectproto/connectproto.go` — `MarshalOptions: protojson.MarshalOptions{Resolver: resolved.resolver}` без `UseProtoNames` и `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, а «Other settings can be configured by mutating the exported fields before use»; `connecthttp/protocol_connect.go` — GET разрешён только `StreamTypeUnary` с `IdempotencyNoSideEffects`; у `WithHTTPGet` — «Note, however, that servers don't automatically set any cache headers»;
- [протокол Connect](https://connectrpc.com/docs/protocol), даты на странице нет: о GET — «These requests do not require any special HTTP headers, and can be viewed in a browser tab. They are designed to be easy to make and easy to cache in browsers, proxies, and CDNs»; деталь ошибки — «the "value" field contains unpadded, base64-encoded binary Protobuf data»; коды — `invalid_argument` и `failed_precondition` — 400, `not_found` — 404, `permission_denied` — 403, `unauthenticated` — 401, `resource_exhausted` — 429, `unavailable` — 503, `unimplemented` — 501, а «there are no user-defined codes»;
- [Connect, CORS](https://connectrpc.com/docs/cors/), даты нет: «For side-effect-free RPCs such as simple queries, Connect supports GET requests that do not require CORS preflights», если «No request headers must be set»; «If your application uses custom request headers, you must explicitly allow them. This includes commonly used headers such as Authorization»;
- [ProtoJSON](https://protobuf.dev/programming-guides/json/), даты нет: «Adding values to an enum is safe if the "Emit enum values as integers" is set on all relevant clients»; [поведение перечислений](https://protobuf.dev/programming-guides/enum/), даты нет: «Proto3 and editions use *open* enums specifically because of the unexpected behavior that *closed* enums cause»;
- [правила buf breaking](https://buf.build/docs/breaking/rules/), даты нет: `RPC_SAME_IDEMPOTENCY_LEVEL` — в категориях `FILE`, `PACKAGE`, `WIRE_JSON` и `WIRE`, «This checks that the idempotency_level RPC option doesn't change. Doing so can result in different HTTP verbs being used».

### 10.2. Fetch, CORS и `<img>`, 2026-11-02

Для architecture 3.1 и 3.9; R03, R18. Прочитано 2026-11-02:

- [Fetch Standard](https://fetch.spec.whatwg.org/), «Last Updated 19 October 2026»: режим кеша запроса — «Unless stated otherwise, it is "default"», а «"default": Fetch will inspect the HTTP cache on the way to the network»; `fetch()` без своего `cache` режима не меняет. `Authorization` нет в безопасном списке заголовков запроса, и «A CORS non-wildcard request-header name is a header name that is a byte-case-insensitive match for `Authorization`». `Access-Control-Max-Age` — «Indicates the number of seconds (5 by default)», а запись кеша preflight держит URL, и совпадение требует, чтобы «entry's URL equals request's current URL»;
- [HTML Standard, «Updating the image data»](https://html.spec.whatwg.org/multipage/images.html#updating-the-image-data), «Last Updated 30 October 2026»: запрос картинки — «the result of creating a potential-CORS request given urlString, "image", and the current state of element's crossorigin content attribute», и заголовков страницы в нём нет;
- Chromium, [`services/network/cors/preflight_result.cc`](https://github.com/chromium/chromium/blob/98b9eab7ac9e36a7228c5143db756f699c8ec4e1/services/network/cors/preflight_result.cc), коммит 98b9eab: «constexpr base::TimeDelta kMaxTimeout = base::Hours(2);»; `preflight_cache.h` — «static constexpr size_t kMaxEntriesPerTopFrameSite = 32u;». Firefox, [`netwerk/protocol/http/nsCORSListenerProxy.cpp`](https://github.com/mozilla-firefox/firefox/blob/abbeff4fd9e02e3f6195153493fc0825eafe5d6d/netwerk/protocol/http/nsCORSListenerProxy.cpp), коммит abbeff4: «// Cap at 24 hours.».

### 10.3. Время проверки у `tutor`, 2026-11-02

Для architecture 3.9; R19. [altnovatrail/tutor](https://github.com/altnovatrail/tutor/tree/be607e5), коммит be607e5 — последний на `main` 2026-11-02, `docs/architecture.md`, прочитано тогда же через `git`:

- architecture 3.4 tutor, «Итог замера подсказок» (T76): в столбце «p50, с» — «| Opus 5.5 · `high` | … | 5,1 | $0,0152 |»;
- там же, «Итог двух вызовов сочинений» (T92): «| Opus 5.5 · `high` | 0 из 198 | 0 из 197 | 1,00 | 2 из 45 | $0,030 | 11,9 |», последний столбец — «Время p50, с»;
- времени вызова чтения и пары читателей в документах нет: отчёт чтения считает «**время** p50 и p95 и цена вызова», но прогоны лежат вне git, а пару считают по журналам отдельных читателей — покрытие и ошибки. Порядка, в котором идут читатели пары в продукте, документы не называют;
- судья с ключом — тот же вызов с картинкой работы: architecture 3.12 tutor, серия T45, медианы — «Opus 5.5 · `high` | 4 | 4 из 4 | 2,5 с» и «Haiku 5.5 · `low` | 9 | 7 из 8 | 2,7 с»; architecture 3.4 tutor — «**Время ответа**, p50: Opus — 2,5–3,2 с, Haiku · `low` — 2,2–2,8 с» (T66) и «Вызов Opus — $0,012 в форме A и $0,020 в форме D, p50 — 3,0–3,9 с» (T77);
- architecture 3.12 tutor: «Вердикт ждёт только вызова чтения, без ключа, а сильная модель — разбор и подсказка, с ключом — идёт только после «пока нет»».

### 10.4. canvas-api о канале, 2026-11-02

Для architecture 3.9; R19. [mathtrail/canvas-api, `docs/decisions.md`](https://github.com/mathtrail/canvas-api/blob/0901af0/docs/decisions.md), коммит 0901af0 — последний на `main`, прочитано 2026-11-02 через `gh api`: R23 canvas-api — «При оплате Cloud Run за запросы CPU выделяется только на время обработки запроса, и фоновая работа после ответа почти не получает процессора. Поэтому всё остаётся внутри запроса», а поправка 2026-10-15 — «Всё по-прежнему делается внутри запроса — довод о процессоре после ответа в силе»; R60 canvas-api — «Centrifugo и OpenFGA из стека не уходят: … они приходят в E15. В E2 их нет не потому, что «проверка — один запрос и один ответ», — этот довод … отверг, — а потому, что в E2 нет беты».

### 10.5. Connect v2, 2026-11-09

Для architecture 3.9; R33. Прочитано 2026-11-09:

- модуль контрактов `contracts/v0.9.11`, а с R40 — `contracts/v0.12.0`, где `go.mod` и `apiv1connect/account.connect.go` те же: `go.mod` требует `connectrpc.com/connect/v2 v2.0.2`, а `apiv1connect/account.connect.go` — `func RegisterAccountServiceHandler(server *connect.Server, svc AccountServiceHandler)`, у каждого метода — `if err := stream.Receive(&req); err != nil { return err }` перед вызовом обработчика, а у `UnimplementedAccountServiceHandler` — `connect.NewError(connect.CodeUnimplemented, …)`;
- `connectrpc.com/connect/v2` [v2.0.2](https://github.com/connectrpc/connect-go/tree/v2.0.2) — `go 1.26.0`, лицензия Apache-2.0, из модуля через `go mod download`; `docs/v2-guide.md`: «an interceptor can reject a call using only its headers, before any payload work, which is what makes authn be able to be an interceptor in v2 rather than HTTP middleware»; «an interceptor can observe individual messages by wrapping the stream before calling `next`»; «only a locally authored `*connect.Error` is serialized to the wire. Any other error reaches the client as a bare code with no message»; «`Mount` installs one `http.Handler` per registered procedure onto a mux»; `connectproto/connectproto.go`: у `NewJSONCodec` — `DiscardUnknown: true, // Ensure difference schema versions unmarshal.`, а `MarshalOptions` — поле, которое можно настроить «before use».

## 11. Удаление и выгрузка

Для architecture 3.10: сервис входа об удалении и свежем входе, RFC 9470, коды Connect, canvas-api и `app`, задания Cloud Run и `changelog` OpenFGA.

### 11.1. Firebase Auth об удалении и свежем входе, 2026-11-02

Для architecture 3.10; R20, R21. Прочитано 2026-11-02:

- Admin SDK для Go [v4.22.0](https://github.com/firebase/firebase-admin-go/tree/v4.22.0) — последний релиз, коммит 66a72c3, исходники через `gh api`:
  - `auth/auth.go`: у `Token` — ``AuthTime int64 `json:"auth_time"` ``; `VerifyIDTokenAndCheckRevoked` «verifies the provided ID token, and additionally checks that the token has not been revoked or disabled» и на каждом отказе возвращает `nil, err`; `checkRevokedOrDisabled` зовёт `GetUser` и сравнивает `token.IssuedAt*1000 < user.TokensValidAfterMillis`; `hasAuthErrorCode` — `fe, ok := err.(*internal.FirebaseError)`, без `errors.As`;
  - `auth/token_verifier.go`: `clockSkewSeconds = 300`, а `IsIDTokenInvalid` — «An ID token is considered invalid when it is malformed (i.e. contains incorrect data), expired or revoked», без «пользователя нет»;
  - `auth/user_mgt.go`: несуществующий пользователь — «no user exists with the …» с кодом `USER_NOT_FOUND`, и `IsUserNotFound` его узнаёт; `RevokeRefreshTokens` «updates the user's TokensValidAfterMillis to the current UTC second», и «existing ID tokens may remain active until their natural expiration (one hour)»; `UserMetadata` — «Timestamps are in milliseconds since epoch», а `LastRefreshTimestamp` — «The time at which the user was last active (ID token refreshed), or 0 if the user was never active»;
  - `auth/export_users.go`: `Users(ctx, nextPageToken)` перебирает пользователей проекта;
  - `integration/auth/user_mgt_test.go`: `TestDeleteNonExistingUser` — `DeleteUser` несуществующего пользователя отвечает ошибкой, которую узнаёт `IsUserNotFound`; `integration/auth/auth_test.go`: «If a token was issued, and then in the same second tokens were revoked, the token will have the same timestamp as the tokensValidAfterMillis, and will therefore not be considered revoked»;
- SDK Firebase для веба, `@firebase/auth` [1.13.8](https://github.com/firebase/firebase-js-sdk/tree/@firebase/auth@1.13.8) — коммит cc7ead4, тот же, что `firebase` 13.1.0, исходники через `gh api`:
  - `packages/auth/src/api/errors.ts`: `[ServerError.USER_NOT_FOUND]: AuthErrorCode.TOKEN_EXPIRED`;
  - `packages/auth/src/core/errors.ts`: `TOKEN_EXPIRED = 'user-token-expired'` и «The user's credential is no longer valid. The user must sign in again.»;
  - `packages/auth/src/core/user/invalidation.ts`: при `auth/user-disabled` и `auth/user-token-expired` SDK зовёт `signOut()` и бросает ошибку дальше;
  - `packages/auth/src/core/user/reauthenticate.ts`: повторный вход сверяет `user.uid === localId`, иначе `auth/user-mismatch`, и в него же переводит `auth/user-not-found`;
  - `packages/auth/src/model/public_types.ts`: `authTime` — «This is the time the user authenticated (signed in) and not the time the token was refreshed».

  Что повторный вход ставит новый `auth_time`, прямо не сказано нигде: это вывод из того, что `reauthenticateWithPopup` — новый вход у сервиса входа. На dev-проекте 2026-11-12 выход и новый вход окном сменили время последнего входа пользователя у сервиса входа — косвенная проверка, а не сам токен (T42; поправка 2026-11-12 к R20);
- [REST API Firebase Auth](https://firebase.google.com/docs/reference/rest/auth), обновлено 2026-10-20, обмен refresh token на ID token: «USER_NOT_FOUND: The user corresponding to the refresh token was not found. It is likely the user was deleted.»;
- [управление пользователями](https://firebase.google.com/docs/auth/web/manage-users), обновлено 2026-10-21: «Some security-sensitive actions—such as deleting an account, setting a primary email address, and changing a password—require that the user has recently signed in»;
- [сеансовые cookie](https://firebase.google.com/docs/auth/admin/manage-cookies), обновлено 2026-10-20: «For sensitive applications, the auth_time should be checked before issuing the session cookie», а в образце — `new Date().getTime() / 1000 - decodedIdToken.auth_time < 5 * 60`, иначе 401;
- [сеансы пользователя](https://firebase.google.com/docs/auth/admin/manage-sessions), обновлено 2026-10-20: «Firebase ID tokens are short lived and last for an hour», а refresh token перестаёт действовать, когда «The user is deleted»;
- [связи аккаунта Google со сторонними приложениями](https://support.google.com/accounts/answer/13533235), даты на странице нет: «If you delete a link, Google stops automatic sign-in to the app. This doesn't delete your data on the app». Снимает ли связь удаление пользователя у приложения, страница не говорит.

### 11.2. RFC 9470, 2026-11-02

Для architecture 3.10; R20. [OAuth 2.0 Step Up Authentication Challenge Protocol](https://www.rfc-editor.org/rfc/rfc9470), сентябрь 2023, прочитано 2026-11-02: «insufficient_user_authentication: The authentication event associated with the access token presented with the request does not meet the authentication requirements of the protected resource»; «max_age: This value indicates the allowable elapsed time in seconds since the last active authentication event associated with the access token»; в примере ответа — `HTTP/1.1 401 Unauthorized` с `max_age="5"`.

### 11.3. Коды Connect, 2026-11-02

Для architecture 3.10; R20. [протокол Connect](https://connectrpc.com/docs/protocol), даты на странице нет, прочитано 2026-11-02: `unauthenticated` — «Caller doesn't have valid authentication credentials for the operation», и «use `unauthenticated` if the user can't be identified or presents invalid credentials»; `failed_precondition` — «if the client must explicitly fix the system state before retrying».

### 11.4. Удаление в canvas-api, 2026-11-02

Для architecture 3.10; R21. [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), коммит 0901af0, прочитано 2026-11-02 через `gh api`:

- R27 canvas-api: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен», «Кортежи удаляются пачками до 100, отсутствующие пропускаются», «Аккаунт удаляется в порядке «дети → `parents` → отзыв токенов и `DeleteUser`»»;
- spec 9.8 canvas-api: «Повтор после сбоя на `DeleteUser` видит, что строки `parents` уже нет, и просто доводит удаление в Firebase»; «Другие устройства родителя стирают черновики, когда там откроется SPA и узнает об удалении»;
- spec 12.1 canvas-api: «Иначе ответ `REAUTH_REQUIRED`», а в таблице кодов — «`REAUTH_REQUIRED` | 401 | нужен свежий вход: `auth_time` старше 5 минут»;
- privacy 7 canvas-api: «Удаление требует свежего входа: PIN не подходит, ребёнок может его знать». П-7 canvas-api — «Хранить ли доказательство согласия после удаления аккаунта», и «решает юрист».

Ни задания, которое доводит удаление, ни ответа другим устройствам там нет: повтор вёл клиент.

### 11.5. `app` о стирании на устройстве, 2026-11-02

Для architecture 3.10; R20. [altnovatrail/app, `docs/architecture.md`](https://github.com/altnovatrail/app/blob/375a09b/docs/architecture.md), коммит 375a09b, прочитано 2026-11-02 через `gh api`. В architecture 3.2 app, «Когда что стирается»: «выход владельца аккаунта — всё аккаунта: базы всех профилей, общий кеш, реестр и сессия»; «вход другого аккаунта — если в реестре другой ID аккаунта, `app` стирает всё прежнее раньше, чем что-либо откроет … Так бывает, когда прежний вход кончился без выхода»; «удаление аккаунта — `api` отвечает, что аккаунта больше нет, и `app` стирает всё аккаунта и выходит».

### 11.6. Задания Cloud Run, 2026-11-09

Для architecture 3.10; R38. Прочитано 2026-11-09:

- [контракт контейнера](https://docs.cloud.google.com/run/docs/container-contract), даты нет: заданию Cloud Run задаёт `CLOUD_RUN_JOB` — «The name of the Cloud Run job being run», а сервису — `K_SERVICE`, «The name of the Cloud Run service being run»;
- [таймаут задачи](https://docs.cloud.google.com/run/docs/configuring/task-timeout), даты нет: «By default, each task runs for a maximum of 10 minutes», а наибольший — «up to 168 hours (7 days)»; «If the task attempt does not complete within this time, it will be stopped».

### 11.7. `changelog` OpenFGA, 2026-11-09

Для architecture 3.10; R38. `assets/migrations/postgres/001_initialize_schema.sql` в [openfga/openfga v1.22.1](https://github.com/openfga/openfga/blob/v1.22.1/assets/migrations/postgres/001_initialize_schema.sql), прочитано 2026-11-09: `CREATE TABLE changelog` с полями `store`, `object_type`, `object_id`, `relation`, `_user`, `operation`, `ulid`, `inserted_at TIMESTAMPTZ NOT NULL` и ключом `PRIMARY KEY (store, ulid, object_type)`; индекса по времени нет.

## 12. canvas-api: решения и спецификация

Чтение, на котором стоят сразу несколько разделов, — architecture 3.1, 3.2, 3.6, 3.9 и 3.10.

### 12.1. Решения и спецификация canvas-api, 2026-10-19

Для architecture 3.1, 3.2, 3.6, 3.9 и 3.10. [mathtrail/canvas-api, `docs/`](https://github.com/mathtrail/canvas-api/tree/0901af0/docs), ветка `main`, коммит 0901af0, прочитано 2026-10-19. Репозиторий склонирован во временную папку сессии (RULES.md, Citations). Цитаты дословные:

- R09 canvas-api: «OpenFGA работает сайдкар-контейнером в сервисе canvas-api», «хранилище OpenFGA — отдельная база в том же проекте Neon»;
- R14 canvas-api: «Все запросы, кроме горячего пути — выдачи токенов Centrifugo, — проверяют токен с отзывом (`VerifyIDTokenAndCheckRevoked`)»; «Перед `DeleteUser` вызывается `RevokeRefreshTokens`»; «Необратимые действия требуют свежего входа (`auth_time` ≤ 5 минут)»;
- spec 12.1 canvas-api: «**Свежий вход** — `auth_time` не старше 5 минут — нужен для удаления профиля ребёнка, удаления аккаунта и смены забытого PIN. Иначе ответ `REAUTH_REQUIRED`, и SPA просит войти заново»;
- spec 12.2 canvas-api: «PIN — барьер интерфейса, а не граница безопасности»; «Поэтому необратимые действия требуют свежего входа, а не PIN»;
- R27 canvas-api: «Удаление: сначала Neon, потом кортежи OpenFGA; повтор идемпотентен»;
- spec 9.8 canvas-api: «Дневные счётчики родителя остаются: иначе удаление и новый профиль снимали бы потолок трат»;
- R29 canvas-api: «Доступ в бету — по одноразовым инвайт-кодам», «Email canvas-api не читает»;
- R35 canvas-api: «Исчерпанный дневной лимит не прерывает занятие: ребёнок решает дальше сам, без проверки»; «Сделанный вызов засчитывается, даже если он не удался или вышел по тайм-ауту»;
- R23 canvas-api: «HTTP-ответ приходит последним и повторяет все события на случай разрыва WebSocket»;
- R41 canvas-api: «Разбор черновика идёт секунды, поэтому ранний `verdict` по WebSocket по-прежнему нужен»; отвергнуто автором 2026-10-11 — «убрать Centrifugo и Upstash и отдавать всё в HTTP-ответе», «убрать OpenFGA и проверять права кодом в Postgres»; «Довод «проверка теперь — один запрос и один ответ» без новых фактов повторно не предлагать».
- прочитано 2026-11-02 для architecture 3.1, тот же коммит, через `gh api`:
  - R06 canvas-api: «Модель OpenFGA заранее описывает учителя и класс, но кортежей для них MVP не пишет»;
  - R07 canvas-api: «Субъект доступа — всегда родитель: запросы идут с его Firebase ID token, профиль ребёнка передаётся параметром»;
  - R08 canvas-api: «система сразу готова к realtime и B2B»;
  - R26 canvas-api: «Статус `verified` ставит оператор командой `cmd/admin` после того, как получил подписанную форму (П-1)»; «До `verified` профили детей и холсты не создаются»;
  - R28 canvas-api: «Сервис находит или создаёт store `canvas`, сравнивает последнюю модель со встроенной и при расхождении записывает новую»;
  - spec 9.1 canvas-api: «пишется внутри транзакции Neon, до `COMMIT`» — о кортеже OpenFGA; «Он безвреден: права на пустой объект ничего не дают. Его удаляет ежедневная очистка» — о «висящем» кортеже;
  - spec 12.1 canvas-api: «Firebase Auth, провайдеры Google и Apple. Для Apple нужно членство в Apple Developer Program»;
  - spec 12.2 canvas-api: «не больше 5 попыток за 15 минут»; «На сервере хранится только хеш argon2id»; «меняется после свежего входа, без старого PIN» — о забытом PIN.
- прочитано 2026-11-02 для architecture 3.2, тот же коммит, через `gh api`:
  - R08 canvas-api: в составе стека — «Neon Postgres, Upstash Redis»; отвергнуто — «Firestore вместо Postgres»;
  - R54 canvas-api: «Neon: `aws-us-east-1` — та же агломерация, задержка до базы около 1–2 мс. У Neon регионы только в AWS (документация Neon, прочитана 2026-10-14)» — о паре с Cloud Run в `us-east4`; отвергнуто — `us-central1`: «ближайший Neon — `aws-us-east-2` (Огайо), около 15–20 мс на каждый запрос к базе»;
  - R27 canvas-api: «`changelog` OpenFGA сам не чистится и хранил бы связи «uid ↔ ребёнок» бессрочно»; «Ежедневная очистка удаляет «висящие» кортежи и таблицу `changelog` OpenFGA старше суток»;
  - spec 3.2 canvas-api: миграции — «`github.com/pressly/goose/v3` | v3.28.0»; spec 11 canvas-api: «Она запускается как Cloud Run job с образом нового релиза до выкладки сервиса»; spec 15.5 canvas-api: миграции — «по схеме «сначала добавить, потом убрать»»;
  - spec 3.3 canvas-api: «Neon | Free | 0,5 ГБ на проект, 100 CU-часов в месяц, автопауза через 5 мин (не отключается), восстановление на момент времени — 6 часов»;
  - privacy 7 canvas-api: база `canvas` удаляется «сразу; из истории восстановления (PITR) — через 6 часов», а в базе `openfga` «`changelog` — ежедневной очисткой, до суток; из PITR — через 6 часов после этого»;
  - privacy 8 canvas-api: профиль ребёнка без активности 12 месяцев «удаляется автоматически, без отдельного уведомления. Этот срок заранее указан в уведомлении при согласии: email у нас нет, писать некуда (П-6)».
