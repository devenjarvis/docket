// One pull request, as a set of sheets: Brief, Context, one sheet per
// decision, the full Diff in reading order, and Sign-off.

import { h, get, post, toast, plural, clock, ago, tri, prose, openExternal, byline } from "./dom.js";
import { beforeAfter } from "./viz.js";
import { renderConversation } from "./conversation.js";
import { href, go, refresh, itemHref } from "./app.js";
import { act } from "./conversation.js";
import { mineHref } from "./mine.js";

const KIND = { judgment: "Needs you", skim: "Verified, skim", mechanical: "Mechanical, proven" };
// A claim's status, in a fact-checker's words.
const CLAIM = { proved: ["True", ""], contradicted: ["False", ".contra"], unproven: ["Can't tell", ".open"], unstated: ["Not said", ".faint"] };

// words are what a sheet calls things: a review for someone else, or a
// self-review of the user's own pull request, where a decision is a finding
// and sign-off is the wrap-up.
const REVIEW_WORDS = { decision: "Decision", decisions: "decision", signoff: "Sign-off", toSignoff: "sign-off",
  blocking: "Blocks the merge", ask: "Stays open", accept: "Accepts it" };
const SELF_WORDS = { decision: "Finding", decisions: "finding", signoff: "Wrap up", toSignoff: "wrap up",
  blocking: "Fix first", ask: "Note for reviewers", accept: "Fine as is" };
export const words = (v) => (v && v.self ? SELF_WORDS : REVIEW_WORDS);

function sheetsOf(v) {
  const b = v.brief;
  if (!b) return [];
  const W = words(v);
  const out = [{ sheet: "brief", label: "Brief" }];
  if (b.context) out.push({ sheet: "context", label: "Context" });
  (b.decisions || []).forEach((d, i) => out.push({ sheet: "d", id: d.id, n: i + 1, label: `${i + 1}`, decision: d }));
  out.push({ sheet: "diff", label: "Diff" });
  out.push({ sheet: "conversation", label: "Conversation" });
  out.push({ sheet: "signoff", label: W.signoff });
  return out;
}
// isNew says whether a decision first appeared in this read, after the
// reviewer's last look.
const isNew = (v, id) => !!(v.brief && v.brief.since && (v.brief.since.new || []).includes(id));
const newStamp = (v, id) => isNew(v, id) && h("span.stamp.open", { class: "count" }, "New");

const at = (S, list) => Math.max(0, list.findIndex((s) => s.sheet === S.route.sheet && (s.sheet !== "d" || s.id === S.route.id)));

// The id that active time is counted against for the sheet on screen.
export function currentTimeTarget(S) {
  const r = S.route;
  if (r.sheet === "d") return r.id;
  if (r.sheet === "brief" || r.sheet === "context") return r.sheet;
  if (r.sheet === "diff") {
    // The section whose heading most recently passed the top of the window.
    let best = null;
    for (const el of document.querySelectorAll("[data-section]")) {
      if (el.getBoundingClientRect().top < innerHeight * 0.4) best = el.dataset.section;
    }
    return best;
  }
  return null;
}

export function reviewKeys(S, e) {
  const v = S.review;
  if (!v || !v.brief) return;
  const list = sheetsOf(v), i = at(S, list);
  const to = (j) => { const s = list[j]; if (s) go(href(v.key, s.sheet, s.id)); };
  if (e.key === "n" && S.route.sheet === "signoff") { const r = nextItem(S); if (r) go(itemHref(r)); }
  else if (e.key === "ArrowRight" || e.key === "n") to(i + 1);
  else if (e.key === "u") { const d = nextOpen(v, S.route.id); if (d) go(href(v.key, "d", d.id)); else toast(`Every ${words(v).decisions} is answered.`); }
  else if (e.key === "s") go(href(v.key, "signoff"));
  else if (e.key === "Backspace" && advancedFrom) { e.preventDefault(); const back = advancedFrom; advancedFrom = null; go(back); }
  else if (e.key === "ArrowLeft" || e.key === "p") to(i - 1);
  else if (S.route.sheet === "d" && /^[1-9]$/.test(e.key)) {
    const d = v.brief.decisions.find((x) => x.id === S.route.id);
    const o = d && d.options[Number(e.key) - 1];
    if (o) answer(v, d, { option: o.id });
  }
}

// nextOpen is the first unanswered decision after the one with id after,
// wrapping round to the start; null when every decision is answered.
function nextOpen(v, after) {
  const ds = v.brief.decisions || [];
  const from = ds.findIndex((d) => d.id === after) + 1;
  for (let k = 0; k < ds.length; k++) {
    const d = ds[(from + k) % ds.length];
    if (d.id !== after && !(v.answers[d.id] || {}).option) return d;
  }
  return null;
}

// advancedFrom is the decision sheet an answer moved the reviewer away
// from, so Backspace can bring them back.
let advancedFrom = null;
let advanceTimer;

async function answer(v, d, patch) {
  const cur = v.answers[d.id] || {};
  const next = { option: cur.option || "", comment: cur.comment || "", feedback: cur.feedback || "", ...patch };
  // Choosing a different option starts from that option's own draft.
  const chose = patch.option && patch.option !== cur.option;
  if (chose) next.comment = "";
  try { await post("/api/answer", { key: v.key, decision: d.id, ...next }); }
  catch (e) { toast(e.message); return; }
  await refresh(true);
  if (chose) advanceAfter(v, d);
}

