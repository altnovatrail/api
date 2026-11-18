# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T118. Тексты без истории: архитектура 3.9–3.11

- **Цель:** разделы 3.9–3.11 `docs/architecture.md` держат текущее устройство, без истории (R160 spec).
- **Сделать:** как в T116.
- **Как проверить:** `just -f spec/justfile docs-check-repo`; `/code-review high`.
- **Разделы:** R160 spec; architecture 3.9–3.11.

#### T119. Комментарии кода по R160 spec: `internal/learning`

- **Цель:** комментарии `internal/learning` — по R160 spec, «Comments».
- **Сделать:** комментарий говорит, чего не скажут код и имена: почему, ограничение, ловушка, контракт экспортируемого API; без пересказа кода, истории, номеров задач и разделов документов; где причина — решение, ограничение одной фразой и номер в конце, `(R25)`; номер решения `spec` — `(R25 spec)`, другого репозитория — `(R100 tutor)`. Код и тесты — те же.
- **Как проверить:** `just -f spec/justfile docs-check-repo`; проверки правила `.claude/rules/go.md`; `/code-review high`.
- **Разделы:** R160 spec, R32 spec.

#### T120. Комментарии кода по R160 spec: `internal/store`, `internal/account`, `internal/config`

- **Цель:** комментарии этих пакетов — по R160 spec, «Comments».
- **Сделать:** как в T119.
- **Как проверить:** как в T119.
- **Разделы:** R160 spec, R32 spec.

#### T121. Комментарии кода по R160 spec: `cmd/api` и пакеты `internal/` от `rpc` до `logging`

- **Цель:** комментарии `cmd/api`, `internal/rpc`, `internal/tutorstub`, `internal/packages`, `internal/admission`, `internal/model`, `internal/sweep` и `internal/logging` — по R160 spec, «Comments».
- **Сделать:** как в T119.
- **Как проверить:** как в T119.
- **Разделы:** R160 spec, R32 spec.

#### T122. Комментарии кода по R160 spec: остальной Go, justfile, CI, Dockerfile и скрипты

- **Цель:** комментарии остального кода — по R160 spec, «Comments»: пакеты `internal/`, которых нет в T119–T121, `cmd/modules-check`, Go в `scripts/`, а также `#` justfile, `.github/`, `Dockerfile`, `.devcontainer/` и `scripts/`.
- **Сделать:** как в T119; файлы `.proto`, скопированные из релизов, — не трогать, `just learner-model-proto check` сверяет их с релизом. Проверки частей — по `.claude/rules/go.md` и `.claude/rules/ci.md`.
- **Как проверить:** `just -f spec/justfile docs-check-repo`; проверки правил `.claude/rules/go.md` и `.claude/rules/ci.md`; `/code-review high`.
- **Разделы:** R160 spec, R32 spec.

#### T123. `"comments": true` в `docs-budgets.json` с релизом `learner-model` по R160 spec

- **Цель:** `docs-check-repo` проверяет номера решений в комментариях кода (R32 spec).
- **Почему:** в `internal/export/proto/novatrail/learnermodel/v1/common.proto` — копии релиза `learner-model` — комментарий называет `R135 of spec`, и проверка читает его как `R135` этого журнала, которой нет; файл правится только новым релизом `learner-model` (T114).
- **Сделать:** перейти на релиз `learner-model`, чей `common.proto` пишет номер по R32 spec: `go.mod`, `just learner-model-proto`, что релиз несёт — по правилам `.claude/rules/go.md`; добавить `"comments": true` в `docs-budgets.json`.
- **Как проверить:** `just -f spec/justfile docs-check-repo` с проверкой комментариев; проверки правила `.claude/rules/go.md`; `/code-review high`.
- **Разделы:** R160 spec, R32 spec; строка 7.29 COORDINATION.md spec.
