# RUN.md — work plan of api

What api does, does not do and has still to decide — [docs/architecture.md](docs/architecture.md); why — [docs/decisions.md](docs/decisions.md). The platform is in `spec/`, a submodule at tag v0.19.12: the component's card — architecture 3.5 spec; the order of repositories — repositories 6 spec; learners' data and law — privacy spec; the platform's decisions — spec's log. Small tasks, one at a time, each reviewed by the author; only the current epic is in detail (R19 spec). Other tables, each a file opening with its rules (R153 spec): 2, epics for later — [run/EPICS.md](run/EPICS.md); 3, questions to the author — [run/QUESTIONS.md](run/QUESTIONS.md); 4, debt — [run/DEBT.md](run/DEBT.md). Open tasks outside an epic — [run/TASKS.md](run/TASKS.md) (R155 spec). The archives of the done epics — [docs/plan/](docs/plan/).

Language (R154 spec): rules and tables in English, terse, for the session; the bodies of tasks in table 1 and in `run/TASKS.md` in Russian — the author writes and reviews them.

## Rules

A task starts with «Выполни задачу T01 из RUN.md» in a new session in the devcontainer. One task — one session (R143 spec): the hook `UserPromptSubmit` of [.claude/settings.json](.claude/settings.json) — `spec/tools/session/` — stops «Выполни задачу T…» or «планируй T…» of another task in the same session and names `/clear`; the same task passes.

1. Do only the named task; touch other tasks' files only where it requires.
2. Read first: [CLAUDE.md](CLAUDE.md) with the rules it imports (RULES.md of `spec/`), the task — in table 1, or only its own body of `run/TASKS.md` (its opening says how) —, the sections it cites — its own and spec's —, [docs/decisions.md](docs/decisions.md).
   - Conflict with the documents or something missing — stop and ask; never guess.
   - A gap in the component itself — a subsection of [docs/architecture.md](docs/architecture.md), section 2. A gap across repositories — a row of [run/QUESTIONS.md](run/QUESTIONS.md) whose question opens with the mark «В реестр spec»: spec's session finds it on `main`, and it gets its О-, П- or Т- number once the author decides it there (R81 spec; RULES.md, Across repositories).
   - From `run/DEBT.md` and `run/QUESTIONS.md` take only the rows the task and its sections name and those `grep -n` finds by the paths and parts it changes (R143 spec).
3. **Devcontainer only.** If `NOVATRAIL_DEVCONTAINER` is not `1`, stop before the first edit and ask to reopen in the container; "only documents" is no reason to go on. If `spec/` is empty, ask for `git submodule update --init`. Exception: a cloud session the author started for the task runs the checks it can reach, and its report names each one that did not run.
4. Outside this repository — editing `spec` or other repositories, their settings, anything that costs money — only after a warning (what, where, cost) and the author's confirmation. `spec/` is read only here; a task of its own moves it to a new tag.
   - Exception, no confirmation: the task's checks by R146 spec — browser: Playwright in the devcontainer, custom-token sign-in of a stand-in user on the dev project; AI: the `tutor` stub in `api` dev mode. A real AI call only where the task checks the model's words: dev key under the author's spend limit at the provider, within the task's ceiling, or with confirmation where it has none; «Сделано» names the provider, the calls and the cost.
   - Nothing on devices: engines are Chromium, WebKit and Firefox in CI; what only a device shows — pen, device voices, VoiceOver and TalkBack, the home screen — stays a row of [run/DEBT.md](run/DEBT.md) until the author decides otherwise.
5. Facts on AI providers, laws, prices and other platforms — from the primary source, a record of `docs/sources.md` with quote and date read (RULES.md). What is not confirmed is not stated: it becomes an open question.
6. End with the checks of what the task changed: each part's checks are its rule, `.claude/rules/<part>.md` (CLAUDE.md, Environment); documents — `just -f spec/justfile docs-check-repo` (R32 spec, R145 spec).

   Then `/code-review high`, one round: fix the findings, do not rerun. A small change may skip the review even if «Как проверить» names it.
