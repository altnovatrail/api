# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T123. `"comments": true` в `docs-budgets.json` с релизом `learner-model` по R160 spec

- **Цель:** `docs-check-repo` проверяет номера решений в комментариях кода (R32 spec).
- **Почему:** в `internal/export/proto/novatrail/learnermodel/v1/common.proto` — копии релиза `learner-model` — комментарий называет `R135 of spec`, и проверка читает его как `R135` этого журнала, которой нет; файл правится только новым релизом `learner-model` (T114).
- **Сделать:** перейти на релиз `learner-model`, чей `common.proto` пишет номер по R32 spec: `go.mod`, `just learner-model-proto`, что релиз несёт — по правилам `.claude/rules/go.md`; добавить `"comments": true` в `docs-budgets.json`.
- **Как проверить:** `just -f spec/justfile docs-check-repo` с проверкой комментариев; проверки правила `.claude/rules/go.md`; `/code-review high`.
- **Разделы:** R160 spec, R32 spec; строка 7.29 COORDINATION.md spec.
