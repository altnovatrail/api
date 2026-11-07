# api

[![CI](https://github.com/altnovatrail/api/actions/workflows/ci.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/ci.yml)
[![CodeQL](https://github.com/altnovatrail/api/actions/workflows/codeql.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/altnovatrail/api/badge)](https://scorecard.dev/viewer/?uri=github.com/altnovatrail/api)
[![License](https://img.shields.io/github/license/altnovatrail/api)](LICENSE)

[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)

The NovaTrail platform service, the only service of the first slice. It checks the account owner's sign-in and keeps the learners, their consent and their attempts in its database — among the platform's parts, the one place where learners' data is stored on the server, and the learner model lives there too, as an append-only log of evidence. It runs each check — tutor reads handwritten work and module-kit gives the verdict, while a choice tapped among options is checked without AI — then has learner-model weigh the attempt's evidence and engine pick the next lesson or task with a reason code: all four are libraries in its own process, and it reaches AI providers only through tutor, without the learner's identifiers.

A component of [NovaTrail](https://github.com/altnovatrail/spec/tree/v0.9.4), a universal open platform for adaptive learning, interactive lessons and AI tutoring. It belongs to stage 2, First slice with learners, with tutor, canvas, app, skill-map and infra.

## Status

Detailing is done. The first epic of [RUN.md](RUN.md) decided everything the component's card left open, and [docs/architecture.md](docs/architecture.md) now describes the service; the code epics it proposes are in RUN.md, table 2. There is no code here yet. Its first build with app, the first app, which the author alone runs locally, comes after the module format's code and content, the learner model, engine and tutor's first code; spec's contract between app and api is already out. The skeleton, database, sign-in and deletion need none of these, and spec's cross-repository plan lets them start now (RUN.md, table 2). No child learns here before verifiable parental consent and the contracts with AI providers are in place.

## Documents

The documents are in Russian.

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links, what was decided and how the service is built.
- [docs/decisions.md](docs/decisions.md) — the decision log: what was decided, why, and what was rejected.
- [RUN.md](RUN.md) — the work plan, in epics.

The working rules, for people and coding agents alike, are in [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec/tree/v0.9.4), checked out here as a git submodule at `v0.9.4`: clone this repository with `git clone --recurse-submodules`.

## License

[Apache-2.0](LICENSE).
