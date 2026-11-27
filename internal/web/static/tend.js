// The agent's work on one of the user's own pull requests: what it is doing,
// the steps that wait for the user, and a log of what it did with Undo.

import { h, post, toast, ago, prose } from "./dom.js";
import { refresh } from "./app.js";

const KIND = {
  fix: "Commit",
  reply: "Reply",
  resolve: "Resolve thread",
  "update-branch": "Update branch from base",
  rerun: "Re-run failed checks",
  "merge-base": "Merge the base branch",
};

const STATE = {
  done: ["Done", ""],
  undone: ["Undone", ".faint"],
  rejected: ["Sent back", ".faint"],
  failed: ["Failed", ".red"],
  skipped: ["Skipped", ".faint"],
  working: ["Working", ".open"],
};

// send posts once: the button that sent it stays off until the answer, so a
// second click cannot act twice.
async function send(path, body, msg, ev) {
  const btn = ev && ev.currentTarget;
  if (btn) btn.disabled = true;
  try {
    await post(path, body);
    if (msg) toast(msg);
    refresh(true);
    return true;
  } catch (e) {
    toast(e.message);
    if (btn) btn.disabled = false;
    return false;
  }
}

function patchView(text) {
  if (!text) return null;
  const lines = text.replace(/\n$/, "").split("\n").map((l) => {
    const cls = l.startsWith("+") && !l.startsWith("+++") ? ".add" : l.startsWith("-") && !l.startsWith("---") ? ".del" : "";
    return h("div.ln" + cls, h("span.tx", l));
  });
  return h("div.diff.patch", h("div.lines", { style: { maxHeight: "420px", overflowY: "auto" } }, lines));
}

function threadOf(m, id) {
  return (m.conversation.threads || []).find((t) => t.id === id);
}

function threadLine(m, id) {
  const t = threadOf(m, id);
  if (!t) return null;
  const last = (t.comments || [])[t.comments.length - 1];
  return h("div.small.muted", { style: { marginTop: "4px" } },
    h("span.mono", t.path + (t.line ? ":" + t.line : "")),
    last && [" · ", h("b", last.author), ": ", (last.body || "").slice(0, 160)]);
}

function verdictLine(a) {
  const v = a.verdict;
  if (!v) return null;
  const by = v.floor ? "a fixed rule" : v.model ? `the gate on ${v.model}` : "the gate";
  return h("div.small", { style: { marginTop: "6px" } },
    h("b", v.decision === "auto" ? "Safe to do alone: " : "Held for you: "), v.reason, h("span.muted", ` (${by})`));
}

function card(key, m, a) {
  const body = a.kind === "reply" ? h("textarea.field", { rows: 4, "aria-label": "Reply text" }, a.body || "") : null;
  const note = h("input.field", { placeholder: "What should it do instead?", "aria-label": "Note for the agent" });
  const back = h("div.row.hidden", { style: { gap: "8px", flexWrap: "nowrap", marginTop: "8px" } }, note,
    h("button.btn", { onclick: (ev) => send("/api/decide", { key, action: a.id, choice: "send-back", note: note.value }, "Sent back. The agent plans again with your note.", ev) }, "Send back"));
  return h("div.block", { style: { border: "2px solid var(--red)", padding: "12px", marginBottom: "12px" } },
    h("div.row", { style: { alignItems: "baseline", gap: "8px" } }, h("span.stamp.red", KIND[a.kind] || a.kind), h("span", prose(a.why))),
    a.thread && threadLine(m, a.thread),
    verdictLine(a),
    a.kind === "reply" ? h("div", { style: { marginTop: "8px" } }, body) : patchView(a.patch),
    h("div.row", { style: { gap: "8px", marginTop: "8px" } },
      h("button.btn.primary", { onclick: (ev) => send("/api/decide", { key, action: a.id, choice: "approve", body: body ? body.value : "" },
        a.kind === "fix" || a.kind === "merge-base" ? "Pushed." : "Done.", ev) }, a.kind === "reply" ? "Post reply" : a.kind === "fix" || a.kind === "merge-base" ? "Push it" : "Do it"),
      h("button.btn.quiet", { onclick: () => { back.classList.toggle("hidden"); note.focus(); } }, "Send back…")),
    back);
}

