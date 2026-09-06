# Engineering Diary

Latest entries first. Record significant decisions, architecture changes, and non-obvious context.

---

## 2026-09-06 — Second scorecard, and cleaning up the first fix wave's wake

Re-ran `/scorecard` after the big 2026-09-01/02 fix wave. Code grade moved
C → B-, agent-readiness landed at C+. The valuable part wasn't the grade —
it was that a fresh adversarial pass caught the *wake* of the previous
wave: two P0 regressions the wave itself introduced, plus two "fixes" that
were reported done but never actually landed. Worth remembering as a
pattern: a burst of rapid fixes needs a follow-up audit precisely because
each fix is a new, unreviewed change.

The two regressions were both from good-intentioned hardening applied one
layer too broadly. (1) The 2026-09-01 `resolveSession` refactor made a
mistyped session name a hard error instead of silently hitting `default` —
correct — but it probed `rest[0]` even when `rest[0]` was one of the
command's own passthrough flags (`--x`, `--role`), so every no-session
mouse/ax-find invocation died with `no session named "--x"`. (2) The
PID-reuse identity guard correctly withheld the *signal* from a recycled
PID, but the `tmux kill-session` and socket unlink right below it — both
derived from that same PID — still fired, so `stop` could destroy a
different live session that had recycled the PID. Lesson filed: a guard
must cover *every* action derived from the thing it's guarding, not just
the first one.

Design calls this round. The start gate's `exec <cmdStr>` truncated
single-string compound commands (`-- 'a && b'` ran only `a`) because
`exec` replaces the shell with one program; the fix runs the single-arg
shell-string form via `exec sh -c '<string>'` while keeping the multi-arg
argv form as a direct `exec` (so pane_pid stays the real program for
TargetPID). The FIFO moved into the same 0700 runtime dir the control
socket already uses — the socket hardening had left the FIFO behind in
bare `/tmp`, which is exactly the kind of half-migration an audit is for.

Deliberately NOT done, and why: the GLM security pass rated the poisoned
`./.hangon` confused-deputy vector HIGH under an untrusted-CWD threat
model. I closed the arbitrary-unlink half (removeSocketFile refuses
non-sockets) but parked the dial-path and CWD-auto-trust halves as a P2 —
they need a threat-model decision (is an untrusted CWD in scope at all?),
not a mechanical patch. Same discipline on the render `bgOverlap` finding:
it's the likely root cause of the downstream screenshot P1, but it wants
the golden-master test alongside it, so it stayed a well-annotated Open
item rather than a rushed change in a "low-hanging-fruit" pass.

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
