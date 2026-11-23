# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T145. Ключ `"citations": true` с релизом `learner-model` и тегом `spec`, чьи `.proto` без ссылок

- **Цель:** `"citations": true` в `docs-budgets.json`, как в `spec/repo-template/files/docs-budgets.json`; `just -f spec/justfile docs-check-repo` с ним — зелёный.
- **Почему:** T142 переписала журнал по строгому правилу R171 spec, но проверка с ключом находит 16 ссылок в копиях `.proto` из `internal/export/proto/`, которые `api` не правит: `learnermodel/v1/` — файлы релиза `learner-model` (`just learner-model-proto check`; v0.1.16 в `go.mod` и последний v0.1.18 — со ссылками, правка — PR #100 `learner-model`), `observation/v1/` — `spec/contracts` на теге (v0.19.12 и v0.19.13 — со ссылками). Решено исполнителем по рекомендации — строка В88.
- **Сделать:** перейти на релиз `learner-model`, чьи `.proto` без ссылок: `go.mod`, `just learner-model-proto`, проверки правила `go` и `just ci-modules`; на тег `spec`, чей `contracts/novatrail/observation/v1/observation.proto` без ссылок, — по тексту перехода на тег (`spec/tools/orchestrator/tasks/spec-tag.md`), если волна 0 оркестратора его ещё не сделала: подмодуль, модули Go `spec` и `spec/contracts`, копия `observation/v1/`, тег в CLAUDE.md и RUN.md. Затем — ключ; находки с ним вне копий — по строгому правилу (текст — в теле T144).
- **Как проверить:** `docs-check-repo` с ключом — зелёный; `just ci-lint`, `ci-test`, `ci-vuln`, `ci-licenses`, `ci-modules`; `just dev-check` при переходе на тег или релиз.
- **Разделы:** R171 spec; T142, T144; В84, В85, В88.
