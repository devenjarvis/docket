// Settings: what Docket did and what it cost (Metrics), and how it works
// (Agent, Policy, Reviews, Data). One sheet, a tab per part.

import { h, svg, get, post, toast, plural, ago } from "./dom.js";
import { S as state, refresh } from "./app.js";

export const TABS = [["metrics", "Metrics"], ["agent", "Agent"], ["policy", "Policy"], ["reviews", "Reviews"], ["data", "Data"]];
const PERIODS = [[7, "7 days"], [30, "30 days"], [90, "90 days"], [0, "All"]];

export function period() {
  const raw = localStorage.getItem("docket-period");
  const v = Number(raw);
  return raw !== null && PERIODS.some(([d]) => d === v) ? v : 30;
}

// load fetches what a tab shows.
export async function loadSettings(S) {
  const tab = S.route.tab;
  S.settings = S.settings || {};
  if (tab === "metrics") S.settings.metrics = await get("/api/metrics?days=" + period());
  if (tab === "agent") S.settings.models = await get("/api/metrics?days=0");
  if (tab === "policy") S.settings.repos = (await get("/api/repos")).repos;
  if (tab === "data") {
    const [data, log] = await Promise.all([get("/api/data"), get("/api/logtail?n=200")]);
    S.settings.data = data;
    S.settings.log = log.lines;
  }
}

// --- formatting ------------------------------------------------------------
export function dur(s) {
  s = Math.round(s || 0);
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h` + (m % 60 ? ` ${m % 60} min` : "");
}
export function money(x) {
  if (!x) return "$0";
  if (x < 0.01) return "<$0.01";
  if (x < 100) return "$" + x.toFixed(2);
  return "$" + Math.round(x).toLocaleString("en-US");
}
export function tokens(n) {
  n = n || 0;
  if (n < 1000) return String(n);
  if (n < 1e6) return (n / 1000).toFixed(n < 1e4 ? 1 : 0) + "k";
  return (n / 1e6).toFixed(n < 1e7 ? 2 : 1) + "M";
}
const pct = (a, b) => (b ? Math.round(a / b * 100) : 0);
const num = (n) => (n || 0).toLocaleString("en-US");

// niceMax rounds v up to 1, 2 or 5 times a power of ten, for an axis.
function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

function bucketLabel(iso, unit) {
  const d = new Date(iso);
  if (unit === "month") return d.toLocaleDateString("en-US", { month: "short", year: "2-digit" });
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

// --- charts ------------------------------------------------------------------

// columns draws stacked columns, one per bucket. series: [{label, cls, get(b)}].
function columns(buckets, unit, series, fmt, what) {
  const totals = buckets.map((b) => series.reduce((n, s) => n + s.get(b), 0));
  // Four steps of a round size, so every gridline has a round label.
  const top = niceMax(Math.max(...totals, 0) / 4) * 4;
  const tip = h("div.tip", { hidden: true, role: "status" });
  const cols = h("div.cols", buckets.map((b, i) => {
    const segs = series.map((s) => {
      const v = s.get(b);
      return v > 0 ? h("i", { class: s.cls, style: { height: `calc(${v / top * 100}% - 2px)` } }) : null;
    });
    const col = h("div.col", { tabindex: 0, "aria-label": `${bucketLabel(b.start, unit)}: ${fmt(totals[i])} ${what}` }, segs);
    const show = () => {
      tip.replaceChildren(
        h("div", unit === "week" ? "Week of " : "", bucketLabel(b.start, unit), ": ", h("span.num", fmt(totals[i])), " ", what),
        h("div", series.map((s) => `${fmt(s.get(b))} ${s.label.toLowerCase()}`).join(" · ")));
      tip.hidden = false;
      const x = col.offsetLeft + col.offsetWidth / 2, w = tip.offsetWidth;
      tip.style.left = Math.max(0, Math.min(cols.clientWidth - w, x - w / 2)) + "px";
    };
    col.addEventListener("mouseenter", show);
    col.addEventListener("focus", show);
    col.addEventListener("mouseleave", () => { tip.hidden = true; });
    col.addEventListener("blur", () => { tip.hidden = true; });
    return col;
  }), tip);
  const every = Math.ceil(buckets.length / 8);
  return h("div.colchart",
    h("div.axis", { "aria-hidden": "true" }, h("span.sizer", fmt(top)), [4, 3, 2, 1, 0].map((k) => h("span", { style: { top: (100 - k * 25) + "%" } }, fmt(top * k / 4)))),
    cols,
    h("div.ticks", buckets.map((b, i) => h("span", i % every ? "" : bucketLabel(b.start, unit)))));
}

function legend(series) {
  return h("div.legend", series.map((s) => h("span", h("i.sw", { class: s.cls }), s.label)));
}

function chart(title, aside, ...body) {
  return h("div.chart", h("header", h("span.label.ink", title), aside), h("div.plot", body));
}

// hbars draws labelled horizontal bars on one scale. rows: [label, value, text, red?].
function hbars(rows) {
  const top = Math.max(...rows.map((r) => r[1]), 0) || 1;
  return h("div.hbars", rows.map(([label, v, text, red]) => [
    h("span.small", label),
    h("div.track" + (red ? ".red" : ""), { role: "img", "aria-label": `${label}: ${text}` }, h("i", { style: { width: (v / top * 100) + "%" } })),
    h("span.v", text)]));
}

// scatter plots each signed review: lines that needed the reviewer across,
// minutes up.
function scatter(points) {
  const host = h("div.scatter");
  const plot = svg("svg", { role: "img", "aria-label": `${points.length} reviews: minutes spent against lines that needed you` });
  host.append(plot);
  const xs = points.map((p) => p.need), ys = points.map((p) => p.seconds / 60);
  const xMax = niceMax(Math.max(...xs, 10)), yMax = niceMax(Math.max(...ys, 5));
  const draw = () => {
    const W = host.clientWidth, H = host.clientHeight;
    if (!W) return;
    const L = 40, B = 28, T = 10, R = 20;
    const x = (v) => L + v / xMax * (W - L - R), y = (v) => H - B - v / yMax * (H - B - T);
    const kids = [];
    for (let k = 0; k <= 4; k++) {
      const v = yMax * k / 4, xv = xMax * k / 4;
      kids.push(svg("line", { x1: L, x2: W - R, y1: y(v), y2: y(v), class: "grid" }));
      kids.push(svg("text", { x: L - 6, y: y(v) + 4, "text-anchor": "end" }, Math.round(v)));
      kids.push(svg("text", { x: x(xv), y: H - 8, "text-anchor": "middle" }, Math.round(xv)));
    }
    kids.push(svg("line", { x1: L, x2: W - R, y1: H - B, y2: H - B, class: "base" }));
    for (const p of points) {
      kids.push(svg("rect", { x: x(p.need) - 4, y: y(p.seconds / 60) - 4, width: 8, height: 8, class: "dot" },
        svg("title", {}, `${p.repo} #${p.number}: ${p.need} lines needing you, ${dur(p.seconds)}`)));
    }
    plot.replaceChildren(...kids);
  };
  new ResizeObserver(draw).observe(host);
  return host;
}

