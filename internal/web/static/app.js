import { h, get, post, toast, plural, ago, tri } from "./dom.js";
import { renderReview, reviewKeys, currentTimeTarget } from "./review.js";
import { renderRules } from "./rules.js";
import { renderMineList, renderMine } from "./mine.js";

const app = document.getElementById("app");
export const S = { st: null, review: null, draft: null, rules: null, mine: null, conv: null, route: {}, stale: false };

// --- routes: #/, #/r/<owner>/<repo>/<n>/<sheet>[/<id>], #/mine, #/m/<owner>/<repo>/<n>, #/rules, #/settings
function parse() {
  const p = location.hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
  if (p[0] === "r" && p.length >= 4) {
    return { name: "review", key: `${p[1]}/${p[2]}#${p[3]}`, sheet: p[4] || "brief", id: p[5] || "" };
  }
  if (p[0] === "m" && p.length >= 4) return { name: "minepr", key: `${p[1]}/${p[2]}#${p[3]}` };
  if (p[0] === "mine") return { name: "mine" };
  if (p[0] === "rules") return { name: "rules" };
  if (p[0] === "settings") return { name: "settings" };
  return { name: "queue" };
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
  } catch (e) {
    if (S.route.name === "review") { toast(e.message); go("#/"); return; }
    if (S.route.name === "minepr") { toast(e.message); go("#/mine"); return; }
    toast(e.message);
  }
  render();
}

// refresh re-reads the server without losing what the reviewer is typing.
export async function refresh(force) {
  const el = document.activeElement;
  if (!force && el && (el.tagName === "TEXTAREA" || (el.tagName === "INPUT" && el.type === "text"))) {
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
  else if (S.route.name === "settings") view = [masthead(), settings()];
  else if (S.route.name === "minepr" && S.mine) view = renderMine(S);
  else if (S.route.name === "mine") view = [masthead(), h("div.sheet", inboxTabs("mine"), h("div.body", renderMineList(S)))];
  else view = [masthead(), queue()];
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
      h("a", { href: "#/settings", class: "plain" }, h("span.label", "Settings"), h("span.cell-v", S.st.demo ? "demo mode" : "open"))));
}

// inboxTabs switches between the pull requests waiting on the user's review
// and the ones they wrote.
function inboxTabs(active) {
  const open = (S.st.queue.reviews || []).filter((r) => r.lane !== "done").length;
  const mine = S.st.mine || [], need = mine.filter((m) => m.lane === "needs-you").length;
  return h("nav.sheets", { "aria-label": "Inbox" },
    h("a", { href: "#/", "aria-current": active === "queue" ? "page" : null }, `To review · ${open}`),
    h("a", { href: "#/mine", "aria-current": active === "mine" ? "page" : null }, `Yours · ${mine.length}`, need ? h("span.stamp.red", { class: "count" }, `${need} need you`) : null));
}

