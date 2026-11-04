"use strict";
// ---------- Связь с ядром ----------
const TOKEN = (location.hash.slice(1) || sessionStorage.getItem("hco-token") || "");
sessionStorage.setItem("hco-token", TOKEN);
history.replaceState(null, "", location.pathname);

async function api(path, body) {
  const opt = { headers: { "X-Token": TOKEN } };
  if (body !== undefined) {
    opt.method = "POST";
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const r = await fetch("/api/" + path, opt);
  const data = await r.json().catch(() => null);
  if (!r.ok) throw new Error((data && data.error) || r.statusText);
  return data;
}

// ---------- Утилиты ----------
const $ = id => document.getElementById(id);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
function bytes(b) {
  const u = LANG === "en" ? ["B", "KB", "MB", "GB", "TB"] : ["Б", "КБ", "МБ", "ГБ", "ТБ"];
  let v = Number(b) || 0, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (i === 0 ? v.toFixed(0) : v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)) + " " + u[i];
}
const rate = v => bytes(v) + (LANG === "en" ? "/s" : "/с");
function uptime(s) {
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  const [D, H, M] = LANG === "en" ? ["d", "h", "min"] : ["д", "ч", "мин"];
  return d ? `${d} ${D} ${h} ${H} ${m} ${M}` : h ? `${h} ${H} ${m} ${M}` : `${m} ${M}`;
}
const clamp01 = v => Math.max(0, Math.min(1, v));
function shade(f) { return f >= .85 ? "var(--f3)" : f >= .6 ? "var(--f2)" : f >= .3 ? "var(--f1)" : "var(--f0)"; }
function meterHTML(label, frac, value, lw, vw) {
  const st = (lw ? `--lw:${lw}px;` : "") + (vw !== undefined ? `--vw:${vw}px;` : "");
  return `<div class="meter" style="${st}"><span class="l">${esc(label)}</span><div class="bar"><i style="width:${(clamp01(frac) * 100).toFixed(1)}%;background:${shade(frac)}"></i></div><span class="v">${esc(value)}</span></div>`;
}
function toast(msg, err) {
  const el = document.createElement("div");
  el.className = "toast" + (err ? " err" : "");
  el.textContent = msg;
  $("toasts").appendChild(el);
  setTimeout(() => el.remove(), err ? 6500 : 3500);
}
async function busy(btn, fn) {
  btn.disabled = true; btn.classList.add("busy");
  try { return await fn(); } catch (e) { toast(e.message, true); } finally { btn.disabled = false; btn.classList.remove("busy"); }
}
// Модальное окно: возвращает Promise<boolean>
function modal({ title, body, ok = t("ok"), cancel = t("cancel"), danger = false, node }) {
  return new Promise(res => {
    $("mTitle").textContent = title;
    const b = $("mBody");
    b.textContent = body || "";
    if (node) b.appendChild(node);
    $("mOk").textContent = ok;
    $("mOk").className = "btn " + (danger ? "btn-danger" : "btn-accent");
    $("mCancel").textContent = cancel;
    $("mCancel").classList.toggle("hidden", cancel === null);
    $("modal").classList.remove("hidden");
    const close = v => { $("modal").classList.add("hidden"); $("mOk").onclick = $("mCancel").onclick = null; res(v); };
    $("mOk").onclick = () => close(true);
    $("mCancel").onclick = () => close(false);
  });
}
document.addEventListener("keydown", e => { if (e.key === "Escape" && !$("modal").classList.contains("hidden")) $("mCancel").click(); });

// ---------- Графики ----------
class Spark {
  constructor(canvas, max) { this.c = canvas; this.max = max; this.data = []; this.cap = 90; }
  push(v) { this.data.push(v); if (this.data.length > this.cap) this.data.shift(); this.draw(); }
  draw() {
    const c = this.c, dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth, h = c.clientHeight;
    if (!w || !h) return;
    if (c.width !== Math.round(w * dpr)) { c.width = Math.round(w * dpr); c.height = Math.round(h * dpr); }
    const g = c.getContext("2d"); g.setTransform(dpr, 0, 0, dpr, 0, 0); g.clearRect(0, 0, w, h);
    const cs = getComputedStyle(document.documentElement);
    g.strokeStyle = cs.getPropertyValue("--grid"); g.lineWidth = 1; g.setLineDash([2, 3]);
    for (let i = 1; i < 4; i++) { const y = Math.round(h * i / 4) + .5; g.beginPath(); g.moveTo(0, y); g.lineTo(w, y); g.stroke(); }
    g.setLineDash([]);
    const d = this.data; if (d.length < 2) return;
    let mx = this.max || Math.max(...d) * 1.25 || 1;
    const step = w / (this.cap - 1), x0 = w - (d.length - 1) * step;
    const pts = d.map((v, i) => [x0 + i * step, h - 2 - Math.min(v / mx, 1) * (h - 5)]);
    const grad = g.createLinearGradient(0, 0, 0, h);
    grad.addColorStop(0, cs.getPropertyValue("--area-top")); grad.addColorStop(1, cs.getPropertyValue("--area-bot"));
    g.beginPath(); g.moveTo(pts[0][0], h); pts.forEach(p => g.lineTo(p[0], p[1])); g.lineTo(pts[pts.length - 1][0], h); g.closePath();
    g.fillStyle = grad; g.fill();
    g.beginPath(); pts.forEach((p, i) => i ? g.lineTo(p[0], p[1]) : g.moveTo(p[0], p[1]));
    g.strokeStyle = cs.getPropertyValue("--line"); g.lineWidth = 1.6; g.lineJoin = "round"; g.stroke();
  }
}

// ---------- Состояние ----------
let HOST = {}, CFG = {}, SNAP = null, RULES = [];
const sparks = {};
const state = { sortCol: "cpu", sortDesc: true, sel: null, logId: 0, tempScanned: false };

// ---------- Навигация ----------
function showPage(name) {
  document.querySelectorAll("#nav button").forEach(b => b.classList.toggle("active", b.dataset.page === name));
  document.querySelectorAll(".page").forEach(p => p.classList.toggle("active", p.id === "page-" + name));
  if (name === "tweaks") loadTweaks(false);
  if (name === "autostart") loadAutostart(false);
  if (name === "maintenance") loadMaintenance();
  if (name === "drivers") loadDrivers(false);
  if (name === "registry" && !REG_ISSUES) $("regList").innerHTML = `<div class="muted" style="padding:20px 4px">${t("reg_intro")}</div>`;
  if (name === "overview") Object.values(sparks).forEach(s => s.draw());
  if (typeof onPageShow === "function") onPageShow(name);
  state.page = name;
  $("main").scrollTop = 0;
}
document.querySelectorAll("#nav button").forEach(b => b.onclick = () => showPage(b.dataset.page));

// ---------- Шапка ----------
function renderHeader() {
  $("hostLine1").textContent = `${HOST.hostname} · ${HOST.os}`;
  $("hostLine2").textContent = `${HOST.cpu} · ${t("threads_ram", HOST.logical, bytes(HOST.memTotal))}`;
  const b = $("rightsBadge");
  b.textContent = HOST.elevated ? t("admin") : t("limited");
  b.classList.toggle("on", HOST.elevated);
  $("elevateBtn").classList.toggle("hidden", !HOST.canElevate);
}
$("elevateBtn").onclick = () => busy($("elevateBtn"), () => api("elevate", {}));

