import { isCallError } from "mygo-runtime";
import { Devices, Settings, Smart, type DeviceInfo, type DeviceState, type DPIStage, type SmartConfig } from "./mygo";

const $ = <T extends HTMLElement = HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
const RATES = [125, 500, 1000, 2000, 4000, 8000];
const MAX_STAGES = 5;

let dev: DeviceInfo | undefined;
let state: DeviceState | undefined;

// ---------- helpers ----------

let toastTimer = 0;
function toast(msg: string, error = false) {
  const t = $("#toast");
  t.textContent = msg;
  t.className = error ? "error" : "";
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (t.hidden = true), 2600);
}

async function attempt(fn: () => Promise<unknown>, ok?: string) {
  try {
    await fn();
    if (ok) toast(ok);
    return true;
  } catch (e) {
    toast(isCallError(e) ? e.message : String(e), true);
    return false;
  }
}

function debounce<A extends unknown[]>(fn: (...a: A) => void, ms: number) {
  let t = 0;
  return (...a: A) => {
    clearTimeout(t);
    t = window.setTimeout(() => fn(...a), ms);
  };
}

function segmented(el: HTMLElement, values: number[], current: number, onPick: (v: number) => void) {
  el.replaceChildren(
    ...values.map((v) => {
      const b = document.createElement("button");
      b.textContent = `${v} Hz`;
      b.classList.toggle("on", v === current);
      b.onclick = () => {
        el.querySelectorAll("button").forEach((x) => x.classList.toggle("on", x === b));
        onPick(v);
      };
      return b;
    }),
  );
}

// ---------- navigation ----------

document.querySelectorAll<HTMLButtonElement>(".nav").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll(".nav").forEach((x) => x.classList.toggle("active", x === b));
    document.querySelectorAll<HTMLElement>(".view").forEach((v) => (v.hidden = v.id !== `view-${b.dataset.view}`));
    if (b.dataset.view === "smart") loadSmart();
  };
});

document.querySelectorAll<HTMLButtonElement>(".tab").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll(".tab").forEach((x) => x.classList.toggle("active", x === b));
    document.querySelectorAll<HTMLElement>(".tab-body").forEach((v) => (v.hidden = v.id !== `tab-${b.dataset.tab}`));
  };
});

// ---------- device ----------

async function loadDevices(rescan = false) {
  try {
    const list = rescan ? await Devices.rescan() : await Devices.list();
    dev = list[0];
  } catch (e) {
    toast(isCallError(e) ? e.message : String(e), true);
    dev = undefined;
  }
  $("#no-device").hidden = !!dev;
  $("#device-body").hidden = !dev;
  $("#dev-name").textContent = dev?.name ?? "未连接设备";
  $("#dev-conn").textContent = dev ? (dev.wireless ? "HyperPolling 无线" : "有线") : "";
  $("#dev-conn").hidden = !dev;
  if (dev) await refreshState();
}

// full=false only refreshes battery, so a periodic read never clobbers an edit in progress.
async function refreshState(full = true) {
  if (!dev) return;
  let fresh: DeviceState;
  try {
    fresh = await Devices.state(dev.id);
  } catch {
    $("#dev-conn").textContent = "未响应（休眠中？）";
    $("#dev-battery").hidden = true;
    return;
  }
  $("#dev-conn").textContent = dev.wireless ? "HyperPolling 无线" : "有线";
  if (full || !state) {
    state = fresh;
    render();
  } else {
    state.battery = fresh.battery;
    state.charging = fresh.charging;
    renderBattery();
  }
}

function render() {
  if (!dev || !state) return;
  const s = state;
  $("#cur-dpi").textContent = s.dpi.x === s.dpi.y ? `${s.dpi.x}` : `${s.dpi.x} / ${s.dpi.y}`;
  renderStages();
  segmented($("#poll"), dev.pollRates ?? RATES, s.pollRate, (hz) =>
    attempt(() => Devices.setPollRate(dev!.id, hz), `回报率已设为 ${hz} Hz`).then((ok) => ok && (s.pollRate = hz)),
  );
  $("#poll-hint").textContent = s.pollRate >= 4000 ? "高回报率会增加 CPU 占用和耗电" : "";
  renderBattery();

  const idle = $<HTMLInputElement>("#idle");
  idle.value = String(Math.round(s.idleTime / 60));
  $("#idle-val").textContent = idle.value;
  const low = $<HTMLInputElement>("#low");
  low.value = String(Math.round(s.lowBattery / 5) * 5);
  $("#low-val").textContent = low.value;
}

