"use strict";
// Разделы версии 1.8: проверка ПК, расписание, место на диске, программы,
// службы и задачи, сеть, сбои, отчёт и защита, история, палитра команд, политики.

// ---------- Общее ----------
const lvDot = l => `<span class="lv lv${l < 0 ? "x" : l}"></span>`;
const loc = () => LANG === "en" ? "en-GB" : "ru-RU";
function fmtDate(s, withTime = true) {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d)) return String(s);
  return d.toLocaleString(loc(), withTime ? { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" } : { day: "2-digit", month: "2-digit", year: "numeric" });
}
const tm = (k, sub) => (t(k) || {})[sub] ?? sub;
const loadingHTML = key => `<div class="loading">${esc(t(key || "loading"))}</div>`;
const emptyHTML = key => `<div class="empty">${esc(t(key))}</div>`;
function pol() { return HOST.policy || {}; }
function polBlocks(rule) {
  const p = pol();
  if (!p.managed) return false;
  if (p.readOnly) return true;
  return !!{ tweaks: p.disableTweaks, registry: p.disableRegistry, programs: p.disablePrograms, services: p.disableServices, disk: p.disableDisk, drivers: p.disableDrivers }[rule];
}
async function confirmRun(title, body, ok, danger) { return modal({ title, body, ok, danger }); }

const loaded = {};
function onPageShow(name, force) {
  const f = {
    disk: () => loadDisk(force), programs: () => loadPrograms(force), services: () => loadServices(force),
    network: () => loadAdapters(force), crashes: () => loadCrashes(force), report: () => loadReportPage(force),
    history: () => loadHistory(), optimize: () => renderSchedule(), autostart: () => loadBootMini(force),
  }[name];
  if (f) f();
  if (force && name === "drivers") loadDrivers(true);
  if (force && name === "autostart") loadAutostart(true);
  if (force && name === "tweaks") loadTweaks(true);
}
function onSubShow(sub) {
  if (sub === "sv-tasks") loadTasks(false);
  if (sub === "pr-updates" && !UPGRADES) $("upList").innerHTML = `<div class="muted" style="padding:16px 4px">${t("pr_upd_intro")}</div>`;
}

// ---------- Ожидающая перезагрузка ----------
async function loadReboot() {
  let r; try { r = await api("reboot"); } catch { return; }
  $("rebootBanner").classList.toggle("hidden", !r.pending);
  $("rebootReasons").textContent = (r.reasons || []).join(" · ");
}

// ---------- Проверка ПК ----------
const CHECK_PAGES = { optimize: 1, disk: 1, crashes: 1, drivers: 1, autostart: 1, tweaks: 1, maintenance: 1, report: 1, overview: 1 };
async function runCheckup() {
  const btn = $("checkupBtn");
  $("checkupList").innerHTML = loadingHTML("checkup_running");
  await busy(btn, async () => {
    const r = await api("checkup", {});
    $("checkupSub").textContent = t("checkup_res", r.bad, r.warn, (r.elapsed / 1000).toFixed(0));
    $("checkupList").innerHTML = `<div class="check-list">` + r.items.map((it, i) => `<div class="check-row l${it.level}">
      ${lvDot(it.level)}<div class="grow"><div class="n">${esc(it.title)}</div>${it.detail ? `<div class="d">${esc(it.detail)}</div>` : ""}</div>
      <div class="tags">${it.action ? `<button class="btn btn-accent ck-act" data-i="${i}">${t(it.action === "quickclean" ? "act_quickclean" : "free_mem")}</button>` : ""}
      ${it.page && CHECK_PAGES[it.page] && !navHidden(it.page) ? `<button class="btn ck-go" data-p="${it.page}">${t("open_page")}</button>` : ""}</div></div>`).join("") + `</div>`;
    $("checkupList").querySelectorAll(".ck-go").forEach(b => b.onclick = () => showPage(b.dataset.p));
    $("checkupList").querySelectorAll(".ck-act").forEach(b => b.onclick = () => busy(b, async () => {
      const it = r.items[b.dataset.i];
      if (it.action === "quickclean") { const x = await api("quickclean", {}); toast(t("cleaned", bytes(x.freed))); }
      else toast(await api("opt/trim", {}));
      b.closest(".check-row").classList.add("fixed"); b.remove();
    }));
  });
}
$("checkupBtn").onclick = runCheckup;

// ---------- Расписание ----------
const SCHED_CATS = ["user_temp", "win_temp", "browser_cache", "wer", "thumbs", "wu_download", "recycle"];
const SCHED_ADMIN = { win_temp: 1, wu_download: 1 };
function renderSchedule() {
  const s = CFG.schedule || { day: 0, hour: 19, cats: [] };
  $("schedOn").checked = !!s.enabled;
  $("schedDay").innerHTML = t("sched_days").map((d, i) => `<option value="${i}">${d}</option>`).join("");
  $("schedDay").value = s.day;
  $("schedHour").innerHTML = Array.from({ length: 24 }, (_, h) => `<option value="${h}">${String(h).padStart(2, "0")}:00</option>`).join("");
  $("schedHour").value = s.hour;
  const cats = new Set(s.cats || []);
  $("schedCats").innerHTML = SCHED_CATS.map(c => `<label class="check"><input type="checkbox" value="${c}" ${cats.has(c) ? "checked" : ""}><span>${t("sc_" + c)}${SCHED_ADMIN[c] ? ` <span class="muted small">(${t("needs_admin")})</span>` : ""}</span></label>`).join("");
  $("schedLast").textContent = s.last_run ? t("schedule_last", s.last_run) : t("schedule_never");
  const save = () => setCfg({ schedule: { enabled: $("schedOn").checked, day: Number($("schedDay").value), hour: Number($("schedHour").value),
    cats: [...$("schedCats").querySelectorAll("input:checked")].map(c => c.value) } });
  $("schedOn").onchange = $("schedDay").onchange = $("schedHour").onchange = save;
  $("schedCats").querySelectorAll("input").forEach(c => c.onchange = save);
}
$("schedRun").onclick = () => busy($("schedRun"), async () => {
  const r = await api("schedule/run", {});
  toast(t("sched_done", bytes(r.freed)));
  CFG = await api("config"); renderSchedule();
});

// ---------- Место на диске ----------
const DISK = { cats: null, scan: null, poll: null, path: "" };
async function loadDisk(force) {
  api("disk/drives").then(ds => {
    $("diskDrives").innerHTML = ds.map(d => {
      const total = d.files, used = d.size, pct = total ? used / total : 0;
      return `<div class="card drive"><div class="toolbar"><b class="grow" title="${esc(d.path)}">${esc(d.name)}</b><button class="btn drive-scan" data-p="${esc(d.path)}">${t("analyze")}</button></div>
        <div class="bar big-bar"><i style="width:${(pct * 100).toFixed(1)}%;background:${shade(pct)}"></i></div>
        <div class="muted small" style="margin-top:6px">${t("disk_free", bytes(total - used), bytes(total))} · ${(pct * 100).toFixed(0)}%</div></div>`;
    }).join("");
    $("diskDrives").querySelectorAll(".drive-scan").forEach(b => b.onclick = () => startDiskScan(b.dataset.p));
    if (!DISK.path && ds[0]) { DISK.path = ds[0].path; $("diskPath").value = DISK.path; }
  }).catch(() => {});
  if (!DISK.cats || force) {
    $("junkList").innerHTML = loadingHTML();
    try { DISK.cats = await api("disk/categories"); } catch (e) { $("junkList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  }
  renderJunk();
  if (!DISK.scan) pollDiskScan(true);
}
function junkCleanable(c) { return c.clean && c.size > 0 && (!c.admin || HOST.elevated); }
function renderJunk() {
  const cats = DISK.cats || [];
  const safe = cats.filter(c => junkCleanable(c) && c.id !== "recycle");
  const total = cats.filter(junkCleanable).reduce((a, c) => a + c.size, 0);
  const safeBytes = safe.reduce((a, c) => a + c.size, 0);
  $("junkTotal").textContent = total ? t("junk_total", bytes(total)) : "";
  $("junkCleanAll").textContent = t("junk_clean_all", bytes(safeBytes));
  $("junkCleanAll").disabled = !safe.length || polBlocks("disk");
  const max = Math.max(1, ...cats.map(c => c.size));
  $("junkList").innerHTML = `<div class="items">` + cats.map(c => {
    let btn = "";
    if (c.clean) btn = `<button class="btn junk-clean" data-id="${c.id}" ${junkCleanable(c) && !polBlocks("disk") ? "" : "disabled"}>${t("clean_btn")}</button>`;
    else if (c.page === "open" && c.paths.length) btn = `<button class="btn junk-open" data-p="${esc(c.paths[0])}">${t("open_btn")}</button>`;
    else if (c.page === "cleanmgr" && c.size) btn = `<button class="btn junk-open" data-p="cleanmgr">${t("cleanmgr")}</button>`;
    else if (c.page === "tweaks" && c.size && HOST.tweaks) btn = `<button class="btn junk-tw">${t("to_tweaks")}</button>`;
    return `<div class="item junk-item">
      <div class="junk-size"><b>${c.size ? bytes(c.size) : `<span class="muted">${t("junk_empty")}</span>`}</b><div class="bar"><i style="width:${(c.size / max * 100).toFixed(1)}%;background:${shade(c.size / max)}"></i></div></div>
      <div><div class="n">${esc(c.title)}</div><div class="d">${esc(c.desc)}</div>${c.note ? `<div class="c">${esc(c.note)}</div>` : ""}</div>
      <div class="tags">${c.admin && !HOST.elevated ? `<span class="tag">${t("needs_admin")}</span>` : ""}${btn}</div></div>`;
  }).join("") + `</div>`;
  $("junkList").querySelectorAll(".junk-clean").forEach(b => b.onclick = () => cleanCat(cats.find(c => c.id === b.dataset.id), b));
  $("junkList").querySelectorAll(".junk-open").forEach(b => b.onclick = () => api("disk/reveal", { path: b.dataset.p }).catch(e => toast(e.message, true)));
  $("junkList").querySelectorAll(".junk-tw").forEach(b => b.onclick = () => { showPage("tweaks"); showSub("settings"); $("twSetSearch").value = LANG === "en" ? "hibernat" : "гибернац"; renderSettings && SETTINGS && renderSettings(); });
}
async function cleanCat(c, btn) {
  const q = c.id === "recycle" ? t("clean_recycle_q", bytes(c.size)) : t("clean_q", c.title, bytes(c.size));
  if (!await confirmRun(t("junk"), q, t("clean_btn"), c.id === "recycle")) return;
  await busy(btn, async () => {
    const r = await api("disk/clean", { id: c.id, title: c.title });
    toast(t("cleaned", bytes(r.freed)));
  });
  loadDisk(true);
}
$("junkReload").onclick = () => loadDisk(true);
$("junkCleanAll").onclick = async () => {
  const safe = (DISK.cats || []).filter(c => junkCleanable(c) && c.id !== "recycle");
  if (!await confirmRun(t("junk"), t("junk_all_q", safe.map(c => `• ${c.title} — ${bytes(c.size)}`).join("\n")), t("clean_btn"))) return;
  await busy($("junkCleanAll"), async () => {
    let freed = 0;
    for (const c of safe) { try { freed += (await api("disk/clean", { id: c.id, title: c.title })).freed; } catch (e) { toast(`${c.title}: ${e.message}`, true); } }
    toast(t("cleaned", bytes(freed)));
  });
  loadDisk(true);
};
async function startDiskScan(p) {
  p = (p || $("diskPath").value).trim();
  if (!p) return;
  DISK.path = p; $("diskPath").value = p;
  try { await api("disk/scan", { root: p }); } catch (e) { return toast(e.message, true); }
  pollDiskScan();
}
async function pollDiskScan(once) {
  clearTimeout(DISK.poll);
  let s; try { s = await api("disk/scan"); } catch { return; }
  $("diskCancel").classList.toggle("hidden", !s.running);
  $("diskScanBtn").disabled = s.running;
  if (s.running) {
    $("diskStatus").textContent = t("disk_scanning", s.files.toLocaleString(loc()), bytes(s.bytes));
    DISK.poll = setTimeout(pollDiskScan, 600);
    return;
  }
  if (!s.done) return;
  DISK.scan = s;
  renderDiskScan();
}
function crumbs(root) {
  const win = root.includes("\\") || /^[A-Za-z]:/.test(root);
  const sep = win ? "\\" : "/";
  const parts = root.split(/[\\/]+/).filter(Boolean);
  const out = [];
  let acc = win ? "" : "/";
  parts.forEach((p, i) => {
    acc = win ? (i === 0 ? p + "\\" : acc.replace(/\\$/, "") + "\\" + p) : (acc === "/" ? "/" + p : acc + "/" + p);
    out.push([p, acc]);
  });
  if (!win) out.unshift(["/", "/"]);
  return out.map(([n, path]) => `<button class="crumb" data-p="${esc(path)}">${esc(n)}</button>`).join(`<span class="muted">${sep === "\\" ? "›" : "/"}</span>`);
}
function renderDiskScan() {
  const s = DISK.scan;
  $("diskCrumbs").innerHTML = crumbs(s.root);
  $("diskCrumbs").querySelectorAll(".crumb").forEach(b => b.onclick = () => startDiskScan(b.dataset.p));
  $("diskStatus").textContent = s.err ? s.err : t("disk_done", bytes(s.bytes), s.files.toLocaleString(loc()), (s.elapsed / 1000).toFixed(1)) + (s.skipped ? t("disk_skipped", s.skipped) : "");
  const max = Math.max(1, ...(s.folders || []).map(f => f.size));
  const canDel = !polBlocks("disk");
  $("diskFolders").innerHTML = (s.folders || []).slice(0, 60).map(f => `<div class="item fs-item">
    <div class="fs-icon">${f.dir ? "▸" : "·"}</div>
    <div class="grow fs-main"><div class="n ${f.dir ? "fs-dir" : ""}" ${f.dir ? `data-p="${esc(f.path)}"` : ""} title="${esc(f.path)}">${esc(f.name)}</div>
      <div class="bar"><i style="width:${(f.size / max * 100).toFixed(1)}%;background:${shade(f.size / max)}"></i></div></div>
    <div class="tags"><span class="fs-size">${bytes(f.size)}</span>${f.protected ? `<span class="tag">${t("protected")}</span>` : ""}
      <button class="icon-btn fs-reveal" data-p="${esc(f.path)}" title="${t("reveal")}">↗</button>
      ${!f.protected && canDel ? `<button class="icon-btn fs-del" data-p="${esc(f.path)}" title="${t("recycle")}">✕</button>` : ""}</div></div>`).join("") || emptyHTML("nothing_found");
  $("diskFiles").innerHTML = (s.largest || []).slice(0, 40).map(f => `<div class="item fs-item">
    <div class="fs-icon">·</div>
    <div class="grow fs-main"><div class="n" title="${esc(f.path)}">${esc(f.name)}</div><div class="d mono" title="${esc(f.path)}">${esc(f.path.slice(0, f.path.length - f.name.length))} · ${fmtDate(f.mod, false)}</div></div>
    <div class="tags"><span class="fs-size">${bytes(f.size)}</span>${f.protected ? `<span class="tag">${t("protected")}</span>` : ""}
      <button class="icon-btn fs-reveal" data-p="${esc(f.path)}" title="${t("reveal")}">↗</button>
      ${!f.protected && canDel ? `<button class="icon-btn fs-del" data-p="${esc(f.path)}" title="${t("recycle")}">✕</button>` : ""}</div></div>`).join("") || emptyHTML("nothing_found");
  document.querySelectorAll("#page-disk .fs-dir").forEach(n => n.onclick = () => startDiskScan(n.dataset.p));
  document.querySelectorAll("#page-disk .fs-reveal").forEach(b => b.onclick = () => api("disk/reveal", { path: b.dataset.p }).catch(e => toast(e.message, true)));
  document.querySelectorAll("#page-disk .fs-del").forEach(b => b.onclick = () => recyclePath(b.dataset.p));
}
async function recyclePath(p) {
  if (!await confirmRun(t("recycle"), t("recycle_q", p), t("recycle"), true)) return;
  try {
    await api("disk/recycle", { path: p });
    toast(t("recycled"));
    const s = DISK.scan;
    s.folders = (s.folders || []).filter(f => f.path !== p);
    s.largest = (s.largest || []).filter(f => f.path !== p && !f.path.startsWith(p + "\\") && !f.path.startsWith(p + "/"));
    renderDiskScan();
  } catch (e) { toast(e.message, true); }
}
$("diskScanBtn").onclick = () => startDiskScan();
$("diskPath").addEventListener("keydown", e => { if (e.key === "Enter") startDiskScan(); });
$("diskCancel").onclick = () => api("disk/cancel", {});

// ---------- Программы ----------
let PROGS = null, UPGRADES = null, WINGET = false;
const prSel = new Set(), upSel = new Set();
async function loadPrograms(force) {
  if (!HOST.programs || (PROGS && !force)) { if (PROGS) renderPrograms(); return; }
  $("prList").innerHTML = loadingHTML();
  try { const r = await api("programs"); PROGS = r.programs; WINGET = r.winget; } catch (e) { $("prList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  [...prSel].forEach(id => { if (!PROGS.some(p => p.id === id)) prSel.delete(id); });
  renderPrograms();
}
function renderPrograms() {
  const q = $("prSearch").value.trim().toLowerCase();
  const sort = Number($("prSort").value || 0);
  let list = PROGS.filter(p => !q || (p.name + " " + p.publisher).toLowerCase().includes(q));
  list.sort(sort === 1 ? (a, b) => b.size - a.size : sort === 2 ? (a, b) => (b.installDate || "").localeCompare(a.installDate || "") : (a, b) => a.name.localeCompare(b.name, loc()));
  $("prStats").textContent = t("pr_stats", PROGS.length, bytes(PROGS.reduce((a, p) => a + (p.size || 0), 0)));
  const can = !polBlocks("programs");
  $("prList").innerHTML = list.length ? `<div class="items">` + list.map(p => `<div class="item pr-item">
    <input type="checkbox" class="pr-chk" data-id="${esc(p.id)}" ${prSel.has(p.id) ? "checked" : ""} ${p.quiet && can ? "" : "disabled"} title="${p.quiet ? t("pr_silent") : ""}">
    <div class="pr-main"><div class="avatar">${esc((p.name.match(/[\p{L}\p{N}]/u) || ["?"])[0].toUpperCase())}</div>
      <div><div class="n">${esc(p.name)}</div><div class="d">${esc([p.publisher, p.version && "v" + p.version, p.installDate && fmtDate(p.installDate, false), p.size ? bytes(p.size) : "", p.scope === "machine" ? t("pr_all_users") : t("pr_user")].filter(Boolean).join(" · "))}</div></div></div>
    <div class="tags">${can ? `<button class="btn pr-un" data-id="${esc(p.id)}">${t("pr_uninstall")}</button>` : ""}</div></div>`).join("") + `</div>` : emptyHTML("nothing_found");
  $("prList").querySelectorAll(".pr-chk").forEach(c => c.onchange = () => { c.checked ? prSel.add(c.dataset.id) : prSel.delete(c.dataset.id); updPrQuiet(); });
  $("prList").querySelectorAll(".pr-un").forEach(b => b.onclick = () => { const p = PROGS.find(x => x.id === b.dataset.id); uninstallPrograms([p], false); });
  updPrQuiet();
}
function updPrQuiet() { $("prQuiet").textContent = t("pr_quiet_n", prSel.size); $("prQuiet").disabled = !prSel.size; }
async function uninstallPrograms(list, quiet) {
  const body = quiet ? t("pr_uninstall_quiet_q", list.length, list.map(p => "• " + p.name).join("\n")) : t("pr_uninstall_q", list[0].name);
  if (!await confirmRun(t("pr_uninstall"), body, t("pr_uninstall"), true)) return;
  try { await api("programs/uninstall", { ids: list.map(p => p.id), quiet }); } catch (e) { return toast(e.message, true); }
  showJobModal(list.length === 1 ? list[0].name : t("pr_uninstall"), async () => {
    prSel.clear();
    await loadPrograms(true);
    const gone = list.filter(p => !PROGS.some(x => x.id === p.id));
    if (gone.length === 1) offerLeftovers(gone[0]);
  });
  pollMaintJob();
}
async function offerLeftovers(p) {
  let l; try { l = await api("programs/leftovers?id=" + encodeURIComponent(p.id)); } catch (e) { return; }
  if (!l.length) { toast(t("pr_leftovers_none")); return; }
  const box = document.createElement("div"); box.className = "list";
  l.forEach(x => box.insertAdjacentHTML("beforeend", `<label class="check"><input type="checkbox" value="${esc(x.id)}" checked><span class="mono-s">${esc(x.path)}${x.size ? " — " + bytes(x.size) : ""}</span></label>`));
  if (!await modal({ title: t("pr_leftovers_title", p.name), body: t("pr_leftovers_hint"), ok: t("pr_leftovers_do"), node: box, danger: true })) return;
  const ids = [...box.querySelectorAll("input:checked")].map(c => c.value);
  try {
    const r = await api("programs/leftovers/remove", { ids, name: p.name });
    toast(t("pr_leftovers_done", r.removed));
    (r.failed || []).forEach(f => toast(f, true));
  } catch (e) { toast(e.message, true); }
}
$("prSearch").oninput = () => PROGS && renderPrograms();
$("prSort").onchange = () => PROGS && renderPrograms();
$("prReload").onclick = () => loadPrograms(true);
$("prQuiet").onclick = () => uninstallPrograms(PROGS.filter(p => prSel.has(p.id)), true);
$("upCheck").onclick = () => busy($("upCheck"), async () => {
  $("upList").innerHTML = loadingHTML();
  try { UPGRADES = await api("programs/upgrades"); } catch (e) { $("upList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; UPGRADES = null; return; }
  upSel.clear(); renderUpgrades();
});
function renderUpgrades() {
  const can = !polBlocks("programs");
  $("upAll").disabled = !UPGRADES.length || !can;
  $("upAll").textContent = upSel.size ? t("pr_upd_sel", upSel.size) : t("pr_upd_all");
  if (!UPGRADES.length) { $("upList").innerHTML = emptyHTML("pr_upd_none"); return; }
  $("upList").innerHTML = `<div class="muted small" style="margin:0 2px 8px">${t("pr_upd_found", UPGRADES.length)}</div><div class="items">` + UPGRADES.map(u => `<label class="item">
    <input type="checkbox" class="up-chk" value="${esc(u.id)}" ${upSel.has(u.id) ? "checked" : ""} ${can ? "" : "disabled"}>
    <div><div class="n">${esc(u.name)}</div><div class="d mono">${esc(u.id)}</div></div>
    <div class="tags"><span class="tag">${esc(u.version)}</span><span class="muted">→</span><span class="tag solid">${esc(u.available)}</span></div></label>`).join("") + `</div>`;
  $("upList").querySelectorAll(".up-chk").forEach(c => c.onchange = () => { c.checked ? upSel.add(c.value) : upSel.delete(c.value); renderUpgrades(); });
}
$("upAll").onclick = async () => {
  try { await api("programs/upgrade", { ids: [...upSel] }); } catch (e) { return toast(e.message, true); }
  showJobModal(t("pr_upd_title"), () => { UPGRADES = null; $("upCheck").click(); });
  pollMaintJob();
};

// ---------- Службы ----------
let SERVICES = null, TASKS = null;
async function loadServices(force) {
  if (!HOST.sysadmin) return;
  if (SERVICES && !force) return renderServices();
  $("svList").innerHTML = loadingHTML();
  try { SERVICES = await api("services"); } catch (e) { $("svList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderServices();
}
function renderServices() {
  const q = $("svSearch").value.trim().toLowerCase();
  const third = $("svThird").checked, running = $("svRunning").checked;
  const list = SERVICES.filter(s => (!third || !s.windows) && (!running || s.state === "running") &&
    (!q || (s.display + " " + s.name + " " + s.desc).toLowerCase().includes(q)));
  $("svStats").textContent = t("sv_stats", list.length, SERVICES.length, SERVICES.filter(s => s.state === "running").length);
  const can = HOST.elevated && !polBlocks("services");
  const starts = ["auto", "delayed", "manual", "disabled"];
  $("svList").innerHTML = (HOST.elevated ? "" : `<p class="help" style="margin:0 2px 10px">${t("sv_need_admin")}</p>`) + (list.length ? `<div class="items">` + list.map(s => `<div class="item sv-item">
    <span class="state-dot ${s.state === "running" ? "on" : ""}" title="${tm("sv_state", s.state)}"></span>
    <div class="sv-main"><div class="n">${esc(s.display)} <span class="muted mono-s">${esc(s.name)}</span></div>${s.desc ? `<div class="d">${esc(s.desc)}</div>` : ""}</div>
    <div class="tags">${s.critical ? `<span class="tag">${t("sv_critical")}</span>` : ""}<span class="tag ${s.state === "running" ? "solid" : ""}">${tm("sv_state", s.state)}</span>
      <select class="input sv-start" data-n="${esc(s.name)}" ${can && starts.includes(s.start) ? "" : "disabled"}>${(starts.includes(s.start) ? starts : [s.start]).map(o => `<option value="${o}" ${o === s.start ? "selected" : ""} ${o === "disabled" && s.critical ? "disabled" : ""}>${tm("sv_start", o)}</option>`).join("")}</select>
      ${s.state === "running"
        ? `<button class="btn sv-act" data-n="${esc(s.name)}" data-a="restart" ${can ? "" : "disabled"}>${t("sv_restart_btn")}</button><button class="btn sv-act" data-n="${esc(s.name)}" data-a="stop" ${can && !s.critical ? "" : "disabled"}>${t("sv_stop_btn")}</button>`
        : `<button class="btn sv-act" data-n="${esc(s.name)}" data-a="start" ${can && s.start !== "disabled" ? "" : "disabled"}>${t("sv_start_btn")}</button>`}
    </div></div>`).join("") + `</div>` : emptyHTML("nothing_found"));
  $("svList").querySelectorAll(".sv-start").forEach(sel => sel.onchange = async () => {
    const s = SERVICES.find(x => x.name === sel.dataset.n);
    sel.disabled = true;
    try { await api("services/start-type", { name: s.name, display: s.display, start: sel.value }); s.start = sel.value; toast(`${s.display}: ${tm("sv_start", sel.value)}`); }
    catch (e) { toast(e.message, true); sel.value = s.start; }
    sel.disabled = false;
  });
  $("svList").querySelectorAll(".sv-act").forEach(b => b.onclick = async () => {
    const s = SERVICES.find(x => x.name === b.dataset.n);
    if (b.dataset.a === "stop" && !await confirmRun(t("sv_stop_btn"), t("sv_stop_q", s.display), t("sv_stop_btn"), true)) return;
    await busy(b, async () => {
      await api("services/action", { name: s.name, display: s.display, action: b.dataset.a, prev: s.state });
      toast(`${s.display}: ${t({ start: "sv_start_btn", stop: "sv_stop_btn", restart: "sv_restart_btn" }[b.dataset.a])} — ${t("done")}`);
    });
    loadServices(true);
  });
}
["svSearch"].forEach(id => $(id).oninput = () => SERVICES && renderServices());
["svThird", "svRunning"].forEach(id => $(id).onchange = () => SERVICES && renderServices());
$("svReload").onclick = () => loadServices(true);

// ---------- Задачи планировщика ----------
async function loadTasks(force) {
  if (!HOST.sysadmin) return;
  if (TASKS && !force) return renderTasks();
  $("tkList").innerHTML = loadingHTML("tk_loading");
  try { TASKS = await api("tasks"); } catch (e) { $("tkList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  renderTasks();
}
function taskResult(r) {
  if (r === 0) return "OK";
  if (r === 267011) return t("tk_never");
  if (r === 267009) return tm("sv_state", "running");
  return "0x" + (r >>> 0).toString(16).toUpperCase();
}
function renderTasks() {
  const q = $("tkSearch").value.trim().toLowerCase();
  const hide = $("tkHideMs").checked;
  const list = TASKS.filter(x => (!hide || !x.windows) && (!q || (x.path + " " + x.action + " " + x.author + " " + x.desc).toLowerCase().includes(q)));
  $("tkStats").textContent = t("tk_stats", list.length, TASKS.length);
  const can = !polBlocks("services");
  $("tkList").innerHTML = list.length ? `<div class="items">` + list.slice(0, 400).map(x => `<div class="item">
    <label class="switch"><input type="checkbox" class="tk-on" data-p="${esc(x.path)}" ${x.enabled ? "checked" : ""} ${can ? "" : "disabled"}><i></i></label>
    <div><div class="n">${esc(x.name)} <span class="muted small">${esc(x.folder)}</span></div>
      <div class="d mono" title="${esc(x.action)}">${esc(x.action || x.desc || "—")}</div>
      <div class="d">${esc([x.author, x.triggers.map(g => tm("tk_trig", g)).join(", "), x.lastRun ? t("tk_last", fmtDate(x.lastRun)) : t("tk_never"), x.nextRun ? t("tk_next", fmtDate(x.nextRun)) : ""].filter(Boolean).join(" · "))}</div></div>
    <div class="tags"><span class="tag ${x.result !== 0 && x.result !== 267011 && x.lastRun ? "solid" : ""}">${esc(taskResult(x.result))}</span>
      <button class="btn tk-run" data-p="${esc(x.path)}" ${can ? "" : "disabled"}>${t("tk_run")}</button></div></div>`).join("") + `</div>` : emptyHTML("nothing_found");
  $("tkList").querySelectorAll(".tk-on").forEach(c => c.onchange = async () => {
    c.disabled = true;
    const x = TASKS.find(k => k.path === c.dataset.p);
    try { await api("tasks/enable", { path: x.path, on: c.checked }); x.enabled = c.checked; toast(`${x.name}: ${c.checked ? t("as_on") : t("as_off")}`); }
    catch (e) { toast(e.message, true); c.checked = !c.checked; }
    c.disabled = false;
  });
  $("tkList").querySelectorAll(".tk-run").forEach(b => b.onclick = () => busy(b, async () => { await api("tasks/run", { path: b.dataset.p }); toast(t("done")); }));
}
$("tkSearch").oninput = () => TASKS && renderTasks();
$("tkHideMs").onchange = () => TASKS && renderTasks();
$("tkReload").onclick = () => loadTasks(true);

// ---------- Сеть ----------
async function loadAdapters(force) {
  if (loaded.net && !force) return;
  loaded.net = true;
  $("netAdapters").innerHTML = loadingHTML();
  let list; try { list = await api("net/adapters"); } catch (e) { $("netAdapters").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  $("netAdapters").innerHTML = list.length ? list.map(a => `<div class="adapter ${a.status === "Up" ? "" : "off"} ${a.virtual ? "virt" : ""}">
    <div class="toolbar"><b class="grow">${esc(a.name)}</b>${a.wifi ? `<span class="tag">Wi-Fi</span>` : ""}<span class="tag ${a.status === "Up" ? "solid" : ""}">${esc(a.status)}</span></div>
    ${a.desc ? `<div class="muted small">${esc(a.desc)}</div>` : ""}
    <div class="kv" style="margin-top:6px">${[["IPv4", a.ipv4.join(", ")], [t("gateway"), a.gateway.join(", ")], ["DNS", a.dns.join(", ")], ["MAC", a.mac], [t("speed"), a.speed]]
      .filter(([, v]) => v).map(([k, v]) => `<span>${esc(k)}</span><span class="mono-s">${esc(v)}</span>`).join("")}</div></div>`).join("") : emptyHTML("nothing_found");
}
$("netReload").onclick = () => loadAdapters(true);
async function netCheck() {
  $("netVerdict").innerHTML = loadingHTML();
  await busy($("netCheckBtn"), async () => {
    const r = await api("net/check", {});
    const row = (ok, label, ms) => `<div class="check-row l${ok ? 0 : 2}">${lvDot(ok ? 0 : 2)}<div class="grow">${esc(label)}</div><span class="muted">${ok && ms >= 0 ? ms + " " + t("ms") : "—"}</span></div>`;
    $("netVerdict").innerHTML = `<div class="check-list">${row(r.dns, t("net_dns"), r.dnsMs)}${row(r.tcp, t("net_inet"), r.tcpMs)}</div>
      <p class="verdict-text l${r.verdict === "ok" ? 0 : r.verdict === "slow" ? 1 : 2}">${esc(t("nv_" + r.verdict))}</p>` +
      (r.verdict === "dns" || r.verdict === "offline" ? `<button class="btn" id="netToMaint">${t("nav_maintenance")}</button>` : "");
    if ($("netToMaint")) $("netToMaint").onclick = () => showPage("maintenance");
  });
}
$("netCheckBtn").onclick = netCheck;
$("netFlush").onclick = async () => {
  if (!HOST.maintenance) return toast(t("win_only"), true);
  try { await api("maintenance/run", { id: "flush_dns" }); showJobModal(t("net_flush")); pollMaintJob(); } catch (e) { toast(e.message, true); }
};
function netOut(html) { const o = $("netOut"); o.classList.remove("hidden"); o.innerHTML = html; }
$("netPing").onclick = () => busy($("netPing"), async () => {
  const host = $("netHost").value.trim(), port = Number($("netPort").value) || 443;
  const r = await api("net/ping", { host, port });
  if (r.error || !r.ip || r.lost === r.times.length) return netOut(esc(t("ping_fail")));
  netOut(esc(t("ping_res", r.host, r.port, r.ip, r.times.map(x => x < 0 ? "×" : x).join(" / "), r.lost, r.times.length, r.dnsMs)));
});
$("netPortBtn").onclick = () => busy($("netPortBtn"), async () => {
  const host = $("netHost").value.trim(), port = Number($("netPort").value) || 443;
  const r = await api("net/port", { host, port });
  netOut(esc(r.open ? t("port_open", port, host, r.ms) : t("port_closed", port, host)));
});
$("netTrace").onclick = async () => {
  const host = $("netHost").value.trim();
  try { await api("net/trace", { host }); showJobModal(t("net_trace") + " · " + host); pollMaintJob(); } catch (e) { toast(e.message, true); }
};

// ---------- Сбои ----------
let CR_DAYS = 30;
function renderCrDays() {
  $("crDays").innerHTML = [7, 30, 90].map((d, i) => `<button data-d="${d}" class="${d === CR_DAYS ? "on" : ""}">${t("cr_days")[i]}</button>`).join("");
  $("crDays").querySelectorAll("button").forEach(b => b.onclick = () => { CR_DAYS = Number(b.dataset.d); renderCrDays(); loadCrashes(true); });
}
async function loadCrashes(force) {
  if (!HOST.diag) return;
  if (loaded.cr && !force) return;
  loaded.cr = true;
  ["crBsod", "crApps", "crUnexp", "crEvents"].forEach(id => $(id).innerHTML = loadingHTML(id === "crBsod" ? "cr_loading" : "loading"));
  let r; try { r = await api("crashes?days=" + CR_DAYS); } catch (e) { $("crBsod").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  $("crNote").textContent = r.note || "";
  $("crBsod").innerHTML = r.bsod.length ? r.bsod.map(c => `<div class="bsod">
    <div class="toolbar"><b class="mono-s">${esc(c.bug.hex)}</b><b class="grow">${esc(c.bug.name)}</b><span class="tag">${t(c.source === "dump" ? "cr_dump" : "cr_event")}</span><span class="muted small">${fmtDate(c.time)}</span></div>
    <div class="d">${esc(c.bug.hint)}</div>${c.file ? `<div class="muted small mono-s">${esc(c.file)}</div>` : ""}</div>`).join("") : `<div class="ok-line">${lvDot(0)} ${t("cr_none_bsod")}</div>`;
  $("crApps").innerHTML = r.apps.length ? `<table class="mini"><tbody>` + r.apps.slice(0, 15).map(a => `<tr><td><b>${esc(a.app)}</b><div class="muted small mono-s">${esc([a.module, a.exception].filter(Boolean).join(" · "))}</div></td>
    <td><span class="tag">${t(a.kind === "hang" ? "cr_hang" : "cr_crash")}</span></td><td class="num">${t("cr_times", a.count)}</td><td class="num muted small">${fmtDate(a.last)}</td></tr>`).join("") + `</tbody></table>` : `<div class="ok-line">${lvDot(0)} ${t("cr_none_apps")}</div>`;
  $("crUnexp").innerHTML = r.unexpected.length ? `<p style="margin:0 0 6px">${esc(t("cr_unexp_n", r.unexpected.length))}</p><div class="muted small">${r.unexpected.slice(0, 8).map(x => fmtDate(x)).join(" · ")}</div>` : `<div class="ok-line">${lvDot(0)} ${t("cr_none_unexp")}</div>`;
  $("crEvents").innerHTML = r.events.length ? `<div class="items">` + r.events.map(e => `<div class="item ev-item"><span class="ev-count">${e.count}×</span>
    <div><div class="n">${esc(e.provider)} <span class="muted small">#${e.id} · ${esc(e.log)}</span></div><div class="d">${esc(e.message)}</div></div>
    <div class="tags"><span class="tag ${e.level === 1 ? "solid" : ""}">${t(e.level === 1 ? "ev_crit" : "ev_err")}</span><span class="muted small">${fmtDate(e.last)}</span></div></div>`).join("") + `</div>` : `<div class="ok-line">${lvDot(0)} ${t("cr_none_events")}</div>`;
}
$("crReload").onclick = () => loadCrashes(true);

// ---------- Отчёт, защита, загрузка ----------
let LAST_REPORT = "";
function loadReportPage(force) {
  renderPolicyInfo();
  if (loaded.rep && !force) return;
  loaded.rep = true;
  loadSecurity(); loadBoot();
}
async function loadSecurity() {
  if (!HOST.diag) { $("secList").innerHTML = emptyHTML("sec_none"); return; }
  $("secList").innerHTML = loadingHTML();
  let list; try { list = await api("security"); } catch (e) { $("secList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  $("secList").innerHTML = `<div class="check-list">` + list.map(s => `<div class="check-row l${s.level}">${lvDot(s.level)}
    <div class="grow"><div class="n">${esc(s.title)}</div><div class="d">${esc(s.state)}${s.detail ? `<br>${esc(s.detail)}` : ""}</div></div>
    ${s.page && !navHidden(s.page) ? `<button class="btn sec-go" data-p="${s.page}">${t("fix")}</button>` : ""}</div>`).join("") + `</div>`;
  $("secList").querySelectorAll(".sec-go").forEach(b => b.onclick = () => showPage(b.dataset.p));
}
$("secReload").onclick = loadSecurity;
let BOOT = null;
async function fetchBoot(force) {
  if (BOOT && !force) return BOOT;
  try { BOOT = await api("boot"); } catch { BOOT = null; }
  return BOOT;
}
function bootHTML(b, full) {
  if (!b) return emptyHTML("boot_none");
  let h = `<div class="muted small" style="margin-bottom:8px">${t("boot_last", fmtDate(b.lastBoot))}</div>`;
  if (b.boots.length) {
    const list = b.boots.slice(0, full ? 8 : 3), max = Math.max(...list.map(x => x.total), 1);
    h += `<div class="group-title">${t("boot_recent")}</div>` + list.map(x => meterHTML(fmtDate(x.time), x.total / max, t("boot_sec", (x.total / 1000).toFixed(1)), 130, 60)).join("");
  }
  if (b.slow.length) {
    h += `<div class="group-title" style="margin-top:10px">${t("boot_slow")}</div><div class="check-list">` + b.slow.slice(0, full ? 10 : 5).map(d => `<div class="check-row">
      <span class="tag">${tm("boot_kind", d.kind)}</span><div class="grow"><div class="n">${esc(d.friendly || d.name)}</div><div class="d mono-s">${esc(d.name)}</div></div><b>${t("boot_plus", (d.degradation / 1000).toFixed(1))}</b></div>`).join("") + `</div>`;
  } else if (!b.boots.length && b.note) h += `<p class="help">${esc(b.note)}</p>`;
  return h;
}
async function loadBoot() { $("bootInfo").innerHTML = loadingHTML(); $("bootInfo").innerHTML = bootHTML(await fetchBoot(true), true); }
async function loadBootMini(force) {
  if (!HOST.diag) return;
  const b = await fetchBoot(force);
  if (!b || (!b.boots.length && !b.slow.length)) return;
  $("bootCard").classList.remove("hidden");
  $("bootMini").innerHTML = bootHTML(b, false);
}
$("rpMake").onclick = async () => {
  $("rpLast").textContent = t("rp_making");
  await busy($("rpMake"), async () => {
    const r = await api("report", { open: true });
    LAST_REPORT = r.path;
    $("rpLast").textContent = t("rp_saved", r.path);
    toast(t("rp_saved", r.path));
  });
};
$("rpFolder").onclick = () => api("report/reveal", { path: LAST_REPORT }).catch(e => toast(e.message, true));
$("polTemplate").onclick = () => busy($("polTemplate"), async () => { const r = await api("policy/template", {}); toast(t("rp_tpl_saved", r.path)); });
const openLogs = () => api("logs/open", {}).catch(e => toast(e.message, true));
$("openLogs").onclick = openLogs; $("openLogs2").onclick = openLogs;
function renderPolicyInfo() {
  const p = pol();
  if (!p.managed) { $("policyInfo").textContent = t("pol_none"); return; }
  const names = t("pol_names");
  const on = Object.keys(names).filter(k => p[k]).map(k => names[k]);
  if (p.hideSections && p.hideSections.length) on.push((LANG === "en" ? "hidden: " : "скрыто: ") + p.hideSections.join(", "));
  if (p.profile) on.push((LANG === "en" ? "profile: " : "профиль: ") + p.profile);
  $("policyInfo").textContent = t("pol_active", p.source, on.join("; ") || "—");
}

// ---------- История ----------
let HIST = null;
async function loadHistory() {
  try { HIST = await api("history"); } catch (e) { $("hiList").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  const areas = [...new Set(HIST.map(h => h.area))];
  const cur = $("hiArea").value;
  $("hiArea").innerHTML = `<option value="">${t("hi_all")}</option>` + areas.map(a => `<option value="${a}">${esc(tm("area", a))}</option>`).join("");
  $("hiArea").value = areas.includes(cur) ? cur : "";
  renderHistory();
}
function renderHistory() {
  const q = $("hiSearch").value.trim().toLowerCase(), area = $("hiArea").value;
  const list = HIST.filter(h => (!area || h.area === area) && (!q || (h.title + " " + h.detail).toLowerCase().includes(q)));
  const can = !pol().readOnly;
  $("hiList").innerHTML = list.length ? `<div class="items">` + list.slice(0, 500).map(h => `<div class="item hi-item ${h.undoneAt ? "na" : ""}">
    <div class="hi-time">${fmtDate(h.time)}</div>
    <div><div class="n">${esc(h.title)}</div>${h.detail ? `<div class="d mono-s" title="${esc(h.detail)}">${esc(h.detail)}</div>` : ""}
      <div class="d">${esc([tm("area", h.area), tm("src", h.source), h.user, h.admin ? t("r_admin") : ""].filter(Boolean).join(" · "))}</div>
      ${!h.undo && h.undoNote && !h.undoneAt ? `<div class="c">${esc(h.undoNote)}</div>` : ""}</div>
    <div class="tags">${h.undoneAt ? `<span class="tag">${t("hi_undone")}</span>` : h.undo && can ? `<button class="btn hi-undo" data-id="${h.id}">${t("hi_undo")}</button>` : ""}</div></div>`).join("") + `</div>` : emptyHTML("hi_empty");
  $("hiList").querySelectorAll(".hi-undo").forEach(b => b.onclick = async () => {
    const h = HIST.find(x => x.id === Number(b.dataset.id));
    if (!await confirmRun(t("hi_undo"), t("hi_undo_q", h.title), t("hi_undo"))) return;
    await busy(b, async () => { await api("history/undo", { id: h.id }); toast(t("done")); });
    loadHistory();
  });
}
$("hiSearch").oninput = () => HIST && renderHistory();
$("hiArea").onchange = () => HIST && renderHistory();
$("hiClear").onclick = async () => {
  if (!await confirmRun(t("hi_clear"), t("hi_clear_q"), t("hi_clear"), true)) return;
  await api("history/clear", {}); loadHistory();
};

// ---------- Палитра команд (Ctrl+K) ----------
const PAL = { items: [], shown: [], idx: 0 };
function navHidden(page) { const b = document.querySelector(`#nav button[data-page="${page}"]`); return !b || b.classList.contains("hidden"); }
function palSource() {
  const out = [];
  const both = k => [].concat(DICT[k] || []).filter(x => typeof x === "string").join(" ");
  document.querySelectorAll("#nav button[data-page]").forEach(b => {
    if (b.classList.contains("hidden")) return;
    const key = (b.querySelector("[data-t]") || {}).dataset?.t;
    out.push({ kind: "pal_page", label: b.textContent.trim(), kw: both(key), run: () => showPage(b.dataset.page) });
  });
  const act = (k, run, kw = "") => out.push({ kind: "pal_action", label: t(k), kw: kw + " " + both(k), run });
  act("act_checkup", () => { showPage("optimize"); runCheckup(); }, "check scan проверка");
  act("free_mem", () => api("opt/trim", {}).then(m => toast(m)).catch(e => toast(e.message, true)), "ram memory память");
  if (!polBlocks("disk")) act("act_quickclean", () => api("quickclean", {}).then(r => toast(t("cleaned", bytes(r.freed)))).catch(e => toast(e.message, true)), "junk temp мусор");
  act("rp_make", () => { showPage("report"); $("rpMake").click(); }, "report отчёт");
  act("net_check_run", () => { showPage("network"); netCheck(); }, "internet интернет");
  if (HOST.tweaks) act("restore_point", () => api("tweaks/restore-point", {}).then(() => toast(t("rp_ok"))).catch(e => toast(e.message, true)), "restore");
  if (HOST.canElevate) act("elevate", () => api("elevate", {}), "admin админ");
  act("act_theme", () => { const v = CFG.theme === "dark" ? "light" : "dark"; document.documentElement.dataset.theme = v; setCfg({ theme: v }).then(renderSettingsPage); }, "theme dark light тема");
  act("pr_upd_check", () => { showPage("programs"); showSub("pr-updates"); $("upCheck").click(); }, "winget update");
  (MAINT || []).forEach(m => out.push({ kind: "pal_maint", label: m.title, kw: m.desc, run: () => runMaint(m.id, m.title) }));
  (SETTINGS || []).forEach(s => out.push({ kind: "pal_tweak", label: s.title, kw: s.desc, run: () => { showPage("tweaks"); showSub("settings"); $("twSetSearch").value = s.title; renderSettings(); } }));
  (PROGS || []).forEach(p => out.push({ kind: "pal_prog", label: p.name, kw: p.publisher, run: () => { showPage("programs"); showSub("pr-list"); $("prSearch").value = p.name; renderPrograms(); } }));
  return out;
}
async function openPalette() {
  if (HOST.tweaks && !SETTINGS) api("tweaks/settings").then(s => { SETTINGS = s; if (!$("palette").classList.contains("hidden")) palFilter(); }).catch(() => {});
  if (HOST.maintenance && !MAINT) api("maintenance/actions").then(m => { MAINT = m; }).catch(() => {});
  PAL.items = palSource();
  $("palInput").placeholder = t("pal_ph");
  $("palInput").value = "";
  $("palette").classList.remove("hidden");
  palFilter();
  $("palInput").focus();
}
function closePalette() { $("palette").classList.add("hidden"); }
function palFilter() {
  PAL.items = palSource();
  const q = $("palInput").value.trim().toLowerCase();
  const scored = PAL.items.map(it => {
    const l = it.label.toLowerCase(), k = (it.kw || "").toLowerCase();
    let sc = !q ? (it.kind === "pal_page" ? 2 : it.kind === "pal_action" ? 1 : 0) : l.startsWith(q) ? 5 : l.includes(q) ? 4 : k.includes(q) ? 2 : -1;
    return [sc, it];
  }).filter(([s]) => s >= 0).sort((a, b) => b[0] - a[0]);
  PAL.shown = scored.slice(0, q ? 14 : 22).map(([, it]) => it);
  PAL.idx = 0;
  renderPalette();
}
function renderPalette() {
  $("palList").innerHTML = PAL.shown.map((it, i) => `<div class="pal-row ${i === PAL.idx ? "on" : ""}" data-i="${i}"><span class="grow">${esc(it.label)}</span><span class="muted small">${t(it.kind)}</span></div>`).join("") || emptyHTML("nothing_found");
  $("palList").querySelectorAll(".pal-row").forEach(r => { r.onclick = () => palRun(Number(r.dataset.i)); r.onmousemove = () => { if (PAL.idx !== Number(r.dataset.i)) { PAL.idx = Number(r.dataset.i); renderPalette(); } }; });
  const on = $("palList").querySelector(".on"); if (on) on.scrollIntoView({ block: "nearest" });
}
function palRun(i) { const it = PAL.shown[i]; closePalette(); if (it) it.run(); }
$("palInput").oninput = palFilter;
$("palInput").addEventListener("keydown", e => {
  if (e.key === "ArrowDown") { PAL.idx = Math.min(PAL.idx + 1, PAL.shown.length - 1); renderPalette(); e.preventDefault(); }
  else if (e.key === "ArrowUp") { PAL.idx = Math.max(PAL.idx - 1, 0); renderPalette(); e.preventDefault(); }
  else if (e.key === "Enter") { palRun(PAL.idx); e.preventDefault(); }
  else if (e.key === "Escape") { closePalette(); e.preventDefault(); }
});
$("palette").onclick = e => { if (e.target === $("palette")) closePalette(); };
$("paletteBtn").onclick = openPalette;
document.addEventListener("keydown", e => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); $("palette").classList.contains("hidden") ? openPalette() : closePalette(); }
  if (e.key === "F5") { e.preventDefault(); onPageShow(state.page || "overview", true); } // F5 — обновить раздел, а не всё окно
});

// ---------- Политики и запуск ----------
function applyPolicy() {
  const p = pol();
  $("policyBadge").classList.toggle("hidden", !p.managed);
  (p.hideSections || []).forEach(sec => { const b = document.querySelector(`#nav button[data-page="${sec.trim()}"]`); if (b) b.classList.add("hidden"); });
  document.querySelectorAll(".policy-note").forEach(n => {
    const on = polBlocks(n.dataset.rule);
    n.classList.toggle("hidden", !on);
    n.textContent = on ? t("pol_blocked") : "";
  });
}
// Заголовки групп меню без видимых пунктов скрываются.
function tidyNav() {
  let head = null, any = false;
  const flush = () => { if (head) head.classList.toggle("hidden", !any); };
  [...$("nav").children].forEach(el => {
    if (el.classList.contains("nav-group")) { flush(); head = el; any = false; }
    else if (el.dataset.page && !el.classList.contains("hidden")) any = true;
    else if (el.classList.contains("side-spacer")) { flush(); head = null; }
  });
  flush();
}
function init2() {
  $("navPrograms").classList.toggle("hidden", !HOST.programs);
  $("navServices").classList.toggle("hidden", !HOST.sysadmin);
  $("navCrashes").classList.toggle("hidden", !HOST.diag);
  $("navTweaks").classList.toggle("hidden", !HOST.tweaks);
  [["programs", HOST.programs], ["services", HOST.sysadmin], ["crashes", HOST.diag]].forEach(([k, on]) => {
    $(k + "Unsupported").classList.toggle("hidden", on);
    $(k + "Main").classList.toggle("hidden", !on);
  });
  $("prSort").innerHTML = t("pr_sort").map((s, i) => `<option value="${i}">${s}</option>`).join("");
  $("cliHelp").textContent = t("cli_help");
  renderCrDays();
  renderSchedule();
  applyPolicy();
  tidyNav();
  if (HOST.diag) { loadReboot(); setInterval(loadReboot, 10 * 60 * 1000); }
}
