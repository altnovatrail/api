# AGENTS.md

The working rules of this repository are in [CLAUDE.md](CLAUDE.md), and they bind every coding agent, not only Claude Code. Read it in full before any work and follow it as written: what it asks of Claude, it asks of you. It stays the one copy of the rules; this file only says what stands in for the parts that are Claude Code's own:

- **Imports.** A line of CLAUDE.md that reads `@` and a path brings that file in, and Claude Code reads it as part of CLAUDE.md. Read each such file in full as well: the rules every NovaTrail repository shares come in this way.
- **The cloud-session exception to "Devcontainer only"** is for a cloud session of Claude Code. A Codex session that does not see `NOVATRAIL_DEVCONTAINER=1` stops before its first edit.
- **The review before a task is called done.** `/code-review high` is a Claude Code command, and you cannot run it; Codex's `/review` stands in for it. Stop and ask the author to run it, then handle its findings as the rules for `/code-review high` say. Until a review has run, the report says that it did not.
- **Memory.** Claude Code's memory is not shared with you. What has to outlive a session goes into the repository's documents, as CLAUDE.md says.
