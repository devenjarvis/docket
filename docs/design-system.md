# Docket design system: Overprint

Docket is printed like a risograph zine: two drums of ink on cheap paper, a little off register. It looks made by hand because it is made the way a zine is: one dark ink that carries everything, one bright ink saved for a single job, and the second print never quite landing on the first.

## Where it lives

| File | What |
|---|---|
| `internal/web/static/docket.css` | The implementation: tokens, components and utilities. Components reference tokens only. |
| `docs/tokens.json` | The record of every token (each colourway in both modes, type, spacing, lines) with a usage note each, and the contrast pairs. The Design System artifact is built from it. |
| `internal/web/tokens_test.go` | `TestTokensMatchStylesheet` fails when `docket.css` and `tokens.json` disagree, or when a colour is added to the stylesheet without being recorded. `TestContrast` fails when any pair in `tokens.json` drops under its minimum in any colourway or mode. |
| `internal/web/static/styleguide.html` | The living style guide: every token and component, rendered by `docket.css` itself, in every colourway and both modes. Open `/static/styleguide.html` while `mise run demo` runs. |

To change a colour: change it in both mode blocks of its colourway in `docket.css` and in `tokens.json`, then `mise run test`. To add a colourway: add its two blocks and its record, then its id to `INKS` in `settings.js`, the list in `index.html`'s first script, and the style guide's picker; the test checks all three.

## Two drums

| Drum | Token | Means | Used for |
|---|---|---|---|
| **Key** | `--ink`, `--text` | what is in the pull request, and what the agent found | every word and line, frames, headings, code, the primary button, the current tab |
| **Spot** | `--spot` | **only ever yours** | your actions, your decisions, Your call, a step held for you, a count of what waits on you, the time you have |

The agent prints in the key drum too, but in its own voice: **dashed** rules (`--pencil`) and **italic** text. A finding is told apart from a fact by line and type, not hue. Claims are checked the way a fact-checker would mark them: **True**, **False**, **Can't tell**, **Not said**.

The spot drum is a fill, never text: under your action button as an offset print, under Your call, inside the decision triangle, behind a chosen option. Where it must carry text or a control's edge (a "Needs you" stamp, `.text-red`) it is `--spot-ink`: the spot overprinted with the key drum until it reads at 4.5:1. The wordmark and the inbox's cover art are its only decorative prints: the cover of the zine.

Diffs never use the spot drum, and never red and green: an added line is a tint of the key drum, a removed one is faint and struck through.

Class names that say `red` (`.btn.red`, `.stamp.red`, `.text-red`, `.bar.red`) mean the spot drum. The name is older than colourways.

## Misregistration

Nothing is blurred and nothing has a shadow. What looks like one is the second drum landing off register, by `--off` (4 px): the spot drum under your action, a red banner and the toast, twice that under Your call. A title gets a faint second pass of the key drum. Pressing your action button moves it onto its spot print, as if the drums lined up.

## Colourways

Each is two real riso inks, chosen in Settings › Reviews and kept with the mode in the webview's storage (`docket-ink`, `docket-theme`). Both are set on `<html>` as `data-ink` and `data-theme` before first paint.

| Colourway | Drums | Key / spot (light) |
|---|---|---|
| Black and Tomato (default) | Black · Bright Red | `#1e1d1b` / `#ff5a36` |
| Federal and Pink | Federal Blue · Fluorescent Pink | `#2b4b9b` / `#ff5cb8` |
| Hunter and Orange | Hunter Green · Fluorescent Orange | `#2e5b4b` / `#ff7a45` |
| Plum and Sunflower | Plum · Sunflower | `#6e2a4e` / `#ffb511` |
| Teal and Yellow | Teal · Yellow | `#00626a` / `#ffd60a` |
| Blue and Orange | Medium Blue · Orange | `#2546a0` / `#ff6c2f` |

Dark mode is the same two drums on black stock: the key drum turns pale, the spot stays bright.

Every colourway declares the same tokens:

