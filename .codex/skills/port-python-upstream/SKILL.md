---
name: port-python-upstream
description: Use when maintaining this repository's Go main branch while tracking Python upstream changes, checking pending upstream commits, porting Python behavior to Go, renaming go/main/python branches, or using ours merges to mark Python commits as absorbed.
---

# Port Python Upstream

## Core Model

Use Git history as the source of truth for upstream porting state:

- `main` is the Go product branch and must not track `upstream/main`.
- `python` is the Python upstream mirror and should track `upstream/main`.
- `python` should stay clean: update it with `git merge --ff-only upstream/main`.
- `main..python` lists Python upstream commits not yet marked as absorbed by Go.
- After porting behavior into Go, use an `ours` merge on `main` to mark the Python commit or range as absorbed without copying Python files into the Go tree.

## Required Safety Checks

Before branch renames, pushes, or merges:

1. Check `git status --short` and do not overwrite user work.
2. Check `git remote -v`, `git branch -vv`, and the current branch.
3. Distinguish local branch changes from remote/default-branch changes.
4. Ask before force-pushing, deleting branches, changing remote defaults, or rewriting published history.

Do not merge Go commits into `python`. Do not use regular merge from `python` into `main` for porting markers.

## Initial Branch Shape

When converting the current repository layout:

```powershell
git branch -m main python
git branch -m go main

git switch python
git branch --set-upstream-to=upstream/main python

git switch main
git branch --unset-upstream
```

Then verify:

```powershell
git branch -vv
git log --oneline main..python
```

Remote changes are separate. If the user wants `origin/main` to become Go `main`, confirm first, then use a guarded push such as `--force-with-lease` only when appropriate.

## Routine Sync Workflow

Update the Python mirror:

```powershell
git fetch upstream
git switch python
git merge --ff-only upstream/main
```

Find pending upstream work:

```powershell
git switch main
git log --oneline main..python
```

Classify each pending commit:

| Class | Action |
| --- | --- |
| Go behavior needed | Read Python diff, write Go tests, port behavior, run Go tests |
| Python-only runtime | Usually skip implementation, then mark absorbed only if intentionally irrelevant |
| Docs/assets/config still applicable | Cherry-pick or manually port if safe |
| Unknown | Inspect diff before deciding |

## Porting Workflow

For each commit or contiguous range:

1. Inspect Python diff from `python`.
2. Identify the Go module that owns the behavior.
3. Add or update Go tests first when behavior changes.
4. Implement the Go equivalent without importing Python-only runtime code.
5. Run targeted tests, then `go test ./...` when practical.
6. Commit the Go migration on `main`.
7. Mark the Python commit as absorbed with an `ours` merge on `main`.

Example:

```powershell
git switch main
# edit Go code and tests
go test ./app/products/web/admin ./app/dataplane/reverse/protocol -count=1
git commit -m "fix(auth): port upstream NSFW settings flow"

git merge --no-ff -s ours <python-sha> -m "chore(upstream): mark Python upstream <python-sha> as absorbed"
git log --oneline main..python
```

`-s ours` records the merge relationship but keeps the Go tree content from `main`.

## Important Limitation

Git merge ancestry is transitive. If you `ours` merge a later Python commit, Git also treats its ancestors as merged. Therefore:

- Prefer processing pending Python commits in order.
- If skipping a middle commit, explicitly decide whether its ancestors should also be considered absorbed.
- For a batch, only use an `ours` merge at the end of a contiguous range when every commit in that range has been ported or intentionally skipped.

## Common Mistakes

- Regular-merging `python` into `main`: this can bring Python runtime files into the Go branch.
- Updating `python` with non-fast-forward local commits: this destroys its value as an upstream mirror.
- Treating `main..python` as "not implemented" after doing local Go code but before the `ours` marker: the implementation and marker must both happen.
- Marking a commit absorbed before tests show the Go behavior matches.
