# Todo

<!-- Format: [status] P<priority> (category) Title -->
<!-- Status: [ ] open, [~] in progress, [x] done, [-] won't fix -->
<!-- Priority: P0 critical, P1 high, P2 medium, P3 low -->
<!-- Category: bug, feature, chore, docs -->

## Open


- [ ] **P2** (bug) Control socket relies on umask; no access control
  `holder.go:47-53`: socket created in `os.TempDir()` with default umask; under
  umask 002/000 any local user can inject keystrokes (= code execution). Put
  sockets under a 0700 dir (e.g. `~/.hangon/run/`) or chmod 0600 after Listen.

- [ ] **P2** (bug) `cmd.Wait()` races pipe readers in --no-pty mode
  `backend_process.go:335-348`: Wait closes the pipes while io.Copy still reads
  (documented-incorrect os/exec usage); output tails truncated. Use a WaitGroup
  or assign the ring buffers to cmd.Stdout/Stderr directly.

- [ ] **P2** (docs) README flagship examples use `keys "q"` — bare letters aren't valid keys
  `README.md:74,197-206`, `topicScreenshots` in main.go: `keys "q"` / `"i"` /
  `": w enter"` all fail with "unknown key" (should be `send "q"`, as
  demo/record.sh correctly does). While in there: add mouse-click/drag/scroll to
  the README (currently absent), fix the stale key list, Go version claim
  (1.21 vs go.mod 1.25), the backwards stopall advice (README:14-17), and the
  gc "only acts on unreferenced resources" claim (false until the P0 is fixed).

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

- [ ] **P3** (chore) Windows build is broken but three Windows files are maintained
  `GOOS=windows go build` fails (`syscall.Mkfifo`, backend_process.go:87).
  Delete platform_windows.go/procscan_windows.go/statelock_windows.go or fix the build.

- [ ] **P3** (chore) FIFOs leak on SIGKILL and gc never reaps them
  `backend_process.go:85-88,273-279`: `/tmp/hangon-<pid>.fifo` removed only in
  closeTmux(). Add a FIFO sweep to gc.

- [ ] **P3** (chore) Ship THIRD_PARTY_LICENSES with release binaries
  Static binaries embed creack/pty (MIT) and nhooyr.io/websocket (ISC); both
  require the notice to accompany distribution. Also: nhooyr.io/websocket is
  deprecated upstream (moved to github.com/coder/websocket); go.mod marks both
  deps `// indirect` wrongly (run `go mod tidy`).

- [ ] **P3** (chore) Split main.go (1,844 lines)
  Start with the 755-line help corpus → help.go. Longer term: internal/ packages.

- [ ] **P3** (chore) stopall is serial with a flat 500ms sleep per session
  `main.go:476-483`: 20 sessions ≥ 10s. Reuse killProcessGracefully + a WaitGroup.

- [ ] **P3** (chore) demo/ is an aborted recording
  `demo/hangon-demo.cast` captured the recorder erroring out; `record.sh:44`
  needs `stopall --force` now. Re-record or delete.

## Done

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
- [x] **P2** (bug) Unquoted `$TMPDIR` in the pipe-pane shell string
  `2026-09-01`: `pipePaneCmd` was built with `fmt.Sprintf("cat >> %s",
  pb.fifoPath)`, interpolated unquoted into a string tmux runs via `sh -c`.
  `pb.fifoPath` comes from `os.TempDir()`, which honors `$TMPDIR` — not a
  fixed trusted literal — so a `TMPDIR` containing a space silently
  misdirects output (reproduced: `cat >> repro/a b/hangon-test.fifo` under
  `sh -c` created an empty file `repro/a` and errored trying to read
  nonexistent `b/hangon-test.fifo` as a command argument, silently
  discarding stdin) and one containing shell metacharacters would be
  command injection. Fixed with a new `shellSingleQuote` helper (POSIX
  single-quote escaping: close-quote, escaped literal quote, reopen-quote)
  used at the `pipe-pane` call site. `TestShellSingleQuote_Escaping` proves
  the helper round-trips through a real shell for spaces, `'`, `$(...)`,
  `;`, backticks; `TestPipePaneCmd_QuotesFifoPathWithSpace` is the
  behavioral proof — builds the real `pipePaneCmd` string for a FIFO path
  containing a space and confirms via `sh -c` that `cat` writes to the
  exact intended file. The pre-existing `pipe-pane` `Run()` error check
  (checked by prior work) was left as-is.
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
