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

// --- masthead: the wordmark, today's issue and the ways round Docket ----------
const DAY = new Intl.DateTimeFormat("en-GB", { weekday: "short", day: "numeric", month: "short" });

function masthead() {
  const q = S.st.queue, all = S.st.inbox || [];
  const n = (band) => all.filter((it) => it.band === band).length;
  const r = S.route, here = r.name === "queue" ? r.tab || "queue" : r.name;
  const link = (name, hash, label, count) => h("a", { href: hash, "aria-current": here === name ? "page" : null },
    label, count !== undefined && h("span.n", count));
  // Whatever the agent is reading now, so it is never a mystery.
  const live = all.find((it) => it.live);
  return h("header.mast",
    h("a.brand", { href: "#/" }, "Docket"),
    h("div.mast-r",
      h("div.dateline",
        h("span", DAY.format(new Date())),
        h("span", q.viewer || "not signed in"),
        h("span", S.st.agent.name || "no agent set up"),
        h("span", q.syncError ? "GitHub sync failed" : "GitHub checked " + ago(q.lastSync)),
        live && h("span.agent-now", h("span.live"), `Reading #${live.pr.number}: `, h("span", { "data-activity": live.key }, live.activity || "starting"))),
      h("nav.mast-nav", { "aria-label": "Docket" },
        link("queue", "#/", "In this issue", todo().length),
        link("waiting", "#/waiting", "Waiting on others", n("waiting")),
        link("reviewed", "#/reviewed", "Back issues", n("done")),
        link("rules", "#/rules", "Rulebook", S.st.rules),
        link("settings", "#/settings", "Settings"))));
}

