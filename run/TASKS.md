# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T148. `docs/sources.md` по правилу записей: темы 6–9

- **Цель:** записи тем 6–9 `docs/sources.md` — показ решения и подсказки, квота ИИ платформы, урок, задача с рубрикой — по правилу записей `spec/RULES.md`, как T147 сделала с темами 2–5.
- **Почему:** вторая часть T147 (правило 11): строка 7.69 COORDINATION.md `spec` ждёт T147–T149.
- **Сделать:** записи 6.1, 6.2, 8.1, 9.1 — файлы MathTrail, репозиториев NovaTrail и теги `spec` — удалить, их утверждения в архитектуре и журнале — ссылка на репозиторий@коммит, как в записи; 7.1 — оставить цены и справку Anthropic, пункты `tutor`, canvas-api, mathtrail-standalone и `app` — ссылками на месте, пересказ сверок 2026-11-11 и 2026-11-12 — свернуть; таблица указателя раздела 4 архитектуры — так же. Номера не переиспользовать, архивы `docs/plan/` не трогать, смысл утверждений не менять.
- **Как проверить:** `just -f spec/justfile docs-check-repo` — зелёный; размер `docs/sources.md` до и после — в отчёт.
- **Разделы:** `spec/RULES.md`, «The bar» и «Citations»; R171 `spec`; T147.

#### T149. `docs/sources.md` по правилу записей: темы 10–13

- **Цель:** записи тем 10–13 `docs/sources.md` — протокол, удаление и выгрузка, canvas-api, проверки кода — по правилу записей `spec/RULES.md`, как T147 сделала с темами 2–5.
- **Почему:** третья часть T147 (правило 11): строка 7.69 COORDINATION.md `spec` ждёт T147–T149.
- **Сделать:** записи 10.3, 10.4, 10.5, 10.6, 11.4, 11.5, 11.7, 12.1 — файлы `tutor`, MathTrail, `app`, модуль контрактов `spec`, код на версии — удалить, утверждения — ссылка на месте с версией или коммитом; 10.1, 10.2, 11.1 — оставить страницы без версии и стандарты, код на версии и коммите — ссылками у утверждений; 10.7 — без ссылки на запись `tutor` и вывода из кода `tutor`; тема 12 и пункт о ней во вводной `docs/sources.md` — убрать, если записей не останется; таблица указателя раздела 4 архитектуры — так же. Номера не переиспользовать, архивы `docs/plan/` не трогать, смысл утверждений не менять.
- **Как проверить:** `just -f spec/justfile docs-check-repo` и проверки частей, чьи файлы задеты (комментарии кода — правило `go`), — зелёные; размер `docs/sources.md` до и после — в отчёт.
- **Разделы:** `spec/RULES.md`, «The bar» и «Citations»; R171 `spec`; T147.
