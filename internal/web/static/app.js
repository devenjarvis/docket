import { h, get, post, toast, plural, ago, tri } from "./dom.js";
import { act } from "./conversation.js";
import { renderReview, reviewKeys, currentTimeTarget } from "./review.js";
import { renderRules } from "./rules.js";
import { renderMine, mineHref } from "./mine.js";
import { renderSettings, loadSettings, TABS } from "./settings.js";

const app = document.getElementById("app");
export const S = { st: null, review: null, draft: null, rules: null, mine: null, conv: null, settings: null, route: {}, stale: false };

// --- routes: #/, #/waiting, #/reviewed, #/r/<owner>/<repo>/<n>/<sheet>[/<id>], #/m/<owner>/<repo>/<n>, #/rules, #/settings[/<tab>]
function parse() {
  const p = location.hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
  if (p[0] === "r" && p.length >= 4) {
    return { name: "review", key: `${p[1]}/${p[2]}#${p[3]}`, sheet: p[4] || "brief", id: p[5] || "" };
  }
  if (p[0] === "m" && p.length >= 4) return { name: "minepr", key: `${p[1]}/${p[2]}#${p[3]}` };
  if (p[0] === "waiting" || p[0] === "reviewed") return { name: "queue", tab: p[0] };
  if (p[0] === "rules") return { name: "rules" };
  if (p[0] === "settings") return { name: "settings", tab: TABS.some(([t]) => t === p[1]) ? p[1] : "metrics" };
  return { name: "queue", tab: "queue" };
}
export function href(key, sheet, id) {
  const m = key.match(/^(.+)\/(.+)#(\d+)$/);
  return `#/r/${m[1]}/${m[2]}/${m[3]}/${sheet || "brief"}${id ? "/" + id : ""}`;
}
export const go = (hash) => { location.hash = hash; };

async function load() {
  S.route = parse();
  flushTime();
  try {
    S.st = await get("/api/state");
    if (S.route.name === "review") {
      S.review = await get("/api/review?key=" + encodeURIComponent(S.route.key));
      S.draft = S.route.sheet === "signoff" && S.review.brief ? await get("/api/draft?key=" + encodeURIComponent(S.route.key)) : null;
      S.conv = S.route.sheet === "conversation" ? await get("/api/conversation?key=" + encodeURIComponent(S.route.key)) : null;
    } else {
      S.review = null;
    }
    S.mine = S.route.name === "minepr" ? await get("/api/mine/pr?key=" + encodeURIComponent(S.route.key)) : null;
    if (S.route.name === "rules") S.rules = await get("/api/rules");
    if (S.route.name === "settings") await loadSettings(S);
  } catch (e) {
    if (S.route.name === "review") { toast(e.message); go("#/"); return; }
    if (S.route.name === "minepr") { toast(e.message); go("#/"); return; }
    toast(e.message);
  }
  render();
}

// refresh re-reads the server without losing what the reviewer is typing.
export async function refresh(force) {
  const el = document.activeElement;
  if (!force && el && (el.tagName === "TEXTAREA" || (el.tagName === "INPUT" && (el.type === "text" || el.type === "number")))) {
    S.stale = true;
    return;
  }
  S.stale = false;
  const y = scrollY;
  await load();
  scrollTo(0, y);
}
document.addEventListener("focusout", () => { if (S.stale) setTimeout(() => refresh(), 50); });

function render() {
  let view;
  if (S.route.name === "review" && S.review) view = renderReview(S);
  else if (S.route.name === "rules") view = [masthead(), renderRules(S)];
  else if (S.route.name === "settings") view = [masthead(), renderSettings(S)];
  else if (S.route.name === "minepr" && S.mine) view = renderMine(S);
  else view = [masthead(), queue(S.route.tab)];
  app.replaceChildren(...[view].flat());
  const pr = (S.route.name === "review" && S.review && S.review.pr) || (S.route.name === "minepr" && S.mine && S.mine.pr);
  document.title = pr ? `${pr.repo} #${pr.number} · Docket` : "Docket";
}

// --- masthead: the app's own title block ------------------------------------
function masthead() {
  const q = S.st.queue;
  const cell = (label, value, extra) => h("div", h("span.label", label), h("span.cell-v", value), extra);
  return h("div.sheet", { class: "mb-l" },
    h("div.titleblock", { class: "bare" },
      h("a.brand", { href: "#/" }, "Docket"),
      cell("Reviewer", q.viewer || "not signed in"),
      cell("Agent", S.st.agent.name || "none set up"),
      h("div.wide", h("span.label", "GitHub"), h("span.cell-v", { class: "v-mono" },
        q.syncError ? "sync failed" : "checked " + ago(q.lastSync))),
      h("a", { href: "#/rules", class: "plain" }, h("span.label", "Rulebook"), h("span.cell-v", plural(S.st.rules, "rule"))),
      h("a", { href: "#/settings", class: "plain", "aria-current": S.route.name === "settings" ? "page" : null },
        h("span.label", "Settings"), h("span.cell-v", S.st.demo ? "demo mode" : "metrics, agent, policy"))));
}

// --- inbox: reviews and the user's own pull requests, in one list ---------------
const BANDS = {
  quick: ["Quick", "A minute or two each. Clearing these gets things merged."],
  closer: ["Needs a closer look", "Reviews with questions for you, and fixes only you can make."],
  skim: ["Skim and approve", "Nothing to decide, and nobody is blocked on these."],
  waiting: ["Waiting on others", "Your agent, CI or a reviewer has these. They come back up when they need you."],
  done: ["Reviewed", "Reviews you posted. They leave once the pull request closes."],
};
const TODO = ["quick", "closer", "skim"];
const BUDGETS = [[15, "15 min"], [30, "30 min"], [60, "1 h"], [0, "All"]];

export function itemHref(it) { return it.mine ? mineHref(it.key) : href(it.key); }
const todo = () => (S.st.inbox || []).filter((it) => TODO.includes(it.band));

function budget() {
  let b = 30;
  try { const v = localStorage.getItem("docket-budget"); if (v !== null && BUDGETS.some(([m]) => String(m) === v)) b = +v; } catch (e) { /* no storage */ }
  return b;
}

// inboxTabs splits what is the user's to do from what others have.
function inboxTabs(active) {
  const all = S.st.inbox || [];
  const n = (band) => all.filter((it) => it.band === band).length;
  const tab = (name, hash, label) => h("a", { href: hash, "aria-current": active === name ? "page" : null }, label);
  return h("nav.sheets", { "aria-label": "Inbox" },
    tab("queue", "#/", `Inbox · ${todo().length}`),
    tab("waiting", "#/waiting", `Waiting on others · ${n("waiting")}`),
    tab("reviewed", "#/reviewed", `Reviewed · ${n("done")}`));
}

// pullRef is the row's first line: where it is, whose it is, its questions.
function pullRef(it) {
  const p = it.pr, tris = [];
  for (let i = 1; i <= (it.decisions || 0); i++) tris.push(tri(i, i <= it.answered));
  return h("div.mono.small.muted", { class: "row mid gap-u" },
    h("span", `${p.owner}/${p.repo} #${p.number}`),
    it.mine ? h("span.stamp.sm.solid", "Yours") : h("span.stamp.sm", "Review"),
    !it.mine && h("span", p.author),
    tris.length > 0 && h("span.row", { class: "gap-s" }, tris));
}

function inboxRow(it, past, open) {
  const go2 = () => go(itemHref(it));
  const tr = h("tr.go" + (past ? ".past" : ""), { tabindex: 0, onclick: go2, onkeydown: (e) => { if (e.key === "Enter") go2(); } },
    h("td", h("button.btn.red.small.move-btn", {
      onclick: (e) => { e.stopPropagation(); it.action === "Merge" && it.mine ? open(it) : go2(); },
    }, it.action)),
    h("td", pullRef(it), h("div", { class: "item-title" }, it.pr.title), h("div.why", it.why)),
    h("td.r.num", `${it.minutes} min`),
    h("td.r.small.muted", ago(it.since).replace(" ago", "")));
  return tr;
}

// mergeRow is the confirm that opens under a Merge row, so a pull request
// that is ready can merge without leaving the inbox.
function mergeRow(it, close) {
  const m = (S.st.mine || []).find((x) => `${x.pr.owner}/${x.pr.repo}#${x.pr.number}` === it.key);
  const methods = (m && m.state.methods) || [];
  const method = methods.includes("SQUASH") ? "SQUASH" : methods[0];
  const name = { MERGE: "Merge", SQUASH: "Squash and merge", REBASE: "Rebase and merge" }[method] || "Merge";
  return h("tr.expand", h("td"), h("td", { colspan: 3 },
    method
      ? h("div.row", { class: "mid gap-u" },
        h("span", `${name} `, h("span.mono", it.pr.headRef), " into ", h("span.mono", it.pr.baseRef), "?"),
        h("button.btn.red.small", { onclick: async (e) => {
          e.currentTarget.disabled = true;
          if (await act(it.key, { kind: "merge", method, confirm: true })) { toast("Merged."); refresh(true); } else e.currentTarget.disabled = false;
        } }, "Confirm merge"),
        h("button.btn.quiet.small", { onclick: close }, "Cancel"))
      : h("div.row", { class: "mid gap-u" }, h("span", "This repository doesn't allow merging from here."), h("a.small", { href: itemHref(it) }, "Open it"))));
}

function register(items, opts) {
  const { cut, opened, open, bands } = opts;
  let sum = 0, cutDone = false;
  const bodies = bands.map((band) => {
    const list = items.filter((it) => it.band === band);
    if (!list.length) return null;
    const [title, sub] = BANDS[band];
    const rows = [h("tr.bandrow", h("td", { colspan: 4 }, h("h2.h2", `${title} · ${list.length}`), h("span.small.muted", sub)))];
    for (const it of list) {
      if (cut && !cutDone && sum + it.minutes > cut) {
        rows.push(h("tr.cut", h("td", { colspan: 4 }, h("div.dim", h("span", sum ? `Your ${cut} minutes end here` : `Nothing fits in ${cut} minutes`)))));
        cutDone = true;
      }
      sum += it.minutes;
      rows.push(inboxRow(it, cutDone, open));
      if (opened === it.key) rows.push(mergeRow(it, () => open(null)));
    }
    return h("tbody", rows);
  });
  return h("div.scroll-x", h("table.reg.inbox",
    h("thead", h("tr", h("th", "Do"), h("th", "Pull request"), h("th.r", "Time"), h("th.r", "Waiting"))),
    bodies));
}

// others is a plain list for the waiting and reviewed tabs.
function others(items, band) {
  const list = items.filter((it) => it.band === band);
  const [title, sub] = BANDS[band];
  if (!list.length) return h("div.block", h("h2.h2", band === "done" ? "No posted reviews" : "Nothing is waiting on anyone else"), h("p.lede", sub));
  return h("div.block",
    h("div.row", { class: "spread base" }, h("h2.h2", `${title} · ${list.length}`), h("span.small.muted", sub)),
    h("div.scroll-x", h("table.reg.lanes",
      h("thead", h("tr", h("th", "Pull request"), h("th", "Status"), h("th", "What's happening"), h("th.r", "Updated"))),
      h("tbody", list.map((it) => h("tr.go", { tabindex: 0, onclick: () => go(itemHref(it)), onkeydown: (e) => { if (e.key === "Enter") go(itemHref(it)); } },
        h("td", pullRef(it), h("div", { class: "item-title" }, it.pr.title)),
        h("td", it.live && h("span.live"), h("span.stamp.sm" + (it.band === "done" ? ".solid" : ".open"), it.status || "Posted")),
        h("td.small.text-2", it.live && it.activity ? h("span", { "data-activity": it.key }, it.activity) : it.why),
        h("td.r.small.muted", ago(it.band === "done" ? it.since : it.pr.updatedAt))))))));
}

let openedMerge = null;

function queue(tab) {
  const q = S.st.queue, all = S.st.inbox || [], list = todo();
  const b = budget();
  const total = list.reduce((n, it) => n + it.minutes, 0);
  let fit = 0;
  for (const it of list) { if (b && fit + it.minutes > b) break; fit += it.minutes; }
  const waiters = [...new Set(list.map((it) => it.waiter).filter(Boolean))];
  const nMine = list.filter((it) => it.mine).length;

  const addInput = h("input.field.mono", { id: "add", placeholder: "owner/repo#123 or a pull request URL", "aria-label": "Pull request to read" });
  const add = async () => {
    if (!addInput.value.trim()) return;
    try { const r = await post("/api/add", { ref: addInput.value }); addInput.value = ""; go(href(r.key)); }
    catch (e) { toast(e.message); }
  };
  addInput.addEventListener("keydown", (e) => { if (e.key === "Enter") add(); });

  const setBudget = (m) => { try { localStorage.setItem("docket-budget", String(m)); } catch (e) { /* no storage */ } render(); };
  const open = (it) => { openedMerge = it ? it.key : null; render(); };

  let body;
  if (tab === "waiting") body = others(all, "waiting");
  else if (tab === "reviewed") body = others(all, "done");
  else body = [
    h("div.tally",
      h("div", h("div.label", "To do"), h("div.big.red", list.length), h("div.small.muted", `${list.length - nMine} reviews, ${nMine} of yours`)),
      h("div", h("div.label", "People waiting on you"), h("div.big", waiters.length), h("div.small.muted", waiters.slice(0, 4).join(", ") + (waiters.length > 4 ? ` and ${waiters.length - 4} more` : ""))),
      h("div", h("div.label", "All of it"), h("div.big", `${total} min`), h("div.small.muted", b ? `${fit} min fits in ${b}` : "")),
      h("div", { class: "stack vcenter" },
        h("label.label", { for: "add" }, "Read another"),
        h("div.row", { class: "one-line gap-u" }, addInput, h("button.btn", { onclick: add }, "Read")))),
    list.length > 0 && h("div.row", { class: "spread mid" },
      h("div.row", { class: "mid gap-m" },
        h("span.label.ink", "I have"),
        h("div.seg", { role: "group", "aria-label": "Time you have" },
          BUDGETS.map(([m, label]) => h("button", { "aria-pressed": String(m === b), onclick: () => setBudget(m) }, label)))),
      h("button.btn.primary", { onclick: () => go(itemHref(list[0])) }, `Start: ${list[0].action} #${list[0].pr.number}`)),
    list.length > 0 ? register(list, { cut: b, opened: openedMerge, open, bands: TODO })
      : h("div.block", h("h2.h2", "Nothing to do"),
        h("p.lede", "Review requests, and your own pull requests when they need something from you, show up here on their own.")),
  ];

  return h("div.sheet", inboxTabs(tab || "queue"), h("div.body",
    S.st.demo && h("div.banner", "Demo mode: sample pull requests and a scripted agent. Nothing is read from or posted to GitHub."),
    q.syncError && h("div.banner.red", q.syncError),
    q.mineError && h("div.banner.red", q.mineError),
    !S.st.agent.command && h("div.banner.red", "No agent is set up, so nothing can be read. ", h("a", { href: "#/settings/agent" }, "Pick one in Settings.")),
    body,
    h("div.row", { class: "spread mid" },
      h("span.small.muted", `Checks GitHub every ${Math.round(S.st.config.pollSeconds / 60) || 1} min. `,
        h("span.kbd", "j"), " ", h("span.kbd", "k"), " move between rows, ", h("span.kbd", "Enter"), " opens one."),
      h("button.btn.quiet.small", { onclick: async () => { try { await post("/api/sync"); } catch (e) { toast(e.message); } refresh(true); } }, "Check now"))));
}

// --- time: active seconds per decision or section -----------------------------
let pending = {};
setInterval(() => {
  if (document.hidden || !document.hasFocus() || S.route.name !== "review" || !S.review || !S.review.brief) return;
  const id = currentTimeTarget(S);
  if (!id) return;
  const k = S.route.key + "\n" + id;
  pending[k] = (pending[k] || 0) + 5;
}, 5000);
function flushTime() {
  const p = pending; pending = {};
  for (const [k, seconds] of Object.entries(p)) {
    const [key, id] = k.split("\n");
    post("/api/time", { key, id, seconds }).catch(() => {});
  }
}
setInterval(flushTime, 20000);
addEventListener("pagehide", flushTime);

// --- live updates ---------------------------------------------------------------
function connect() {
  const es = new EventSource("/api/events");
  es.onmessage = (m) => {
    const e = JSON.parse(m.data);
    if (e.type === "ask") {
      const el = document.querySelector(`[data-ask="${e.id}"]`);
      if (el) el.textContent += e.text;
      return;
    }
    if (e.type === "activity") {
      if (S.route.name === "queue" || (S.route.name === "review" && e.key === S.route.key && S.review && S.review.status !== "ready")) {
        const el = document.querySelector(`[data-activity="${CSS.escape(e.key)}"]`);
        if (el) el.textContent = e.text; else if (S.route.name === "queue") refresh();
      }
      return;
    }
    if (e.type === "review" && S.route.name === "review" && e.key !== S.route.key) return;
    if (e.type === "mine") {
      const r = S.route.name;
      if (r === "queue" || r === "mine" || (r === "minepr" && (!e.key || e.key === S.route.key))) refresh();
      return;
    }
    if (e.type === "tend") {
      const el = document.querySelector(`[data-tend="${CSS.escape(e.key)}"]`);
      if (el) el.textContent = e.text;
      return;
    }
    if (e.type === "conversation") {
      if (S.route.name === "review" && S.route.sheet === "conversation" && e.key === S.route.key) refresh();
      return;
    }
    refresh();
  };
  es.onerror = () => { es.close(); setTimeout(connect, 2000); };
}

addEventListener("hashchange", () => { scrollTo(0, 0); load(); });
addEventListener("keydown", (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const t = e.target;
  if (t.tagName === "TEXTAREA" || t.tagName === "INPUT" || t.tagName === "SELECT") return;
  if (S.route.name === "review") reviewKeys(S, e);
  else if (S.route.name === "queue" && (e.key === "j" || e.key === "k")) moveRow(e.key === "j" ? 1 : -1);
});

// moveRow moves focus through the inbox's rows; Enter on a row opens it.
function moveRow(by) {
  const rows = [...document.querySelectorAll("tr.go[tabindex]")];
  if (!rows.length) return;
  const i = rows.indexOf(document.activeElement);
  const next = rows[i < 0 ? (by > 0 ? 0 : rows.length - 1) : Math.min(rows.length - 1, Math.max(0, i + by))];
  next.focus();
  next.scrollIntoView({ block: "nearest" });
}
connect();
load();