| Token | Job |
|---|---|
| `--film` | the table the pages lie on, with a faint speckle of both drums |
| `--sheet` | a page |
| `--sunk` | wells: code, inputs, the tab strip, a hovered row, rows past the time you have |
| `--ink` | the key drum: linework, headings, frames |
| `--text` / `-2` / `-3` | body, secondary, meta |
| `--hair` | hairlines; never text, never a control's only edge |
| `--pencil` / `--pencil-wash` | the agent's dashed rules and labels, and the fill inside them |
| `--spot` / `--spot-wash` | the spot drum, and a lighter pass of it as a ground |
| `--spot-ink` | the spot drum where it must carry text or an edge |
| `--on-spot`, `--on-ink` | text on a spot fill, on a key-drum fill |
| `--add` / `--add-ink`, `--del` / `--del-ink` | diff fills and their text |

On a spot-wash ground (Your call, a held step, a red banner) `--text-3` and `--pencil` fall back to `--text-2`, which reads there in every colourway. `tokens.json` lists every pair that is checked, all at 4.5:1.

## Type

| Face | Job |
|---|---|
| **Big Shoulders Display** (900) | titles, section heads, big numbers, the wordmark. Condensed poster type, always uppercase. |
| **Work Sans** | running text and buttons. Italic is the agent's voice. |
| **Martian Mono** | labels, tabs, stamps, code, paths and numbers. Labels are 11 px uppercase. |

All three are the Latin subsets from Fontsource under the SIL Open Font License, bundled in `internal/web/static/fonts`; the app makes no network request for anything it shows.

## Shape

- Corners are square.
- Lines are printed, so a little heavy: hairline `1px`, `--line` `2px` for controls and frames, `--heavy` `3px` for a sheet, Your call, a band's rule and a chosen option.
- Stamps are set a degree or two off straight, alternating, like a rubber stamp.
- Textures are screens of the key drum: halftone dots for "verified, worth a skim" and a second series, fine ruling for "mechanical".
- Spacing sits on an 8 px unit (`--u`).

## Components

Each has a class in `docket.css` and a rendered example in the style guide.

**Frame**
- **Sheet** (`.sheet`): the page.
- **Masthead** (`header.mast`): heads the app's own pages (the inbox, Waiting on others, Back issues, the rulebook, settings). The wordmark, printed over the spot drum; a dateline (today, the reviewer, the agent, when GitHub was last checked, and what the agent is reading now); and the ways round, each with its count. The current one is filled with the key drum.
- **Byline** (`header.byline`, built by `byline()` in `dom.js`): heads a pull request's pages. The wordmark, then one line of facts: where it is, its title, who wrote it, the commit that was read, sheet n of m. Facts marked `.narrow-hide` drop on a phone.
- **Sheet index** (`nav.sheets`): a review's tabs, in reading order: Brief, Context, one per decision, Diff, Conversation, Sign-off. The current one is filled with the key drum. A decision's tab carries its triangle and a `.tab-title`.
- **Decision spread** (`.layout-spread`): a decision as a feature. The article (`.feature`) leads with a kicker (`.kicker`: the triangle, "Decision n of m" and why it is a question at all), then the `headline` in poster type (`.headline`), the finding as a fact (`.statement`), the detail, the before and after, what the agent found and the lines that matter. Beside it the **fact check** (`.factcheck`): every claim the pull request makes, the one this decision is about first, on a pass of the spot drum (`.claim.here`). Your call is the third column. Under 1180 px the fact check moves under the article; under 860 px everything stacks.

**Marks**
- **Triangle** (`.tri`): numbers your decisions, printed in the spot drum: open while unanswered, filled once answered. It appears in the inbox, the tabs, the claims and the sign-off. `.tri-link` leads a link with one.
- **Stamp** (`.stamp`): status in a box. Solid: established. Dashed: open. Spot fill (`.contra`): contradicted. Dashed pencil (`.faint`): the agent's label. Spot ink (`.red`): needs you. Key fill (`.solid`): settled.
- **Pencil box** (`.pencil`): anything the agent says or did, dashed.

**Yours** (the spot drum: the next move is yours)
- **Your call** (`.yours`): a pass of the spot drum under a key-drum frame, the spot printed again off register below it. Only your controls go in it. One per sheet, `.sticky` in the side column.
- **Option** (`.option`): a choice with its number key and its consequence stamped on it. Chosen, it fills with the spot drum; the comment it drafted opens under the options, labelled with the line it attaches to, and the panel's last button goes on to the next open decision, or to Sign-off.
- **Held step** (`.held`): a step the agent planned on your own pull request and the gate held for you.