function renderBattery() {
  const s = state!;
  const bat = $("#dev-battery");
  bat.hidden = s.battery < 0;
  bat.textContent = `${s.charging ? "⚡ " : ""}${s.battery}%`;
  bat.classList.toggle("low", s.battery >= 0 && s.battery <= s.lowBattery);
  $("#bat-num").textContent = s.battery < 0 ? "—" : `${s.battery}`;
  $("#bat-fill").style.width = `${Math.max(0, s.battery)}%`;
  $("#bat-state").textContent = s.battery < 0 ? "无法读取电量" : s.charging ? "正在充电" : "使用电池";
}

// --- DPI stages ---

const saveStages = debounce(() => {
  if (!dev || !state) return;
  const { stages, activeStage } = state;
  attempt(() => Devices.setDPIStages(dev!.id, stages, activeStage)).then((ok) => {
    if (ok) {
      state!.dpi = { ...stages[activeStage - 1]! };
      $("#cur-dpi").textContent = String(state!.dpi.x);
    }
  });
}, 350);

function renderStages() {
  if (!dev || !state) return;
  const s = state;
  s.stages ??= [];
  const link = $<HTMLInputElement>("#xy-link").checked;
  const box = $("#stages");
  box.replaceChildren(
    ...s.stages.map((st, i) => {
      const card = document.createElement("div");
      card.className = "stage" + (i + 1 === s.activeStage ? " on" : "");
      card.innerHTML = `
        <div class="stage-top"><span class="num">${i + 1}</span>
          <button class="x" title="删除档位" ${s.stages.length <= 1 ? "disabled" : ""}>✕</button></div>
        <label>X <input class="dx" type="number" min="100" max="${dev!.maxDpi}" step="50" value="${st.x}"></label>
        ${link ? "" : `<label>Y <input class="dy" type="number" min="100" max="${dev!.maxDpi}" step="50" value="${st.y}"></label>`}
        <input class="slider" type="range" min="100" max="${dev!.maxDpi}" step="50" value="${st.x}">`;
      const dx = card.querySelector<HTMLInputElement>(".dx")!;
      const dy = card.querySelector<HTMLInputElement>(".dy");
      const slider = card.querySelector<HTMLInputElement>(".slider")!;
      const clamp = (v: number) => Math.min(dev!.maxDpi, Math.max(100, Math.round(v) || 100));
      const setX = (v: number) => {
        st.x = clamp(v);
        if (link) st.y = st.x;
        dx.value = slider.value = String(st.x);
        saveStages();
      };
      slider.oninput = () => setX(+slider.value);
      dx.onchange = () => setX(+dx.value);
      if (dy) dy.onchange = () => ((st.y = clamp(+dy.value)), (dy.value = String(st.y)), saveStages());
      card.querySelector<HTMLButtonElement>(".x")!.onclick = (e) => {
        e.stopPropagation();
        s.stages.splice(i, 1);
        if (s.activeStage > s.stages.length) s.activeStage = s.stages.length;
        renderStages();
        saveStages();
      };
      card.onclick = (e) => {
        if ((e.target as HTMLElement).closest("input")) return;
        s.activeStage = i + 1;
        box.querySelectorAll(".stage").forEach((c, j) => c.classList.toggle("on", j === i));
        saveStages();
      };
      return card;
    }),
  );
  $<HTMLButtonElement>("#add-stage").disabled = s.stages.length >= MAX_STAGES;
}

