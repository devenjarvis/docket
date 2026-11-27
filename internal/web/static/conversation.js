// A pull request's conversation: reviews, line threads and comments, in the
// order they happened, with the controls to answer them.

import { h, post, toast, ago, prose, openExternal } from "./dom.js";

const REVIEW = {
  APPROVED: ["Approved", ".solid"],
  CHANGES_REQUESTED: ["Changes requested", ".open"],
  COMMENTED: ["Commented", ".faint"],
  DISMISSED: ["Dismissed", ".faint"],
};

// act sends one action and reports failure in a toast. It returns whether
// the action went through.
export async function act(key, action) {
  try { await post("/api/act", { key, ...action }); return true; }
  catch (e) { toast(e.message); return false; }
}

function who(name, viewer) {
  return name === viewer ? "you" : name;
}

function link(url, text) {
  return url ? h("a.small", { href: url, target: "_blank", rel: "noreferrer", onclick: openExternal }, text) : null;
}

function body(text) {
  return text && text.trim() ? h("div", { class: "prewrap mt" }, prose(text)) : null;
}

function comment(c, viewer) {
  return h("div", { class: "py" },
    h("div.small.muted", h("b", who(c.author, viewer)), " · ", ago(c.at), " ", link(c.url, "on GitHub")),
    body(c.body));
}

// replyBox is a textarea and a button; done is called once the reply posted.
// Folded, it is a quiet button that opens the box, so a pull request with
// many threads does not run to several screens of empty boxes.
function replyBox(label, placeholder, send, folded) {
  const ta = h("textarea.field", { rows: 2, placeholder, "aria-label": label });
  const btn = h("button.btn.small", {
    onclick: async () => {
      if (!ta.value.trim()) return;
      btn.disabled = true;
      if (await send(ta.value)) ta.value = "";
      btn.disabled = false;
    },
  }, label);
  const box = h("div", { class: "stack mt" + (folded ? " hidden" : "") }, ta,
    h("div.row", { class: "gap-u" }, btn, folded && h("button.btn.small.quiet", { onclick: () => { ta.value = ""; box.classList.add("hidden"); opener.classList.remove("hidden"); } }, "Cancel")));
  if (!folded) return box;
  const opener = h("button.btn.small.quiet", { class: "mt", onclick: () => { opener.classList.add("hidden"); box.classList.remove("hidden"); ta.focus(); } }, label);
  return h("div", opener, box);
}

function thread(key, t, viewer, author, onChange) {
  const last = t.comments[t.comments.length - 1] || {};
  // A thread waits on the viewer when they wrote the pull request or took
  // part in it, and someone else spoke last.
  const involved = author === viewer || t.comments.some((c) => c.author === viewer);
  const onYou = !t.resolved && involved && last.author !== viewer;
  const resolve = h("button.btn.small.quiet", {
    onclick: async () => { if (await act(key, { kind: t.resolved ? "unresolve" : "resolve", thread: t.id })) onChange(); },
  }, t.resolved ? "Reopen" : "Resolve");
  const inner = [
    t.diffHunk && h("pre.code", { style: { maxHeight: "160px", overflow: "auto", fontSize: "12px" } }, t.diffHunk.split("\n").slice(-6).join("\n")),
    t.comments.map((c) => comment(c, viewer)),
    replyBox("Reply", "Reply in this thread", async (text) => {
      const ok = await act(key, { kind: "reply", thread: t.id, body: text });
      if (ok) onChange();
      return ok;
    }, true),
  ];
  const head = h("div.row", { class: "spread mid gap-u" },
    h("span.row", { class: "mid gap-u" },
      h("code", `${t.path}${t.line ? ":" + t.line : ""}`),
      t.resolved ? h("span.stamp.faint", "Resolved") : onYou ? h("span.stamp.red", "Waiting on you") : h("span.stamp.open", "Open"),
      t.outdated && h("span.stamp.faint", "Outdated")),
    resolve);
  // A resolved thread folds away; an open one stays open.
  if (t.resolved) {
    return h("details.block", { id: "thread-" + t.id }, h("summary", head), inner);
  }
  return h("div.block.entry" + (onYou ? ".on-you" : ""), { id: "thread-" + t.id, "data-on-you": onYou ? "1" : null }, head, inner);
}

// renderConversation draws conv for the pull request key, written by
// author. onChange is called after an action so the caller can fetch again.
export function renderConversation(key, conv, viewer, author, onChange) {
  const items = [];
  for (const r of conv.reviews || []) {
    // An empty COMMENTED review is the wrapper GitHub makes for line
    // comments; the threads already show those.
    if (r.state === "COMMENTED" && !(r.body || "").trim()) continue;
    items.push({ at: r.at, el: reviewItem(r, viewer) });
  }
  for (const c of conv.comments || []) items.push({ at: c.at, el: h("div.block", { class: "entry" }, comment(c, viewer)) });
  for (const t of conv.threads || []) items.push({ at: (t.comments[0] || {}).at, el: thread(key, t, viewer, author, onChange) });
  items.sort((a, b) => new Date(a.at) - new Date(b.at));

  const open = (conv.threads || []).filter((t) => !t.resolved).length;
  return h("div.block",
    h("div.row", { class: "spread base" },
      h("h2.h2", "Conversation"),
      h("span.small.muted", open ? `${open} open thread${open === 1 ? "" : "s"}` : "No open threads")),
    items.length ? items.map((i) => i.el) : h("p.muted", "Nobody has said anything yet."),
    h("div.block.rule-top",
      h("span.label", "Comment on the pull request"),
      replyBox("Comment", "A comment for everyone on this pull request", async (text) => {
        const ok = await act(key, { kind: "comment", body: text });
        if (ok) onChange();
        return ok;
      })));
}

function reviewItem(r, viewer) {
  const [label, cls] = REVIEW[r.state] || [r.state, ".faint"];
  return h("div.block", { class: "entry" },
    h("div.row", { class: "mid gap-u" },
      h("b", who(r.author, viewer)), h("span.stamp" + cls, label), h("span.small.muted", ago(r.at)), link(r.url, "on GitHub")),
    body(r.body));
}
