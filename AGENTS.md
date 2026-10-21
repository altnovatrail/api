# AGENTS.md

The working rules of this repository are in [CLAUDE.md](CLAUDE.md), and they bind every coding agent, not only Claude Code. Read it in full before any work and follow it as written: what it asks of Claude, it asks of you. It stays the one copy of the rules; this file only says what stands in for the parts that are Claude Code's own:

- **Imports.** A line of CLAUDE.md that reads `@` and a path brings that file in, and Claude Code reads it as part of CLAUDE.md. Read each such file in full as well: the rules every NovaTrail repository shares come in this way.
- **The cloud-session exception to "Devcontainer only"** is for a cloud session of Claude Code. A Codex session that does not see `NOVATRAIL_DEVCONTAINER=1` stops before its first edit.
- **The review before a task is called done.** `/code-review high` is a Claude Code command, and you cannot run it; Codex's `/review` stands in for it. When rule 6 of RUN.md calls for a review, stop and ask the author to run it, once, then handle its findings as the rules for `/code-review high` say. Whether a small change goes without a review is yours to decide, as rule 6 says. The report says whether a review ran, and if none did, why.
- **Memory.** Claude Code's memory is not shared with you. What has to outlive a session goes into the repository's documents, as CLAUDE.md says.