// kv is a two-column list of named numbers.
function kv(rows) {
  return h("div.kv", rows.filter(Boolean).map(([k, v]) => [h("span", k), h("span.n", v)]));
}

// --- the page ----------------------------------------------------------------
export function renderSettings(S) {
  const tab = S.route.tab;
  const body = { metrics, agent, policy, reviews, data }[tab] || metrics;
  return h("div.sheet",
    h("nav.sheets", { "aria-label": "Settings" },
      TABS.map(([id, label]) => h("a", { href: "#/settings/" + id, "aria-current": tab === id ? "page" : null }, label))),
    h("div.body", body(S)));
}

const save = async (patch) => { try { await post("/api/settings", patch); refresh(true); } catch (e) { toast(e.message); } };

// --- Metrics ------------------------------------------------------------------
function metrics(S) {
  const m = S.settings && S.settings.metrics;
  if (!m) return h("p.lede", "Loading…");
  const r = m.report, c = r.cost, days = period();
  const span = days ? `the last ${days} days` : "all the time Docket has kept a ledger";
  const periodPicker = h("div.seg", { role: "group", "aria-label": "Period" }, PERIODS.map(([d, label]) =>
    h("button", { "aria-pressed": String(days === d), onclick: () => { localStorage.setItem("docket-period", d); refresh(true); } }, label)));

  let title = `You signed ${plural(r.signed, "review")} in ${days ? days + " days" : "all"}, in a median ${dur(r.yourMedian)} each`;
  if (!r.signed) title = `You signed no reviews in ${span}`;
  const head = h("div.row", { class: "spread base" },
    h("div.block", { class: "gap-s lead" },
      h("h1.title", title),
      h("p.lede", "Counted from the ledger Docket keeps of every read, question, review and step the agent takes on your pull requests. ",
        r.empty ? "The ledger is empty: it fills from now on." : `It starts ${new Date(r.since).toLocaleDateString("en-US", { month: "long", day: "numeric", year: "numeric" })}.`,
        " Nothing here leaves this computer.")),
    periodPicker);

  const delta = r.signedBefore || r.signed ? `${r.signed - r.signedBefore >= 0 ? "+" : "−"}${Math.abs(r.signed - r.signedBefore)} on the ${days ? days + " days" : "period"} before` : "none before";
  const tally = h("div.tally",
    h("div", h("div.label", "Reviews signed"), h("div.big", r.signed), h("div.delta", days ? delta : `${plural(r.reads, "read")} in all`)),
    h("div", h("div.label", "Your time, median"), h("div.big", dur(r.yourMedian)), h("div.delta", `${dur(r.yourTotal)} in all`)),
    h("div", h("div.label", "Agent reading, median"), h("div.big", dur(r.readMedian)), h("div.delta", `${plural(r.reads, "read")}, ${r.readsFailed} failed`)),
    h("div", h("div.label", "Lines that needed you"), h("div.big.red", pct(r.needLines, r.totalLines) + "%"), h("div.delta", `${num(r.needLines)} of ${num(r.totalLines)} changed`)),
    h("div", h("div.label", "Agent cost"), h("div.big", money(c.total)), h("div.delta", c.perSigned ? `${money(c.perSigned)} per signed review` : `${tokens(c.tokens.totalTokens)} tokens`)));

  const verdicts = [
    { label: "Approved", cls: "f-ink", get: (b) => b.verdicts.APPROVE || 0 },
    { label: "Changes requested", cls: "f-hatch", get: (b) => b.verdicts.REQUEST_CHANGES || 0 },
    { label: "Commented", cls: "f-open", get: (b) => b.verdicts.COMMENT || 0 },
  ];
  const signedChart = chart(`Reviews signed per ${r.unit}`, legend(verdicts), columns(r.buckets, r.unit, verdicts, (v) => String(Math.round(v)), "signed"));

  const time = [["Decisions", r.timeBy.decisions || 0, true], ["Diff", r.timeBy.diff || 0], ["Brief", r.timeBy.brief || 0], ["Context", r.timeBy.context || 0]];
  const timeChart = chart("Where your time went", h("span.small.muted", `active time, ${dur(r.yourTotal)}`),
    r.yourTotal ? hbars(time.map(([l, v, red]) => [l, v, dur(v), red])) : h("p.small.muted", "No time recorded in this period."));
  const scatterChart = chart("Your time against the lines that needed you", h("span.small.muted", "one square per review · minutes up, lines across"),
    r.points.length ? scatter(r.points) : h("p.small.muted", "No signed reviews in this period."));

  return [head, tally, signedChart, h("div.grid2", timeChart, scatterChart), costSection(r), h("div.grid2",
    h("div.block",
      h("h2.h2", "The agent's reading"),
      kv([
        ["Pull requests read", r.reads],
        ["Read again after the author pushed", r.rereads],
        ["Briefs sent back by the validator", r.sentBack],
        ["Reads that failed", r.readsFailed],
        ["Kept to Docket's file policy", `${r.enforced} of ${r.reads - r.readsFailed}`],
        ["Claims proved · contradicted · not proven", `${r.proved} · ${r.contradicted} · ${r.unproven}`],
      ])),
    h("div.block",
      h("h2.h2", "Your decisions"),
      kv([
        ["Decisions answered", r.answered],
        ["Took the agent's draft as written", Math.max(r.answered - r.edited, 0)],
        ["Edited the draft comment", r.edited],
        ["Blocking comments posted", r.blocking],
        ["Follow-up questions to the agent", r.asks],
        ["Answers carried over on a re-read", r.carried],
        ["Mechanical sections you spot-checked", `${r.audited} of ${r.mechanical}`],
        ["Attention receipt included", `${r.receipts} of ${r.signed}`],
      ]))),
  rulebook(m.rules), tendSection(r.tend), familiarity(m.map)];
}

