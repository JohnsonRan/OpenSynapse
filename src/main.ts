import { isCallError } from "mygo-runtime";
import {
  Devices,
  events,
  Settings,
  Scenes,
  type DeviceInfo,
  type DeviceState,
  type DPIStage,
  type RGB,
  type Scene,
  type ScenesConfig,
  type SmartPoll,
} from "./mygo";
import { setLang, t, translateDom } from "./i18n";

// Language first: everything below renders text.
const lang = await Settings.language().catch(() => ({ choice: "", effective: navigator.language.startsWith("zh") ? "zh-CN" : "en" }));
setLang(lang.effective);
translateDom();

const $ = <T extends HTMLElement = HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
const RATES = [125, 500, 1000, 2000, 4000, 8000];
const MAX_STAGES = 5;
const CATEGORIES = ["mouse", "keyboard", "mouse-mat", "accessory", "monitor"] as const;
const category = (c: string) => ((CATEGORIES as readonly string[]).includes(c) ? t(`cat.${c}` as "cat.mouse") : c);
const EFFECT_IDS = ["off", "static", "breathing", "spectrum", "wave"] as const;
const effectName = (e: string) => ((EFFECT_IDS as readonly string[]).includes(e) ? t(`effect.${e}` as "effect.off") : e);
const SWATCHES = ["#44d62c", "#ffffff", "#ff2d2d", "#ff8a00", "#ffe100", "#00c8ff", "#2f5bff", "#b400ff", "#ff3ea5"];

let list: DeviceInfo[] = [];
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

function segmented<T>(el: HTMLElement, values: T[], current: T, label: (v: T) => string, onPick: (v: T) => void) {
  el.replaceChildren(
    ...values.map((v) => {
      const b = document.createElement("button");
      b.textContent = label(v);
      b.classList.toggle("on", v === current);
      b.onclick = () => {
        el.querySelectorAll("button").forEach((x) => x.classList.toggle("on", x === b));
        onPick(v);
      };
      return b;
    }),
  );
}
const hz = (v: number) => `${v} Hz`;

const toHex = (c: RGB) => "#" + [c.r, c.g, c.b].map((v) => v.toString(16).padStart(2, "0")).join("");
const fromHex = (h: string): RGB => ({ r: parseInt(h.slice(1, 3), 16), g: parseInt(h.slice(3, 5), 16), b: parseInt(h.slice(5, 7), 16) });

// ---------- navigation ----------

function showView(name: string) {
  document.querySelectorAll<HTMLElement>(".view").forEach((v) => (v.hidden = v.id !== `view-${name}`));
  const rail = name === "device" ? "devices" : name;
  document.querySelectorAll<HTMLElement>(".nav").forEach((x) => x.classList.toggle("active", x.dataset.view === rail));
  if (name === "devices") loadDevices();
  if (name === "scenes") loadScenes();
}

document.querySelectorAll<HTMLButtonElement>(".nav").forEach((b) => (b.onclick = () => showView(b.dataset.view!)));
$("#back").onclick = () => showView("devices");

function showTab(name: string) {
  document.querySelectorAll<HTMLElement>(".tab").forEach((x) => x.classList.toggle("active", x.dataset.tab === name));
  document.querySelectorAll<HTMLElement>(".tab-body").forEach((v) => (v.hidden = v.id !== `tab-${name}`));
}
document.querySelectorAll<HTMLButtonElement>(".tab").forEach((b) => (b.onclick = () => showTab(b.dataset.tab!)));

// ---------- overview ----------

const ICONS: Record<string, string> = {
  mouse: '<path d="M12 2a6 6 0 0 0-6 6v8a6 6 0 0 0 12 0V8a6 6 0 0 0-6-6zm0 2v5M6 10h12" />',
  keyboard: '<rect x="2" y="6" width="20" height="12" rx="2" /><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10" />',
  "mouse-mat": '<rect x="3" y="5" width="18" height="14" rx="3" /><path d="M7 15h10" />',
  accessory: '<circle cx="12" cy="12" r="8" /><path d="M12 8v8M8 12h8" />',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="1" /><path d="M8 20h8M12 16v4" />',
};

