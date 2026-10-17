"use strict";

const $ = (id) => document.getElementById(id);
const SVG = "http://www.w3.org/2000/svg";

// el("div", {class: "x"}, "text", child, ...) builds DOM without innerHTML.
function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === false || v == null) continue;
    if (k === "class") node.className = v;
    else if (k === "style") node.style.cssText = v;
    else node.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    node.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return node;
}

// MingCute line icons from /icons.svg.
function icon(name, cls) {
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("class", `icon${cls ? " " + cls : ""}`);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(SVG, "use");
  use.setAttribute("href", `/icons.svg#${name}`);
  svg.append(use);
  return svg;
}

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

const lines = (text) => text.split("\n").map((s) => s.trim()).filter((s) => s && !s.startsWith("#"));
const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
const joinNames = (names) => names.length <= 1 ? names.join("") : `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
const shortId = (id) => `[ ${id.slice(0, 6)} … ${id.slice(-4)} ]`;

function copyButton(getText, label) {
  const btn = el("button", { class: "icon-btn", type: "button", title: label, "aria-label": label }, icon("copy-2"));
  btn.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(getText());
      btn.replaceChildren(icon("check"));
    } catch {
      btn.replaceChildren(icon("close"));
    }
    setTimeout(() => btn.replaceChildren(icon("copy-2")), 1400);
  });
  return btn;
}

// ---------- theme ----------

function syncThemeButton() {
  const dark = document.documentElement.getAttribute("data-theme") !== "light";
  const btn = $("theme");
  btn.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
  btn.title = btn.getAttribute("aria-label");
  btn.replaceChildren(icon(dark ? "sun" : "moon"));
}

$("theme").addEventListener("click", () => {
  const next = document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light";
  document.documentElement.setAttribute("data-theme", next);
  try { localStorage.setItem("sbx-theme", next); } catch {}
  syncThemeButton();
});
syncThemeButton();

// ---------- state ----------

let versions = []; // matrix nodes from /api/status: {name, label, agent, running}
let lastResult = null;

// "/Satoshi:29.4.2/Knots:20260508/" -> "[ Knots 29.4.2 · 20260508 ]"
function agentTag(label) {
  const v = versions.find((x) => x.label === label);
  if (!v?.agent) return "[ not running ]";
  const parts = v.agent.split("/").filter(Boolean).map((p) => p.split(":"));
  const sat = parts.find((p) => p[0] === "Satoshi");
  const knots = parts.find((p) => p[0] === "Knots");
  return knots ? `[ Knots ${sat?.[1] ?? ""} · ${knots[1]} ]` : `[ Satoshi ${sat?.[1] ?? v.agent} ]`;
}

// ---------- header health ----------

async function loadStatus() {
  const box = $("health");
  try {
    const { nodes } = await api("/api/status");
    versions = nodes.filter((n) => !n.builder);
    const down = versions.filter((n) => !n.running);
    const height = nodes.find((n) => n.builder)?.height;
    box.classList.toggle("down", down.length > 0);
    box.title = versions.map((n) => `${n.label}: ${n.running ? n.agent : "not running"}`).join("\n");
    box.replaceChildren(
      el("span", { class: "sdot", style: `color: var(${down.length ? "--reject" : "--accept"})`, "aria-hidden": "true" }),
      down.length ? `${joinNames(down.map((n) => n.label))} not running · run sbx up` : `${versions.length} versions running`,
      height ? el("span", { class: "sep extra", "aria-hidden": "true" }, "·") : null,
      height ? el("span", { class: "extra" }, `regtest ${height} blocks`) : null);
    $("check-label").textContent = `Check ${versions.length} versions`;
  } catch {
    box.classList.add("down");
    box.replaceChildren(icon("wifi-off"), "Can't reach sbx. Is sbx serve running?");
  }
}

// ---------- input ----------

const HEX = /^[0-9a-fA-F]+$/;

function describeInput() {
  const txs = lines($("txs").value);
  const out = $("detected");
  out.classList.remove("bad");
  if (!txs.length) { out.textContent = ""; return; }
  const bad = txs.findIndex((t) => !HEX.test(t) || t.length % 2);
  if (bad >= 0) {
    out.classList.add("bad");
    out.textContent = `line ${bad + 1} isn't hex`;
    return;
  }
  out.textContent = txs.length === 1 ? "1 tx" : `package · ${txs.length - 1} parent${txs.length > 2 ? "s" : ""} → child`;
}

