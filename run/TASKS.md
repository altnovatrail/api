# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T104. Режим чтения почерка: переменная `API_READING_MODE`

- **Цель:** `api` выбирает режим чтения почерка `tutor` переменной окружения `API_READING_MODE`: `pair` (по умолчанию, как сейчас, R68 spec) или `single` — один Haiku 5.5 · medium; в `single` одно наблюдение — итог чтения. Релиз, который `infra` выложит.
- **Почему:** R76 spec, R156 spec; слово автора 2026-11-17: «Пускай будут разные режимы. Режим как сейчас и режим когда работает только Haiku medium — выстави сейчас этот режим»; поправка 2026-11-17 к R68 spec (её пишет задача spec того же прогона; до её тега ссылаться на слово автора). Находка В42 `infra` (T69): пара на сайте дала «Читается двояко» и $0,0087 за «Проверить».
- **Сделать:**
  - поднять `tutor` до v0.1.45 — релиз с `Config.Reading` (T161 tutor, R125 tutor; `compare` v0.1.45 с мержем #187 — identical);
  - `internal/config`: `VarReadingMode = "API_READING_MODE"`, значения `pair`|`single`, пусто — `pair`, чужое — ошибка старта; передать в `tutor.Config`;
  - `internal/learning/attempt.go` `joined()`: в `pair` — как сейчас (ровно два, `observation.Join`); в `single` — ровно одно наблюдение, без `Join`: `module-kit` проверяет его и даёт вердикт по нему (`observation.Verdicts` уже даёт вердикт по одному допустимому наблюдению — прочитано на `main` `module-kit` 2026-11-17); `api` хранит его, как хранит соединённое (R86 spec); иначе `CAUSE_AI_FAILURE`;
  - `api check` проверяет переменную; `single` разрешён на любой ступени (`API_ADMISSION_RUNG`), и `author`, и `full`: слово автора 2026-11-17 «оставить single для абсолютно всех» отменяет отказ при `full` из строки 6.30 — задача spec T180 правит строку и R68/R07 сейчас; на `full` чтение спрашивает допуск, как в `tutor` v0.1.45 (запись пары, читатель — Haiku), и отказ допуска — это исход `NOT_ADMITTED`, а не отказ конфигурации;
  - `internal/tutorstub`: число читателей — по режиму;
  - тесты обоих режимов (конфиг, `joined`, путь попытки со стабом), покрытие изменённых строк ≥ 90%; `docs/architecture.md` и решение в `docs/decisions.md` api; релиз.
- **Как проверить:** проверки репозитория зелёные; релиз вышел.
- **Строка:** 6.30 COORDINATION.md spec (поправка 2026-11-17 к R68, R86 spec; T179 spec) — её текст главнее этого, если они расходятся, кроме отказа при ступени `full`: его автор отменил (см. выше).
- **Разделы:** R68, R80, R86 spec; architecture 3.5 spec; R80 api; `internal/learning/attempt.go`, `internal/config/admission.go`, `internal/tutorstub/tutorstub.go`.
