# Engineering Diary

Latest entries first. Record significant decisions, architecture changes, and non-obvious context.

---

## 2026-09-01 — Dedicated tmux server, and a full codebase audit

hangon previously ran all its tmux sessions on the user's default tmux server.
This caused two problems that showed up during real development: leaked
`hangon-<pid>` sessions cluttering `tmux ls`, and — much worse — `go test`
killing the developer's genuinely live hangon sessions, because the gc
integration tests isolated *state* (temp HOME) but scanned the *real* tmux
server and machine-wide `_serve` processes with an empty live set.

Decision: all tmux traffic now goes through `tmuxCmd()` (tmux.go), pinned to a
dedicated server socket (`tmux -L hangon`, overridable via
`HANGON_TMUX_SOCKET`). Tests get a per-run socket (`hangon-test-<pid>`) via
`TestMain`, with `kill-server` teardown. Targets use `tmuxExact()` → `=name:` —
the `=` disables tmux's silent prefix/fnmatch matching, and the trailing colon
is required because pane-scoped commands (set-option, pipe-pane, send-keys)
reject a bare `=name`. That last detail cost a debugging round: the first
attempt used `=name` and broke pipe-pane silently, which is also how we learned
`set-option -t <sess>` had been fragile all along and that `load-buffer`'s `-t`
is a target-*client* flag we were misusing (now dropped).

Known incompleteness, tracked in TODO.md as P0: the socket change fixed the
*tmux* half of the blast-radius problem only. `gc`'s process scan is still
machine-wide and cross-state-dir, so `go test` can still kill real holders, and
`hangon gc` in a project with a local `./.hangon` kills sessions tracked in
`~/.hangon`. The fix is to match `--state-dir` in the scanned argv.

Same day, a full `/scorecard` audit graded the codebase C overall and seeded
TODO.md with the findings. Recurring themes worth remembering: swallowed tmux
errors turn failures into silent empty output; anything without tests drifted
(mouse.go shipped with SGR press/release inverted); and the docs promise things
the code doesn't do (the README's `keys "q"` examples never worked). Project
conventions (CLAUDE.md) were expanded accordingly: CI-able pre-commit checks,
test-first, atomic commits, docs-with-changes.
