# api — measurements

> The results of the measurements of `api`: what was measured, how, on what, and the numbers that came out. The document that describes how it is built, [architecture.md](architecture.md), refers here and holds no numbers of measurements itself, only what follows from them (RULES.md, How we work; R143 spec). A measurement is cited by its section, as any document is: `measurements 2.1`, and from another repository `measurements 2.1 api`.
>
> An external fact a measurement rests on is a record of [sources.md](sources.md). A new measurement is a new section or subsection at the end, and a section's number does not change. Until T62, 2026-11-13, these results stood in architecture 3.3, 3.6, 3.9 and 4, and the run of the quota and the size of the binary on `tutor` v0.1.24 were not in architecture: the run was only in the body of T52 in the archive [docs/plan/E6.md](plan/E6.md), and the size in R54; the claims, and the references to this document, stayed in architecture. The details of the measurements of the first app that architecture did not hold come from the bodies of T42 and T45 in the archive [docs/plan/E3.md](plan/E3.md).

## 1. Size of the binary and the image

### 1.1. The binary with OpenFGA, 2026-11-08

For R31, where OpenFGA is embedded as a library (sources 2.4). The `api` binary with OpenFGA, `CGO_ENABLED=0 -trimpath -s -w`, is 33,816,736 bytes against 11,915,424 for v0.1.1: measured 2026-11-08.

### 1.2. The binary with the Admin SDK port, 2026-11-09

For R32 (sources 2.5). A trial in a copy of the module outside the repository: `cmd/api`, which imports the port of the sign-in service on the Admin SDK for Go. The `linux/amd64` binary with `-trimpath` and `-s -w` is 51,765,408 bytes against 33,804,448 without the port.

### 1.3. The image, 2026-11-09 and 2026-11-11

For R07, R52 and R53 (architecture 3.3). The size of the image is printed by the job "Image" of CI:
- without packages it was the binary — 56.0 MB — on the runtime: 58,359,952 bytes, 58.4 MB, for the release image and the image from source, the run of pull request #45 on 2026-11-09;
- with the move to `spec` v0.15.0 the binary grew (R52), and the image of `main` without packages is 115,392,656 bytes;
- with the packages of `modules` v0.2.5 both images are 130,402,471 bytes, 130.4 MB, pull request #58 on 2026-11-11: the packages add 15.0 MB (R53).

### 1.4. The binary on `tutor` v0.1.24, T45, 2026-11-12

For R54. The release binary for `linux/amd64` — from 113,029,280 down to 79,339,680 bytes, by the flags of `release-files`, 2026-11-12: `api` itself provides the Anthropic adapter, and `openai-go/v3` and `coder/websocket` left the binary's modules, and `openai` is not in it. This measurement was not in architecture: it is in R54 and in the body of T45 of the archive E3; T62 moved it here so that the history of the size is in one place.

## 2. The first app

The author walked the first app's path in a browser, locally: `app` and `api` on one machine, preflight local, and the token check, the database and the AI provider over the network, as in a deployment (architecture 3.9). The time is the server's time by the log of `serve`: the verdict is `request.duration_ms` of `SubmitWork`, the reading is `ai_usage.duration_ms` of the reading call, one for both models of the pair. The database is Neon `aws-us-east-1` from the author's machine, so the overhead is a property of the local run.

### 2.1. The verdict, the engine choice and the price of a check, T42, 2026-11-12

For the 2026-11-12 amendment to R19 and the estimates of architecture 3.6 and 3.9. The first build with `app`, `tutor` v0.1.21: 112 requests, 10 checks by handwriting — all reached Anthropic, and both models of the pair answered —, the words of a rung by fallback text, 3 engine choices (T42):

| What | p50 | Spread |
|---|---|---|
| the verdict — `SubmitWork` as a whole | 9.3 s | 6.3–17.9 s |
| the reading by the pair Opus 5.5 · `high` and Haiku 5.5 · `medium` | 6.7 s | 3.3–15.2 s |
| the rest in `SubmitWork` — the token, the database and the checks | 2.7 s | 1.5–9.7 s, the first submission the longest |
| the second request without a call — `GetRungWords` with fallback text | 1.2 s | 1.2 s |
| the engine choice — `ChooseNext`, an upper bound on reading its input | 2.5 s | 1.7–4.3 s |
| the end of a lesson with a choice — `CompleteLesson` | 2.0 s | 1.8–2.2 s |

