// One pull request, as a set of sheets: Brief, Context, one sheet per
// decision, the full Diff in reading order, and Sign-off.

import { h, post, toast, plural, clock, ago, tri, prose, openExternal } from "./dom.js";
import { beforeAfter } from "./viz.js";
import { renderConversation } from "./conversation.js";
import { href, go, refresh } from "./app.js";

const KIND = { judgment: "Needs you", skim: "Verified, skim", mechanical: "Mechanical, proven" };
const CLAIM = { proved: ["Proved", ""], contradicted: ["Contradicted", ".contra"], unproven: ["Unproven", ".open"], unstated: ["Unstated", ".faint"] };

function sheetsOf(v) {
  const b = v.brief;
  if (!b) return [];
  const out = [{ sheet: "brief", label: "Brief" }];
  if (b.context) out.push({ sheet: "context", label: "Context" });
  (b.decisions || []).forEach((d, i) => out.push({ sheet: "d", id: d.id, n: i + 1, label: `${i + 1}`, decision: d }));
  out.push({ sheet: "diff", label: "Diff" });
  out.push({ sheet: "conversation", label: "Conversation" });
  out.push({ sheet: "signoff", label: "Sign-off" });
  return out;
}
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
  if (e.key === "ArrowRight" || e.key === "n") to(i + 1);
  else if (e.key === "ArrowLeft" || e.key === "p") to(i - 1);
  else if (S.route.sheet === "d" && /^[1-9]$/.test(e.key)) {
    const d = v.brief.decisions.find((x) => x.id === S.route.id);
    const o = d && d.options[Number(e.key) - 1];
    if (o) answer(v, d, { option: o.id });
  }
}

async function answer(v, d, patch) {
  const cur = v.answers[d.id] || {};
  const next = { option: cur.option || "", comment: cur.comment || "", feedback: cur.feedback || "", ...patch };
  // Choosing a different option starts from that option's own draft.
  if (patch.option && patch.option !== cur.option) next.comment = "";
  try { await post("/api/answer", { key: v.key, decision: d.id, ...next }); refresh(true); }
  catch (e) { toast(e.message); }
}

// --- frame ------------------------------------------------------------------
export function renderReview(S) {
  const v = S.review, p = v.pr, b = v.brief;
  const list = sheetsOf(v), i = at(S, list);
  const cell = (label, value, cls) => h("div" + (cls || ""), h("span.label", label), h("span.cell-v", value));
  const head = h("div.titleblock",
    h("a.brand", { href: "#/", title: "Back to the queue" }, "Docket"),
    cell("Pull request", `${p.owner}/${p.repo} #${p.number}`),
    cell("Title", p.title, ".wide"),
    cell("Author", p.author),
    cell("Read at", v.readSha ? v.readSha.slice(0, 7) : "not yet"),
    cell("Sheet", list.length ? `${i + 1} of ${list.length}` : "none"));

  const tabs = b && h("nav.sheets", { "aria-label": "Sheets" },
    list.map((s) => h("a", { href: href(v.key, s.sheet, s.id), "aria-current": s === list[i] ? "page" : null, title: s.decision ? s.decision.title : null },
      s.decision ? [tri(s.n, !!(v.answers[s.id] && v.answers[s.id].option)), h("span", { style: { maxWidth: "16ch", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textTransform: "none", fontFamily: "var(--body)", letterSpacing: 0, fontSize: "13px" } }, s.decision.title)] : s.label)),
    h("span.spacer"),
    h("a", { href: p.url || "#", target: "_blank", rel: "noreferrer", onclick: openExternal }, "On GitHub"));

  let body;
  if (!b) body = waiting(v);
  else if (S.route.sheet === "context" && b.context) body = context(v);
  else if (S.route.sheet === "d") body = decision(v, list[i]);
  else if (S.route.sheet === "diff") body = fullDiff(v);
  else if (S.route.sheet === "signoff") body = signoff(S, v);
  else if (S.route.sheet === "conversation") body = S.conv ? renderConversation(v.key, S.conv, S.st.queue.viewer, v.pr.author, () => refresh(true)) : h("p.muted", "Could not read the conversation.");
  else body = brief(v, list);

  const banners = [
    v.summary.pushed && v.status !== "reading" && v.status !== "queued" && h("div.banner.red", "The author pushed since this was read. A fresh read is queued; your answers carry over for every decision that still stands."),
    b && (v.status === "reading" || v.status === "queued") && h("div.banner", h("span.live"), "Reading again: ", h("span", { "data-activity": v.key }, v.activity || "queued"), ". What you see is the earlier read."),
    b && b.since && h("div.banner", h("b", "Since your last look: "), prose(b.since.summary)),
    v.status === "posted" && v.posted && h("div.banner", `You posted this review ${ago(v.posted.at)} (${v.posted.event.toLowerCase().replace("_", " ")}). `, h("a", { href: v.posted.url, target: "_blank", rel: "noreferrer", onclick: openExternal }, "See it on GitHub")),
  ];
  return h("div.sheet", head, tabs, h("div.body", banners, body, b && pager(v, list, i)));
}

