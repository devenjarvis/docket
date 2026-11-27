// The user's own pull requests: the list, sorted by whose move it is, and one
// pull request with what blocks it, its checks, its conversation and the
// controls to move it along.

import { h, get, toast, ago, openExternal } from "./dom.js";
import { renderConversation, act } from "./conversation.js";
import { refresh } from "./app.js";

export function mineHref(key) {
  const m = key.match(/^(.+)\/(.+)#(\d+)$/);
  return `#/m/${m[1]}/${m[2]}/${m[3]}`;
}

const LANES = {
  "needs-you": ["Needs you", "The next move is yours."],
  ready: ["Ready to merge", "Nothing stands in the way."],
  waiting: ["Waiting on others", "Checks are running or a reviewer has not answered yet."],
};

const LANE_STAMP = { "needs-you": ["Needs you", ".red"], ready: ["Ready to merge", ".solid"], waiting: ["Waiting", ".open"] };

export function renderMineList(S) {
  const mine = S.st.mine || [];
  const row = (m) => {
    const p = m.pr;
    const failing = (m.checks || []).filter(failed).length;
    const total = (m.checks || []).length;
    const open = (m.conversation.threads || []).filter((t) => !t.resolved).length;
    return h("tr.go", { onclick: () => { location.hash = mineHref(p.owner + "/" + p.repo + "#" + p.number); }, tabindex: 0,
      onkeydown: (e) => { if (e.key === "Enter") location.hash = mineHref(p.owner + "/" + p.repo + "#" + p.number); } },
      h("td", h("div.mono.small.muted", `${p.owner}/${p.repo} #${p.number}`),
        h("div", { style: { fontWeight: 700, fontSize: "15px" } }, p.title),
        h("div.small.muted", (m.blockers || []).map((b) => b.text).join(" ") || "Ready to merge.")),
      h("td.num", total ? `${total - failing} of ${total}` : "none"),
      h("td.num", open || ""),
      h("td.r.small.muted", ago(p.updatedAt)));
  };
  const groups = Object.entries(LANES).map(([lane, [title, sub]]) => {
    const list = mine.filter((m) => m.lane === lane);
    if (!list.length) return null;
    return h("div.block",
      h("div.row", { style: { alignItems: "baseline", justifyContent: "space-between" } }, h("h2.h2", `${title} · ${list.length}`), h("span.small.muted", sub)),
      h("div.scroll-x", h("table.reg",
        h("thead", h("tr", h("th", "Pull request"), h("th", "Checks passing"), h("th", "Open threads"), h("th.r", ""))),
        h("tbody", list.map(row)))));
  });
  const need = mine.filter((m) => m.lane === "needs-you").length, ready = mine.filter((m) => m.lane === "ready").length;
  return [
    S.st.queue.mineError && h("div.banner.red", S.st.queue.mineError),
    h("div.tally",
      h("div", h("div.label", "Yours, open"), h("div.big", mine.length), h("div.small.muted", "pull requests you wrote")),
      h("div", h("div.label", "Need you"), h("div.big.red", need), h("div.small.muted", "failing checks, conflicts, or a reviewer waiting")),
      h("div", h("div.label", "Ready to merge"), h("div.big", ready), h("div.small.muted", ""))),
    groups.some(Boolean) ? groups : h("div.block", h("h2.h2", "You have no open pull requests"),
      h("p.lede", "Pull requests you open appear here on their own, with what stands between each one and merging.")),
  ];
}

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
    runs.length > 0 && h("div.row", { style: { marginTop: "8px" } },
      runs.map((run) => h("button.btn.small", { onclick: async () => { if (await act(key, { kind: "rerun", run })) refresh(true); } }, "Re-run failed jobs"))));
}

