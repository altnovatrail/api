# api — measurements

> The results of the measurements of `api`: what was measured, how, on what, and the numbers that came out. The document that describes how it is built, [architecture.md](architecture.md), refers here and holds no numbers of measurements itself, only what follows from them. A measurement is cited by its section, as any document is: `measurements 2.1`, and from another repository `measurements 2.1 api`.
>
> A fact no run checks that a measurement rests on is a record of [sources.md](sources.md); a library's documentation or code at a version or commit is a link beside it. A new measurement is a new section or subsection at the end, and a section's number does not change. A section's heading names what was measured and the date, not a task.

## 1. Size of the binary and the image

### 1.1. The binary with OpenFGA, 2026-11-08

For OpenFGA embedded as a library ([`server.go`](https://github.com/openfga/openfga/blob/v1.22.1/pkg/server/server.go) of OpenFGA v1.22.1). The `api` binary with OpenFGA, `CGO_ENABLED=0 -trimpath -s -w`, is 33,816,736 bytes against 11,915,424 for v0.1.1: measured 2026-11-08.

### 1.2. The binary with the Admin SDK port, 2026-11-09

For the port of the sign-in service ([firebase-admin-go v4.22.0](https://github.com/firebase/firebase-admin-go/tree/v4.22.0)). A trial in a copy of the module outside the repository: `cmd/api`, which imports the port of the sign-in service on the Admin SDK for Go. The `linux/amd64` binary with `-trimpath` and `-s -w` is 51,765,408 bytes against 33,804,448 without the port. The port pulls in 32 new modules — `cloud.google.com/go` with `firestore`, `storage`, `monitoring`, `auth` and `iam`, `google.golang.org/api`, `golang.org/x/oauth2`, otel, s2a-go, go-jose and others; each one's LICENSE is Apache-2.0, MIT or BSD-3, and none has a NOTICE. `go-licenses check` by the allowed list passed with no refusals, and govulncheck v1.9.0 said: "Your code is affected by 0 vulnerabilities".

### 1.3. The image, 2026-11-09 and 2026-11-11

For the module packages in the image. The size of the image is printed by the job "Image" of CI:
- without packages it was the binary — 56.0 MB — on the runtime: 58,359,952 bytes, 58.4 MB, for the release image and the image from source, the run of pull request #45 on 2026-11-09;
- with the move to `spec` v0.15.0 the binary grew, and the image of `main` without packages is 115,392,656 bytes;
- with the packages of `modules` v0.2.5 both images are 130,402,471 bytes, 130.4 MB, pull request #58 on 2026-11-11: the packages add 15.0 MB.

### 1.4. The binary on `tutor` v0.1.24, 2026-11-12

For the move to `tutor` v0.1.24. The release binary for `linux/amd64` — from 113,029,280 down to 79,339,680 bytes, by the flags of `release-files`, 2026-11-12: `api` itself provides the Anthropic adapter, and `openai-go/v3` and `coder/websocket` left the binary's modules, and `openai` is not in it.

## 2. The first app

The author walked the first app's path in a browser, locally: `app` and `api` on one machine, preflight local, and the token check, the database and the AI provider over the network, as in a deployment. The time is the server's time by the log of `serve`: the verdict is `request.duration_ms` of `SubmitWork`, the reading is `ai_usage.duration_ms` of the reading call, one for both models of the pair. The database is Neon `aws-us-east-1` from the author's machine, so the overhead is a property of the local run.

### 2.1. The verdict, the engine choice and the price of a check, 2026-11-12

For the check of handwriting in two requests and the estimates of the quota and of the protocol. The first build with `app`, `tutor` v0.1.21: 112 requests, 10 checks by handwriting — all reached Anthropic, and both models of the pair answered —, the words of a rung by fallback text, 3 engine choices:

| What | p50 | Spread |
|---|---|---|
| the verdict — `SubmitWork` as a whole | 9.3 s | 6.3–17.9 s |
| the reading by the pair Opus 5.5 · `high` and Haiku 5.5 · `medium` | 6.7 s | 3.3–15.2 s |
| the rest in `SubmitWork` — the token, the database and the checks | 2.7 s | 1.5–9.7 s, the first submission the longest |
| the second request without a call — `GetRungWords` with fallback text | 1.2 s | 1.2 s |
| the engine choice — `ChooseNext`, an upper bound on reading its input | 2.5 s | 1.7–4.3 s |
| the end of a lesson with a choice — `CompleteLesson` | 2.0 s | 1.8–2.2 s |

- Haiku 5.5 · `medium` thinks 286–1047 tokens, p50 681, and Opus 5.5 · `high` 0–462, p50 201. The time of `ai_usage` is one for the pair, so the log does not say which model holds up the reading.
- The estimate of the protocol — the verdict after the reading, ≈ 3–4 s — and the measurement is three times larger. Why the reading is longer than the judge of `tutor` was the question В53, answered in the decision log with the move to `spec` v0.16.3.
- The price: 10 checks — Opus 14,631 input tokens and 3,688 output, Haiku 14,631 and 8,256. By the prices checked 2026-11-11 this is $0.138, ≈ $0.014 per check against the estimate of the quota, $0.0086, and the most of it is Opus's output.

### 2.2. The tutor's words, 2026-11-12

For the move to `tutor` v0.1.24 and the check of handwriting in two requests. The second run with `app`, on `tutor` v0.1.24: a new account, 8 checks by handwriting, 3 words of a rung, 3 "explain differently", "Hint" and 2 "correct answer words" — they are, as expected, "not admitted":

| What | p50 | Spread |
|---|---|---|
| the verdict — `SubmitWork` as a whole, 8 checks | 5.3 s | 4.2–6.1 s |
| the reading by the pair | 3.0 s | 2.7–4.0 s |
| the words of a rung — `GetRungWords`, 3 calls | 10.6 s | 9.8–12.7 s |
| of them, the call of Opus 5.5 · `high` | 7.9 s | 7.4–11.1 s |
| "explain differently" — `ExplainDifferently`, 3 calls | 8.7 s | 7.5–8.9 s |

- The verdict came faster than in the first run: Haiku thought less. The words take twice the protocol's estimate of ≈ 5 s. Why the words are longer than the measurement of `tutor` was the question В54, answered in the decision log with the move to `spec` v0.16.3.
- The source of the words, by `attempt_rungs` in the dev database: all 3 words of a rung from the model, "explain differently" 2 from the model and 1 by fallback text: the leak check or the form check failed.
- The price — the words $0.132 for 6 calls, the readings $0.078 for 8, in all ≈ $0.21.

## 3. Platform AI quota with `app`, 2026-11-12

For the quota in code. A run on the dev project: the author's account on the tariff `probe` — the caps of the profile and the account $0.08 and $0.10, a typical task $0.06 —, running out, the outcome "quota" and a change of tariff by a command. The first day — the author in a browser, the second — the executor with the same requests that `app` sends, through `curl`, with the token of the author's user, and the handwriting a synthetic PNG with digits. Anthropic's prices were checked 2026-11-12 — Opus 5.5 $4 and $20 per MTok of input and output, Haiku 5.5 $0.10 and $0.50 —, the same as in sources 7.1.

- **The first day** — by the offset −6 h that `app` sent with its first request: 4 checks — one "unreadable", "ambiguous" —, 2 words from the model; then the words of a rung — fallback text, and "explain differently" — «Время наставника на сегодня кончилось. Оно вернётся в 00:00».
- **The second day:** after midnight the checks went again. On `probe` a new task by handwriting does not start when the profile has more than $0.02 spent and reserved: "again" started at $0.0145, and at $0.0217 — the refusal `resource_exhausted` `CAUSE_QUOTA` with the next day, 2026-11-13 06:00 UTC, and the same for a lesson's segment. The attempt already started — 8 checks, then "unchecked": the slot `unchecked`, the outcome `CAUSE_QUOTA`, no wrong answers added. After `just dev tariff -tariff closed_slice` a new task by handwriting started on the same day.
- **The price of the run** — $0.1518: 15 checks — $0.1203, a word of a rung — $0.0185, "explain differently" — $0.0130; a check by the pair — $0.0072–0.0135.
- **Time:** the reading — p50 2.4 s, 1.8–8.9 s, `SubmitWork` as a whole — p50 4.6 s.
- **Counters:** in the export — $0.068326 for the profile and $0.083508 for the account, nothing reserved; the counters agree with the sum of `cost_micro_usd` of the log to the microdollar.
- **The log of `serve`** — 98 lines of JSON, without UID, email, keys, the token and connection strings; `quota_exhausted` — 4 lines with the cap `profile`, `platform_limit` — none.

## 4. Keeping the savings

The budgets of size that `docs-check-repo` measures by [docs-budgets.json](../docs-budgets.json), and the tokens of the sessions, a row at each epic's archive. The first row measures the five sessions that moved `api` to the line 0.16 of `spec`, rebuilt its documents and translated them.

### 4.1. Budgets, 2026-11-13

Measured 2026-11-13, after the translation of the documents, by `docs-check-repo` itself, with every limit of a draft of `docs-budgets.json` set to 1 KB: the check then printed the size of each thing it measures. The largest of each kind, in KB of 1,024 bytes, and the limit of the block `repository` of `spec/tools/docs-check/budgets.json`:

| What | The largest | Limit |
|---|---|---|
| CLAUDE.md / RUN.md / `DEBT.md` | 8.9 / 34.1 / 16.7 | 12 / 96 / 128 |
| a rule of `.claude/rules/` | 2.1, `ci` | 6 |
| an entry of the decision log, with its amendments | 19.4, the entry on the version a new attempt gets | 40 |
| the own text under a heading, outside the six sections below | 23.5, the section of architecture on deletion and export; in the other documents 10.0, sources 7.1 | 24 |
| a row of a table of RUN.md / `DEBT.md` | 1.4 / 2.1, Д26 | 4 / 4 |

Six sections of architecture stayed over 24 KB after the move to the line 0.16 and the translation. Each has a limit of its own in `docs-budgets.json`: its size plus 10–20%, rounded to a whole KB, as the rule of budgets of `spec` sets.

| Section of architecture | Size, KB | Limit, KB |
|---|---|---|
| Purpose and bounds | 24.9 | 28 |
| Sign-in and permissions | 28.6 | 32 |
| Database and learner model | 30.0 | 34 |
| Module versions and packages in the image | 26.6 | 30 |
| Platform AI quota | 28.0 | 32 |
| Protocol between `app` and `api` | 54.6 | 62 |

- The section on deletion and export is 0.5 KB under its limit: the next edit that adds to it compresses it, or raises its limit by an edit of `docs-budgets.json`, seen in review.
- The section on the protocol is more than twice the limit of a section — about 22 thousand tokens at 2.5 bytes a token (measurements 1.6 spec) — Д36.
- With the edits that wrote the budgets, on 2026-11-13, the check measures 181 things.

### 4.2. The sessions' tokens

"Before" is the row `-workspaces-api` of measurements 1.9 spec: 2026-10-13T00:00 … 2026-11-12T14:27, 23 sessions, 6,109 / 4,741 calls of the main session and of subagents, a call's context 371.4 / 104.4 thousand tokens, 17 compactions, $954.57. "After" is `just -f spec/justfile tokens-report --since 2026-11-12T21:19 --row`, from the start of the first session of the move to the line 0.16: from 00:00 of 2026-11-12, the window would also take the sessions of the epics before it. The last session is counted to the moment of the measurement.

| Project | Window, UTC | Sessions | Calls, main / subagents | Context of a call, k, main / subagents | Compactions | Cost at API prices |
|---|---|---|---|---|---|---|
| `-workspaces-api` — the move to the line 0.16 | 2026-11-12T21:19 … 2026-11-13T06:04 | 5 | 546 / 639 | 173.9 / 86.5 | 0 | $62.95 |
| `-workspaces-api` — E8 | 2026-11-12T19:56 … 2026-11-13T15:53 | 9 | 943 / 717 | 172.3 / 87.1 | 0 | $90.42 |
| `-workspaces-api` — E9 | 2026-11-13T15:52 … 2026-11-13T22:04 | 9 | 741 / 175 | 166.8 / 67.8 | 1 | $54.75 |
| `-workspaces-api` — E10 | 2026-11-13T22:02 … 2026-11-14T05:34 | 6 | 840 / 467 | 186.5 / 103.5 | 3 | $77.83 |
| `-workspaces-api` — E11 | 2026-11-14T05:31 … 2026-11-14T20:13 | 7 | 502 / 160 | 152.9 / 74.0 | 0 | $37.03 |
| `-workspaces-api` — E12 | 2026-11-14T19:49 … 2026-11-15T02:22 | 8 | 810 / 212 | 172.0 / 68.7 | 0 | $57.94 |
| `-workspaces-api` — E13 | 2026-11-15T01:55 … 2026-11-15T16:03 | 4 | 377 / 1,458 | 164.4 / 164.0 | 2 | $100.54 |

- The context of a main call fell from 371.4 to 173.9 thousand, −53%, and of a subagent's from 104.4 to 86.5, −17%. The windows differ in work: "before" is mostly the code of E2–E7, "after" the move to the tag and documents; most subagent calls are the blind reading of the translations.
- By session, in order, main calls and a main call's context: 183, 241.9 thousand; 125, 196.2; 91, 112.2; 96, 116.6; 51, 93.0.
- The reference of `api` for the threshold of an analysis of the sessions is its row "after" in measurements 1.9 spec, not this row.
- E8, at its archive on 2026-11-13: `tokens-report --since 2026-11-12T19:56 --row`, from the start of the session that archived E7, set up E8 and did its first task; the window holds the five sessions of the move above and the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E7's archive and the first task — 146, 209.1 thousand; the second task — 175, 176.2; the third — 53, 73.9. No analysis of the sessions is due: `api` has no reference yet, a main call's context is below the row of the move to the line 0.16, and `docs-budgets.json` has been written once and raised never.
- E9, at its archive on 2026-11-13: `tokens-report --since 2026-11-13T15:52 --row`, from the start of the session that archived E8, set up E9 and did the last edit of a task of E8; the window holds the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E8's archive — 50, 73.7 thousand; the tasks of E9, in order — 58, 92.5; 32, 83.1; 126, 210.3; 76, 100.8; 98, 154.7; 178, 189.8; 107, 161.9; E9's archive — 18, 50.8, to the measurement. The one compaction is the sixth task's. No analysis of the sessions is due: `api` still has no reference — measurements 1.9 spec at v0.16.5 holds only its row before — a main call's context is below the row of E8, and `docs-budgets.json` has had no limit raised: the limit of the section on the protocol was only taken out.
- E10, at its archive on 2026-11-14: `tokens-report --since 2026-11-13T22:02 --row`, from the start of the session that archived E9 — whole now, to the measurement in the row of E9 —, the session that set up E10 and the archive's own session to the moment of the measurement. By session, main calls and a main call's context: E9's archive — 41, 67.8 thousand; E10's setup — 45, 107.0; the tasks of E10, in order — 144, 169.7; 260, 157.5; 326, 173.6; E10's archive — 24, 72.1, to the measurement. The compactions are one of the second task and two of the third. No analysis of the sessions is due: the reference of `api` is now its row in measurements 1.9 spec at v0.17.1, 162.6 thousand, and a main call's context here is 14.7% above it, under 25%, over 840 main calls; `docs-budgets.json` has had no limit raised since it was written.
- E11, at its archive on 2026-11-14: `tokens-report --since 2026-11-14T05:31 --row`, from the start of the session that archived E10 — whole now, to the measurement in the row of E10 —, to the archive's own session at the moment of the measurement, which also did a task between epics. By session, main calls and a main call's context: E10's archive — 40, 85.7 thousand; the first task of E11 — 107, 124.9; a session of the author's question on the local run, with no task — 9, 46.5; E11's setup — 32, 84.9; the second task — 102, 144.5; the third — 148, 185.8; E11's archive and the task between epics — 64, 126.5, to the measurement. No compactions. No analysis of the sessions is due: a main call's context, 152.9 thousand, is 6.0% below the reference of `api`, its row in measurements 1.9 spec at v0.17.1, 162.6 thousand; `docs-budgets.json` has had no limit raised since it was written.
- E12, at its archive on 2026-11-15: `tokens-report --since 2026-11-14T19:49 --row`, from the start of the session that archived E11 and did a task between epics — whole now, to the measurement in the row of E11 —, to the archive's own session at the moment of the measurement, which also did a task between epics and set up E13. By session, main calls and a main call's context: E11's archive and the task between epics — 90, 149.1 thousand; E12's setup — 31, 104.1; a session of the request of `app` for E13, with no task — 62, 118.4; the tasks of E12, in order — 78, 111.0; 130, 191.4; 141, 227.3; 127, 164.1; E12's archive, the task between epics and E13's setup — 151, 191.5, to the measurement. No compactions. No analysis of the sessions is due: a main call's context, 172.0 thousand, is 5.8% above the reference of `api`, its row in measurements 1.9 spec at v0.17.1, 162.6 thousand, under 25%; `docs-budgets.json` has had no limit raised since it was written.
- E13, at its archive on 2026-11-15: `tokens-report --since 2026-11-15T01:55 --row`, from the start of the session that archived E12, did a task between epics and set up E13 — whole now, past the measurement in the row of E12 —, to the archive's own session at the moment of the measurement. By session, main and subagent calls and a main call's context: E12's archive, the task between epics and E13's setup — 192 and 53, 178.5 thousand; seven tasks of E13, in one session — 79 and 1,405, 165.9; the first task of E13 — 75 and 0, 109.2; E13's archive — 31 and 0, 94.7, to the measurement. The compactions are one of each of the first two sessions. Most of the window's cost, $79.00 of $100.54, is the session of the seven tasks, nearly all of it in its subagents' calls. No analysis of the sessions is due: a main call's context, 164.4 thousand, is 1.1% above the reference of `api`, its row in measurements 1.9 spec at v0.17.1, 162.6 thousand, under 25%; `docs-budgets.json` has had no limit raised since it was written.

## 5. Fuzzing, 2026-11-13

For the fuzz tests of the Go code. `just fuzz 5m` in the devcontainer, 20 workers of `go test -fuzz` on this machine (`nproc` 20), Go 1.27.4, each test for five minutes from its seeds; the executions are those of the last line of each run. No test failed, and no input was written to `testdata/fuzz/`.

| Test | Package | Executions in 5 minutes |
|---|---|---|
| `FuzzRead` | `internal/invite` | 35,175,054 |
| `FuzzCursorOf` | `internal/learning` | 2,411,589 |
| `FuzzFromName` | `internal/packages` | 10,599,276 |
| `FuzzParseConfig` | `internal/packages` | 5,665,657 |
| `FuzzUnpack` | `internal/packages` | 31,940 before the refusal of sparse files, 302,342 after |
| `FuzzRoutes` | `internal/server` | 11,324,242 |

`FuzzUnpack` stood for tens of seconds at 0 executions a second. One cause was an entry of PAX sparse, which the reader of `archive/tar` unfolds into zeros up to the limit of 512 MB (`readGNUSparsePAXHeaders` of [`reader.go`](https://github.com/golang/go/blob/go1.27.4/src/archive/tar/reader.go) of Go 1.27.4; the input `internal/packages/testdata/fuzz/FuzzUnpack/sparse-file`): a tar of 3,584 bytes, 193 after gzip, with the records `GNU.sparse.major=1`, `minor=0`, `name=inventory.json` and `realsize=500000000`, was unpacked by `unpack` to a file of 500,000,000 bytes; once `unpack` refused it, the run made ten times as many executions. Its remaining stops are the minimization of new inputs, up to 60 s each by default: a minute with `-fuzzminimizetime 2s` made 79,353 executions against 39,520 without it.

The refusal does not bound the memory of the build; the limit on the size of an unpacked package does: 500,000,000 zero bytes gzip to 485,269 bytes (`head -c 500000000 /dev/zero | gzip -c | wc -c`, the default level), and a regular file of them unpacks as far as the limit allows.

After the review the two tests changed and were fuzzed again, `just fuzz 5m <test>` with `-fuzzminimizetime 2s`, without a failure: `FuzzRoutes`, now on `rpc.Mount` of every service, 1,244,726 executions; `FuzzCursorOf`, now bounded at the year 9999 too, 744,648.

## 6. The check of an own key, 2026-11-14

For the check of an own key. `just dev-ai` — `serve` on the models, at the stage `author`, on the dev projects of Firebase and Neon —, from the devcontainer, 2026-11-14 at about 19:15 UTC: `CheckOwnKey` of the owner's profile of the dev account, three requests one after another. The times are `duration_ms` of the request's line of the log, and the call's own beside it, by `curl` from the script:

| Key | Answer | `duration_ms` | The call, ms | Requests to Anthropic |
|---|---|---|---|---|
| the author's dev key of Anthropic, as an own key | `OWN_KEY_CHECK_ACCEPTED` | 713 | 721 | 2 `count_tokens` |
| a synthetic key of Anthropic's form | `OWN_KEY_CHECK_INVALID` | 472 | 480 | 2 `count_tokens`, each 401 |
| a synthetic key of OpenAI | `OWN_KEY_CHECK_PROVIDER_CLOSED` | 323 | 353 | none |

The two requests are those of the two models `tutor` v0.1.33 would call with the key — Opus 5.5 and Haiku 5.5 —, one after the other: a gate with a counting adapter dials two models for one check. The log held neither key, and its lines `request` named the end alone, `key_check`. Token counting is free (sources 10.7): four requests, $0.

## 7. The words after a correct answer on a model, 2026-11-15

For the words after a correct answer. `serve` on the models, as `just dev-ai serve` runs it — the dev file's key of platform AI, `tutor` v0.1.35, at the stage `author`, on the dev projects of Firebase and Neon —, from the devcontainer, 2026-11-15 at about 15:35 UTC. Under the tag's texts these calls are "not admitted" until 2026-11-27, the day version 3 of the privacy policy is in force; the run went before it, on a copy of the legal texts by `API_LEGAL_DIR`: the tag's texts with version 3 of `privacy-policy` in force from 2026-11-15. The code and its rule of the day are those of the service; only the copy's day differs, and a copy's history is its operator's. The account is a synthetic one of its own, `dev-t84`, by an invite code, on `closed_slice`: an owner's profile, adult, on platform AI, with `novatrail/olympiad-math` 0.3.0 of `modules` v0.2.7. The dev account's family day had no room left for a task AI reads, since the runs of `dev-check` on the stub keep their reserves as spent. The requests went by `curl`, as `app` sends them, through the functions of `scripts/dev.sh`; the work was a synthetic PNG of the number, in thick strokes.

| Request | Task | Answer | Tokens: input, output (of them thinking) | The model, ms (first token) | `request.duration_ms` | Cost |
|---|---|---|---|---|---|---|
| `SubmitWork` with the work, the pair | `tri-12-d1-1`, a sum, by handwriting | correct, read as the right value | Opus 5.5: 1,441, 61 (0); Haiku 5.5: 1,441, 173 (112) | 3,412; 2,667 | 6,942 | $0.0072 |
| `GetCorrectAnswerWords` with the work | the same | words | Opus 5.5: 1,315, 351 (316) | 5,704 (5,395) | 7,686 | $0.0123 |
| `SubmitWork` by tap | `kl-34-d1-2`, knights and liars, `choice` alone | correct | — | — | 1,642 | $0 |
| `GetCorrectAnswerWords` without work | the same | words | Opus 5.5: 1,252, 440 (371) | 6,144 (5,538) | 8,020 | $0.0138 |
| `GetCorrectAnswerWords` again, a new key | the same | `failed_precondition`, `CAUSE_NO_MORE_WORDS` | — | — | 815 | $0 |

- **The words**, retold, since the tutor's words are content: with the work — praise that the total written is right, and the question how the learner found the sum of the numbers; without work — one sentence on which statement settles the task first, and the question whether supposing the opposite helped. Both ended with a question and passed the leak check and `words.Correct`: the answers held the words, and no line `request` held `words_check`. No words failed a check.
- **The time** of the words is the model's thinking: of 351 and 440 output tokens, 316 and 371 are thinking, and the first token comes at 5.4–5.5 s. The request as a whole takes 7.7–8.0 s, less than the words of a rung of measurements 2.2, p50 10.6 s.
- **The quota:** the export after the run — 33,303 micro-dollars spent of the profile and of the account, nothing in reserve, from 0 before; the sum of `cost_micro_usd` of the four lines `ai_usage`, to the micro-dollar. The prices are those of sources 7.1: Opus 5.5 $4 and $20 per MTok, Haiku 5.5 $0.10 and $0.50.
- **The log of `serve`** — 52 lines of JSON; the check `clean` of `scripts/dev.sh` passed: no UID, email, connection string, key or token. Neither words, the tasks' statements nor the crop were in it.
- **In all:** Anthropic, 4 calls — the reading by the pair and two words —, $0.0333.

## 8. What a run of `dev-check` spends of the quota, 2026-11-15

For the tariff `dev_check`. `just dev-check` on the dev projects of Firebase and Neon, twice one after the other in one family day, from the devcontainer, 2026-11-15 at about 17:00 UTC, on `modules` v0.2.7. The first run found the account `dev-author` with no code kept, deleted it and made it anew on the tariff `dev_check`; the second gave it the tariff by `api tariff` and the kept code. At its end each run prints what the account's counters of the export hold, over their family days.

| Run | The account's counters: spent | In reserve |
|---|---|---|
| the first, on the account made anew | 174,210 | 0 |
| the second | 348,420 | 0 |

- **One run** spends 174,210 micro-dollars of the account's family day — the same in both runs, to the micro-dollar, and in two more after the review, at 522,630 and 696,840. Of them, by the values of `internal/quota` and the stub's rule, the child's workbook page of 8 boxes is 112,000 (14,000 a box) and the owner's essay 30,000; the remaining 32,210 are the readings of handwriting and the words after a correct answer, at the prices `tutor` expects. The owner's profile spends 62,210 of them, and the child's profile, deleted at the end of the run, the page.
- **What it means for the tariffs:** on `closed_slice` the account's $1.50 holds 8 runs a day and the owner's $0.50 the same; on `dev_check` the account's $15.00 holds 86 and the owner's $5.00 80.
- **Money:** none. The first server's key of platform AI is one Anthropic refuses, and the second server is on the stub of `tutor`: the micro-dollars are the reserves kept as spent and the stub's prices, not a provider's charges. No call of AI reached a model, $0.

## 9. The jobs "Image" and "Modules", 2026-11-16

For the jobs of CI and the release. The time of each job, from `completed_at` less `started_at` of `actions/runs/<id>/jobs` of the GitHub API, read 2026-11-16: before — the last five successful runs of CI on `main` (43736033707, 43742167838, 43807183856, 43809505651, 43814975191); after — the runs of pull request 107: Image in run 43817811699 and its second attempt, Modules in that run and in 43817420516, of the commit before the review's fixes, which do not touch it.

| Job | Before, the five runs | Before, median | After |
|---|---|---|---|
| Image | 768, 502, 418, 415, 421 s | 418 s | 271, 388 s |
| Modules | 150, 200, 148, 178, 154 s | 154 s | 136, 172 s |

- **The cache** gave its entry in each of the four, `toolchain-Linux-X64-f8ac05ef…`, 390 MB, saved by "Go" on `main`: the restore 4–8 s, and `toolchain.sh` had the image loaded 12–36 s after it, as measurements 3.1 spec found the load.
- **`release-files`, once now,** took 156 s and 236 s of the two runs of "Image", and the stage `build` of the image from the source 72 s and 107 s; its binary was the release's, byte for byte, in both. The second run, 169 s on `main` (measurements 3.1 spec), is gone.
- **What it means:** two runs are no median, and the runners' spread — `release-files` 156 against 236 s on one commit — is as wide as what was saved; "Image" was below `main`'s median in both, by 147 and 30 s. "Modules" saves what the build of the stage cost it, its spread aside.

## 10. CI on `.github` v0.1.30, 2026-11-16

For CI and the release on `.github` v0.1.30. The time of each job, from `completed_at` less `started_at` of `actions/runs/<id>/jobs` of the GitHub API, read 2026-11-16: before — the last five successful runs of CI and of CodeQL on `main` on `.github` v0.1.24 (CI 43807183856, 43809505651, 43814975191, 43822628657, 43827520586; CodeQL 43807183852, 43809505652, 43814975151, 43822628609, 43827520417); after — the first run of pull request 109, CI 43856145389 — its first attempt, and its second for "Go / test" — and CodeQL 43856145378 of the same commit.

| Job | Before, the five runs | Before, median | After |
|---|---|---|---|
| Image | 418, 415, 421, 355, 276 s | 415 s | 383 s |
| Modules | 148, 178, 154, 132, 185 s | 154 s | 132 s |
| Go / lint | 125, 151, 146, 128, 120 s | 128 s | 113 s |
| Go / test | 397, 383, 392, 274, 400 s | 392 s | 419 s, red: `TestARequestEndsAtItsTimeLimit`; 278 s, green, the second attempt |
| Go / vuln | 65, 85, 77, 50, 60 s | 65 s | 49 s |
| Go / licenses | 63, 87, 51, 63, 56 s | 63 s | 51 s |
| Go / toolchain | — | — | 7 s |
| CodeQL / analyze (go) | 126, 132, 138, 198, 144 s | 138 s | 112 s |

- **The image** came from the cache in every job of the pull request: the entry `toolchain-Linux-X64-f8ac05ef…` of the path `novatrail-toolchain.tar`, 390 MB, which "Documents" had saved on `main`, the file of the image `novatrail-toolchain`; `toolchain.sh` had it loaded in "Image" and "Modules" 13–21 s after the restore, and "Go / toolchain" found it and built nothing.
- **The Go caches** missed in every job — `go-Linux-X64-<check>-…`: `main` has none until its first push on v0.1.30 —, so the jobs of "Go" ran as cold as before, and what they differ by is the runners' spread. The gain of the Go caches is for the runs after that push; this section has none.
- **`TestARequestEndsAtItsTimeLimit`** of `internal/rpc` got `unavailable` where it wants `deadline_exceeded`: its time limit is 50 ms, and on that runner it ran out in the checks before the handler, whose failure `rpc.Failed` answers `unavailable`. It is the first failure of the test in the last 15 failed runs of CI, and the job's second attempt passed. A flake of the time limit, not of the move: the code under test and its Go are those of `main`.
- **CodeQL's `go`** took Go from the archive the Dockerfile pins, with no build of the stage: 112 s against the median 138 s.
- **The release** of v0.1.30 runs only on `main`; before, its three runs that put out a release took 1308, 1193 and 1171 s (Release 43807893468, 43810453379, 43828414762). The first one after the merge is the measurement of the release, as the Go caches are of "Go".

## 11. CI on `spec` v0.18.4 and `.github` v0.1.31, 2026-11-16

For CI, the release and the devcontainer on `spec` v0.18.4 and `.github` v0.1.31. The time of each job and of its long steps, from `completed_at` less `started_at` of `actions/runs/<id>/jobs` of the GitHub API, read 2026-11-16: before — the run of CI on `main` after the merge of the move to `.github` v0.1.30, 43861555933, on `.github` v0.1.30, whose Go caches missed, as it was the first push that saved them (measurements 10); the release of that commit, Release 43862499430; and Withdrawals 43878837719 of the same day.

| Job | Before | Its long steps, before | After | Its long steps, after |
|---|---|---|---|---|
| Image | 326 s | the image from the cache 4 s; "Build and check the images" 318 s | 299 s | "Build and check the images" 294 s |
| Modules | 137 s | the image from the cache 7 s; `ci-modules` 118 s | 152 s | "Check modules.json" 136 s |
| Documents | 52 s | the image from the cache 4 s and loaded 31 s; the check 12 s | 64 s | the image 31 s; the check 13 s |
| Go / toolchain | — | — | 111 s | the image 61 s; the files of `toolchain` and the core 17 s each |
| Go / lint | 152 s | the image 6 s and 32 s; `ci-lint` 98 s | 172 s | the image 7 s and 21 s; `ci-lint` 129 s |
| Go / vuln | 70 s | the image 9 s and 12 s; `ci-vuln` 28 s | 74 s | the image 9 s and 14 s; `ci-vuln` 29 s |
| Go / licenses | 84 s | the image 5 s and 34 s; `ci-licenses` 34 s | 79 s | the image 6 s and 32 s; `ci-licenses` 28 s |
| Go / test | 319 s | the image 5 s and 29 s; `ci-test` 270 s | 410 s | the image 15 s and 21 s; `ci-test` 356 s |
| Release / release | 552 s | "Work out the version" 270 s; "Build the files" 138 s; the signatures 83 s | 351 s | "Work out the version" 22 s; "Build the files" 206 s; the signatures 55 s |
| Withdrawals | 241 s | the stage `toolchain` built from the Dockerfile and `ci-withdrawals`, one step, 233 s | — | not run on `main` since the merge |

After — read 2026-11-16: the run of CI on `main` after the merge of the move, 43909131792, the first on the new Dockerfile, and the release of that commit, Release 43912635423. The run is cold: the keys hold the hash of the Dockerfile, so "Image" and "Modules" found no image in the cache and built their stage inside their long step, and every job of "Go" missed its Go caches and saved them (its log: "Cache not found", "Cache saved with key"). The before run missed its Go caches too and found its images, so "Go / test" — 319 s against 410 s, `ci-test` 270 s against 356 s — compares two cold Go caches, one run a side, and "Image" and "Modules" compare a warm image with a cold one. The release, with no cache either way, gained 201 s: "Work out the version" 248 s less and the signatures 28 s less, while "Build the files" took 68 s more. The run of the pull request, 43907246572, gave "Image" 392 s and "Go / test" 408 s, `ci-test` 358 s. The warm numbers and the first "Withdrawals" on the core are measurements 13.

## 12. What a run of `dev-check` spends of the quota on `tutor` v0.1.46, 2026-11-17

For the move to `tutor` v0.1.46. `just dev-check` on the dev projects of Firebase and Neon, twice one after the other in one family day, from the devcontainer, 2026-11-17 at about 04:30 UTC, on `modules` v0.2.7 and `tutor` v0.1.46, the reading mode `pair`; the account `dev-author` on the tariff `dev_check` held what the day's earlier runs had spent.

| Run | The account's counters: spent | In reserve |
|---|---|---|
| the first | 1,988,920 | 0 |
| the second | 2,052,187 | 0 |

- **One run** spends 63,267 micro-dollars of the account's family day, against 174,210 in measurements 8. Of them the child's workbook page of 8 boxes is 30,165, `tutor`'s expected price of a page by the pair, in place of 14,000 a box; the owner's essay 892 — the reading 271 and the assessment 621, in place of 30,000 —; the remaining 32,210 are the readings of handwriting as in measurements 8. The prices are those of `Gate.Price` on the stub's gate, read the same day.
- **What it means for the tariffs:** the owner's profile spends 33,102 a run; on `closed_slice` the account's $1.50 holds 23 runs a day and the owner's $0.50 15; on `dev_check` the account's $15.00 holds 237 and the owner's $5.00 151.
- **Money:** none, as in measurements 8: the first server's key is one Anthropic refuses, the second server is on the stub of `tutor`. No call of AI reached a model, $0.

## 13. CI with warm caches on `spec` v0.18.6–v0.18.14, 2026-11-17

For CI on the shared image of the devcontainer; Д45 closed. The time of each job and of its long step, from `completed_at` less `started_at` of `actions/runs/<id>/jobs` of the GitHub API, and the caches from the jobs' logs, read 2026-11-17: the eight runs of CI on `main` from the merge of the last change of `.devcontainer/Dockerfile` — 43972631133, the cold one; 43979009553 and 44024756573, the warm ones; 43983363145, 43999016266, 44005962289, 44019977255 and 44114431112, which missed —, their releases, and Withdrawals 44104888691 of 2026-11-17.

| Job | Cold | Warm Go caches, the two | Go caches missed, the five, median | Its long step, the same three |
|---|---|---|---|---|
| Image | 267 s | 320, 273 s | 276 s, the seven warm runs | "Build and check the images" 255; 313, 254; median of seven 256 s |
| Modules | 162 s | 136, 122 s | 136 s, the seven | "Check modules.json" 152; 116, 108; median of seven 116 s |
| Documents | 67 s | 48, 51 s | 48 s, the seven | — |
| Go / toolchain | 267 s, built the images | 12, 14 s | 12 s, the seven | — |
| Go / lint | 163 s | 68, 62 s | 170 s | `ci-lint` 122; 3, 2; 126 s |
| Go / test | 289 s | 211, 166 s | 381 s | `ci-test` 245; 158, 109; 329 s |
| Go / vuln | 57 s | 66, 41 s | 72 s | `ci-vuln` 29; 5, 4; 29 s |
| Go / licenses | 78 s | 55, 50 s | 65 s | `ci-licenses` 28; 1, 1; 27 s |

- **The images** came from the cache in every job after the cold run, whose "Go / toolchain" built `toolchain` and `toolchain-core` in 219 s and saved them; "Image", "Modules" and "Documents" spent 3–9 s restoring it. "Image" is not faster than measurements 11's cold 299 s by more than its spread over the seven warm runs, 253–354 s: its long step builds and checks the release's files, which no cache holds.
- **The Go caches** hit only where `go.sum` did not change: their key is `go-<os>-<arch>-<check>-<hash of the Dockerfile>-<hash of go.sum>`, with no `restore-keys` (`go.yml` of `.github` v0.1.31 and v0.1.32). Of the two warm runs, one changed Go code but no dependency and the other only documents; the five other runs moved `tutor`, `learner-model` or `spec` and found no entry ("Cache not found for input keys"). Warm, "Go / test" is 166–211 s against 319 s on `main` before the move to `spec` v0.18.4, `ci-lint` 2–3 s against 98 s; missed, they are as cold as before. In `api` a task moves a library more often than not, so the gain of the Go caches holds in two of the seven runs after the cold one. The key is `.github`'s: row В83.
- **The release** of a version (Release 43975333910, 43984576297, 43999913140, 44006899539, 44020786062, 44118079218) took 269–340 s, median 274 s, against 351 s after the move to `spec` v0.18.4 and 552 s before it: "Work out the version" 23–39 s, "Build the files" 140–221 s, the signatures 26–38 s. The two warm runs, which put out no release, took 40 and 38 s.
- **Withdrawals** ran on `toolchain-core` from the cache: 58 s against 241 s, the image restored in 8 s by the key `toolchain-core-…` and the check 35 s.
- **One PostgreSQL a package** (Д43): a run of `TestADatabaseIsMigratedOrEmpty` alone — the supervisor's build from the cache, `initdb`, the start and the migrated template — took 4.3–5.4 s in the devcontainer, three runs; twelve packages start one each. One server for the run would save at most about 55 s of the packages' time, of some 315 s they add up to in the warm run 44024756573, and less of `ci-test`'s 109–355 s, since `go test` runs the packages in parallel.

## 14. A re-sign-in by a popup on the dev project, 2026-11-12

For the fresh sign-in: whether a re-sign-in gives a new `auth_time`. `@firebase/auth` [1.13.8](https://github.com/firebase/firebase-js-sdk/tree/@firebase/auth@1.13.8/packages/auth) does not say so directly: it follows from `reauthenticateWithPopup` being a new sign-in at the sign-in service. On the dev project a sign-out and a new sign-in by a popup changed the user's last sign-in time at the sign-in service — an indirect check, not of the token itself.