function pager(v, list, i) {
  const prev = list[i - 1], next = list[i + 1];
  const name = (s) => s.decision ? `Decision ${s.n}` : s.label;
  return h("div.row.rule-top", { style: { justifyContent: "space-between", alignItems: "center" } },
    prev ? h("a.btn.quiet", { href: href(v.key, prev.sheet, prev.id) }, "← " + name(prev)) : h("span"),
    h("span.small.muted", h("span.kbd", "←"), " ", h("span.kbd", "→"), " move between sheets"),
    next ? h("a.btn.primary", { href: href(v.key, next.sheet, next.id) }, name(next) + " →") : h("span"));
}

function waiting(v) {
  if (v.status === "failed") {
    return h("div.block",
      h("h1.title", "This one could not be read"),
      h("pre.code.out", v.error),
      h("div.row", h("button.btn.primary", { onclick: () => post("/api/reread", { key: v.key }).then(() => refresh(true)) }, "Try again"),
        h("button.btn.quiet", { onclick: () => post("/api/remove", { key: v.key }).then(() => go("#/")) }, "Remove from the queue")));
  }
  return h("div.block",
    h("h1.title", "Your agent is reading this"),
    h("p.lede", "It has the branch and the commit it forked from checked out side by side. It runs both, checks what the description claims, applies your rulebook, and comes back with what it proved, what it could not, and where you are needed."),
    h("div.pencil", h("div.label", "Now"), h("div", h("span.live"), h("span", { "data-activity": v.key }, v.activity || "Queued"))),
    h("p.small.muted", "You can leave. Docket tells you when it is ready."));
}

