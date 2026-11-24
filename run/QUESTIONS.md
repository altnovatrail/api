# QUESTIONS.md — questions to the author of `api`

Table 3 of [RUN.md](../RUN.md): the author's open questions that tasks leave (R153 spec, R160 spec).

- Never read whole: take the rows the task and its sections name — `grep -n '^| В1 ' run/QUESTIONS.md`.
- A row is a question a task leaves open (RUN.md rule 9) or one a run of spec's orchestrator waits on (R156 spec). Record the author's answer where it belongs — the task, a document or [DEBT.md](DEBT.md) — and delete the row.
- A row whose question opens with the mark «В реестр spec» is for `spec`: its session finds it on `main` (R81 spec; RULES.md, Across repositories). It leaves once `spec` has recorded it: the task that rests on it names its number.

## Table 3. Questions to the author

Numbers are not reused: next — В89.

| ID | Question | From | Options | Recommendation |
|---|---|---|---|---|
| В87 | The entries of moves to a tag of `spec` in `docs/decisions.md` — R01 and R02 here, «The `spec` tag: v0.6.6», «v0.9.4», «v0.9.11» and «v0.16.3» in the parts that follow — are mostly a list of what a past tag brought, by the decisions of `spec`; under the strict rule of citations and a text in force, what stays of such an entry? | T135 | 1. Its text in force: what `api` decided about how it stands on `spec` — the submodule, the Go modules, the template, the documents check — with its why and what it rejected; the list of what a past tag brought goes, as history git and the archives hold, and so does a rejected option that only concerned that move. 2. The list stays, its numbers replaced by words: the entry keeps a chronicle of `spec` that `spec` holds itself. 3. The entries go whole, as moves are commits with no entry: R01 and R02 also hold decisions in force. | 1: a move is a commit with no entry, and what is in force stays; decided by the executor by the recommendation, before the author's review — R01 and R02 are rewritten so, and the later parts follow them |