// --- queue -------------------------------------------------------------------
function queue() {
  const q = S.st.queue, rs = q.reviews || [];
  const lanes = {
    judgment: ["Needs your judgment", "Sorted by what is at stake, not by age."],
    skim: ["Read it, no question for you", "The agent found nothing to ask, but behaviour changes here that a person should read."],
    clear: ["Nothing needs you", "Every claim checked out and no behaviour needs a person's eye. Sign after a spot-check."],
    reading: ["Being read", "Your agent has the branch checked out."],
    failed: ["Could not be read", ""],
    done: ["Signed", ""],
  };
  const open = rs.filter((r) => r.lane !== "done");
  const need = open.reduce((n, r) => n + r.needLines, 0), total = open.reduce((n, r) => n + r.totalLines, 0);
  const decisions = open.reduce((n, r) => n + (r.decisions - r.answered), 0);
  const minutes = open.filter((r) => r.status === "ready").reduce((n, r) => n + r.minutes, 0);

  const addInput = h("input.field.mono", { id: "add", placeholder: "owner/repo#123 or a pull request URL", "aria-label": "Pull request to read" });
  const add = async () => {
    if (!addInput.value.trim()) return;
    try { const r = await post("/api/add", { ref: addInput.value }); addInput.value = ""; go(href(r.key)); }
    catch (e) { toast(e.message); }
  };
  addInput.addEventListener("keydown", (e) => { if (e.key === "Enter") add(); });

  const row = (r) => {
    const p = r.pr;
    const ready = r.status === "ready" || r.status === "posted";
    const tris = [];
    for (let i = 1; i <= r.decisions; i++) tris.push(tri(i, i <= r.answered));
    return h("tr.go", { onclick: () => go(href(r.key)), tabindex: 0, onkeydown: (e) => { if (e.key === "Enter") go(href(r.key)); } },
      h("td", h("div.mono.small.muted", `${p.owner}/${p.repo} #${p.number}`),
        h("div", { class: "item-title" }, p.title),
        h("div.small.muted", `${p.author} · ${(p.why || []).join(", ")}${r.slice ? ` · ${r.slice} files are yours` : ""}`),
        r.pushed && h("div.small", { class: "text-red" }, "The author pushed since this was read. Reading again.")),
      h("td", ready
        ? (r.decisions ? h("div.row", { class: "gap-s" }, tris) : h("span.stamp.faint", "none"))
        : r.status === "failed" ? h("span.stamp.open", "failed")
        : h("span.small", h("span.live"), h("span", { "data-activity": r.key }, r.activity || "Queued"))),
      h("td", ready && h("div", h("div.num", `${r.needLines} of ${r.totalLines}`),
        h("div.meter", { title: "share of changed lines that need a person" }, h("i", { style: { width: (r.totalLines ? Math.max(r.needLines / r.totalLines * 100, r.needLines ? 4 : 0) : 0) + "%" } })))),
      h("td.r.num", ready ? `${r.minutes} min` : ""),
      h("td.r.small.muted", r.status === "failed" ? r.error.split("\n")[0].slice(0, 80) : r.contradicted ? h("span.stamp.contra", `${r.contradicted} contradicted`) : ago(p.updatedAt)));
  };

  const groups = Object.entries(lanes).map(([lane, [title, sub]]) => {
    const list = rs.filter((r) => r.lane === lane);
    if (!list.length) return null;
    return h("div.block",
      h("div.row", { class: "spread base" },
        h("h2.h2", `${title} · ${list.length}`), sub && h("span.small.muted", sub)),
      h("div.scroll-x", h("table.reg.queue",
        h("thead", h("tr", h("th", "Pull request"), h("th", "Decisions"), h("th", "Lines needing you"), h("th.r", "Your time"), h("th.r", ""))),
        h("tbody", list.map(row)))));
  });

  return h("div.sheet", inboxTabs("queue"), h("div.body",
    S.st.demo && h("div.banner", "Demo mode: sample pull requests (three to review, three of yours) and a scripted agent. Nothing is read from or posted to GitHub."),
    q.syncError && h("div.banner.red", q.syncError),
    !S.st.agent.command && h("div.banner.red", "No agent is set up, so nothing can be read. ", h("a", { href: "#/settings" }, "Pick one in Settings.")),
    h("div.tally",
      h("div", h("div.label", "Waiting on you"), h("div.big", open.length), h("div.small.muted", "pull requests, found automatically")),
      h("div", h("div.label", "Lines that need a person"), h("div.big.red", need), h("div.small.muted", total ? `of ${total} changed (${Math.round(need / total * 100)}%)` : "")),
      h("div", h("div.label", "Open decisions"), h("div.big", decisions), h("div.small.muted", minutes ? `about ${minutes} min of reading` : "")),
      h("div", { class: "stack vcenter" },
        h("label.label", { for: "add" }, "Read another"),
        h("div.row", { class: "one-line gap-u" }, addInput, h("button.btn", { onclick: add }, "Read")))),
    groups.some(Boolean) ? groups : h("div.block",
      h("h2.h2", "Nothing is waiting on you"),
      h("p.lede", "Pull requests where your review is requested or that are assigned to you appear here on their own, and your agent starts reading each one as it arrives.")),
    h("div.row", { class: "spread mid" },
      h("span.small.muted", `Checks GitHub every ${Math.round(S.st.config.pollSeconds / 60) || 1} min for review requests and assignments. `,
        h("span.kbd", "j"), " ", h("span.kbd", "k"), " move between rows, ", h("span.kbd", "Enter"), " opens one."),
      h("button.btn.quiet.small", { onclick: async () => { try { await post("/api/sync"); } catch (e) { toast(e.message); } refresh(true); } }, "Check now"))));
}

