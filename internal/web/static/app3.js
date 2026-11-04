"use strict";
// Разделы версии 1.9: история нагрузки, здоровье железа, подключения и порты,
// дубликаты, снимки системы, Центр обновлений Windows, инвентаризация, оповещения.

const fmtPct = v => (v == null || v < 0) ? "—" : `${v.toFixed(v < 10 ? 1 : 0)}%`;
const bps = v => bytes(v) + (LANG === "en" ? "/s" : "/с");
const timeHM = ms => new Date(ms).toLocaleTimeString(loc(), { hour: "2-digit", minute: "2-digit" });
const dayHM = ms => new Date(ms).toLocaleString(loc(), { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });

function onPageShow3(name, force) {
  const f = { loadhist: () => loadLoadHist(), hwhealth: () => loadHw(force), wu: () => loadWu(force), snaps: () => loadSnaps() }[name];
  if (f) f();
  if (name === "settings") renderAlertSettings();
  if (name === "overview") loadAlerts();
  if (name === "network") { const a = document.querySelector("#page-network .subtabs .active"); if (a) onSubShow3(a.dataset.sub, force); }
  if (name !== "network") stopConnsAuto();
}
function onSubShow3(sub, force) {
  if (sub === "net-conns") { loadConns(); startConnsAuto(); } else if (sub && sub.startsWith("net-")) stopConnsAuto();
  if (sub === "net-ports") loadPorts(force);
  if (sub === "dk-dupes") initDupes();
  if (sub === "wu-history") loadWuHist(force);
}

// ---------- Оповещения на «Обзоре» ----------
async function loadAlerts() {
  let list;
  try { list = await api("alerts"); } catch { return; }
  list = list.filter(a => a.key !== "reboot"); // для неё на «Обзоре» есть своя плашка
  $("alertsCard").classList.toggle("hidden", !list.length);
  $("alertsList").innerHTML = list.map(a => `<div class="check-row l${a.level}">${lvDot(a.level)}
    <div class="grow"><div class="n">${esc(a.title)}</div><div class="d">${esc(a.text)} <span class="muted">· ${t("al_since", dayHM(a.since))}</span></div></div>
    ${a.page ? `<button class="btn al-go" data-p="${esc(a.page)}">${t("al_open")}</button>` : ""}</div>`).join("");
  $("alertsList").querySelectorAll(".al-go").forEach(b => b.onclick = () => showPage(b.dataset.p));
}
$("alertsSettings").onclick = () => { showPage("settings"); setTimeout(() => $("alOn").scrollIntoView({ block: "center", behavior: "smooth" }), 50); };

