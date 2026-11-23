# QUESTIONS.md — questions to the author of `api`

Table 3 of [RUN.md](../RUN.md): the author's open questions that tasks leave (R153 spec, R160 spec).

- Never read whole: take the rows the task and its sections name — `grep -n '^| В1 ' run/QUESTIONS.md`.
- A row is a question a task leaves open (RUN.md rule 9) or one a run of spec's orchestrator waits on (R156 spec). Record the author's answer where it belongs — the task, a document or [DEBT.md](DEBT.md) — and delete the row.
- A row whose question opens with the mark «В реестр spec» is for `spec`: its session finds it on `main` (R81 spec; RULES.md, Across repositories). It leaves once `spec` has recorded it: the task that rests on it names its number.

## Table 3. Questions to the author

Numbers are not reused: next — В86.

| ID | Question | From | Options | Recommendation |
|---|---|---|---|---|
| В84 | «В реестр spec»: the check of R32 spec reads the comments of `.proto` files that `api` copies from a release of `learner-model` and may not edit — `just learner-model-proto check` compares them with the release —, and `R135 of spec` in `internal/export/proto/novatrail/learnermodel/v1/common.proto` reads as this log's `R135`, which does not exist. So `"comments": true`, T123, waits on a release of `learner-model` whose `common.proto` its own row 7.29 fixes, and row 7.34 of `COORDINATION.md` spec, «Зависит от» —, does not say so. R160 spec puts the `.proto` files code is generated from outside its rule on history, while R32 spec checks the numbers in their comments; `learner-model` v0.1.16 writes `R135 spec`, and T123 took it with `"comments": true` | T114 | 1. Row 7.34 depends on 7.29 for T123 — on the release of `learner-model` it brings; the check stays as it is, the copy is fixed at its source. 2. `docs-check` skips the comments of files a repository names as copies of a release, a list in `docs-budgets.json`: no wait, but one more key and a blind spot. | 1: the source repository's own check already catches the number, and a copy is fixed by its next release |
| В85 | «В реестр spec»: the copies of `learner-model`'s `.proto` files in `internal/export/proto/novatrail/learnermodel/v1/`, which `api` may not edit, cite their source's decisions as `R10 of github.com/altnovatrail/learner-model` (`common.proto`), `R03, R04, R06, R08 and R45 of …` (`state.proto`) and `R08 of …` (`evidence.proto`); the check of R32 spec reads that form as this log's `R03`…`R45`, which exist by chance, so `"comments": true` passes them unchecked, and a number of `learner-model` past this log's last fails `api`'s `docs-check-repo` on a file only a release fixes | T123 | 1. `learner-model` cites its own numbers in its `.proto` files as `R10 learner-model`, the form of R32 spec that the check reads as another repository's, by a release `api` then takes. 2. `docs-check` reads `R10 of github.com/altnovatrail/<repository>` as another repository's: no change of `learner-model`, one more form in `citations.mjs`. | 1: one form of citation across repositories, and the copy is fixed at its source, as В84 chose |
