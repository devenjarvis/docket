# Docket design system: Whiteprint

Review is redlining. Before screens, a change to a design was reviewed on a print of the drawing: the drawing in ink, the checker's notes in pencil, and the corrections that had to be made in red. Docket borrows that whole convention, because it already solves Docket's problem: three parties mark the same sheet and nobody may confuse them.

## Where it lives

| File | What |
|---|---|
| `internal/web/static/docket.css` | The implementation: tokens, components and utilities. Components reference tokens only. |
| `docs/tokens.json` | The record of every token (colours in both themes, type, spacing, lines) with a usage note each. The Design System artifact is built from it. |
| `internal/web/tokens_test.go` | Fails when `docket.css` and `tokens.json` disagree, or when a colour is added to the stylesheet without being recorded. |
| `internal/web/static/styleguide.html` | The living style guide: every token and component, rendered by `docket.css` itself, in both themes. Open `/static/styleguide.html` while `make demo` runs. |

To add or change a colour: change it in both theme blocks of `docket.css` and in `tokens.json`, then `make test`.

## Three marks

| Mark | Line | Means | Used for |
|---|---|---|---|
| **Ink** | solid | what is in the pull request | code, claims, headings, frames |
| **Pencil** | dashed, grey | what the agent did or found; provisional | evidence, proofs, the work log, sources |
| **Red** | solid, heavy | what the reviewer decides; the only binding mark | decisions, the verdict, "needs you" |

The line style carries the meaning, so the three are told apart without relying on hue.

Red is never decoration and never an error colour. If something is red, it is yours.

Diffs do not use red and green. An added line is filled blue; a removed line is greyed and struck through. This keeps red free and reads the same for colour-blind reviewers.

## Two themes

| Token | Whiteprint (light) | Blueprint (dark) | Job |
|---|---|---|---|
| `--film` | `#E9ECE6` | `#0A1C36` | the table the sheets lie on, with a faint 16 px grid |
| `--sheet` | `#F6F7F3` | `#0F2747` | a sheet |
| `--sunk` | `#E1E5DD` | `#0A1B33` | wells: code, inputs, the tab strip |
| `--ink` | `#1B2163` | `#D9E6FA` | linework and headings |
| `--text` / `-2` / `-3` | `#181B33` `#40455F` `#5D6279` | `#E7EEF9` `#B7C7E1` `#93A8C9` | body, secondary, meta |
| `--hair` | `#B4BACB` | `#2D4A77` | hairlines |
| `--pencil` / `--pencil-wash` | `#565B6B` `#ECEEE9` | `#A3B8D8` `#132F55` | the agent |
| `--red` / `--red-wash` | `#BF2A1A` `#F5DCD6` | `#FF8B76` `#43202A` | the reviewer |
| `--add` / `--del` | `#D7E1F6` `#E2E0D9` | `#1B4078` `#0A1930` | diff fills |

Every text colour reads at 4.5:1 or better on the grounds it is used on, in both themes (the lowest: `red` on `red-wash`, 4.5:1; `text-3` on `sunk`, 4.7:1). `hair` is 1.8:1, so it is never used for text or for a control's only border. Quiet and disabled buttons are bordered in `text-3` (4.5:1 or better), meeting the 3:1 WCAG asks of a control's boundary; a disabled one stays dashed.

A whiteprint is a blue-violet line on drafting film; the blueprint is what it replaced, a pale line on Prussian blue. The theme follows the system by default.

## Type

| Face | Job | Why |
|---|---|---|
| **Routed Gothic** (and Wide) | labels, titles, buttons, stamps | Drawn from the Leroy lettering templates used to letter engineering drawings. Wide, all caps, tracked out for labels; mixed case for titles. |
| **B612** | body text, statements | Designed for Airbus cockpit displays: built to be read quickly and without mistakes. |
| **Commit Mono** | code, numbers, paths | Neutral, with tabular figures. |

Routed Gothic draws `1` as a bare stroke, which reads as `I` in a label. Its `unicode-range` leaves digits out, so numbers in lettering fall through to B612.

All three are bundled as WOFF2 under the SIL Open Font License; the app makes no network request for anything it shows.

## Shape

- Corners are square. Nothing has a radius or a shadow.
- Three line weights: hairline `1px`, object line `1.5px` (`--line`), border line `3px` (`--heavy`). A sheet's frame and the reviewer's panel use the border line.
- Spacing sits on an 8 px unit (`--u`).

## Icon

A sheet from a drawing set on the Blueprint table: the sheet in ink with its border line and title block, a red revision cloud around two lines, and a filled revision triangle: a decision, answered. It uses the system's colours and no others, and the triangle is the same mark the app numbers decisions with, so red still means only the reviewer.

| File | What |
|---|---|
| `internal/desktop/icon/appicon.svg` | The drawing. The tile follows the macOS icon grid (824 px on a 1024 canvas); the rounded tile is the one radius in the system, because the platform asks for it. |
| `internal/desktop/appicon.png` | What the desktop app embeds, rendered from the SVG with `make icon`. |
| `internal/web/static/favicon.svg` | The browser tab's icon: the sheet and the triangle, without the cloud or grid, which do not survive 16 px. |

## Components

Each has a class in `docket.css` and a rendered example in the style guide.

