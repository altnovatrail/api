# The documents of api

What the documents of `api` are, how to work on it and how a change comes in. The component itself, in a few lines, is the repository's [README](../README.md).

## Documents

- [architecture.md](architecture.md) — the component: what it does and does not do, its data and links, what was decided and how the service is built.
- [build.md](build.md) — how api is built, checked, released, configured and run: its commands and variables, the sign-in it expects in production, and its run on the dev projects and with app.
- [sources.md](sources.md) — the external facts the documents rest on: each source with its address, the date it was read and a quote.
- [measurements.md](measurements.md) — the results of measurements: what was measured, how, and the numbers.
- [decisions.md](decisions.md) — the decision log: what was decided, why, and what was rejected.
- [plan/](plan/) — the archives of the done epics.
- [RUN.md](../RUN.md) — the work plan, in epics; `run/` — the plan's other tables: [the epics for later](../run/EPICS.md), [the open questions](../run/QUESTIONS.md), [the technical debt](../run/DEBT.md) and [the tasks outside an epic](../run/TASKS.md).

The working rules, for people and coding agents alike, are in [CLAUDE.md](../CLAUDE.md) and [AGENTS.md](../AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec), checked out here as a git submodule: clone this repository with `git clone --recurse-submodules`.

## Releases

A release comes out by itself once CI passes on `main`: signed static binaries, the `Dockerfile` and the licenses, and NovaTrail publishes no image. What it holds, how to verify it and how to build the image around its binary are [build.md, Releases](build.md#releases).

## Contributing

Report a bug or propose a change in an [issue](https://github.com/altnovatrail/api/issues), in English or Russian, and open one before a pull request unless the pull request fixes a typo or a broken link. How a change comes in, with every commit signed off, and what every change keeps to are in the organization's [CONTRIBUTING.md](https://github.com/altnovatrail/.github/blob/main/CONTRIBUTING.md); the rules every NovaTrail repository shares are in spec's [RULES.md](https://github.com/altnovatrail/spec/blob/main/RULES.md): code comes with its tests, and no learner's data, not even in examples, which are synthetic. Report a vulnerability privately, as the [security policy](https://github.com/altnovatrail/api/security/policy) says, never in a public issue.

Work in the devcontainer: open the repository in VS Code and choose "Reopen in Container". It holds every tool at the version the repository pins. The checks are the recipes of the [justfile](../justfile) that [build.md, Build and checks](build.md#build-and-checks) names, and `just --list` names each with its arguments. A check exits with a non-zero status when it finds a problem. CI runs them all on every pull request and on `main` — all but `just ci-withdrawals`, which the job Withdrawals runs once a day —, builds and checks the image, and a pull request merges only when they pass and its changed lines are at least 90% covered. Documents are checked by `just -f spec/justfile docs-check-repo`. The answers of the project to the criteria of OpenSSF Best Practices are in [.bestpractices.json](../.bestpractices.json).
