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