async function loadDevices(rescan = false) {
  try {
    list = rescan ? await Devices.rescan() : await Devices.list();
  } catch (e) {
    toast(isCallError(e) ? e.message : String(e), true);
    list = [];
  }
  renderOverview();
}

function renderOverview() {
  $("#no-device").hidden = list.length > 0;
  $("#device-grid").replaceChildren(
    ...list.map((d) => {
      const card = document.createElement("button");
      card.className = "device-card";
      card.innerHTML = `
        <svg viewBox="0 0 24 24">${ICONS[d.category] ?? ICONS.accessory}</svg>
        <div class="dc-cat">${category(d.category)}</div>
        <div class="dc-name"></div>
        <div class="dc-meta muted small"></div>`;
      card.querySelector(".dc-name")!.textContent = d.name;
      card.onclick = () => openDevice(d);
      if (d.features.battery) {
        Devices.state(d.id)
          .then((s) => {
            if (s.battery >= 0) card.querySelector(".dc-meta")!.textContent = `${s.charging ? "⚡ " : ""}${t("overview.battery", { pct: s.battery })}`;
          })
          .catch(() => (card.querySelector(".dc-meta")!.textContent = t("overview.noAnswer")));
      }
      return card;
    }),
  );
}
$("#rescan").onclick = () => loadDevices(true);

// Plugging or unplugging: refresh the overview, and never leave a page up for
// a device that is gone. A mouse moving between cable and receiver changes ID,
// so its page follows it to the other connection.
events.devicesChanged.on((next) => {
  list = next;
  renderOverview();
  if (!dev || $("#view-device").hidden) return;
  if (list.some((d) => d.id === dev!.id)) {
    refreshState();
    return;
  }
  const sibling = list.find((d) => d.family === dev!.family);
  if (sibling) {
    toast(t(sibling.wired ? "device.toWired" : "device.toWireless"));
    openDevice(sibling);
  } else {
    toast(t("device.gone", { name: dev.name }));
    dev = undefined;
    showView("devices");
  }
});

// ---------- one device ----------

async function openDevice(d: DeviceInfo) {
  dev = d;
  state = undefined;
  const f = d.features;
  $("#dev-cat").textContent = category(d.category);
  $("#dev-name").textContent = d.name;
  $("#dev-conn").textContent = "";
  $("#dev-battery").hidden = true;
  const tabs: Record<string, boolean> = {
    perf: f.dpi || f.dpiStages || f.pollRate,
    lighting: !!f.lighting,
    power: f.battery || f.idleTime || f.lowBattery,
    receiver: f.receiverLeds,
  };
  document.querySelectorAll<HTMLElement>(".tab").forEach((t) => (t.hidden = !tabs[t.dataset.tab!]));
  $("#card-stages").hidden = !f.dpiStages;
  $("#card-dpi").hidden = !f.dpi || f.dpiStages;
  $("#card-poll").hidden = !f.pollRate;
  $("#card-battery").hidden = !f.battery;
  $("#card-idle").hidden = !f.idleTime;
  $("#card-low").hidden = !f.lowBattery;
  $("#card-brightness").hidden = !f.lighting?.brightness;
  const first = Object.keys(tabs).find((k) => tabs[k]);
  showTab(first ?? "perf");
  document.querySelector<HTMLElement>(".tabs")!.hidden = !first;
  showView("device");
  if (!smart) smart = await Scenes.smart().catch(() => undefined);
  await refreshState();
}

