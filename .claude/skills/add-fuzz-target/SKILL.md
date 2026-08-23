---
name: add-fuzz-target
description: Add a Go fuzz target to envoke and wire it into CI. Use when writing a new Fuzz* function — the target does not run in CI until it is registered in .dagger, and nothing detects the omission.
---

# Adding a fuzz target

A `Fuzz*` function that only exists in a `_test.go` file runs its seed corpus
as an ordinary test and is **never actually fuzzed**. Registering it is a
separate, manual step.

## The two files

1. **The target itself**, in the package's `_test.go` (today: only
   `internal/config/fuzz_test.go`). Give it seeds via `f.Add` — every seed
   runs under `dagger call -m .dagger test` as a normal test case, so a seed
   that reproduces a past bug is also a regression test.

2. **`.dagger/main.go` — the `fuzzTargets` slice**, as a `{pkg, name}` pair.
   It is listed explicitly rather than discovered precisely so that forgetting
   it is a visible omission; nothing fails if you skip this, the target just
   silently never gets fuzzed.

## What is worth fuzzing here

Input that is unstructured, attacker-influenced, and decides what shell code
runs: the hand-rolled config parser and pattern compilation. `FuzzCompilePattern`
found the `)|(` case, where a pattern wrapped into a top-level alternation
outside the `^(?:...)$` anchors and matched every directory on the machine —
that seed is still in the corpus.

## Running it

`dagger call -m .dagger fuzz` is a short time-boxed burst per target, and is
deliberately **not** a `+check`: a fixed-duration run in every CI job would tax
each build for a low hit rate. Give it longer for a substantial change:
`dagger call -m .dagger fuzz --fuzz-time=5m`. Never `go test -fuzz` on the
host.

Go writes a new crasher to `testdata/fuzz/<Target>/`, but the Dagger run does
that **inside the container and exports nothing** — the failing input is in
the command's output and the corpus file is gone with the container. Turn it
into an `f.Add` seed by hand, which is the durable form anyway: seeds run
under `test` on every commit, an unexported corpus file would not.
