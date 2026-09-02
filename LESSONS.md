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
