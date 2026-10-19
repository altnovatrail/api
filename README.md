# api

The NovaTrail platform service, the only service of the first slice. It checks the account owner's sign-in and keeps the learners, their consent and their attempts in its database — among the platform's parts, the one place where learners' data is stored on the server; whether the learner model lives there too is still an open question of spec's. It runs each check — tutor reads handwritten work and module-kit gives the verdict, while an answer typed or chosen is checked without AI — then has learner-model weigh the attempt's evidence and engine pick the next lesson or task with a reason code: all four are libraries in its own process, and it reaches AI providers only through tutor, without the learner's identifiers.

A component of [NovaTrail](https://github.com/altnovatrail/spec/tree/v0.1.0), a universal open platform for adaptive learning, interactive lessons and AI tutoring. It belongs to stage 2, First slice with learners, with tutor, canvas, app, skill-map and infra.

## Status

Detailing. The documents come first: the first epic of [RUN.md](RUN.md) decides what the component's card left open, and the code follows in the order RUN.md sets. There is no code here yet: it comes after the first code of tutor and canvas, which measures how well AI reads the work, and no child learns here before verifiable parental consent and the contracts with AI providers are in place.

## Documents

The documents are in Russian.

- [docs/architecture.md](docs/architecture.md) — the component: what it does and does not do, its data and links, and what is still to decide.
- [docs/decisions.md](docs/decisions.md) — the decision log: what was decided, why, and what was rejected.
- [RUN.md](RUN.md) — the work plan, in epics.

The working rules, for people and coding agents alike, are in [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md). The platform's shared vocabulary, formats, contracts and decisions are in [spec](https://github.com/altnovatrail/spec/tree/v0.1.0), checked out here as a git submodule at `v0.1.0`: clone this repository with `git clone --recurse-submodules`.

## License

[Apache-2.0](LICENSE).