// ---------- Обзор ----------
function renderOverview(s) {
  $("sCpu").textContent = s.cpu.toFixed(0) + "%";
  $("sCpuSub").textContent = t("cpu_sub", HOST.logical, HOST.physical);
  $("sMem").textContent = s.memPct.toFixed(0) + "%";
  $("sMemSub").textContent = t("mem_sub", bytes(s.memUsed), bytes(s.memTotal), bytes(s.memAvail));
  $("sIo").textContent = rate(s.diskR + s.diskW);
  $("sIoSub").textContent = t("io_sub", rate(s.diskR), rate(s.diskW));
  $("sNet").textContent = rate(s.netRx + s.netTx);
  $("sNetSub").textContent = t("net_sub", rate(s.netRx), rate(s.netTx));
  sparks.cpu.push(s.cpu); sparks.mem.push(s.memPct); sparks.io.push(s.diskR + s.diskW); sparks.net.push(s.netRx + s.netTx);

  const cores = $("cores");
  cores.classList.toggle("c3", s.cores.length > 16);
  cores.innerHTML = s.cores.map((v, i) => meterHTML(`${t("core")} ${i}`, v / 100, v.toFixed(0) + "%")).join("");

  $("memMeters").innerHTML =
    meterHTML(t("ram"), s.memPct / 100, `${bytes(s.memUsed)} / ${bytes(s.memTotal)}`) +
    (s.swapTotal ? meterHTML(t("swap"), s.swapPct / 100, `${bytes(s.swapUsed)} / ${bytes(s.swapTotal)}`) : meterHTML(t("swap"), 0, t("swap_off")));
  const info = [[t("uptime"), uptime(s.uptime)], [t("procs_threads"), `${s.procs.length} / ${s.threads}`]];
  if (s.load && s.load.length) info.push([t("load"), s.load.map(v => v.toFixed(2)).join(" · ")]);
  $("sysInfo").innerHTML = info.map(([k, v]) => `<span>${esc(k)}</span><span>${esc(v)}</span>`).join("");

  $("drives").innerHTML = s.disks.length
    ? s.disks.map(d => meterHTML(d.mount, d.pct / 100, `${bytes(d.used)} / ${bytes(d.total)}`, 110)).join("")
    : `<div class="muted">${t("no_drives")}</div>`;

  const top = [...s.procs].sort((a, b) => b.cpu - a.cpu || b.rss - a.rss).slice(0, 7);
  $("topProcs").innerHTML = top.map(p => `<tr><td>${esc(p.name)} <span class="muted">(${p.pid})</span></td><td class="num">${p.cpu.toFixed(1)}%</td><td class="num muted">${bytes(p.rss)}</td><td><div class="bar"><i style="width:${Math.min(p.cpu, 100)}%;background:${shade(p.cpu / 100 + .3)}"></i></div></td></tr>`).join("");
}

// ---------- Процессы ----------
const COLS = [["pid", "h_pid", 1], ["name", "h_name"], ["user", "h_user"], ["cpu", "h_cpu", 1], ["rss", "h_mem", 1], ["thr", "h_thr", 1], ["prio", "h_prio"]];
function renderProcHead() {
  $("procHead").innerHTML = COLS.map(([k, label, num]) => {
    const arrow = state.sortCol === k ? (state.sortDesc ? " ↓" : " ↑") : "";
    return `<th data-k="${k}" class="${num ? "num " : ""}${state.sortCol === k ? "sorted" : ""}">${t(label)}${arrow}</th>`;
  }).join("");
  $("procHead").querySelectorAll("th").forEach(th => th.onclick = () => {
    const k = th.dataset.k;
    if (state.sortCol === k) state.sortDesc = !state.sortDesc; else { state.sortCol = k; state.sortDesc = ["cpu", "rss", "thr", "pid"].includes(k); }
    renderProcHead(); renderProcs();
  });
}
function prioName(p) { return p >= 0 ? t("prio")[p] : "—"; }
function renderProcs() {
  if (!SNAP) return;
  const q = $("procSearch").value.trim().toLowerCase();
  let rows = SNAP.procs.filter(p => !q || p.name.toLowerCase().includes(q) || String(p.pid).includes(q) || (p.user || "").toLowerCase().includes(q));
  const k = state.sortCol, dir = state.sortDesc ? -1 : 1;
  rows.sort((a, b) => {
    let x = a[k], y = b[k];
    if (typeof x === "string") { x = x.toLowerCase(); y = (y || "").toLowerCase(); }
    const c = x < y ? -1 : x > y ? 1 : 0;
    return c ? c * dir : b.rss - a.rss;
  });
  $("procCount").textContent = t("of", rows.length, SNAP.procs.length);
  const hot = 50 / HOST.logical;
  $("procBody").innerHTML = rows.map(p => `<tr data-pid="${p.pid}" class="${p.pid === state.sel ? "sel" : p.cpu >= hot ? "hot" : ""}"><td class="num">${p.pid}</td><td title="${esc(p.name)}">${esc(p.name)}</td><td>${esc(p.user || "—")}</td><td class="num">${p.cpu.toFixed(1)}%</td><td class="num">${bytes(p.rss)}</td><td class="num">${p.thr}</td><td>${prioName(p.prio)}</td></tr>`).join("");
  syncSelection();
}
$("procBody").onclick = e => {
  const tr = e.target.closest("tr"); if (!tr) return;
  state.sel = Number(tr.dataset.pid);
  $("procBody").querySelectorAll("tr.sel").forEach(r => r.classList.remove("sel"));
  tr.classList.add("sel");
  syncSelection();
};
$("procSearch").oninput = renderProcs;
function selected() { return SNAP && SNAP.procs.find(p => p.pid === state.sel); }
function syncSelection() {
  const p = selected();
  const on = !!p;
  ["prioSel", "affBtn", "suspBtn", "resBtn", "ruleBtn", "killBtn"].forEach(id => $(id).disabled = !on);
  if (!on) { state.sel = null; $("selInfo").textContent = t("select_proc"); return; }
  $("selInfo").innerHTML = `<b>${esc(p.name)}</b> · PID ${p.pid} · ${t("col_cpu")} ${p.cpu.toFixed(1)}% · ${bytes(p.rss)}`;
  if (document.activeElement !== $("prioSel")) $("prioSel").value = p.prio >= 0 ? p.prio : "";
}
function fillPrioSelect() {
  $("prioSel").innerHTML = `<option value="" disabled>${t("h_prio")}</option>` + t("prio").map((n, i) => `<option value="${i}">${n}</option>`).join("");
}
$("prioSel").onchange = async () => {
  const p = selected(); if (!p) return;
  try { await api("proc/priority", { pid: p.pid, prio: Number($("prioSel").value) }); toast(`${p.name}: ${t("prio")[$("prioSel").value]}`); }
  catch (e) { toast(e.message, true); }
  $("prioSel").blur();
};
async function procAction(path, label) {
  const p = selected(); if (!p) return;
  try { await api(path, { pid: p.pid }); toast(`${p.name}: ${label}`); } catch (e) { toast(e.message, true); }
}
$("suspBtn").onclick = () => procAction("proc/suspend", t("suspend"));
$("resBtn").onclick = () => procAction("proc/resume", t("resume"));
const SYSTEM = new Set(["system", "registry", "smss", "csrss", "wininit", "winlogon", "services", "lsass", "svchost", "dwm", "explorer", "fontdrvhost", "systemd", "init", "kthreadd", "xorg", "launchd", "kernel_task", "windowserver"]);
$("killBtn").onclick = async () => {
  const p = selected(); if (!p) return;
  let body = t("kill_q", p.name, p.pid);
  if (SYSTEM.has(p.name.toLowerCase().replace(/\.exe$/, ""))) body += t("kill_sys");
  if (await modal({ title: t("kill_title"), body, ok: t("kill"), danger: true })) procAction("proc/kill", t("kill"));
};
$("ruleBtn").onclick = () => {
  const p = selected(); if (!p) return;
  $("rPattern").value = p.name; $("rPrio").value = "1"; $("rCores").value = "";
  showPage("rules"); $("rPattern").focus();
};
$("affBtn").onclick = async () => {
  const p = selected(); if (!p) return;
  if (!HOST.affinity) return modal({ title: t("affinity"), body: t("aff_unsupported"), cancel: null });
  let cur;
  try { cur = new Set(await api("proc/affinity?pid=" + p.pid)); } catch (e) { return toast(e.message, true); }
  const wrap = document.createElement("div");
  const btns = document.createElement("div"); btns.className = "toolbar"; btns.style.marginTop = "10px";
  const grid = document.createElement("div"); grid.className = "core-grid";
  const n = HOST.logical;
  for (let i = 0; i < n; i++) grid.insertAdjacentHTML("beforeend", `<label class="check"><input type="checkbox" value="${i}" ${cur.has(i) ? "checked" : ""}><span>${t("core")} ${i}</span></label>`);
  const boxes = () => [...grid.querySelectorAll("input")];
  [["all", () => true], ["first_half", i => i < Math.ceil(n / 2)], ["none", () => false]].forEach(([k, f]) => {
    const b = document.createElement("button"); b.className = "btn"; b.textContent = t(k);
    b.onclick = () => boxes().forEach((c, i) => c.checked = f(i)); btns.appendChild(b);
  });
  wrap.append(btns, grid);
  if (!await modal({ title: t("aff_title", p.name), body: t("aff_hint"), ok: t("apply"), node: wrap })) return;
  const cores = boxes().filter(c => c.checked).map(c => Number(c.value));
  try { await api("proc/affinity", { pid: p.pid, cores }); toast(`${p.name}: ${t("rule_cores", cores.join(","))}`); } catch (e) { toast(e.message, true); }
};

