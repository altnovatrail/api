# QUESTIONS.md — questions to the author of `api`

Table 3 of [RUN.md](../RUN.md): the author's open questions that tasks leave (R153 spec, R154 spec, R160 spec; R85).

- Never read whole: take the rows the task and its sections name — `grep -n '^| В1 ' run/QUESTIONS.md`.
- A row is a question a task leaves open (RUN.md rule 9) or one a run of spec's orchestrator waits on (R156 spec). Record the author's answer where it belongs — the task, a document or [DEBT.md](DEBT.md) — and delete the row.
- A row whose question opens with the mark «В реестр spec» is for `spec`: its session finds it on `main` (R81 spec; RULES.md, Across repositories). It leaves once `spec` has recorded it: the task that rests on it names its number.

## Table 3. Questions to the author

Numbers are not reused: next — В85.

| ID | Question | From | Options | Recommendation |
|---|---|---|---|---|
| В84 | «В реестр spec»: the check of R32 spec reads the comments of `.proto` files that `api` copies from a release of `learner-model` and may not edit — `just learner-model-proto check` compares them with the release —, and `R135 of spec` in `internal/export/proto/novatrail/learnermodel/v1/common.proto` reads as this log's `R135`, which does not exist. So `"comments": true`, T123, waits on a release of `learner-model` whose `common.proto` its own row 7.29 fixes, and row 7.34 of `COORDINATION.md` spec, «Зависит от» —, does not say so | T114 | 1. Row 7.34 depends on 7.29 for T123 — on the release of `learner-model` it brings; the check stays as it is, the copy is fixed at its source. 2. `docs-check` skips the comments of files a repository names as copies of a release, a list in `docs-budgets.json`: no wait, but one more key and a blind spot. | 1: the source repository's own check already catches the number, and a copy is fixed by its next release |