function costSection(r) {
  const c = r.cost;
  const jobs = [
    { label: "Reading", cls: "f-ink", get: (b) => b.cost.reading || 0 },
    { label: "Questions", cls: "f-hatch", get: (b) => b.cost.questions || 0 },
    { label: "Your pull requests", cls: "f-open", get: (b) => b.cost.mine || 0 },
  ];
  const job = (k) => c.byJob[k] || { cost: 0, tokens: 0, runs: 0 };
  const source = (m) => m.reported && m.estimated ? h("span.stamp.faint", "reported and estimated")
    : m.reported ? h("span.stamp", "reported")
    : m.estimated ? h("span.stamp.open", "estimated")
    : h("span.stamp.faint", "no price");
  const notes = [];
  if (c.runs && c.reporting < c.runs) notes.push(`${c.reporting} of ${plural(c.runs, "agent run")} reported usage; the rest add nothing here.`);
  if (c.unpriced.length) notes.push(h("span", `No cost was reported for ${c.unpriced.join(", ")}, and no price is set, so those tokens are not counted in dollars. `, h("a", { href: "#/settings/agent" }, "Set a price under Agent.")));
  if (c.other.length) notes.push(`Costs reported in ${c.other.join(", ")} are left out of the dollar total.`);
  return h("div.block.rule-top",
    h("div.row", { class: "spread base" }, h("h2.h2", "What the agent cost"),
      h("span.small.muted", `${money(c.total)} · ${tokens(c.tokens.totalTokens)} tokens`)),
    h("p.small.muted.measure", "Your agent reports cost and tokens over the Agent Client Protocol. Where it reports tokens but no cost, Docket estimates from the prices you set under Agent; the table says which is which."),
    h("div.grid2",
      chart(`Cost per ${r.unit}`, legend(jobs), columns(r.buckets, r.unit, jobs, (v) => money(v), "")),
      h("div.block",
        kv([
          ["Reading pull requests", `${money(job("reading").cost)} · ${plural(job("reading").runs, "run")}`],
          ["Questions from Your call", `${money(job("questions").cost)} · ${plural(job("questions").runs, "run")}`],
          ["Your pull requests", `${money(job("mine").cost)} · ${plural(job("mine").runs, "run")}`],
          ["Per signed review (reading and questions)", c.perSigned ? money(c.perSigned) : "—"],
          ["Per run on your pull requests", c.perRun ? money(c.perRun) : "—"],
          ["Reported by the agent · estimated", `${money(c.reported)} · ${money(c.estimated)}`],
        ]),
        notes.length ? h("div.pencil.small", h("div.label", "About these numbers"), notes.map((n) => h("p", n))) : null)),
    c.byModel.length ? h("div.scroll-x", h("table.reg",
      h("thead", h("tr", h("th", "Model"), h("th.r", "Runs"), h("th.r", "Input"), h("th.r", "Output"), h("th.r", "Thinking"), h("th.r", "Cache read"), h("th.r", "Cost"), h("th", "Cost is"))),
      h("tbody", c.byModel.map((m) => h("tr",
        h("td", m.model || h("span.muted", "not named")),
        h("td.r.num", m.runs), h("td.r.num", tokens(m.inputTokens)), h("td.r.num", tokens(m.outputTokens)),
        h("td.r.num", tokens(m.thoughtTokens)), h("td.r.num", tokens(m.cachedReadTokens)),
        h("td.r.num", money(m.reported + m.estimated)), h("td", source(m)))))))
      : h("p.small.muted", "No usage reported in this period."));
}