// ---------- Правила ----------
function fmtCores(c) {
  if (!c || !c.length) return "";
  const s = [...c].sort((a, b) => a - b), out = [];
  for (let i = 0; i < s.length;) { let j = i; while (j + 1 < s.length && s[j + 1] === s[j] + 1) j++; out.push(j > i ? `${s[i]}-${s[j]}` : `${s[i]}`); i = j + 1; }
  return out.join(",");
}
function describeRule(r) {
  const parts = [];
  if (r.priority >= 0) parts.push(t("rule_prio", t("prio")[r.priority]));
  if (r.cores && r.cores.length) parts.push(t("rule_cores", fmtCores(r.cores)));
  return parts.join(" · ") || t("rule_none");
}
function renderRules() {
  const el = $("rulesList");
  if (!RULES.length) { el.innerHTML = `<div class="empty">${t("no_rules")}</div>`; return; }
  el.innerHTML = RULES.map((r, i) => `<div class="rule ${r.enabled ? "" : "off"}">
    <label class="switch" title="${r.enabled ? t("on") : t("off")}"><input type="checkbox" data-i="${i}" class="r-toggle" ${r.enabled ? "checked" : ""}><i></i></label>
    <div class="grow"><b>${esc(r.pattern)}</b><span>${esc(describeRule(r))}</span></div>
    <button class="icon-btn r-del" data-i="${i}" title="${t("del")}">✕</button></div>`).join("");
  el.querySelectorAll(".r-toggle").forEach(c => c.onchange = () => { RULES[c.dataset.i].enabled = c.checked; saveRules(); });
  el.querySelectorAll(".r-del").forEach(b => b.onclick = () => { RULES.splice(b.dataset.i, 1); saveRules(); });
}
async function saveRules() {
  try { RULES = await api("rules", RULES); } catch (e) { toast(e.message, true); }
  renderRules();
}
function fillRuleForm() {
  $("rPrio").innerHTML = `<option value="-1">${t("keep")}</option>` + t("prio").map((n, i) => `<option value="${i}">${n}</option>`).join("");
  $("rCores").placeholder = t("cores_ph", HOST.logical - 1);
  if (!HOST.affinity) { $("rCores").disabled = true; $("rCores").placeholder = t("aff_unsupported"); }
}
$("addRule").onclick = async () => {
  const pattern = $("rPattern").value.trim();
  if (!pattern) return toast(t("rule_need_pattern"), true);
  let cores = [];
  try { cores = await api("rules/parse-cores", { text: $("rCores").value }); } catch (e) { return toast(e.message, true); }
  const priority = Number($("rPrio").value);
  if (priority < 0 && !cores.length) return toast(t("rule_need_action"), true);
  RULES.push({ pattern, priority, cores, enabled: true });
  await saveRules();
  $("rPattern").value = ""; $("rCores").value = "";
  toast(t("rule_added"));
  api("rules/apply", {}).catch(() => {});
};
const applyRulesNow = btn => busy(btn, async () => toast(t("rules_applied", await api("rules/apply", {}))));
$("applyRules").onclick = () => applyRulesNow($("applyRules"));
$("applyRules2").onclick = () => applyRulesNow($("applyRules2"));
$("autoRules").onchange = () => setCfg({ auto_rules: $("autoRules").checked });

// ---------- Оптимизация ----------
function renderOptimize(s) {
  $("score").textContent = s.score;
  $("verdict").textContent = t(s.score >= 85 ? "v0" : s.score >= 65 ? "v1" : s.score >= 45 ? "v2" : "v3");
  const sb = $("scoreBar"); sb.style.width = s.score + "%"; sb.style.background = "var(--accent)";
  $("recs").innerHTML = s.recs.length
    ? s.recs.map(r => `<div class="rec ${r.level === 2 ? "crit" : ""}"><span class="m">${["•", "!", "!!"][r.level]}</span><div><div class="t">${esc(r.title)}</div><div class="d">${esc(r.detail)}</div></div></div>`).join("")
    : `<div class="muted">${t("no_issues")}</div>`;
}
$("trimBtn").onclick = () => busy($("trimBtn"), async () => toast(await api("opt/trim", {})));
$("hogsBtn").onclick = async () => {
  const hogs = await api("opt/hogs").catch(e => (toast(e.message, true), null));
  if (!hogs) return;
  if (!hogs.length) return modal({ title: t("hogs_title"), body: t("hogs_none", CFG.hog_threshold), cancel: null });
  const list = document.createElement("div"); list.className = "list";
  hogs.forEach(h => list.insertAdjacentHTML("beforeend", `<label class="check"><input type="checkbox" value="${h.pid}" checked><span>${esc(h.name)} (PID ${h.pid}) — ${t("col_cpu")} ${h.cpu.toFixed(1)}%, ${bytes(h.rss)}</span></label>`));
  if (!await modal({ title: t("hogs_title"), body: t("hogs_hint"), ok: t("hogs_ok"), node: list })) return;
  const pids = [...list.querySelectorAll("input:checked")].map(c => Number(c.value));
  try { toast(t("hogs_done", await api("opt/lower", { pids }))); } catch (e) { toast(e.message, true); }
};
$("scanTemp").onclick = () => busy($("scanTemp"), async () => {
  const r = await api("opt/tempscan", {});
  state.tempScan = r;
  $("tempInfo").textContent = r.files ? t("temp_found", r.files, bytes(r.bytes), r.days, r.roots.join(", ")) : t("temp_empty", r.days);
  setTimeout(() => $("cleanTemp").disabled = !r.files, 0);
});
$("cleanTemp").onclick = async () => {
  const r = state.tempScan; if (!r) return;
  if (!await modal({ title: t("cleanup"), body: t("temp_q", r.files, bytes(r.bytes)), ok: t("clean_temp"), danger: true })) return;
  await busy($("cleanTemp"), async () => {
    const x = await api("opt/tempclean", {});
    $("tempInfo").textContent = t("temp_done", x.removed, bytes(x.freed), x.failed);
    state.tempScan = null;
  });
  $("cleanTemp").disabled = true;
};
function renderPowerSeg(active) {
  $("powerSeg").innerHTML = t("power_names").map((n, i) => `<button data-v="${i}" class="${i === active ? "on" : ""}">${n}</button>`).join("");
  $("powerSeg").querySelectorAll("button").forEach(b => b.onclick = async () => {
    const v = Number(b.dataset.v);
    try { await api("power", { profile: v }); renderPowerSeg(v); $("powerMsg").textContent = t("power_active", t("power_names")[v]); toast(t("power_active", t("power_names")[v]).split(".")[0]); }
    catch (e) { toast(e.message, true); }
  });
}
async function loadPower() {
  try {
    const r = await api("power");
    renderPowerSeg(r.profile);
    $("powerMsg").textContent = r.profile >= 0 ? t("power_active", t("power_names")[r.profile]) : t("power_unknown", r.error || "");
  } catch (e) { renderPowerSeg(-1); }
}
$("autoTrim").onchange = () => setCfg({ auto_trim: $("autoTrim").checked });
$("thr").oninput = () => $("thrLbl").textContent = t("thr_lbl", $("thr").value);
$("thr").onchange = () => setCfg({ trim_threshold: Number($("thr").value) });

