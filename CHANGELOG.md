# Changelog

## 2026-09-01

- Fix output printed immediately at process startup being lost forever:
  tmux began running the pane's command the instant `new-session` returned,
  but `pipe-pane` (and hangon's FIFO reader) were only wired up several
  `tmux` round-trips later, and `pipe-pane` never replays backlog — so any
  output produced in that gap (a fast one-shot command's entire output, or
  a longer-lived command's startup banner) vanished with no way to recover
  it, and `expect` on a startup banner would time out. Fixed by starting
  the pane behind a `read`-based gate (`read -r _hangon_start; exec
  <command>`) that blocks until hangon explicitly releases it with
  `send-keys "Enter"` once `remain-on-exit`, `pipe-pane`, and the FIFO
  reader goroutine are all live — guaranteeing zero output can be produced
  before it's being captured. Reproduced deterministically pre-fix (3/3
  runs of a one-shot `echo hi` produced empty `readall`; `expect` on a
  `sh -c 'echo BANNER; ...'` startup banner timed out every time run
  immediately after start) and added
  `TestIntegration_ImmediateOutputNotLost`, confirmed failing against the
  unfixed code and passing after.
- Fix a tmux session that vanishes externally (e.g. `tmux kill-session`
  run outside hangon) being reported as exit code 0: the poll goroutine
  closed `done` on a failed `has-session` check without ever setting
  `exitCode`, so it stayed at its zero value and `hangon wait` claimed
  success for a killed session. Now surfaced as a distinct, non-zero
  outcome: `exitCode = -1` alongside a `"session terminated externally:
  exit status unknown"` error, which `hangon wait` reports via its
  existing `fatal()` path (message on stderr, exit 2) rather than ever
  printing "exit code: 0". Also: (a) the `set-option remain-on-exit` call
  now checks its error and fails `start` loudly instead of silently
  leaving remain-on-exit off (which would otherwise cause exactly this
  bug the instant a plain, non-killed command exited); (b) the dead
  `&exec.ExitError{}` zero-value assignment is gone; (c) the two
  `display` calls used to check `pane_dead` and then `pane_dead_status`
  are merged into one `#{pane_dead},#{pane_dead_status}` round-trip (this
  also resolves the separate P2 perf TODO about that poll, since the code
  was being rewritten here anyway). Reproduced pre-fix (`hangon wait` on
  an externally-killed session printed "exit code: 0", exit 0) and added
  `TestIntegration_VanishedSessionExitCode`, confirmed failing against
  the unfixed code and passing after; also asserts real exit codes (0
  and 7) still propagate unchanged. Not changed: `hangon status` still
  has no concept of exit code at all (it only reports the wrapping holder
  process's liveness) — out of scope for this bug, which is specifically
  about `wait`'s exit-code reporting.

- Fix `hangon gc` killing sessions belonging to OTHER state directories: it
  built its "live" PID set from a single state dir but then scanned and
  killed every `hangon _serve` process (and every `hangon-<pid>` tmux
  session) on the machine, including ones validly tracked by a different
  state dir (another `--local` checkout, another install, another agent's
  isolated state dir). `gcOrphanedServeProcesses`/`gcOrphanedTmuxSessions`
  now cross-check each candidate's own `--state-dir` argument (parsed from
  the cmdline `listServeProcesses` already returns) and leave alone
  anything that can't be positively confirmed to belong to the state dir
  `gc` is running against. Reproduced with an integration test that starts
  a tracked session under state dir X and runs `gc` against a separate,
  empty state dir Y; before the fix, X's holder and tmux session were both
  killed, confirmed surviving after. Not fixed here (tracked separately,
  P2 TODO): PID-reuse — a recycled PID can still be signalled without an
  identity check.
- Fix SGR mouse press/release bytes being emitted backwards: `sgrMouseSeq`
  in `mouse.go` sent `'m'` for press and `'M'` for release, the reverse of
  the xterm SGR (mode 1006) spec. Effect: every `mouse-click` sent
  release-then-press, which most TUIs silently drop, and `mouse-scroll`
  (press-only, no release event) was a complete no-op. Added 24 golden
  byte-sequence unit tests for click/drag/scroll across all buttons and
  modifier combos, and verified the full CLI-to-app path against a real
  stdin-reading script in an isolated tmux session
- Add `hangon resize [SESSION] --cols N --rows N` to change a running
  session's terminal size, plus `--cols`/`--rows` on `hangon start` to set
  the initial size (default stays 80x24). For tmux-backed process sessions
  this runs `tmux resize-window` on hangon's own dedicated server (never
  the user's default server) and updates the geometry used by `screen` and
  `screenshot`; for the legacy raw-PTY fallback (no tmux installed) it
  calls `pty.Setsize`, which delivers SIGWINCH the same way a real
  terminal emulator would. Sessions with no terminal grid (`tcp`, `ws`,
  `macos`) return a clear "not supported" error instead of silently doing
  nothing. This restores the resize capability a downstream QA suite
  depended on: it broke not because sessions became "raw PTY" (they never
  did — this build is still tmux-backed) but because 21ddf4e moved
  hangon's tmux sessions onto their own dedicated server (`tmux -L
  hangon`), so the suite's direct `tmux resize-window -t "hangon-$PID"`
  against the *default* server silently stopped finding them
- Run all tmux sessions on a dedicated server socket (`tmux -L hangon`,
  overridable via `HANGON_TMUX_SOCKET`) so hangon never sees or kills the
  user's personal tmux sessions and its own sessions don't clutter `tmux ls`;
  all `-t` targets now use exact-match (`=name:`) instead of tmux's silent
  prefix matching, and the test suite runs on its own per-run socket
  (missed changelog entry for the 2026-08-31 commit)
- Add project conventions: `TODO.md` bug tracker (seeded from a full
  `/scorecard` audit), `DIARY.md` engineering diary, expanded `CLAUDE.md`
  rules, and a `make fmt-check` target so `make check` fails on unformatted
  files instead of silently rewriting them (`mouse.go` reformatted)

- Fix three `hangon screenshot` PNG rendering bugs:
  - Large regions of a full-frame screenshot could render as a solid wrong
    color instead of the app's actual background — caused by
    `tmux capture-pane -e -p` silently trimming trailing colored whitespace
    from the end of every line (now captured with `-N` to preserve it)
  - Thin gray seams between adjacent character cells, most visible on light
    backgrounds — caused by drawing one `<rect>` per cell for backgrounds
    instead of one per contiguous same-color run, which produces a hairline
    anti-aliasing artifact at every cell boundary when rasterized
  - The triangle glyphs ◢ ◣ ◤ ◥ (Unicode Geometric Shapes) rendered as tiny
    off-center wedges instead of filling the cell, because they were drawn
    as font glyphs; they're now drawn as full-cell vector polygons instead,
    matching how real terminal emulators render them
- Add `hangon gc [--dry-run]` to reap orphaned resources left behind by
  ungraceful holder deaths (crash, OOM kill, `kill -9`): stale state.json
  entries pointing at dead processes, orphaned `hangon-<pid>` tmux sessions,
  and orphaned `hangon _serve` holder processes with no tracked session at all
- `hangon stopall` now requires `--force` to actually stop anything; without
  it, it previews what it would stop and exits without touching any session.
  The default state directory is shared machine-wide, so an unscoped
  `stopall` affects every hangon session on the machine, not just the
  caller's own — this closes the accidental "killed someone else's sessions"
  footgun
- Unrecognized `--flags` (e.g. a typo, or a flag that was never added) are now
  a hard error instead of being silently absorbed as positional arguments;
  use `--` to pass literal arguments that happen to start with `--`
- Fix a name-collision race in `hangon start`: two concurrent `start --name X`
  calls could previously both pass the "is this name free" check and both
  register a holder process, permanently leaking one of them. The name is now
  claimed atomically under the state lock immediately after the holder is
  spawned
- Fix a lost-update race in `stopall`: it used to finish by writing back a
  brand-new empty state file, which could silently discard a session added by
  another process while `stopall` was still killing the sessions it found —
  it now only removes the exact sessions it actually processed
- Document that reinstalling a locally-built binary must go through
  `make install` / `go install`, not `go build` + `cp`: overwriting an
  in-use binary's file in place (rather than the atomic rename `go install`
  does) can get a still-running `hangon` process SIGKILLed by macOS's
  code-signature page-in validation

## 2026-04-23

- Add mouse event injection: mouse-click, mouse-drag, mouse-scroll commands
- Support click modifiers (shift, alt, ctrl), double/triple click, right/middle button
- Drag with configurable intermediate steps for smooth selection
- Scroll wheel with directional delta

## 2026-04-22

- Add modifier+key combos: shift-arrow, alt-letter, ctrl-arrow, ctrl-space, and more
- Fix NUL byte transmission through tmux (use load-buffer/paste-buffer for binary data)
- Add --stdin flag to `hangon send` for piping raw bytes

## 2026-04-12

- Rewrite README with tutorials and simplified examples

## 2026-04-10

- Simplify release: auto-build on every push to main
- Add Homebrew install instructions to README

## 2026-04-09

- Add GitHub Actions release workflow with auto-increment versioning

## 2026-03-16

- Split --help into topic-based system with platform-conditional macOS content
- Add build artifacts to gitignore and make clean target

## 2026-03-15

- Initial release of hangon
- Add E2E test suite
- Restructure README for users and agents