function rulebook(rules) {
  return h("div.block.rule-top",
    h("div.row", { class: "spread base" }, h("h2.h2", "Rulebook"), h("a.small", { href: "#/rules" }, "Open the rulebook")),
    rules.length ? h("div.scroll-x", h("table.reg",
      h("thead", h("tr", h("th", "Rule"), h("th.r", "Fired"), h("th.r", "Kept"), h("th.r", "Waved off"), h("th", "Kept share"), h("th", ""))),
      h("tbody", rules.map((r) => {
        const judged = r.stats.agreed + r.stats.overrode;
        return h("tr",
          h("td", h("span.mono.small.muted", r.id + " "), r.text),
          h("td.r.num", r.stats.fired), h("td.r.num", r.stats.agreed), h("td.r.num", r.stats.overrode),
          h("td", h("div.bar", { class: "w-meter", role: "img", "aria-label": `${pct(r.stats.agreed, judged)}% kept` }, h("i", { style: { width: pct(r.stats.agreed, judged) + "%" } }))),
          h("td", r.fading ? h("span.stamp.open", "Fading") : r.status === "trial" ? h("span.stamp.faint", "Trial") : null));
      }))))
      : h("p.small.muted", "The rulebook is empty."));
}

function tendSection(t) {
  const o = t.outcomes || {};
  return h("div.block.rule-top",
    h("h2.h2", "The agent on your pull requests"),
    t.runs ? h("div.grid2",
      hbars([
        ["Went ahead on its own", o.auto || 0, plural(o.auto || 0, "step")],
        ["Held, then done by you", o.approved || 0, plural(o.approved || 0, "step"), true],
        ["Held, sent back", o.rejected || 0, plural(o.rejected || 0, "step"), true],
        ["Undone by you", o.undone || 0, plural(o.undone || 0, "step"), true],
      ]),
      kv([
        [`Runs (${t.autoRuns} started by a comment or a failed check)`, t.runs],
        ["Runs that failed", t.failed],
        ["Held steps, by a fixed rule", t.floor],
        ["Held steps, by the gate's judgment", t.judged],
        ["Stopped after three runs in a row", t.stopped],
      ]))
      : h("p.small.muted", "The agent did not work on your pull requests in this period."));
}

function familiarity(map) {
  const repos = Object.entries(map || {}).sort((a, b) => a[0].localeCompare(b[0]));
  const shade = (n) => (n >= 10 ? 4 : n >= 6 ? 3 : n >= 3 ? 2 : n >= 1 ? 1 : 0);
  return h("div.block.rule-top",
    h("h2.h2", "What you know of each repository"),
    h("p.small.muted.measure", "Reviews through Docket per area: the map the agent is given to decide when you need a Context sheet."),
    repos.length ? [
      h("div.scroll-x", h("table.reg",
        h("thead", h("tr", h("th", { class: "fit" }, "Repository"), h("th", "Areas, by reviews"))),
        h("tbody", repos.map(([repo, areas]) => h("tr",
          h("td.mono.small", { class: "nowrap" }, repo),
          h("td", h("div.areas", Object.entries(areas).sort((a, b) => b[1].reviews - a[1].reviews).map(([area, s]) =>
            h("span.area", { class: "hm" + shade(s.reviews), title: `${s.reviews} reviews, last ${ago(s.last)}` }, h("span.mono", area), h("b", s.reviews)))))))))),
      h("div.legend", [["hm1", "1–2"], ["hm2", "3–5"], ["hm3", "6–9"], ["hm4", "10 or more"]].map(([c, l]) => h("span", h("i.sw", { class: c }), l)))]
      : h("p.small.muted", "Nothing yet. Each review you post adds the areas you read."));
}