// --- inbox: reviews and the user's own pull requests, in one list ---------------
const BANDS = {
  quick: ["Quick", "A minute or two each. Clearing these gets things merged."],
  closer: ["Needs a closer look", "Reviews with questions for you, and fixes only you can make."],
  skim: ["Skim and approve", "Nothing to decide, and nobody is blocked on these."],
  waiting: ["Waiting on others", "Your agent, CI or a reviewer has these, or you snoozed them. They come back up when they need you."],
  done: ["Back issues", "Reviews you posted. They leave once the pull request closes."],
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

// item is one entry on the contents page: its number, what it is, why it is
// here, and the move that is yours.
function item(it, no, past, open) {
  const go2 = () => go(itemHref(it));
  return h("article.item" + (past ? ".past" : ""), { tabindex: 0, "data-row": "", "data-key": it.key, onclick: go2, onkeydown: (e) => { if (e.key === "Enter" && e.target === e.currentTarget) go2(); } },
    h("span.item-no", { "aria-hidden": "true" }, String(no).padStart(2, "0")),
    h("div.item-body",
      pullRef(it),
      h("a.item-title.plain", { href: itemHref(it), onclick: (e) => e.stopPropagation() }, it.pr.title),
      h("div.why", it.why, h("span.item-time", ` · ${it.minutes} min · waiting ${ago(it.since).replace(" ago", "")}`))),
    h("div.item-do",
      h("button.btn.red.small", {
        onclick: (e) => { e.stopPropagation(); it.action === "Merge" && it.mine ? open(it, "merge") : go2(); },
      }, it.action),
      h("button.btn.quiet.small", { onclick: (e) => { e.stopPropagation(); open(it, "snooze"); } }, "Snooze")));
}

// snoozeRow opens under a row to put it aside until it changes or until
// tomorrow morning.
function snoozeRow(it, close) {
  const snooze = (kind) => async (e) => {
    e.currentTarget.disabled = true;
    try { await post("/api/snooze", { key: it.key, kind }); toast(kind === "tomorrow" ? "Snoozed until tomorrow morning." : "Snoozed until it changes."); close(); }
    catch (err) { toast(err.message); e.currentTarget.disabled = false; }
  };
  return h("div.item-expand",
    h("div.row", { class: "mid gap-u" },
      h("span", "Snooze it"),
      h("button.btn.small", { onclick: snooze("change") }, "Until it changes"),
      h("button.btn.small", { onclick: snooze("tomorrow") }, "Until tomorrow"),
      h("button.btn.quiet.small", { onclick: close }, "Cancel"),
      h("span.small.muted", "It waits with the others, and comes back on its own.")));
}

// mergeRow is the confirm that opens under a Merge row, so a pull request
// that is ready can merge without leaving the inbox.
function mergeRow(it, close) {
  const m = (S.st.mine || []).find((x) => `${x.pr.owner}/${x.pr.repo}#${x.pr.number}` === it.key);
  const methods = (m && m.state.methods) || [];
  const method = methods.includes("SQUASH") ? "SQUASH" : methods[0];
  const name = { MERGE: "Merge", SQUASH: "Squash and merge", REBASE: "Rebase and merge" }[method] || "Merge";
  return h("div.item-expand",
    method
      ? h("div.row", { class: "mid gap-u" },
        h("span", `${name} `, h("span.mono", it.pr.headRef), " into ", h("span.mono", it.pr.baseRef), "?"),
        h("button.btn.red.small", { onclick: async (e) => {
          e.currentTarget.disabled = true;
          if (await act(it.key, { kind: "merge", method, confirm: true })) { toast("Merged."); refresh(true); } else e.currentTarget.disabled = false;
        } }, "Confirm merge"),
        h("button.btn.quiet.small", { onclick: close }, "Cancel"))
      : h("div.row", { class: "mid gap-u" }, h("span", "This repository doesn't allow merging from here."), h("a.small", { href: itemHref(it) }, "Open it")));
}

// contents is the inbox's list: bands in order, numbered straight through, cut
// where the time the user has runs out.
function contents(items, opts) {
  const { cut, opened, open, bands } = opts;
  let sum = 0, no = 0, cutDone = false;
  return bands.map((band) => {
    const list = items.filter((it) => it.band === band);
    if (!list.length) return null;
    const [title, sub] = BANDS[band];
    const rows = [h("div.band", h("h2.h2", `${title} · ${list.length}`), h("span.small.muted", sub))];
    for (const it of list) {
      if (cut && !cutDone && sum + it.minutes > cut) {
        rows.push(h("div.dim", h("span", sum ? `Your ${cut} minutes end here` : `Nothing fits in ${cut} minutes`)));
        cutDone = true;
      }
      sum += it.minutes;
      rows.push(item(it, ++no, cutDone, open));
      if (opened && opened.key === it.key) rows.push((opened.kind === "snooze" ? snoozeRow : mergeRow)(it, () => open(null)));
    }
    return h("section.band-list", rows);
  });
}

// others is a plain list for the waiting and reviewed tabs.
function others(items, band) {
  const list = items.filter((it) => it.band === band);
  const [title, sub] = BANDS[band];
  if (!list.length) return h("div.block", h("h1.title", band === "done" ? "No back issues yet" : "Nothing is waiting on anyone else"), h("p.lede", sub));
  return h("div.block",
    h("div.block", { class: "gap-u" }, h("h1.title", `${title} · ${list.length}`), h("p.lede", sub)),
    h("div.scroll-x", h("table.reg.lanes",
      h("thead", h("tr", h("th", "Pull request"), h("th", "Status"), h("th", "What's happening"), h("th.r", "Updated"))),
      h("tbody", list.map((it) => h("tr.go", { tabindex: 0, "data-row": "", onclick: () => go(itemHref(it)), onkeydown: (e) => { if (e.key === "Enter") go(itemHref(it)); } },
        h("td", pullRef(it), h("div", { class: "item-title" }, it.pr.title)),
        h("td", it.live && h("span.live"), h("span.stamp.sm" + (it.band === "done" ? ".solid" : it.snoozed ? ".faint" : ".open"), it.status || "Posted"),
          it.snoozed && h("div", { class: "mt" }, h("button.btn.small", { onclick: async (e) => {
            e.stopPropagation();
            try { await post("/api/snooze", { key: it.key, kind: "" }); toast("Back in the inbox."); refresh(true); } catch (err) { toast(err.message); }
          } }, "Wake it now"))),
        h("td.small.text-2", it.live && it.activity ? h("span", { "data-activity": it.key }, it.activity) : it.why),
        h("td.r.small.muted", ago(it.band === "done" ? it.since : it.pr.updatedAt))))))));
}

// opened is the row with a confirm open under it: { key, kind }.
let opened = null;

const WORDS = ["No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"];
const word = (n) => WORDS[n] || String(n);

// dek is the cover's one-paragraph summary of what is in this issue.
function dek(list) {
  const n = (band) => list.filter((it) => it.band === band).length;
  const out = [];
  const q = n("quick"), c = n("closer"), k = n("skim");
  if (q) out.push(`${word(q)} ${q === 1 ? "is" : "are"} quick.`);
  if (c) out.push(`${word(c)} need${c === 1 ? "s" : ""} a proper read.`);
  if (k) out.push(`${word(k)} you can skim.`);
  return out.join(" ");
}
const tonight = () => { const hr = new Date().getHours(); return hr < 12 ? "this morning" : hr < 17 ? "this afternoon" : "tonight"; };

function queue(tab) {
  const q = S.st.queue, all = S.st.inbox || [], list = todo();
  const b = budget();
  const total = list.reduce((n, it) => n + it.minutes, 0);
  let fit = 0;
  for (const it of list) { if (b && fit + it.minutes > b) break; fit += it.minutes; }
  const waiters = [...new Set(list.map((it) => it.waiter).filter(Boolean))];
  const nMine = list.filter((it) => it.mine).length;

  const addInput = h("input.field.mono", { id: "add", placeholder: "owner/repo#123 or a pull request URL" });
  const add = async () => {
    if (!addInput.value.trim()) return;
    try { const r = await post("/api/add", { ref: addInput.value }); addInput.value = ""; go(href(r.key)); }
    catch (e) { toast(e.message); }
  };
  addInput.addEventListener("keydown", (e) => { if (e.key === "Enter") add(); });

  const setBudget = (m) => { try { localStorage.setItem("docket-budget", String(m)); } catch (e) { /* no storage */ } render(); };
  const open = (it, kind) => { opened = it ? { key: it.key, kind } : null; render(); };

  const banners = [
    S.st.demo && h("div.banner", "Demo mode: sample pull requests and a scripted agent. Nothing is read from or posted to GitHub."),
    q.syncError && h("div.banner.red", q.syncError),
    q.mineError && h("div.banner.red", q.mineError),
    !S.st.agent.command && h("div.banner.red", "No agent is set up, so nothing can be read. ", h("a", { href: "#/settings/agent" }, "Pick one in Settings.")),
  ];
  const foot = h("div.row.rule-top", { class: "spread mid" },
    h("span.small.muted", `Checks GitHub every ${Math.round(S.st.config.pollSeconds / 60) || 1} min. `,
      h("span.kbd", "j"), " ", h("span.kbd", "k"), " move between rows, ", h("span.kbd", "Enter"), " opens one, ", h("span.kbd", "s"), " snoozes it."),
    h("button.btn.quiet.small", { onclick: async () => { try { await post("/api/sync"); } catch (e) { toast(e.message); } refresh(true); } }, "Check now"));

  if (tab === "waiting" || tab === "reviewed") {
    return h("div.sheet", h("div.body", banners, others(all, tab === "waiting" ? "waiting" : "done"), foot));
  }

  // The cover: how much there is, in words, and where to start.
  const cover = h("aside.cover", { "aria-label": "This issue" },
    h("div.cover-art", { "aria-hidden": "true" }, h("i"), h("i")),
    h("div.cover-n", { "aria-hidden": "true" }, list.length),
    h("h1.cover-head", list.length ? `${list.length === 1 ? "thing" : "things"} waiting on you ${tonight()}` : `Nothing waiting on you ${tonight()}`),
    h("p.cover-dek", list.length ? dek(list) : "Review requests, and your own pull requests when they need something from you, show up here on their own."),
    list.length > 0 && h("div.kv.small",
      h("span", "Reviews for others"), h("span.n", list.length - nMine),
      h("span", "Your own pull requests"), h("span.n", nMine),
      h("span", waiters.length ? `Waiting on you: ${waiters.slice(0, 3).join(", ")}${waiters.length > 3 ? ` and ${waiters.length - 3} more` : ""}` : "Nobody is waiting on you"), h("span.n", waiters.length),
      h("span", "All of it"), h("span.n", `${total} min`)),
    list.length > 0 && h("div.block", { class: "gap-u" },
      h("span.label.ink", "I have"),
      h("div.seg", { role: "group", "aria-label": "Time you have" },
        BUDGETS.map(([m, label]) => h("button", { "aria-pressed": String(m === b), onclick: () => setBudget(m) }, label))),
      b ? h("span.small.muted", `${fit} of ${total} min fit.`) : null),
    list.length > 0 && h("button.btn.red", { onclick: () => go(itemHref(list[0])) }, `Start: ${list[0].action} #${list[0].pr.number}`),
    h("div.block.cover-add", { class: "gap-u" },
      h("label.label", { for: "add" }, "Read another"),
      h("div.row", { class: "one-line gap-u" }, addInput, h("button.btn", { onclick: add }, "Read"))));

  return h("div.sheet.issue",
    cover,
    h("div.body.contents",
      banners,
      list.length > 0 ? [h("div.label.ink", "In this issue"), contents(list, { cut: b, opened, open, bands: TODO })]
        : h("div.block", h("h2.h2", "Nothing to do"), h("p.lede", "Nothing is asking for you. Waiting on others has what other people have.")),
      foot));
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
  else if (S.route.name === "queue" && e.key === "s" && t.matches && t.matches("[data-row][data-key]")) {
    const it = todo().find((x) => x.key === t.dataset.key);
    if (it) { opened = { key: it.key, kind: "snooze" }; render(); }
  }
});

// moveRow moves focus through the inbox's rows; Enter on a row opens it.
function moveRow(by) {
  const rows = [...document.querySelectorAll("[data-row][tabindex]")];
  if (!rows.length) return;
  const i = rows.indexOf(document.activeElement);
  const next = rows[i < 0 ? (by > 0 ? 0 : rows.length - 1) : Math.min(rows.length - 1, Math.max(0, i + by))];
  next.focus();
  next.scrollIntoView({ block: "nearest" });
}
connect();
load();