// ---------- История нагрузки ----------
const LH = { hours: 24, data: null, pin: null, hover: null };
function lhRangeLabels() {
  const labels = t("lh_range");
  $("lhRange").querySelectorAll("button").forEach((b, i) => { b.textContent = labels[i]; b.classList.toggle("on", Number(b.dataset.h) === LH.hours); });
}
$("lhRange").querySelectorAll("button").forEach(b => b.onclick = () => { LH.hours = Number(b.dataset.h); LH.pin = null; lhRangeLabels(); loadLoadHist(); });
$("lhReload").onclick = () => loadLoadHist();
async function loadLoadHist() {
  lhRangeLabels();
  let r;
  try { r = await api("loadhist?hours=" + LH.hours); } catch (e) { toast(e.message, true); return; }
  LH.data = r;
  const R = r.range;
  if (!r.enabled) {
    $("lhInfo").textContent = t("lh_disabled");
  } else {
    const step = R.step >= 3600000 ? (R.step / 3600000) + " " + t("lh_h") : (R.step / 60000) + " " + t("lh_min");
    const cov = R.minutes >= 60 ? (R.minutes / 60).toFixed(1).replace(/\.0$/, "") + " " + t("lh_h") : R.minutes + " " + t("lh_min");
    $("lhInfo").textContent = t("lh_info", cov, step);
  }
  drawLoadHist();
  const topRow = (n, v, frac) => meterHTML(n, frac, v, 170);
  $("lhTopCpu").innerHTML = R.topCpu.length ? R.topCpu.map(x => topRow(x.n, t("lh_avg", fmtPct(x.v)), x.v / Math.max(R.topCpu[0].v, 1))).join("") : emptyHTML(r.enabled ? "lh_empty" : "lh_disabled");
  $("lhTopMem").innerHTML = R.topMem.length ? R.topMem.map(x => topRow(x.n, bytes(x.v), x.v / R.topMem[0].v)).join("") : emptyHTML(r.enabled ? "lh_empty" : "lh_disabled");
  renderLhMoment();
}
function lhGeom() {
  const c = $("lhChart"), w = c.clientWidth, h = c.clientHeight;
  const R = LH.data.range, pad = { l: 34, r: 8, t: 8, b: 4 };
  const x = ms => pad.l + (ms - R.from) / (R.to - R.from) * (w - pad.l - pad.r);
  const y = v => pad.t + (1 - Math.min(Math.max(v, 0), 100) / 100) * (h - pad.t - pad.b);
  return { c, w, h, pad, x, y, R };
}
function drawLoadHist() {
  if (!LH.data) return;
  const { c, w, h, pad, x, y, R } = lhGeom();
  if (!w || !h) return;
  const dpr = window.devicePixelRatio || 1;
  if (c.width !== Math.round(w * dpr) || c.height !== Math.round(h * dpr)) { c.width = Math.round(w * dpr); c.height = Math.round(h * dpr); }
  const g = c.getContext("2d"); g.setTransform(dpr, 0, 0, dpr, 0, 0); g.clearRect(0, 0, w, h);
  const cs = getComputedStyle(document.documentElement), col = v => cs.getPropertyValue(v).trim();
  g.font = "11px Segoe UI, system-ui, sans-serif"; g.fillStyle = col("--dim"); g.textAlign = "right"; g.textBaseline = "middle";
  [0, 25, 50, 75, 100].forEach(v => {
    const yy = Math.round(y(v)) + .5;
    g.strokeStyle = col("--grid"); g.setLineDash(v ? [2, 3] : []); g.beginPath(); g.moveTo(pad.l, yy); g.lineTo(w - pad.r, yy); g.stroke();
    g.fillText(v + "%", pad.l - 6, yy);
  });
  g.setLineDash([]);
  const P = R.points;
  // ось времени
  const ticks = [], span = R.to - R.from, stepT = span <= 4 * 3600e3 ? 3600e3 / 2 : span <= 26 * 3600e3 ? 3 * 3600e3 : 24 * 3600e3;
  let tt = Math.ceil(R.from / stepT) * stepT;
  if (stepT === 24 * 3600e3) { const d = new Date(R.from); d.setHours(24, 0, 0, 0); tt = d.getTime(); }
  for (; tt < R.to; tt += stepT) ticks.push(tt);
  $("lhAxis").innerHTML = ticks.map(v => `<span style="left:${x(v)}px">${stepT >= 24 * 3600e3 ? new Date(v).toLocaleDateString(loc(), { day: "2-digit", month: "2-digit" }) : timeHM(v)}</span>`).join("");
  ticks.forEach(v => { const xx = Math.round(x(v)) + .5; g.strokeStyle = col("--grid"); g.setLineDash([2, 3]); g.beginPath(); g.moveTo(xx, pad.t); g.lineTo(xx, h - pad.b); g.stroke(); });
  g.setLineDash([]);
  if (!P.length) return;
  // разрывы: если между точками больше 3 шагов — программа не работала
  const segs = []; let cur = [];
  P.forEach((p, i) => { if (i && p.t - P[i - 1].t > R.step * 3) { segs.push(cur); cur = []; } cur.push(p); });
  segs.push(cur);
  const area = g.createLinearGradient(0, pad.t, 0, h);
  area.addColorStop(0, col("--area-top")); area.addColorStop(1, col("--area-bot"));
  segs.forEach(sg => {
    if (!sg.length) return;
    // пики ЦП — светлой полосой
    g.fillStyle = col("--hot");
    sg.forEach(p => { g.fillRect(x(p.t), y(p.cpuMax), Math.max(1, x(p.t + R.step) - x(p.t)), y(p.cpu) - y(p.cpuMax)); });
    g.beginPath(); g.moveTo(x(sg[0].t), y(0)); sg.forEach(p => g.lineTo(x(p.t), y(p.cpu))); g.lineTo(x(sg[sg.length - 1].t), y(0)); g.closePath();
    g.fillStyle = area; g.fill();
    g.beginPath(); sg.forEach((p, i) => i ? g.lineTo(x(p.t), y(p.cpu)) : g.moveTo(x(p.t), y(p.cpu)));
    g.strokeStyle = col("--line"); g.lineWidth = 1.6; g.lineJoin = "round"; g.stroke();
    g.beginPath(); sg.forEach((p, i) => i ? g.lineTo(x(p.t), y(p.mem)) : g.moveTo(x(p.t), y(p.mem)));
    g.strokeStyle = col("--f1"); g.lineWidth = 1.2; g.setLineDash([4, 3]); g.stroke(); g.setLineDash([]);
  });
  const mark = LH.hover || LH.pin;
  if (mark) {
    const xx = Math.round(x(mark.t)) + .5;
    g.strokeStyle = col("--text"); g.globalAlpha = .5; g.beginPath(); g.moveTo(xx, pad.t); g.lineTo(xx, h - pad.b); g.stroke(); g.globalAlpha = 1;
    g.fillStyle = col("--line"); g.beginPath(); g.arc(x(mark.t), y(mark.cpu), 3.5, 0, 7); g.fill();
  }
}
function lhNearest(ev) {
  if (!LH.data || !LH.data.range.points.length) return null;
  const { x } = lhGeom(), rect = $("lhChart").getBoundingClientRect(), mx = ev.clientX - rect.left;
  let best = null, bd = 1e9;
  LH.data.range.points.forEach(p => { const d = Math.abs(x(p.t) - mx); if (d < bd) { bd = d; best = p; } });
  return bd < 30 ? best : null;
}
function lhTopText(list, fmt) { return (list || []).slice(0, 3).map(x => `${esc(x.n)} <b>${fmt(x.v)}</b>`).join(" · ") || `<span class="muted">${t("lh_none")}</span>`; }
$("lhChart").addEventListener("mousemove", ev => {
  const p = lhNearest(ev); LH.hover = p; drawLoadHist();
  const tip = $("lhTip");
  if (!p) { tip.classList.add("hidden"); return; }
  tip.innerHTML = `<b>${dayHM(p.t)}</b> · ${t("cpu")} ${fmtPct(p.cpu)} (${t("lh_peak", fmtPct(p.cpuMax))}) · ${t("memory")} ${fmtPct(p.mem)}
    <div>${lhTopText(p.tc, fmtPct)}</div>`;
  tip.classList.remove("hidden");
  const { x } = lhGeom(), wrap = $("lhChart").clientWidth;
  const left = Math.min(Math.max(x(p.t) - tip.offsetWidth / 2, 0), wrap - tip.offsetWidth);
  tip.style.left = left + "px";
});
$("lhChart").addEventListener("mouseleave", () => { LH.hover = null; $("lhTip").classList.add("hidden"); drawLoadHist(); });
$("lhChart").addEventListener("click", ev => { LH.pin = lhNearest(ev); renderLhMoment(); drawLoadHist(); });
function renderLhMoment() {
  const p = LH.pin, box = $("lhMoment");
  if (!p) { box.classList.add("hidden"); return; }
  box.classList.remove("hidden");
  box.innerHTML = `<div class="toolbar" style="margin-bottom:8px"><div class="card-title grow" style="margin:0">${esc(t("lh_moment", dayHM(p.t), fmtPct(p.cpu), fmtPct(p.mem)))}</div><span class="muted small">${t("lh_io", bps(p.dr + p.dw), bps(p.nr + p.nt))}</span><button class="icon-btn" id="lhUnpin">✕</button></div>
    <div class="grid2"><div>${(p.tc || []).map(x => meterHTML(x.n, x.v / 100, fmtPct(x.v), 170)).join("") || `<span class="muted">${t("lh_none")}</span>`}</div>
    <div>${(p.tm || []).map(x => meterHTML(x.n, x.v / (p.tm[0].v || 1), bytes(x.v), 170)).join("") || `<span class="muted">${t("lh_none")}</span>`}</div></div>`;
  $("lhUnpin").onclick = () => { LH.pin = null; renderLhMoment(); drawLoadHist(); };
}
window.addEventListener("resize", () => { if (state.page === "loadhist") drawLoadHist(); });

