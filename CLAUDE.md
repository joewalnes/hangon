# CLAUDE.md

## Agent operations

- **Verification recipe**: build with `go build -o <scratch>/hangon .`, then drive
  the real binary in an isolated environment:
  `export HOME=<temp dir>; export HANGON_TMUX_SOCKET=hangon-<agent>-$$` then
  `hangon start process --name t -- python3 -i`, `expect ">>>"`, `sendline`,
  `screen`, `screenshot`, `stop`. A change is not verified until the real binary
  demonstrates the changed behavior. Tests: `go test ./...`; e2e: `bash test/e2e.sh`.
- **Autonomy policy**: 3 worker agents; merge to local main, gate, then push to
  origin/main. NOTE: every push to main republishes the public `latest` release
  (release.yml) — push gated, working commits only.
- **Shared singletons (never touch)**: the user's default tmux server (bare
  `tmux` with no `-L`); the production hangon server `tmux -L hangon` and its
  state in `~/.hangon` (other live agents on this machine use hangon right now);
  the installed binary `~/go/bin/hangon`. Agents always set their own
  `HANGON_TMUX_SOCKET` and `HOME` when running hangon or its tests. NEVER run
  `hangon gc` or `hangon stopall` against real state.
- **Known hazard (until the P0 gc fix lands)**: `go test ./...` runs gc
  integration tests whose machine-wide `_serve` process scan SIGKILLs real
  hangon sessions. Land the P0 state-dir scoping fix before any full-suite run;
  until then use targeted `go test -run <Test>` excluding gc integration tests.
- **Do-not-touch**: release.yml semantics (publish-on-push is deliberate);
  `demo/` (parked, see TODO P3).
- **Requests lane**: `TODO.md` (no separate ASKS.md); human-origin entries and
  regressions reported by downstream users outrank machine-generated findings
  of equal priority.
- **Setup version**: project-setup 2026-09-01.

## Bug tracking

Bugs and tasks are tracked in `TODO.md`. Use `/todo` to add entries and
`/bug-bash` to work through them. When fixing something, move its entry to
Done with the date and a one-line resolution note.

## Engineering diary

Maintain `DIARY.md` — add an entry when making significant changes,
architectural decisions, or non-obvious tradeoffs. Latest entries at top.
Write in narrative form, not bullet dumps. Focus on *why* and *context*,
not *what* (that's in the commits).

## Changelog

Update `CHANGELOG.md` with every commit. Format: grouped by date (newest
first), one bullet per change with a short description. Keep it
human-readable — no commit hashes, no authors.

## Commits

Break work into small atomic commits — one logical change per commit. Don't
bundle unrelated changes. A bug fix, a new feature, and a refactor are three
commits, not one.

## Pre-commit checks

Always run before committing:

```bash
make fmt-check    # gofmt -s -l — fails on unformatted files
go vet ./...
go test ./...
```

Do not commit if tests fail or checks report problems. Fix first. `make check`
runs all of the above plus the e2e suite. Note: `go test` requires tmux and
python3 installed; integration tests skip without tmux.

## Test-first

Before implementing a feature or fix:
1. Write a test that captures the expected behavior
2. Run it — verify it **fails** (if it passes, the test isn't testing the right thing)
3. Implement until the test passes
4. Keep a healthy mix: fast unit tests for logic, integration tests (real tmux,
   real holder processes) to validate it works in context

Don't skip step 2 — a test that never failed never caught anything. (Cautionary
tale in this repo: `mouse.go` shipped with SGR press/release inverted because it
had zero tests.)

## Documentation

Update README.md and the in-binary help text (`main.go` help strings) before
committing if the change affects the CLI interface, flags, env vars, key names,
or user-visible behavior. The README, `subcommandHelp`, and the help topics are
three copies of the same information — keep them agreeing.

## Code quality

Run `/scorecard` periodically — after completing a feature, before major PRs,
or when returning to the project after a while. Address critical findings
before moving on; file the rest in `TODO.md`.

## Evolving preferences

When the user expresses a coding preference, convention, or correction during
a session, offer to encode it into this CLAUDE.md file so it persists across
sessions.

## Mistake retrospectives

When you make a mistake (especially forgetting something the user asked for):
1. Acknowledge it directly
2. Identify the root cause — why did this happen?
3. Suggest a concrete project change to prevent recurrence (a CLAUDE.md rule,
   a pre-commit check, a test)
Don't just apologize — fix the system.

## Completing requests

When the user gives multiple requests:
1. Queue them mentally but complete ONE fully before starting the next
2. "Complete" means: code written, built, tests passing, verified working
3. Never mark something done until you've verified it end-to-end
4. If you can't complete a request in one go, say so explicitly rather than
   half-doing it and moving on
5. If requests conflict or depend on each other, state the dependency and ask
   which to prioritize

Anti-patterns to avoid:
- Starting 4 things, finishing 0
- Committing and moving on when the current thing isn't verified
- Changing behavior without testing that the behavior changed
- Saying "let me ignore X for now" — either fix it or add it to TODO.md