- Haiku 5.5 · `medium` thinks 286–1047 tokens, p50 681, and Opus 5.5 · `high` 0–462, p50 201. The time of `ai_usage` is one for the pair, so the log does not say which model holds up the reading.
- The estimate of architecture 3.9 — the verdict after the reading, ≈ 3–4 s — and the measurement is three times larger. Why the reading is longer than the judge of `tutor` is the question В53; where the answer went is named by R62.
- The price: 10 checks — Opus 14,631 input tokens and 3,688 output, Haiku 14,631 and 8,256. By the prices checked 2026-11-11 this is $0.138, ≈ $0.014 per check against the estimate of $0.0086 (architecture 3.6), and the most of it is Opus's output.

### 2.2. The tutor's words, T45, 2026-11-12

For R54 and the 2026-11-12 amendment to R19. The second run with `app`, on `tutor` v0.1.24: a new account, 8 checks by handwriting, 3 words of a rung, 3 "explain differently", "Hint" and 2 "correct answer words" — they are, as expected, "not admitted" (T45):

| What | p50 | Spread |
|---|---|---|
| the verdict — `SubmitWork` as a whole, 8 checks | 5.3 s | 4.2–6.1 s |
| the reading by the pair | 3.0 s | 2.7–4.0 s |
| the words of a rung — `GetRungWords`, 3 calls | 10.6 s | 9.8–12.7 s |
| of them, the call of Opus 5.5 · `high` | 7.9 s | 7.4–11.1 s |
| "explain differently" — `ExplainDifferently`, 3 calls | 8.7 s | 7.5–8.9 s |

- The verdict came faster than in the first run: Haiku thought less. The words take twice the estimate of ≈ 5 s (architecture 3.9). Why the words are longer than the measurement of `tutor` is the question В54; where the answer went is named by R62.
- The source of the words, by `attempt_rungs` in the dev database: all 3 words of a rung from the model, "explain differently" 2 from the model and 1 by fallback text: the leak check or the form check failed.
- The price — the words $0.132 for 6 calls, the readings $0.078 for 8, in all ≈ $0.21.

## 3. Platform AI quota with `app`, T52, 2026-11-12

For R55–R59 and architecture 3.6. A run on the dev project: the author's account on the tariff `probe` — the caps of the profile and the account $0.08 and $0.10, a typical task $0.06 —, running out, the outcome "quota" and a change of tariff by a command. The first day — the author in a browser, the second — the executor with the same requests that `app` sends, through `curl`, with the token of the author's user, and the handwriting a synthetic PNG with digits (T52). Anthropic's prices were checked 2026-11-12 — Opus 5.5 $4 and $20 per MTok of input and output, Haiku 5.5 $0.10 and $0.50 —, the same as in sources 7.1.

- **The first day** — by the offset −6 h that `app` sent with its first request: 4 checks — one "unreadable", "ambiguous" —, 2 words from the model; then the words of a rung — fallback text, and "explain differently" — «Время наставника на сегодня кончилось. Оно вернётся в 00:00».
- **The second day:** after midnight the checks went again. On `probe` a new task by handwriting does not start when the profile has more than $0.02 spent and reserved: "again" started at $0.0145, and at $0.0217 — the refusal `resource_exhausted` `CAUSE_QUOTA` with the next day, 2026-11-13 06:00 UTC, and the same for a lesson's segment. The attempt already started — 8 checks, then "unchecked": the slot `unchecked`, the outcome `CAUSE_QUOTA`, no wrong answers added. After `just dev tariff -tariff closed_slice` a new task by handwriting started on the same day.
- **The price of the run** — $0.1518: 15 checks — $0.1203, a word of a rung — $0.0185, "explain differently" — $0.0130; a check by the pair — $0.0072–0.0135.
- **Time:** the reading — p50 2.4 s, 1.8–8.9 s, `SubmitWork` as a whole — p50 4.6 s.
- **Counters:** in the export — $0.068326 for the profile and $0.083508 for the account, nothing reserved; the counters agree with the sum of `cost_micro_usd` of the log to the microdollar.
- **The log of `serve`** — 98 lines of JSON, without UID, email, keys, the token and connection strings; `quota_exhausted` — 4 lines with the cap `profile`, `platform_limit` — none.

## 4. Keeping the savings

For R145 spec and R63: the budgets of size that `docs-check-repo` measures by [docs-budgets.json](../docs-budgets.json), and the tokens of the sessions, a row at each epic's archive (RUN.md, «Как устроен план: эпики»). The first row, T65, measures the sessions T61–T65, which moved `api` to the line 0.16 of `spec`, rebuilt its documents and translated them (R62).

### 4.1. Budgets, T65, 2026-11-13