// ---------- Здоровье железа ----------
let HW = null, hwTimer = null;
// Пока страница открыта — обновлять каждые 2 с (видеокарта — каждые 2 с, диски и батарея — раз в 10 с).
function hwAuto() {
  clearTimeout(hwTimer);
  hwTimer = setTimeout(async () => {
    if (state.page !== "hwhealth" || document.hidden) return;
    try { HW = await api("health"); renderHw(); } catch {}
    hwAuto();
  }, 2000);
}
document.addEventListener("visibilitychange", () => { if (!document.hidden && state.page === "hwhealth") hwAuto(); });
async function loadHw(force) {
  if (!HOST.health) { $("hwUnsupported").classList.remove("hidden"); $("hwMain").classList.add("hidden"); return; }
  if (force) { await busy($("hwReload"), async () => { HW = await api("health/refresh", {}); }); }
  else { try { HW = await api("health"); } catch (e) { toast(e.message, true); return; } }
  renderHw();
  hwAuto();
}
$("hwReload").onclick = () => loadHw(true);
function hwBadge(l) { return `<span class="tag ${l === 2 ? "solid" : ""}">${tm("hw_lvl", l) || t("hw_lvl")[l]}</span>`; }
function kvRows(rows) { return `<div class="kv hw-kv">${rows.filter(Boolean).map(([k, v]) => `<span>${esc(k)}</span><span>${v}</span>`).join("")}</div>`; }
function renderHw() {
  const h = HW || {};
  if (!h.ready) { if (!$("hwDisks").querySelector(".loading")) $("hwDisks").innerHTML = loadingHTML("hw_waiting"); return; }
  $("hwUpdated").textContent = t("hw_updated", new Date(h.updated).toLocaleTimeString(loc()));
  const lvls = t("hw_lvl");
  const unknownWear = (h.disks || []).every(d => !d.tempC && !d.wearPct && !d.powerOnH);
  $("hwLimited").classList.toggle("hidden", !(h.limited && unknownWear));
  const na = `<span class="muted">${t("hw_unknown")}</span>`;
  $("hwDisks").innerHTML = (h.disks || []).length ? `<div class="hw-grid">` + h.disks.map(d => `<div class="hw-cell l${d.level}">
    <div class="toolbar">${lvDot(d.level)}<b class="grow">${esc(d.name)}</b><span class="tag">${esc(d.media || "—")}</span><span class="tag ${d.level === 2 ? "solid" : ""}">${lvls[d.level]}</span></div>
    ${d.reason ? `<div class="hw-reason">${esc(tm("hw_reason", d.reason))}</div>` : ""}
    ${kvRows([
      d.size ? [t("hw_size"), bytes(d.size)] : null,
      [t("hw_status"), esc(d.health || "—")],
      [t("hw_temp"), d.tempC ? `${d.tempC} °C${d.tempMax ? ` <span class="muted">· ${t("hw_temp_max", d.tempMax + " °C")}</span>` : ""}` : na],
      [t("hw_wear"), d.media === "HDD" ? "—" : (d.wearPct || d.powerOnH ? `${d.wearPct}%` : na)],
      [t("hw_hours"), d.powerOnH ? t("hw_hours_v", d.powerOnH.toLocaleString(loc()), Math.round(d.powerOnH / 24)) : na],
      [t("hw_errors"), d.powerOnH || d.readErr || d.writeErr ? `${d.readErr} / ${d.writeErr}` : na],
    ])}</div>`).join("") + `</div>` : emptyHTML("nothing_found");
  const g = (h.gpus || []);
  $("hwGpu").innerHTML = g.length ? g.map(x => `<div class="toolbar" style="margin-bottom:6px">${lvDot(x.level)}<b class="grow">${esc(x.name)}</b>${x.tempC ? hwBadge(x.level) : ""}</div>
    ${meterHTML(t("hw_gpu_load"), x.usage / 100, fmtPct(x.usage), 120)}
    ${x.vramTotal ? meterHTML(t("hw_vram"), x.vramUsed / x.vramTotal, `${bytes(x.vramUsed)} / ${bytes(x.vramTotal)}`, 120) : ""}
    ${x.tempC ? meterHTML(t("hw_temp"), x.tempC / 100, x.tempC + " °C", 120) : `<p class="help" style="margin:6px 0">${t("hw_no_temp")}</p>`}
    ${x.fanPct >= 0 ? meterHTML(t("hw_fan"), x.fanPct / 100, x.fanPct + "%", 120) : ""}
    ${x.powerW ? `<div class="hrow"><span class="hn">${t("hw_power")}</span><span class="hv">${x.powerW.toFixed(0)} ${LANG === "en" ? "W" : "Вт"}</span></div>` : ""}`).join(`<div class="sep"></div>`) : `<p class="muted">${t("hw_no_gpu")}</p>`;
  const b = h.battery || {};
  $("hwBat").innerHTML = b.present ? `<div class="toolbar" style="margin-bottom:6px">${lvDot(b.level)}<b class="grow">${b.percent}%${b.charging ? " · " + t("hw_charging") : ""}</b>${b.wearPct > 0 ? hwBadge(b.level) : ""}</div>
    ${meterHTML(t("hw_charge"), b.percent / 100, b.percent + "%", 120)}
    ${b.wearPct > 0 ? meterHTML(t("hw_wear"), b.wearPct / 100, fmtPct(b.wearPct), 120) : ""}
    ${kvRows([b.designWh ? [t("hw_capacity"), t("hw_capacity_v", Math.round(b.fullWh).toLocaleString(loc()), Math.round(b.designWh).toLocaleString(loc()))] : null,
      b.cycles ? [t("hw_cycles"), String(b.cycles)] : null])}` : `<p class="muted">${t("hw_no_bat")}</p>`;
}