// advanceAfter moves on from a decision just answered: to the next open
// decision, or to Sign-off once none is left. It waits a beat so the choice
// is seen, and stays put if the reviewer starts typing or moves first.
function advanceAfter(v, d) {
  clearTimeout(advanceTimer);
  const here = location.hash;
  advanceTimer = setTimeout(() => {
    const el = document.activeElement;
    if (location.hash !== here || (el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT"))) return;
    const answers = { ...v.answers, [d.id]: { option: "x" } };
    const to = nextOpen({ ...v, answers }, d.id);
    const n = (id) => v.brief.decisions.findIndex((x) => x.id === id) + 1;
    advancedFrom = here;
    go(to ? href(v.key, "d", to.id) : href(v.key, "signoff"));
    const W = words(v);
    toast(`${to ? `On to ${W.decisions} ${n(to.id)}` : `Every ${W.decisions} is answered: on to ${W.signoff}`}. Backspace goes back to ${W.decisions} ${n(d.id)}.`);
  }, 400);
}

// --- frame ------------------------------------------------------------------
export function renderReview(S) {
  const v = S.review, p = v.pr, b = v.brief, W = words(v);
  const list = sheetsOf(v), i = at(S, list);
  const head = byline([
    ["", `${p.owner}/${p.repo} #${p.number}`, ".mono"],
    ["", p.title, ".title-fact.narrow-hide"],
    v.self ? ["", "self-review", ".narrow-hide"] : ["by", p.author, ".narrow-hide"],
    ["read at", v.readSha ? v.readSha.slice(0, 7) : "not yet", ".mono.narrow-hide"],
    ["sheet", list.length ? `${i + 1} of ${list.length}` : "none"],
  ]);

  // Past two decisions, a decision's tab is its triangle alone, so the index
  // keeps to one row; Sign-off is pinned at the right either way.
  const compact = (b && b.decisions || []).length > 2;
  const tab = (s) => h("a", { href: href(v.key, s.sheet, s.id), "aria-current": s === list[i] ? "page" : null, title: s.decision ? `${W.decision} ${s.n}: ${s.decision.title}` : null,
    "aria-label": s.decision && compact ? `${W.decision} ${s.n}: ${s.decision.title}` : null, class: s.decision && compact ? "tab-tri" : null },
  s.decision ? [tri(s.n, !!(v.answers[s.id] && v.answers[s.id].option)), !compact && h("span", { class: "tab-title" }, v.self ? s.decision.headline || s.decision.title : s.decision.title), newStamp(v, s.id)] : s.label);
  const tabs = b && h("nav.sheets", { "aria-label": "Sheets" },
    list.filter((s) => s.sheet !== "signoff").map(tab),
    h("span.spacer"),
    list.filter((s) => s.sheet === "signoff").map(tab),
    h("a", { href: p.url || "#", target: "_blank", rel: "noreferrer", onclick: openExternal }, "On GitHub"),
    v.self ? h("a", { href: mineHref(v.key) }, "Your pull request")
      : h("a", { href: "#/", onclick: (ev) => { ev.preventDefault(); post("/api/remove", { key: v.key }).then(() => go("#/")); } }, "Remove from the queue"));

  let body;
  if (!b) body = waiting(v);
  else if (S.route.sheet === "context" && b.context) body = context(v);
  else if (S.route.sheet === "d") body = decision(v, list[i]);
  else if (S.route.sheet === "diff") body = fullDiff(v);
  else if (S.route.sheet === "signoff") body = v.self ? wrapUp(S, v) : signoff(S, v);
  else if (S.route.sheet === "conversation") body = S.conv ? renderConversation(v.key, S.conv, S.st.queue.viewer, v.pr.author, () => refresh(true)) : h("p.muted", "Could not read the conversation.");
  else body = brief(S, v, list);

  const banners = [
    v.summary.pushed && v.status !== "reading" && v.status !== "queued" && h("div.banner.red", v.self
      ? ["You pushed since this was read. Your answers carry over to the next read for every finding that still stands. ", h("button.btn.small", { onclick: () => post("/api/self", { key: v.key }).then(() => refresh(true), (e) => toast(e.message)) }, "Read it again")]
      : "The author pushed since this was read. A fresh read is queued; your answers carry over for every decision that still stands."),
    b && (v.status === "reading" || v.status === "queued") && h("div.banner", h("span.live"), "Reading again: ", h("span", { "data-activity": v.key }, v.activity || "queued"), ". What you see is the earlier read."),
    b && b.since && h("div.banner", h("b", "Since your last look: "), prose(b.since.summary)),
    v.status === "posted" && v.posted && h("div.banner", `You posted this review ${ago(v.posted.at)} (${v.posted.event.toLowerCase().replace("_", " ")}). `, h("a", { href: v.posted.url, target: "_blank", rel: "noreferrer", onclick: openExternal }, "See it on GitHub")),
    v.self && b && b.since && (b.since.resolved || []).length > 0 && h("div.banner", `${plural(b.since.resolved.length, "earlier finding")} resolved by the push.`),
  ];
  return h("div.sheet", head, tabs, h("div.body", banners, body, b && pager(v, list, i)));
}

function pager(v, list, i) {
  const prev = list[i - 1], cur = list[i];
  let next = list[i + 1];
  const W = words(v);
  const name = (s) => s.decision ? `${W.decision} ${s.n}` : s.label;
  const link = (s, cls) => h("a.btn" + cls, { href: href(v.key, s.sheet, s.id) }, s === prev ? "← " + name(s) : name(s) + (cls === ".primary" ? " →" : ""));
  // Once every decision is answered, the way forward is Sign-off; Diff and
  // Conversation stay a click away beside it.
  const done = (v.brief.decisions || []).every((d) => (v.answers[d.id] || {}).option);
  let aside = [];
  if (done && cur.sheet !== "signoff") {
    next = list.find((s) => s.sheet === "signoff");
    aside = list.filter((s) => (s.sheet === "diff" || s.sheet === "conversation") && s !== cur && s !== prev)
      .map((s) => h("a.small", { href: href(v.key, s.sheet, s.id) }, s.label));
  }
  const open = (v.brief.decisions || []).some((d) => !(v.answers[d.id] || {}).option);
  return h("div.row.rule-top", { class: "spread mid" },
    prev ? link(prev, ".quiet") : h("span"),
    h("span.small.muted", h("span.kbd", "←"), " ", h("span.kbd", "→"), " sheets",
      open && [" · ", h("span.kbd", "u"), ` next open ${W.decisions}`], " · ", h("span.kbd", "s"), ` ${W.toSignoff}`),
    h("div.row", { class: "mid" }, aside, next ? link(next, ".primary") : h("span")));
}

function waiting(v) {
  if (v.status === "failed") {
    return h("div.block",
      h("h1.title", "This one could not be read"),
      h("pre.code.out", v.error),
      h("div.row", h("button.btn.primary", { onclick: () => post("/api/reread", { key: v.key }).then(() => refresh(true)) }, "Try again"),
        !v.self && h("button.btn.quiet", { onclick: () => post("/api/remove", { key: v.key }).then(() => go("#/")) }, "Remove from the queue")));
  }
  if (v.self) {
    return h("div.block",
      h("h1.title", "Your agent is reviewing your pull request"),
      h("p.lede", "It reads it the way you read other people's: it runs the branch and the commit it forked from side by side, checks what the description claims, holds it to your rulebook, and comes back with what a reviewer would stop on, so you can settle it before anyone else looks."),
      h("div.pencil", h("div.label", "Now"), h("div", h("span.live"), h("span", { "data-activity": v.key }, v.activity || "Queued"))),
      h("p.small.muted", "You can leave. Docket tells you when it is ready."));
  }
  return h("div.block",
    h("h1.title", "Your agent is reading this"),
    h("p.lede", "It has the branch and the commit it forked from checked out side by side. It runs both, checks what the description claims, applies your rulebook, and comes back with what it proved, what it could not, and where you are needed."),
    h("div.pencil", h("div.label", "Now"), h("div", h("span.live"), h("span", { "data-activity": v.key }, v.activity || "Queued"))),
    h("p.small.muted", "You can leave. Docket tells you when it is ready."));
}

// --- brief --------------------------------------------------------------------
function brief(S, v, list) {
  const b = v.brief, sum = v.summary;
  // Nothing needs the reviewer here: offer the short way, one random hunk
  // and then the same confirm row as Sign-off.
  const fast = sum.lane === "clear" && v.status === "ready" ? { el: h("div") } : null;
  if (fast && S.fast && S.fast.key === v.key) setTimeout(() => spotAndApprove(S, v, fast), 0);
  const total = b.sections.reduce((n, s) => n + s.lines, 0) || 1;
  const by = (k) => b.sections.filter((s) => s.kind === k).reduce((n, s) => n + s.lines, 0);
  const first = list[1];
  const decN = (id) => b.decisions.findIndex((d) => d.id === id) + 1;
  const W = words(v);
  const startLabel = first.sheet === "context" ? "Start with the context" : first.decision ? `Start with ${W.decisions} 1` : "Read the diff";
  const howMuch = sum.decisions ? `${plural(sum.decisions, W.decisions)}, about ${Math.max(sum.minutes, 1)} min`
    : v.self ? "Nothing a reviewer would stop on" : "No decisions for you";

  return h("div.cols",
    h("div.main",
      h("div.block",
        h("h1.title", v.pr.title),
        h("p.lede", prose(b.summary)),
        h("div.row", { class: "mid" },
          v.self && !sum.decisions ? h("a.btn.primary", { href: mineHref(v.key) }, "Back to your pull request") : h("a.btn.primary", { href: href(v.key, first.sheet, first.id) }, startLabel),
          fast && !v.self && h("button.btn", { onclick: () => spotAndApprove(S, v, fast) }, "Spot-check and approve"),
          h("span.small.muted", howMuch)),
        fast && fast.el),

      h("div.block",
        h("div.label.ink", `Where the ${total} changed lines go`),
        h("div.attn", { role: "img", "aria-label": `${by("judgment")} lines need you, ${by("skim")} verified, ${by("mechanical")} mechanical` },
          b.sections.map((s) => h("div.fill-" + s.kind, { style: { flex: `${Math.max(s.lines, 1)} 0 0` }, title: `${s.title}: ${s.lines} lines` }))),
        h("div.row.small", { class: "gap-2d" },
          h("span", h("span.key-sw.fill-judgment"), `Needs you: ${by("judgment")}`),
          h("span", h("span.key-sw.fill-skim"), `Verified, worth a skim: ${by("skim")}`),
          h("span", h("span.key-sw.fill-mechanical"), `Mechanical, proven: ${by("mechanical")}`))),

      h("div.block",
        h("div.label.ink", "Fact check: what the pull request claims"),
        h("div.scroll-x", h("table.reg", h("tbody", (b.claims || []).map((c) => {
          const [word, cls] = CLAIM[c.status] || [c.status, ""];
          return h("tr",
            h("td", { class: "fit" }, h("span.stamp" + cls, word)),
            h("td", h("div", { class: "strong" }, prose(c.text)), h("div.small.muted", prose(c.evidence))),
            h("td.r", { class: "fit nowrap" }, c.decision && decN(c.decision) ? h("a", { href: href(v.key, "d", c.decision), class: "tri-link" }, tri(decN(c.decision), !!(v.answers[c.decision] || {}).option), "your call") : ""));
        })))),
        !(b.claims || []).length && h("p.muted", "The description makes no checkable claims.")),

      h("div.block",
        h("div.label.ink", "Reading order, by intent"),
        h("div.scroll-x", h("table.reg",
          h("thead", h("tr", h("th", ""), h("th", "Part"), h("th", "How it was covered"), h("th.r", "Lines"))),
          h("tbody", b.sections.map((s, i) => h("tr.go", { onclick: () => go(href(v.key, "diff") + "/" + s.id) },
            h("td.num", i + 1),
            h("td", h("div", { class: "strong" }, s.title), h("div.small.muted.mono", s.files.map((f) => f.path).join(", "))),
            h("td.small", s.kind === "judgment" ? h("span.stamp.red", "Needs you") : [h("span.stamp" + (s.kind === "mechanical" ? ".faint" : ".open"), KIND[s.kind]), h("div.muted", { class: "mt-s" }, prose(s.proof))]),
            h("td.r.num", s.lines)))))))),

    h("div.side",
      v.self ? h("div.block",
        h("div.label.ink", "Your pull request"),
        h("div.small", "Your agent reviewed it for you, the way it reviews other people's, before anyone else looks. What you mark to fix goes to your agent; what you explain posts as a note for reviewers."),
        h("a.small", { href: mineHref(v.key) }, "Checks, conversation and merge")) : h("div.block",
        h("div.label.ink", "Why you are here"),
        h("div", (v.pr.why || []).join(", ") || "added by hand"),
        v.slice && v.slice.length ? h("div.small.muted", `${v.slice.length} of the changed files fall to you by CODEOWNERS. The rest are in sections marked outside your slice.`) : null),
      h("div.block",
        h("div.label.ink", "Your rules on this change"),
        (b.ruleHits || []).length ? b.ruleHits.map((r) => {
          const rule = v.rules[r.rule];
          return h("div.small", { style: { display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 10px" } },
            h("span.stamp" + (r.result === "violated" ? ".red" : r.result === "passed" ? "" : ".open"), { class: "self-start" }, r.rule === "repo" ? "Repo" : r.rule),
            h("div", rule ? h("div", { class: "strong" }, rule.text) : r.rule === "repo" ? h("div", { class: "strong" }, "The repository's own conventions") : null,
              h("div.muted", prose(r.note))));
        }) : h("div.small.muted", "None of your rules applies to this change."),
        h("a.small", { href: "#/rules" }, "Open the rulebook")),
      (b.uncovered || []).length ? h("div.block",
        h("div.label.ink", "Nothing looked at"),
        h("ul.small", { class: "list" }, b.uncovered.map((u) => h("li", prose(u))))) : null,
      h("div.pencil",
        h("div.label", `What ${v.agent || "the agent"} did`),
        h("div.small", { style: { display: "grid", gridTemplateColumns: "1fr auto", gap: "4px 12px" } },
          (b.work || []).map((w) => [h("span", prose(w.did)), h("span.mono.muted", w.result)])),
        h("div.small.muted", { class: "mt" },
          `${v.model || ""}${v.readTookSeconds ? ` · took ${clock(v.readTookSeconds)}` : ""} · ${ago(v.readAt)}. It gives no verdict.`,
          v.isolation === "soft" ? " It used its own tools, so Docket's directory limits were not enforced." : ""),
        h("button.btn.quiet.small", { class: "mt", onclick: () => post(v.self ? "/api/self" : "/api/reread", { key: v.key }).then(() => refresh(true), (e) => toast(e.message)) }, "Read it again"))));
}

// spotAndApprove picks one hunk from the sections taken on proof, and once the
// reviewer says the proof holds for it, records the spot-check and asks to
// post Approve.
function spotAndApprove(S, v, fast) {
  const hunks = v.brief.sections.filter((s) => s.kind !== "judgment").flatMap((s) =>
    (v.sectionDiffs[s.id] || []).flatMap((fd) => (fd.hunks || []).map((hk) => ({ section: s, fd: { ...fd, hunks: [hk] } }))));
  if (!hunks.length) { go(href(v.key, "signoff")); return; }
  // The pick is kept, so a refresh from a live update shows the same hunk.
  if (!S.fast || S.fast.key !== v.key || S.fast.n >= hunks.length) S.fast = { key: v.key, n: Math.floor(Math.random() * hunks.length) };
  const pick = hunks[S.fast.n];
  const confirm = h("div.block.hidden", { class: "gap-u well" });
  // The draft is kept on S too: recording the spot-check sends a live update
  // that redraws the Brief, and the confirm row has to survive it.
  const showConfirm = (d) => {
    confirm.replaceChildren(...confirmPost(S, v, "APPROVE", d.comments || [], () => "", () => !!d.includeReceipt, () => { S.fast = null; fast.el.replaceChildren(); }));
    confirm.classList.remove("hidden");
    confirm.querySelector("button").focus();
  };
  const holds = async (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    try {
      await post("/api/audit", { key: v.key, section: pick.section.id, done: true });
      const d = await get("/api/draft?key=" + encodeURIComponent(v.key));
      if ((d.locks || []).length) { toast("Approve is locked: " + d.locks.map((l) => l.text).join(", ")); go(href(v.key, "signoff")); return; }
      S.fast.draft = d;
      showConfirm(d);
    } catch (e) { toast(e.message); btn.disabled = false; }
  };
  if (S.fast.draft) setTimeout(() => showConfirm(S.fast.draft), 0);
  fast.el.replaceChildren(h("div.block",
    h("div.label", `One hunk, picked at random from ${pick.section.title}`),
    h("div.pencil", h("div.label", "The agent's proof"), h("div", prose(pick.section.proof))),
    diffBlock(pick.fd),
    h("div.row", { class: "mid gap-u" },
      h("button.btn.primary", { onclick: holds }, "The proof holds: approve"),
      h("a.btn.quiet", { href: href(v.key, "diff") + "/" + pick.section.id }, "Read the whole section")),
    confirm));
}

// --- context ---------------------------------------------------------------------
function context(v) {
  const c = v.brief.context;
  return h("div.cols",
    h("div.main",
      h("div.block", h("div.label", "Context for this review"), h("h1.title", c.title), h("p.lede", prose(c.lede))),
      (c.path || []).length ? h("div.block", h("div.label.ink", "The path this change sits on"),
        h("div.path", c.path.map((s) => h("div.step" + (s.changed ? ".changed" : ""), h("b", prose(s.label)), h("small", prose(s.note)), s.changed && h("small", { style: { fontFamily: "var(--lettering-wide)", letterSpacing: "0.09em", textTransform: "uppercase", marginTop: "4px" } }, "changed here"))))) : null,
      beforeAfter(c.beforeAfter, (f) => outURL(v, f)),
      (c.facts || []).length ? h("div.block", h("div.label.ink", "Worth holding in mind"),
        h("div.scroll-x", h("table.reg", h("tbody", c.facts.map((f) => h("tr", h("td", prose(f.text)), h("td.small.mono.muted", { style: { width: "34%" } }, f.source))))))) : null,
      c.analogy ? h("div.block", h("div.label.ink", "Like something you know"), h("p", prose(c.analogy.text)), h("div.small.mono.muted", c.analogy.source)) : null),
    h("div.side",
      h("div.block", h("div.label.ink", "Your map of this repository"),
        (v.areas || []).map((a) => h("div.small",
          h("div.row", { class: "spread gap-u" }, h("span.mono", a.area), h("span.muted", a.reviews ? `${plural(a.reviews, "review")}, ${ago(a.last)}` : "not reviewed here")),
          h("div.bar", h("i", { style: { width: Math.min(100, (a.reviews || 0) * 12) * freshness(a.last) + "%" } })))),
        h("p.small.muted", "Counted from reviews you signed in Docket. It fades with time, so this page covers what you have lost or never had.")),
      h("div.pencil", h("div.label", `Built from ${plural((c.sources || []).length, "source")}`),
        h("div.small.mono", (c.sources || []).map((s) => h("div", s.ref, s.note ? h("span.muted", " · " + s.note) : null))))));
}

function freshness(last) {
  if (!last || last.startsWith("0001")) return 0;
  const months = (Date.now() - new Date(last).getTime()) / (86400e3 * 30);
  return Math.max(0.15, 1 - months / 12);
}
const outURL = (v, file) => `/api/out?key=${encodeURIComponent(v.key)}&file=${encodeURIComponent(file)}`;

// --- diff ---------------------------------------------------------------------------
function diffBlock(fd) {
  const lines = [];
  (fd.hunks || []).forEach((hk, i) => {
    if (i > 0 || hk.newStart > 1) lines.push(h("div.gap", `@@ line ${hk.newStart}${hk.header ? "  " + hk.header : ""}`));
    for (const l of hk.lines) {
      const cls = l.k === "+" ? ".add" : l.k === "-" ? ".del" : "";
      lines.push(h("div.ln" + cls + (l.hot ? ".hot" : ""),
        h("span.mk"), h("span.no", l.o || ""), h("span.no", l.n || ""), h("span.sg", l.k === " " ? "" : l.k === "-" ? "−" : "+"), h("span.tx", l.t || " ")));
    }
  });
  return h("div.diff",
    h("div.file", h("span", fd.path), h("span.muted", fd.status === "modified" ? "" : fd.status)),
    fd.binary ? h("div.gap", "Binary file") : h("div.lines", lines));
}

function fullDiff(v) {
  const b = v.brief, want = location.hash.split("/")[6];
  setTimeout(() => { const el = want && document.querySelector(`[data-section="${want}"]`); if (el) el.scrollIntoView(); }, 0);
  return h("div.block", { class: "gap-xl" },
    h("div.block", h("h1.title", "The whole change, in reading order"),
      h("p.lede", "Sections that need you are open. The rest are closed with the agent's proof on the lid; open one to skim it, or spot-check a hunk to keep the proof honest.")),
    b.sections.map((s, i) => {
      const fds = v.sectionDiffs[s.id] || [];
      const closed = s.kind !== "judgment" && want !== s.id;
      const hunks = fds.flatMap((fd) => (fd.hunks || []).map((hk) => ({ ...fd, hunks: [hk] })));
      const sample = h("div");
      const spot = () => {
        // The pick is kept, so a refresh from a live update shows the same hunk.
  if (!S.fast || S.fast.key !== v.key || S.fast.n >= hunks.length) S.fast = { key: v.key, n: Math.floor(Math.random() * hunks.length) };
  const pick = hunks[S.fast.n];
        sample.replaceChildren(h("div.block", h("div.label", "One hunk, picked at random"), diffBlock(pick),
          h("label.check", h("input", { type: "checkbox", checked: !!v.audited[s.id], onchange: (e) => post("/api/audit", { key: v.key, section: s.id, done: e.target.checked }) }),
            "I checked this hunk and the proof holds for it")));
      };
      const lid = h("div.row", { class: "base gap-m" },
        h("span.num.muted", i + 1),
        h("div.grow", h("h2.h2", s.title), h("div.small.muted", prose(s.why))),
        s.kind === "judgment" ? h("span.stamp.red", "Needs you") : h("span.stamp" + (s.kind === "mechanical" ? ".faint" : ".open"), KIND[s.kind]),
        h("span.num.small.muted", plural(s.lines, "line")));
      const decs = b.decisions.map((d, n) => [d, n + 1]).filter(([d]) => d.section === s.id);
      return h("section.block.rule-top", { "data-section": s.id },
        lid,
        decs.length ? h("div.row", { class: "gap-2d" }, decs.map(([d, n]) => h("a.small", { href: href(v.key, "d", d.id), class: "tri-link" }, tri(n, !!(v.answers[d.id] || {}).option), d.title))) : null,
        s.kind !== "judgment" && h("div.pencil", h("div.label", "Proof"), h("div", prose(s.proof)),
          hunks.length ? h("div.row", { class: "mt mid" },
            h("button.btn.small", { onclick: spot }, "Spot-check a hunk"),
            v.audited[s.id] && h("span.stamp", "Spot-checked by you")) : null, sample),
        closed
          ? h("details", h("summary", h("span.label.ink", `Show all ${plural(s.lines, "line")} in ${plural(fds.length, "file")}`)), h("div.block", fds.map(diffBlock)))
          : h("div.block", fds.map(diffBlock)));
    }));
}

// --- one decision --------------------------------------------------------------------
function decision(v, sheet) {
  const d = sheet.decision, a = v.answers[d.id] || {};
  const chosen = d.options.find((o) => o.id === a.option);
  const rule = d.rule && v.rules[d.rule];

  const evidence = (d.evidence || []).map((e) => h("div.pencil",
    h("div.row", { class: "spread base mb" },
      h("div.label", { class: "agent-ink" }, `${e.kind === "probe" ? "Probe the agent wrote and ran" : e.kind === "run" ? "The agent ran" : e.kind === "search" ? "The agent searched" : "The agent read"}: ${e.title}`),
      e.passed === false ? h("span.stamp.solid", "Fails") : e.passed === true ? h("span.stamp", "Passes") : null),
    e.code && h("pre.code", e.code),
    e.command && h("div.small.mono.muted", { class: "my" }, "$ " + e.command),
    e.output && h("pre.code.out", e.output),
    e.file && h("div.small.muted", { class: "mt" }, "Saved as ", h("span.mono", e.file), " in this review's folder, so it can be rerun.")));

  const asks = (v.asks || []).filter((q) => q.decision === d.id);
  const askInput = h("input.field", { placeholder: "Ask the agent to check something", "aria-label": "Question for the agent" });
  const ask = async () => {
    if (!askInput.value.trim()) return;
    try { await post("/api/ask", { key: v.key, decision: d.id, question: askInput.value }); askInput.value = ""; refresh(true); }
    catch (e) { toast(e.message); }
  };
  askInput.addEventListener("keydown", (e) => { if (e.key === "Enter") ask(); });

  const comment = chosen && h("textarea.field", { rows: 5, id: "comment-" + d.id, value: a.comment || chosen.comment || "",
    onchange: (e) => answer(v, d, { comment: e.target.value === chosen.comment ? "" : e.target.value }) });

  const fb = (id, label) => h("button.btn.small" + (a.feedback === id ? ".primary" : ""), { onclick: () => answer(v, d, { feedback: id }) }, label);
  const makeRule = async () => {
    try {
      const r = await post("/api/rule", { text: d.proposedRule.text, applies: d.proposedRule.applies, scope: `${v.pr.owner}/${v.pr.repo}`, severity: chosen && chosen.effect === "blocking" ? "blocking" : "note", status: "trial",
        origin: `Drafted from your decision on ${v.pr.owner}/${v.pr.repo} #${v.pr.number}`, why: "drafted from a decision, on trial" });
      toast(`${r.id} added, on trial in ${v.pr.owner}/${v.pr.repo} only.`);
    } catch (e) { toast(e.message); }
  };

  // The kicker says why this is a question at all.
  const claims = v.brief.claims || [];
  const mine = claims.filter((c) => c.decision === d.id);
  const why = mine.some((c) => c.status === "contradicted") ? "the description says otherwise"
    : mine.some((c) => c.status === "unproven") ? "the description claims it, nothing proved it"
    : mine.some((c) => c.status === "unstated") ? "the pull request doesn't mention it"
    : rule ? `your rule ${rule.id} raised it` : d.rule === "repo" ? "the repository's conventions" : v.self ? "a reviewer would stop on it" : "only you can answer it";
  const count = v.brief.decisions.length;
  const next = nextOpen(v, d.id);
  const W = words(v);
  const where = d.anchor && d.anchor.path ? `${d.anchor.path.split("/").pop()}:${d.anchor.line}` : "";

  return h("div.layout-spread",
    h("article.feature",
      h("div.kicker", tri(sheet.n, !!a.option), h("span.label.ink", `${W.decision} ${sheet.n} of ${count} · ${why}`), newStamp(v, d.id)),
      d.headline ? [h("h1.headline", d.headline), h("p.statement", prose(d.title))] : h("h1.statement", prose(d.title)),
      h("p.lede", prose(d.detail)),
      beforeAfter(d.beforeAfter, (f) => outURL(v, f)),
      evidence.length ? h("div.block", h("div.label.ink", "What the agent found"), evidence) : null,
      (v.hotDiffs[d.id] || []).length ? h("div.block", h("div.label.ink", "The lines that matter"), (v.hotDiffs[d.id] || []).map(diffBlock),
        h("div.small.muted", "A bar in the margin marks them. ", h("a", { href: href(v.key, "diff") + "/" + d.section }, "See the whole section"))) : null),

    h("aside.factcheck", { "aria-label": "Fact check" },
      h("h2.h2", "Fact check"),
      h("p.small.muted", "What the pull request says about itself, checked by the agent."),
      claims.length ? [...mine, ...claims.filter((c) => c.decision !== d.id)].map((c) => {
        const [word, cls] = CLAIM[c.status] || [c.status, ""];
        const n = c.decision && c.decision !== d.id ? v.brief.decisions.findIndex((x) => x.id === c.decision) + 1 : 0;
        return h("div.claim" + (c.decision === d.id ? ".here" : ""),
          h("span.stamp" + cls, word),
          h("div.small", prose(c.text)),
          c.decision === d.id && h("div.small.muted", "This is the claim in question."),
          n > 0 && h("a.small", { href: href(v.key, "d", c.decision), class: "tri-link" }, tri(n, !!(v.answers[c.decision] || {}).option), `decision ${n}`));
      }) : h("p.small.muted", "The description makes no checkable claims.")),

    h("div.yours-col",
      h("div.yours", { class: "sticky" },
        h("div.label", "Your call"),
        h("div", { class: "question" }, prose(d.question)),
        h("div.small.muted", "The agent cannot decide this."),
        h("div.block", { class: "gap-u" }, d.options.map((o, i) =>
          h("button.option", { "aria-pressed": String(a.option === o.id), onclick: () => answer(v, d, { option: a.option === o.id ? "" : o.id }) },
            h("span.key", i + 1), h("span", h("b", o.label), h("small", prose(o.detail)),
              h("span.stamp" + (o.effect === "blocking" ? ".red" : o.effect === "ask" ? ".open" : ""), W[o.effect] || o.effect))))),
        chosen && !(v.self && chosen.effect === "accept") && h("div.block",
          h("label.label", { for: "comment-" + d.id }, v.self
            ? (chosen.effect === "blocking" ? "The fix, as your agent will get it" : where ? `Note for reviewers on ${where}` : "Note for reviewers")
            : chosen.effect === "accept" ? "A note to post, if you want one" : where ? `Comment on ${where}` : "Comment for the review"),
          comment,
          h("div.small.muted", v.self
            ? (chosen.effect === "blocking" ? "Drafted by the agent. Your agent works from this when you hand it the fixes, or fix it yourself." : "Drafted by the agent. It posts under your name only when you post your notes, so make it yours.")
            : (chosen.comment ? "Drafted by the agent. It goes out under your name, so make it yours. " : "") + "Nothing posts until you sign.")),
        h("div.block.rule-top", { class: "gap-s" },
          h("div.label", "Ask before you decide"),
          asks.map((q) => h("div.block", { class: "gap-s" }, h("div.small", { class: "strong" }, q.question),
            h("div.thread.small", { "data-ask": q.id }, q.pending ? "" : q.answer), q.pending && h("div.small.muted", h("span.live"), "The agent is looking"))),
          h("div.row", { class: "one-line gap-u" }, askInput, h("button.btn.small", { onclick: ask }, "Ask"))),
        rule && h("div.block.rule-top",
          h("div.small", "Raised by your rule ", h("b", rule.id), ": ", rule.text),
          h("div.row", { class: "mid gap-s" }, h("span.small.muted", "Right to stop you?"), fb("useful", "Yes"), fb("not-here", "Not in this repo"), fb("stop", "Stop raising it"))),
        !rule && d.proposedRule && h("div.block.rule-top",
          h("div.small", "No rule of yours raised this. Want to be stopped here again?"),
          h("div.small", { class: "strong" }, d.proposedRule.text),
          h("div", h("button.btn.small", { onclick: makeRule }, "Make it a rule, on trial"))),
        a.option && h("a.btn.primary", { href: next ? href(v.key, "d", next.id) : href(v.key, "signoff") },
          next ? `Next: ${W.decisions} ${v.brief.decisions.indexOf(next) + 1} →` : `On to ${W.toSignoff} →`),
        h("div.small.muted", h("span.kbd", "1"), " to ", h("span.kbd", String(d.options.length)), " choose"))));
}

// --- sign-off -------------------------------------------------------------------------
function signoff(S, v) {
  const d = S.draft, b = v.brief;
  if (!d) return h("p", "Loading");
  const comments = (d.comments || []).map((c) => ({ ...c }));
  const bodyBox = h("textarea.field", { rows: 3, value: d.body || "", "aria-label": "Review summary",
    placeholder: "Anything you want to say in your own words. Comments that cannot sit on a line are added on their own." });
  // Whether the receipt goes in: the setting decides the default, and the
  // reviewer can change it for this one review.
  if (!S.receipt || S.receipt.key !== v.key) S.receipt = { key: v.key, on: !!d.includeReceipt };
  const decN = (id) => b.decisions.findIndex((x) => x.id === id) + 1;

  return h("div.cols",
    h("div.main",
      h("div.block", h("h1.title", `You are signing ${plural(b.decisions.length, "decision")}, not ${plural(v.pr.files || Object.values(v.sectionDiffs).flat().length, "file")}`),
        h("p.lede", "This is what your name goes on. It posts to GitHub as one review, attached to the commit that was read.")),

      h("div.block", h("div.label.ink", "Your decisions"),
        b.decisions.length ? h("div.scroll-x", h("table.reg", h("tbody", b.decisions.map((dec, i) => {
          const a = v.answers[dec.id] || {}, o = dec.options.find((x) => x.id === a.option);
          return h("tr.go", { onclick: () => go(href(v.key, "d", dec.id)) },
            h("td", { class: "fit" }, tri(i + 1, !!o)),
            h("td", h("div", { class: "strong" }, prose(dec.title), newStamp(v, dec.id)), h("div.small.muted", o ? `${o.label}. ${o.detail}` : "Not answered")),
            h("td.r", { class: "fit" }, !o ? h("span.stamp.open", "Open") : o.effect === "blocking" ? h("span.stamp.contra", "Blocking") : o.effect === "ask" ? h("span.stamp.open", "Asked") : h("span.stamp", "Accepted")),
            h("td.r.num.muted", { class: "fit" }, clock(v.seconds[dec.id] || 0)));
        })))) : h("p.muted", "The agent found nothing that needs a person's judgment.")),

      h("div.block", h("div.label.ink", `Comments to post · ${comments.length}`),
        comments.map((c) => h("div.block", { class: "gap-s" },
          h("div.row", { class: "mid gap-u" },
            c.decision && decN(c.decision) ? tri(decN(c.decision), true) : null,
            h("span.stamp" + (c.label === "Blocking" ? ".contra" : c.label === "Question" ? ".open" : ""), c.label),
            c.standard && h("span.stamp.faint", c.standard),
            h("span.small.mono.muted", c.inline ? `${c.path}:${c.line}` : "in the review body")),
          h("textarea.field", { rows: Math.min(8, 2 + Math.ceil(c.body.length / 90)), value: c.body, "aria-label": "Comment", oninput: (e) => { c.body = e.target.value; } }))),
        !comments.length && h("p.muted", "No comments.")),

      h("div.block", h("div.label.ink", "Review summary"), bodyBox),

      h("div.block", h("div.row", { class: "spread mid" }, h("div.label.ink", `Attention receipt · ${clock(d.totalSeconds || 0)} of your time`),
          h("label.check", h("input", { type: "checkbox", checked: S.receipt.on, onchange: (e) => { S.receipt.on = e.target.checked; } }), "Include it in the review")),
        h("div.scroll-x", h("table.reg",
          h("thead", h("tr", h("th", "Part"), h("th.r", "Lines"), h("th", "Covered by"), h("th.r", "Your time"))),
          h("tbody", (d.receipt || []).map((l) => h("tr", h("td", l.title), h("td.r.num", l.lines),
            h("td.small", l.covered), h("td.r.num", clock(l.seconds))))))),
        h("p.small.muted", "Nobody downstream has to guess whether approved meant read. It posts folded, below your summary, with the time as it stands when you post."))),

    h("div.side", verdict(S, v, d, comments, bodyBox)));
}

// --- wrap up (a self-review's sign-off) -------------------------------------------
// wrapUp settles what the author decided: fixes go to their agent or to
// them, notes for reviewers post as one comment review, and nothing is
// approved. Then the pull request's next move.
function wrapUp(S, v) {
  const d = S.draft, b = v.brief, W = words(v);
  if (!d) return h("p", "Loading");
  const key = v.key, p = v.pr;
  const m = (S.st.mine || []).find((x) => `${x.pr.owner}/${x.pr.repo}#${x.pr.number}` === key);
  const t = m && m.tend;
  const tendOff = (S.st.config.tend || "auto") === "off";
  const rows = b.decisions.map((dec, i) => {
    const a = v.answers[dec.id] || {};
    return { dec, n: i + 1, a, o: dec.options.find((x) => x.id === a.option) };
  });
  const open = rows.filter((r) => !r.o).length;
  const fixes = rows.filter((r) => r.o && r.o.effect === "blocking");
  const handed = v.handed && v.handed.sha === v.readSha ? v.handed : null;
  const posted = v.posted && v.posted.sha === v.readSha ? v.posted : null;
  const comments = (d.comments || []).map((c) => ({ ...c }));
  const decN = (id) => b.decisions.findIndex((x) => x.id === id) + 1;
  const hand = (by, msg) => async (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    try { await post("/api/handoff", { key, by }); toast(msg); refresh(true); } catch (e) { toast(e.message); btn.disabled = false; }
  };

  let fixState;
  if (!fixes.length) fixState = null;
  else if (handed && handed.by === "you") fixState = h("p.small", "You're fixing these yourself. When you push, Docket reads it again and says which findings the push resolved.");
  else if (handed) {
    const held = t ? (t.actions || []).filter((a) => a.state === "waiting").length : 0;
    fixState = h("div.pencil",
      h("div.label", "Handed to your agent " + ago(handed.at)),
      t && t.status === "running" ? h("div", h("span.live"), h("span", { "data-tend": key }, t.activity || "Working")) :
        held ? h("div", `${plural(held, "step")} ${held === 1 ? "waits" : "wait"} for your OK. `, h("a", { href: mineHref(key) }, "Check it")) :
          h("div", t && t.summary ? prose(t.summary) : "It's done. ", " ", h("a", { href: mineHref(key) }, "See what it did")),
      h("div.small.muted", { class: "mt-s" }, "Each commit goes through the same gate as any step on your pull requests. When one is pushed, Docket reads it again."));
  } else {
    fixState = h("div.block", { class: "gap-s" },
      h("div.row", { class: "mid gap-u" },
        h("button.btn.primary", { disabled: tendOff || null, title: tendOff ? "The agent is off for your pull requests. Turn it on in Settings." : null,
          onclick: hand("agent", "Your agent is on it.") }, fixes.length === 1 ? "Have the agent fix it" : "Have the agent fix them"),
        h("button.btn.quiet", { onclick: hand("you", "Noted. Push when you're done; Docket reads it again.") }, "I'll fix them myself")),
      h("p.small.muted", "Your agent commits on top of your branch. A small, clearly asked fix goes ahead on its own; anything bigger waits for your OK, with an Undo either way."));
  }

  const confirmRow = h("div.block.hidden", { class: "gap-u well" });
  const askPost = () => {
    const inline = comments.filter((c) => c.inline && c.body.trim()).length;
    confirmRow.replaceChildren(
      h("div", `Post ${plural(comments.length, "note")} as one comment on your pull request${inline ? `, ${inline} on their lines` : ""}?`),
      h("div.row", { class: "mid gap-u" },
        h("button.btn.red", { onclick: (ev) => send(S, v, "COMMENT", "", comments, false, ev) }, "Post notes"),
        h("button.btn.quiet", { onclick: () => confirmRow.classList.add("hidden") }, "Cancel")));
    confirmRow.classList.remove("hidden");
    confirmRow.querySelector("button").focus();
  };

  const next = nextItem(S);
  return h("div.cols",
    h("div.main",
      h("div.block",
        h("h1.title", open ? `${plural(open, "finding")} still to settle` : "Wrap up your self-review"),
        h("p.lede", "What you chose to fix goes to your agent, or stays with you. What you chose to explain posts as notes on your pull request, so reviewers read your reasoning before they have to ask. Nothing here approves anything.")),

      h("div.block", h("div.label.ink", "Your findings"),
        rows.length ? h("div.scroll-x", h("table.reg", h("tbody", rows.map((r) => h("tr.go", { onclick: () => go(href(key, "d", r.dec.id)) },
          h("td", { class: "fit" }, tri(r.n, !!r.o)),
          h("td", h("div", { class: "strong" }, prose(r.dec.headline || r.dec.title), newStamp(v, r.dec.id)), h("div.small.muted", r.o ? `${r.o.label}.` : "Not settled")),
          h("td.r", { class: "fit" }, !r.o ? h("span.stamp.open", "Open") : h("span.stamp" + (r.o.effect === "blocking" ? ".contra" : r.o.effect === "ask" ? ".open" : ""), W[r.o.effect]))))))) :
          h("p.muted", "Your agent found nothing a reviewer would stop on.")),

      fixes.length > 0 && h("div.block", h("div.label.ink", `Fix before review · ${fixes.length}`),
        fixes.map((r) => h("div.block", { class: "gap-s" },
          h("div.row", { class: "mid gap-u" }, tri(r.n, true), h("span", { class: "strong" }, prose(r.dec.headline || r.dec.title)),
            r.dec.anchor && r.dec.anchor.path && h("span.small.mono.muted", `${r.dec.anchor.path}:${r.dec.anchor.line}`)),
          h("div.small", prose(r.a.comment || r.o.comment || r.o.label)))),
        fixState),

      h("div.block", h("div.label.ink", `Notes for reviewers · ${comments.length}`),
        comments.map((c) => h("div.block", { class: "gap-s" },
          h("div.row", { class: "mid gap-u" },
            c.decision && decN(c.decision) ? tri(decN(c.decision), true) : null,
            h("span.small.mono.muted", c.inline ? `${c.path}:${c.line}` : "in the comment body")),
          h("textarea.field", { rows: Math.min(8, 2 + Math.ceil(c.body.length / 90)), value: c.body, "aria-label": "Note", oninput: (e) => { c.body = e.target.value; } }))),
        !comments.length && h("p.muted", "No notes. A finding you choose to leave and explain becomes one."),
        posted ? h("div.small", `Posted ${ago(posted.at)}. `, posted.url && h("a", { href: posted.url, target: "_blank", rel: "noreferrer", onclick: openExternal }, "See them on GitHub"))
          : comments.length > 0 && h("div", h("button.btn", { onclick: askPost }, "Post notes…")),
        confirmRow),

      (d.warnings || []).length > 0 && h("div.block", { class: "gap-s" }, d.warnings.map((w) => reasonLink(v, w, h("span.small.muted", prose(w.text)))))),

    h("div.side", h("div.yours", { class: "sticky" },
      h("div.label", "Then"),
      p.draft && h("button.btn" + (!open && (!fixes.length || handed) ? ".primary" : ""), { onclick: async (ev) => {
        ev.currentTarget.disabled = true;
        if (await act(key, { kind: "ready" })) { toast("Marked ready for review."); refresh(true); } else ev.currentTarget.disabled = false;
      } }, "Mark ready for review"),
      h("a.btn", { href: mineHref(key) }, "Back to your pull request"),
      next && h("div.block", { class: "gap-s rule-top" },
        h("div.label", "Next up"),
        h("div.small", h("span.mono", `${next.pr.owner}/${next.pr.repo} #${next.pr.number}`), next.mine ? " · yours" : ""),
        h("div", { class: "strong" }, next.pr.title),
        h("a.btn.primary", { href: itemHref(next) }, `${next.action} · ${next.minutes} min →`)),
      h("div.small.muted", "When you push, Docket reads it again and carries your answers over for every finding that still stands."))));
}

const EVENT = { APPROVE: "Approve", COMMENT: "Comment only", REQUEST_CHANGES: "Request changes" };

// verdict is the sign-off's red panel. A verdict button only opens a confirm
// row, as Merge does on your own pull requests: nothing posts on one click.
// Once the review is posted, the panel becomes a receipt and the way on to
// the next pull request.
function verdict(S, v, d, comments, bodyBox) {
  const panel = (...kids) => h("div.yours", { class: "sticky" }, kids);
  if (v.status === "posted" && v.posted && S.repost !== v.key) return panel(posted(S, v));
  const locked = (d.locks || []).length > 0, suggested = d.event;
  const confirmRow = h("div.block.hidden", { class: "gap-u well" });
  const ask = (event) => {
    confirmRow.replaceChildren(...confirmPost(S, v, event, comments, () => bodyBox.value, () => S.receipt.on, () => confirmRow.classList.add("hidden")));
    confirmRow.classList.remove("hidden");
    confirmRow.querySelector("button").focus();
  };
  return panel(
    h("div.label", "Verdict"),
    h("button.btn" + (suggested === "APPROVE" ? ".primary" : ""), { disabled: locked, onclick: () => ask("APPROVE") }, locked ? "Approve · locked" : "Approve"),
    h("button.btn" + (suggested === "COMMENT" ? ".primary" : ""), { onclick: () => ask("COMMENT") }, "Comment only"),
    h("button.btn.red", { "aria-pressed": String(suggested === "REQUEST_CHANGES"), onclick: () => ask("REQUEST_CHANGES") }, "Request changes"),
    confirmRow,
    locked && h("div.block", { class: "gap-s" }, h("div.small", "Approve unlocks when none of these remain:"),
      d.locks.map((l) => reasonLink(v, l, h("span.small", l.text)))),
    (d.warnings || []).map((w) => reasonLink(v, w, h("span.small.muted", prose(w.text)))),
    h("div.small.muted", "When the author pushes, Docket reads again and brings you only what changed."));
}

// confirmPost is the confirm row's content: what will post, where, and the
// one button that posts it.
function confirmPost(S, v, event, comments, body, receipt, cancel) {
  const inline = comments.filter((c) => c.inline && c.body.trim()).length;
  const sha = v.readSha ? v.readSha.slice(0, 7) : "the commit that was read";
  return [
    h("div", "Post ", h("i", EVENT[event]), ` with ${inline ? plural(inline, "inline comment") : "no inline comments"} on `, h("span.mono", sha), "?"),
    h("div.row", { class: "mid gap-u" },
      h("button.btn.red", { onclick: (ev) => send(S, v, event, body(), comments, receipt(), ev) }, "Post review"),
      h("button.btn.quiet", { onclick: cancel }, "Cancel")),
  ];
}

// reasonLink makes a lock or warning lead to where it is settled: the
// decision behind it, or the section to spot-check.
function reasonLink(v, r, text) {
  const n = r.decision ? v.brief.decisions.findIndex((x) => x.id === r.decision) + 1 : 0;
  if (n) return h("a.small", { href: href(v.key, "d", r.decision), class: "tri-link" }, tri(n, !!(v.answers[r.decision] || {}).option), text);
  if (r.section) return h("div", text, " ", h("a.small", { href: href(v.key, "diff") + "/" + r.section }, "Spot-check one"));
  return h("div", text);
}

async function send(S, v, event, body, comments, receipt, ev) {
  const btn = ev && ev.currentTarget;
  if (btn) btn.disabled = true;
  try {
    await post("/api/post", { key: v.key, event, body, comments, receipt });
    S.repost = null;
    S.fast = null;
    toast(v.self ? "Your notes are on GitHub." : "Posted to GitHub.");
    // The way on to the next review is on Sign-off.
    if (S.route.sheet !== "signoff") go(href(v.key, "signoff")); else refresh(true);
  } catch (e) {
    toast(e.message);
    if (btn) btn.disabled = false;
  }
}

// posted is the receipt that replaces the verdict buttons, and the way on to
// whatever the inbox has next, review or not.
function posted(S, v) {
  const next = nextItem(S);
  return [
    h("div.label", "Posted"),
    h("div", "You posted ", h("i", EVENT[v.posted.event] || "a review"), " ", ago(v.posted.at), "."),
    v.posted.url && h("a.small", { href: v.posted.url, target: "_blank", rel: "noreferrer", onclick: openExternal }, "See it on GitHub"),
    next
      ? h("div.block", { class: "gap-s rule-top" },
        h("div.label", "Next up"),
        h("div.small", h("span.mono", `${next.pr.owner}/${next.pr.repo} #${next.pr.number}`), next.mine ? " · yours" : ""),
        h("div", { class: "strong" }, next.pr.title),
        h("div.small.text-2", next.why),
        h("a.btn.primary", { href: itemHref(next) }, `${next.action} · ${next.minutes} min →`))
      : h("div.block", { class: "gap-s" }, h("div", { class: "strong" }, "Nothing else to do"), h("a.btn", { href: "#/" }, "Back to the inbox")),
    next && h("div.small.muted", h("span.kbd", "n"), " opens it"),
    h("button.btn.quiet.small", { onclick: () => { S.repost = v.key; refresh(true); } }, "Post another review"),
  ];
}

// nextItem is the first thing in the inbox to do after this review: the
// inbox's own order, so it may be one of the user's pull requests.
export function nextItem(S) {
  return ((S.st && S.st.inbox) || []).find((it) => it.key !== S.route.key && ["quick", "closer", "skim"].includes(it.band)) || null;
}
