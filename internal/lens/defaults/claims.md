---
name: Description against code
on: true
---
Read the title and description and list every checkable thing they say. For each, find where the code keeps it or fails to. Then look for what the code does that the description does not mention: a changed default, a new side effect, a removed behaviour, a new dependency, a migration.

Report a claim the code does not keep, and behaviour a reviewer would want to be told about that the description leaves out. Do not report claims the code keeps.
