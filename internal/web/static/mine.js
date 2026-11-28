// One of the user's own pull requests: what blocks it, its checks, its
// conversation and the controls to move it along. The list is the inbox's.

import { h, get, post, toast, ago, openExternal, byline, prose, plural, tri } from "./dom.js";
import { renderConversation, act } from "./conversation.js";
import { refresh, href } from "./app.js";
import { renderTend, heldCards, startTend } from "./tend.js";

export function mineHref(key) {
  const m = key.match(/^(.+)\/(.+)#(\d+)$/);
  return `#/m/${m[1]}/${m[2]}/${m[3]}`;
}

const LANE_STAMP = { "needs-you": ["Needs you", ".red"], ready: ["Ready to merge", ".solid"], waiting: ["Waiting", ".open"] };

const failed = (c) => c.status === "COMPLETED" && ["FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR"].includes(c.conclusion);
const pending = (c) => c.status !== "COMPLETED";

function duration(c) {
  if (!c.startedAt || c.startedAt.startsWith("0001")) return "";
  const end = c.completedAt && !c.completedAt.startsWith("0001") ? new Date(c.completedAt) : new Date();
  const s = Math.max(0, Math.round((end - new Date(c.startedAt)) / 1000));
  return s < 60 ? `${s} s` : `${Math.floor(s / 60)} min ${s % 60} s`;
}

function checkStamp(c) {
  if (pending(c)) return h("span.stamp.open", c.status === "QUEUED" ? "Queued" : "Running");
  if (failed(c)) return h("span.stamp.red", c.conclusion.toLowerCase().replace("_", " "));
  return h("span.stamp" + (c.conclusion === "SUCCESS" ? "" : ".faint"), c.conclusion.toLowerCase());
}

function checks(m) {
  const list = m.checks || [];
  if (!list.length) return h("p.muted", "No checks ran on the head commit.");
  const key = m.pr.owner + "/" + m.pr.repo + "#" + m.pr.number;
  const runs = [...new Set(list.filter((c) => failed(c) && c.runId).map((c) => c.runId))];
  const rows = [];
  for (const c of [...list].sort((a, b) => failed(b) - failed(a) || pending(b) - pending(a))) {
    rows.push(h("tr",
      h("td", h("b", c.name), c.workflow && h("div.small.muted", c.workflow), c.required && h("div.small.muted", "required")),
      h("td", checkStamp(c)),
      h("td.num.small", duration(c)),
      h("td.r", c.url && h("a.small", { href: c.url, target: "_blank", rel: "noreferrer", onclick: openExternal }, "Details"))));
    if (failed(c) && c.jobId) {
      const out = h("pre.code.out", { style: { maxHeight: "360px", overflow: "auto" } }, "Loading the log…");
      let loaded = false;
      const d = h("details", {
        ontoggle: async () => {
          if (!d.open || loaded) return;
          loaded = true;
          try { out.textContent = (await get(`/api/log?key=${encodeURIComponent(key)}&job=${c.jobId}`)).log || "The log is empty."; }
          catch (e) { out.textContent = e.message; loaded = false; }
        },
      }, h("summary.small", "The end of the failed log"), out);
      rows.push(h("tr", h("td", { colspan: 4 }, d)));
    }
  }
  return h("div",
    h("div.scroll-x", h("table.reg", h("tbody", rows))),
    runs.length > 0 && h("div.row", { class: "mt" },
      runs.map((run) => h("button.btn.small", { onclick: async () => { if (await act(key, { kind: "rerun", run })) refresh(true); } }, "Re-run failed jobs"))));
}

// controls is the author's own panel: only what GitHub would let them do now.
function controls(S, m) {
  const key = m.pr.owner + "/" + m.pr.repo + "#" + m.pr.number, st = m.state, p = m.pr;
  const methods = st.methods || [];
  const ready = (m.blockers || []).length === 0;
  const pick = h("select.field", { "aria-label": "Merge method", class: "w-auto" },
    methods.map((x) => h("option", { value: x, selected: x === "SQUASH" || null }, { MERGE: "Merge commit", SQUASH: "Squash and merge", REBASE: "Rebase and merge" }[x] || x)));
  const confirmRow = h("div.row.hidden", { class: "mid gap-u" });
  const mergeBtn = ready && methods.length > 0 && h("button.btn.red", {
    onclick: () => {
      confirmRow.replaceChildren(
        h("span", `Merge ${p.headRef} into ${p.baseRef} with ${pick.selectedOptions[0].textContent.toLowerCase()}?`),
        h("button.btn.red", { onclick: async () => { if (await act(key, { kind: "merge", method: pick.value, confirm: true })) { toast("Merged."); location.hash = "#/"; } } }, "Confirm merge"),
        h("button.btn.quiet", { onclick: () => confirmRow.classList.add("hidden") }, "Cancel"));
      confirmRow.classList.remove("hidden");
    },
  }, "Merge");
  const reviewers = [...new Set((m.conversation.reviews || []).map((r) => r.author).filter((a) => a !== p.author))];
  const who = h("input.field.mono", { value: reviewers.join(", "), placeholder: "logins, comma separated", "aria-label": "Reviewers to ask" });
  const doit = (action, msg) => async () => { if (await act(key, action)) { if (msg) toast(msg); refresh(true); } };
  // Merge shows only when nothing blocks it; until then the move is Merge
  // when ready, where the repository allows auto-merge.
  const autoBtn = st.autoMergeAllowed && !ready && methods.length > 0 && (st.autoMerge
    ? h("button.btn", { onclick: doit({ kind: "auto-merge", on: false }, "Auto-merge is off.") }, "Turn off auto-merge")
    : h("button.btn", { onclick: () => doit({ kind: "auto-merge", on: true, method: pick.value }, "GitHub will merge it once nothing blocks it.")() }, "Merge when ready"));
  return h("div.yours",
    h("span.label", "Actions"),
    heldCards(S),
    (mergeBtn || autoBtn) ? h("div.row", { class: "mid gap-u" }, !st.autoMerge && pick, mergeBtn, autoBtn)
      : !ready && h("p.small.muted", "Merge opens once nothing blocks it. The blockers each have their move on the left."),
    st.autoMerge && h("p.small.muted", `Auto-merge is on (${st.autoMerge.toLowerCase()}): GitHub merges it once nothing blocks it.`),
    confirmRow,
    h("div.row", { class: "one-line gap-u mt" }, who,
      h("button.btn", { onclick: () => doit({ kind: "request-review", logins: who.value.split(",").map((s) => s.trim()).filter(Boolean) }, "Review requested.")() }, "Ask for review")));
}

// blockerMoves gives a blocker the action that clears it, where there is one.
function blockerMoves(S, m, key, b) {
  const t = m.tend, mode = S.st.config.tend || "auto";
  const held = t && (t.actions || []).some((a) => a.state === "waiting");
  const canTend = mode !== "off" && !(t && t.status === "running") && !held;
  const agent = canTend && h("button.btn.small", { onclick: (ev) => startTend(S, key, ev) }, "Let the agent fix it");
  const doit = (action, msg, label) => h("button.btn.small", { onclick: async (ev) => {
    ev.currentTarget.disabled = true;
    if (await act(key, action)) { if (msg) toast(msg); refresh(true); } else ev.currentTarget.disabled = false;
  } }, label);
  const jump = (sels, label) => h("button.btn.small.quiet", { onclick: () => {
    const el = [sels].flat().map((x) => document.querySelector(x)).find(Boolean);
    if (el) { el.scrollIntoView({ block: "center" }); const f = el.querySelector("button, textarea, summary"); if (f) f.focus({ preventScroll: true }); }
  } }, label);
  switch (b.kind) {
  case "draft": return doit({ kind: "ready" }, "Marked ready for review.", "Mark ready");
  case "behind": return [doit({ kind: "update-branch" }, "Branch updated from base.", "Update branch"), agent];
  case "conflicts": return agent;
  case "checks-failing": {
    const runs = [...new Set((m.checks || []).filter((c) => failed(c) && c.runId).map((c) => c.runId))];
    return [runs.length > 0 && h("button.btn.small", { onclick: async (ev) => {
      ev.currentTarget.disabled = true;
      let ok = true;
      for (const run of runs) ok = (await act(key, { kind: "rerun", run })) && ok;
      if (ok) { toast("Re-running the failed jobs."); refresh(true); } else ev.currentTarget.disabled = false;
    } }, "Re-run failed jobs"), agent];
  }
  case "changes-requested":
  case "unresolved-threads": {
    return [b.onYou && jump(["[data-on-you]", "div[id^=thread-]"], "Go to the thread"), b.onYou && agent];
  }
  case "review-required": return jump("[aria-label='Reviewers to ask']", "Ask for review");
  default: return null;
  }
}

const EFFECT = { blocking: ["Fix first", ".contra"], ask: ["Note for reviewers", ".open"], accept: ["Fine as is", ""] };

// selfBlock is what the change does and what a reviewer would stop on, from
// the agent's self-review of it: the top of the page, since it is what the
// author should see before anyone else does.
function selfBlock(S) {
  const m = S.mine, p = m.pr, key = p.owner + "/" + p.repo + "#" + p.number, v = S.self;
  const mode = S.st.config.selfReview || "auto";
  const ask = (label, primary) => h("button.btn.small" + (primary ? ".primary" : ""), { onclick: async (ev) => {
    const btn = ev.currentTarget;
    btn.disabled = true;
    try { await post("/api/self", { key }); toast("Your agent is reviewing it."); refresh(true); } catch (e) { toast(e.message); btn.disabled = false; }
  } }, label);
  const head = (...right) => h("div.row", { class: "spread base" }, h("h2.h2", "What it changes"), h("div.row", { class: "mid gap-u" }, right));

  if (!v) {
    if (mode === "off") {
      return h("div.block", head(),
        h("p.small.muted", "Self-review is off. Turned on, your agent reviews your pull requests the way it reviews other people's, before anyone else looks. ",
          h("a", { href: "#/settings/policy" }, "Settings")));
    }
    return h("div.block", head(ask("Self-review it", true)),
      h("p", "Your agent can review this the way it reviews other people's: what it changes, whether the description holds up, and what a reviewer would stop on, so you settle it before anyone else spends time on it."));
  }
  const b = v.brief, busy = v.status === "queued" || v.status === "reading";
  const live = busy && h("div.banner", h("span.live"), b ? "Reading it again: " : "Your agent is reviewing it: ", h("span", { "data-activity": key }, v.activity || "queued"));
  if (!b) {
    return h("div.block", head(v.status === "failed" && ask("Try again")),
      live,
      v.status === "failed" && h("div.banner.red", h("b", "The self-review failed: "), v.error));
  }
  const decs = b.decisions || [];
  const rows = decs.map((d, i) => {
    const a = v.answers[d.id] || {};
    return { d, n: i + 1, o: d.options.find((x) => x.id === a.option) };
  });
  const open = rows.filter((r) => !r.o);
  const s = v.summary;
  const wrap = !open.length && ((s.fixes > 0 && !s.handed) || (s.notes > 0 && !s.posted));
  const claims = b.claims || [];
  const said = claims.filter((c) => c.status !== "unstated");
  const tally = ["proved", "contradicted", "unproven"].map((k) => [k, said.filter((c) => c.status === k).length]).filter(([, n]) => n);
  const word = { proved: "true", contradicted: "false", unproven: "can't tell" };
  const total = b.sections.reduce((n, x) => n + x.lines, 0);
  const need = b.sections.filter((x) => x.kind === "judgment").reduce((n, x) => n + x.lines, 0);
  const pushed = s.pushed && !busy;
  return h("div.block", head(
    open.length ? h("a.btn.small.primary", { href: href(key, "d", open[0].d.id) }, `Settle ${plural(open.length, "finding")}`)
      : wrap ? h("a.btn.small.primary", { href: href(key, "signoff") }, "Wrap up")
        : h("a.btn.small", { href: href(key, "brief") }, "Read the self-review")),
    live,
    pushed && h("div.banner", "You pushed since this was read. ", mode === "auto" ? "" : ask("Read it again")),
    b.since && h("div.banner", h("b", "Since your last push: "), prose(b.since.summary)),
    h("p.lede", prose(b.summary)),
    h("div.small.text-2", [
      total > 0 && `${plural(total, "changed line")}, ${need} needing a reviewer's eye`,
      said.length > 0 && `the description: ${tally.map(([k, n]) => `${n} ${word[k]}`).join(", ")}`,
      claims.some((c) => c.status === "unstated") && `${plural(claims.filter((c) => c.status === "unstated").length, "thing")} it doesn't say`,
    ].filter(Boolean).join(" · ")),
    rows.length ? h("div.scroll-x", { class: "mt" }, h("table.reg", h("tbody", rows.map((r) => h("tr.go", { onclick: () => { location.hash = href(key, "d", r.d.id); } },
      h("td", { class: "fit" }, tri(r.n, !!r.o)),
      h("td", h("div", { class: "strong" }, prose(r.d.headline || r.d.title)), r.d.headline && h("div.small.muted", prose(r.d.title))),
      h("td.r", { class: "fit" }, r.o ? h("span.stamp" + EFFECT[r.o.effect][1], EFFECT[r.o.effect][0]) : h("span.stamp.open", "Open")))))))
      : h("p.small", { class: "mt" }, "Nothing a reviewer would stop on."),
    h("p.small.muted", { class: "mt-s" }, `Self-review by ${v.agent || "your agent"} at `, h("span.mono", (v.readSha || "").slice(0, 7)), ` · ${ago(v.readAt)}.`));
}

export function renderMine(S) {
  const m = S.mine, p = m.pr, key = p.owner + "/" + p.repo + "#" + p.number;
  const [lane, laneCls] = LANE_STAMP[m.lane] || [m.lane, ""];
  const head = byline([
    ["", `${p.owner}/${p.repo} #${p.number}`, ".mono"],
    ["", `${p.headRef} → ${p.baseRef}`, ".mono.narrow-hide"],
    ["head", (p.headSha || "").slice(0, 7), ".mono.narrow-hide"],
    ["checked", ago(m.fetchedAt)],
  ]);
  const blockers = (m.blockers || []).length
    ? h("div.scroll-x", h("table.reg", h("tbody", m.blockers.map((b) => h("tr",
      h("td", { class: b.onYou ? "text-red" : "text-2" }, b.text),
      h("td.r", h("div.row", { class: "end gap-u" }, blockerMoves(S, m, key, b))))))))
    : h("p", "Nothing blocks it. It can merge now.");
  return h("div.sheet", head,
    h("nav.sheets", { "aria-label": "Pull request" },
      h("a", { href: "#/" }, "← Inbox"), h("span.spacer"),
      h("a", { href: p.url || "#", target: "_blank", rel: "noreferrer", onclick: openExternal }, "On GitHub")),
    h("div.body",
      h("div.cols",
        h("div.main",
          h("div.row", { class: "mid gap-m" }, h("h1.title", { class: "m0" }, p.title), h("span.stamp" + laneCls, lane), p.draft && h("span.stamp.faint", "Draft")),
          selfBlock(S),
          h("div.block.rule-top", h("h2.h2", "What stands in the way"), blockers),
          h("div.block.rule-top", renderTend(S)),
          h("div.block.rule-top", h("h2.h2", "Checks"), checks(m)),
          h("div.block.rule-top", renderConversation(key, m.conversation, S.st.queue.viewer, p.author, () => refresh(true)))),
        h("div.side.wide", controls(S, m)))));
}