function updateMempoolCount() {
  const n = lines($("mempool").value).length;
  const badge = $("mempool-count");
  badge.hidden = n === 0;
  badge.textContent = String(n);
}

const GROUPS = { basic: "Single transaction", package: "Package (parent + child)", mempool: "Against a mempool" };

async function runExample(id) {
  showError("");
  renderPending("Building the example with the sandbox wallet…");
  try {
    const req = await api(`/api/examples/${encodeURIComponent(id)}`, {});
    $("txs").value = req.txs.join("\n");
    $("mempool").value = (req.mempool || []).join("\n");
    $("mempool-box").open = (req.mempool || []).length > 0;
    describeInput();
    updateMempoolCount();
    await runCheck();
  } catch (err) {
    showError(err.message);
    renderEmpty();
  }
}

// ---------- examples dropdown (custom listbox) ----------
// Button + listbox with aria-activedescendant. Positioned against the viewport so the
// scrolling input column can't clip it; flips upward when there's no room below.

const dd = { open: false, options: [], active: -1, chosen: null };

function ddSetActive(i) {
  dd.options.forEach((o, k) => o.classList.toggle("active", k === i));
  dd.active = i;
  const list = $("example-list");
  if (i >= 0) {
    list.setAttribute("aria-activedescendant", dd.options[i].id);
    dd.options[i].scrollIntoView({ block: "nearest" });
  } else {
    list.removeAttribute("aria-activedescendant");
  }
}

function ddPlace() {
  const btn = $("example-btn").getBoundingClientRect();
  const pop = $("example-pop");
  const width = Math.min(360, window.innerWidth - 32);
  const below = window.innerHeight - btn.bottom - 12;
  const above = btn.top - 12;
  const maxH = Math.min(380, Math.max(below, above) - 8);
  pop.style.width = `${width}px`;
  pop.style.left = `${Math.max(16, Math.min(btn.left, window.innerWidth - width - 16))}px`;
  $("example-list").style.maxHeight = `${maxH}px`;
  const flip = below < 240 && above > below;
  pop.classList.toggle("up", flip);
  pop.style.top = flip ? "" : `${btn.bottom + 6}px`;
  pop.style.bottom = flip ? `${window.innerHeight - btn.top + 6}px` : "";
}

function ddOpen() {
  if (dd.open || !dd.options.length) return;
  dd.open = true;
  ddPlace();
  $("examples").classList.add("open");
  $("example-btn").setAttribute("aria-expanded", "true");
  const start = dd.options.findIndex((o) => o.dataset.id === dd.chosen);
  ddSetActive(start >= 0 ? start : 0);
  $("example-list").focus({ preventScroll: true });
}

function ddClose(returnFocus) {
  if (!dd.open) return;
  dd.open = false;
  $("examples").classList.remove("open");
  $("example-btn").setAttribute("aria-expanded", "false");
  if (returnFocus) $("example-btn").focus();
}

function ddChoose(i) {
  const opt = dd.options[i];
  if (!opt) return;
  dd.chosen = opt.dataset.id;
  dd.options.forEach((o) => o.setAttribute("aria-selected", String(o === opt)));
  ddClose(true);
  runExample(opt.dataset.id);
}

