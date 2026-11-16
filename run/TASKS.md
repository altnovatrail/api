# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T99. Скорость CI: релиз `.github` строки 7.13

- **Цель:** CI `api` — на релизе `.github` строки 7.13 COORDINATION.md spec: кеш Go в «Go», параллельная сверка файлов релиза; что проверки доказывают, не меняется.
- **Почему:** строка 7.15 COORDINATION.md spec, её часть, что ждёт релиза `.github` строки 7.13; T98 сделала то, что не ждёт (R27, R28). Замеры — measurements 3.1 spec, measurements 9.
- **Зависит от:** релиз `.github` строки 7.13 — оркестратор запустит задачу, когда он выйдет. Если в нём уже стадия `toolchain-core` по тегу `spec` строки 7.14 — эту часть не брать, она — T100.
- **Сделать:**
  1. Все вызовы `.github` в `ci.yml`, `codeql.yml`, `scorecard.yml`, `release.yml` — на SHA релиза, версия рядом; `gh release verify <tag> -R altnovatrail/.github`; что поменялось — по `git diff <прежний тег> <новый> -- .github/workflows` `.github`; поправка к R27.
  2. Файл образа `toolchain`: если релиз меняет путь или ключ записи кеша в `go.yml` и `documents.yml` («один путь и один тег во всех workflow»), шаги «Toolchain image from the cache» jobs Image и Modules `ci.yml` — на тот же путь и ключ, чтобы читать ту же запись (R27, поправка T98); `TOOLCHAIN_TAR` — тот же путь.
  3. Если релиз даёт вызывающему кеш Go и для своих job — тот же том кеша в `image.sh` (`release-files`) и `modules.sh` (`ci-modules`); иначе — не трогать.
  4. По желанию — одна PostgreSQL на прогон `ci-test`, а не на пакет (`internal/store/storetest`, R29): замерить «Go / test» с тёплым `GOCACHE` и решить по замеру; не выигрывает — запись в «Открыто» с цифрами.
- **Как проверить:** проверки тронутых частей по правилам репозитория; CI pull request зелёный; время изменённых job до и после — по прогонам `main` и pull request, в отчёте и в measurements; `just -f spec/justfile docs-check-repo`; `/code-review high`, один круг.
- **Разделы:** строки 7.13 и 7.15 COORDINATION.md spec; measurements 3.1 spec; R76 spec, R156 spec; R27, R28, R29; measurements 9.

#### T100. Скорость CI: тег `spec` строки 7.14

- **Цель:** `api` — на теге `spec` строки 7.14 COORDINATION.md spec: стадия `toolchain-core` в `.devcontainer/Dockerfile`, apt — только источники снимка (T166 spec), и на релизе `.github`, что проверяет в ней; что проверки доказывают, не меняется.
- **Почему:** строка 7.15 COORDINATION.md spec, её часть, что ждёт тега `spec` строки 7.14 (apt по T166 spec) и релиза `.github` строки 7.13 со стадией `toolchain-core`; переход на тег меняет `.devcontainer/`, поэтому он — последняя из задач строки 7.15 (T98, T99).
- **Зависит от:** тег `spec` строки 7.14 и T99; релиз `.github` с `toolchain-core`, если он выйдет отдельно от релиза T99.
- **Сделать:**
  1. Переход на тег `spec` — по правилам перехода (RULES.md spec, Across repositories): подмодуль, модули Go `spec` и `spec/contracts`, что тег приносит; новая запись журнала, как R84.
  2. `.devcontainer/` — по заготовке `just -f spec/justfile new-repo-draft api go 3.5 <tag>`, с тремя отличиями `api` (R30): стадия `toolchain-core`, apt `libnss-wrapper` — только из источников снимка; поправка к R30.
  3. Вызовы `.github` — на релиз со стадией `toolchain-core`, если он есть; job Image и Modules — по той стадии, которая им нужна (`image.sh` — `release-files`, `modules.sh` — Node и Go), шаги кеша — на её запись; поправки к R27 и R28.
- **Как проверить:** проверки тронутых частей по правилам репозитория; контейнер пересобран, `just ci-lint`, `ci-test`, `ci-vuln`, `ci-licenses`, `ci-modules` в нём; CI pull request зелёный; время изменённых job до и после — в отчёте и в measurements; `just -f spec/justfile docs-check-repo`; `/code-review high`, один круг.
- **Риски:** задача меняет `.devcontainer/`: после слияния контейнер пересобирают («Rebuild Container»), и её не запускают, когда пересобрать некому (T98, «Сделать» 3).
- **Разделы:** строки 7.13, 7.14 и 7.15 COORDINATION.md spec; T166 spec; measurements 3.1 spec; R27, R28, R30.
