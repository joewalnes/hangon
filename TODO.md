# Todo

<!-- Format: [status] P<priority> (category) Title -->
<!-- Status: [ ] open, [~] in progress, [x] done, [-] won't fix -->
<!-- Priority: P0 critical, P1 high, P2 medium, P3 low -->
<!-- Category: bug, feature, chore, docs -->

## Open

- [ ] **P1** (bug) Release pipeline is broken; no binaries or Homebrew tap actually work
  Verified via `gh api repos/joewalnes/hangon/releases` (0 releases) and
  `gh api repos/joewalnes/hangon/actions/runs` (the last several `Release`
  workflow runs on `main` all show `conclusion: failure`). The Homebrew tap
  repo (`joewalnes/homebrew-tap`, `Formula/hangon.rb`) and README's binary
  `curl` commands both point at
  `github.com/joewalnes/hangon/releases/latest/download/...`, which 404s
  with no release published. `go install github.com/joewalnes/hangon@latest`
  still works (confirmed) since Go modules don't need a GitHub release, only
  a fetchable commit. README's Install section reworded 2026-09-01 to stop
  claiming the broken paths work; fix `.github/workflows/release.yml` (find
  out why it's failing — likely a `gh release create`/token permissions
  issue) to actually restore them.

- [ ] **P2** (bug) Control socket relies on umask; no access control
  `holder.go:47-53`: socket created in `os.TempDir()` with default umask; under
  umask 002/000 any local user can inject keystrokes (= code execution). Put
  sockets under a 0700 dir (e.g. `~/.hangon/run/`) or chmod 0600 after Listen.

- [ ] **P2** (bug) Unquoted `$TMPDIR` in the pipe-pane shell string; error swallowed
  `backend_process.go:109-110`: `fmt.Sprintf("cat >> %s", fifoPath)` runs via
  `sh -c` inside tmux. Quote the path, and check the `Run()` error — a pipe-pane
  failure currently means read/expect silently return nothing forever.

- [ ] **P2** (bug) `cmd.Wait()` races pipe readers in --no-pty mode
  `backend_process.go:335-348`: Wait closes the pipes while io.Copy still reads
  (documented-incorrect os/exec usage); output tails truncated. Use a WaitGroup
  or assign the ring buffers to cmd.Stdout/Stderr directly.

- [ ] **P2** (bug) PID reuse: stop/stopall/gc signal PIDs with no identity check
  `main.go:393-407,474-491`, `gc.go:184-197`: a recycled holder PID gets
  SIGINT/SIGKILL. Verify via cmdline (the `ps` scan already exists) before
  signalling. Also consider a random suffix in tmux session names.

- [ ] **P2** (chore) RingBuffer: replace per-byte modulo loops with two copy() calls
  `ringbuffer.go:33-95`: ~1M modulos for a full `readall` (~100x slower than
  memmove), and Write holds the lock for a per-byte loop.

- [ ] **P2** (chore) Extract `resolveSession()` and `killProcessGracefully()` helpers
  The session-name resolution block is copy-pasted at 11 sites in main.go
  (~110 lines, with two divergent copies); the SIGINT→wait→SIGKILL dance exists
  4x with 4 different grace periods. Also `sessionNameForPID(pid)` for the
  `"hangon-%d"` format written out in 4 files.

- [ ] **P2** (bug) Mistyped session names silently operate on `default`
  `main.go:509-520` etc.: `hangon read typo` reads the default session and
  exits 0. The rest[0]-probe heuristic should error on unknown names.

- [ ] **P3** (chore) Split main.go (1,844 lines)
  Start with the 755-line help corpus → help.go. Longer term: internal/ packages.

- [ ] **P3** (chore) stopall is serial with a flat 500ms sleep per session
  `main.go:476-483`: 20 sessions ≥ 10s. Reuse killProcessGracefully + a WaitGroup.