// --- Agent --------------------------------------------------------------------
function agent(S) {
  const st = state.st, c = st.config;
  const cmd = h("input.field.mono", { value: c.agentCommand || "", placeholder: "a command that starts an ACP agent on stdio", "aria-label": "Custom agent command" });
  const dir = h("input.field.mono", { placeholder: "/path/to/folder", "aria-label": "Folder to search for agents" });
  const addDir = () => { if (dir.value.trim()) save({ agentPath: [...c.agentPath, dir.value.trim()] }); };
  dir.addEventListener("keydown", (e) => { if (e.key === "Enter") addDir(); });
  const restart = st.started && st.started.concurrency !== c.concurrency;
  const checkOut = h("span.small", { "aria-live": "polite" });
  const shown = (r) => r.ok
    ? h("span", h("span.stamp", "Ready"), ` ${r.name} started in ${r.seconds < 0.1 ? "under 0.1" : r.seconds.toFixed(1)} s${r.model && r.model !== r.name ? ", on " + r.model : ""}`)
    : h("span", h("span.stamp.contra", "Failed"), " ", r.error);
  if (S.settings.check) checkOut.append(shown(S.settings.check));
  const check = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    checkOut.replaceChildren(h("span.live"), "Starting the agent…");
    try {
      const r = await post("/api/agent/check");
      S.settings.check = r;
      checkOut.replaceChildren(shown(r));
      // The models it offered can now be priced.
      if (r.ok && r.models && r.models.length) refresh(true);
    } catch (err) { checkOut.replaceChildren(err.message); }
    btn.disabled = false;
  };
  return [
    h("h1.title", "Agent"),
    h("p.lede", "Docket never calls a model itself. It starts the coding agent you already use, over the Agent Client Protocol, under your own sign-in."),
    h("div.scroll-x", h("table.reg", h("tbody", st.agents.map((a) => h("tr",
      h("td", h("b", a.name), h("div.mono.small.muted", a.command)),
      h("td", a.ready ? h("span.stamp", "Ready") : a.found ? h("span.stamp.open", "Needs its ACP adapter") : h("span.stamp.faint", "Not found")),
      h("td.small.muted", a.ready ? "" : a.install),
      h("td.r", h("button.btn.small" + ((c.agent === a.id && !c.agentCommand) || (!c.agent && !c.agentCommand && st.agent.command === a.command) ? ".primary" : ""),
        { disabled: !a.ready, onclick: () => save({ agent: a.id }) }, "Use"))))))),
    h("div",
      setting("Custom command", "Any command that starts an ACP agent on stdio. It overrides the list above.",
        h("div.row", { class: "one-line gap-u" }, cmd, h("button.btn", { onclick: () => save({ agentCommand: cmd.value }) }, "Use this command"))),
      setting("Where to look for agents", "Folders searched before your login shell's PATH. Use this when Docket started from the Dock cannot find an agent your terminal can.",
        h("div.stack",
          c.agentPath.length ? h("div.row", { class: "gap-u" }, c.agentPath.map((d) => h("span.chip", d,
            h("button", { "aria-label": "Remove " + d, onclick: () => save({ agentPath: c.agentPath.filter((x) => x !== d) }) }, "×")))) : h("span.small.muted", "None: only your login shell's PATH."),
          h("div.row", { class: "one-line gap-u" }, dir, h("button.btn", { onclick: addDir }, "Add")))),
      setting("Read at once", "How many pull requests the agent reads at the same time. Each read holds two checkouts and one agent process.",
        h("div.row", { class: "mid" },
          h("span.stepper",
            h("button", { "aria-label": "Fewer", disabled: c.concurrency <= 1, onclick: () => save({ concurrency: c.concurrency - 1 }) }, "−"),
            h("output", { "aria-live": "polite" }, c.concurrency),
            h("button", { "aria-label": "More", disabled: c.concurrency >= 6, onclick: () => save({ concurrency: c.concurrency + 1 }) }, "+")),
          h("span.small.muted", restart ? "Takes effect the next time Docket starts." : "1 to 6"))),
      setting("Check the agent", "Starts the agent, opens a session in an empty folder and stops it, so a broken install shows here and not on your next pull request.",
        h("div.row", { class: "mid" }, h("button.btn", { onclick: check }, "Check now"), checkOut))),
    prices(S)];
}

