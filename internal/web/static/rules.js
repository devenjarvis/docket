// The rulebook: what the reviewer expects, grouped by where it holds.

import { h, post, toast, plural } from "./dom.js";
import { refresh } from "./app.js";

const SEV = { blocking: "Blocks", question: "Asks", note: "Notes" };

export function renderRules(S) {
  const data = S.rules || { rules: [], history: [] };
  const live = data.rules.filter((r) => r.status !== "retired");
  const retired = data.rules.filter((r) => r.status === "retired");
  const everywhere = live.filter((r) => r.scope === "everywhere");
  const repos = [...new Set(live.filter((r) => r.scope !== "everywhere").map((r) => r.scope))].sort();
  const fading = live.filter((r) => r.fading);
  const trial = live.filter((r) => r.status === "trial");

  const save = async (rule, why) => {
    try { await post("/api/rule", { ...rule, why }); refresh(true); } catch (e) { toast(e.message); }
  };

  const form = (rule, done) => {
    const text = h("textarea.field", { rows: 2, value: rule.text || "", placeholder: "One sentence: what must be true of a change.", "aria-label": "Rule" });
    const applies = h("input.field", { value: rule.applies || "", placeholder: "the kind of code it is about, e.g. SQL migrations (blank: any)", "aria-label": "Applies to" });
    const scope = h("input.field.mono", { value: rule.scope === "everywhere" || !rule.scope ? "" : rule.scope, placeholder: "owner/repo (blank: everywhere)", "aria-label": "Scope" });
    const sev = h("select.field", { "aria-label": "How a finding is posted" }, Object.entries(SEV).map(([k, v]) => h("option", { value: k, selected: (rule.severity || "note") === k }, `${v} (${k})`)));
    return h("div.block", { class: "well" },
      text, h("div.row", h("div.grow", applies), h("div.grow", scope), h("div", { style: { flex: "0 1 200px" } }, sev)),
      h("div.row", h("button.btn.primary", { onclick: () => save({ ...rule, text: text.value, applies: applies.value, scope: scope.value.trim() || "everywhere", severity: sev.value }, rule.id ? "edited by you" : "written by you") }, rule.id ? "Save" : "Add rule"),
        done && h("button.btn.quiet", { onclick: done }, "Cancel")));
  };

  const row = (r) => {
    const judged = r.stats.agreed + r.stats.overrode;
    const pct = judged ? Math.round(r.stats.agreed / judged * 100) : 0;
    const editor = h("div");
    const edit = () => editor.replaceChildren(form(r, () => editor.replaceChildren()));
    return h("tr",
      h("td.mono", { class: "fit" }, r.id),
      h("td",
        h("div", { class: "item-title" }, r.text),
        h("div.small.muted", [r.applies && `applies to ${r.applies}`, r.origin, (r.except || []).length && `not in ${r.except.join(", ")}`].filter(Boolean).join(" · ")),
        r.fading && h("div.small", { class: "text-red mt-s" }, `Fading: you waved this off ${r.stats.overrode} of the last ${judged} times.`),
        editor),
      h("td", { class: "fit nowrap" },
        h("span.stamp" + (r.severity === "blocking" ? ".red" : r.severity === "question" ? ".open" : ".faint"), SEV[r.severity] || r.severity),
        r.status === "trial" && h("div", { class: "mt" }, h("span.stamp.open", "On trial"))),
      h("td.small", { style: { width: "150px" } }, judged ? [`fired ${r.stats.fired} · agreed ${r.stats.agreed}`, h("div.bar" + (r.fading ? ".red" : ""), h("i", { style: { width: pct + "%" } }))] : h("span.muted", "not fired yet")),
      h("td.r", { class: "fit nowrap" },
        h("div.row", { class: "gap-s end one-line" },
          r.status === "trial" && h("button.btn.small", { onclick: () => save({ ...r, status: "active" }, "confirmed after trial") }, "Keep"),
          r.scope !== "everywhere" && r.status !== "trial" && h("button.btn.small", { onclick: () => save({ ...r, scope: "everywhere" }, "widened to everywhere by you") }, "Widen"),
          h("button.btn.small", { onclick: edit }, "Edit"),
          h("button.btn.small.quiet", { onclick: () => save({ ...r, status: "retired" }, "retired by you") }, "Retire"))));
  };

  const group = (title, sub, list) => list.length ? h("div.block",
    h("div.row", { class: "spread base" }, h("h2.h2", `${title} · ${list.length}`), h("span.small.muted", sub)),
    h("div.scroll-x", h("table.reg", h("tbody", list.map(row))))) : null;

  const adder = h("div");
  return h("div.sheet", h("div.body",
    h("div.block",
      h("h1.title", "Your rulebook"),
      h("p.lede", "What you expect from a change, written down once. Some rules hold everywhere you review, some only in one repository. They live on this computer as plain files and belong to you, not to any team; no repository has to install anything."),
      h("div.row", h("button.btn.primary", { onclick: () => adder.replaceChildren(form({}, () => adder.replaceChildren())) }, "Write a rule")),
      adder),

    (fading.length || trial.length) ? h("div.yours",
      h("div.label", `Waiting for you · ${fading.length + trial.length}`),
      trial.map((r) => h("div.small", h("b", r.id), ` is on trial: "${r.text}" Keep it, edit it or retire it below.`)),
      fading.map((r) => h("div.small", h("b", r.id), ` is fading: you override it more than you keep it. Retire it, or limit where it holds.`))) : null,

    group("Everywhere you review", "applied wherever the code they describe shows up", everywhere),
    repos.map((repo) => group(`Only in ${repo}`, "a rule starts where you said it, and widens only when you say so", live.filter((r) => r.scope === repo))),
    !live.length && h("p.muted", "No rules yet. The quickest way to get one: when a decision stops you and no rule raised it, Docket offers to make it one."),

    h("div.cols",
      h("div.main", h("div.block.rule-top", h("h2.h2", "How it stays current"),
        h("div.row", { class: "gap-l" },
          h("div.grow.block", { style: { minWidth: "240px" } }, h("div.label.ink", "Docket does on its own"),
            h("ul.small", { class: "list" },
              ["Counts every firing and whether you agreed.", "Exempts a repository when you answer \"not in this repo\".", "Flags a rule as fading when you override it most of the time.", "Has the agent read each repository's own conventions fresh on every pull request, and reports them as the repository's, not yours."].map((t) => h("li", t)))),
          h("div.grow.block", { style: { minWidth: "240px" } }, h("div.label.ink", "Docket asks you first"),
            h("ul.small", { class: "list" },
              ["Adding a rule: it drafts one from a decision, you put it on trial.", "Keeping a rule after its trial.", "Widening a rule from one repository to everywhere.", "Retiring a rule, unless you said \"stop raising it\"."].map((t) => h("li", t))))))),
      h("div.side", h("div.pencil", h("div.label", "Recent changes"),
        h("div.small.mono", (data.history || []).length ? data.history.map((l) => h("div", l)) : "none yet"),
        h("div.small.muted", { class: "mt" }, "Every change is a commit in ", h("span.mono", data.dir || "~/.docket/rules"), ", so you can read the history or undo one.")))),

    retired.length ? h("details", h("summary", h("span.label.ink", `Retired · ${plural(retired.length, "rule")}`)),
      h("div.scroll-x", h("table.reg", h("tbody", retired.map((r) => h("tr", h("td.mono", r.id), h("td.muted", r.text),
        h("td.r", h("button.btn.small", { onclick: () => save({ ...r, status: "active" }, "brought back by you") }, "Bring back")))))))) : null));
}