async function loadExamples() {
  const examples = await api("/api/examples");
  const list = $("example-list");
  for (const [group, label] of Object.entries(GROUPS)) {
    const items = examples.filter((e) => e.group === group);
    if (!items.length) continue;
    const gid = `dd-g-${group}`;
    list.append(el("div", { role: "group", "aria-labelledby": gid, class: "dd-group" },
      el("div", { id: gid, class: "dd-group-label", role: "presentation" }, label),
      items.map((ex) => el("div", { id: `dd-o-${ex.id}`, role: "option", class: "dd-opt", "aria-selected": "false", "data-id": ex.id },
        el("span", { class: "dd-opt-text" }, ex.title), icon("check", "dd-check")))));
  }
  dd.options = [...list.querySelectorAll(".dd-opt")];
  dd.options.forEach((o, i) => {
    o.addEventListener("pointermove", () => { if (dd.active !== i) ddSetActive(i); });
    o.addEventListener("click", () => ddChoose(i));
  });

  const btn = $("example-btn");
  btn.addEventListener("click", () => (dd.open ? ddClose(true) : ddOpen()));
  btn.addEventListener("keydown", (e) => {
    if (["ArrowDown", "ArrowUp", "Enter", " "].includes(e.key)) { e.preventDefault(); ddOpen(); }
  });
  list.addEventListener("keydown", (e) => {
    const last = dd.options.length - 1;
    const moves = { ArrowDown: Math.min(dd.active + 1, last), ArrowUp: Math.max(dd.active - 1, 0), Home: 0, End: last };
    if (e.key in moves) { e.preventDefault(); ddSetActive(moves[e.key]); }
    else if (e.key === "Enter" || e.key === " ") { e.preventDefault(); ddChoose(dd.active); }
    else if (e.key === "Escape") { e.preventDefault(); ddClose(true); }
    else if (e.key === "Tab") ddClose(false);
  });
  document.addEventListener("pointerdown", (e) => {
    if (dd.open && !$("examples").contains(e.target)) ddClose(false);
  });
  window.addEventListener("resize", () => ddClose(false));
  document.addEventListener("scroll", (e) => {
    if (dd.open && e.target !== list) ddClose(false);
  }, true);
}

function showError(msg) {
  const box = $("error");
  box.hidden = !msg;
  if (msg) box.replaceChildren(icon("warning"), el("div", {}, msg));
}

// ---------- check ----------

async function runCheck() {
  const txs = lines($("txs").value);
  if (!txs.length) {
    showError("Paste a raw transaction first, or load one from Examples.");
    $("txs").focus();
    return;
  }
  showError("");
  $("check").disabled = true;
  renderPending("Starting a throwaway node per version…");
  try {
    lastResult = await api("/api/check", { txs, mempool: lines($("mempool").value) });
    render(lastResult);
  } catch (err) {
    showError(err.message);
    if (lastResult) render(lastResult); else renderEmpty();
  } finally {
    $("check").disabled = false;
    loadStatus();
  }
}

// ---------- rendering ----------

const STATES = {
  accept: [null, "Accepts"],
  reject: [null, "Rejects"],
  error: [null, "No result"],
  pending: ["loading", "Checking"],
  idle: [null, "Waiting"],
};

function versionRow(label, state, i, opts = {}) {
  const [ic, word] = STATES[state];
  return el("li", { class: `vrow ${state}${opts.odd ? " odd" : ""}`, style: `--i: ${i}` },
    icon("server"),
    el("div", { class: "vtext" },
      el("div", { class: "vline" },
        el("span", { class: "name" }, label),
        el("span", { class: "midsep", "aria-hidden": "true" }, "·"),
        el("span", { class: "vstate" },
          ic ? icon(ic, "spin") : el("span", { class: "sdot", "aria-hidden": "true" }), word),
        opts.odd ? el("span", { class: "sr-only" }, "Differs from the majority") : null),
      el("div", { class: "vsub" }, agentTag(label))));
}

function versionList(cells) {
  return el("ol", { class: "vlist", "aria-label": "Verdict per version" },
    cells.map((c, i) => versionRow(c.label, c.state, i, c)));
}

