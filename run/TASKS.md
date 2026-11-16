# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T100. Скорость CI: тег `spec` строки 7.14

- **Цель:** `api` — на теге `spec` строки 7.14 COORDINATION.md spec: стадия `toolchain-core` в `.devcontainer/Dockerfile`, apt — только источники снимка (T166 spec), и на релизе `.github`, что проверяет в ней; что проверки доказывают, не меняется.
- **Почему:** строка 7.15 COORDINATION.md spec, её часть, что ждёт тега `spec` строки 7.14 (apt по T166 spec) и релиза `.github` строки 7.13 со стадией `toolchain-core`; переход на тег меняет `.devcontainer/`, поэтому он — последняя из задач строки 7.15 (T98, T99).
- **Зависит от:** тег `spec` строки 7.14 и T99; релиз `.github` с T66 `.github` — стадией `toolchain-core` в релизе и проверках: он выходит после T99, ему нужен переход `.github` на тот же тег (T171 spec). T99 взяла `.github` v0.1.30, без T66.
- **Сделать:**
  1. Переход на тег `spec` — по правилам перехода (RULES.md spec, Across repositories): подмодуль, модули Go `spec` и `spec/contracts`, что тег приносит; новая запись журнала, как R84.
  2. `.devcontainer/` — по заготовке `just -f spec/justfile new-repo-draft api go 3.5 <tag>`, с тремя отличиями `api` (R30): стадия `toolchain-core`, apt `libnss-wrapper` — только из источников снимка; поправка к R30.
  3. Вызовы `.github` — на релиз с T66 `.github`: `release.yml` строит `toolchain-core` без кеша, а lint, vuln, licenses и документы идут в ней (T66 `.github`, его решение журнала); что поменялось — по `git diff v0.1.30 <новый тег> -- .github/workflows` `.github`. Смена того, как строится образ, даёт новый путь файла образа во всех workflow разом (R42 `.github`, «One entry»): шаги «Toolchain image from the cache» job Image и Modules `ci.yml`, `TOOLCHAIN_TAR` и имя образа в `toolchain.sh` — на тот путь, имя и ключ, что `.github` даёт стадии, нужной job (`image.sh` — `release-files`, `modules.sh` — Node и Go); поправки к R27 и R28.
- **Как проверить:** проверки тронутых частей по правилам репозитория; контейнер пересобран, `just ci-lint`, `ci-test`, `ci-vuln`, `ci-licenses`, `ci-modules` в нём; CI pull request зелёный; время изменённых job до и после — в отчёте и в measurements; `just -f spec/justfile docs-check-repo`; `/code-review high`, один круг.
- **Риски:** задача меняет `.devcontainer/`: после слияния контейнер пересобирают («Rebuild Container»), и её не запускают, когда пересобрать некому (T98, «Сделать» 3).
- **Разделы:** строки 7.13, 7.14 и 7.15 COORDINATION.md spec; T166 spec; measurements 3.1 spec; R27, R28, R30.