function setting(title, text, control) {
  return h("div.setting", h("div", h("b", title), text && h("p.small.muted", text)), h("div", control));
}

// prices lets the user price the models their agent uses, for runs that
// report tokens but no cost.
function prices(S) {
  const c = state.st.config, saved = c.prices || {};
  const seen = ((S.settings.models && S.settings.models.report.cost.byModel) || []).map((m) => m.model).filter(Boolean);
  const checked = (S.settings.check && S.settings.check.models) || [];
  const names = [...new Set([...Object.keys(saved), ...seen, ...checked])].sort();
  const fields = ["input", "output", "cache_read", "cache_write"];
  const inputs = {};
  const row = (name) => {
    inputs[name] = {};
    const p = saved[name] || {};
    return h("tr",
      h("td", name),
      fields.map((f) => h("td.r", inputs[name][f] = h("input.field.mono.price", { type: "number", min: 0, step: "0.01", value: p[f] ?? "", "aria-label": `${name}: ${f.replace("_", " ")} price`, placeholder: "—" }))),
      h("td", saved[name] ? h("span.stamp", "Priced") : h("span.stamp.faint", "No price")));
  };
  const newName = h("input.field", { placeholder: "a model's name, as the agent reports it", "aria-label": "Model name" });
  const store = () => {
    const out = {};
    for (const [name, f] of Object.entries(inputs)) {
      const p = {};
      let any = false;
      for (const k of fields) { const v = parseFloat(f[k].value); if (!isNaN(v)) { p[k] = v; any = true; } }
      if (any) out[name] = { input: 0, output: 0, ...p };
    }
    return out;
  };
  return h("div.block.rule-top",
    h("h2.h2", "Model prices"),
    h("p.lede", "When the agent reports a cost, Metrics uses it. When it reports only tokens, Metrics estimates the cost from these prices, in US dollars per million tokens. Leave a model blank to count its tokens without a cost."),
    names.length ? h("div.scroll-x", h("table.reg",
      h("thead", h("tr", h("th", "Model"), h("th.r", "Input"), h("th.r", "Output"), h("th.r", "Cache read"), h("th.r", "Cache write"), h("th", ""))),
      h("tbody", names.map(row)))) : h("p.small.muted", "No model has reported usage yet. Check the agent above to list its models, or add one by name."),
    h("div.row", { class: "one-line gap-u" }, newName, h("button.btn", { onclick: () => {
      const n = newName.value.trim();
      if (!n) return;
      save({ prices: { ...store(), [n]: { input: 0, output: 0 } } });
    } }, "Add model")),
    names.length ? h("div.row", h("button.btn.primary", { onclick: () => save({ prices: store() }) }, "Save prices")) : null);
}