- [ ] **P3** (chore) demo/ is an aborted recording
  `demo/hangon-demo.cast` captured the recorder erroring out; `record.sh:44`
  needs `stopall --force` now. Re-record or delete.

## Done

- [x] **P3** (chore) FIFOs leak on SIGKILL and gc never reaps them
  `2026-09-01`: `backend_process.go` only removes `/tmp/hangon-<pid>.fifo`
  from `closeTmux()`, which a SIGKILLed holder never reaches. Added
  `gcOrphanedFIFOs` (gc.go): scans `os.TempDir()` for names matching
  `^hangon-(\d+)\.fifo$` and removes any whose pid is not alive — no
  `--state-dir` cross-check is needed or done, since a dead pid can't
  belong to any live session anywhere (safe to remove unconditionally)
  and a live pid is always left alone regardless of state dir (removing
  a live foreign FIFO would break that session's streaming). Wired into
  `runGC`'s summary line and respects `--dry-run`. Bite-tested
  (`TestIntegration_GC_ReapsOrphanedFIFO`,
  `TestIntegration_GC_FIFOSweepRespectsDryRun` in gc_test.go): creates a
  fake FIFO named after a dead pid and one named after a live pid
  (this test process's own), confirmed both tests fail against
  unfixed code (temporarily disabling the `gcOrphanedFIFOs` call — the
  orphan survives gc, exactly as the bug describes) and pass once
  restored. Incidentally, running this on the dev machine's real `/tmp`
  swept 5,419 genuinely leaked FIFOs from prior test runs — independent
  confirmation the leak is real in the wild. Full isolated `go test
  -count=1 ./...` and `test/e2e.sh` both still green (e2e: 38/40, same
  2 pre-existing failures — "expect stale data" and "read after expect"
  — reproduced identically against unmodified main before this change,
  unrelated to gc/FIFO and out of this branch's file scope).

- [x] **P3** (chore) Windows build is broken but three Windows files are maintained
  `2026-09-01`: verified `GOOS=windows go build ./...` fails with
  `./backend_process.go:115:20: undefined: syscall.Mkfifo` (the FIFO the
  holder pipes tmux output through has no Windows equivalent in the
  current design). Deleted platform_windows.go, procscan_windows.go, and
  statelock_windows.go rather than fixing the build, since restoring
  Windows support requires redesigning the FIFO path first, which is a
  bigger change than three build-tag files. The deletion is one `git
  revert` away if someone picks that up. `go build ./...`, `go vet
  ./...`, and `go test -count=1 ./...` all clean afterward on
  darwin/amd64. Also dropped `hangon.exe` from `.gitignore` since there's
  no Windows build target anymore.

- [x] **P3** (chore) Ship THIRD_PARTY_LICENSES with release binaries
  `2026-09-01`: added `THIRD_PARTY_LICENSES` at repo root reproducing the
  full MIT text for `github.com/creack/pty` v1.1.24 and the ISC text for
  `nhooyr.io/websocket` v1.8.17, copied byte-for-byte from
  `$GOMODCACHE/<module>@<version>/LICENSE{,.txt}` (diffed against the
  cached files to confirm exact match) with a header naming dependency,
  version, and license type. Ran `go mod tidy`, which dropped the
  incorrect `// indirect` markers on both deps (they're directly
  imported). `go build ./...` still clean. README's License section now
  points at the new file. Not done: migrating off `nhooyr.io/websocket`
  to its maintained fork `github.com/coder/websocket` — noted as a
  separate decision, not a chore.

- [x] **P1** (chore) No CI runs any tests
  `2026-09-01`: added `.github/workflows/ci.yml`, on `pull_request` and on
  `push` to `main`. Ubuntu runner: checkout, `actions/setup-go` from
  `go.mod`'s version, `gofmt -s -l .` gate (fails with the file list on
  drift — bite-tested locally by temporarily un-formatting `tmux.go`,
  confirmed the check fails and reverting makes it pass again), `go vet
  ./...`, `go build ./...`, install `tmux`+`python3` via `apt-get`, then
  `go test ./...` with `HANGON_TMUX_SOCKET=hangon-ci` set explicitly (the
  runner is already isolated, but this keeps CI's tmux socket
  self-documenting and matches the dedicated-socket convention used
  everywhere else). Verified every step locally by running the exact
  commands in order in an isolated worktree: gofmt clean, vet clean, build
  clean, `go test ./...` passed (`ok ... 20.638s`) with no impact on the
  default tmux server or the production `tmux -L hangon` server (checked
  before/after). Deliberately did NOT add `test/e2e.sh` to CI yet: even
  though the e2e socket fix (see above) got it to 40/40 locally, adding
  e2e to CI is a separate decision (slower, needs its own review of
  flakiness/timing under CI hardware) and wasn't asked for here — tracked
  as a possible follow-up, not done in this change. Deliberately did NOT
  touch `.github/workflows/release.yml` — its publish-on-every-push-to-main
  behavior is a documented deliberate choice (do-not-touch), so `ci.yml`
  was added alongside it as a second, independent workflow rather than
  gating release on tests.

- [x] **P1** (bug) e2e gc test broken by the dedicated tmux socket change
  `2026-09-01`: fixed. `test/e2e.sh` now exports `HANGON_TMUX_SOCKET`
  (defaulting to `hangon-e2e-$$`, respecting a caller-provided value) at the
  top of the script and routes every direct tmux invocation (teardown's
  orphan scan at the old :51-52, and the gc test's `has-session` checks at
  the old :603/:620) through a `tmx()` wrapper (`tmux -L
  "$HANGON_TMUX_SOCKET"`) instead of bare `tmux`, matching what the hangon
  binary itself already does (tmux.go). Added a `tmx kill-server` to
  teardown so the dedicated socket doesn't outlive the run. Reproduced the
  bug first, standalone and via the unmodified script (with
  `HANGON_TMUX_SOCKET` exported for the hangon binary but the script's own
  bare `tmux has-session` still missing the session it put on the dedicated
  socket): `test_gc_reaps_crashed_holder` failed with "test setup invalid"
  in both cases (39/40 passed). After the fix: 40/40 passed, confirmed the
  user's default `tmux` server and the production `tmux -L hangon` server
  were untouched by the run (checked via `tmux ls` / `tmux -L hangon ls`
  before and after). Audited the rest of the 997-line script for other
  un-isolated state: `setup()` already exports a per-run temp `HOME`
  (`mktemp -d`), which covers `~/.hangon` state-dir isolation (`state.go`
  resolves via `os.UserHomeDir()`, i.e. `$HOME`) — no other un-isolated
  touches found; `fifoPath`/control-socket paths use `os.TempDir()` but are
  PID/session-name-scoped so they don't collide across runs.

- [x] **P1** (bug) Vanished tmux session reported as exit code 0
  `2026-09-01`: the poll goroutine closed `done` on a failed `has-session`
  check without setting `exitCode`, leaving it at its zero value, so
  `hangon wait` printed "exit code: 0" for a session killed externally.
  Fixed: (a) `set-option remain-on-exit`'s error is now checked and fails
  `start` loudly (a swallowed failure here would silently reproduce this
  exact bug on any normal exit); (b) a vanished session now sets
  `exitCode = -1` plus a `"session terminated externally: exit status
  unknown"` error, which `hangon wait` surfaces via its existing
  `fatal()` path (stderr message, exit 2) — never "exit code: 0"; (c) the
  dead `&exec.ExitError{}` zero-value assignment is deleted. As a natural
  side effect of rewriting this block, also merged the `#{pane_dead}` and
  `#{pane_dead_status}` polls into one `display` call, which resolves the
  separate "Perf: merge the pane_dead poll" P2 below. Reproduced pre-fix
  (`hangon wait` on an externally-killed session printed "exit code: 0",
  exit 0) and added `TestIntegration_VanishedSessionExitCode` (fails on
  unfixed code, passes after; also asserts real exit codes 0 and 7 still
  propagate). Not done: `hangon status` still doesn't report exit-code
  information at all (only holder-process liveness) — out of scope, this
  bug was specifically about `wait`.
- [x] **P2** (chore) Perf: merge the pane_dead poll into one tmux call
  `2026-09-01`: resolved as a side effect of the "Vanished tmux session
  reported as exit code 0" fix above, which was already rewriting this
  exact poll loop — `has-session` + two separate `display` calls per tick
  is now `has-session` + one `display -p '#{pane_dead},#{pane_dead_status}'`.
  Backoff on the poll interval was not added (separate concern, not part
  of either fix above).
- [x] **P1** (bug) Output printed before pipe-pane activates is lost
  `2026-09-01`: tmux ran the pane's command the instant `new-session`
  returned, but `pipe-pane`/the FIFO reader weren't wired up until several
  more tmux round-trips later, and `pipe-pane` never replays backlog — so
  output produced in that gap (a one-shot command's entire output, or a
  longer command's startup banner) was gone forever. Fixed by starting the
  pane behind a `read`-based gate (`read -r _hangon_start; exec
  <command>`) released via `send-keys "Enter"` only after remain-on-exit,
  pipe-pane, and the FIFO reader are all live, so no output can be
  produced before it's being captured. `exec` keeps `pane_pid` pointing at
  the real command, matching prior `TargetPID` behavior. Reproduced
  deterministically pre-fix (3/3 runs of a one-shot `echo hi` produced
  empty `readall`; `expect` on a `sh -c 'echo BANNER; ...'` startup
  banner timed out every time run immediately after start) and added
  `TestIntegration_ImmediateOutputNotLost` (fails on unfixed code, passes
  after) — see backend_process.go's `startWithTmux`.
- [x] **P2** (docs) README flagship examples use `keys "q"` — bare letters aren't valid keys
  `2026-09-01`: every claim re-verified against a real build of the binary
  (isolated `HOME`/`HANGON_TMUX_SOCKET`), not just rewritten from the audit
  notes. `README.md:74,197-206` and `topicScreenshots`/`topicKeys` in
  `main.go`: `hangon keys "q"`/`"i"`/`": w enter"` all fail with `unknown
  key` — switched to `send`/`sendline`, and the full corrected vim sequence
  (insert via `send "i"`, type text, `keys escape`, `send ":w"` + `keys
  enter`, screenshot, `send ":q"` + `keys enter`) was run end-to-end
  against real vim: file saved with the typed content, screenshot
  produced, vim exited cleanly. Added mouse-click/-drag/-scroll to the
  README's I/O table with examples, each smoke-tested against a real
  session. Fixed the stale README/topicKeys key list (was missing
  shift-*/alt-*/ctrl-arrow/ctrl-space, added 2026-04-22); topicKeys now
  defers to `hangon keys --help` instead of duplicating, and a new test
  (`keys_test.go`: `TestHelpKeysDocumentedExactlyMatchKeyMaps`,
  `TestKeyMapsHaveIdenticalKeySets`) parses that help text and keeps it,
  keyMap, and tmuxKeyMap from ever silently drifting apart again
  (guard-bite proof: manually removed a key from each of the three sources
  in turn, confirmed the corresponding test failed with a clear message,
  restored). Fixed Go version claim (1.21+ → 1.25+, matches `go.mod`).
  Reworded the agent-facing `stopall` guidance (README:14-17), which had
  it backwards — telling agents to always pass `--force`, defeating the
  safety preview — to preview-first-then-`--force`-once-confirmed. The gc
  "only acts on unreferenced resources" claim needed no change: the P0 gc
  bug referenced by this ticket was already fixed (see below), so that
  claim is now true. Added `hangon ls --help` (missing from
  `subcommandHelp`, so the `ls` alias 404'd on `--help` with exit 2, even
  though `ls` itself worked). Made `HANGON_TIMEOUT` actually affect
  `expect`'s default timeout (previously documented but dead: `runExpect`
  hardcoded 30 and always sent a non-zero timeout to the holder, so the
  server-side env-var-aware fallback in `doExpect` was never reached);
  chose to fix the behavior (not just the doc) since it's the smaller,
  more honest fix — confirmed `HANGON_TIMEOUT=2s` against a never-matching
  pattern now times out in ~2s instead of 30s. Investigated the Homebrew
  tap claim via `gh api`: `joewalnes/homebrew-tap` and its `Formula/hangon.rb`
  do exist and are well-formed, but depend on a GitHub release that
  doesn't exist (0 releases; the `Release` workflow's last several runs on
  `main` all failed) — reworded README's Install section to lead with `go
  install .../hangon@latest` (confirmed working) and state the Homebrew/
  binary-download situation honestly instead of leaving it as an unqualified
  "just run this" claim; filed the underlying release-pipeline breakage as
  a new P1 bug below rather than fixing it here (out of scope for a docs
  pass). Not changed: `demo/record.sh` and `demo/hangon-demo.cast`
  (untracked, not part of this branch) — already use `send "q"` correctly,
  nothing to fix there.
- [x] **P0** (bug) `hangon gc` kills sessions belonging to other state directories
  `2026-09-01`: fixed by scoping `gcOrphanedServeProcesses`/`gcOrphanedTmuxSessions`
  to only act on a `_serve` process (or its tmux session) whose own `--state-dir`
  argument matches the state dir `gc` is running against, parsed from the cmdline
  `listServeProcesses` already returns.
- [x] **P0** (bug) SGR mouse press/release inverted — clicks dropped, scroll
  is a no-op (2026-09-01). `sgrMouseSeq` in `mouse.go` had the suffix mapping
  backwards: `release=false` produced `'m'` and `release=true` produced `'M'`,
  the opposite of the xterm ctlseqs SGR mode (1006) rule that `'M'` is
  press/motion and `'m'` is release. Every click sent release-then-press
  (most TUIs drop the unmatched release); wheel events (press-only, no
  release) were emitted as `'m'`, making `mouse-scroll` a complete no-op.
  Fixed by swapping the two branches in `sgrMouseSeq`; call sites already
  passed `release` with the correct intent, so no other code changed.
  Added 24 golden byte-sequence tests in `mouse_test.go` (`sgrMouseSeq`,
  `mouseModifiers`, `buttonNumber`, `mouseClick`, `mouseDrag`, `mouseScroll`)
  covering every button, shift/alt/ctrl combos, single/double click, drag
  motion (`+32` flag), and scroll up/down/repeat — confirmed failing against
  the unfixed code, then green after the fix. Verified end-to-end in an
  isolated tmux socket: `hangon start process` a Python script that enables
  `\x1b[?1006h\x1b[?1002h` and echoes raw stdin, then drove it with
  `hangon mouse-click/-drag/-scroll` — received bytes matched the golden
  sequences exactly (press `M` before release `m`; drag motions carry `+32`
  and stay `M`-coded; scroll notches are repeated bare `M` events).
  Not touched: `sendMouseSeqs`'s double/triple-click delay timing (separate
  concern, not part of the M/m inversion), and mouse-move/motion-only events
  (hangon has no CLI command for bare hover motion without a held button).
- [x] **P1** (bug) No way to resize a running session's terminal — root cause was
  misdiagnosed as a "raw PTY rewrite"; actual cause is the dedicated-tmux-socket
  move (`21ddf4e`)
  **Corrected diagnosis:** sessions are still tmux-backed — nothing was
  "rewritten to raw PTY". `21ddf4e` ("Run tmux on a dedicated server socket")
  moved every hangon `process` session from the user's *default* tmux server
  onto its own dedicated one (`tmux -L hangon`), so `hangon status` and
  `tmux -L hangon list-sessions` both show `hangon-<holderPID>` sessions
  exactly as before — they just don't show up under a bare `tmux
  list-sessions` anymore, which is what made this look like the sessions had
  disappeared. Downstream, at least one project (zepto's QA suite,
  `qa/lib/qa-helpers.sh`'s `qa_resize_window`) called
  `tmux resize-window -t "hangon-$PID" -x COLS -y ROWS` with no `-L` flag,
  i.e. against the *default* server — which has never had hangon's sessions
  on it since `21ddf4e` shipped. tmux's `resize-window` against a
  nonexistent target reports "session not found" without raising in most
  wrapper scripts that don't check the exit code, which is why this broke
  silently with no error and no deprecation notice.
  **Fix:** added a native `hangon resize [SESSION] --cols N --rows N`
  command (wired through the JSON protocol as `MethodResize` /
  `ResizeParams`) so callers no longer need to reach around hangon into
  tmux at all. For tmux-backed sessions it runs `tmux resize-window` via
  the existing `tmuxCmd`/`tmuxExact` helpers (never a bare
  `exec.Command("tmux", ...)`, so it can't leak onto the wrong server) and
  updates `pb.tmuxCols`/`pb.tmuxRows` so `screen` and `screenshot` reflect
  the new geometry. For the legacy raw-PTY fallback (used only when tmux
  isn't installed at all) it calls `pty.Setsize`, which raises SIGWINCH via
  the same `TIOCSWINSZ` ioctl a real terminal emulator uses, and resizes
  the embedded `Terminal` grid. `tcp`/`ws`/`macos` sessions have no
  terminal grid and return a clear "not supported by this backend type"
  error rather than silently no-op'ing. Also added `--cols`/`--rows` to
  `hangon start` for setting the initial size (default unchanged: 80x24).
  Documented in `README.md` (command table + a new note that external
  tooling must target `tmux -L hangon`, not the default server),
  `CHANGELOG.md`, and `hangon resize --help` / `hangon start --help`.
- [x] **P1** (bug) `expect` misses patterns split across FIFO read chunks — 2026-09-01
  `holder.go:310-362` + `ringbuffer.go:51`: each loop iteration matched only
  the newest `RingBuffer.ReadFrom` chunk in isolation, so a pattern split
  across two FIFO reads (`">>"` then `"> "`) never matched, and once a
  later chunk did match, earlier chunks' bytes were silently dropped from
  the result. Fixed by extracting `expectFromBuffer` (holder.go), which
  accumulates chunks into a rolling `[]byte` capped at the ring buffer's
  own size (`RingBuffer.Size()`, new method) and matches the regex against
  the accumulation as a whole. Shipped semantics: on match, `Result`
  contains everything from the expect's starting cursor through the end of
  the match (no pre-match loss); the session read cursor advances only to
  just past the match end, so bytes after the match remain readable by a
  later `read`/`expect`. Also hoisted the per-iteration `time.After` into a
  single `time.NewTimer` for the whole call (was leaking one unfired timer
  per loop iteration on chatty output), and collapsed the duplicated
  match-and-advance code in the timeout path into the one loop. Not fixed:
  the rare case where the ring buffer overwrites bytes between polls
  (producer outruns 1MB between iterations) still loses those specific
  bytes — inherent to the fixed-size ring buffer, same limit a plain `read`
  already has; `expectFromBuffer` detects the discontinuity and
  resynchronizes instead of splicing non-contiguous data. Tests:
  `holder_test.go` (6 new tests covering split-across-two-writes,
  split-across-many-byte-writes, pre-match-bytes-preserved,
  timeout-when-absent, anchored patterns, and bounded accumulation);
  `integration_test.go:78` updated from "read may be empty" (old lossy
  behavior) to assert the next prompt is still visible after `expect`.