// ---------- Сеть: подключения ----------
let CONNS = null, connsTimer = null;
const connOpen = new Set();
function startConnsAuto() { stopConnsAuto(); if ($("ncAuto").checked) connsTimer = setInterval(() => { if (state.page === "network") loadConns(); }, 5000); }
function stopConnsAuto() { clearInterval(connsTimer); connsTimer = null; }
$("ncAuto").onchange = startConnsAuto;
$("ncReload").onclick = () => loadConns();
$("ncSearch").oninput = () => renderConns();
async function loadConns() {
  if (!CONNS) $("ncList").innerHTML = loadingHTML();
  try { CONNS = await api("net/conns"); } catch (e) { $("ncList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderConns();
}
function renderConns() {
  if (!CONNS) return;
  const q = $("ncSearch").value.trim().toLowerCase();
  const list = CONNS.filter(p => !q || (p.name + " " + p.remotes.join(" ") + " " + p.conns.map(c => c.remote + " " + (c.rhost || "")).join(" ")).toLowerCase().includes(q));
  $("ncStats").textContent = t("nc_stats", CONNS.length, CONNS.reduce((a, p) => a + p.established, 0));
  $("ncList").innerHTML = list.length ? `<div class="items">` + list.map(p => {
    const open = connOpen.has(p.pid);
    return `<div class="item nc-item" data-pid="${p.pid}">
      <span class="state-dot ${p.established ? "on" : ""}"></span>
      <div class="sv-main"><div class="n">${esc(p.name || "PID " + p.pid)} <span class="muted mono-s">PID ${p.pid}</span></div>
        <div class="d">${esc(p.remotes.slice(0, 5).join(", "))}${p.remotes.length > 5 ? " · " + t("nc_more", p.remotes.length - 5) : ""}</div>
        ${open ? `<div class="nc-detail">${p.conns.map(c => `<div><span class="mono-s">${c.proto.toUpperCase()}</span> ${esc(c.local)} → <b>${esc(c.rhost || c.remote)}</b>${c.rhost ? ` <span class="muted">${esc(c.remote)}</span>` : ""} <span class="muted">${esc(c.state)}</span></div>`).join("")}</div>` : ""}
      </div>
      <div class="tags"><span class="tag">${t("nc_conns", p.conns.length)}</span></div></div>`;
  }).join("") + `</div>` : emptyHTML("nothing_found");
  $("ncList").querySelectorAll(".nc-item").forEach(el => el.onclick = () => { const pid = Number(el.dataset.pid); connOpen.has(pid) ? connOpen.delete(pid) : connOpen.add(pid); renderConns(); });
}

// ---------- Сеть: открытые порты ----------
let PORTS = null, FW = {};
$("npReload").onclick = () => loadPorts(true);
$("npLocal").onchange = () => renderPorts();
$("npFw").onclick = () => busy($("npFw"), async () => {
  $("npStats").textContent = t("np_fw_running");
  const v = await api("net/firewall", {});
  FW = {}; v.forEach(x => FW[x.key] = x);
  renderPorts();
});
async function loadPorts(force) {
  if (PORTS && !force) { renderPorts(); return; }
  $("npList").innerHTML = loadingHTML();
  try { PORTS = await api("net/ports"); } catch (e) { $("npList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderPorts();
}
function renderPorts() {
  if (!PORTS) return;
  const ext = PORTS.filter(p => p.exposure !== "local"), loc_ = PORTS.length - ext.length;
  $("npStats").textContent = t("np_stats", ext.length, loc_);
  $("npFw").classList.toggle("hidden", !HOST.diag);
  const list = $("npLocal").checked ? PORTS : ext;
  $("npList").innerHTML = list.length ? `<div class="items">` + list.map(p => {
    const fw = FW[p.key];
    return `<div class="item np-item ${p.risky ? "attn" : ""}">
      <span class="np-port mono-s">${p.proto.toUpperCase()} ${p.port}</span>
      <div class="sv-main"><div class="n">${esc(p.name || (p.pid === 4 ? t("np_system") : "PID " + p.pid))}${p.known ? ` <span class="muted">· ${esc(p.known)}</span>` : ""}</div>
        <div class="d">${esc(p.path || "")}${p.addr ? ` <span class="mono-s">${esc(p.addr)}</span>` : ""}</div></div>
      <div class="tags">${p.risky ? `<span class="tag ${fw && fw.state === "norule" ? "" : "solid"}">${t("np_risky")}</span>` : ""}${fw ? `<span class="tag ${fw.state === "off" ? "solid" : ""}" title="${esc(fw.rule || "")}">${tm("np_fw_state", fw.state)}</span>` : ""}<span class="tag">${tm("np_exp", p.exposure)}</span></div></div>`;
  }).join("") + `</div>` : emptyHTML("nothing_found");
}

// ---------- Дубликаты ----------
let DUP = null, dupTimer = null;
const dupSel = new Set();
function initDupes() {
  if (!$("dupMin").options.length) $("dupMin").innerHTML = [1, 10, 50, 100, 500].map(v => `<option value="${v}">${t("dup_min", bytes(v * 1048576))}</option>`).join("");
  if (!$("dupPath").value) $("dupPath").value = (typeof DISK !== "undefined" && DISK.path) || $("diskPath").value || "";
  pollDupes();
}
$("dupStart").onclick = () => busy($("dupStart"), async () => {
  dupSel.clear();
  await api("disk/dupes", { root: $("dupPath").value.trim(), minMB: Number($("dupMin").value) });
  pollDupes();
});
$("dupPath").addEventListener("keydown", e => { if (e.key === "Enter") $("dupStart").click(); });
$("dupCancel").onclick = () => api("disk/dupes/cancel", {}).catch(() => {});
async function pollDupes() {
  clearTimeout(dupTimer);
  try { DUP = await api("disk/dupes"); } catch { return; }
  $("dupStart").classList.toggle("hidden", DUP.running);
  $("dupCancel").classList.toggle("hidden", !DUP.running);
  if (DUP.running) {
    $("dupStatus").innerHTML = `<span class="spin" style="display:inline-block;vertical-align:-2px"></span> ` + esc(DUP.phase === "hash" ? t("dup_hash", DUP.hashed, DUP.toHash) : t("dup_walk", DUP.files));
    dupTimer = setTimeout(pollDupes, 700);
    return;
  }
  renderDupes();
}
function renderDupes() {
  if (!DUP || !DUP.done) { $("dupStatus").textContent = ""; $("dupList").innerHTML = ""; return; }
  $("dupStatus").textContent = t("dup_done", DUP.groups.length, bytes(DUP.wasted), DUP.files, (DUP.elapsed / 1000).toFixed(0)) + (DUP.canceled ? t("dup_canceled") : "");
  if (!DUP.groups.length) { $("dupList").innerHTML = emptyHTML("dup_none"); return; }
  const can = !polBlocks("disk");
  const selSize = [...dupSel].reduce((a, k) => { const [gi] = k.split(":"); return a + DUP.groups[gi].size; }, 0);
  $("dupList").innerHTML = `<div class="toolbar actionbar" style="margin:10px 0"><button class="btn" id="dupSelExtra" ${can ? "" : "disabled"}>${t("dup_select_extra")}</button><span class="grow muted small">${dupSel.size ? bytes(selSize) : ""}</span><button class="btn btn-accent" id="dupDel" ${dupSel.size && can ? "" : "disabled"}>${t("dup_del_sel", dupSel.size)}</button></div>` +
    DUP.groups.slice(0, 200).map((g, gi) => `<div class="card dup-group"><div class="toolbar" style="margin-bottom:4px"><b class="grow">${esc(g.files[0].path.split(/[\\/]/).pop())}</b><span class="muted small">${t("dup_copies", g.files.length, bytes(g.size))}</span><span class="tag solid">${bytes(g.wasted)}</span></div>
      ${g.files.map((f, fi) => `<label class="dup-file ${f.protected ? "na" : ""}"><input type="checkbox" class="dup-chk" data-k="${gi}:${fi}" ${dupSel.has(gi + ":" + fi) ? "checked" : ""} ${f.protected || !can ? "disabled" : ""}>
        <span class="grow mono-s" title="${esc(f.path)}">${esc(f.path)}</span><span class="muted small">${fmtDate(f.mod)}</span>${fi === 0 ? `<span class="tag">${t("dup_keep")}</span>` : ""}</label>`).join("")}</div>`).join("");
  $("dupList").querySelectorAll(".dup-chk").forEach(c => c.onchange = () => {
    const [gi] = c.dataset.k.split(":");
    if (c.checked) {
      dupSel.add(c.dataset.k);
      const left = DUP.groups[gi].files.filter((_, fi) => !dupSel.has(gi + ":" + fi)).length;
      if (!left) { dupSel.delete(c.dataset.k); toast(t("dup_last"), true); }
    } else dupSel.delete(c.dataset.k);
    renderDupes();
  });
  $("dupSelExtra").onclick = () => { DUP.groups.forEach((g, gi) => g.files.forEach((f, fi) => { if (fi > 0 && !f.protected) dupSel.add(gi + ":" + fi); })); renderDupes(); };
  $("dupDel").onclick = async () => {
    const keys = [...dupSel];
    if (!await confirmRun(t("dk_dupes"), t("dup_confirm", keys.length, bytes(selSize)), t("dup_del_extra"), true)) return;
    let ok = 0;
    await busy($("dupDel"), async () => {
      for (const k of keys) {
        const [gi, fi] = k.split(":").map(Number);
        const f = DUP.groups[gi].files[fi];
        try { await api("disk/recycle", { path: f.path }); f.gone = true; ok++; } catch (e) { toast(`${f.path}: ${e.message}`, true); }
      }
    });
    dupSel.clear();
    DUP.groups.forEach(g => { g.files = g.files.filter(f => !f.gone); g.wasted = g.size * (g.files.length - 1); });
    DUP.groups = DUP.groups.filter(g => g.files.length > 1);
    DUP.wasted = DUP.groups.reduce((a, g) => a + g.wasted, 0);
    toast(t("dup_deleted", ok));
    renderDupes();
  };
}

// ---------- Обновления Windows ----------
let WU = null, wuTimer = null, WU_HIST = null;
const wuSel = new Set();
async function loadWu(force) {
  try { WU = await api("wu"); } catch (e) { toast(e.message, true); return; }
  $("wuUnsupported").classList.toggle("hidden", WU.supported);
  $("wuMain").classList.toggle("hidden", !WU.supported);
  if (!WU.supported) return;
  if (!$("wuPauseDays").options.length) $("wuPauseDays").innerHTML = [7, 14, 21, 35].map(d => `<option value="${d}">${t("wu_days", d)}</option>`).join("");
  const p = WU.pause, admin = HOST.elevated && !polBlocks("write");
  $("wuPauseState").textContent = (p.paused ? t("wu_paused_until", fmtDate(p.until, false)) : t("wu_active")) + (HOST.elevated ? "" : " " + t("wu_need_admin"));
  $("wuPause").classList.toggle("hidden", p.paused); $("wuPauseDays").classList.toggle("hidden", p.paused);
  $("wuResume").classList.toggle("hidden", !p.paused);
  [$("wuPause"), $("wuResume")].forEach(b => b.disabled = !admin);
  renderWuList();
  if (WU.search.running) { clearTimeout(wuTimer); wuTimer = setTimeout(() => { if (state.page === "wu") loadWu(); }, 3000); }
}
function renderWuList() {
  const s = WU.search, admin = HOST.elevated && !polBlocks("write");
  $("wuCheck").disabled = s.running;
  if (s.running) { $("wuList").innerHTML = loadingHTML("wu_checking"); $("wuStats").textContent = ""; $("wuInstall").disabled = true; $("wuInstall").textContent = t("wu_install", 0); return; }
  if (s.err) { $("wuList").innerHTML = `<div class="empty">${esc(s.err)}</div>`; return; }
  if (!s.done) { $("wuList").innerHTML = `<div class="muted" style="padding:16px 4px">${t("wu_intro")}</div>`; $("wuInstall").textContent = t("wu_install", 0); return; }
  $("wuStats").textContent = t("wu_stats", s.items.length, dayHM(s.checked));
  [...wuSel].forEach(id => { if (!s.items.some(u => u.id === id)) wuSel.delete(id); });
  $("wuList").innerHTML = s.items.length ? `<div class="items">` + s.items.map(u => `<label class="item wu-item">
      <input type="checkbox" class="wu-chk" data-id="${esc(u.id)}" ${wuSel.has(u.id) ? "checked" : ""} ${admin ? "" : "disabled"}>
      <div class="sv-main"><div class="n">${esc(u.title)}</div><div class="d">${esc([u.kb ? "KB" + u.kb : "", u.cat, u.size ? bytes(u.size) : ""].filter(Boolean).join(" · "))}</div></div>
      <div class="tags">${u.sev ? `<span class="tag solid">${esc(u.sev)}</span>` : ""}${u.driver ? `<span class="tag">${t("wu_driver")}</span>` : ""}${u.optional ? `<span class="tag">${t("wu_optional")}</span>` : ""}${u.reboot ? `<span class="tag">${t("wu_reboot")}</span>` : ""}</div></label>`).join("") + `</div>` : emptyHTML("wu_none");
  $("wuList").querySelectorAll(".wu-chk").forEach(c => c.onchange = () => { c.checked ? wuSel.add(c.dataset.id) : wuSel.delete(c.dataset.id); updWuBtn(); });
  updWuBtn();
}
function updWuBtn() { $("wuInstall").textContent = t("wu_install", wuSel.size); $("wuInstall").disabled = !wuSel.size || !HOST.elevated; }
$("wuCheck").onclick = async () => { try { await api("wu/search", {}); } catch (e) { toast(e.message, true); } loadWu(); };
$("wuInstall").onclick = async () => {
  const items = WU.search.items.filter(u => wuSel.has(u.id));
  try {
    await api("wu/install", { ids: items.map(u => u.id), titles: items.map(u => u.title) });
    showJobModal(t("nav_wu"), () => { wuSel.clear(); loadWu(); WU_HIST = null; });
    pollMaintJob();
  } catch (e) { toast(e.message, true); }
};
$("wuPause").onclick = () => busy($("wuPause"), async () => { await api("wu/pause", { days: Number($("wuPauseDays").value) }); toast(t("wu_paused_ok")); loadWu(); });
$("wuResume").onclick = () => busy($("wuResume"), async () => { await api("wu/pause", { days: 0 }); toast(t("wu_resumed_ok")); loadWu(); });
$("wuOpen").onclick = () => api("open-settings", { page: "windowsupdate" }).catch(e => toast(e.message, true));
$("wuHistReload").onclick = () => loadWuHist(true);
$("wuFailedOnly").onchange = () => renderWuHist();
async function loadWuHist(force) {
  if (WU_HIST && !force) { renderWuHist(); return; }
  $("wuHist").innerHTML = loadingHTML();
  try { WU_HIST = await api("wu/history"); } catch (e) { $("wuHist").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderWuHist();
}
function renderWuHist() {
  if (!WU_HIST) return;
  const list = $("wuFailedOnly").checked ? WU_HIST.filter(h => h.result >= 3) : WU_HIST;
  $("wuHist").innerHTML = list.length ? `<div class="items">` + list.map(h => {
    const lvl = h.result === 2 ? 0 : h.result === 3 ? 1 : 2;
    return `<div class="item">${lvDot(lvl)}<div class="sv-main"><div class="n">${esc(h.title)}</div>
      <div class="d">${fmtDate(h.date)}${h.op === 2 ? " · " + t("wu_uninstall") : ""}${h.result >= 3 ? ` · <span class="mono-s">${esc(h.hr)}</span>` : ""}</div>
      ${h.hint ? `<div class="c">${esc(h.hint)}</div>` : ""}</div>
      <div class="tags"><span class="tag ${lvl === 2 ? "solid" : ""}">${tm("wu_result", h.result)}</span></div></div>`;
  }).join("") + `</div>` : emptyHTML("wu_hist_empty");
}

// ---------- Снимки системы ----------
let SNAPS = [], SNAP_DIFF = null, snapPick = null, snapArea = "";
async function loadSnaps() {
  try { SNAPS = await api("snapshots"); } catch (e) { toast(e.message, true); return; }
  renderSnaps();
}
function renderSnaps() {
  $("snList").innerHTML = SNAPS.length ? `<div class="items">` + SNAPS.map(s => {
    const c = s.counts || {};
    const picking = snapPick && snapPick !== s.id;
    return `<div class="item sn-item ${snapPick === s.id ? "attn" : ""}"><span class="state-dot on"></span>
      <div class="sv-main"><div class="n">${esc(s.name)}</div><div class="d">${dayHM(s.time)} · ${t("sn_counts", c.programs || 0, c.autostart || 0, c.services || 0, c.tasks || 0, c.drivers || 0)}</div></div>
      <div class="tags">${picking ? `<button class="btn btn-accent sn-with" data-id="${s.id}">${t("sn_vs")}</button>` :
        `<button class="btn btn-accent sn-now" data-id="${s.id}">${t("sn_vs_now")}</button><button class="btn sn-pick" data-id="${s.id}">${t("sn_vs")}</button>
         <button class="btn sn-ren" data-id="${s.id}">${t("sn_rename")}</button><button class="icon-btn sn-del" data-id="${s.id}" title="${t("delete")}">✕</button>`}</div></div>`;
  }).join("") + `</div>` + (snapPick ? `<p class="help">${t("sn_pick")}</p>` : "") : emptyHTML("sn_empty");
  const byId = id => SNAPS.find(s => s.id === id);
  $("snList").querySelectorAll(".sn-now").forEach(b => b.onclick = () => snapCompare(b.dataset.id, "now", b));
  $("snList").querySelectorAll(".sn-pick").forEach(b => b.onclick = () => { snapPick = snapPick === b.dataset.id ? null : b.dataset.id; renderSnaps(); });
  $("snList").querySelectorAll(".sn-with").forEach(b => b.onclick = () => { const a = snapPick; snapPick = null; snapCompare(a, b.dataset.id, b); });
  $("snList").querySelectorAll(".sn-del").forEach(b => b.onclick = async () => {
    if (!await confirmRun(t("nav_snaps"), t("sn_del_q", byId(b.dataset.id).name), t("delete"), true)) return;
    try { await api("snapshots/delete", { id: b.dataset.id }); loadSnaps(); } catch (e) { toast(e.message, true); }
  });
  $("snList").querySelectorAll(".sn-ren").forEach(b => b.onclick = async () => {
    const inp = document.createElement("input"); inp.className = "input"; inp.style.width = "100%"; inp.value = byId(b.dataset.id).name; inp.maxLength = 80;
    if (!await modal({ title: t("sn_rename_q"), node: inp })) return;
    try { await api("snapshots/rename", { id: b.dataset.id, name: inp.value }); loadSnaps(); } catch (e) { toast(e.message, true); }
  });
}
$("snTake").onclick = () => busy($("snTake"), async () => {
  toast(t("sn_taking"));
  await api("snapshots", { name: $("snName").value.trim() });
  $("snName").value = "";
  await loadSnaps();
});
async function snapCompare(a, b, btn) {
  $("snDiffCard").classList.remove("hidden");
  $("snDiff").innerHTML = loadingHTML(b === "now" ? "sn_comparing" : "loading");
  $("snDiffAreas").innerHTML = ""; $("snDiffTitle").textContent = t("nav_snaps");
  const run = async () => { SNAP_DIFF = await api("snapshots/diff", { a, b }); snapArea = ""; renderSnapDiff(); };
  try { btn ? await busy(btn, run) : await run(); } catch (e) { $("snDiff").innerHTML = `<div class="empty">${esc(e.message)}</div>`; }
  $("snDiffCard").scrollIntoView({ behavior: "smooth", block: "start" });
}
$("snDiffClose").onclick = () => { $("snDiffCard").classList.add("hidden"); SNAP_DIFF = null; };
function renderSnapDiff() {
  const d = SNAP_DIFF;
  if (!d) return;
  $("snDiffTitle").textContent = t("sn_diff_title", d.a.name, d.b.id === "now" ? t("sn_now") : d.b.name);
  const total = d.changes.length, areas = Object.keys(d.summary);
  $("snDiffAreas").innerHTML = [`<button class="btn ${snapArea === "" ? "btn-accent" : ""} sn-area" data-a="">${t("sn_all", total)}</button>`]
    .concat(areas.map(a => `<button class="btn ${snapArea === a ? "btn-accent" : ""} sn-area" data-a="${a}">${tm("sn_area", a)} (${d.summary[a]})</button>`)).join("");
  $("snDiffAreas").querySelectorAll(".sn-area").forEach(b => b.onclick = () => { snapArea = b.dataset.a; renderSnapDiff(); });
  const list = d.changes.filter(c => !snapArea || c.area === snapArea);
  const sym = { added: "+", removed: "−", changed: "~" };
  const words = t("sn_words");
  const human = v => String(v || "").replace(/^(on|off|auto|delayed|manual|disabled|boot|system|all|iface|local)(?= · |$)/, w => words[w] || w);
  $("snDiff").innerHTML = list.length ? `<div class="items">` + list.map(c => `<div class="item sn-ch ${c.kind}">
      <span class="sn-sym">${sym[c.kind]}</span>
      <div class="sv-main"><div class="n">${esc(c.key)}</div>
        <div class="d mono">${c.kind === "changed" ? `${esc(human(c.before))} → ${esc(human(c.after))}` : esc(human(c.after || c.before))}</div></div>
      <div class="tags"><span class="tag">${tm("sn_area", c.area)}</span><span class="tag ${c.kind === "added" ? "solid" : ""}">${tm("sn_kind", c.kind)}</span></div></div>`).join("") + `</div>` : emptyHTML("sn_no_changes");
}

// ---------- Инвентаризация ----------
async function saveInventory(fmt, btn) {
  await busy(btn, async () => { const p = await api("inventory", { format: fmt }); toast(t("inv_saved", p)); });
}
$("invJson").onclick = () => saveInventory("json", $("invJson"));
$("invCsv").onclick = () => saveInventory("csv", $("invCsv"));

// ---------- Настройки оповещений ----------
function renderAlertSettings() {
  const a = CFG.alerts || {};
  $("alOn").checked = !!a.enabled;
  $("alDisk").value = a.disk_free_pct || 0; $("alDiskTemp").value = a.disk_temp || 0;
  $("alGpuTemp").value = a.gpu_temp || 0; $("alMem").value = a.mem_pct || 0;
  $("alHealth").checked = !!a.disk_health; $("alBattery").checked = !!a.battery; $("alReboot").checked = !!a.reboot;
  $("alSvcList").innerHTML = (a.services || []).map((s, i) => `<span class="chip">${esc(s)}<button data-i="${i}" title="${t("delete")}">✕</button></span>`).join("") || `<span class="muted small">${t("al_svc_none")}</span>`;
  $("alSvcList").querySelectorAll("button").forEach(b => b.onclick = () => { const l = (CFG.alerts.services || []).slice(); l.splice(Number(b.dataset.i), 1); saveAlerts({ services: l }); });
  document.querySelectorAll(".al-form input, #alHealth, #alBattery, #alReboot, #alSvcInput, #alSvcAdd").forEach(el => el.disabled = !a.enabled);
  if (!$("lhDays").options.length) $("lhDays").innerHTML = [0, 1, 3, 7, 14, 31].map(d => `<option value="${d}">${d ? t("lh_days_opt", d) : t("lh_off")}</option>`).join("");
  $("lhDays").value = CFG.load_history_days;
  if (HOST.sysadmin && !$("alSvcNames").options.length && (CFG.alerts || {}).enabled) api("services").then(sv => { $("alSvcNames").innerHTML = sv.filter(s => !s.windows || s.start === "auto").map(s => `<option value="${esc(s.name)}">${esc(s.display)}</option>`).join(""); }).catch(() => {});
}
async function saveAlerts(patch, quiet) {
  const a = Object.assign({}, CFG.alerts, patch);
  await setCfg({ alerts: a });
  renderAlertSettings();
  if (!quiet) toast(t("al_saved"));
  api("alerts/check", {}).then(() => loadAlerts()).catch(() => {});
}
$("alOn").onchange = () => saveAlerts({ enabled: $("alOn").checked });
[["alDisk", "disk_free_pct"], ["alDiskTemp", "disk_temp"], ["alGpuTemp", "gpu_temp"], ["alMem", "mem_pct"]].forEach(([id, k]) => $(id).onchange = () => saveAlerts({ [k]: Math.max(0, Math.round(Number($(id).value) || 0)) }));
[["alHealth", "disk_health"], ["alBattery", "battery"], ["alReboot", "reboot"]].forEach(([id, k]) => $(id).onchange = () => saveAlerts({ [k]: $(id).checked }));
function addWatchedService() {
  const v = $("alSvcInput").value.trim();
  if (!v || !/^[\w.\- ]{1,80}$/.test(v)) return;
  const l = (CFG.alerts.services || []).slice();
  if (!l.some(x => x.toLowerCase() === v.toLowerCase())) l.push(v);
  $("alSvcInput").value = "";
  saveAlerts({ services: l });
}
$("alSvcAdd").onclick = addWatchedService;
$("alSvcInput").addEventListener("keydown", e => { if (e.key === "Enter") addWatchedService(); });
$("lhDays").onchange = async () => { await setCfg({ load_history_days: Number($("lhDays").value) }); toast(t("restart_to_apply")); };

// ---------- Запуск ----------
function init3() {
  $("navWu").classList.toggle("hidden", !HOST.diag);
  $("navHw").classList.toggle("hidden", !HOST.health);
  $("navSnaps").classList.toggle("hidden", !HOST.sysadmin && !HOST.programs);
  lhRangeLabels();
  renderAlertSettings();
  tidyNav();
  loadAlerts();
  setInterval(loadAlerts, 60 * 1000);
}