function logRow(key, m, a) {
  const [label, cls] = STATE[a.state] || [a.state, ".faint"];
  const open = h("details", h("summary.small", "Show what it changed"), patchView(a.patch));
  return h("tr",
    h("td", h("b", KIND[a.kind] || a.kind), h("div.small", prose(a.why)),
      a.kind === "reply" && a.body && h("div.small.muted", { style: { whiteSpace: "pre-wrap", marginTop: "4px" } }, a.body),
      a.thread && threadLine(m, a.thread),
      a.result && h("div.small.muted", a.result),
      a.patch && open),
    h("td", h("span.stamp" + cls, label), a.verdict && h("div.small.muted", a.verdict.decision === "auto" ? "on its own" : "you approved")),
    h("td.r.small.muted", a.at && !a.at.startsWith("0001") ? ago(a.at) : ""),
    h("td.r", a.state === "done" && a.undo && h("button.btn.small", { onclick: (ev) => send("/api/undo", { key, action: a.id }, "Undone.", ev) }, "Undo")));
}

// tendStatus is the one-line state for the list of own pull requests.
export function tendStatus(m) {
  const t = m.tend;
  if (!t) return null;
  if (t.status === "running") return "The agent is working on it.";
  if (t.status === "failed") return "The agent's last run failed.";
  const waiting = (t.actions || []).filter((a) => a.state === "waiting").length;
  if (waiting) return `The agent has ${waiting} ${waiting === 1 ? "step" : "steps"} waiting for you.`;
  const done = (t.actions || []).filter((a) => a.state === "done").length;
  return done ? `The agent did ${done} ${done === 1 ? "step" : "steps"} ${ago(t.at)}.` : null;
}

export function renderTend(S) {
  const m = S.mine, p = m.pr, key = p.owner + "/" + p.repo + "#" + p.number, t = m.tend;
  const mode = S.st.config.tend || "auto";
  const running = t && t.status === "running";
  const held = t ? (t.actions || []).filter((a) => a.state === "waiting").length : 0;
  const start = h("button.btn" + (t ? "" : ".primary"), {
    disabled: running || mode === "off" || held > 0 || null,
    title: mode === "off" ? "The agent is off for your pull requests. Turn it on in Settings." : held ? "Answer the steps that wait for you first." : "",
    onclick: (ev) => send("/api/tend", { key }, "The agent is on it.", ev),
  }, t ? "Run the agent again" : "Let the agent work on it");
  const modeLine = { auto: "It acts on its own when a step is safe, and asks you about the rest.", ask: "Every step waits for you.", off: "It is off. Turn it on in Settings." }[mode];

  const head = h("div.row", { style: { alignItems: "baseline", justifyContent: "space-between" } },
    h("h2.h2", "The agent"), start);
  if (!t) {
    return h("div.block", head,
      h("p", "The agent can fix what reviewers asked for and what broke the checks, answer threads, and bring the branch up to date. " + modeLine),
      h("p.small.muted", "Docket starts it on its own when a reviewer comments or a check fails, after this first run."));
  }
  const actions = t.actions || [];
  const waiting = actions.filter((a) => a.state === "waiting");
  const log = actions.filter((a) => a.state && a.state !== "waiting");
  const earlier = [...(t.earlier || [])].reverse();
  return h("div.block", head,
    h("p.small.muted", modeLine + (t.agent ? ` ${t.agent}${t.model && t.model !== t.agent ? " on " + t.model : ""}.` : "")),
    running && h("div.banner", h("b", "Working: "), h("span", { "data-tend": key }, t.activity || "Starting")),
    t.status === "failed" && h("div.banner.red", h("b", "The run failed: "), t.error),
    t.summary && h("p.lede", prose(t.summary)),
    (t.notes || []).length > 0 && h("p.small.muted", "Your notes for this run: " + t.notes.join(" · ")),
    waiting.length > 0 && h("div", { style: { marginTop: "12px" } },
      h("div.label", `Waiting for you · ${waiting.length}`),
      waiting.map((a) => card(key, m, a))),
    log.length > 0 && h("div", { style: { marginTop: "12px" } },
      h("div.label", "What it did"),
      h("div.scroll-x", h("table.reg", h("tbody", log.map((a) => logRow(key, m, a)))))),
    earlier.length > 0 && h("details", { style: { marginTop: "12px" } },
      h("summary.label", `Earlier runs · ${earlier.length} ${earlier.length === 1 ? "step" : "steps"}`),
      h("div.scroll-x", h("table.reg", h("tbody", earlier.map((a) => logRow(key, m, a)))))),
    !running && !actions.length && t.status === "ready" && h("p", "The agent found nothing it could do."));
}