// controls is the author's own panel: only what GitHub would let them do now.
function controls(m) {
  const key = m.pr.owner + "/" + m.pr.repo + "#" + m.pr.number, st = m.state, p = m.pr;
  const methods = st.methods || [];
  const ready = (m.blockers || []).length === 0;
  const pick = h("select.field", { "aria-label": "Merge method", style: { width: "auto" } },
    methods.map((x) => h("option", { value: x, selected: x === "SQUASH" || null }, { MERGE: "Merge commit", SQUASH: "Squash and merge", REBASE: "Rebase and merge" }[x] || x)));
  const confirmRow = h("div.row.hidden", { style: { alignItems: "center", gap: "8px" } });
  const mergeBtn = h("button.btn.red", {
    disabled: !ready || !methods.length,
    title: ready ? "" : "Merging opens once nothing blocks it",
    onclick: () => {
      confirmRow.replaceChildren(
        h("span", `Merge ${p.headRef} into ${p.baseRef} with ${pick.selectedOptions[0].textContent.toLowerCase()}?`),
        h("button.btn.red", { onclick: async () => { if (await act(key, { kind: "merge", method: pick.value, confirm: true })) { toast("Merged."); location.hash = "#/mine"; } } }, "Confirm merge"),
        h("button.btn.quiet", { onclick: () => confirmRow.classList.add("hidden") }, "Cancel"));
      confirmRow.classList.remove("hidden");
    },
  }, "Merge");
  const has = (k) => (m.blockers || []).some((b) => b.kind === k);
  const reviewers = [...new Set((m.conversation.reviews || []).map((r) => r.author).filter((a) => a !== p.author))];
  const who = h("input.field.mono", { value: reviewers.join(", "), placeholder: "logins, comma separated", "aria-label": "Reviewers to ask" });
  const doit = (action, msg) => async () => { if (await act(key, action)) { if (msg) toast(msg); refresh(true); } };
  return h("div.yours",
    h("span.label", "Your move"),
    h("div.row", { style: { alignItems: "center", gap: "8px" } }, pick, mergeBtn,
      st.autoMergeAllowed && !ready && (st.autoMerge
        ? h("button.btn", { onclick: doit({ kind: "auto-merge", on: false }, "Auto-merge is off.") }, "Turn off auto-merge")
        : h("button.btn", { onclick: () => doit({ kind: "auto-merge", on: true, method: pick.value }, "GitHub will merge it once nothing blocks it.")() }, "Merge when ready"))),
    st.autoMerge && h("p.small.muted", `Auto-merge is on (${st.autoMerge.toLowerCase()}): GitHub merges it once nothing blocks it.`),
    confirmRow,
    h("div.row", { style: { gap: "8px", marginTop: "8px" } },
      p.draft && h("button.btn", { onclick: doit({ kind: "ready" }, "Marked ready for review.") }, "Mark ready for review"),
      has("behind") && h("button.btn", { onclick: doit({ kind: "update-branch" }, "Branch updated from base.") }, "Update branch from base"),
      has("conflicts") && h("span.small.muted", "The conflicts have to be resolved in a checkout of the branch.")),
    h("div.row", { style: { gap: "8px", marginTop: "8px", flexWrap: "nowrap" } }, who,
      h("button.btn", { onclick: () => doit({ kind: "request-review", logins: who.value.split(",").map((s) => s.trim()).filter(Boolean) }, "Review requested.")() }, "Ask for review")));
}

export function renderMine(S) {
  const m = S.mine, p = m.pr, key = p.owner + "/" + p.repo + "#" + p.number;
  const cell = (label, value, cls) => h("div" + (cls || ""), h("span.label", label), h("span.cell-v", value));
  const [lane, laneCls] = LANE_STAMP[m.lane] || [m.lane, ""];
  const head = h("div.titleblock",
    h("a.brand", { href: "#/mine", title: "Back to your pull requests" }, "Docket"),
    cell("Pull request", `${p.owner}/${p.repo} #${p.number}`),
    cell("Title", p.title, ".wide"),
    cell("Branch", `${p.headRef} → ${p.baseRef}`),
    cell("Head", (p.headSha || "").slice(0, 7)),
    cell("Checked", ago(m.fetchedAt)));
  const blockers = (m.blockers || []).length
    ? h("ul", { style: { margin: 0, paddingLeft: "20px" } }, m.blockers.map((b) => h("li", { style: { color: b.onYou ? "var(--red)" : "var(--text-2)", marginBottom: "4px" } }, b.text)))
    : h("p", "Nothing blocks it. It can merge now.");
  return h("div.sheet", head,
    h("nav.sheets", { "aria-label": "Pull request" },
      h("a", { href: "#/mine" }, "← Yours"), h("span.spacer"),
      h("a", { href: p.url || "#", target: "_blank", rel: "noreferrer", onclick: openExternal }, "On GitHub")),
    h("div.body",
      h("div.cols",
        h("div.main",
          h("div.row", { style: { alignItems: "center", gap: "12px" } }, h("h1.title", { style: { margin: 0 } }, p.title), h("span.stamp" + laneCls, lane)),
          h("div.block", h("h2.h2", "What stands in the way"), blockers),
          h("div.block.rule-top", h("h2.h2", "Checks"), checks(m)),
          h("div.block.rule-top", renderConversation(key, m.conversation, S.st.queue.viewer, p.author, () => refresh(true)))),
        h("div.side.wide", controls(m)))));
}