7. Do not commit: the author commits after review. A run of spec's orchestrator, «Сделай эпик E…», is the author's word otherwise: its executor works by the contract `spec/tools/orchestrator/executor.md` — commits, pushes and opens the pull request, never merges (R156 spec).
8. End with a short report in Russian: what was done, files, how to check, what the review found or why there was none, what is open and what goes «в реестр spec».
9. When done: table 1 `[ ]` → `[x]`; append to the body «Сделано (дата)» — one line, what was done and its pull request, which says how it was checked; where the author commits after review, the report says it. What stays open is rows, not text: a question for the author, of [run/QUESTIONS.md](run/QUESTIONS.md); what is put off, of [run/DEBT.md](run/DEBT.md) (R160 spec).
   - Then move the body verbatim, relative links fixed, to the epic archive `docs/plan/<epic>.md`, with a gist sentence — an unheaded list item at the start of the receiving section: «T01 — сделана <дата>: <итог одной фразой>». Neither stays here, and a search by heading finds one body, in the archive (R153 spec).
   - A task outside an epic's plan — a move to a tag of `spec`, an analysis by the threshold of R145 spec — is a row of table 1, and its body is in `run/TASKS.md` (R155 spec). Done, it goes to the current epic's archive; between epics, table 1 holds such rows alone, and they go to the archive of the epic that comes into table 1 next (R63; R85).
