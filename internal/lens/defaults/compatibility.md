---
name: Compatibility
on: true
---
Find what this change breaks for anything outside it: a public function, type, endpoint, flag, config key, file format, database schema, event or message shape that changes or disappears. Check callers in the repository. Check whether a migration is reversible and whether old and new versions can run side by side during a deploy.

For each finding, name who is broken and how.
