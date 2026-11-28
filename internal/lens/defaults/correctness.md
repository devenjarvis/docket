---
name: Correctness
on: true
---
Look for code that does the wrong thing. Edge cases the change gets wrong: empty input, zero, one, many, the last element, a nil or missing value, a duplicate, a value at a limit. Errors that are swallowed, returned without context, or leave state half-changed. Loops that do not end or end early. Concurrency: shared state touched without a lock, a goroutine or promise nobody waits for, a race between a check and its use. Resources opened and not closed.

Report only what you can point at in the code. Name the input that breaks it.