// --- Policy -------------------------------------------------------------------
function policy(S) {
  const c = state.st.config, repos = S.settings.repos || [];
  const setRepo = async (repo, patch) => {
    try { const r = await post("/api/repo", { repo, ...patch }); S.settings.repos = r.repos; refresh(true); } catch (e) { toast(e.message); }
  };
  const tendLabel = { auto: "Act when safe", ask: "Always ask", off: "Off" };
  const sel = (label, value, opts, onchange, disabled) => h("select.field", { "aria-label": label, disabled, onchange: (e) => onchange(e.target.value) },
    opts.map(([v, t]) => h("option", { value: v, selected: value === v }, t)));
  const addRepo = h("input.field.mono", { placeholder: "owner/name", "aria-label": "Repository to add" });
  const off = repos.filter((r) => r.off).length;
  return [
    h("h1.title", "What the agent may do"),
    setting("Run the pull request's code", "Running the code is what lets a claim be proved instead of argued, and it means code from the pull request executes on this computer. Off, the agent reads and searches only, and marks what it could not prove.",
      h("label.check", h("input", { type: "checkbox", checked: c.runCode, onchange: (e) => save({ runCode: e.target.checked }) }),
        "Everywhere, unless a repository below says otherwise")),
    h("div.block",
      h("div.row", { class: "spread base" }, h("h2.h2", "Per repository"),
        h("span.small.muted", off ? `${plural(off, "repository")} turned off` : "Every repository Docket has met")),
      h("p.small.muted.measure", "Turn Docket off for a repository and it leaves it alone: its pull requests are not read, listed or worked on, and the ones in your queue go. Turning it back on finds them again."),
      h("div.scroll-x", h("table.reg.repos",
        h("thead", h("tr", h("th", "Repository"), h("th", "Docket"), h("th", "Run code"), h("th", "Agent on your pull requests"), h("th.r", "Signed"))),
        h("tbody", repos.length ? repos.map((r) => h("tr", { class: r.off ? "is-off" : null },
          h("td.mono", r.repo, r.open && !r.off ? h("div.small.muted", `${r.open} open now`) : null),
          h("td", sel(`Docket in ${r.repo}`, r.off ? "off" : "on", [["on", "On"], ["off", "Off"]], (v) => setRepo(r.repo, { off: v === "off" }))),
          h("td", sel(`Run code in ${r.repo}`, r.runCode, [["default", `Default (${c.runCode ? "on" : "off"})`], ["on", "On"], ["off", "Off"]], (v) => setRepo(r.repo, { runCode: v }), r.off)),
          h("td", sel(`Agent on your pull requests in ${r.repo}`, r.tend, [["default", `Default (${tendLabel[c.tend].toLowerCase()})`], ...Object.entries(tendLabel)], (v) => setRepo(r.repo, { tend: v }), r.off)),
          h("td.r.num", r.reviews)))
          : h("tr", h("td", { colspan: 5 }, h("span.small.muted", "No repository yet. Add one below to set it before its first pull request arrives.")))))),
      h("div.row", { class: "one-line gap-u" }, addRepo,
        h("button.btn", { onclick: () => { if (addRepo.value.trim()) setRepo(addRepo.value.trim(), { off: true }); } }, "Turn off"),
        h("button.btn.quiet", { onclick: () => { if (addRepo.value.trim()) setRepo(addRepo.value.trim(), { runCode: "off" }); } }, "Never run its code"))),
    h("div.block.rule-top",
      h("h2.h2", "Your pull requests"),
      h("p.lede", "When a reviewer comments or a check fails on a pull request you wrote, the agent can fix it, answer, and bring the branch up to date. It pushes new commits on top of your branch and never rewrites it."),
      [["auto", "Act on its own when it is safe", "A small model checks each step. A rename a reviewer asked for goes ahead; a dependency change, a CI edit, a large diff or a reply that pushes back waits for you. Everything it did has an Undo."],
       ["ask", "Always ask me first", "The agent plans every step, and none runs until you approve it."],
       ["off", "Off", "The agent never works on your pull requests."]].map(([v, label, text]) =>
        h("label.check", { class: "top" },
          h("input", { type: "radio", name: "tend", checked: (c.tend || "auto") === v, onchange: () => save({ tend: v }) }),
          h("span", h("b", label), h("div.small.muted", { class: "measure" }, text)))),
      h("div.pencil.small", h("div.label", "Always held for you"),
        "Dependency manifests and lockfiles, toolchain versions, CI configuration, deleted or skipped tests, more than 60 changed lines, conflict resolutions, and replies that are not agreement or plain information. These are fixed and cannot be turned off.")),
  ];
}

// --- Reviews ------------------------------------------------------------------
// The colourways in docket.css: id, name, and the two riso drums it is printed with.
const INKS = [
  ["black-tomato", "Black and Tomato", "Black · Bright Red"],
  ["federal-pink", "Federal and Pink", "Federal Blue · Fluorescent Pink"],
  ["hunter-orange", "Hunter and Orange", "Hunter Green · Fluorescent Orange"],
  ["plum-sunflower", "Plum and Sunflower", "Plum · Sunflower"],
  ["teal-yellow", "Teal and Yellow", "Teal · Yellow"],
  ["blue-orange", "Blue and Orange", "Medium Blue · Orange"],
];
const NOTIFY = {
  read: "A pull request is read and waits on me",
  lane: "One of mine becomes ready to merge, or starts to need me",
  activity: "Someone reviews or comments on one of mine",
  tend: "The agent did steps on one of mine, or holds one for me",
};