// The heading row stays put; on desktop only .result-body scrolls.
function setResult(headRight, ...body) {
  $("result").replaceChildren(
    el("div", { class: "col-head" }, el("h2", { class: "caps" }, "Result"), headRight),
    el("div", { class: "rule" }),
    el("div", { class: "result-body" }, ...body));
}

function renderPending(message) {
  $("result").setAttribute("aria-busy", "true");
  setResult(null,
    el("div", { class: "result-head" },
      el("div", { class: "headline" }, icon("loading", "spin tone-muted"),
        el("div", {}, el("h2", {}, "Checking…"), el("p", {}, message)))),
    versionList(versions.map((v) => ({ label: v.label, state: "pending" }))));
}

function renderEmpty() {
  $("result").setAttribute("aria-busy", "false");
  setResult(null,
    el("div", { class: "headline" },
      el("div", {}, el("h2", {}, "See which versions accept your transaction"),
        el("p", {}, "Each version runs on a private regtest network. A check starts a throwaway node per version, so nothing you test is broadcast or kept."))),
    versionList(versions.map((v) => ({ label: v.label, state: "idle" }))),
    el("section", { class: "section" },
      el("h3", { class: "caps" }, "Where to get transaction hex"),
      el("ul", { class: "ways" },
        el("li", {}, icon("file-code"), el("div", {},
          el("b", {}, "Try an example"),
          el("span", {}, "Examples builds a fresh transaction with the sandbox wallet and checks it straight away."))),
        el("li", {}, icon("terminal-box"), el("div", {},
          el("b", {}, "Make one with the sandbox wallet"),
          el("span", {}, "Returns signed hex without broadcasting it:"),
          el("code", {}, `scripts/sandbox-cli -named send outputs='{"<address>": 0.001}' fee_rate=1 options='{"add_to_wallet": false}'`))),
        el("li", {}, icon("server"), el("div", {},
          el("b", {}, "Use your own wallet code"),
          el("span", {}, "Point it at regtest RPC 127.0.0.1:19450 (user sbx, wallet sbx), build without broadcasting, and paste the hex. Mainnet transactions can't be checked here: their coins don't exist on regtest."))))));
}

const HEADLINES = {
  "all-accept": ["check-circle", "tone-accept", "Every version accepts it"],
  "disagree": ["git-compare", "", "Versions disagree"],
  "reject-same": ["close-circle", "tone-reject", "Every version rejects it"],
  "reject-different": ["close-circle", "tone-reject", "Every version rejects it, for different reasons"],
  "error": ["warning", "tone-warn", "The check couldn't run"],
};

// Group rejecting versions by the same root cause, so identical reasons show once.
function groupRejections(verdicts) {
  const groups = new Map();
  for (const v of verdicts) {
    if (v.accepted || v.error) continue;
    const key = (v.causes || []).map((c) => `${c.tx}|${c.explain}`).join("||") + "##" + (v.knock_on || []).join(",");
    if (!groups.has(key)) groups.set(key, { causes: v.causes || [], knockOn: v.knock_on || [], members: [] });
    groups.get(key).members.push(v);
  }
  return [...groups.values()];
}

function rawBlock(cause, members) {
  // Raw reasons can differ in detail (e.g. "0 < 15" vs "0 < 141"); show each distinct one.
  const byReason = new Map();
  for (const m of members) {
    const c = (m.causes || []).find((x) => x.tx === cause.tx && x.explain === cause.explain);
    if (!c) continue;
    if (!byReason.has(c.reason)) byReason.set(c.reason, []);
    byReason.get(c.reason).push(m.label);
  }
  const several = byReason.size > 1;
  return [...byReason.entries()].map(([reason, who]) =>
    el("div", { class: "raw" },
      el("code", {}, several ? el("span", { class: "who" }, joinNames(who)) : null, `${cause.tx}: ${reason}`),
      copyButton(() => reason, "Copy reason")));
}

