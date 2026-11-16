# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T97\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T96. Раскладка плана по R153–R155 spec

- **Цель:** план `api` — в раскладке и на языке заготовки `spec` v0.18.3: правила RUN.md и таблицы — на английском, кратко, для сессии; таблицы 2–4 — `run/EPICS.md`, `run/QUESTIONS.md` и `run/DEBT.md` вместо `DEBT.md` в корне; открытые задачи вне эпика — `run/TASKS.md`; правило 7 называет контракт исполнителя оркестратора.
- **Почему:** R153, R154, R155 и R156 spec, строка Д250 spec; переход T94 оставил раскладку своей задаче (правило 11; R84).
- **Сделать:**
  - по `spec/repo-template/files/` и выводу `just new-repo-draft api go 3.5 v0.18.3` — RUN.md (разделы «Rules», «Epics», «Table 1. Current epic — …»), `run/` с началами таблиц, CLAUDE.md — абзацы о RUN.md и `run/`, README.md, где называет план; отличия правил `api` (R63 и другие решения журнала) — сохранить;
  - тела задач, описание эпика и архивы `docs/plan/` — на русском (R154 spec); ссылки на `DEBT.md` в документах — на `run/DEBT.md`, в архивах — тоже, если их проверяет `docs-check`;
  - `docs-budgets.json` — пределы новых файлов, если нужны; поправки к записям журнала, текст которых называет место таблиц; решение журнала.
- **Как проверить:** `just -f spec/justfile docs-check-repo`; `/code-review high`.
- **Разделы:** R153, R154, R155, R156 spec; Д250 spec; repositories 2.2 spec; R63, R84.

#### T97. Ответы контракта T160 spec: причины отказов

- **Цель:** `api` отвечает так, как называет контракт `spec` v0.18.3: причины отказов, которые дал T160 spec.
- **Почему:** Д245, Д246 и Д247 spec — ответы `spec` на В68, В71 и В76; T94 перенёс модули Go на тег, а причины не шлёт (R84). Запросы работ ученика при ожидании согласия (поправка 2026-11-15 к R113 spec) сделала T94: без них выгрузка версии 3 называла бы работы, которых `app` не прочтёт.
- **Сделать** (по [тексту контрактов](../spec/contracts/README.md), section 15):
  - Д245 spec: `own_key` без поставщика или ключа — `invalid_argument` без `Refusal` в `CheckOwnKey` и в `ownKeySent` попытки;
  - Д246 spec: `PutWork` — вид работы по задаче попытки или по прежней работе; дайджест пакета, которого индекс образа не знает, — `CAUSE_PACKAGE_UNKNOWN`; версия вне окна форматов без прежней работы — `failed_precondition` и `CAUSE_VERSION_NOT_GIVEN`; отозванная версия, которую образ держит, — вид по задаче;
  - Д247 spec: `CheckOwnKey` сверх предела R78 — `resource_exhausted`, `CAUSE_KEY_CHECKS_EXHAUSTED` и `Refusal.next_key_check`;
  - сверить с тем же разделом прочее, что изменилось с v0.17.12: сообщение больше 11 MiB — `resource_exhausted` без `Refusal`, `history_length` от 0, право на слова после верного ответа (R77);
  - тесты; поправки к R70, R72, R73 и R78, к каким нужно; architecture 3.9.
- **Решить при планировании:** что из списка `api` уже делает — сверить код с текстом контрактов, прежде чем менять.
- **Как проверить:** `just ci-lint`, `ci-test`, `ci-vuln`, `ci-licenses`; `just fuzz 5m` тех тестов, чей читатель меняется; `/code-review high`. Вызовов ИИ нет, $0.
- **Разделы:** Д245, Д246, Д247 spec; [текст контрактов](../spec/contracts/README.md), section 15; R70, R72, R73, R77, R78, R84.