// full=false only refreshes battery, so a periodic read never clobbers an edit in progress.
async function refreshState(full = true) {
  if (!dev || $("#view-device").hidden) return;
  const id = dev.id;
  let fresh: DeviceState;
  try {
    fresh = await Devices.state(id);
  } catch {
    $("#dev-conn").textContent = t("device.asleep");
    return;
  }
  if (dev?.id !== id) return; // switched device meanwhile
  $("#dev-conn").textContent = "";
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
  const f = dev.features;
  $("#cur-dpi").textContent = s.dpi.x === s.dpi.y ? `${s.dpi.x}` : `${s.dpi.x} / ${s.dpi.y}`;
  if (f.dpiStages) renderStages();
  const dpi = $<HTMLInputElement>("#dpi");
  dpi.max = String(dev.maxDpi || 16000);
  dpi.value = String(s.dpi.x);
  $("#dpi-val").textContent = String(s.dpi.x);
  if (f.pollRate) {
    segmented($("#poll"), dev.pollRates, s.pollRate, hz, (v) =>
      attempt(() => Devices.setPollRate(dev!.id, v), t("poll.set", { hz: v })).then((ok) => ok && (s.pollRate = v)),
    );
  }
  $("#poll-hint").textContent =
    smart?.enabled && dev.category === "mouse"
      ? t("poll.smartOn")
      : s.pollRate >= 4000
        ? t("poll.highCost")
        : "";
  renderBattery();

  const idle = $<HTMLInputElement>("#idle");
  idle.value = String(Math.round(s.idleTime / 60));
  $("#idle-val").textContent = idle.value;
  const low = $<HTMLInputElement>("#low");
  low.value = String(Math.round(s.lowBattery / 5) * 5);
  $("#low-val").textContent = low.value;

  document.querySelectorAll<HTMLSelectElement>("[data-led]").forEach((sel) => {
    sel.innerHTML = LED_MODES.map((m, i) => `<option value="${i}">${t(m)}</option>`).join("");
    sel.value = String(s.receiverLeds[+sel.dataset.led!] ?? 0);
  });

  if (f.lighting) renderLighting();
}

function renderBattery() {
  const s = state!;
  const bat = $("#dev-battery");
  bat.hidden = s.battery < 0;
  bat.textContent = `${s.charging ? "⚡ " : ""}${s.battery}%`;
  bat.classList.toggle("low", s.battery >= 0 && s.lowBattery > 0 && s.battery <= s.lowBattery);
  $("#bat-num").textContent = s.battery < 0 ? "—" : `${s.battery}`;
  $("#bat-fill").style.width = `${Math.max(0, s.battery)}%`;
  $("#bat-state").textContent = t(s.battery < 0 ? "power.unreadable" : s.charging ? "power.charging" : "power.onBattery");
}