function feeRate(rate) {
  const s = rate < 1 ? rate.toFixed(2) : rate.toFixed(1);
  const [whole, dec] = s.split(".");
  return el("div", { class: "tx-rate" },
    el("span", { class: "big" }, whole, el("span", { class: "dec" }, `.${dec}`)),
    el("span", { class: "unit" }, "sat/vB"));
}

function render(r) {
  $("result").setAttribute("aria-busy", "false");
  const counted = r.verdicts.filter((v) => !v.error);
  const accepted = counted.filter((v) => v.accepted);
  const rejected = counted.filter((v) => !v.accepted);
  const minorityAccepts = accepted.length <= rejected.length;
  const isOdd = (v) => r.agreement === "disagree" && !v.error && v.accepted === minorityAccepts;

  const [hIcon, tone, title] = HEADLINES[r.agreement] || ["information", "", r.agreement];
  let summary = `${accepted.length} of ${counted.length} accept`;
  if (r.agreement === "disagree") {
    const odd = counted.filter(isOdd);
    summary += `. ${joinNames(odd.map((v) => v.label))} (highlighted) ${odd.length === 1 ? (odd[0].accepted ? "accepts" : "rejects") : (odd[0].accepted ? "accept" : "reject")}, the rest ${odd[0].accepted ? "reject" : "accept"}`;
  }
  if (r.mempool_txs) summary += `. Checked against ${plural(r.mempool_txs, "mempool transaction")}`;
  summary += ".";

  const copyJson = el("button", { class: "btn brackets ghost", type: "button" }, icon("copy-2"), "Copy JSON");
  copyJson.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(lastResult, null, 2));
      copyJson.replaceChildren(icon("check"), "Copied");
    } catch {
      copyJson.replaceChildren(icon("close"), "Couldn't copy");
    }
    setTimeout(() => copyJson.replaceChildren(icon("copy-2"), "Copy JSON"), 1500);
  });

  const parts = [
    el("div", { class: "headline" }, icon(hIcon, tone),
      el("div", {}, el("h2", {}, title), el("p", {}, summary))),
    versionList(r.verdicts.map((v) => ({
      label: v.label, state: v.error ? "error" : v.accepted ? "accept" : "reject", odd: isOdd(v),
    }))),
  ];

  if (r.notes?.length) {
    parts.push(el("section", { class: "section" },
      el("h3", { class: "caps" }, icon("information"), "Why they split"),
      r.notes.map((n) => el("p", { class: "note" }, n))));
  }

  const setup = r.verdicts.filter((v) => v.setup_issues?.length);
  for (const v of setup) {
    for (const s of v.setup_issues) {
      parts.push(el("div", { class: "warnline" }, icon("warning"),
        el("span", {}, `${v.label} refused ${s.tx} (${s.reason.split(",")[0]}), so it isn't seeing the same mempool. Treat its verdict with care.`)));
    }
  }

  const groups = groupRejections(r.verdicts);
  if (groups.length || accepted.length || r.verdicts.some((v) => v.error)) {
    const list = el("div", { class: "groups" });
    for (const g of groups) {
      const body = el("div", { class: "group-body" });
      for (const c of g.causes) body.append(...rawBlock(c, g.members));
      for (const k of g.knockOn) {
        body.append(el("div", { class: "knock" }, icon("corner-down-right"),
          `${k[0].toUpperCase() + k.slice(1)} rejected only because its parent was.`));
      }
      list.append(el("div", { class: "group" },
        el("div", { class: "group-head" }, icon("close-circle", "tone-reject"),
          el("div", {},
            el("div", { class: "group-title" }, g.causes.map((c) => c.explain).join(" ")),
            el("div", { class: "group-meta" }, g.members.map((m) => m.label).join(" · ")))),
        body));
    }
    for (const v of r.verdicts.filter((x) => x.error)) {
      list.append(el("div", { class: "group" },
        el("div", { class: "group-head" }, icon("warning", "tone-warn"),
          el("div", {}, el("div", { class: "group-title" }, `${v.label}: no result`),
            el("div", { class: "group-meta" }, v.error)))));
    }
    if (accepted.length) {
      const evicts = accepted.find((v) => v.evicts)?.evicts;
      list.append(el("div", { class: "accepted" }, icon("check-circle"),
        el("span", {}, `Accepted by ${joinNames(accepted.map((v) => v.label))}`),
        evicts ? el("span", { class: "meta" }, `· replaces ${plural(evicts, "mempool transaction")}`) : null));
    }
    parts.push(el("section", { class: "section" }, el("h3", { class: "caps" }, "Details"), list));
  }

  parts.push(el("section", { class: "section" },
    el("h3", { class: "caps" }, r.txs.length === 1 ? "Transaction" : "Package"),
    el("div", { class: "txs" }, r.txs.map((t) => txBlock(t, flagsFor(r, t))))));

  setResult(copyJson, ...parts);
}