**Controls**
- **Button** (`.btn`, `.primary`, `.red`, `.quiet`, `.small`): square, uppercase, outlined. `.primary` fills with the key drum. `.red` is your move: filled with the key drum, the spot drum printed under it off register; pressing it lands it on its print. A disabled button is dashed and says why next to it.
- **Field** (`.field`, `label.check`): a sunk well with a key-drum line along the bottom.
- **Segmented** (`.seg`): one of a few choices; the pressed one is filled.
- **Colourway choice** (`.inks`, `.ink-choice`): a printed swatch of a colourway's two drums, overprinted where they meet. The swatch carries its own `data-ink` and `data-theme`, so it shows its colours whatever the page's.

**Data**
- **Attention bar** (`.attn`): where the changed lines go. Spot drum: needs you. Halftone: verified, worth a skim. Fine ruling: mechanical.
- **Register** (`table.reg`): every table. `tr.go` is a whole-row link.
- **Inbox** (`.sheet.issue`): an issue of a zine. The **cover** (`.cover`) says how much there is in words: the count in poster type (`.cover-n`), "things waiting on you tonight" (`.cover-head`), one sentence on the bands (`.cover-dek`), the counts that matter, the time you have, Start, and Read another; behind the count, a halftone disc and a block of the spot drum (`.cover-art`), the cover's one decorative print. The **contents page** (`.contents`) lists everything in bands (`.band`), numbered straight through (`.item-no`), each entry (`.item`) led by its title as a link, then why it is here in the agent's voice (`.why`), and on the right your move (`.btn.red`) and Snooze. A confirm opens under an entry (`.item-expand`). Under 980 px the cover sits above the contents.
- **Cut line** (`.dim`): the time you said you have, a dashed line to cut along with a spot label. Entries past it (`.item.past`) fade to the secondary inks, and their move loses its spot print.
- **Tally** (`.tally`): counts at the head of an inbox, in poster type; `.big.red` prints the spot drum under a count that is yours.
- **Path** (`.path`), **Before and after** (`.ba`), **Diff** (`.diff`): the key drum only. A decision's lines carry a spot bar in the margin (`.hot`).
- **Chart** (`.chart`, `.colchart`), **Bars** (`.hbars`), **Named numbers** (`.kv`), **Area map** (`.areas`, `.hm0`–`.hm4`): series differ by screen (solid, halftone, open), never by hue alone; the spot drum only for what was yours.

**Feedback**
- **Banner** (`.banner`, `.banner.red`): one sentence about the whole sheet. Dashed for information; a spot pass when you must know before acting.
- **Conversation entry** (`.entry`, `.entry.on-you`): a review, comment or thread; a spot bar when the next move is yours.
- **Keys** (`.kbd`, `.live`): keyboard hints, and the square that blinks while the agent works.
- **Toast** (`.toast`): key-drum fill, spot print under it.

## Before and after

The base is drawn faint and open, the head solid; both share one scale. Under each side a dimension line gives its total extent, so "5 s became 40 s" is seen before it is read. A stamp says whether the comparison was **measured** or **reasoned, not run**.

## Utilities

Components draw; utilities only arrange. They sit at the end of `docket.css`, all on the 8 px unit, so screens do not need inline styles: `.row` and `.block` with `.gap-s` (4) `.gap-u` (8) `.gap-m` (12) `.gap-l` (32) `.gap-xl` (40) `.gap-2d`; alignment `.mid` `.base` `.top` `.spread` `.end` `.one-line`; spacing `.mt-s` `.mt` `.mt-l` `.mb` `.mb-l` `.py` `.my`; text `.strong` `.item-title` `.question` `.text-red` `.text-2` `.prewrap` `.nowrap` `.measure`; tables `.fit`; `.list`, `.stack`, `.sticky`, `.plain`, `.well`. Inline `style` is kept for values computed from data (a bar's width, a segment's flex).

## Voice

Docket reads like a zine put together for one reader: the inbox is today's issue, posted reviews are back issues, and a decision leads with a headline the agent writes (`headline` in the brief: two to six words, nothing in it the title does not back up).


Plain words, short sentences, no praise. State the finding as a fact, then ask the question. Buttons say what happens ("Request changes", "Spot-check a hunk"). The agent is never given a verdict to deliver, and the interface never implies it has one.