// --- settings ----------------------------------------------------------------
function settings() {
  const c = S.st.config;
  const save = async (patch) => { try { await post("/api/settings", patch); refresh(true); } catch (e) { toast(e.message); } };
  const theme = localStorage.getItem("docket-theme") || "auto";
  const setTheme = (t) => { localStorage.setItem("docket-theme", t); location.reload(); };
  const cmd = h("input.field.mono", { value: c.agentCommand || "", placeholder: "a command that starts an ACP agent on stdio", "aria-label": "Custom agent command" });
  return h("div.sheet", h("div.body",
    h("h1.title", "Settings"),
    h("div.block.rule-top",
      h("h2.h2", "Agent"),
      h("p.lede", "Docket never calls a model itself. It starts the coding agent you already use, over the Agent Client Protocol, under your own sign-in."),
      h("div.scroll-x", h("table.reg", h("tbody", S.st.agents.map((a) => h("tr",
        h("td", h("b", a.name), h("div.mono.small.muted", a.command)),
        h("td", a.ready ? h("span.stamp", "Ready") : a.found ? h("span.stamp.open", "Needs its ACP adapter") : h("span.stamp.faint", "Not found")),
        h("td.small.muted", a.ready ? "" : a.install),
        h("td.r", h("button.btn.small" + ((c.agent === a.id && !c.agentCommand) || (!c.agent && !c.agentCommand && S.st.agent.command === a.command) ? ".primary" : ""),
          { disabled: !a.ready, onclick: () => save({ agent: a.id }) }, "Use"))))))),
      h("div.row", { class: "one-line" }, cmd, h("button.btn", { onclick: () => save({ agentCommand: cmd.value }) }, "Use this command"))),
    h("div.block.rule-top",
      h("h2.h2", "What the agent may do"),
      h("label.check", h("input", { type: "checkbox", checked: c.runCode, onchange: (e) => save({ runCode: e.target.checked }) }),
        "Let the agent build and run the pull request's code in its throwaway checkouts"),
      h("p.small.muted", { class: "measure" }, "Running the code is what lets a claim be proved instead of argued, and it means code from the pull request executes on this computer. Turn it off for repositories whose authors you would not run code from; the agent then reads and searches only, and marks what it could not prove.")),
    h("div.block.rule-top",
      h("h2.h2", "Your pull requests"),
      h("p.lede", "When a reviewer comments or a check fails on a pull request you wrote, the agent can fix it, answer, and bring the branch up to date. It pushes new commits on top of your branch and never rewrites it."),
      [["auto", "Act on its own when it is safe", "A small model checks each step. A rename a reviewer asked for goes ahead; a dependency change, a CI edit, a large diff or a reply that pushes back waits for you. Everything it did has an Undo."],
       ["ask", "Always ask me first", "The agent plans every step, and none runs until you approve it."],
       ["off", "Off", "The agent never works on your pull requests."]].map(([v, label, text]) =>
        h("label.check", { class: "top" },
          h("input", { type: "radio", name: "tend", checked: (c.tend || "auto") === v, onchange: () => save({ tend: v }) }),
          h("span", h("b", label), h("div.small.muted", { class: "measure" }, text))))),
    h("div.block.rule-top",
      h("h2.h2", "Reviews"),
      h("label.check", h("input", { type: "checkbox", checked: c.receipt, onchange: (e) => save({ receipt: e.target.checked }) }),
        "Include the attention receipt in the review I post (what I read, what I took on the agent's proof)"),
      h("div.row", { class: "mid" }, h("span", "Appearance"),
        ["auto", "whiteprint", "blueprint"].map((t) => h("button.btn.small" + (theme === t ? ".primary" : ""), { onclick: () => setTheme(t) }, t)))),
    h("p.small.muted.mono", "Data: " + S.st.home)));
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
  else if ((S.route.name === "queue" || S.route.name === "mine") && (e.key === "j" || e.key === "k")) moveRow(e.key === "j" ? 1 : -1);
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
