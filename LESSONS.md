# Lessons

Append-only ledger of things that actually bit during agent work on this
repo. Before adding an entry, look for an existing one to sharpen instead.

## Env-var isolation evaporates; put the guard in the invocation itself

**What happened:** Two near-misses in one go-team run. Worker A ran
Read/Edit against the shared checkout instead of its worktree (caught by
`git status` before commit). Worker D exported `HANGON_TMUX_SOCKET` in one
shell, then a later fresh shell silently dropped it and one test run used
the production tmux socket for a few seconds.

**What it cost:** Nothing, both times — caught by self-review and the
production session happened to survive. Pure luck plus honesty.

**The rule that would have prevented it:** Isolation that lives in ambient
state (an exported env var, a cd, "remember to use the worktree") does not
survive shell restarts or habit. Put the guard inside every single
invocation: `HANGON_TMUX_SOCKET=x cmd ...` inline on each command line,
absolute worktree paths in every file operation, and a gate that refuses
to act when the precondition doesn't hold (the merge gate's branch check
is the model). Instructions are advisory; per-invocation structure binds.

**Scope:** general
