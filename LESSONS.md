# Lessons

Append-only ledger of things that actually bit during agent work on this
repo. Before adding an entry, look for an existing one to sharpen instead.

## Env-var isolation evaporates; put the guard in the invocation itself

**What happened:** Three occurrences in one go-team run. Worker A ran
Read/Edit against the shared checkout instead of its worktree (caught by
`git status` before commit, self-reverted). Worker D exported
`HANGON_TMUX_SOCKET` in one shell, then a later fresh shell silently
dropped it and one test run used the production tmux socket for a few
seconds. Worker F wrote 87 lines of implementation into the shared
checkout's backend_process.go while its own worktree sat untouched —
caught by a *different* worker's routine `git status`, preserved as a
patch by the foreman, and sent back.

**What it cost:** Nothing, three times — self-review, cross-checking
between workers, and a clean-checkout sweep each caught it before a
commit. Pure luck compounding; the fourth time won't announce itself.

**The rule that would have prevented it:** Isolation that lives in ambient
state (an exported env var, a cd, "remember to use the worktree") does not
survive shell restarts or habit. Put the guard inside every single
invocation: `HANGON_TMUX_SOCKET=x cmd ...` inline on each command line,
absolute worktree paths in every file operation, and a gate that refuses
to act when the precondition doesn't hold (the merge gate's branch check
is the model). Instructions are advisory; per-invocation structure binds.

**Scope:** general

## A finished report is not a finished branch — check for the commit hash

**What happened:** Worker G delivered a detailed, fully-verified report
(every claim checked out behaviorally) but never ran `git commit` — its
branch tip was still the merge-base. `git merge` then reported
"Already up to date" and would have silently merged nothing while the
foreman recorded the work as landed. Separately, the foreman's own retry
ran `git merge` inside the worker's worktree (a lingering `cd` in a
compound command), producing a second vacuous "success."

**What it cost:** Nothing — the missing commit hash in the report and
the empty merge output were both noticed. Either alone could have
shipped a phantom "landed" line to the human.

**The rule that would have prevented it:** Require a commit hash in
every worker report, and treat a merge whose output lists no files as a
failure to investigate, never a success. After any merge, confirm from
the repo root that HEAD moved and the expected paths changed
(`git diff --stat ORIG_HEAD..HEAD`).

**Scope:** general

## The gate only proves what it runs

**What happened:** The expect-semantics rework merged cleanly through a
gate that ran `go test ./...` but not `test/e2e.sh`. Two e2e tests had
been relying on terminal echo duplicating their marker strings, and the
new consume-through-match semantics broke them — discovered only when a
later worker happened to run e2e for unrelated reasons (38/40 on main).

**What it cost:** Two pushes shipped with a broken e2e suite; roughly an
hour of latency before detection, plus foreman time to triage.

**The rule that would have prevented it:** Every check the project
considers part of "green" belongs in the merge gate, or it will drift.
When adding a new suite (or discovering one), add it to the gate the
same day. Corollary: when a gate passes but a suite outside it fails,
treat the gate as the defect too, not just the code.

**Scope:** general