// ---------- Твики ----------
let APPS = null, SETTINGS = null;
const appSel = new Set();
// Вкладки внутри раздела: переключение затрагивает только свою страницу.
document.querySelectorAll(".subtabs button").forEach(b => b.onclick = () => showSub(b.dataset.sub));
function showSub(sub) {
  const b = document.querySelector(`.subtabs button[data-sub="${sub}"]`);
  if (!b) return;
  const page = b.closest(".page");
  page.querySelectorAll(".subtabs button").forEach(x => x.classList.toggle("active", x === b));
  page.querySelectorAll(".subpane").forEach(s => s.classList.toggle("active", s.id === "sub-" + sub));
  if (sub === "settings") loadSettings();
  if (typeof onSubShow === "function") onSubShow(sub);
}
async function loadTweaks(force) {
  if (!HOST.tweaks) return;
  if (APPS && !force) return;
  $("appsList").innerHTML = `<div class="loading">${t("loading_apps")}</div>`;
  try { APPS = await api("tweaks/apps"); } catch (e) { $("appsList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  [...appSel].forEach(id => { const a = APPS.find(x => x.id === id); if (!a || !a.installed) appSel.delete(id); });
  renderApps();
}
const APP_GROUPS = ["ads", "xbox", "media", "bing", "office", "ai", "tools", "system"];
function renderApps() {
  if (!APPS) return;
  const only = $("twOnlyInst").checked;
  let html = "";
  for (const g of APP_GROUPS) {
    const items = APPS.filter(a => a.group === g && (!only || a.installed));
    if (!items.length) continue;
    html += `<div class="group"><div class="group-title">${t("g_" + g)}</div><div class="items">` + items.map(a => `
      <label class="item ${a.installed ? "" : "na"}">
        <input type="checkbox" data-id="${a.id}" ${appSel.has(a.id) ? "checked" : ""} ${a.installed ? "" : "disabled"}>
        <div><div class="n">${esc(a.name)}</div><div class="d">${esc(a.desc)}</div>${a.caution ? `<div class="c">${esc(a.caution)}</div>` : ""}</div>
        <div class="tags">${a.recommended ? `<span class="tag">${t("recommended")}</span>` : ""}<span class="tag ${a.installed ? "solid" : ""}">${a.installed ? t("installed") : t("not_installed")}</span></div>
      </label>`).join("") + `</div></div>`;
  }
  $("appsList").innerHTML = html || `<div class="empty">${t("no_apps")}</div>`;
  $("appsList").querySelectorAll("input[data-id]").forEach(c => c.onchange = () => { c.checked ? appSel.add(c.dataset.id) : appSel.delete(c.dataset.id); updateRemoveBtn(); });
  updateRemoveBtn();
}
function updateRemoveBtn() { $("twRemove").textContent = t("remove_n", appSel.size); $("twRemove").disabled = !appSel.size; }
$("twOnlyInst").onchange = renderApps;
$("twReload").onclick = () => loadTweaks(true);
$("twRec").onclick = () => { (APPS || []).forEach(a => { if (a.recommended && a.installed) appSel.add(a.id); }); renderApps(); };
$("twNone").onclick = () => { appSel.clear(); renderApps(); };
$("twRemove").onclick = async () => {
  const names = APPS.filter(a => appSel.has(a.id)).map(a => "• " + a.name).join("\n");
  let body = t("remove_q", appSel.size, names);
  if (!HOST.elevated) body += t("remove_admin_note");
  if (!await modal({ title: t("nav_tweaks"), body, ok: t("del"), danger: true })) return;
  const btn = $("twRemove");
  await busy(btn, async () => {
    const res = await api("tweaks/apps/remove", { ids: [...appSel] });
    const ok = res.filter(r => r.ok).length;
    toast(t("removed_n", ok, res.length), ok < res.length);
    res.filter(r => !r.ok).forEach(r => toast(`${(APPS.find(a => a.id === r.id) || {}).name || r.id}: ${r.msg}`, true));
    appSel.clear();
  });
  loadTweaks(true);
};
async function loadSettings() {
  if (!HOST.tweaks) return;
  $("settingsList").innerHTML = `<div class="loading">${t("loading")}</div>`;
  try { SETTINGS = await api("tweaks/settings"); } catch (e) { $("settingsList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderSettings();
}
const SET_GROUPS = ["privacy", "ads", "ai", "taskbar", "explorer", "perf", "gaming", "update", "system", "security"];
const RESTART_HINT = { explorer: "restart_explorer_hint", logoff: "restart_logoff_hint", reboot: "restart_reboot_hint" };
function renderSettings() {
  if (!SETTINGS) return;
  const q = $("twSetSearch").value.trim().toLowerCase();
  const recOnly = $("twRecOnly").checked;
  const on = SETTINGS.filter(s => s.enabled).length;
  const recLeft = SETTINGS.filter(s => s.recommended && !s.enabled).length;
  $("twSetStats").textContent = t("tw_stats", on, SETTINGS.length);
  $("twApplyRec").textContent = t("apply_rec_n", recLeft);
  $("twApplyRec").disabled = !recLeft;
  let html = "";
  for (const g of SET_GROUPS) {
    const items = SETTINGS.filter(s => s.group === g && (!recOnly || s.recommended) &&
      (!q || (s.title + " " + s.desc).toLowerCase().includes(q)));
    if (!items.length) continue;
    const gOn = items.filter(s => s.enabled).length;
    html += `<div class="group"><div class="group-title">${t("gs_" + g)} <span class="gcount">${gOn}/${items.length}</span></div><div class="items">` + items.map(s => `
      <div class="item">
        <label class="switch"><input type="checkbox" data-id="${s.id}" ${s.enabled ? "checked" : ""}><i></i></label>
        <div><div class="n">${esc(s.title)}</div><div class="d">${esc(s.desc)}</div>${s.caution ? `<div class="c">${esc(s.caution)}</div>` : ""}</div>
        <div class="tags">${s.recommended ? `<span class="tag">${t("recommended")}</span>` : ""}${s.admin ? `<span class="tag">${t("needs_admin")}</span>` : ""}${s.restart ? `<span class="tag">${t(RESTART_HINT[s.restart])}</span>` : ""}</div>
      </div>`).join("") + `</div></div>`;
  }
  $("settingsList").innerHTML = html || `<div class="empty">${t("nothing_found")}</div>`;
  $("settingsList").querySelectorAll("input[data-id]").forEach(c => c.onchange = async () => {
    c.disabled = true;
    const s = SETTINGS.find(x => x.id === c.dataset.id);
    try {
      await api("tweaks/settings", { id: c.dataset.id, on: c.checked });
      s.enabled = c.checked;
      if (s.restart && c.checked) toast(`${s.title}: ${t(RESTART_HINT[s.restart])}`);
    } catch (e) { toast(e.message, true); c.checked = !c.checked; }
    c.disabled = false;
    renderSettings();
  });
}
$("twSetSearch").oninput = renderSettings;
$("twRecOnly").onchange = renderSettings;
$("twSetReload").onclick = loadSettings;
$("twApplyRec").onclick = async () => {
  const todo = SETTINGS.filter(s => s.recommended && !s.enabled && (HOST.elevated || !s.admin));
  const skipped = SETTINGS.filter(s => s.recommended && !s.enabled && s.admin && !HOST.elevated);
  let body = t("apply_rec_q", todo.length) + "\n\n" + todo.map(s => "• " + s.title).join("\n");
  if (skipped.length) body += t("apply_rec_skip", skipped.length);
  if (!todo.length) return modal({ title: t("apply_rec"), body: t("apply_rec_none") + (skipped.length ? t("apply_rec_skip", skipped.length) : ""), cancel: null });
  if (!await modal({ title: t("apply_rec"), body, ok: t("apply") })) return;
  await busy($("twApplyRec"), async () => {
    let ok = 0;
    for (const s of todo) {
      try { await api("tweaks/settings", { id: s.id, on: true }); ok++; }
      catch (e) { toast(`${s.title}: ${e.message}`, true); }
    }
    toast(t("applied_n", ok, todo.length), ok < todo.length);
  });
  loadSettings();
};
$("restorePoint").onclick = () => busy($("restorePoint"), async () => { await api("tweaks/restore-point", {}); toast(t("rp_ok")); });
$("restartExplorer").onclick = () => busy($("restartExplorer"), async () => { await api("tweaks/restart-explorer", {}); toast(t("explorer_ok")); });

// ---------- Автозагрузка ----------
let AUTOSTART = null;
async function loadAutostart(force) {
  if (!HOST.autostart) return;
  if (AUTOSTART && !force) return;
  $("autostartList").innerHTML = `<div class="loading">${t("loading")}</div>`;
  try { AUTOSTART = await api("autostart"); } catch (e) { $("autostartList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderAutostart();
}
function renderAutostart() {
  if (!AUTOSTART) return;
  const q = $("asSearch").value.trim().toLowerCase();
  const on = AUTOSTART.filter(a => a.enabled).length;
  $("asStats").textContent = t("as_stats", on, AUTOSTART.length);
  const items = AUTOSTART.filter(a => !q || (a.name + " " + a.command).toLowerCase().includes(q));
  if (!items.length) { $("autostartList").innerHTML = `<div class="empty">${t("nothing_found")}</div>`; return; }
  $("autostartList").innerHTML = `<div class="items">` + items.map(a => `
    <div class="item">
      <label class="switch"><input type="checkbox" data-id="${esc(a.id)}" ${a.enabled ? "checked" : ""}><i></i></label>
      <div><div class="n">${esc(a.name)}</div><div class="d mono" title="${esc(a.command)}">${esc(a.command)}</div></div>
      <div class="tags"><span class="tag">${esc(a.location)}</span>${a.admin ? `<span class="tag">${t("needs_admin")}</span>` : ""}</div>
    </div>`).join("") + `</div>`;
  $("autostartList").querySelectorAll("input[data-id]").forEach(c => c.onchange = async () => {
    c.disabled = true;
    const a = AUTOSTART.find(x => x.id === c.dataset.id);
    try { await api("autostart", { id: c.dataset.id, on: c.checked, name: a.name }); a.enabled = c.checked; toast(`${a.name}: ${c.checked ? t("as_on") : t("as_off")}`); }
    catch (e) { toast(e.message, true); c.checked = !c.checked; }
    c.disabled = false;
    renderAutostart();
  });
}
$("asSearch").oninput = renderAutostart;
$("asReload").onclick = () => loadAutostart(true);

// ---------- Обслуживание ----------
let MAINT = null, maintPoll = null;
const MAINT_GROUPS = ["repair", "network"];
async function loadMaintenance() {
  if (!HOST.maintenance || MAINT) return;
  try { MAINT = await api("maintenance/actions"); } catch (e) { $("maintList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderMaintenance();
  pollMaintJob();
}
function renderMaintenance() {
  let html = "";
  for (const g of MAINT_GROUPS) {
    const items = MAINT.filter(a => a.group === g);
    if (!items.length) continue;
    html += `<div class="group"><div class="group-title">${t("mg_" + g)}</div><div class="items">` + items.map(a => `
      <div class="item">
        <div><div class="n">${esc(a.title)}</div><div class="d">${esc(a.desc)}</div></div>
        <div class="tags">${a.mins ? `<span class="tag">~${a.mins} ${t("min")}</span>` : ""}${a.admin ? `<span class="tag">${t("needs_admin")}</span>` : ""}
          <button class="btn maint-run" data-id="${a.id}" data-title="${esc(a.title)}">${t("run")}</button></div>
      </div>`).join("") + `</div></div>`;
  }
  $("maintList").innerHTML = html;
  $("maintList").querySelectorAll(".maint-run").forEach(b => b.onclick = () => runMaint(b.dataset.id, b.dataset.title));
}
async function runMaint(id, title) {
  try { await api("maintenance/run", { id }); } catch (e) { return toast(e.message, true); }
  showJobModal(title);
  pollMaintJob();
}
let jobModalOpen = false, jobOnDone = null;
function showJobModal(title, onDone) {
  jobModalOpen = true;
  jobOnDone = onDone || null;
  $("mTitle").textContent = title;
  $("mBody").innerHTML = `<div id="jobLog" class="joblog"></div>`;
  $("mOk").classList.add("hidden");
  $("mCancel").textContent = t("running");
  $("mCancel").classList.remove("hidden");
  $("modal").classList.remove("hidden");
  $("mCancel").onclick = () => { jobModalOpen = false; $("modal").classList.add("hidden"); $("mOk").classList.remove("hidden"); };
}
async function pollMaintJob() {
  clearInterval(maintPoll);
  const tick = async () => {
    let j;
    try { j = await api("maintenance/job"); } catch { return; }
    const lines = j.lines || [];
    if (jobModalOpen && $("jobLog")) {
      $("jobLog").innerHTML = lines.length ? lines.map(l => `<div>${esc(l)}</div>`).join("") : `<div class="muted">${esc(t("running"))}</div>`;
      $("jobLog").scrollTop = 1e9;
      $("mCancel").textContent = j.running ? t("running") : t("close");
    }
    if (!j.running) {
      clearInterval(maintPoll);
      if (j.done && !jobModalOpen) toast(j.ok ? t("maint_done") : t("maint_fail"), !j.ok);
      if (j.done && jobOnDone) { const f = jobOnDone; jobOnDone = null; f(j); }
    }
  };
  // опрос запускаем до первого запроса — сбой одного ответа не должен его остановить
  maintPoll = setInterval(() => tick().catch(() => {}), 1000);
  await tick().catch(() => {});
}
$("maintRestore").onclick = () => busy($("maintRestore"), async () => { await api("tweaks/restore-point", {}); toast(t("rp_ok")); });

// ---------- Драйверы ----------
let DEVICES = null, DRV_AVAIL = {}, DRV_CHECKED = false, drvPoll = null;
const drvSel = new Set();
const DRV_ORDER = ["Материнская плата", "Motherboard", "Процессор", "Processor", "Видеокарта", "Graphics", "Сеть", "Network", "Звук", "Audio"];
async function loadDrivers(force) {
  if (!HOST.drivers) return;
  $("drvUseWU").checked = !CFG.driver_no_wu;
  if (!DEVICES || force) {
    $("devicesList").innerHTML = `<div class="loading">${t("loading")}</div>`;
    try { DEVICES = await api("drivers"); } catch (e) { $("devicesList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
    [...drvSel].forEach(id => { if (!DEVICES.some(d => d.id === id)) drvSel.delete(id); });
    renderDevices();
    loadSysInfo();
  }
  refreshDrvAvail();
}
let SYSINFO = null;
async function loadSysInfo() {
  try { SYSINFO = await api("sysinfo"); } catch { return; }
  const rows = [["board", "si_board"], ["cpu", "si_cpu"], ["gpu", "si_gpu"], ["ram", "si_ram"], ["os", "si_os"]]
    .filter(([k]) => SYSINFO[k]).map(([k, lbl]) => `<span>${t(lbl)}</span><span>${esc(SYSINFO[k])}</span>`);
  $("sysInfoBody").innerHTML = rows.join("") || `<span class="muted">${t("loading")}</span>`;
}
$("sysCopy").onclick = async () => {
  if (!SYSINFO) return;
  const txt = [["si_board", SYSINFO.board], ["si_cpu", SYSINFO.cpu], ["si_gpu", SYSINFO.gpu], ["si_ram", SYSINFO.ram], ["si_os", SYSINFO.os]]
    .filter(([, v]) => v).map(([k, v]) => `${t(k)}: ${v}`).join("\n");
  try { await navigator.clipboard.writeText(txt); toast(t("copied")); } catch { toast(txt); }
};
const drvHas = id => { const a = DRV_AVAIL[id]; return a && (a.source === "nvidia" || a.source === "wu"); };
function drvStatusHTML(d) {
  const a = DRV_AVAIL[d.id];
  if (!a) return d.needsDriver ? `<span class="tag solid">${t("needs_driver")}</span>` : "";
  if (a.source === "nvidia" || a.source === "wu") {
    const src = a.source === "nvidia" ? "nvidia.com" : "Windows Update";
    return `<span class="tag solid" title="${esc(a.title)}">${t("drv_avail", a.version || t("drv_newer"))}</span><span class="tag">${src}${a.size ? " · " + bytes(a.size) : ""}</span>`;
  }
  if (a.source === "current") return `<span class="tag">${a.note ? esc(a.note) : t("drv_current")}</span>`;
  return `<span class="tag" title="${esc(a.note || "")}">${t("drv_none")}</span>`;
}
function renderDevices() {
  if (!DEVICES) return;
  const q = $("drvSearch").value.trim().toLowerCase();
  const list = DEVICES.filter(d => !q || (d.name + " " + d.manu + " " + d.category).toLowerCase().includes(q));
  const byCat = {};
  list.forEach(d => { (byCat[d.category] = byCat[d.category] || []).push(d); });
  const cats = Object.keys(byCat).sort((a, b) => {
    const ia = DRV_ORDER.indexOf(a), ib = DRV_ORDER.indexOf(b);
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);
  });
  if (!cats.length) { $("devicesList").innerHTML = `<div class="empty">${t("nothing_found")}</div>`; updDrvButtons(); return; }
  $("devicesList").innerHTML = cats.map(cat => `<div class="group"><div class="group-title">${esc(cat)}</div><div class="items">` +
    byCat[cat].map(d => {
      const a = DRV_AVAIL[d.id];
      const cur = a && a.source === "nvidia" && a.current ? a.current : d.version;
      return `<div class="item drv-item ${d.needsDriver || drvHas(d.id) ? "attn" : ""}">
      <input type="checkbox" class="drv-chk" data-id="${esc(d.id)}" ${drvSel.has(d.id) ? "checked" : ""} ${DRV_CHECKED && !drvHas(d.id) ? "disabled" : ""}>
      <div><div class="n">${esc(d.name)}</div><div class="d">${esc([d.manu, cur ? "v" + cur : "", d.date].filter(Boolean).join(" · ") || t("no_driver_info"))}${a && a.title && drvHas(d.id) ? `<br><span class="muted">→ ${esc(a.title)}</span>` : ""}</div></div>
      <div class="tags">${drvStatusHTML(d)}
        ${drvHas(d.id) ? `<button class="btn btn-accent drv-one" data-id="${esc(d.id)}">${t("drv_update")}</button>` : ""}
        ${d.vendorURL ? `<button class="icon-btn drv-site" data-url="${esc(d.vendorURL)}" title="${t("drv_site", d.vendor)}">↗</button>` : ""}</div>
    </div>`; }).join("") + `</div></div>`).join("");
  $("devicesList").querySelectorAll(".drv-chk").forEach(c => c.onchange = () => { c.checked ? drvSel.add(c.dataset.id) : drvSel.delete(c.dataset.id); updDrvButtons(); });
  $("devicesList").querySelectorAll(".drv-one").forEach(b => b.onclick = () => updateDrivers([b.dataset.id], false));
  $("devicesList").querySelectorAll(".drv-site").forEach(b => b.onclick = () => api("drivers/site", { url: b.dataset.url }).catch(e => toast(e.message, true)));
  updDrvButtons();
}
function updDrvButtons() {
  const n = Object.keys(DRV_AVAIL).filter(drvHas).length;
  const blocked = typeof polBlocks === "function" && polBlocks("drivers");
  $("drvUpdSel").textContent = t("drv_update_sel", drvSel.size);
  $("drvUpdSel").disabled = !drvSel.size || blocked;
  $("drvUpdAll").textContent = DRV_CHECKED ? t("drv_update_all_n", n) : t("drv_update_all");
  $("drvUpdAll").disabled = (DRV_CHECKED && !n) || blocked;
  $("drvSelInfo").textContent = drvSel.size ? t("drv_sel_info", drvSel.size) : "";
  const boxes = [...document.querySelectorAll(".drv-chk:not(:disabled)")];
  $("drvSelAll").checked = boxes.length > 0 && boxes.every(c => c.checked);
  $("drvSummary").textContent = DRV_CHECKED ? (n ? t("drv_found_n", n) : t("drv_all_current")) : t("drv_intro");
}
$("drvSelAll").onchange = () => {
  document.querySelectorAll(".drv-chk:not(:disabled)").forEach(c => { c.checked = $("drvSelAll").checked; c.checked ? drvSel.add(c.dataset.id) : drvSel.delete(c.dataset.id); });
  updDrvButtons();
};
$("drvSearch").oninput = renderDevices;
$("drvReload").onclick = () => loadDrivers(true);
$("drvUseWU").onchange = () => setCfg({ driver_no_wu: !$("drvUseWU").checked });
// Проверка и установка идут в фоне: строка хода в разделе, журнал — по кнопке,
// по завершении — уведомление (даже если пользователь ушёл в другой раздел).
let drvWatching = false, drvJobKind = "", drvLogOpen = false;
function renderDrvProgress(job) {
  const lines = (job && job.lines) || [];
  const running = job && job.running;
  $("drvProgress").classList.toggle("hidden", !running && !lines.length);
  $("drvProgress").classList.toggle("done", !running);
  const last = [...lines].reverse().find(l => l.trim()) || t("driver_working");
  $("drvProgText").textContent = running ? last.replace(/^[▸\s]+/, "") : (job.ok ? t("drv_done_ok") : t("drv_done_err")) + " " + last.replace(/^[✓×▸!\s]+/, "");
  $("drvLog").innerHTML = lines.map(l => `<div>${esc(l)}</div>`).join("");
  $("drvLog").classList.toggle("hidden", !drvLogOpen);
  if (drvLogOpen) $("drvLog").scrollTop = 1e9;
  $("drvLogToggle").textContent = t(drvLogOpen ? "drv_log_hide" : "drv_log_show");
  $("drvCheck").disabled = !!running;
  if (running) { $("drvUpdSel").disabled = true; $("drvUpdAll").disabled = true; }
}
$("drvLogToggle").onclick = () => { drvLogOpen = !drvLogOpen; $("drvLog").classList.toggle("hidden", !drvLogOpen); $("drvLogToggle").textContent = t(drvLogOpen ? "drv_log_hide" : "drv_log_show"); $("drvLog").scrollTop = 1e9; };
async function refreshDrvAvail() {
  let r; try { r = await api("drivers/updates"); } catch { return; }
  DRV_AVAIL = r.avail || {}; DRV_CHECKED = !!r.checked;
  const running = r.job && r.job.running;
  renderDevices();
  renderDrvProgress(r.job);
  clearTimeout(drvPoll);
  if (running) {
    drvWatching = true;
    if (!drvJobKind) drvJobKind = r.job.phase;
    drvPoll = setTimeout(refreshDrvAvail, 1500);
    return;
  }
  if (drvWatching) { // операция только что завершилась
    drvWatching = false;
    const n = Object.keys(DRV_AVAIL).filter(drvHas).length;
    if (drvJobKind === "check") {
      drvSel.clear(); Object.keys(DRV_AVAIL).filter(drvHas).forEach(id => drvSel.add(id));
      renderDevices(); renderDrvProgress(r.job);
      toast(t("nav_drivers") + ": " + (r.job.ok ? (n ? t("drv_found_n", n) : t("drv_all_current")) : t("drv_done_err")), !r.job.ok);
    } else {
      toast(t("nav_drivers") + ": " + (r.job.ok ? t("drv_installed_ok") : t("drv_done_err")), !r.job.ok);
      drvSel.clear(); loadDrivers(true);
    }
    drvJobKind = "";
  }
}
function startDrvWatch(kind) {
  drvJobKind = kind; drvWatching = true;
  $("drvProgress").classList.remove("hidden", "done");
  $("drvProgText").textContent = t("driver_working");
  $("drvCheck").disabled = true;
  refreshDrvAvail();
}
$("drvCheck").onclick = async () => {
  try { await api("drivers/check", {}); } catch (e) { return toast(e.message, true); }
  startDrvWatch("check");
};
async function updateDrivers(ids, all) {
  if (!HOST.elevated) return toast(t("driver_need_admin"), true);
  const names = (DEVICES || []).filter(d => all ? drvHas(d.id) : ids.includes(d.id)).map(d => d.name);
  const body = all && !DRV_CHECKED ? t("drv_update_all_q0") : t("drv_update_q", names.map(n => "• " + n).join("\n"));
  if (!await modal({ title: t("drv_update"), body: body + "\n\n" + t("drv_bg_note"), ok: t("drv_update") })) return;
  try { await api("drivers/update", { ids, all, names }); } catch (e) { return toast(e.message, true); }
  startDrvWatch("update");
}
$("drvUpdSel").onclick = () => updateDrivers([...drvSel], false);
$("drvUpdAll").onclick = () => updateDrivers([], true);

// ---------- Registry healer ----------
let REG_ISSUES = null;
const REG_GROUPS = ["startup", "apppaths", "uninstall"];
async function scanRegistry() {
  await busy($("regScan"), async () => {
    try { REG_ISSUES = await api("reg/scan"); } catch (e) { toast(e.message, true); return; }
    renderRegistry();
  });
}
$("regScan").onclick = scanRegistry;
function renderRegistry() {
  if (!REG_ISSUES) return;
  $("regStats").textContent = t("reg_found", REG_ISSUES.length);
  $("regSelAll").disabled = !REG_ISSUES.length;
  updateRegFix();
  if (!REG_ISSUES.length) { $("regList").innerHTML = `<div class="empty">${t("reg_clean")}</div>`; return; }
  let html = "";
  for (const g of REG_GROUPS) {
    const items = REG_ISSUES.filter(i => i.category === g);
    if (!items.length) continue;
    html += `<div class="group"><div class="group-title">${t("rc_" + g)} <span class="gcount">${items.length}</span></div><div class="items">` +
      items.map(i => `<label class="item">
        <input type="checkbox" class="reg-chk" data-id="${esc(i.id)}">
        <div><div class="n">${esc(i.title)}</div><div class="d mono" title="${esc(i.location)}">${esc(i.detail)}</div></div>
        <div class="tags"><span class="tag src">${esc(i.location.split("\\")[0])}</span></div>
      </label>`).join("") + `</div></div>`;
  }
  $("regList").innerHTML = html;
  $("regList").querySelectorAll(".reg-chk").forEach(c => c.onchange = updateRegFix);
}
function regSelected() { return [...document.querySelectorAll(".reg-chk:checked")].map(c => c.dataset.id); }
function updateRegFix() {
  const n = regSelected().length;
  $("regFix").textContent = t("reg_fix_n", n);
  $("regFix").disabled = !n;
}
$("regSelAll").onclick = () => { document.querySelectorAll(".reg-chk").forEach(c => c.checked = true); updateRegFix(); };
$("regFix").onclick = async () => {
  const ids = regSelected();
  if (!ids.length) return;
  if (!await modal({ title: t("nav_registry"), body: t("reg_fix_confirm", ids.length), ok: t("reg_fix_do"), danger: true })) return;
  await busy($("regFix"), async () => {
    try {
      const rep = await api("reg/fix", { ids });
      toast(t("reg_fixed", rep.removed));
      if (rep.backup) await modal({ title: t("reg_backup_title"), body: t("reg_backup_body", rep.backup), cancel: null });
      REG_ISSUES = await api("reg/scan");
      renderRegistry();
    } catch (e) { toast(e.message, true); }
  });
};
$("regRestore").onclick = () => busy($("regRestore"), async () => { await api("tweaks/restore-point", {}); toast(t("rp_ok")); });

// ---------- Журнал ----------
const LOG = [];
const MARK = ["•", "+", "!", "×"];
async function pollLog() {
  try {
    const items = await api("log?since=" + state.logId);
    if (items.length) {
      items.forEach(e => LOG.unshift(e));
      if (LOG.length > 1000) LOG.length = 1000;
      state.logId = items[items.length - 1].id;
      const last = items[items.length - 1];
      $("status").textContent = `${MARK[last.lvl]}  ${last.msg}`;
      state.statusHold = Date.now() + 5000;
      renderLog();
    }
  } catch (e) { /* ядро недоступно — сообщит опрос снимков */ }
}
function renderLog() {
  $("logList").innerHTML = LOG.map(e => `<div class="log-row l${e.lvl}"><span class="tm">${e.time}</span><span class="mk">${MARK[e.lvl]}</span><span>${esc(e.msg)}</span></div>`).join("");
}
$("clearLog").onclick = async () => { await api("log/clear", {}); LOG.length = 0; renderLog(); };

// ---------- Настройки ----------
async function setCfg(patch) {
  try { CFG = await api("config", patch); } catch (e) { toast(e.message, true); }
  return CFG;
}
function renderSettingsPage() {
  document.querySelectorAll("#langSeg button").forEach(b => b.classList.toggle("on", b.dataset.v === LANG));
  document.querySelectorAll("#themeSeg button").forEach(b => b.classList.toggle("on", b.dataset.v === CFG.theme));
  $("interval").innerHTML = [1, 2, 3, 5, 10].map(v => `<option value="${v}">${t("sec", v)}</option>`).join("");
  $("interval").value = CFG.interval_sec;
  $("tempAge").innerHTML = [1, 2, 7, 30].map(v => `<option value="${v}">${t("days", v)}</option>`).join("");
  $("tempAge").value = CFG.temp_age_days;
  $("hogThr").value = CFG.hog_threshold; $("hogLbl").textContent = t("hog_lbl", CFG.hog_threshold);
  $("autoRules").checked = CFG.auto_rules; $("autoTrim").checked = CFG.auto_trim;
  $("thr").value = CFG.trim_threshold; $("thrLbl").textContent = t("thr_lbl", CFG.trim_threshold);
  $("gameMode").checked = CFG.game_mode; $("notify").checked = CFG.notify; $("autoUpdate").checked = CFG.auto_update_check;
  $("closeTray").checked = CFG.close_to_tray; $("runStartup").checked = CFG.run_at_startup;
  renderGames();
  if (!state.tempScan) $("tempInfo").textContent = t("temp_none_yet", CFG.temp_age_days);
  const about = [[t("version"), HOST.version], [t("system"), `${HOST.os} (${HOST.goos})`], [t("rights"), HOST.elevated ? t("r_admin") : t("r_user")],
    [t("affinity"), HOST.affinity ? t("available") : t("unavailable")], [t("config_file"), HOST.configPath]];
  $("about").innerHTML = about.map(([k, v]) => `<span>${esc(k)}</span><span>${esc(v)}</span>`).join("");
}
document.querySelectorAll("#langSeg button").forEach(b => b.onclick = async () => {
  if (b.dataset.v === LANG) return;
  await setCfg({ lang: b.dataset.v });
  location.reload();
});
document.querySelectorAll("#themeSeg button").forEach(b => b.onclick = async () => {
  document.documentElement.dataset.theme = b.dataset.v;
  await setCfg({ theme: b.dataset.v });
  renderSettingsPage();
  Object.values(sparks).forEach(s => s.draw());
});
$("interval").onchange = () => { setCfg({ interval_sec: Number($("interval").value) }).then(schedulePoll); };
$("tempAge").onchange = () => setCfg({ temp_age_days: Number($("tempAge").value) });
$("hogThr").oninput = () => $("hogLbl").textContent = t("hog_lbl", $("hogThr").value);
$("hogThr").onchange = () => setCfg({ hog_threshold: Number($("hogThr").value) });

// Игровой режим
function renderGames() {
  const games = CFG.games || [];
  $("gamesList").innerHTML = games.length
    ? games.map((g, i) => `<span class="chip">${esc(g)}<button data-i="${i}" title="${t("del")}">✕</button></span>`).join("")
    : `<span class="muted small">${t("games_empty")}</span>`;
  $("gamesList").querySelectorAll("button").forEach(b => b.onclick = async () => {
    const games = (CFG.games || []).slice(); games.splice(Number(b.dataset.i), 1);
    await setCfg({ games }); renderGames();
  });
}
$("gameMode").onchange = () => setCfg({ game_mode: $("gameMode").checked });
$("notify").onchange = () => setCfg({ notify: $("notify").checked });
$("autoUpdate").onchange = () => setCfg({ auto_update_check: $("autoUpdate").checked });
$("closeTray").onchange = () => setCfg({ close_to_tray: $("closeTray").checked });
$("runStartup").onchange = async () => { await setCfg({ run_at_startup: $("runStartup").checked }); toast($("runStartup").checked ? t("startup_on") : t("startup_off")); };
async function addGame() {
  let v = $("gameInput").value.trim(); if (!v) return;
  const games = (CFG.games || []).slice();
  if (!games.some(g => g.toLowerCase() === v.toLowerCase())) games.push(v);
  $("gameInput").value = "";
  await setCfg({ games }); renderGames();
}
$("gameAdd").onclick = addGame;
$("gameInput").addEventListener("keydown", e => { if (e.key === "Enter") addGame(); });

// Проверка обновлений
$("checkUpdate").onclick = () => busy($("checkUpdate"), async () => {
  let info;
  try { info = await api("update"); } catch (e) { $("updateMsg").textContent = e.message; return; }
  if (info.available) {
    $("updateMsg").textContent = t("update_available", info.latest);
    if (await modal({ title: t("check_update"), body: t("update_available", info.latest) + (info.notes ? "\n\n" + info.notes : ""), ok: t("open_releases") }))
      api("update/open", {}).catch(() => {});
  } else {
    $("updateMsg").textContent = t("update_none", info.current);
  }
});

// Экспорт / импорт профиля
$("exportProfile").onclick = async () => {
  try {
    const p = await api("profile/export");
    const blob = new Blob([JSON.stringify(p, null, 2)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob); a.download = "HealCoreOps-profile.json"; a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
    toast(t("profile_exported"));
  } catch (e) { toast(e.message, true); }
};
$("importProfile").onclick = () => $("importFile").click();
$("importFile").onchange = async () => {
  const f = $("importFile").files[0]; if (!f) return;
  $("importFile").value = "";
  let p;
  try { p = JSON.parse(await f.text()); } catch { return toast(t("bad_profile"), true); }
  if (!await modal({ title: t("import_profile"), body: t("import_confirm"), ok: t("apply") })) return;
  try {
    const rep = await api("profile/import", p);
    CFG = await api("config"); RULES = await api("rules");
    LANG = CFG.lang === "en" ? "en" : "ru";
    toast(t("profile_imported", rep.changed));
    setTimeout(() => location.reload(), 800);
  } catch (e) { toast(e.message, true); }
};

// ---------- Здоровье железа ----------
async function pollHealth() {
  if (!HOST.health) return;
  let h;
  try { h = await api("health"); } catch { return; }
  if (!h.ready) return;
  const rows = [];
  (h.gpus || []).forEach(g => {
    let v = `${g.usage.toFixed(0)}%`;
    if (g.vramTotal) v += ` · ${bytes(g.vramUsed)} / ${bytes(g.vramTotal)}`;
    else if (g.vramUsed) v += ` · ${bytes(g.vramUsed)}`;
    rows.push(meterHTML(t("gpu"), g.usage / 100, v, 120));
  });
  if (h.battery && h.battery.present) {
    const b = h.battery;
    let v = `${b.percent}%` + (b.charging ? " ⚡" : "");
    if (b.wearPct > 0) v = `${t("wear")} ${b.wearPct.toFixed(0)}% · ${v}`;
    rows.push(meterHTML(t("battery"), b.percent / 100, v, 120));
  }
  (h.disks || []).forEach(d => {
    const bits = [d.media];
    if (d.health) bits.push(d.health);
    if (d.tempC) bits.push(d.tempC + "°C");
    if (d.wearPct) bits.push(t("wear") + " " + d.wearPct + "%");
    const label = d.name.length > 22 ? d.name.slice(0, 21) + "…" : d.name;
    rows.push(`<div class="hrow"><span class="hn" title="${esc(d.name)}">${esc(label)}</span><span class="hv">${esc(bits.join(" · "))}</span></div>`);
  });
  $("healthBody").innerHTML = rows.join("") || `<div class="muted">${t("no_health")}</div>`;
  $("healthCard").classList.remove("hidden");
}

// ---------- Цикл обновления ----------
let pollTimer = null, failures = 0;
async function poll() {
  try {
    SNAP = await api("snapshot");
    failures = 0;
    renderOverview(SNAP);
    renderProcs();
    renderOptimize(SNAP);
    if (!state.statusHold || Date.now() > state.statusHold)
      $("status").textContent = t("status", SNAP.procs.length, SNAP.threads, SNAP.rulesActive, CFG.interval_sec);
    $("clock").textContent = new Date(SNAP.time).toLocaleTimeString(LANG === "en" ? "en-GB" : "ru-RU");
  } catch (e) {
    if (++failures > 2) $("status").textContent = t("conn_lost");
  }
}
function schedulePoll() {
  clearInterval(pollTimer);
  pollTimer = setInterval(poll, (CFG.interval_sec || 2) * 1000);
}

async function init() {
  [HOST, CFG, RULES] = await Promise.all([api("host"), api("config"), api("rules")]);
  LANG = CFG.lang === "en" ? "en" : "ru";
  document.documentElement.dataset.theme = CFG.theme === "light" ? "light" : "dark";
  applyI18n();
  renderHeader();
  sparks.cpu = new Spark($("cCpu"), 100); sparks.mem = new Spark($("cMem"), 100);
  sparks.io = new Spark($("cIo"), 0); sparks.net = new Spark($("cNet"), 0);
  renderProcHead(); fillPrioSelect(); fillRuleForm(); renderRules(); renderSettingsPage();
  $("tweaksUnsupported").classList.toggle("hidden", HOST.tweaks);
  $("tweaksMain").classList.toggle("hidden", !HOST.tweaks);
  $("autostartUnsupported").classList.toggle("hidden", HOST.autostart);
  $("autostartMain").classList.toggle("hidden", !HOST.autostart);
  $("maintUnsupported").classList.toggle("hidden", HOST.maintenance);
  $("maintMain").classList.toggle("hidden", !HOST.maintenance);
  $("navAutostart").classList.toggle("hidden", !HOST.autostart);
  $("navMaintenance").classList.toggle("hidden", !HOST.maintenance);
  $("navDrivers").classList.toggle("hidden", !HOST.drivers);
  $("navRegistry").classList.toggle("hidden", !HOST.reghealer);
  $("driversUnsupported").classList.toggle("hidden", HOST.drivers);
  $("driversMain").classList.toggle("hidden", !HOST.drivers);
  $("registryUnsupported").classList.toggle("hidden", HOST.reghealer);
  $("registryMain").classList.toggle("hidden", !HOST.reghealer);
  $("ghLink").onclick = () => api("open-url", {}).catch(() => {});
  if (HOST.version) $("appVer").textContent = "v" + HOST.version;
  if (HOST.goos !== "windows") // трей и автозапуск — только в приложении Windows
    $("closeTray").closest(".card").classList.add("hidden");
  updateRemoveBtn();
  loadPower();
  await poll();
  schedulePoll();
  pollLog(); setInterval(pollLog, 1500);
  if (HOST.health) { pollHealth(); setInterval(pollHealth, 10000); }
  if (typeof init2 === "function") init2();
  window.addEventListener("resize", () => Object.values(sparks).forEach(s => s.draw()));
}
init().catch(e => { document.body.innerHTML = `<pre style="padding:20px">${esc(e.message)}</pre>`; });