function reviews(S) {
  const st = state.st, c = st.config;
  let theme = localStorage.getItem("docket-theme") || "auto";
  if (theme === "whiteprint") theme = "light";
  if (theme === "blueprint") theme = "dark";
  const ink = document.documentElement.dataset.ink;
  const mode = document.documentElement.dataset.theme;
  const keep = (k, v) => {
    localStorage.setItem(k, v);
    window.docketApplyTheme();
    refresh(true);
  };
  const polls = [[60, "1 minute"], [120, "2 minutes"], [300, "5 minutes"], [600, "10 minutes"], [900, "15 minutes"]];
  if (!polls.some(([s]) => s === c.pollSeconds)) polls.push([c.pollSeconds, dur(c.pollSeconds)]);
  const restart = st.started && st.started.pollSeconds !== c.pollSeconds;
  const off = new Set(c.notifyOff);
  return [
    h("h1.title", "Reviews and the app"),
    h("div",
      setting("Check GitHub every", "For review requests, assignments, and changes on your own pull requests.",
        h("div.row", { class: "mid" },
          h("select.field", { "aria-label": "How often to check GitHub", onchange: (e) => save({ pollSeconds: Number(e.target.value) }) },
            polls.map(([s, l]) => h("option", { value: s, selected: c.pollSeconds === s }, l))),
          restart && h("span.small.muted", "Takes effect the next time Docket starts."))),
      setting("Attention receipt", "A short list at the end of the review you post: what you read, and what you took on the agent's proof.",
        h("label.check", h("input", { type: "checkbox", checked: c.receipt, onchange: (e) => save({ receipt: e.target.checked }) }), "Include it in the review I post")),
      setting("Notifications", "In the desktop app.",
        h("div.stack", { class: "gap-s" }, st.notifyKinds.map((k) => h("label.check",
          h("input", { type: "checkbox", checked: !off.has(k), onchange: (e) => save({ notifyOff: e.target.checked ? c.notifyOff.filter((x) => x !== k) : [...c.notifyOff, k] }) }),
          NOTIFY[k] || k)))),
      setting("Light or dark", "Auto follows the system.",
        h("div.seg", { role: "group", "aria-label": "Light or dark" }, [["auto", "Auto"], ["light", "Light"], ["dark", "Dark"]].map(([t, l]) =>
          h("button", { "aria-pressed": theme === t ? "true" : "false", onclick: () => keep("docket-theme", t) }, l)))),
      setting("Colourway", "Docket is printed in two inks. The darker one carries every word; the brighter one only ever marks what is yours to do.",
        h("div.inks", { role: "group", "aria-label": "Colourway" }, INKS.map(([id, name, drums]) =>
          h("button.ink-choice", { "aria-pressed": ink === id ? "true" : "false", onclick: () => keep("docket-ink", id) },
            h("span.swatch", { "data-ink": id, "data-theme": mode, "aria-hidden": "true" }, h("i"), h("i")),
            h("b", name), h("small", drums))))),
      setting("Keyboard", "",
        h("div.small.text-2", h("span.kbd", "j"), " ", h("span.kbd", "k"), " move between rows · ", h("span.kbd", "Enter"), " opens one · ",
          h("span.kbd", "1"), "–", h("span.kbd", "9"), " choose an option · ", h("span.kbd", "n"), " ", h("span.kbd", "p"), " next and previous sheet · ", h("span.kbd", "s"), " sign-off"))),
  ];
}

// --- Data ---------------------------------------------------------------------
function bytes(n) {
  if (n < 1024) return `${n} B`;
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return `${n < 10 ? n.toFixed(1) : Math.round(n)} ${u[i]}`;
}

function data(S) {
  const d = S.settings.data, log = S.settings.log || [];
  if (!d) return h("p.lede", "Loading…");
  let armed = false;
  const clear = h("button.btn.red", { onclick: async () => {
    if (!armed) { armed = true; clear.textContent = "Clear it: this cannot be undone"; return; }
    try { await post("/api/ledger/clear"); toast("The metrics ledger is empty."); refresh(true); } catch (e) { toast(e.message); }
  } }, "Clear the metrics ledger");
  const sweep = async (e) => {
    e.currentTarget.disabled = true;
    try { const r = await post("/api/sweep"); toast(r.freed ? `Freed ${bytes(r.freed)}.` : "Nothing to remove: every checkout is in use."); refresh(true); }
    catch (err) { toast(err.message); }
  };
  const copy = async () => {
    try {
      const r = await fetch("/api/ledger");
      await navigator.clipboard.writeText(await r.text());
      toast("The ledger is on the clipboard, one JSON object per line.");
    } catch (e) { toast("Could not copy: " + e.message); }
  };
  const actions = {
    reviews: h("button.btn.small.quiet", { onclick: sweep }, "Remove unused"),
    "ledger.jsonl": h("span.row", { class: "gap-u end" }, h("button.btn.small.quiet", { onclick: copy }, "Copy"), h("a.btn.small.quiet", { href: "/api/ledger", download: "docket-ledger.jsonl" }, "Download")),
  };
  return [
    h("h1.title", "Data"),
    h("p.lede", "Everything Docket keeps is in ", h("span.mono", d.home), ". Set ", h("span.mono", "DOCKET_HOME"), " to move it."),
    h("div.scroll-x", h("table.reg",
      h("thead", h("tr", h("th", "What"), h("th", "Path"), h("th.r", "Size"), h("th.r", ""))),
      h("tbody", d.parts.map((p) => h("tr",
        h("td", p.what), h("td.mono.small", p.path), h("td.r.num", { class: "nowrap" }, bytes(p.bytes)), h("td.r", actions[p.path] || null))),
        h("tr", h("td", h("b", "In all")), h("td"), h("td.r.num", h("b", bytes(d.total))), h("td"))))),
    h("div.block",
      h("div.row", { class: "spread base" }, h("h2.h2", "Log"),
        h("span.row", { class: "mid gap-u" }, h("span.small.muted", `last ${log.length} lines of docket.log`), h("button.btn.small.quiet", { onclick: () => refresh(true) }, "Refresh"))),
      log.length ? h("pre.code.out.log", log.join("\n")) : h("p.small.muted", "The log is empty.")),
    h("div.block.rule-top",
      h("h2.h2", "Reset"),
      h("div.row", { class: "mid" }, clear, h("span.small.muted", "Removes every metric. Your reviews, rules and settings are kept."))),
  ];
}
