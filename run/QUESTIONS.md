# QUESTIONS.md — questions to the author of `api`

Table 3 of [RUN.md](../RUN.md): the author's open questions from tasks' «Открыто» (R153 spec, R154 spec; R85).

- Never read whole: take the rows the task and its sections name — `grep -n '^| В1 ' run/QUESTIONS.md`.
- A row comes from a task's «Открыто» (RUN.md rule 9) or from a run of spec's orchestrator that waits on the author (R156 spec). Record the author's answer where it belongs — the task, a document or [DEBT.md](DEBT.md) — and delete the row.
- A row whose question opens with the mark «В реестр spec» is for `spec`: its session finds it on `main` (R81 spec; RULES.md, Across repositories). It leaves once `spec` has recorded it: the task that rests on it names its number.

## Table 3. Questions to the author

Numbers are not reused: next — В84.

| ID | Question | From | Options | Recommendation |
|---|---|---|---|---|
| В82 | В реестр spec: с T105 журнал свидетельств `api` хранит поправку уровня взрослым — факт без попытки (R157 spec; [текст контрактов](../spec/contracts/README.md), раздел 15: «The fact goes into the export»). В выгрузке версии 3 у записи журнала `evidence_entry` поле `attempt_id` обязательно и описано как «The attempt the evidence is of; one per attempt», `package_digest` — «The digest of the attempt's package, by which wrong_key versions are filtered»; места для факта без попытки нет. `api` пишет поправку записью журнала с её собственным ID в `attempt_id` и пакетом текущей версии модуля в `package_digest`; сама запись несёт `level_set_by_adult: true` и `task_id` `<модуль>/level/<полоса>` (R89). Схема и правила выгрузки это принимают, описание поля — нет; `api` сворачивает поправку при любом пакете, и `wrong_key` её не отсекает | T105 | (а) следующая версия выгрузки: `attempt_id` у записи поправки нет, вместо него `correction_id`, а `package_digest` описан для обоих видов записей, и фильтр `wrong_key` — только для попыток; (б) описание `attempt_id` в версии 3 расширить на ID поправки — внутри линии `release-version` не даёт менять аннотацию (как Д263 spec); (в) поправка — отдельным списком файла профиля | (а): запись журнала остаётся одной сущностью, а читатель отличает поправку по полю, а не по содержимому свидетельства; до новой версии `api` пишет ID поправки в `attempt_id`, как сейчас, и выгрузка проходит схему и правила версии 3 |
| В83 | В реестр spec, для `.github`: Go-кэши `go.yml` (v0.1.31, v0.1.32) берутся только по точному ключу `go-<os>-<arch>-<проверка>-<хеш Dockerfile>-<хеш go.sum>`, без `restore-keys`. В `api` после T101 из семи тёплых прогонов CI на `main` пять сменили `go.sum` (переходы `tutor`, `learner-model`, `spec`) и не нашли ни одной записи: «Go / test» 381 с и `ci-lint` 126 с против 166–211 с и 2–3 с с попаданием (measurements 13) | T109 | (а) `restore-keys` по префиксу без хеша `go.sum`: кэш сборки Go адресован содержимым, кэш модулей проверяется по `go.sum`, а сохранение — под новым точным ключом; (б) ключ без хеша `go.sum`, сохранение при каждом прогоне `main`; (в) как сейчас | (а): переход библиотеки, частый в репозиториях с Go, перестаёт обнулять кэши, а точный ключ по-прежнему пишет свежую запись |
