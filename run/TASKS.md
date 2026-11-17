# TASKS.md — tasks outside an epic of `api`

Open tasks outside the current epic, each as its body, in Russian (R154 spec, R155 spec; R85). Each is also a row of table 1 of [RUN.md](../RUN.md) (rule 9; R63).

- Never read whole: a session reads only its own task — `grep -n '^#### T' run/TASKS.md` lists them, `awk '/^#### T99\./{p=1;print;next} /^#### /{p=0} p' run/TASKS.md` gives one body.
- A new task outside an epic goes here as a body, its number the one free when it is written down (RUN.md, Citations). When done, the body leaves for the archive (RUN.md rule 9).

## Tasks outside an epic

#### T105. Поправка уровня взрослым

- **Цель:** взрослый аккаунта ставит уровень модуля на полосу проще или сложнее, модель учитывает это сразу, ученик видит, что уровень поставил взрослый.
- **Почему:** R157 spec и её поправка T173; строка 6.28 COORDINATION.md spec; architecture 3.5 spec; privacy 4 и 7 spec.
- **Сделать:** переход на тег `spec` с T178 и на релиз `learner-model` строки 4.20; метод `ProgressService` строки 1.22 — запрос владельца аккаунта для любого профиля аккаунта, и ученика с собственным согласием тоже; полоса — соседняя с текущей, повтор ничего не меняет; факт — в журнал свидетельств по `Append`; прогресс отдаёт, что уровень поставил взрослый, и когда; выгрузка и удаление — с журналом (R135 spec); тесты; релиз.
  - Дополнение T178 spec (контракт строки 1.22): метод — `SetModuleLevel`, отказы и повтор — по комментарию метода и [тексту контрактов](../spec/contracts/README.md), раздел 15; полоса уровня и дата поправки — в `GetSkillMap`, последние поправки — в `GetProgress`.
  - Тег `spec` — v0.18.9 (последний; в нём T178, T179, T180), релиз `learner-model` строки 4.20 — v0.1.13.
- **Как проверить:** проверки `api` по его RUN.md; поправка двигает уровень на полосу, ответ прогресса несёт её дату; выгрузка профиля несёт факт.
- **Разделы:** строки 1.22, 4.20 и 6.28 COORDINATION.md spec; R157 spec, privacy 4 spec; R08.
