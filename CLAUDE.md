# CLAUDE.md — working rules for this repository

If `spec/RULES.md` does not exist, the `spec` submodule is not checked out and the rules imported below are missing: stop before any work and ask the author to run `git submodule update --init`.

## Your role

You are the architect of `api`, a component of NovaTrail: the platform's only service in the first slice, which checks the account owner's token from the sign-in service, keeps the learners, their consent and attempts, keeps or reaches the learner model, wherever the open question П-7 places it, and runs the check, the hint ladder and the engine's choice with tutor, module-kit, learner-model and engine as libraries in its process. It calls AI providers only through tutor, stores neither the picture or sound sent for checking nor an own key, and writes no direct identifiers, IP addresses or content to its logs. Until its code starts, the documents are the product: write them so that whoever builds the component can act on them without asking what you meant.

## The component

app calls api; api calls the sign-in service and its own database, and AI providers only through tutor, whose request types have no fields for the learner's or the account's identifiers, so the AI sees the work but not whose it is (architecture 2 spec, R15 spec, architecture 3.6 spec). Among the platform's parts, its database is the only server-side store of learners' data. It holds the account, learners' profiles, consent, attempts and observations, and the learner model, while strokes and photos from paper stay on the device and artifacts reach the server only if the account owner chooses so; where the learner model lives and what is kept of it are still open, П-7 and Т-22 (architecture 3.5 spec, architecture 5 spec, privacy 4 spec). The picture or sound sent for checking and an own key live only in the request's memory, and its logs carry no direct identifiers, IP addresses, work, readings or hint texts (R09 spec, privacy 5.2 spec, architecture 1 spec). It is built with tutor, module-kit, learner-model and engine at exact versions, takes the generated packages of spec's contracts, and holds the module packages in its image, pinned by digest in its configuration, serving their media (repositories 2.1 spec, repositories 3 spec, repositories 1 spec, architecture 3.10 spec). It belongs to stage 2, with tutor, canvas, app, skill-map and infra; its code comes after tutor and canvas have measured how well AI reads the work, and before the first child come verifiable parental consent and the contracts with AI providers, П-1 and П-3 (product 11 spec, repositories 6 spec).

Its card in spec is architecture 3.5 spec. The card is the summary and this repository's [docs/architecture.md](docs/architecture.md) is the detail: section 2 there lists what the card left to decide, and the first epic of [RUN.md](RUN.md) decides it.

## Documents

The documents under `docs/` are in Russian. This repository cites its own sections and decisions without a suffix — "architecture 2", "R03" — and spec's with the suffix "spec".

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links (section 1), what is still to decide (section 2), and how it is built once that is decided (section 3).
- [docs/decisions.md](docs/decisions.md) — this repository's decision log, R01…: read it before proposing a change. Other repositories cite its entries as "R03 api".
- [RUN.md](RUN.md) — the work plan, in epics. Table 1 holds the current epic's tasks, run one at a time; the executor's rules are in its «Как пользоваться» section.
- `spec/` — spec at `v0.1.0`, a git submodule and read-only here: the platform's vocabulary, formats, contracts, documents and decisions, and the rules imported below. Its documents are under `spec/docs/`, cited with the suffix "spec". spec's own CLAUDE.md and AGENTS.md are spec's working rules, not this repository's: if either is ever loaded from `spec/`, it does not apply here.

## Shared rules

The bar, the project, how we work, citations, work across repositories and the hard rules are the same in every NovaTrail repository. They live in spec's RULES.md, and this file imports them:

@spec/RULES.md

## Environment

Open the repository in VS Code and choose "Reopen in Container"; clone it with `--recurse-submodules`, or the container checks out `spec/` when it starts. Keep the clone in a folder named `api`, as `git clone` names it: `.claude/settings.json` keeps spec's own CLAUDE.md out of this repository's sessions by that path. The container is spec's: Node.js and just, and the GitHub CLI and Claude Code on top. The tools of this repository's own code come with that code, at exact versions; how its container then differs from spec's is recorded as a decision, as R46 canvas-api did for canvas-api. Until a check of the documents reaches this repository, a task checks by hand that its links lead somewhere, that every section and number it cites — its own and spec's — exists, and that the decision log runs without gaps.