Measured 2026-11-13, after T64, by `docs-check-repo` itself, with every limit of a draft of `docs-budgets.json` set to 1 KB: the check then printed the size of each thing it measures. The largest of each kind, in KB of 1,024 bytes, and the limit of the block `repository` of `spec/tools/docs-check/budgets.json`:

| What | The largest | Limit |
|---|---|---|
| CLAUDE.md / RUN.md / `DEBT.md` | 8.9 / 34.1 / 16.7 | 12 / 96 / 128 |
| a rule of `.claude/rules/` | 2.1, `ci` | 6 |
| an entry of the decision log, with its amendments | 19.4, R07 | 40 |
| the own text under a heading, outside the six sections below | 23.5, architecture 3.10; in the other documents 10.0, sources 7.1 | 24 |
| a row of a table of RUN.md / `DEBT.md` | 1.4 / 2.1, Д26 | 4 / 4 |

Six sections of architecture stayed over 24 KB after T61–T64. Each has a limit of its own in `docs-budgets.json`: its size plus 10–20%, rounded to a whole KB, as R145 spec sets.

| Section | Size, KB | Limit, KB |
|---|---|---|
| architecture 1 | 24.9 | 28 |
| architecture 3.1 | 28.6 | 32 |
| architecture 3.2 | 30.0 | 34 |
| architecture 3.3 | 26.6 | 30 |
| architecture 3.6 | 28.0 | 32 |
| architecture 3.9 | 54.6 | 62 |

- architecture 3.10 is 0.5 KB under its limit: the next edit that adds to it compresses it, or raises its limit by an edit of `docs-budgets.json`, seen in review (R145 spec).
- architecture 3.9 is more than twice the limit of a section — about 22 thousand tokens at 2.5 bytes a token (measurements 1.6 spec) — Д36.
- With the edits of T65, on 2026-11-13, the check measures 181 things.

### 4.2. The sessions' tokens

"Before" is the row `-workspaces-api` of measurements 1.9 spec: 2026-10-13T00:00 … 2026-11-12T14:27, 23 sessions, 6,109 / 4,741 calls of the main session and of subagents, a call's context 371.4 / 104.4 thousand tokens, 17 compactions, $954.57. "After" is `just -f spec/justfile tokens-report --since 2026-11-12T21:19 --row`, from the start of the session of T61: from 00:00 of 2026-11-12, as the text of T65 named it, the window would also take the sessions of T42–T58. The session of T65 is counted to the moment of the measurement.

| Project | Window, UTC | Sessions | Calls, main / subagents | Context of a call, k, main / subagents | Compactions | Cost at API prices |
|---|---|---|---|---|---|---|
| `-workspaces-api` — T61–T65 | 2026-11-12T21:19 … 2026-11-13T06:04 | 5 | 546 / 639 | 173.9 / 86.5 | 0 | $62.95 |
| `-workspaces-api` — E8, T58–T65 | 2026-11-12T19:56 … 2026-11-13T15:53 | 9 | 943 / 717 | 172.3 / 87.1 | 0 | $90.42 |
| `-workspaces-api` — E9, T66–T72 | 2026-11-13T15:52 … 2026-11-13T22:04 | 9 | 741 / 175 | 166.8 / 67.8 | 1 | $54.75 |
| `-workspaces-api` — E10, T73–T75 | 2026-11-13T22:02 … 2026-11-14T05:34 | 6 | 840 / 467 | 186.5 / 103.5 | 3 | $77.83 |