**Frame**
- **Sheet** (`.sheet`): the framed page. A review is a set of sheets, numbered like a drawing set: Brief, Context, one per decision, Diff, Conversation, Sign-off.
- **Title block** (`.titleblock`): the strip of labelled cells every sheet carries: pull request, title, author, the commit that was read, sheet n of m. `.bare` drops its bottom rule (the masthead).
- **Sheet index** (`nav.sheets`): the tabs, in reading order. The current one is lifted: sheet fill, heavy ink rule on top. A decision's tab carries its triangle and a `.tab-title`.

**Marks**
- **Revision triangle** (`.tri`): the drafting mark for "changed here, see revision n". It numbers the reviewer's decisions: outlined while open, filled once answered. It appears in the queue, the tabs, the claims and the sign-off, so a decision is recognisable everywhere. `.tri-link` leads a link with one.
- **Stamp** (`.stamp`): status in a box. Solid border: established. Dashed: open. Filled red (`.contra`): contradicted. Dashed pencil (`.faint`): the agent's label. 2px red (`.red`): needs you. Filled ink (`.solid`): settled.
- **Pencil box** (`.pencil`): anything the agent says or did.

**Reviewer** (red: the next move is yours)
- **Your call** (`.yours`): the red-framed panel. Only the reviewer's controls go in it. One per sheet, `.sticky` in the side column.
- **Option** (`.option`): a choice with its number key, its consequence stamped on it.
- **Held step** (`.held`): a step the agent planned on your own pull request and the gate held for you, with the gate's reason, Push it / Post reply and Send back.

**Controls**
- **Button** (`.btn`, `.primary`, `.red`, `.quiet`, `.small`): square and lettered; the label says what happens. One `.primary` per view. A disabled button is dashed and says why next to it.
- **Field** (`.field`, `label.check`): a sunk well with an ink line along the bottom.

**Data**
- **Attention bar** (`.attn`): where the changed lines go. Solid red: needs you. Hatched ink: verified. Fine ruling: mechanical.
- **Register** (`table.reg`): every table. `tr.go` is a whole-row link. Stacked groups of one table use a fixed layout (`.queue`, `.lanes`) so their columns line up.
- **Tally** (`.tally`): counts at the head of an inbox; `.big.red` when the count is yours.
- **Path** (`.path`): the path a change sits on; `.step.changed` is where it changes.
- **Before and after** (`.ba`): see below.

**Code**
- **Diff** (`.diff`): blue fill for added lines, grey and struck through for removed ones; `.hot` puts a red rule in the margin on the lines a decision is about.

**Metrics and settings**
- **Segmented** (`.seg`): one of a few choices, such as a period; the pressed one is filled ink.
- **Chart** (`.chart`, `.colchart`): a framed detail with its title and legend in the strip on top. Stacked columns tell their series apart by texture: solid ink (`.f-ink`), hatched (`.f-hatch`), open (`.f-open`), so they read without hue and in print. The axis has four round steps; each column has a tooltip on hover and focus.
- **Bars** (`.hbars`): labelled bars on one scale, ink; `.track.red` only for what was the reviewer's (time on decisions, steps the agent held for them).
- **Named numbers** (`.kv`): a list of named figures, the figure in mono at the right.
- **Setting** (`.setting`): what a setting is, on the left; its control on the right. With **Stepper** (`.stepper`) for a small range and **Chip** (`.chip`) for a list the user edits.
- **Area map** (`.areas`, `.area.hm0`–`.hm4`): the parts of a repository, shaded in ink by how often the reviewer read them.

**Feedback**
- **Banner** (`.banner`, `.banner.red`): one sentence about the whole sheet. Pencil for information, red when you must know before acting.
- **Conversation entry** (`.entry`, `.entry.on-you`): a review, comment or thread; a heavy red rule when the next move is yours.
- **Keys** (`.kbd`, `.live`): keyboard hints, and the square that blinks while the agent works.

## Utilities

Components draw; utilities only arrange. They sit at the end of `docket.css`, all on the 8 px unit, so screens do not need inline styles: `.row` and `.block` with `.gap-s` (4) `.gap-u` (8) `.gap-m` (12) `.gap-l` (32) `.gap-xl` (40) `.gap-2d`; alignment `.mid` `.base` `.top` `.spread` `.end` `.one-line`; spacing `.mt-s` `.mt` `.mt-l` `.mb` `.mb-l` `.py` `.my`; text `.strong` `.item-title` `.question` `.text-red` `.text-2` `.prewrap` `.nowrap` `.measure`; tables `.fit`; `.list`, `.stack`, `.sticky`, `.plain`, `.well`. Inline `style` is kept for values computed from data (a bar's width, a segment's flex).

## Before and after

The base is drawn in pencil, as a drawing shows a superseded state; the head in ink. Both sides always share one scale.

- **Timeline**: lanes of spans (a filled bar: in flight; a dotted line: waiting) and instants (a diamond). Under each side, a dimension line gives its total extent the way a drawing gives a length, so "5 s became 40 s" is seen before it is read.
- **Table**: named quantities on each side. A changed row carries a delta in the margin and its head value in ink on blue; unchanged rows recede.
- **Text**: literal output side by side; lines only one side has are marked.
- **Image**: screenshots side by side.

A stamp says whether the comparison was **measured** (the agent ran both) or **reasoned, not run**.

## Voice

Plain words, short sentences, no praise. State the finding as a fact, then ask the question. Buttons say what happens ("Request changes", "Spot-check a hunk"). The agent is never given a verdict to deliver, and the interface never implies it has one.
