---
name: Security
on: true
---
Look for ways this change lets someone do what they should not. Input that reaches a query, a shell, a file path, a template or a redirect without being checked. A permission or ownership check that is missing, moved, or done after the action. Secrets in code, logs or error messages. Cryptography done by hand. A new dependency, or a version bump, that widens what runs. Anything that trusts data from the network, a file or the pull request itself.

Report only a path an attacker could actually take, and say who the attacker is.
