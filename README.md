# Docket

Pull request review built around the reviewer's attention.

Every pull request that waits on you shows up here on its own. Your coding agent checks out the branch, runs it, and comes back with what it proved, what it could not, and the few questions only you can answer. You answer those, and Docket posts the review you signed.

Docket never calls a model itself. It starts the agent you already use (Claude Code, Codex, Gemini CLI, opencode, Goose) over the [Agent Client Protocol](https://agentclientprotocol.com), under your own sign-in. It talks to GitHub through your own `gh`.

## Run it

```bash
go mod tidy          # once: resolves the Wails 3 dependency
make demo            # sample pull requests and a scripted agent, in a browser
make serve           # your real pull requests, in a browser (http://127.0.0.1:4127)
make app && ./docket # the desktop app (Wails 3)
```

You need `git`, `gh` (signed in: `gh auth login`) and one ACP agent on your PATH. The desktop build needs cgo; on Linux also `libgtk-4-dev libwebkitgtk-6.0-dev`.

`make demo` needs none of that and shows the whole loop.

## The loop

1. **Found.** Every two minutes Docket asks GitHub for open pull requests where your review is requested (directly or through a team) or that are assigned to you. You can also paste a reference.
2. **Read.** For each new head commit it makes two throwaway checkouts, the head and the commit it forked from, and starts your agent in them with your rulebook and what Docket knows of your familiarity with the repository. The agent writes `brief.json`. Docket validates it against the diff and sends it back with the list of problems if it does not hold up (twice at most).
3. **Decided.** You get sheets: the Brief (claims checked, where the changed lines go), Context when the code is new to you, one sheet per decision with a before/after, the evidence and a draft comment, and the full Diff in reading order.
4. **Signed.** Approve stays locked while a decision is unanswered, blocking or waiting on the author. The review posts as one GitHub review with inline comments, attached to the commit that was read, with a receipt of what you read and what you took on the agent's proof.
5. **Re-read.** When the author pushes, Docket reads again, carries your answers over for every decision that still stands, and tells you what changed.

The agent also gets what other reviewers have already said, so it does not raise a point someone else already made. Every pull request has a Conversation sheet: reviews, line threads and comments, where you can reply, resolve and comment without opening GitHub.

## Your own pull requests

The Yours tab lists every open pull request you wrote, sorted by whose move it is: **Needs you** (a failing check, a conflict, changes requested, a thread waiting on your reply), **Waiting on others** (checks running, a review not in yet) and **Ready to merge**. Each one shows what stands in the way in plain words, its checks with the end of any failed log, and its conversation.

From there you merge (with one confirmation, and only with the methods the repository allows), turn on GitHub's auto-merge, update the branch, mark a draft ready, ask for review, re-run failed jobs, and answer every thread. Docket notifies you when one becomes ready to merge, starts to need you, or gets a review or comment from someone else.

### The agent works on them

Press **Let the agent work on it** on one of your pull requests, and your agent checks out the branch, reads the threads and the failed logs, and plans steps: a commit for what a reviewer asked or what broke a check, a reply, a resolve, a branch update, a re-run, or a merge of the base branch. After that first run, Docket starts it on its own when a reviewer comments or a check fails, and stops after three such runs in a row until you approve, send back or undo a step, or start a run yourself.

Each step goes through a gate before it runs. Fixed rules hold a step for you when it touches dependency manifests or lockfiles, toolchain versions, CI configuration, deletes or skips tests, changes more than 60 lines, resolves conflicts, or replies with anything but agreement or plain information. Every other step goes to a second session of the same agent, on its smallest model, which decides whether it is safe to do alone. A rename a reviewer asked for goes ahead; a change to how something starts up waits.

Steps that go ahead push to your branch as new commits on top, never with force. If you pushed in the meantime, the push fails and nothing is lost. Steps that wait show as cards with the diff or the reply text and the gate's reason: push or post it (you can edit a reply first), or send it back with a note the agent plans again with. Everything the agent did has an Undo: a revert commit on top, a deleted reply, or a reopened thread.

Settings has three choices: act on its own when it is safe (the default), always ask first, or off.

## What you should know before trusting it

- **The agent runs the pull request's code on your computer.** That is what lets a claim be proved. Turn it off in Settings for repositories whose authors you would not run code from; the agent then only reads and searches.
- **Directory limits are policy, not a sandbox.** Docket bounds what it reads, writes and runs on the agent's behalf. An agent that uses its own tools is bound only by its own permission prompts, which Docket answers for you (allow, inside the checkouts). The Brief says when that happened.
- **The triage is an agent's judgment.** A section marked mechanical or verified is closed by default. Its proof is on the lid, and the spot-check picks a random hunk so the proof gets tested.
- **A tool that learns from you also learns your silences.** The Brief lists what nothing looked at.

## Your data

Everything is under `~/.docket` (`DOCKET_HOME` to move it):

| Path | What |
|---|---|
| `rules/` | Your rulebook: one Markdown file per rule, `stats.json`, and a git history of every change |
| `state/` | One JSON file per review: the brief, your answers, your time |
| `state/mine/` | One JSON file per pull request of yours: merge state, checks, conversation, and the agent's last run |
| `tend/<owner>/<repo>/<n>/<sha>/` | The agent's checkouts of your branches |
| `reviews/<owner>/<repo>/<n>/<sha>/` | The checkouts, the diff, the agent's probes (`out/`) |
| `repos/` | Blobless bare clones |
| `map.json` | Which parts of which repositories you have reviewed, and when |
| `config.json` | Agent, poll interval, run-code, receipt and agent-on-your-pull-requests settings |

## Layout

```
main.go                  docket | serve | demo | version
internal/app             wires engine and server; shared by app and browser
internal/review          the loop: sync, read, answer, compose, post
internal/brief           the reading prompt and the brief validator
internal/diff            unified diff parser and range slicing
internal/repo            clones and per-PR head/base checkouts
internal/gh              GitHub through the gh CLI
internal/rules           the rulebook store
internal/owners          CODEOWNERS: which files are yours
internal/acp             ACP client (from Lathe)
internal/agents          known ACP agents and detection (from Lathe)
internal/loginenv        the login shell's PATH for a Dock launch (from Lathe)
internal/server          HTTP API, server-sent events
internal/web/static      the UI and the design system (no build step)
internal/desktop         the Wails 3 window, notifications, single instance
internal/demo            sample pull requests and the scripted ACP agent
docs/design-system.md    Whiteprint
```

`internal/acp`, `internal/agents` and `internal/loginenv` come from [Lathe](https://github.com/devenjarvis/lathe) (MIT, Deven Jarvis), with the job policies replaced.

## Not built yet

- Proposing rules from your past review comments across repositories.
- A packaged, signed `.app` (Lathe's `build/darwin/package.sh` is the model).
- Team membership for CODEOWNERS: only `@you` entries narrow your slice today.
- Handing blocking comments to the author's agent as tasks.
