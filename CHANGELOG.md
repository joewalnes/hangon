# Changelog

## 2026-09-01

- Fix `expect` missing patterns split across FIFO read chunks and losing
  pre-match output: `doExpect` used to match each new chunk from
  `RingBuffer.ReadFrom` in isolation, so a pattern arriving split across
  two reads (e.g. `>>` then `> `) would never match, and once a later
  chunk did match, any earlier chunk's bytes were silently discarded from
  the result. `expect` now accumulates chunks into a rolling buffer
  (capped at the ring buffer's own size, so memory stays bounded even
  against a chatty process that never matches) and matches against the
  accumulation as a whole. Result now contains everything from the
  expect's starting cursor through the end of the match — no pre-match
  bytes are dropped — and the session's read cursor advances only to just
  past the match, so bytes after it remain available to a later `read`.
  Also hoists the per-iteration `time.After` timer (was allocating one
  every loop iteration) into a single `time.NewTimer` for the whole call,
  and collapses the duplicated match-and-advance code in the timeout path
  into one path.
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
