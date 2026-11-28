---
name: Performance
on: false
---
Find work this change makes slower or larger in a way that matters: a query or request inside a loop, a missing index for a new query, a full scan or copy where a lookup would do, unbounded growth of a list, cache or queue, a blocking call on a hot path, a lock held across I/O.

Say how the cost grows (with what input) and roughly where it starts to hurt. Do not report micro-optimisations.