- The context of a main call fell from 371.4 to 173.9 thousand, −53%, and of a subagent's from 104.4 to 86.5, −17%. The windows differ in work: "before" is mostly the code of E2–E7, "after" the move to the tag and documents; most subagent calls are the blind reading of the translations of T63 and T64.
- By session, main calls and a main call's context: T61 — 183, 241.9 thousand; T62 — 125, 196.2; T63 — 91, 112.2; T64 — 96, 116.6; T65 — 51, 93.0.
- The reference of `api` for the threshold of R145 spec is its measurement after of T124 spec, not this row.
- E8, at its archive on 2026-11-13: `tokens-report --since 2026-11-12T19:56 --row`, from the start of the session that archived E7, set up E8 and did T58; the window holds the sessions of T61–T65 above and the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E7's archive and T58 — 146, 209.1 thousand; T59 — 175, 176.2; T60 — 53, 73.9. No task of analysis by R145 spec: `api` has no reference yet, a main call's context is below the row of T61–T65, and `docs-budgets.json` has been written once, by T65, and raised never.
- E9, at its archive on 2026-11-13: `tokens-report --since 2026-11-13T15:52 --row`, from the start of the session that archived E8, set up E9 and did T60's last edit; the window holds the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E8's archive — 50, 73.7 thousand; T66 — 58, 92.5; T67 — 32, 83.1; T68 — 126, 210.3; T69 — 76, 100.8; T70 — 98, 154.7; T71 — 178, 189.8; T72 — 107, 161.9; E9's archive — 18, 50.8, to the measurement. The one compaction is T71's. No task of analysis by R145 spec: `api` still has no reference — measurements 1.9 spec at v0.16.5 holds only its row before — a main call's context is below the row of E8, and `docs-budgets.json` has had no limit raised: T67 only took the limit of architecture 3.9 out.
- E10, at its archive on 2026-11-14: `tokens-report --since 2026-11-13T22:02 --row`, from the start of the session that archived E9 — whole now, to the measurement in the row of E9 —, the session that set up E10 and the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E9's archive — 41, 67.8 thousand; E10's setup — 45, 107.0; T73 — 144, 169.7; T74 — 260, 157.5; T75 — 326, 173.6; E10's archive — 24, 72.1, to the measurement. The compactions are one of T74 and two of T75. No task of analysis by R145 spec: the reference of `api` is now its row of T124 in measurements 1.9 spec at v0.17.1, 162.6 thousand, and a main call's context here is 14.7% above it, under 25%, over 840 main calls; `docs-budgets.json` has had no limit raised since T65.

## 5. Fuzzing, T59, 2026-11-13

For the 2026-11-13 amendment to R27. `just fuzz 5m` in the devcontainer, 20 workers of `go test -fuzz` on this machine (`nproc` 20), Go 1.27.4, each test for five minutes from its seeds; the executions are those of the last line of each run. No test failed, and no input was written to `testdata/fuzz/`.

| Test | Package | Executions in 5 minutes |
|---|---|---|
| `FuzzRead` | `internal/invite` | 35,175,054 |
| `FuzzCursorOf` | `internal/learning` | 2,411,589 |
| `FuzzFromName` | `internal/packages` | 10,599,276 |
| `FuzzParseConfig` | `internal/packages` | 5,665,657 |
| `FuzzUnpack` | `internal/packages` | 31,940 before the refusal of sparse files, 302,342 after |
| `FuzzRoutes` | `internal/server` | 11,324,242 |

`FuzzUnpack` stood for tens of seconds at 0 executions a second. One cause was an entry of PAX sparse, which the reader of `archive/tar` unfolds into zeros up to the limit of 512 MB (sources 4.5); once `unpack` refused it, the run made ten times as many executions. Its remaining stops are the minimization of new inputs, up to 60 s each by default: a minute with `-fuzzminimizetime 2s` made 79,353 executions against 39,520 without it.

The refusal does not bound the memory of the build; the limit of R41 does: 500,000,000 zero bytes gzip to 485,269 bytes (`head -c 500000000 /dev/zero | gzip -c | wc -c`, the default level), and a regular file of them unpacks as far as the limit allows.

After the review the two tests changed and were fuzzed again, `just fuzz 5m <test>` with `-fuzzminimizetime 2s`, without a failure: `FuzzRoutes`, now on `rpc.Mount` of every service, 1,244,726 executions; `FuzzCursorOf`, now bounded at the year 9999 too, 744,648.

## 6. The check of an own key, T78, 2026-11-14

For R73. `just dev-ai` — `serve` on the models, at the stage `author`, on the dev projects of Firebase and Neon (R39, R66) —, from the devcontainer, 2026-11-14 at about 19:15 UTC: `CheckOwnKey` of the owner's profile of the dev account, three requests one after another. The times are `duration_ms` of the request's line of the log, and the call's own beside it, by `curl` from the script:

| Key | Answer | `duration_ms` | The call, ms | Requests to Anthropic |
|---|---|---|---|---|
| the author's dev key of Anthropic, as an own key | `OWN_KEY_CHECK_ACCEPTED` | 713 | 721 | 2 `count_tokens` |
| a synthetic key of Anthropic's form | `OWN_KEY_CHECK_INVALID` | 472 | 480 | 2 `count_tokens`, each 401 |
| a synthetic key of OpenAI | `OWN_KEY_CHECK_PROVIDER_CLOSED` | 323 | 353 | none |

The two requests are those of the two models `tutor` v0.1.33 would call with the key — Opus 5.5 and Haiku 5.5 —, one after the other: a gate with a counting adapter dials two models for one check. The log held neither key, and its lines `request` named the end alone, `key_check`. Token counting is free (sources 10.7): four requests, $0.