// ---------- transaction breakdown ----------
// Shows only what relay policy cares about, and flags each property a version objected to.

// Which transaction property a reject reason is about.
function flagCategory(reason) {
  if (/min relay fee not met|mempool min fee not met/.test(reason)) return "fee";
  if (/^insufficient fee/.test(reason)) return "rbf";
  if (/scriptpubkey|datacarrier|multi-op-return/.test(reason)) return "opreturn";
  if (/dust/.test(reason)) return "dust";
  if (/bare-multisig/.test(reason)) return "baremultisig";
  if (/TRUC|^version/.test(reason)) return "version";
  if (/non-final|non-BIP68-final/.test(reason)) return "locktime";
  if (/too-long-mempool-chain|script-verify|missing-inputs|missingorspent/.test(reason)) return "inputs";
  return null;
}

// Map of category -> version labels that rejected this tx for that reason.
function flagsFor(r, t) {
  const flags = new Map();
  for (const v of r.verdicts) {
    if (v.accepted || v.error) continue;
    for (const c of v.causes || []) {
      const applies = c.tx === t.label || (c.tx === "package" && (!/child/.test(c.reason) || t.label === "child"));
      const cat = applies && flagCategory(c.reason);
      if (!cat) continue;
      if (!flags.has(cat)) flags.set(cat, []);
      if (!flags.get(cat).includes(v.label)) flags.get(cat).push(v.label);
    }
  }
  return flags;
}

function flagNote(who) {
  return who?.length ? el("span", { class: "flag" }, icon("close-circle"), `Rejected by ${who.join(" · ")}`) : null;
}

const fmtAmount = (sats) => sats >= 1_000_000
  ? `${(sats / 1e8).toFixed(8).replace(/0+$/, "").replace(/\.$/, "")} BTC`
  : `${sats.toLocaleString("en-US")} sat`;