10. Language (RULES.md spec, Hard rules; R154 spec): this plan and `run/` in English, the bodies of tasks, the epic archives `docs/plan/` and the reports in Russian; `docs/` in English; README, CLAUDE.md, AGENTS.md, code, comments, schema descriptions, prompts and normative texts for authors in English; commit messages and a pull request's title and description in English and terse, for the session, never a copy of the report.
11. A task larger than one review (≈ 30–60 minutes of reading): propose a split before starting.
12. Before a new task, close the findings on the previous one (R30 spec); an allowed departure from rule 1. SonarCloud sees merged code: its verdict on the previous task is in [New Code](https://sonarcloud.io/summary/new_code?id=altnovatrail_api); CodeQL and Scorecard — the Security tab, `gh api 'repos/altnovatrail/api/code-scanning/alerts?state=open'`. Fix in code first; "false positive" is the last resort, with the reason in words; what a change cannot close stays open — shared reasons in R33 spec, own ones in the log. Never skip silently. Report these fixes in a separate paragraph: the author commits them apart from the task.

**Statuses:** `[ ]` not started; `[x]` done, the executor marks it; `[-]` not done — the author's decision, reason in the task; `[?]` deferred — the author's decision, reason and when to return in the task. A task that fails the author's review returns to `[ ]` with what to fix.

**Who:** «Claude» — the executor does it, the author reviews; «Claude, проверяю я» — texts the author reads line by line; «я + Claude» — the author does some steps: decisions, sign-ins, GitHub settings, confirmations by rule 4.

**Plan:** «да» — plan mode first: decisions missing from the documents, or larger than one review (rule 11); «—» — run at once.

**Task body** (Russian): «Цель», «Сделать», «Как проверить», «Разделы»; as needed «Не делать», «Решить при планировании», «Мой шаг», «Стоимость», «Риски»; from planning «Почему», «Решено автором (дата)», «Выяснилось при планировании»; when done «Сделано (дата)» (rule 9).

**Citations** — RULES.md spec, Citations: a task's number stands only here, in `run/` and in `docs/plan/` (R171 spec). Own sections — `architecture 2`, own decisions — `R03`; spec's sections and decisions — «architecture 3.5 spec», «R18 spec», its open questions — «Т-22», «П-7», «О-2»: the repository keeps no registers; other NovaTrail repositories' decisions — «R03 tutor»; MathTrail's — «R44 canvas-api», «R163 standalone», in a list «canvas-api: R44, R59». Each log numbers its own: «R19 spec» and `R19` of this log differ. A task — «T03», from another repository «T03 api»; one not here is in `run/TASKS.md` or an archive: `grep -n '^#### T03\.' RUN.md run/TASKS.md docs/plan/*.md`. Task numbers are global and never reused: take the one free when writing it down.

## Epics

- **Table 1** is the current epic: its tasks in detail, run one at a time.
- **Next epic.** Once table 1 is all checked, the author picks the next epic from [run/EPICS.md](run/EPICS.md) and splits it with Claude in plan mode into tasks of one review each, into table 1 and this file.
- **A done epic** goes to `docs/plan/<epic>.md` (R59 canvas-api): bodies and gist sentences are there already (rule 9); move its description and table of tasks verbatim; questions and debt stay in `run/`; until the next epic, table 1 reads «Current epic — none» and names it as the last done epic; measure the epic's sessions — `just -f spec/justfile tokens-report --since <first day of the epic> --row` — as a row of the table of measurements 4.2; a new analysis by the threshold of R145 spec. Never read an archive whole: take the task cited.

## Table 1. Current epic — none

The last done epic is E13, [docs/plan/E13.md](docs/plan/E13.md); the author picks the next one at planning. Until then — tasks between epics (rule 9), their bodies in [run/TASKS.md](run/TASKS.md) and, done, in the archive of the next epic, [docs/plan/E14.md](docs/plan/E14.md).

| Статус | ID | Задача | Кто | План | Зависит от |
|---|---|---|---|---|---|
| [x] | T93 | Тариф `dev_check` для `dev-author` на dev-проектах (В80) | Claude | да | — |
| [x] | T94 | Переход на тег `spec` v0.18.3 | Claude | — | — |
| [x] | T95 | CI на `.github` v0.1.24 | Claude | — | T94 |
| [x] | T96 | Раскладка плана по R153–R155 spec | Claude | — | T95 |
| [x] | T97 | Ответы контракта T160 spec: причины отказов | Claude | да | T96 |
| [x] | T98 | Скорость CI: строка 7.15 | Claude | — | — |
| [x] | T99 | Скорость CI: релиз `.github` строки 7.13 | Claude | — | T98; релиз `.github` строки 7.13 |
| [x] | T100 | Скорость CI: тег `spec` строки 7.14 | Claude | — | T99; тег `spec` строки 7.14 |
| [x] | T101 | Переход на тег `spec` v0.18.6 | Claude | — | — |
| [x] | T102 | `just dev-app` с синтетическим ребёнком | Claude | — | — |
| [x] | T103 | `access_returns` в `OwnKeyLimit` | Claude | — | — |
| [x] | T104 | Режим чтения почерка: `API_READING_MODE` | Claude | — | `tutor` v0.1.45 |
| [x] | T105 | Поправка уровня взрослым | Claude | да | тег `spec` с T178; `learner-model` строки 4.20 |
| [x] | T106 | Переход на релиз `tutor` с записями допуска словом автора | Claude | — | `tutor` v0.1.46 |
| [x] | T107 | README без «Status», «License», фразы о языках и версии spec | Claude | — | — |
| [x] | T108 | Переход на тег `spec` v0.18.14 | Claude | — | — |
| [x] | T109 | Разбор долга: строки, чей срок пришёл | Claude | — | — |
| [x] | T110 | Индекс `modules` с `english-essays` 0.2.1 | Claude | — | `modules` v0.2.12 |
| [x] | T111 | README — только описание компонента и его место в архитектуре | Claude | — | — |
| [x] | T112 | Переход на релиз `tutor` с кешем инструкций слов | Claude | — | `tutor` v0.1.49 |
| [x] | T113 | Переход на тег `spec` v0.18.27 | Claude | — | — |
| [x] | T114 | Тексты без истории (R160 spec) и тег `spec` v0.18.34 | Claude | да | — |
| [x] | T115 | Тексты без истории: CLAUDE.md, README, AGENTS.md, правила частей, `docs/README.md`, `docs/build.md`, архитектура 1, 2, 4 | Claude | — | T114 |
| [x] | T116 | Тексты без истории: архитектура 3.1–3.4 | Claude | — | T114 |
| [x] | T117 | Тексты без истории: архитектура 3.5–3.8 | Claude | — | T114 |
| [x] | T118 | Тексты без истории: архитектура 3.9–3.11 | Claude | — | T114 |
| [x] | T119 | Комментарии кода по R160 spec: `internal/learning` | Claude | — | T114 |
| [x] | T120 | Комментарии кода по R160 spec: `internal/store`, `account`, `config` | Claude | — | T114 |
| [x] | T121 | Комментарии кода по R160 spec: `cmd/api`, `internal/` от `rpc` до `logging` | Claude | — | T114 |
| [x] | T122 | Комментарии кода по R160 spec: остальной Go, justfile, CI, Dockerfile, скрипты | Claude | — | T114 |
| [x] | T123 | `"comments": true` с релизом `learner-model` по R160 spec | Claude | — | T114; релиз `learner-model` строки 7.29 |
| [x] | T124 | Переход на тег `spec` v0.19.4 | Claude | — | — |
| [x] | T125 | Переход на тег `spec` v0.19.11 | Claude | — | — |
| [x] | T126 | Ссылки по R171 spec: план, CLAUDE.md, правила частей, `docs/README.md`, `docs/build.md`, комментарии вне Go; разбивка на T127–T142 | Claude | да | T125 |
| [x] | T127 | Ссылки по R171 spec: комментарии `internal/learning` | Claude | — | T126 |
| [x] | T128 | Ссылки по R171 spec: комментарии `internal/store` с миграциями SQL, `internal/account`, `internal/config` | Claude | — | T126 |
| [x] | T129 | Ссылки по R171 spec: комментарии `cmd/api`, `internal/admission`, `rpc`, `sweep`, `packages` | Claude | — | T126 |
| [x] | T130 | Ссылки по R171 spec: комментарии остального Go | Claude | — | T126 |
| [x] | T131 | Ссылки по R171 spec: архитектура 1, 2, 3.1–3.4, 4 | Claude | — | T126 |
| [x] | T132 | Ссылки по R171 spec: архитектура 3.5–3.8 | Claude | — | T126 |
| [x] | T133 | Ссылки по R171 spec: архитектура 3.9–3.11 | Claude | — | T126 |
| [x] | T134 | Ссылки по R171 spec: `docs/sources.md` и `docs/measurements.md` | Claude | — | T126 |
| [ ] | T135 | Ссылки по R171 spec: журнал: вводная и разделы от «The `spec` tag» до «Database and learner model» | Claude | — | T126 |
| [ ] | T136 | Ссылки по R171 spec: журнал: от «Module versions» до «The `spec` tag: v0.6.6» | Claude | — | T126 |
| [ ] | T137 | Ссылки по R171 spec: журнал: от «Protocol `app` ↔ `api`» до «Database and migrations» | Claude | — | T126 |
| [ ] | T138 | Ссылки по R171 spec: журнал: от «Devcontainer» до «The database of E3 in code» | Claude | — | T126 |
| [ ] | T139 | Ссылки по R171 spec: журнал: от «Attempt without AI in code» до «The learner model in code» | Claude | — | T126 |
| [ ] | T140 | Ссылки по R171 spec: журнал: от «The engine choice in code» до «Withdrawal on a schedule» | Claude | — | T126 |
| [ ] | T141 | Ссылки по R171 spec: журнал: от «The `spec` tag: v0.16.3» до «The board module» | Claude | — | T126 |
| [ ] | T142 | Ссылки по R171 spec: журнал: от «The providers of the own key» до конца; ключ `"citations": true` | Claude | — | T127–T141, T144; релиз `learner-model` и тег `spec` с `.proto` без ссылок |
| [x] | T143 | Переход на тег `spec` v0.19.12 | Claude | — | — |
| [x] | T144 | Ссылки по строгому правилу R171 spec: проход по первой части | Claude | — | T143 |