// --- DPI ---

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
  const max = dev.maxDpi || 16000;
  const link = $<HTMLInputElement>("#xy-link").checked;
  const box = $("#stages");
  box.replaceChildren(
    ...s.stages.map((st, i) => {
      const card = document.createElement("div");
      card.className = "stage" + (i + 1 === s.activeStage ? " on" : "");
      card.innerHTML = `
        <div class="stage-top"><span class="num">${i + 1}</span>
          <button class="x" title="${t("dpi.deleteStage")}" ${s.stages.length <= 1 ? "disabled" : ""}>✕</button></div>
        <label>X <input class="dx" type="number" min="100" max="${max}" step="50" value="${st.x}"></label>
        ${link ? "" : `<label>Y <input class="dy" type="number" min="100" max="${max}" step="50" value="${st.y}"></label>`}
        <input class="slider" type="range" min="100" max="${max}" step="50" value="${st.x}">`;
      const dx = card.querySelector<HTMLInputElement>(".dx")!;
      const dy = card.querySelector<HTMLInputElement>(".dy");
      const slider = card.querySelector<HTMLInputElement>(".slider")!;
      const clamp = (v: number) => Math.min(max, Math.max(100, Math.round(v) || 100));
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
  if (!dev || !state || state.stages.length >= MAX_STAGES) return;
  const last = state.stages.at(-1) ?? { x: 800, y: 800 };
  const max = dev.maxDpi || 16000;
  const next: DPIStage = { x: Math.min(max, last.x * 2), y: Math.min(max, last.y * 2) };
  state.stages.push(next);
  if (!state.activeStage) state.activeStage = 1;
  renderStages();
  saveStages();
};
$("#xy-link").onchange = renderStages;

const saveDPI = debounce((v: number) => dev && attempt(() => Devices.setDPI(dev!.id, v, v)), 300);
$<HTMLInputElement>("#dpi").oninput = (e) => {
  const v = +(e.target as HTMLInputElement).value;
  $("#dpi-val").textContent = String(v);
  saveDPI(v);
};

// --- power ---

$<HTMLInputElement>("#idle").oninput = (e) => ($("#idle-val").textContent = (e.target as HTMLInputElement).value);
$<HTMLInputElement>("#idle").onchange = (e) =>
  dev && attempt(() => Devices.setIdleTime(dev!.id, +(e.target as HTMLInputElement).value * 60), t("power.sleepSaved"));
$<HTMLInputElement>("#low").oninput = (e) => ($("#low-val").textContent = (e.target as HTMLInputElement).value);
$<HTMLInputElement>("#low").onchange = (e) =>
  dev && attempt(() => Devices.setLowBattery(dev!.id, +(e.target as HTMLInputElement).value), t("power.lowSaved"));

// --- receiver ---

const LED_MODES = ["led.off", "led.battery", "led.connection", "led.pollRate", "led.dpi"] as const;
document.querySelectorAll<HTMLSelectElement>("[data-led]").forEach((sel) => {
  sel.onchange = () => {
    if (!dev || !state) return;
    const modes = [...state.receiverLeds];
    modes[+sel.dataset.led!] = +sel.value;
    attempt(() => Devices.setReceiverLEDs(dev!.id, modes), t("receiver.saved")).then((ok) => {
      if (ok) state!.receiverLeds = modes;
      else sel.value = String(state!.receiverLeds[+sel.dataset.led!]);
    });
  };
});

// --- lighting ---

function renderLighting() {
  const l = state!.lighting;
  const caps = dev!.features.lighting!;
  segmented($("#effects"), caps.effects, l.effect, effectName, (e) => {
    l.effect = e;
    renderColorRow();
    applyLighting();
  });
  $("#swatches").replaceChildren(
    ...SWATCHES.map((hex) => {
      const b = document.createElement("button");
      b.className = "swatch";
      b.style.background = hex;
      b.title = hex;
      b.onclick = () => {
        l.color = fromHex(hex);
        $<HTMLInputElement>("#color").value = hex;
        renderColorRow();
        applyLighting();
      };
      return b;
    }),
  );
  $<HTMLInputElement>("#color").value = toHex(l.color);
  renderColorRow();
  const br = $<HTMLInputElement>("#brightness");
  br.value = String(Math.max(0, state!.brightness));
  $("#bright-val").textContent = state!.brightness < 0 ? "—" : br.value;
}

function renderColorRow() {
  const l = state!.lighting;
  const caps = dev!.features.lighting!;
  $("#color-row").hidden = !caps.color || !(l.effect === "static" || l.effect === "breathing");
  const cur = toHex(l.color);
  document.querySelectorAll<HTMLElement>(".swatch").forEach((s) => s.classList.toggle("on", s.title === cur));
}

// The device cannot report its effect, so nothing is sent until one is picked.
const applyLighting = debounce(() => {
  if (!dev || !state || !state.lighting.effect) return;
  attempt(() => Devices.setLighting(dev!.id, state!.lighting));
}, 150);

$<HTMLInputElement>("#color").oninput = (e) => {
  if (!state) return;
  state.lighting.color = fromHex((e.target as HTMLInputElement).value);
  renderColorRow();
  applyLighting();
};

const saveBrightness = debounce((v: number) => dev && attempt(() => Devices.setBrightness(dev!.id, v)), 200);
$<HTMLInputElement>("#brightness").oninput = (e) => {
  const v = +(e.target as HTMLInputElement).value;
  $("#bright-val").textContent = String(v);
  saveBrightness(v);
};

// ---------- scenes ----------

let scenes: ScenesConfig | undefined;

const saveScenes = debounce(() => scenes && attempt(() => Scenes.setConfig(scenes!)), 400);

let smart: SmartPoll | undefined;
const saveSmart = debounce(() => smart && attempt(() => Scenes.setSmart(smart!)), 300);
const rateText = (hz: number) => (hz === 0 ? t("smart.highest") : `${hz} Hz`);
const RATE_CHOICES = [0, ...[...RATES].reverse()];

function rateSelect(sel: HTMLSelectElement, value: number, onChange: (v: number) => void) {
  sel.innerHTML = RATE_CHOICES.map((r) => option(r, rateText(r), r === value)).join("");
  sel.onchange = () => onChange(+sel.value);
  return sel;
}

async function loadScenes() {
  [scenes, smart] = await Promise.all([Scenes.config(), Scenes.smart()]);
  scenes.scenes ??= [];
  smart.exceptions ??= [];
  $<HTMLInputElement>("#scenes-on").checked = scenes.enabled;
  $<HTMLInputElement>("#smart-on").checked = smart.enabled;
  rateSelect($<HTMLSelectElement>("#game-rate"), smart.gameRate, (v) => ((smart!.gameRate = v), saveSmart()));
  rateSelect($<HTMLSelectElement>("#normal-rate"), smart.normalRate, (v) => ((smart!.normalRate = v), saveSmart()));
  renderExceptions();
  renderScenes();
  autoStatus();
}

function renderExceptions() {
  const list = smart!.exceptions;
  $("#exceptions").replaceChildren(
    ...(list.length
      ? list.map((r, i) => {
          const row = document.createElement("div");
          row.className = "rule";
          const exe = document.createElement("input");
          exe.placeholder = t("smart.exePlaceholder");
          exe.value = r.exe;
          exe.onchange = () => ((r.exe = exe.value.trim()), saveSmart());
          const del = document.createElement("button");
          del.className = "ghost x";
          del.textContent = "✕";
          del.onclick = () => (list.splice(i, 1), renderExceptions(), saveSmart());
          row.append(exe, rateSelect(document.createElement("select"), r.rate, (v) => ((r.rate = v), saveSmart())), del);
          return row;
        })
      : [Object.assign(document.createElement("p"), { className: "muted small", textContent: t("smart.noExceptions") })]),
  );
}

$("#add-exception").onclick = () => {
  smart?.exceptions.push({ exe: "", rate: 0 });
  renderExceptions();
  document.querySelector<HTMLInputElement>("#exceptions .rule:last-child input")?.focus();
};
$<HTMLInputElement>("#smart-on").onchange = (e) => {
  if (!smart) return;
  smart.enabled = (e.target as HTMLInputElement).checked;
  syncSwitches();
  saveSmart();
};

function option(value: string | number, label: string, selected: boolean) {
  return `<option value="${value}" ${selected ? "selected" : ""}>${label}</option>`;
}

// Which scene cards are expanded, by key ("game", "desktop", "custom:<i>").
const openScenes = new Set<string>();

// sceneSummary is the one-line view of a collapsed scene.
function sceneSummary(s: Scene, kind: "custom" | "game" | "desktop") {
  const parts: string[] = [];
  if (kind !== "desktop") parts.push(s.apps?.length ? s.apps.join(", ") : kind === "game" ? t("scenes.gameHint") : t("scenes.noPrograms"));
  else parts.push(t("scenes.desktopHint"));
  const set: string[] = [];
  if (s.dpi) set.push(`${s.dpi} DPI`);
  if (s.lighting) set.push(effectName(s.lighting.effect));
  if (s.brightness != null) set.push(`${t("light.brightness")} ${s.brightness}%`);
  parts.push(set.length ? set.join(" · ") : t("scenes.nothingSet"));
  return parts;
}

// sceneCard edits one scene in place. The game and desktop scenes are fixed:
// the game scene takes auto-detected games (plus programs listed for it), the
// desktop scene everything else and has no programs. Cards start collapsed.
function sceneCard(s: Scene, kind: "custom" | "game" | "desktop", key: string, onDelete?: () => void) {
  const card = document.createElement("div");
  card.className = "card scene";
  const light = s.lighting;
  const title = {
    custom: `<input class="scene-name" maxlength="24" />`,
    game: `<h2>${t("scenes.game")}<span class="info" tabindex="0" title="${t("scenes.gameMore")}">i</span></h2>`,
    desktop: `<h2>${t("scenes.desktop")}<span class="info" tabindex="0" title="${t("scenes.desktopMore")}">i</span></h2>`,
  }[kind];
  const apps = { custom: t("scenes.appsCustom"), game: t("scenes.appsGame"), desktop: "" }[kind];
  card.innerHTML = `
    <div class="scene-head" title="${t("scenes.edit")}">
      <svg class="chev" viewBox="0 0 24 24"><path d="M9 6l6 6-6 6" /></svg>
      <div class="scene-title">${title}</div>
      <div class="scene-summary"><span class="sum-apps muted small"></span><span class="sum-set small"></span></div>
      ${kind === "custom" ? `<button class="ghost x" title="${t("scenes.delete")}">✕</button>` : ""}
    </div>
    <div class="scene-body">
    ${
      kind === "desktop"
        ? ""
        : `<div class="scene-row">
      <span class="muted small">${t("scenes.programs")}</span>
      <input class="apps" placeholder="${apps}" />
    </div>`
    }
    <div class="scene-grid">
      <label><span class="muted small">${t("scenes.dpi")}</span>
        <input class="dpi" type="number" min="100" max="50000" step="50" placeholder="${t("scenes.keep")}" /></label>
      <label><span class="muted small">${t("scenes.effect")}</span>
        <select class="effect">${option("", t("scenes.keep"), !light)}${EFFECT_IDS
          .map((k) => option(k, effectName(k), light?.effect === k))
          .join("")}</select></label>
      <label class="scene-color"><span class="muted small">${t("light.color")}</span><input class="color" type="color" /></label>
      <label><span class="muted small"><input class="bright-on" type="checkbox" title="${t("scenes.brightnessToggle")}" /> ${t("light.brightness")} <b class="bright-val"></b></span>
        <input class="bright" type="range" min="0" max="100" step="5" /></label>
    </div>
    </div>`;
  const q = <T extends HTMLElement>(sel: string) => card.querySelector<T>(sel)!;
  const summarize = () => {
    const [a, set] = sceneSummary(s, kind);
    q(".sum-apps").textContent = a!;
    q(".sum-set").textContent = set!;
  };
  const changed = () => (summarize(), saveScenes());
  const setOpen = (open: boolean) => {
    card.classList.toggle("open", open);
    if (open) openScenes.add(key);
    else openScenes.delete(key);
  };
  setOpen(openScenes.has(key));
  q(".scene-head").onclick = (e) => {
    if ((e.target as HTMLElement).closest("input, button, .info")) return;
    setOpen(!card.classList.contains("open"));
  };
  if (kind !== "desktop") {
    const apps = q<HTMLInputElement>(".apps");
    apps.value = (s.apps ?? []).join(", ");
    apps.onchange = () => ((s.apps = apps.value.split(/[,，\s]+/).filter(Boolean)), changed());
  }
  if (kind === "custom") {
    const name = q<HTMLInputElement>(".scene-name");
    name.value = s.name;
    name.onchange = () => ((s.name = name.value.trim() || s.name), (name.value = s.name), changed());
    q<HTMLButtonElement>(".x").onclick = onDelete!;
  }
  const dpi = q<HTMLInputElement>(".dpi");
  dpi.value = s.dpi ? String(s.dpi) : "";
  dpi.onchange = () => ((s.dpi = Math.min(50000, Math.max(0, Math.round(+dpi.value) || 0))), (dpi.value = s.dpi ? String(s.dpi) : ""), changed());
  const color = q<HTMLInputElement>(".color");
  const colorRow = q(".scene-color");
  // visibility, not hidden: the grid keeps its columns aligned across scenes
  const syncColor = () =>
    (colorRow.style.visibility = s.lighting && (s.lighting.effect === "static" || s.lighting.effect === "breathing") ? "" : "hidden");
  color.value = toHex(light?.color ?? { r: 0x44, g: 0xd6, b: 0x2c });
  syncColor();
  q<HTMLSelectElement>(".effect").onchange = (e) => {
    const v = (e.target as HTMLSelectElement).value;
    s.lighting = v ? { effect: v, color: fromHex(color.value) } : null;
    syncColor();
    changed();
  };
  color.oninput = () => {
    if (s.lighting) s.lighting.color = fromHex(color.value);
    changed();
  };
  const brightOn = q<HTMLInputElement>(".bright-on");
  const bright = q<HTMLInputElement>(".bright");
  const brightVal = q(".bright-val");
  const syncBright = () => {
    bright.disabled = s.brightness == null;
    bright.value = String(s.brightness ?? 100);
    brightVal.textContent = s.brightness == null ? t("scenes.keep") : `${s.brightness}%`;
  };
  brightOn.checked = s.brightness != null;
  syncBright();
  brightOn.onchange = () => ((s.brightness = brightOn.checked ? +bright.value : null), syncBright(), changed());
  bright.oninput = () => ((s.brightness = +bright.value), syncBright(), changed());
  summarize();
  return card;
}

function renderScenes() {
  if (!scenes) return;
  const c = scenes;
  $("#scene-list").replaceChildren(
    ...c.scenes.map((s, i) =>
      sceneCard(s, "custom", `custom:${i}`, () => {
        c.scenes.splice(i, 1);
        openScenes.clear(); // indexes shift
        renderScenes();
        saveScenes();
      }),
    ),
    sceneCard(c.game, "game", "game"),
    sceneCard(c.desktop, "desktop", "desktop"),
  );
  syncSwitches();
}

// A feature that is off shows only its switch.
function syncSwitches() {
  $("#smart-body").hidden = !$<HTMLInputElement>("#smart-on").checked;
  const scenesOn = $<HTMLInputElement>("#scenes-on").checked;
  $("#scene-list").hidden = !scenesOn;
  $("#add-scene").hidden = !scenesOn;
}

$("#add-scene").onclick = () => {
  if (!scenes) return;
  scenes.scenes.push({ name: t("scenes.newName", { n: scenes.scenes.length + 1 }), apps: [], dpi: 0, lighting: null, brightness: null });
  openScenes.add(`custom:${scenes.scenes.length - 1}`); // a new scene opens for editing
  renderScenes();
  saveScenes();
  document.querySelector<HTMLInputElement>(`#scene-list .scene:nth-child(${scenes.scenes.length}) .apps`)?.focus();
};
$<HTMLInputElement>("#scenes-on").onchange = (e) => {
  if (!scenes) return;
  scenes.enabled = (e.target as HTMLInputElement).checked;
  syncSwitches();
  saveScenes();
};

async function autoStatus() {
  if ($("#view-scenes").hidden) return;
  const st = await Scenes.status().catch(() => undefined);
  const on = $<HTMLInputElement>("#smart-on").checked || $<HTMLInputElement>("#scenes-on").checked;
  document.querySelector("#view-scenes .status-line")!.classList.toggle("live", on);
  $("#auto-status").textContent = !on
    ? t("auto.allOff")
    : st && st.foreground
      ? [
          t("auto.foreground", { exe: st.foreground }),
          t(st.fullscreen ? "auto.fullscreen" : "auto.windowed"),
          st.gpu >= 0 ? `GPU ${st.gpu.toFixed(0)}%` : "",
          st.game ? t("auto.game") : "",
          st.rate >= 0 ? t("auto.rate", { rate: rateText(st.rate) }) : "",
          st.scene ? t("auto.scene", { name: st.scene }) : "",
        ]
          .filter(Boolean)
          .join(" · ")
      : t("auto.waiting");
}

// ---------- settings ----------

Settings.autostart().then((on) => ($<HTMLInputElement>("#autostart").checked = on));
$<HTMLInputElement>("#autostart").onchange = (e) => {
  const on = (e.target as HTMLInputElement).checked;
  attempt(() => Settings.setAutostart(on), t(on ? "settings.autostartOn" : "settings.autostartOff"));
};
Settings.notify().then((on) => ($<HTMLInputElement>("#notify").checked = on));
$<HTMLInputElement>("#notify").onchange = (e) => {
  const on = (e.target as HTMLInputElement).checked;
  attempt(() => Settings.setNotify(on), t(on ? "settings.notifyOn" : "settings.notifyOff"));
};

const langSel = $<HTMLSelectElement>("#language");
langSel.innerHTML = [
  ["", t("settings.langSystem")],
  ["zh-CN", "简体中文"],
  ["en", "English"],
]
  .map(([v, label]) => option(v!, label!, v === lang.choice))
  .join("");
// Text is rendered all over the page; reloading redraws it in the new language.
langSel.onchange = () => attempt(() => Settings.setLanguage(langSel.value)).then((ok) => ok && location.reload());

// ---------- start ----------

await loadDevices();

// Tray clicks: a device row opens its page, the status row the scenes page.
function goTo(target: string) {
  if (target === "scenes") return showView("scenes");
  const d = list.find((x) => x.id === target);
  if (d) openDevice(d);
}
events.openTarget.on(async (target) => {
  await loadDevices();
  goTo(target);
});
const target = await Devices.takeTarget().catch(() => "");
if (target) goTo(target);
setInterval(() => refreshState(false), 20_000); // battery; the window exists only while open
setInterval(autoStatus, 1_500);