function txBlock(t, flags) {
  const b = t.breakdown;
  const feeFlag = flags.get("fee");
  const block = el("div", { class: "tx" },
    el("div", { class: "tx-head" },
      el("span", { class: "tx-label" }, t.label),
      el("span", { class: "txid", title: t.txid }, shortId(t.txid), copyButton(() => t.txid, "Copy txid"))),
    el("div", { class: `tx-rate-wrap${feeFlag ? " flagged" : ""}` },
      t.feerate != null ? feeRate(t.feerate) : el("div", { class: "tx-rate" }, el("span", { class: "unit" }, "Fee rate unknown")),
      flagNote(feeFlag)),
    el("div", { class: "chips" },
      el("span", { class: "chip" }, `${t.vsize}`, el("span", { class: "k" }, "vB")),
      el("span", { class: "chip" }, t.fee != null ? t.fee.toLocaleString("en-US") : "?", el("span", { class: "k" }, "sat fee")),
      t.version === 3
        ? el("span", { class: `chip chip-accent${flags.get("version") ? " chip-flagged" : ""}` }, "v3 · TRUC")
        : el("span", { class: `chip${flags.get("version") ? " chip-flagged" : ""}` }, el("span", { class: "k" }, "version"), `${t.version}`)),
    flagNote(flags.get("version")));
  if (!b) return block;

  const prop = (label, value, who) => el("div", { class: `prop${who ? " flagged" : ""}` },
    el("dt", {}, label), el("dd", {}, value, flagNote(who)));

  const inputs = b.inputs.map((i) => el("li", { class: "io-row" },
    el("span", { class: "io-n" }, `#${i.n}`),
    el("div", { class: "io-main" },
      el("div", { class: "io-top" }, el("span", { class: "io-type" }, i.type),
        el("span", { class: "io-amt" }, i.sats != null ? fmtAmount(i.sats) : "")),
      el("div", { class: "io-spend" }, i.spend),
      i.relative ? el("div", { class: "io-note" }, i.relative) : null,
      el("div", { class: "io-ref" }, `[ ${i.prevout.slice(0, 8)} … ${i.prevout.split(":")[0].slice(-4)}:${i.prevout.split(":")[1]} ]`))));

  const outputs = b.outputs.map((o) => {
    let who = null;
    if (o.kind === "nulldata") who = flags.get("opreturn");
    else if (o.dust || (o.kind === "anchor" && o.sats === 0)) who = flags.get("dust");
    else if (o.kind === "multisig") who = flags.get("baremultisig");
    else if (o.kind === "nonstandard") who = flags.get("opreturn");
    return el("li", { class: `io-row${who ? " flagged" : ""}` },
      el("span", { class: "io-n" }, `#${o.n}`),
      el("div", { class: "io-main" },
        el("div", { class: "io-top" }, el("span", { class: "io-type" }, o.type), el("span", { class: "io-amt" }, fmtAmount(o.sats))),
        o.note ? el("div", { class: "io-note" }, o.note) : null,
        o.address ? el("div", { class: "io-ref" }, `[ ${o.address.slice(0, 10)} … ${o.address.slice(-6)} ]`) : null,
        flagNote(who)));
  });

  block.append(el("div", { class: "bd" },
    el("dl", { class: "props" },
      prop("Replaceable (RBF)", b.rbf ? "Yes, signals replaceability" : "No signal (full RBF may still replace it)", flags.get("rbf")),
      prop("Locktime", b.locktime_text, flags.get("locktime")),
      prop("Weight", `${b.weight.toLocaleString("en-US")} WU`)),
    el("div", { class: "io-head" }, el("span", { class: "caps" }, `Inputs · ${b.inputs.length}`), flagNote(flags.get("inputs"))),
    el("ul", { class: "io" }, inputs),
    el("div", { class: "io-head" }, el("span", { class: "caps" }, `Outputs · ${b.outputs.length}`)),
    el("ul", { class: "io" }, outputs)));
  return block;
}

// ---------- wiring ----------

$("check").addEventListener("click", runCheck);
document.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !$("check").disabled) {
    e.preventDefault();
    runCheck();
  }
});
$("txs").addEventListener("input", describeInput);
$("mempool").addEventListener("input", updateMempoolCount);
$("clear").addEventListener("click", () => {
  $("txs").value = "";
  $("mempool").value = "";
  describeInput();
  updateMempoolCount();
  showError("");
  lastResult = null;
  renderEmpty();
  $("txs").focus();
});
$("paste").addEventListener("click", async () => {
  try {
    $("txs").value = (await navigator.clipboard.readText()).trim();
    describeInput();
  } catch {
    showError("The browser didn't allow reading the clipboard. Click the box and paste with ⌘V instead.");
  }
});

(async () => {
  await loadStatus();
  renderEmpty();
  loadExamples().catch((err) => showError(err.message));
})();
