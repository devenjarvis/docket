// Before/after: the same scenario on the base and on the head.
//
// The drafting convention carries the meaning: the base is drawn in pencil
// (the superseded state), the head in ink. Both share one scale, so a
// difference in extent is seen before it is read.

import { h, svg, prose } from "./dom.js";

export function beforeAfter(ba, outURL) {
  if (!ba) return null;
  let body;
  if (ba.kind === "timeline") body = timeline(ba);
  else if (ba.kind === "table") body = table(ba);
  else if (ba.kind === "image") body = images(ba, outURL);
  else body = texts(ba);
  return h("figure.ba", { style: { margin: 0 } },
    h("header",
      h("span.label.ink", "Before and after"),
      h("span.stamp" + (ba.measured ? "" : ".open"), ba.measured ? "Measured" : "Reasoned, not run")),
    ba.scenario && h("div.scenario", prose(ba.scenario)),
    body);
}

function niceStep(max) {
  const raw = max / 7;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  for (const m of [1, 2, 5, 10]) if (m * pow >= raw) return m * pow;
  return 10 * pow;
}

function timeline(ba) {
  const W = 780, left = 168, right = 22, plot = W - left - right;
  const laneH = 38, headH = 50, gap = 18, axisH = 30;
  const x = (t) => left + (t / ba.max) * plot;
  const sides = [["BASE", ba.before, true], ["HEAD", ba.after, false]];
  let y = 8;
  const parts = [];
  const bands = [];

  for (const [name, tl, isBase] of sides) {
    const top = y;
    const stroke = isBase ? "var(--pencil)" : "var(--ink)";
    parts.push(svg("text", { x: 14, y: y + 18, class: "side", style: `fill:${stroke}` }, name));
    parts.push(svg("text", { x: left, y: y + 18, class: "sum" }, tl.summary || ""));
    y += headH - 14;
    let end = 0;
    for (const lane of tl.lanes || []) {
      const mid = y + laneH / 2;
      parts.push(svg("text", { x: left - 12, y: mid + 4, "text-anchor": "end", class: "lbl" }, lane.label));
      parts.push(svg("line", { x1: left, x2: left + plot, y1: mid, y2: mid, stroke: "var(--hair)", "stroke-width": 1 }));
      for (const s of lane.spans || []) {
        end = Math.max(end, s.to);
        const x1 = x(s.from), x2 = x(s.to);
        if (s.kind === "wait") {
          // A wait is a gap, drawn the way a drawing shows a hidden edge.
          parts.push(svg("line", { x1, x2, y1: mid, y2: mid, stroke, "stroke-width": 2, "stroke-dasharray": "2 4" }));
        } else {
          parts.push(svg("rect", {
            x: x1, y: mid - 9, width: Math.max(x2 - x1, 2), height: 18,
            fill: isBase ? "none" : "var(--ink)", stroke, "stroke-width": 1.5,
          }));
          if (s.label && x2 - x1 > 38) {
            parts.push(svg("text", { x: (x1 + x2) / 2, y: mid + 4, "text-anchor": "middle", style: `font-size:11px;fill:${isBase ? "var(--pencil)" : "var(--on-ink)"}` }, s.label));
          }
        }
      }
      for (const m of lane.marks || []) {
        end = Math.max(end, m.at);
        const cx = x(m.at);
        // An instant: a diamond, filled on the head, open on the base.
        parts.push(svg("path", {
          d: `M${cx} ${mid - 8} L${cx + 7} ${mid} L${cx} ${mid + 8} L${cx - 7} ${mid} Z`,
          fill: isBase ? "var(--sheet)" : "var(--ink)", stroke, "stroke-width": 1.5,
        }));
      }
      y += laneH;
    }
    // The dimension line: total extent, the way a drawing gives a length.
    const dy = y + 10;
    if (end > 0) {
      const xe = x(end);
      parts.push(svg("line", { x1: left, x2: xe, y1: dy, y2: dy, stroke, "stroke-width": 1 }));
      for (const xx of [left, xe]) parts.push(svg("line", { x1: xx, x2: xx, y1: dy - 6, y2: dy + 6, stroke, "stroke-width": 1.5 }));
      parts.push(svg("rect", { x: (left + xe) / 2 - 30, y: dy - 9, width: 60, height: 18, fill: "var(--sheet)" }));
      parts.push(svg("text", { x: (left + xe) / 2, y: dy + 4, "text-anchor": "middle", style: `font-weight:700;font-size:12px;fill:${stroke}` }, `${trim(end)} ${ba.unit || ""}`));
    }
    y += 26;
    bands.push([top, y]);
    y += gap;
  }

  // The shared scale.
  const axisY = y - gap + 6;
  const grid = [];
  const step = niceStep(ba.max);
  for (let t = 0; t <= ba.max + 1e-9; t += step) {
    const gx = x(t);
    grid.push(svg("line", { x1: gx, x2: gx, y1: bands[0][0] + headH - 18, y2: axisY, stroke: "var(--hair)", "stroke-width": 1, "stroke-dasharray": t === 0 ? null : "1 5" }));
    grid.push(svg("text", { x: gx, y: axisY + 16, "text-anchor": "middle" }, trim(t) + (t === 0 ? " " + (ba.unit || "") : "")));
  }
  const sep = bands[0][1] + gap / 2;
  grid.push(svg("line", { x1: 0, x2: W, y1: sep, y2: sep, stroke: "var(--hair)", "stroke-width": 1 }));
  const H = axisY + axisH;

  const legend = h("div.row.small", { style: { padding: "8px 12px", borderTop: "1px solid var(--hair)", gap: "8px 20px", color: "var(--text-2)" } },
    key(svg("rect", { x: 1, y: 3, width: 24, height: 12, fill: "var(--ink)" }), "in flight"),
    key(svg("line", { x1: 0, x2: 26, y1: 9, y2: 9, stroke: "var(--ink)", "stroke-width": 2, "stroke-dasharray": "2 4" }), "waiting"),
    key(svg("path", { d: "M13 2 L20 9 L13 16 L6 9 Z", fill: "var(--ink)" }), "an instant"),
    h("span", "Base is drawn in pencil, head in ink. One scale for both."));

  const pic = svg("svg", { viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": `${ba.title || "Timeline"}. Base: ${ba.before.summary} Head: ${ba.after.summary}` }, grid, parts);
  return h("div", h("div.scroll-x", h("div", { style: { minWidth: "600px" } }, pic)), legend);
}

function key(shape, label) {
  return h("span", { style: { display: "inline-flex", alignItems: "center", gap: "6px", whiteSpace: "nowrap" } },
    svg("svg", { viewBox: "0 0 26 18", width: 26, height: 18, "aria-hidden": "true" }, shape), label);
}

const trim = (n) => String(Math.round(n * 10) / 10);

function table(ba) {
  return h("div.scroll-x", h("table",
    h("thead", h("tr", h("th", ""), h("th", ""), h("th", "Base"), h("th", "Head"))),
    h("tbody", (ba.rows || []).map((r) =>
      h("tr" + (r.changed ? ".changed" : ""),
        h("td.delta", { title: r.changed ? "changed" : "" }, r.changed ? "Δ" : ""),
        h("td", prose(r.label)),
        h("td.was", r.before),
        h("td.now", r.after))))));
}

function texts(ba) {
  const a = (ba.beforeText || "").split("\n"), b = (ba.afterText || "").split("\n");
  const inA = new Set(a), inB = new Set(b);
  const col = (cls, label, lines, other) =>
    h("div." + cls, h("div.label", label),
      h("pre", lines.map((l) => [h("span." + (other.has(l) ? "same" : "diffline"), l), "\n"])));
  return h("div.pair", col("was", "Base", a, inB), col("now", "Head", b, inA));
}

function images(ba, outURL) {
  const img = (label, file) => h("div", h("div.label", label),
    h("div", { style: { padding: "8px 12px 12px" } }, h("img", { src: outURL(file), alt: label + " screenshot" })));
  return h("div.pair", img("Base", ba.beforeText), img("Head", ba.afterText));
}