// --- brief --------------------------------------------------------------------
function brief(v, list) {
  const b = v.brief, sum = v.summary;
  const total = b.sections.reduce((n, s) => n + s.lines, 0) || 1;
  const by = (k) => b.sections.filter((s) => s.kind === k).reduce((n, s) => n + s.lines, 0);
  const first = list[1];
  const decN = (id) => b.decisions.findIndex((d) => d.id === id) + 1;

  return h("div.cols",
    h("div.main",
      h("div.block",
        h("h1.title", v.pr.title),
        h("p.lede", prose(b.summary)),
        h("div.row", { style: { alignItems: "center" } },
          h("a.btn.primary", { href: href(v.key, first.sheet, first.id) }, first.sheet === "context" ? "Start with the context" : first.decision ? "Start with decision 1" : "Read the diff"),
          h("span.small.muted", sum.decisions ? `${plural(sum.decisions, "decision")}, about ${Math.max(sum.minutes, 1)} min` : "No decisions for you"))),

      h("div.block",
        h("div.label.ink", `Where the ${total} changed lines go`),
        h("div.attn", { role: "img", "aria-label": `${by("judgment")} lines need you, ${by("skim")} verified, ${by("mechanical")} mechanical` },
          b.sections.map((s) => h("div.fill-" + s.kind, { style: { flex: `${Math.max(s.lines, 1)} 0 0` }, title: `${s.title}: ${s.lines} lines` }))),
        h("div.row.small", { style: { gap: "8px 24px" } },
          h("span", h("span.key-sw.fill-judgment"), `Needs you: ${by("judgment")}`),
          h("span", h("span.key-sw.fill-skim"), `Verified, worth a skim: ${by("skim")}`),
          h("span", h("span.key-sw.fill-mechanical"), `Mechanical, proven: ${by("mechanical")}`))),

      h("div.block",
        h("div.label.ink", "What the pull request claims, and what held up"),
        h("div.scroll-x", h("table.reg", h("tbody", (b.claims || []).map((c) => {
          const [word, cls] = CLAIM[c.status] || [c.status, ""];
          return h("tr",
            h("td", { style: { width: "1%" } }, h("span.stamp" + cls, word)),
            h("td", h("div", { style: { fontWeight: 700 } }, prose(c.text)), h("div.small.muted", prose(c.evidence))),
            h("td.r", { style: { width: "1%", whiteSpace: "nowrap" } }, c.decision && decN(c.decision) ? h("a", { href: href(v.key, "d", c.decision), style: { display: "inline-flex", gap: "6px", alignItems: "center" } }, tri(decN(c.decision), !!(v.answers[c.decision] || {}).option), "your call") : ""));
        })))),
        !(b.claims || []).length && h("p.muted", "The description makes no checkable claims.")),

      h("div.block",
        h("div.label.ink", "Reading order, by intent"),
        h("div.scroll-x", h("table.reg",
          h("thead", h("tr", h("th", ""), h("th", "Part"), h("th", "How it was covered"), h("th.r", "Lines"))),
          h("tbody", b.sections.map((s, i) => h("tr.go", { onclick: () => go(href(v.key, "diff") + "/" + s.id) },
            h("td.num", i + 1),
            h("td", h("div", { style: { fontWeight: 700 } }, s.title), h("div.small.muted.mono", s.files.map((f) => f.path).join(", "))),
            h("td.small", s.kind === "judgment" ? h("span.stamp.red", "Needs you") : [h("span.stamp" + (s.kind === "mechanical" ? ".faint" : ".open"), KIND[s.kind]), h("div.muted", { style: { marginTop: "4px" } }, prose(s.proof))]),
            h("td.r.num", s.lines)))))))),

    h("div.side",
      h("div.block",
        h("div.label.ink", "Why you are here"),
        h("div", (v.pr.why || []).join(", ") || "added by hand"),
        v.slice && v.slice.length ? h("div.small.muted", `${v.slice.length} of the changed files fall to you by CODEOWNERS. The rest are in sections marked outside your slice.`) : null),
      h("div.block",
        h("div.label.ink", "Your rules on this change"),
        (b.ruleHits || []).length ? b.ruleHits.map((r) => {
          const rule = v.rules[r.rule];
          return h("div.small", { style: { display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 10px" } },
            h("span.stamp" + (r.result === "violated" ? ".red" : r.result === "passed" ? "" : ".open"), { style: { alignSelf: "start" } }, r.rule === "repo" ? "Repo" : r.rule),
            h("div", rule ? h("div", { style: { fontWeight: 700 } }, rule.text) : r.rule === "repo" ? h("div", { style: { fontWeight: 700 } }, "The repository's own conventions") : null,
              h("div.muted", prose(r.note))));
        }) : h("div.small.muted", "None of your rules applies to this change."),
        h("a.small", { href: "#/rules" }, "Open the rulebook")),
      (b.uncovered || []).length ? h("div.block",
        h("div.label.ink", "Nothing looked at"),
        h("ul.small", { style: { margin: 0, paddingLeft: "18px" } }, b.uncovered.map((u) => h("li", prose(u))))) : null,
      h("div.pencil",
        h("div.label", `What ${v.agent || "the agent"} did`),
        h("div.small", { style: { display: "grid", gridTemplateColumns: "1fr auto", gap: "4px 12px" } },
          (b.work || []).map((w) => [h("span", prose(w.did)), h("span.mono.muted", w.result)])),
        h("div.small.muted", { style: { marginTop: "8px" } },
          `${v.model || ""}${v.readTookSeconds ? ` · took ${clock(v.readTookSeconds)}` : ""} · ${ago(v.readAt)}. It gives no verdict.`,
          v.isolation === "soft" ? " It used its own tools, so Docket's directory limits were not enforced." : ""),
        h("button.btn.quiet.small", { style: { marginTop: "8px" }, onclick: () => post("/api/reread", { key: v.key }).then(() => refresh(true)) }, "Read it again"))));
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
          h("div.row", { style: { justifyContent: "space-between", gap: "8px" } }, h("span.mono", a.area), h("span.muted", a.reviews ? `${plural(a.reviews, "review")}, ${ago(a.last)}` : "not reviewed here")),
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
  return h("div.block", { style: { gap: "40px" } },
    h("div.block", h("h1.title", "The whole change, in reading order"),
      h("p.lede", "Sections that need you are open. The rest are closed with the agent's proof on the lid; open one to skim it, or spot-check a hunk to keep the proof honest.")),
    b.sections.map((s, i) => {
      const fds = v.sectionDiffs[s.id] || [];
      const closed = s.kind !== "judgment" && want !== s.id;
      const hunks = fds.flatMap((fd) => (fd.hunks || []).map((hk) => ({ ...fd, hunks: [hk] })));
      const sample = h("div");
      const spot = () => {
        const pick = hunks[Math.floor(Math.random() * hunks.length)];
        sample.replaceChildren(h("div.block", h("div.label", "One hunk, picked at random"), diffBlock(pick),
          h("label.check", h("input", { type: "checkbox", checked: !!v.audited[s.id], onchange: (e) => post("/api/audit", { key: v.key, section: s.id, done: e.target.checked }) }),
            "I checked this hunk and the proof holds for it")));
      };
      const lid = h("div.row", { style: { alignItems: "baseline", gap: "12px" } },
        h("span.num.muted", i + 1),
        h("div.grow", h("h2.h2", s.title), h("div.small.muted", prose(s.why))),
        s.kind === "judgment" ? h("span.stamp.red", "Needs you") : h("span.stamp" + (s.kind === "mechanical" ? ".faint" : ".open"), KIND[s.kind]),
        h("span.num.small.muted", plural(s.lines, "line")));
      const decs = b.decisions.map((d, n) => [d, n + 1]).filter(([d]) => d.section === s.id);
      return h("section.block.rule-top", { "data-section": s.id },
        lid,
        decs.length ? h("div.row", { style: { gap: "8px 16px" } }, decs.map(([d, n]) => h("a.small", { href: href(v.key, "d", d.id), style: { display: "inline-flex", gap: "6px", alignItems: "center" } }, tri(n, !!(v.answers[d.id] || {}).option), d.title))) : null,
        s.kind !== "judgment" && h("div.pencil", h("div.label", "Proof"), h("div", prose(s.proof)),
          hunks.length ? h("div.row", { style: { marginTop: "8px", alignItems: "center" } },
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
    h("div.row", { style: { justifyContent: "space-between", alignItems: "baseline", marginBottom: "8px" } },
      h("div.label", { style: { color: "var(--pencil)" } }, `${e.kind === "probe" ? "Probe the agent wrote and ran" : e.kind === "run" ? "The agent ran" : e.kind === "search" ? "The agent searched" : "The agent read"}: ${e.title}`),
      e.passed === false ? h("span.stamp.solid", "Fails") : e.passed === true ? h("span.stamp", "Passes") : null),
    e.code && h("pre.code", e.code),
    e.command && h("div.small.mono.muted", { style: { margin: "6px 0" } }, "$ " + e.command),
    e.output && h("pre.code.out", e.output),
    e.file && h("div.small.muted", { style: { marginTop: "6px" } }, "Saved as ", h("span.mono", e.file), " in this review's folder, so it can be rerun.")));

  const asks = (v.asks || []).filter((q) => q.decision === d.id);
  const askInput = h("input.field", { placeholder: "Ask the agent to check something. It answers with a run, a search or a file reference.", "aria-label": "Question for the agent" });
  const ask = async () => {
    if (!askInput.value.trim()) return;
    try { await post("/api/ask", { key: v.key, decision: d.id, question: askInput.value }); askInput.value = ""; refresh(true); }
    catch (e) { toast(e.message); }
  };
  askInput.addEventListener("keydown", (e) => { if (e.key === "Enter") ask(); });

  const comment = chosen && h("textarea.field", { rows: 5, "aria-label": "Comment to post", value: a.comment || chosen.comment || "",
    onchange: (e) => answer(v, d, { comment: e.target.value === chosen.comment ? "" : e.target.value }) });

  const fb = (id, label) => h("button.btn.small" + (a.feedback === id ? ".primary" : ""), { onclick: () => answer(v, d, { feedback: id }) }, label);
  const makeRule = async () => {
    try {
      const r = await post("/api/rule", { text: d.proposedRule.text, applies: d.proposedRule.applies, scope: `${v.pr.owner}/${v.pr.repo}`, severity: chosen && chosen.effect === "blocking" ? "blocking" : "note", status: "trial",
        origin: `Drafted from your decision on ${v.pr.owner}/${v.pr.repo} #${v.pr.number}`, why: "drafted from a decision, on trial" });
      toast(`${r.id} added, on trial in ${v.pr.owner}/${v.pr.repo} only.`);
    } catch (e) { toast(e.message); }
  };

  return h("div.cols",
    h("div.main",
      h("div.row", { style: { flexWrap: "nowrap", gap: "16px" } }, tri(sheet.n, !!a.option, true),
        h("div.block", h("h1.statement", prose(d.title)), h("p.lede", prose(d.detail)))),
      beforeAfter(d.beforeAfter, (f) => outURL(v, f)),
      (v.hotDiffs[d.id] || []).length ? h("div.block", h("div.label.ink", "The lines that matter"), (v.hotDiffs[d.id] || []).map(diffBlock),
        h("div.small.muted", "A red rule in the margin marks them. ", h("a", { href: href(v.key, "diff") + "/" + d.section }, "See the whole section"))) : null,
      evidence.length ? h("div.block", h("div.label.ink", "Evidence"), evidence) : null,
      h("div.block", h("div.label.ink", "Ask before you decide"),
        asks.map((q) => h("div.block", { style: { gap: "4px" } }, h("div", { style: { fontWeight: 700 } }, q.question),
          h("div.thread", { "data-ask": q.id }, q.pending ? "" : q.answer), q.pending && h("div.small.muted", h("span.live"), "The agent is looking"))),
        h("div.row", { style: { flexWrap: "nowrap", gap: "8px" } }, askInput, h("button.btn", { onclick: ask }, "Ask")))),

    h("div.side.wide",
      h("div.yours", { style: { position: "sticky", top: "16px" } },
        h("div.label", "Your call"),
        h("div", { style: { font: "700 17px/1.3 var(--body)" } }, prose(d.question)),
        h("div.small.muted", "The agent cannot decide this."),
        h("div.block", { style: { gap: "8px" } }, d.options.map((o, i) =>
          h("button.option", { "aria-pressed": String(a.option === o.id), onclick: () => answer(v, d, { option: a.option === o.id ? "" : o.id }) },
            h("span.key", i + 1), h("span", h("b", o.label), h("small", prose(o.detail)),
              h("span.stamp" + (o.effect === "blocking" ? ".red" : o.effect === "ask" ? ".open" : ""), o.effect === "blocking" ? "Blocks the merge" : o.effect === "ask" ? "Stays open" : "Accepts it"))))),
        chosen && h("div.block",
          h("div.label", chosen.effect === "accept" ? "Note to post (optional)" : "Comment to post"),
          comment,
          h("div.small.muted", d.anchor && d.anchor.path ? ["Attaches to ", h("span.mono", `${d.anchor.path}:${d.anchor.line}`), ". Nothing is posted until you sign."] : "Goes in the review body. Nothing is posted until you sign.")),
        rule && h("div.block.rule-top",
          h("div.small", "Raised by your rule ", h("b", rule.id), ": ", rule.text),
          h("div.row", { style: { gap: "6px", alignItems: "center" } }, h("span.small.muted", "Right to stop you?"), fb("useful", "Yes"), fb("not-here", "Not in this repo"), fb("stop", "Stop raising it"))),
        !rule && d.proposedRule && h("div.block.rule-top",
          h("div.small", "No rule of yours raised this. Want to be stopped here again?"),
          h("div.small", { style: { fontWeight: 700 } }, d.proposedRule.text),
          h("div", h("button.btn.small", { onclick: makeRule }, "Make it a rule, on trial"))),
        h("div.small.muted", h("span.kbd", "1"), " to ", h("span.kbd", String(d.options.length)), " choose"))));
}

// --- sign-off -------------------------------------------------------------------------
function signoff(S, v) {
  const d = S.draft, b = v.brief;
  if (!d) return h("p", "Loading");
  const comments = (d.comments || []).map((c) => ({ ...c }));
  const bodyBox = h("textarea.field", { rows: Math.min(14, 3 + (d.body || "").split("\n").length), value: d.body || "", "aria-label": "Review summary" });
  const locked = (d.locks || []).length > 0;
  const send = async (event) => {
    try {
      const r = await post("/api/post", { key: v.key, event, body: bodyBox.value, comments });
      toast("Posted."); refresh(true); if (r.url) console.log(r.url);
    } catch (e) { toast(e.message); }
  };
  const decN = (id) => b.decisions.findIndex((x) => x.id === id) + 1;
  const suggested = d.event;

  return h("div.cols",
    h("div.main",
      h("div.block", h("h1.title", `You are signing ${plural(b.decisions.length, "decision")}, not ${plural(v.pr.files || Object.values(v.sectionDiffs).flat().length, "file")}`),
        h("p.lede", "This is what your name goes on. It posts to GitHub as one review, attached to the commit that was read.")),

      h("div.block", h("div.label.ink", "Your decisions"),
        b.decisions.length ? h("div.scroll-x", h("table.reg", h("tbody", b.decisions.map((dec, i) => {
          const a = v.answers[dec.id] || {}, o = dec.options.find((x) => x.id === a.option);
          return h("tr.go", { onclick: () => go(href(v.key, "d", dec.id)) },
            h("td", { style: { width: "1%" } }, tri(i + 1, !!o)),
            h("td", h("div", { style: { fontWeight: 700 } }, prose(dec.title)), h("div.small.muted", o ? `${o.label}. ${o.detail}` : "Not answered")),
            h("td.r", { style: { width: "1%" } }, !o ? h("span.stamp.open", "Open") : o.effect === "blocking" ? h("span.stamp.contra", "Blocking") : o.effect === "ask" ? h("span.stamp.open", "Asked") : h("span.stamp", "Accepted")),
            h("td.r.num.muted", { style: { width: "1%" } }, clock(v.seconds[dec.id] || 0)));
        })))) : h("p.muted", "The agent found nothing that needs a person's judgment.")),

      h("div.block", h("div.label.ink", `Comments to post · ${comments.length}`),
        comments.map((c) => h("div.block", { style: { gap: "6px" } },
          h("div.row", { style: { gap: "8px", alignItems: "center" } },
            c.decision && decN(c.decision) ? tri(decN(c.decision), true) : null,
            h("span.stamp" + (c.label === "Blocking" ? ".contra" : c.label === "Question" ? ".open" : ""), c.label),
            c.standard && h("span.stamp.faint", c.standard),
            h("span.small.mono.muted", c.inline ? `${c.path}:${c.line}` : "in the review body")),
          h("textarea.field", { rows: Math.min(8, 2 + Math.ceil(c.body.length / 90)), value: c.body, "aria-label": "Comment", oninput: (e) => { c.body = e.target.value; } }))),
        !comments.length && h("p.muted", "No comments.")),

      h("div.block", h("div.label.ink", "Review summary"), bodyBox),

      h("div.block", h("div.label.ink", `Attention receipt · ${clock(d.totalSeconds || 0)} of your time`),
        h("div.scroll-x", h("table.reg",
          h("thead", h("tr", h("th", "Part"), h("th.r", "Lines"), h("th", "Covered by"), h("th.r", "Your time"))),
          h("tbody", (d.receipt || []).map((l) => h("tr", h("td", l.title), h("td.r.num", l.lines),
            h("td.small", l.covered), h("td.r.num", clock(l.seconds))))))),
        h("p.small.muted", "Nobody downstream has to guess whether approved meant read."))),

    h("div.side",
      h("div.yours", { style: { position: "sticky", top: "16px" } },
        h("div.label", "Verdict"),
        h("button.btn.red", { "aria-pressed": String(suggested === "REQUEST_CHANGES"), onclick: () => send("REQUEST_CHANGES") }, "Request changes"),
        h("button.btn" + (suggested === "COMMENT" ? ".primary" : ""), { onclick: () => send("COMMENT") }, "Comment only"),
        h("button.btn" + (suggested === "APPROVE" ? ".primary" : ""), { disabled: locked, onclick: () => send("APPROVE") }, locked ? "Approve · locked" : "Approve"),
        locked && h("div.block", { style: { gap: "4px" } }, h("div.small", "Approve unlocks when none of these remain:"),
          h("ul.small", { style: { margin: 0, paddingLeft: "18px" } }, d.locks.map((l) => h("li", l)))),
        (d.warnings || []).map((w) => h("div.small.muted", prose(w))),
        h("div.small.muted", "When the author pushes, Docket reads again and brings you only what changed."))));
}
