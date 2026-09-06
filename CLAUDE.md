# CLAUDE.md

## Agent operations

- **Verification recipe**: build with `go build -o <scratch>/hangon .`, then drive
  the real binary in an isolated environment:
  `export HOME=<temp dir>; export HANGON_TMUX_SOCKET=hangon-<agent>-$$` then
  `hangon start process --name t -- python3 -i`, `expect ">>>"`, `sendline`,
  `screen`, `screenshot`, `stop`. A change is not verified until the real binary
  demonstrates the changed behavior. Tests: `go test ./...`; e2e: `bash test/e2e.sh`.
- **Autonomy policy**: 3 worker agents; merge to local main, gate, then push to
  origin/main. NOTE: the `Release` workflow is currently BROKEN (see do-not-touch
  below), so a push to main does not actually publish anything — but treat pushes
  as if it did (push gated, working commits only) since fixing it is an open P1.
- **Shared singletons (never touch)**: the user's default tmux server (bare
  `tmux` with no `-L`); the production hangon server `tmux -L hangon` and its
  state in `~/.hangon` (other live agents on this machine use hangon right now);
  the installed binary `~/go/bin/hangon`. Agents always set their own
  `HANGON_TMUX_SOCKET` and `HOME` when running hangon or its tests. NEVER run
  `hangon gc` or `hangon stopall` against real state.
- **Test isolation**: `go test ./...` is safe to run — gc integration tests are
  scoped to their own state dir and per-run `HANGON_TMUX_SOCKET`, and env
  overrides are applied via `envWith` so no test touches the real `~/.hangon`.
  (The earlier machine-wide-SIGKILL hazard was fixed 2026-09-01; the gc scan is
  now `--state-dir`-scoped.)
- **Do-not-touch `release.yml` — but it is broken, and the fix is an open P1
  requiring a decision, not a mechanical edit.** Root cause (verified via `gh`):
  the repo ruleset restricts ref creation and releases are immutable, so the
  workflow's delete-and-recreate-`latest` strategy fails on every run — zero
  releases exist, and the Homebrew tap / `curl` install paths 404. Restoring it
  means changing the publish semantics (e.g. immutable per-commit tags instead of
  a mutable `latest`), which is a product decision. Don't touch `release.yml`
  speculatively; see TODO.md's release P1.
- **Do-not-touch**: `demo/` (parked, see TODO P3).
- **Requests lane**: `TODO.md` (no separate ASKS.md); human-origin entries and
  regressions reported by downstream users outrank machine-generated findings
  of equal priority.
- **Setup version**: project-setup 2026-09-01. **Last scorecard**: 2026-09-06
  (Code B-, Agent-readiness C+); the fix wave that followed closed the two P0
  regressions (mouse/ax-find resolution, PID-reuse teardown) and the test-env
  isolation gap.

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

Update README.md and the in-binary help text (`help.go` — the
`subcommandHelp` map and the help topics; extracted from main.go in
45374e6) before committing if the change affects the CLI interface, flags,
env vars, key names, or user-visible behavior. The README, `subcommandHelp`,
and the help topics are three copies of the same information — keep them
agreeing (`TestHelpKeysDocumentedExactlyMatchKeyMaps` guards the key list
against the code, but the rest is by hand).

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
