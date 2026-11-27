// A few lines of DOM building, so the UI needs no framework and no build.

// h("div.row", {onclick}, child, "text", [more]) → element.
export function h(spec, ...rest) {
  const [tag, ...classes] = spec.split(".");
  const el = document.createElement(tag || "div");
  if (classes.length) el.className = classes.join(" ");
  for (const a of rest) append(el, a);
  return el;
}

function append(el, a) {
  if (a == null || a === false || a === "") return;
  if (Array.isArray(a)) { for (const x of a) append(el, x); return; }
  if (a instanceof Node) { el.appendChild(a); return; }
  if (typeof a === "object") {
    for (const [k, v] of Object.entries(a)) {
      if (v == null || v === false) continue;
      if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (k === "class") el.className += " " + v;
      else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k === "value" || k === "checked" || k === "disabled" || k === "open") el[k] = v;
      else el.setAttribute(k, v === true ? "" : v);
    }
    return;
  }
  el.appendChild(document.createTextNode(String(a)));
}

// svg("rect", {x: 1}) → SVG element.
export function svg(tag, attrs = {}, ...kids) {
  const el = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const [k, v] of Object.entries(attrs)) if (v != null && v !== false) el.setAttribute(k, v);
  for (const k of kids.flat()) if (k != null) el.appendChild(k instanceof Node ? k : document.createTextNode(String(k)));
  return el;
}

export async function get(path) {
  const r = await fetch(path);
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

export async function post(path, body) {
  const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

let toastTimer;
export function toast(msg) {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), 4500);
}

export const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
export const clock = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;

export function ago(iso) {
  if (!iso || iso.startsWith("0001")) return "never";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 90) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400 * 2) return `${Math.round(s / 3600)} h ago`;
  if (s < 86400 * 60) return `${Math.round(s / 86400)} days ago`;
  return `${Math.round(s / (86400 * 30))} months ago`;
}

// The numbered revision triangle.
export function tri(n, done, big) {
  return h("span.tri" + (done ? ".done" : "") + (big ? ".big" : ""), { "aria-hidden": "true" }, h("span", n));
}

// Inline `code` in agent text, and nothing else: agent text is never HTML.
export function prose(text) {
  const out = [];
  for (const [i, part] of String(text || "").split("`").entries()) {
    if (part === "") continue;
    out.push(i % 2 ? h("code", part) : part);
  }
  return out;
}

// openExternal opens a link in the user's browser: through the desktop app
// when there is one, else in a new tab.
export function openExternal(e) {
  const url = e.currentTarget.href;
  post("/api/open", { url }).then((r) => { if (!r.native) window.open(url, "_blank", "noreferrer"); }).catch(() => {});
  e.preventDefault();
}
