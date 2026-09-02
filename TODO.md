# Todo

<!-- Format: [status] P<priority> (category) Title -->
<!-- Status: [ ] open, [~] in progress, [x] done, [-] won't fix -->
<!-- Priority: P0 critical, P1 high, P2 medium, P3 low -->
<!-- Category: bug, feature, chore, docs -->

## Open


- [ ] **P1** (bug) e2e gc test broken by the dedicated tmux socket change
  `test/e2e.sh:51-52,603,620` calls bare `tmux` (default server); hangon's
  sessions now live on `tmux -L hangon`. The gc test always fails its setup
  check, and the teardown scans the user's personal server. Set
  `HANGON_TMUX_SOCKET` in e2e.sh and route its tmux calls through `-L`.

- [ ] **P1** (chore) No CI runs any tests
  `.github/workflows/` has only release.yml (which publishes on every push to
  main). Add a workflow: `gofmt -l`, `go vet ./...`, `go test ./...`.

- [ ] **P1** (bug) Output printed before pipe-pane activates is lost
  `backend_process.go:95-118`: tmux starts the command at `new-session`;
  pipe-pane is wired up afterwards. Fast commands appear to produce nothing and
  `expect` times out on startup banners.

- [ ] **P1** (bug) Vanished tmux session reported as exit code 0
  `backend_process.go:140-144`: `has-session` failure closes `done` with
  exitCode 0, so `hangon wait` returns success for a command that failed.
  Related: swallowed `set-option remain-on-exit` error (`:106`) causes exactly
  this. Also delete the dead zero-value `&exec.ExitError{}` at `:157`.

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

- [ ] **P2** (chore) Perf: merge the pane_dead poll into one tmux call
  `backend_process.go:137-166`: `has-session` + `display` every 500ms per
  session (~2.8% of a core each). One `display -p '#{pane_dead},#{pane_dead_status}'`
  is a 3x cut; consider backoff.

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