$("#add-stage").onclick = () => {
  if (!state || state.stages.length >= MAX_STAGES) return;
  const last = state.stages.at(-1) ?? { x: 800, y: 800 };
  const next: DPIStage = { x: Math.min(dev!.maxDpi, last.x * 2), y: Math.min(dev!.maxDpi, last.y * 2) };
  state.stages.push(next);
  renderStages();
  saveStages();
};
$("#xy-link").onchange = renderStages;
$("#rescan").onclick = () => loadDevices(true);

$<HTMLInputElement>("#idle").oninput = (e) => ($("#idle-val").textContent = (e.target as HTMLInputElement).value);
$<HTMLInputElement>("#idle").onchange = (e) =>
  dev && attempt(() => Devices.setIdleTime(dev!.id, +(e.target as HTMLInputElement).value * 60), "休眠时间已保存");
$<HTMLInputElement>("#low").oninput = (e) => ($("#low-val").textContent = (e.target as HTMLInputElement).value);
$<HTMLInputElement>("#low").onchange = (e) =>
  dev && attempt(() => Devices.setLowBattery(dev!.id, +(e.target as HTMLInputElement).value), "低电量阈值已保存");

// ---------- smart polling ----------

let smart: SmartConfig | undefined;

const saveSmart = debounce(() => smart && attempt(() => Smart.setConfig(smart!)), 300);

async function loadSmart() {
  smart = await Smart.config();
  smart.rules ??= [];
  $<HTMLInputElement>("#smart-on").checked = smart.enabled;
  segmented($("#smart-default"), RATES, smart.defaultRate, (hz) => ((smart!.defaultRate = hz), saveSmart()));
  renderRules();
  smartStatus();
}

function rateSelect(value: number, onChange: (v: number) => void) {
  const sel = document.createElement("select");
  sel.innerHTML = RATES.map((r) => `<option value="${r}" ${r === value ? "selected" : ""}>${r} Hz</option>`).join("");
  sel.onchange = () => onChange(+sel.value);
  return sel;
}

function renderRules() {
  if (!smart) return;
  const rules = smart.rules;
  $("#rules").replaceChildren(
    ...(rules.length
      ? rules.map((r, i) => {
          const row = document.createElement("div");
          row.className = "rule";
          const exe = document.createElement("input");
          exe.placeholder = "程序文件名，如 cs2.exe";
          exe.value = r.exe;
          exe.onchange = () => ((r.exe = exe.value.trim()), saveSmart());
          const del = document.createElement("button");
          del.className = "ghost x";
          del.textContent = "✕";
          del.onclick = () => (rules.splice(i, 1), renderRules(), saveSmart());
          row.append(exe, rateSelect(r.rate, (v) => ((r.rate = v), saveSmart())), del);
          return row;
        })
      : [Object.assign(document.createElement("p"), { className: "muted small", textContent: "还没有规则。" })]),
  );
}

$("#add-rule").onclick = () => {
  smart?.rules.push({ exe: "", rate: 8000 });
  renderRules();
  document.querySelector<HTMLInputElement>("#rules .rule:last-child input")?.focus();
};
$<HTMLInputElement>("#smart-on").onchange = (e) => {
  if (!smart) return;
  smart.enabled = (e.target as HTMLInputElement).checked;
  saveSmart();
};

async function smartStatus() {
  if ($("#view-smart").hidden) return;
  const st = await Smart.status().catch(() => undefined);
  const on = $<HTMLInputElement>("#smart-on").checked;
  document.querySelector(".status-line")!.classList.toggle("live", on);
  $("#smart-status").textContent = !on
    ? "已关闭"
    : st && st.foreground
      ? `前台：${st.foreground} · 当前 ${st.applied || "—"} Hz`
      : "等待前台程序…";
}

// ---------- settings ----------

Settings.autostart().then((on) => ($<HTMLInputElement>("#autostart").checked = on));
$<HTMLInputElement>("#autostart").onchange = (e) => {
  const on = (e.target as HTMLInputElement).checked;
  attempt(() => Settings.setAutostart(on), on ? "已开启开机启动" : "已关闭开机启动");
};

// ---------- start ----------

await loadDevices();
setInterval(() => refreshState(false), 20_000); // battery; the window exists only while open
setInterval(smartStatus, 1_500);
