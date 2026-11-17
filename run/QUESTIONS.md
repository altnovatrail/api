# QUESTIONS.md — questions to the author of `api`

Table 3 of [RUN.md](../RUN.md): the author's open questions from tasks' «Открыто» (R153 spec, R154 spec; R85).

- Never read whole: take the rows the task and its sections name — `grep -n '^| В1 ' run/QUESTIONS.md`.
- A row comes from a task's «Открыто» (RUN.md rule 9) or from a run of spec's orchestrator that waits on the author (R156 spec). Record the author's answer where it belongs — the task, a document or [DEBT.md](DEBT.md) — and delete the row.
- A row whose question opens with the mark «В реестр spec» is for `spec`: its session finds it on `main` (R81 spec; RULES.md, Across repositories). It leaves once `spec` has recorded it: the task that rests on it names its number.

## Table 3. Questions to the author

Numbers are not reused: next — В82.

| ID | Question | From | Options | Recommendation |
|---|---|---|---|---|
| В81 | В реестр spec: с T104 `api` читает почерк листа и страницы тетради в режиме `API_READING_MODE`: `pair` по умолчанию — два читателя R68 spec, `module-kit` соединяет их наблюдения, — или `single`, один Claude Haiku 5.5 · `medium`, его одно наблюдение `module-kit` проверяет и даёт по нему вердикт без соединения, и `api` хранит его (R88; слово автора 2026-11-17, `tutor` v0.1.45, R125 tutor). Карточка architecture 3.5 spec говорит «with two readers for handwriting → `module-kit` joins their observations into one», а architecture 3.6 spec — о наблюдениях двух читателей; поправку 2026-11-17 к R68 spec и строку 6.30 пишет задача `spec` того же прогона (T179, T180 spec) | T104 | (а) architecture 3.5 spec и architecture 3.6 spec называют оба режима: «with two readers for handwriting, or one in the mode `single`»; (б) карточка не меняется, режим — только в R68 spec и в architecture `api` | (а): деталь `api` меняет то, что говорит карточка, а по RULES.md spec карточка меняется тем же путём; решение автора уже есть — слово 2026-11-17 |
