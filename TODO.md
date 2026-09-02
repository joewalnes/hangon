# Todo

<!-- Format: [status] P<priority> (category) Title -->
<!-- Status: [ ] open, [~] in progress, [x] done, [-] won't fix -->
<!-- Priority: P0 critical, P1 high, P2 medium, P3 low -->
<!-- Category: bug, feature, chore, docs -->

## Open

- [ ] **P1** (bug) `expect` misses patterns split across FIFO read chunks
  `holder.go:329-361` + `ringbuffer.go:51`: each iteration searches only the
  newest chunk and consumes it, so `">>> "` arriving as `">>"` + `"> "` never
  matches. Also destroys pre-match output on success. Fix: accumulate into a
  rolling buffer; hoist the per-iteration `time.After` (timer leak) while there.

- [ ] **P1** (chore) No CI runs any tests
  `.github/workflows/` has only release.yml (which publishes on every push to
  main). Add a workflow: `gofmt -l`, `go vet ./...`, `go test ./...`.

- [ ] **P1** (bug) Output printed before pipe-pane activates is lost
  `backend_process.go:95-118`: tmux starts the command at `new-session`;
  pipe-pane is wired up afterwards. Fast commands appear to produce nothing and
  `expect` times out on startup banners.

- [ ] **P1** (bug) No way to resize a running session's terminal since the tmux-per-session rewrite
  The `2026-09-01 05:05` dev build dropped the one-tmux-window-per-session model
  (`hangon-<holderPID>`) in favor of a raw PTY + Unix socket per `process` session
  — confirmed via `tmux list-sessions` (no `hangon-<PID>` windows exist for new
  sessions) and `hangon status`, which now shows a `Socket:` path instead of a
  tmux target. There is no replacement: `hangon --help`, `hangon start
  process --help`, and `hangon help topics` have no resize/size/cols/rows
  command or flag anywhere. Downstream, at least one project (zepto's QA suite,
  `qa/lib/qa-helpers.sh`'s `qa_resize_window`) depended on
  `tmux resize-window -t "hangon-$PID" -x COLS -y ROWS` for width-dependent
  tests (progressive-disclosure UI, narrow-terminal edge cases) — that broke
  silently the moment the new build was installed, with no error, no
  deprecation notice, just resize calls doing nothing. Needs either a native
  `hangon resize [SESSION] --cols N --rows N` command (set the PTY size via
  `ioctl(TIOCSWINSZ)` and signal the child, same as any terminal emulator
  would on its own resize), or `--cols`/`--rows` at `hangon start` time if
  runtime resize isn't planned — but *something*, since terminal-size-dependent
  testing is a real use case the old tmux-backed model supported for free.

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
