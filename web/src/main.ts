import { isCallError } from "mygo-runtime";
import {
  Audio,
  Devices,
  events,
  Keyboards,
  Macros,
  Settings,
  Scenes,
  Lights,
  Via,
  type AppInfo,
  type AudioDevices,
  type AudioState,
  type Band,
  type EQ,
  type EQPreset,
  type ButtonAction,
  type ButtonInfo,
  type Macro,
  type MacroEvent,
  type DeviceInfo,
  type DeviceState,
  type DPIStage,
  type KeyboardState,
  type Effect as KbEffect,
  type Key as KbKey,
  type KeyboardKeys,
  type RGB,
  type Scene,
  type ScenesConfig,
  type SmartPoll,
  type Tracking,
} from "./mygo";
import { setLang, t, translateDom } from "./i18n";

// Language first: everything below renders text.
const lang = await Settings.language().catch(() => ({
  choice: "",
  effective: navigator.language.startsWith("zh") ? "zh-CN" : navigator.language.startsWith("ja") ? "ja" : "en",
}));
setLang(lang.effective);
translateDom();
// The theme before anything paints: its page is translucent over the window material.
const glass = await Settings.glass().catch(() => ({ supported: false, on: false }));
document.documentElement.classList.toggle("glass", glass.on);

const $ = <T extends HTMLElement = HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
let stopCapture = () => {}; // ends a key capture in progress (captureKey)
const RATES = [125, 500, 1000, 2000, 4000, 8000];
const MAX_STAGES = 5;
const CATEGORIES = ["mouse", "keyboard", "mouse-mat", "accessory", "monitor", "audio"] as const;
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

// Show the scrollbar while an element scrolls (style.css hides it otherwise).
const scrollTimers = new WeakMap<Element, number>();
document.addEventListener(
  "scroll",
  (e) => {
    const el = e.target as HTMLElement;
    if (!el?.classList) return;
    el.classList.add("scrolling");
    clearTimeout(scrollTimers.get(el));
    scrollTimers.set(el, window.setTimeout(() => el.classList.remove("scrolling"), 900));
  },
  true, // capture: scroll events on nested elements do not bubble
);

function showView(name: string) {
  stopCapture();
  if (recording && name !== "macros") $("#macro-record").click(); // leaving stops a recording
  document.querySelectorAll<HTMLElement>(".view").forEach((v) => (v.hidden = v.id !== `view-${name}`));
  const rail = name === "device" ? "devices" : name;
  document.querySelectorAll<HTMLElement>(".nav").forEach((x) => x.classList.toggle("active", x.dataset.view === rail));
  if (name === "devices") loadDevices();
  if (name === "scenes") loadScenes();
  if (name === "macros") loadMacros();
  if (name === "about") loadAbout();
  if (name === "settings") loadSound();
}

// Coming back to Devices from another page shows the device left open; a
// click on Devices while already there goes to the list.
document.querySelectorAll<HTMLButtonElement>(".nav").forEach(
  (b) =>
    (b.onclick = () => {
      const here = document.querySelector<HTMLElement>(".nav.active")?.dataset.view;
      if (b.dataset.view === "devices" && here !== "devices" && dev && list.some((d) => d.id === dev!.id)) {
        showView("device");
        refreshState();
        return;
      }
      if (b.dataset.view === "devices") dev = undefined;
      showView(b.dataset.view!);
    }),
);
$("#back").onclick = () => {
  dev = undefined;
  showView("devices");
};

function showTab(name: string) {
  stopCapture();
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
  audio: '<path d="M4 14v-2a8 8 0 0 1 16 0v2" /><rect x="3" y="14" width="4" height="6" rx="1.5" /><rect x="17" y="14" width="4" height="6" rx="1.5" />',
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

// Devices in groups by category, in CATEGORIES order; unknown categories last.
function renderOverview() {
  $("#no-device").hidden = list.length > 0;
  const rank = (c: string) => {
    const i = (CATEGORIES as readonly string[]).indexOf(c);
    return i < 0 ? CATEGORIES.length : i;
  };
  const cats = [...new Set(list.map((d) => d.category))].sort((a, b) => rank(a) - rank(b));
  $("#device-grid").replaceChildren(
    ...cats.map((c) => {
      const group = document.createElement("section");
      const n = list.filter((d) => d.category === c).length;
      group.innerHTML = `<h2 class="group-title"><span></span><small>${n}</small></h2><div class="device-grid"></div>`;
      group.querySelector("h2 span")!.textContent = category(c);
      group.querySelector(".device-grid")!.replaceChildren(...list.filter((d) => d.category === c).map(deviceCard));
      return group;
    }),
  );
}

// A card shows what the device does right now: DPI and polling rate, or the
// format and EQ of a dongle, and the battery as a bar.
function deviceCard(d: DeviceInfo) {
  const card = document.createElement("button");
  card.className = "device-card";
  card.innerHTML = `
    <div class="dc-head"><svg viewBox="0 0 24 24">${ICONS[d.category] ?? ICONS.accessory}</svg><div class="dc-name"></div></div>
    <div class="dc-chips"></div>
    <div class="dc-battery" hidden><span class="muted small"></span><i><b></b></i></div>`;
  card.querySelector(".dc-name")!.textContent = d.name;
  card.onclick = () => openDevice(d);
  const chips = (...texts: string[]) =>
    card.querySelector(".dc-chips")!.replaceChildren(...texts.map((x) => Object.assign(document.createElement("span"), { className: "chip", textContent: x })));
  if (d.category === "audio") {
    Promise.all([Audio.state(d.id), Audio.presets()])
      .then(([st, mine]) => {
        const presets = mine ?? [];
        const eqOf = (r: string) => (r.startsWith("b:") ? BUILTIN_PRESETS[r.slice(2) as keyof typeof BUILTIN_PRESETS] : presets.find((p) => `u:${p.name}` === r)?.eq);
        const ref = [...Object.keys(BUILTIN_PRESETS).map((k) => `b:${k}`), ...presets.map((p) => `u:${p.name}`)].find((r) => {
          const p = eqOf(r);
          return !!p && sameEQ(p, st.eq);
        });
        chips(...(st.format ? [`${st.format.bits} bit · ${st.format.rate / 1000} kHz`] : []), `${t("tab.eq")} ${ref ? presetLabel(ref) : t("eq.custom")}`);
      })
      .catch(() => chips(t("overview.noAnswer")));
    return card;
  }
  if (isKeyboard(d)) {
    kbApi(d)
      .state(d.id)
      .then((s) => {
        const e = s.effects.find((x) => x.id === s.lighting.effect);
        chips(e ? kbEffectName(e.name) : t("kb.effect.other"));
      })
      .catch(() => chips(t("overview.noAnswer")));
    return card;
  }
  const f = d.features;
  if (!f.dpi && !f.pollRate && !f.battery) return card;
  Devices.state(d.id)
    .then((s) => {
      chips(...(f.dpi ? [`${s.dpi.x} DPI`] : []), ...(f.pollRate && s.pollRate ? [hz(s.pollRate)] : []));
      if (s.battery < 0) return;
      const bat = card.querySelector<HTMLElement>(".dc-battery")!;
      bat.hidden = false;
      bat.querySelector("span")!.textContent = `${s.charging ? "⚡ " : ""}${t("overview.battery", { pct: s.battery })}`;
      const fill = bat.querySelector<HTMLElement>("b")!;
      fill.style.width = `${s.battery}%`;
      fill.classList.toggle("low", s.battery <= 20 && !s.charging);
    })
    .catch(() => chips(t("overview.noAnswer")));
  return card;
}
$("#rescan").onclick = () => loadDevices(true);

// Plugging or unplugging: refresh the overview, and never leave a page up for
// a device that is gone. A mouse moving between cable and receiver changes ID,
// so its page follows it to the other connection.
events.devicesChanged.on((next) => {
  list = next;
  renderOverview();
  if (!$("#view-scenes").hidden) loadScenes(); // its keyboard and EQ sections follow the devices
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
  $("#dev-model").hidden = true;
  state = undefined;
  if (d.category === "audio") return openAudio(d);
  if (isKeyboard(d)) return openKeyboard(d);
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
  loadButtons(d);
  loadInfo(d);
  loadLift(d);
  if (!smart) smart = await Scenes.smart().catch(() => undefined);
  await refreshState();
}

// full=false only refreshes battery, so a periodic read never clobbers an edit in progress.
async function refreshState(full = true) {
  if (!dev || $("#view-device").hidden) return;
  if (dev.category === "audio") return full ? loadAudio(dev.id) : undefined;
  if (isKeyboard(dev)) return full ? loadKeyboard(dev.id) : undefined;
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

const saveDPI = debounce((id: string, v: number) => attempt(() => Devices.setDPI(id, v, v)), 300);
$<HTMLInputElement>("#dpi").oninput = (e) => {
  if (!dev) return;
  const v = +(e.target as HTMLInputElement).value;
  $("#dpi-val").textContent = String(v);
  saveDPI(dev.id, v);
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

const saveBrightness = debounce((id: string, v: number) => attempt(() => Devices.setBrightness(id, v)), 200);
$<HTMLInputElement>("#brightness").oninput = (e) => {
  if (!dev) return;
  const v = +(e.target as HTMLInputElement).value;
  $("#bright-val").textContent = String(v);
  saveBrightness(dev.id, v);
};

// --- device info: firmware, serial, receiver pairing ---

async function loadInfo(d: DeviceInfo) {
  const tab = document.querySelector<HTMLElement>('.tab[data-tab="info"]')!;
  tab.hidden = true;
  const leobog = isKeyboard(d);
  $("#info-hint").textContent = t(!leobog ? "info.firmwareHint" : d.name.startsWith("LEOBOG ") ? "info.firmwareHintLeobog" : "info.firmwareHintKeyboard");
  $("#info-model-row").hidden = !leobog;
  $("#info-model").textContent = leobog ? `${d.name} · USB ${d.id}` : "";
  const info = await (leobog ? kbApi(d).details(d.id) : Devices.details(d.id)).catch(() => undefined);
  if (!info || dev?.id !== d.id || (!info.firmware && !info.serial)) return;
  tab.hidden = false;
  document.querySelector<HTMLElement>(".tabs")!.hidden = false;
  $("#info-fw-row").hidden = !info.firmware;
  $("#info-fw").textContent = info.firmware ? `v${info.firmware}` : "";
  $("#info-sn-row").hidden = !info.serial;
  $("#info-sn").textContent = info.serial;
  $("#info-pair-row").hidden = !info.pairing;
  $("#info-sn-copy").onclick = () => attempt(() => navigator.clipboard.writeText(info.serial), t("info.copied"));
}
$("#info-fw-update").onclick = () =>
  dev && attempt(() => (dev!.driver === "leobog" ? Keyboards.openSupport(dev!.id) : Devices.openSupport(dev!.id, "firmware")));
$("#info-pair").onclick = () => dev && attempt(() => Devices.openSupport(dev!.id, "pairing"));

// --- lift-off distance ---

const LIFT_DEFAULT: Tracking = { asymmetric: false, distance: 2, liftOff: 13, landing: 12 };
let lift: Tracking | undefined;

async function loadLift(d: DeviceInfo) {
  $("#card-lift").hidden = true;
  lift = await Devices.tracking(d.id).catch(() => undefined);
  if (!lift || dev?.id !== d.id) return;
  $("#card-lift").hidden = false;
  renderLift();
}

function renderLift() {
  const l = lift!;
  $<HTMLInputElement>("#lift-asym").checked = l.asymmetric;
  $("#lift-sym").hidden = l.asymmetric;
  $("#lift-asym-body").hidden = !l.asymmetric;
  segmented($("#lift-sym"), [1, 2, 3], l.distance, (v) => t(`lift.${v}` as "lift.1"), (v) => ((l.distance = v), saveLift()));
  const off = $<HTMLInputElement>("#lift-off"), land = $<HTMLInputElement>("#lift-land");
  off.value = String(l.liftOff);
  land.value = String(l.landing);
  $("#lift-off-val").textContent = `${l.liftOff}`;
  $("#lift-land-val").textContent = `${l.landing}`;
}

const saveLift = debounce(() => dev && lift && attempt(() => Devices.setTracking(dev!.id, lift!), t("lift.saved")), 300);

$<HTMLInputElement>("#lift-asym").onchange = (e) => {
  if (!lift) return;
  lift.asymmetric = (e.target as HTMLInputElement).checked;
  renderLift();
  saveLift();
};
// Landing stays below lift-off, as Synapse keeps it.
$<HTMLInputElement>("#lift-off").oninput = (e) => {
  if (!lift) return;
  lift.liftOff = +(e.target as HTMLInputElement).value;
  if (lift.landing >= lift.liftOff) lift.landing = lift.liftOff - 1;
  renderLift();
  saveLift();
};
$<HTMLInputElement>("#lift-land").oninput = (e) => {
  if (!lift) return;
  lift.landing = +(e.target as HTMLInputElement).value;
  if (lift.liftOff <= lift.landing) lift.liftOff = lift.landing + 1;
  renderLift();
  saveLift();
};
$("#lift-reset").onclick = () => {
  if (!lift) return;
  lift = { ...LIFT_DEFAULT };
  renderLift();
  saveLift();
};

// --- buttons ---

const buttonName = (id: number) => ([1, 2, 3, 4, 5, 9, 10, 96].includes(id) ? t(`btn.${id}` as "btn.1") : t("btn.other", { n: id }));

// The tab shows only for devices that answer Synapse's button commands. Each
// button plays a macro, or is remapped on the device: to a mouse button, a
// DPI action or a key (which works without OpenSynapse).
const DPI_ACTIONS = [1, 2, 6, 7];
const MOUSE_TARGETS = [1, 2, 3, 4, 5, 9, 10];
const MODIFIER_VKS = [16, 17, 18, 91, 92];
const modNames = (mods: number) => (["Ctrl", "Shift", "Alt", "Win"] as const).filter((_, i) => mods & (1 << i));
const keyLabel = (vk: number, mods: number) => [...modNames(mods), ...(vk && !MODIFIER_VKS.includes(vk) ? [vkName(vk)] : [])].join("+");

function actionValue(a: ButtonAction) {
  switch (a.kind) {
    case "macro":
      return `macro:${a.macro}`;
    case "mouse":
    case "dpi":
    case "media":
      return `${a.kind}:${a.code}`;
    case "key":
      return `key:${a.code}:${a.mods}`;
  }
  return a.kind; // default, other
}

function parseAction(v: string): ButtonAction {
  const [kind = "default", x = "", y = "0"] = v.split(":");
  if (kind === "macro") return { kind, macro: x, code: 0, mods: 0 };
  return { kind, macro: "", code: +x || 0, mods: +y || 0 };
}

// captureKey takes a key with the modifiers held, or modifiers alone (code
// 0), once every key pressed is released: a remap written while its key is
// down would leave that key stuck. Only one capture runs: picking anything
// else or starting another cancels it.
function captureKey(done: (code: number, mods: number) => void) {
  stopCapture();
  const MOD_BITS: Record<number, number> = { 17: 1, 16: 2, 18: 4, 91: 8, 92: 8 };
  let mods = 0;
  let code = 0;
  const down = new Set<number>();
  const onDown = (e: KeyboardEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.repeat) return;
    down.add(e.keyCode);
    if (MOD_BITS[e.keyCode]) mods |= MOD_BITS[e.keyCode]!;
    else code ||= e.keyCode;
  };
  const onUp = (e: KeyboardEvent) => {
    e.preventDefault();
    e.stopPropagation();
    down.delete(e.keyCode);
    if (down.size === 0 && (code || mods)) (stopCapture(), done(code, mods));
  };
  window.addEventListener("keydown", onDown, true);
  window.addEventListener("keyup", onUp, true);
  stopCapture = () => {
    window.removeEventListener("keydown", onDown, true);
    window.removeEventListener("keyup", onUp, true);
    stopCapture = () => {};
  };
}

function buttonOptions(act: ButtonAction, layer: number, macros: Macro[]) {
  const cur = actionValue(act);
  const group = (label: string, items: [string, string][]) =>
    items.length ? `<optgroup label="${label}">${items.map(([v, l]) => option(v, l, v === cur)).join("")}</optgroup>` : "";
  const keyItems: [string, string][] = act.kind === "key" ? [[cur, keyLabel(act.code, act.mods)]] : [];
  return (
    option("default", t("buttons.default"), cur === "default") +
    (act.kind === "other" ? option("other", t("buttons.other"), true) : "") +
    group(t("buttons.groupMouse"), MOUSE_TARGETS.map((n) => [`mouse:${n}`, buttonName(n)])) +
    group("DPI", DPI_ACTIONS.map((n) => [`dpi:${n}`, t(`dpiAction.${n}` as "dpiAction.1")])) +
    (layer === 0 ? group("Hypershift", [["hypershift", t("buttons.hypershift")]]) : "") +
    group(t("buttons.groupKey"), [...keyItems, ["capture", t("buttons.pressKey")]]) +
    group(t("buttons.groupMacro"), macros.map((m) => [`macro:${m.id}`, esc(m.name)]))
  );
}

// Which layer the Buttons tab edits: 0 normal, 1 while Hypershift is held.
let buttonLayer = 0;

async function loadButtons(d: DeviceInfo) {
  const tab = document.querySelector<HTMLElement>('.tab[data-tab="buttons"]')!;
  tab.hidden = true;
  let buttons: ButtonInfo[];
  let all: Macro[];
  try {
    [buttons, all] = await Promise.all([Devices.buttons(d.id), Macros.list()]);
  } catch {
    return;
  }
  if (dev?.id !== d.id) return;
  tab.hidden = false;
  document.querySelector<HTMLElement>(".tabs")!.hidden = false;
  const renderList = () => {
    const layer = buttonLayer;
    const hasShift = buttons.some((b) => b.action.kind === "hypershift");
    $("#button-layer-hint").textContent = layer === 1 && !hasShift ? t("buttons.layerNoShift") : t(layer ? "buttons.layerShiftHint" : "buttons.hint");
    $("#button-list").replaceChildren(
      ...buttons.map((b) => {
        const row = document.createElement("label");
        row.className = "button-row";
        const sel = document.createElement("select");
        const cur = () => (layer ? b.shift : b.action);
        const render = () => (sel.innerHTML = buttonOptions(cur(), layer, all));
        render();
        sel.disabled = b.id === 1 && layer === 0; // the left button keeps clicking
        const apply = (act: ButtonAction) =>
          attempt(() => Devices.setButton(d.id, b.id, layer, act), t("buttons.saved")).then((ok) => {
            if (ok) {
              if (layer) b.shift = act;
              else b.action = act;
            }
            if (ok && layer === 0) renderList(); // the Hypershift hint may change
            else render();
          });
        sel.onchange = () => {
          stopCapture();
          if (sel.value !== "capture") return void apply(parseAction(sel.value));
          sel.options[sel.selectedIndex]!.textContent = t("macros.pressKey");
          captureKey((code, mods) => apply({ kind: "key", macro: "", code, mods }));
        };
        const name = document.createElement("span");
        name.textContent = buttonName(b.id);
        row.append(name, sel);
        return row;
      }),
    );
  };
  segmented($("#button-layer"), [0, 1], buttonLayer, (v) => t(v ? "buttons.layerShift" : "buttons.layerNormal"), (v) => {
    buttonLayer = v;
    renderList();
  });
  renderList();
}

// ---------- keyboards: BY-520 (Keyboards) and VIA (Via) ----------

const isKeyboard = (d: DeviceInfo) => ["leobog", "via", "lights"].includes(d.driver ?? "");
// The page drives every kind; VIA keyboards lack per-key colors and a factory
// reset, and those of Lights have their lighting alone.
const kbApi = (d: DeviceInfo | undefined = dev) => (d?.driver === "via" ? Via : d?.driver === "lights" ? Lights : Keyboards);

let kb: KeyboardState | undefined;
function kbEffectName(name: string) {
  return t(`kb.effect.${name}` as "kb.effect.off") || name;
}

function openKeyboard(d: DeviceInfo) {
  $("#dev-cat").textContent = category(d.category);
  $("#dev-name").textContent = d.name;
  $("#dev-conn").textContent = "";
  $("#dev-battery").hidden = true;
  document.querySelectorAll<HTMLElement>(".tab").forEach((x) => (x.hidden = !["kblight", "kbkeys", "info"].includes(x.dataset.tab!)));
  document.querySelector<HTMLElement>(".tabs")!.hidden = false;
  showTab("kblight");
  showView("device");
  kb = undefined;
  kbSel = -1;
  loadInfo(d);
  return loadKeyboard(d.id);
}

let kbKeys: KeyboardKeys | undefined;
let kbColors: RGB[] = []; // the custom effect's, by matrix index
let kbMacros: Macro[] = [];

async function loadKeyboard(id: string) {
  if (dev?.driver === "via") {
    try {
      const vm = await Via.models(id);
      if (dev?.id !== id) return;
      const model = $<HTMLSelectElement>("#dev-model");
      model.hidden = !vm.models?.length;
      model.innerHTML = (vm.models ?? []).map((m) => option(esc(m), esc(m), m === vm.model)).join("");
      model.onchange = async () => {
        if (!(await attempt(() => Via.setModel(id, model.value)))) return;
        $("#dev-name").textContent = model.value;
        kbSel = -1;
        loadKeyboard(id);
      };
    } catch {}
  } else {
    $("#dev-model").hidden = true;
  }
  let st: KeyboardState;
  try {
    [st, kbKeys, kbColors, kbMacros] = await Promise.all([kbApi().state(id), kbApi().keys(id), kbApi().keyColors(id), Macros.list()]);
  } catch (e) {
    toast(isCallError(e) ? e.message : String(e), true);
    return;
  }
  if (dev?.id !== id) return; // switched device meanwhile
  kb = st;
  // Remaps and the factory reset need the model's factory keys; most BY-520 models lack them.
  document.querySelector<HTMLElement>('.tab[data-tab="kbkeys"]')!.hidden = !kbKeys.remap;
  $("#kb-factory").hidden = !kbKeys.reset;
  $("#kb-music-card").hidden = !st.perKey;
  $("#kb-colorful-label").hidden = !st.colorful;
  // A keyboard without lighting controls opens on its keys.
  const lit = st.effects.length > 0;
  document.querySelector<HTMLElement>('.tab[data-tab="kblight"]')!.hidden = !lit;
  if (!lit && kbKeys.remap && !$("#tab-kblight").hidden) showTab("kbkeys");
  document.querySelector<HTMLElement>("#tab-kbkeys .info")!.title = t(dev.driver === "via" ? "kb.keysMoreVia" : "kb.keysMore");
  renderKeyboard();
  renderKbKeys();
}

// kbDraw lays the keys out at their place on the keyboard, scaled to the box.
type KbLayout = { keys: KbKey[]; width: number; height: number };
function kbDraw(box: HTMLElement, layout: KbLayout, look: (b: HTMLButtonElement, k: KbKey) => void, onPick: (k: KbKey, b: HTMLButtonElement) => void) {
  const { keys, width: w, height: h } = layout;
  box.style.aspectRatio = `${w} / ${h}`;
  box.replaceChildren(
    ...keys.map((k) => {
      const b = document.createElement("button");
      Object.assign(b.style, { left: `${(k.x / w) * 100}%`, top: `${(k.y / h) * 100}%`, width: `${(k.w / w) * 100}%`, height: `${(k.h / h) * 100}%` });
      b.title = k.name;
      look(b, k);
      b.onclick = () => onPick(k, b);
      return b;
    }),
  );
}

// --- custom effect: paint keys ---

let paintHex = "#ff2d2d";
let painting = false;
window.addEventListener("pointerup", () => (painting = false));
// Each save is a flash write on the keyboard: wait until painting pauses.
const saveKeyColors = debounce(() => dev && attempt(() => Keyboards.setKeyColors(dev!.id, kbColors)), 600);

function renderPaint() {
  const paint = (k: KbKey, b: HTMLButtonElement) => {
    kbColors[k.index] = fromHex(paintHex);
    b.style.background = paintHex;
    saveKeyColors();
  };
  kbDraw(
    $("#kb-paint"),
    kbKeys!,
    (b, k) => {
      b.style.background = toHex(kbColors[k.index] ?? { r: 0, g: 0, b: 0 });
      b.onpointerdown = () => (painting = true);
      b.onpointerenter = () => painting && paint(k, b); // drag across keys to paint them
    },
    paint,
  );
  $("#kb-paint-swatches").replaceChildren(
    ...["#000000", ...SWATCHES].map((hex) => {
      const s = document.createElement("button");
      s.className = "swatch" + (hex === paintHex ? " on" : "");
      s.style.background = hex;
      s.title = hex === "#000000" ? t("kb.paintOff") : hex;
      s.onclick = () => ((paintHex = hex), renderPaint());
      return s;
    }),
  );
  const pick = $<HTMLInputElement>("#kb-paint-color");
  pick.value = paintHex;
  pick.oninput = () => ((paintHex = pick.value), renderPaint());
  $("#kb-paint-all").onclick = () => {
    for (const k of kbKeys!.keys) kbColors[k.index] = fromHex(paintHex);
    renderPaint();
    saveKeyColors();
  };
}

// --- keys ---

let kbLayer = 0;
let kbSel = -1; // matrix index of the key being edited; the key list is reloaded after changes
const MEDIA_CODES = [0xe9, 0xea, 0xe2, 0xcd, 0xb5, 0xb6, 0xb7, 0x6f, 0x70];

function kbKeyOptions(act: ButtonAction) {
  const cur = actionValue(act);
  const group = (label: string, items: [string, string][]) =>
    items.length ? `<optgroup label="${label}">${items.map(([v, l]) => option(v, l, v === cur)).join("")}</optgroup>` : "";
  const keyItems: [string, string][] = act.kind === "key" ? [[cur, keyLabel(act.code, act.mods)]] : [];
  return (
    option("default", t("buttons.default"), cur === "default") +
    (act.kind === "other" ? option("other", t("buttons.other"), true) : "") +
    group(t("buttons.groupKey"), [...keyItems, ["capture", t("buttons.pressKey")]]) +
    group(t("kb.media"), MEDIA_CODES.map((c) => [`media:${c}`, t(`kb.media.${c}` as "kb.media.233")])) +
    group(t("buttons.groupMacro"), kbMacros.map((m) => [`macro:${m.id}`, esc(m.name)]))
  );
}

function renderKbKeys() {
  stopCapture();
  if (!kbKeys || !dev) return;
  const keys = kbKeys;
  const id = dev.id;
  segmented($("#kb-layer"), [0, 1], kbLayer, (v) => t(v ? "kb.layerFn" : "buttons.layerNormal"), (v) => ((kbLayer = v), renderKbKeys()));
  const acts = kbLayer ? keys.fn : keys.normal;
  kbDraw(
    $("#kb-keys"),
    keys,
    (b, k) => {
      const a = acts[keys.keys.indexOf(k)]!;
      b.textContent = k.name;
      b.disabled = k.index === keys.fnIndex; // Fn switches the layer
      b.classList.toggle("changed", a.kind !== "default");
      b.classList.toggle("on", kbSel === k.index);
    },
    (k) => ((kbSel = k.index), renderKbKeys()),
  );
  const i = keys.keys.findIndex((k) => k.index === kbSel);
  $(".kb-key-edit").hidden = i < 0;
  if (i < 0) return;
  const k = keys.keys[i]!;
  const layer = kbLayer;
  $("#kb-key-name").textContent = k.name;
  const sel = $<HTMLSelectElement>("#kb-key-action");
  sel.innerHTML = kbKeyOptions(acts[i]!);
  const apply = (act: ButtonAction) =>
    attempt(() => kbApi().setKey(id, layer, k.index, act), t("buttons.saved")).then((ok) => {
      if (ok) (layer ? keys.fn : keys.normal)[i] = act;
      renderKbKeys();
    });
  sel.onchange = () => {
    stopCapture();
    if (sel.value !== "capture") return void apply(parseAction(sel.value));
    sel.options[sel.selectedIndex]!.textContent = t("macros.pressKey");
    captureKey((code, mods) => apply({ kind: "key", macro: "", code, mods }));
  };
}

// Factory reset takes a second click within 4 s.
let factoryArmed = 0;
$("#kb-factory").onclick = async () => {
  const b = $("#kb-factory");
  if (!factoryArmed) {
    b.textContent = t("kb.factoryConfirm");
    factoryArmed = window.setTimeout(() => ((factoryArmed = 0), (b.textContent = t("kb.factory"))), 4000);
    return;
  }
  clearTimeout(factoryArmed);
  factoryArmed = 0;
  if (!dev) return;
  const id = dev.id;
  // The reset takes a few seconds while the keyboard stores it; the page waits,
  // until it shows the reset settings: an edit before would send the old ones back.
  const view = $("#view-device");
  view.inert = true;
  b.textContent = t("kb.factoryRunning");
  try {
    if (!(await attempt(() => Keyboards.reset(id), t("kb.factoryDone")))) return;
    kbSel = -1;
    if (dev?.id === id) await loadKeyboard(id);
  } finally {
    view.inert = false;
    b.textContent = t("kb.factory");
  }
};

$("#kb-keys-reset").onclick = async () => {
  if (!dev || !kbKeys) return;
  if (!(await attempt(() => kbApi().resetKeys(dev!.id), t("buttons.saved")))) return;
  kbSel = -1;
  loadKeyboard(dev.id);
};

// Every control writes the whole lighting; sliders wait for a pause.
const saveKeyboard = debounce(() => kb && dev && attempt(() => kbApi().setLighting(dev!.id, kb!.lighting)), 150);

const MUSIC_EFFECTS = ["pulse", "spectrum", "bass"];

function renderMusic() {
  const s = kb!;
  const sel = $<HTMLSelectElement>("#kb-music");
  sel.innerHTML = option("", t("kb.musicOff"), !s.music) + MUSIC_EFFECTS.map((e) => option(e, t(`kb.music.${e}` as "kb.music.pulse"), e === s.music)).join("");
  const color = $<HTMLInputElement>("#kb-music-color");
  color.value = toHex(s.musicColor.r || s.musicColor.g || s.musicColor.b ? s.musicColor : { r: 0x44, g: 0xd6, b: 0x2c });
  color.hidden = s.music !== "pulse"; // the other effects pick their own colors
  const save = () => {
    const id = dev!.id;
    s.music = sel.value;
    s.musicColor = fromHex(color.value);
    color.hidden = s.music !== "pulse";
    attempt(() => Keyboards.setMusic(id, s.music, s.musicColor));
  };
  sel.onchange = save;
  color.onchange = save;
  const notices = $<HTMLInputElement>("#kb-notices");
  notices.checked = s.notices;
  notices.onchange = () => {
    const id = dev!.id;
    attempt(() => Keyboards.setNotices(id, notices.checked)).then((ok) => (ok ? (s.notices = notices.checked) : (notices.checked = s.notices)));
  };
}

function renderKeyboard() {
  const s = kb!;
  renderMusic();
  const l = s.lighting;
  const fx = s.effects.find((e) => e.id === l.effect);
  const sel = $<HTMLSelectElement>("#kb-effect");
  // An effect set elsewhere that this app does not offer (per-key colors) shows as is.
  sel.innerHTML = (fx ? "" : option(l.effect, t("kb.effect.other"), true)) + s.effects.map((e) => option(e.id, kbEffectName(e.name), e.id === l.effect)).join("");
  sel.onchange = async () => {
    const id = dev!.id;
    // Each effect keeps its own brightness, speed and color on the keyboard.
    const stored = await kbApi().stored(id, +sel.value).catch(() => ({ ...l, effect: +sel.value }));
    if (dev?.id !== id) return;
    s.lighting = stored;
    const e = s.effects.find((x) => x.id === stored.effect);
    if (e && !e.color) s.lighting.colorful = true;
    renderKeyboard();
    saveKeyboard();
  };
  const custom = fx?.name === "custom";
  const on = !!fx && l.effect !== 0 && !custom; // custom is painted below
  $("#kb-paint-card").hidden = !custom || !kbKeys;
  if (custom && kbKeys) renderPaint();
  $("#kb-color-row").hidden = !on;
  $("#kb-bright-row").hidden = !on;
  $("#kb-speed-row").hidden = !on || !fx!.speed;
  const colorful = $<HTMLInputElement>("#kb-colorful");
  colorful.checked = l.colorful;
  colorful.disabled = !fx?.color; // these effects only cycle colors
  colorful.onchange = () => ((l.colorful = colorful.checked), renderKeyboard(), saveKeyboard());
  const pick = (hex: string) => {
    l.color = fromHex(hex);
    l.colorful = false;
    renderKeyboard();
    saveKeyboard();
  };
  $("#kb-swatches").replaceChildren(
    ...SWATCHES.map((hex) => {
      const b = document.createElement("button");
      b.className = "swatch" + (!l.colorful && toHex(l.color) === hex ? " on" : "");
      b.style.background = hex;
      b.title = hex;
      b.disabled = !fx?.color;
      b.onclick = () => pick(hex);
      return b;
    }),
  );
  const color = $<HTMLInputElement>("#kb-color");
  color.value = toHex(l.color);
  color.disabled = !fx?.color;
  color.oninput = () => pick(color.value);
  for (const [key, el, val] of [["brightness", "#kb-bright", "#kb-bright-val"], ["speed", "#kb-speed", "#kb-speed-val"]] as const) {
    const r = $<HTMLInputElement>(el);
    r.max = String(s.maxLevel);
    r.value = String(l[key]);
    $(val).textContent = `${l[key]} / ${s.maxLevel}`;
    r.oninput = () => {
      l[key] = +r.value;
      $(val).textContent = `${l[key]} / ${s.maxLevel}`;
      saveKeyboard();
    };
  }
}

// ---------- audio dongles ----------

let audio: AudioState | undefined;
let userPresets: EQPreset[] = [];
let chosenPreset = ""; // the menu value picked or saved last: presets can hold the same EQ
const BAND_TYPES = ["lowShelf", "peak", "highShelf"] as const;
const FLAT_FREQS = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000];

// The preamp that keeps the loudest point of the curve at or below 0 dB, so boosts never clip.
function headroom(bands: Band[]) {
  let peak = 0;
  for (let i = 0; i <= 300; i++) {
    const f = 20 * 1000 ** (i / 300);
    peak = Math.max(peak, bands.reduce((sum, b) => sum + bandDb(b, f), 0));
  }
  return -Math.min(10, Math.ceil(peak - 0.01));
}

// A flat EQ with some bands changed. Keys in the order the Go side sends them,
// so presets compare equal as JSON.
function eqWith(changes: Record<number, Partial<Band>> = {}): EQ {
  const bands = FLAT_FREQS.map((freq, i) => ({ freq, q: 0.75, gain: 0, type: "peak", ...changes[i] }));
  return { bands, preamp: headroom(bands) };
}
const BUILTIN_PRESETS = {
  flat: eqWith(),
  bass: eqWith({ 0: { type: "lowShelf", freq: 105, q: 0.7, gain: 6 } }),
  treble: eqWith({ 9: { type: "highShelf", freq: 8000, q: 0.7, gain: 4 } }),
  vocal: eqWith({ 1: { gain: -2 }, 3: { gain: -1 }, 5: { q: 1, gain: 2 }, 6: { q: 1, gain: 3 }, 7: { gain: 1.5 } }),
  fps: eqWith({ 0: { type: "lowShelf", freq: 105, q: 0.7, gain: -4 }, 6: { q: 1.4, gain: 3 }, 7: { q: 1.4, gain: 4 }, 8: { gain: 2 } }),
};
const sameEQ = (a: EQ, b: EQ) => JSON.stringify(a) === JSON.stringify(b);
const signed = (v: number) => (v > 0 ? `+${v}` : `${v}`);

function openAudio(d: DeviceInfo) {
  $("#dev-cat").textContent = category(d.category);
  $("#dev-name").textContent = d.name;
  $("#dev-conn").textContent = "";
  $("#dev-battery").hidden = true;
  document.querySelectorAll<HTMLElement>(".tab").forEach((x) => (x.hidden = !["eq", "audio"].includes(x.dataset.tab!)));
  document.querySelector<HTMLElement>(".tabs")!.hidden = false;
  showTab("eq");
  showView("device");
  audio = undefined;
  chosenPreset = "";
  $("#eq-bands").replaceChildren();
  return loadAudio(d.id);
}

async function loadAudio(id: string) {
  let st: AudioState;
  try {
    userPresets = (await Audio.presets()) ?? [];
    st = await Audio.state(id);
  } catch (e) {
    toast(isCallError(e) ? e.message : String(e), true);
    return;
  }
  if (dev?.id !== id) return; // switched device meanwhile
  audio = st;
  $("#dev-conn").textContent = st.firmware ? t("audio.firmware", { v: st.firmware }) : "";
  renderEQ();
  renderAudio();
}

// Every change saves the whole EQ; the dongle stores it, so wait for a pause.
const saveEQ = debounce((id: string, eq: AudioState["eq"]) => attempt(() => Audio.setEQ(id, eq)), 500);
const eqChanged = () => {
  renderCurve();
  renderPresets();
  if (dev && audio) saveEQ(dev.id, audio.eq);
};

function renderEQ() {
  const eq = audio!.eq;
  const pre = $<HTMLInputElement>("#eq-preamp");
  pre.value = String(eq.preamp);
  $("#eq-preamp-val").textContent = signed(eq.preamp);
  pre.oninput = () => ($("#eq-preamp-val").textContent = signed(+pre.value));
  pre.onchange = () => ((eq.preamp = +pre.value), eqChanged());
  $("#eq-bands").replaceChildren(...eq.bands.map(bandRow));
  renderCurve();
  renderPresets();
}

const presetLabel = (ref: string) => (ref.startsWith("b:") ? t(`eq.preset.${ref.slice(2)}` as "eq.preset.flat") || ref.slice(2) : ref.slice(2));
const deviceName = (devs: { id: string; name: string }[], id: string) => devs.find((d) => d.id === id)?.name ?? t("scenes.deviceGone");
const presetEQ = (v: string) =>
  v.startsWith("b:") ? BUILTIN_PRESETS[v.slice(2) as keyof typeof BUILTIN_PRESETS] : userPresets.find((x) => `u:${x.name}` === v)?.eq;

// The preset menu shows the preset the EQ matches, or "Custom". The one picked
// last wins while it still matches; otherwise the first match.
function renderPresets() {
  const eq = audio!.eq;
  const values = [...Object.keys(BUILTIN_PRESETS).map((k) => `b:${k}`), ...userPresets.map((p) => `u:${p.name}`)];
  const matches = (v: string) => { const p = presetEQ(v); return !!p && sameEQ(p, eq); };
  const current = matches(chosenPreset) ? chosenPreset : (values.find(matches) ?? "");
  const sel = $<HTMLSelectElement>("#eq-preset");
  sel.innerHTML =
    (current ? "" : option("", t("eq.custom"), true)) +
    `<optgroup label="${t("eq.builtin")}">${Object.keys(BUILTIN_PRESETS).map((k) => option(`b:${k}`, t(`eq.preset.${k}` as "eq.preset.flat"), current === `b:${k}`)).join("")}</optgroup>` +
    (userPresets.length ? `<optgroup label="${t("eq.mine")}">${userPresets.map((p) => option(`u:${esc(p.name)}`, esc(p.name), current === `u:${p.name}`)).join("")}</optgroup>` : "");
  $<HTMLButtonElement>("#eq-preset-delete").disabled = !current.startsWith("u:");
}

$<HTMLSelectElement>("#eq-preset").onchange = (e) => {
  const v = (e.target as HTMLSelectElement).value;
  const p = presetEQ(v);
  if (!audio || !p) return;
  chosenPreset = v;
  audio.eq = structuredClone(p);
  renderEQ();
  eqChanged();
};

// Save asks for a name first, in place; a second Save or Enter stores it.
const presetName = $<HTMLInputElement>("#eq-preset-name");
async function savePreset() {
  if (!audio) return;
  if (presetName.hidden) {
    const v = $<HTMLSelectElement>("#eq-preset").value;
    presetName.value = v.startsWith("u:") ? v.slice(2) : "";
    presetName.hidden = false;
    presetName.focus();
    return;
  }
  const name = presetName.value.trim();
  const ok = await attempt(() => Audio.savePreset({ name, eq: audio!.eq }), t("eq.saved", { name }));
  if (!ok) return;
  presetName.hidden = true;
  chosenPreset = `u:${name}`;
  userPresets = (await Audio.presets().catch(() => userPresets)) ?? [];
  renderPresets();
}
$("#eq-preset-save").onclick = savePreset;
presetName.onkeydown = (e) => {
  if (e.key === "Enter") savePreset();
  if (e.key === "Escape") presetName.hidden = true;
};
$("#eq-preset-delete").onclick = async () => {
  const v = $<HTMLSelectElement>("#eq-preset").value;
  if (!v.startsWith("u:")) return;
  const name = v.slice(2);
  if (!(await attempt(() => Audio.deletePreset(name), t("eq.deleted", { name })))) return;
  userPresets = userPresets.filter((p) => p.name !== name);
  renderPresets();
};

function bandRow(b: Band) {
  const row = document.createElement("div");
  row.className = "eq-band";
  row.innerHTML = `
    <select class="eq-type">${BAND_TYPES.map((x) => option(x, t(`eq.${x}`), x === b.type)).join("")}</select>
    <label><input class="eq-freq" type="number" min="20" max="20000" step="1" /> Hz</label>
    <input class="eq-gain" type="range" min="-10" max="10" step="0.5" />
    <b class="eq-gain-val accent"></b>
    <label>Q <input class="eq-q" type="number" min="0.1" max="10" step="0.05" /></label>`;
  const q = <T extends HTMLElement>(sel: string) => row.querySelector<T>(sel)!;
  const freq = q<HTMLInputElement>(".eq-freq");
  const gain = q<HTMLInputElement>(".eq-gain");
  const qv = q<HTMLInputElement>(".eq-q");
  const showGain = () => (q(".eq-gain-val").textContent = `${signed(b.gain)} dB`);
  freq.value = String(b.freq);
  gain.value = String(b.gain);
  qv.value = String(b.q);
  showGain();
  q<HTMLSelectElement>(".eq-type").onchange = (e) => ((b.type = (e.target as HTMLSelectElement).value), eqChanged());
  freq.onchange = () => ((b.freq = Math.min(20000, Math.max(20, Math.round(+freq.value) || 1000))), (freq.value = String(b.freq)), eqChanged());
  qv.onchange = () => ((b.q = Math.min(10, Math.max(0.1, Math.round(+qv.value * 100) / 100 || 0.75))), (qv.value = String(b.q)), eqChanged());
  gain.oninput = () => ((b.gain = +gain.value), showGain(), renderCurve());
  gain.onchange = eqChanged;
  return row;
}

// The response the dongle plays: its biquads (RBJ cookbook, at its 96 kHz) summed in dB.
function bandDb(b: Band, f: number) {
  const A = 10 ** (b.gain / 40);
  const w0 = (2 * Math.PI * b.freq) / 96000;
  const [sin, cos] = [Math.sin(w0), Math.cos(w0)];
  let c: number[];
  if (b.type === "peak") {
    const alpha = sin / (2 * b.q);
    c = [1 + alpha * A, -2 * cos, 1 - alpha * A, 1 + alpha / A, -2 * cos, 1 - alpha / A];
  } else {
    const k = b.type === "lowShelf" ? 1 : -1;
    const s = Math.sqrt(A) * sin * Math.sqrt((A + 1 / A) * (1 / b.q - 1) + 2);
    c = [
      A * (A + 1 - k * (A - 1) * cos + s),
      k * 2 * A * (A - 1 - k * (A + 1) * cos),
      A * (A + 1 - k * (A - 1) * cos - s),
      A + 1 + k * (A - 1) * cos + s,
      -k * 2 * (A - 1 + k * (A + 1) * cos),
      A + 1 + k * (A - 1) * cos - s,
    ];
  }
  const w = (2 * Math.PI * f) / 96000;
  const mag = (x0: number, x1: number, x2: number) =>
    Math.hypot(x0 + x1 * Math.cos(w) + x2 * Math.cos(2 * w), x1 * Math.sin(w) + x2 * Math.sin(2 * w));
  return 20 * Math.log10(mag(c[0]!, c[1]!, c[2]!) / mag(c[3]!, c[4]!, c[5]!));
}

function renderCurve() {
  if (!audio) return;
  const { bands, preamp } = audio.eq;
  const W = 600, H = 140, range = 15; // ±15 dB
  const y = (db: number) => H / 2 - (Math.max(-range, Math.min(range, db)) / range) * (H / 2 - 4);
  const pts = Array.from({ length: 150 }, (_, i) => {
    const f = 20 * 1000 ** (i / 149); // 20 Hz to 20 kHz, evenly on a log scale
    const db = preamp + bands.reduce((sum, b) => sum + bandDb(b, f), 0);
    return `${((i / 149) * W).toFixed(1)},${y(db).toFixed(1)}`;
  });
  const grid = [100, 1000, 10000].map((f) => (Math.log10(f / 20) / 3) * W);
  $("#eq-curve").innerHTML =
    grid.map((x) => `<line class="grid" x1="${x}" x2="${x}" y1="0" y2="${H}" />`).join("") +
    `<line class="grid" x1="0" x2="${W}" y1="${H / 2}" y2="${H / 2}" /><polyline points="${pts.join(" ")}" />`;
}

function renderAudio() {
  const s = audio!;
  const id = dev!.id;
  const fmt = $<HTMLSelectElement>("#audio-format");
  fmt.hidden = !s.format;
  $("#audio-format-hint").textContent = t(s.format ? "audio.formatHint" : "audio.noFormat");
  if (s.format) {
    const key = (f: { bits: number; rate: number }) => `${f.bits}:${f.rate}`;
    fmt.innerHTML = s.formats.map((f) => option(key(f), t("audio.formatName", { bits: f.bits, rate: f.rate }), key(f) === key(s.format!))).join("");
    fmt.onchange = () => {
      const [bits, rate] = fmt.value.split(":").map(Number) as [number, number];
      attempt(() => Audio.setFormat(id, { bits, rate }), t("audio.formatSet")).then((ok) => (ok ? (s.format = { bits, rate }) : renderAudio()));
    };
  }
  const filter = $<HTMLSelectElement>("#audio-filter");
  filter.innerHTML = [1, 2, 3, 4, 5].map((v) => option(v, t(`audio.filter${v}` as "audio.filter1"), v === s.dacFilter)).join("");
  filter.onchange = () => attempt(() => Audio.setDACFilter(id, +filter.value)).then((ok) => (ok ? (s.dacFilter = +filter.value) : renderAudio()));
  const gain = $<HTMLInputElement>("#audio-gain");
  gain.checked = s.highGain;
  gain.onchange = () => attempt(() => Audio.setHighGain(id, gain.checked)).then((ok) => (ok ? (s.highGain = gain.checked) : renderAudio()));
  segmented($("#audio-mode"), [1, 0], s.workMode, (v) => t(`audio.mode${v}` as "audio.mode0"), (v) =>
    attempt(() => Audio.setWorkMode(id, v)).then((ok) => (ok ? (s.workMode = v) : renderAudio())),
  );
  const mic = $<HTMLInputElement>("#audio-mic");
  mic.value = String(s.micGain);
  $("#audio-mic-val").textContent = signed(s.micGain);
  mic.oninput = () => ($("#audio-mic-val").textContent = signed(+mic.value));
  mic.onchange = () => attempt(() => Audio.setMicGain(id, +mic.value)).then((ok) => (ok ? (s.micGain = +mic.value) : renderAudio()));
}

// ---------- macros ----------

const esc = (s: string) => s.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);

// Virtual-key names; letters, digits and F-keys are computed.
const VK_NAMES: Record<number, string> = {
  8: "Backspace", 9: "Tab", 13: "Enter", 16: "Shift", 17: "Ctrl", 18: "Alt", 19: "Pause", 20: "Caps Lock", 27: "Esc", 32: "Space",
  33: "Page Up", 34: "Page Down", 35: "End", 36: "Home", 37: "←", 38: "↑", 39: "→", 40: "↓", 44: "Print Screen", 45: "Insert", 46: "Delete",
  91: "Win", 92: "Right Win", 93: "Menu", 106: "Num *", 107: "Num +", 109: "Num -", 110: "Num .", 111: "Num /", 144: "Num Lock", 145: "Scroll Lock",
  160: "Left Shift", 161: "Right Shift", 162: "Left Ctrl", 163: "Right Ctrl", 164: "Left Alt", 165: "Right Alt",
  173: "Mute", 174: "Volume Down", 175: "Volume Up", 176: "Next Track", 177: "Previous Track", 179: "Play/Pause",
  186: ";", 187: "=", 188: ",", 189: "-", 190: ".", 191: "/", 192: "`", 219: "[", 220: "\\", 221: "]", 222: "'",
};
function vkName(vk: number) {
  if ((vk >= 48 && vk <= 57) || (vk >= 65 && vk <= 90)) return String.fromCharCode(vk);
  if (vk >= 96 && vk <= 105) return `Num ${vk - 96}`;
  if (vk >= 112 && vk <= 135) return `F${vk - 111}`;
  return VK_NAMES[vk] ?? `VK ${vk}`;
}
const MOUSE_BUTTONS = [1, 2, 3, 4, 5];
const inputName = (e: MacroEvent) => (e.kind === "mouse" ? buttonName(e.code) : vkName(e.code));
const MODES = ["once", "repeat", "toggle", "hold"] as const;

let macros: Macro[] = [];
let macro: Macro | undefined; // being edited
let recording = false;

// A macro's saves run one after another, so later ones carry the ID the first assigns.
const macroSaves = new WeakMap<Macro, Promise<unknown>>();
const deletedMacros = new WeakSet<Macro>(); // being deleted: queued saves are dropped

function saveMacro(m: Macro) {
  const next = (macroSaves.get(m) ?? Promise.resolve()).then(() =>
    deletedMacros.has(m)
      ? undefined
      : attempt(async () => {
          const saved = await Macros.save(m);
          if (!m.id) {
            m.id = saved.id;
            renderMacroList();
          }
        }),
  );
  macroSaves.set(m, next);
}

async function loadMacros() {
  macros = (await Macros.list().catch(() => [])) ?? [];
  if (macro && !macros.some((m) => m.id === macro!.id)) macro = undefined;
  macro = macros.find((m) => m.id === macro?.id) ?? macros[0];
  renderMacroList();
  renderMacro();
}

function renderMacroList() {
  $("#macro-list").replaceChildren(
    ...macros.map((m) => {
      const b = document.createElement("button");
      b.className = "macro-item" + (m === macro ? " on" : "");
      b.textContent = m.name;
      b.onclick = () => {
        if (recording) return;
        macro = m;
        renderMacroList();
        renderMacro();
      };
      return b;
    }),
  );
  $("#macro-none").hidden = macros.length > 0;
}

function renderMacro() {
  $("#macro-edit").hidden = !macro;
  if (!macro) return;
  const m = macro;
  const name = $<HTMLInputElement>("#macro-name");
  name.value = m.name;
  name.onchange = () => {
    m.name = name.value.trim() || m.name;
    name.value = m.name;
    renderMacroList();
    saveMacro(m);
  };
  const mode = $<HTMLSelectElement>("#macro-mode");
  mode.innerHTML = MODES.map((k) => option(k, t(`mode.${k}` as "mode.once"), m.mode === k)).join("");
  const count = $<HTMLInputElement>("#macro-count");
  const syncCount = () => ($("#macro-count-row").style.visibility = m.mode === "repeat" ? "" : "hidden");
  count.value = String(m.count || 1);
  syncCount();
  mode.onchange = () => {
    m.mode = mode.value;
    if (m.mode === "repeat" && !m.count) m.count = 1;
    syncCount();
    saveMacro(m);
  };
  count.onchange = () => {
    m.count = Math.min(999, Math.max(1, Math.round(+count.value) || 1));
    count.value = String(m.count);
    saveMacro(m);
  };
  $("#macro-record").textContent = t(recording ? "macros.stop" : "macros.record");
  $("#macro-recording").hidden = !recording;
  renderSteps();
}

function renderSteps() {
  const m = macro!;
  const box = $("#macro-steps");
  if (!m.events.length) {
    box.replaceChildren(Object.assign(document.createElement("p"), { className: "muted small", textContent: t("macros.noSteps") }));
    return;
  }
  box.replaceChildren(
    ...m.events.map((e, i) => {
      const row = document.createElement("div");
      row.className = "step";
      row.innerHTML = `
        <span class="step-delay"><input type="number" min="0" max="600000" step="10" title="${t("macros.delay")}" /> ms</span>
        <span class="step-what ${e.down ? "down" : "up"}"></span>
        <button class="ghost x" title="${t("macros.deleteStep")}">✕</button>`;
      const what = row.querySelector<HTMLElement>(".step-what")!;
      if (e.kind === "key" || e.kind === "mouse") {
        what.textContent = t(e.down ? "macros.down" : "macros.up", { key: inputName(e) });
      } else {
        what.className = `step-what amount ${e.kind}`;
        what.append(stepAmount(m, e));
      }
      const delay = row.querySelector<HTMLInputElement>("input")!;
      delay.value = String(e.delay);
      delay.onchange = () => {
        e.delay = Math.min(600000, Math.max(0, Math.round(+delay.value) || 0));
        delay.value = String(e.delay);
        saveMacro(m);
      };
      row.querySelector<HTMLButtonElement>(".x")!.onclick = () => {
        m.events.splice(i, 1);
        renderSteps();
        saveMacro(m);
      };
      return row;
    }),
  );
}

// The editable amount of a move or wheel step: X and Y, or notches.
function stepAmount(m: Macro, e: MacroEvent) {
  const box = document.createElement("span");
  const num = (label: string, get: () => number, set: (v: number) => void) => {
    const l = document.createElement("label");
    const input = Object.assign(document.createElement("input"), { type: "number", value: String(get()) });
    input.min = "-10000";
    input.max = "10000";
    input.onchange = () => {
      set(Math.round(+input.value) || 0);
      input.value = String(get());
      saveMacro(m);
    };
    l.append(label, input);
    return l;
  };
  if (e.kind === "move") {
    box.append(t("macros.move"), num("X", () => e.x, (v) => (e.x = v)), num("Y", () => e.y, (v) => (e.y = v)));
  } else {
    const clamp = (v: number) => Math.max(-100, Math.min(100, v)) || (e.code > 0 ? 1 : -1);
    box.append(t(e.kind === "wheel" ? "macros.wheel" : "macros.hwheel"), num("", () => e.code, (v) => (e.code = clamp(v))));
  }
  return box;
}

const step = (kind: string, code: number, down: boolean, x = 0, y = 0): MacroEvent => ({
  kind,
  code,
  down,
  x,
  y,
  delay: 50,
});

// A press and its release, or a move or wheel step, as a manual add.
function addSteps(...ev: MacroEvent[]) {
  if (!macro) return;
  const m = macro;
  if (!m.events.length) ev[0]!.delay = 0;
  m.events.push(...ev);
  renderSteps();
  saveMacro(m);
}
const addPress = (kind: string, code: number) => addSteps(step(kind, code, true), step(kind, code, false));

$("#add-macro").onclick = () => {
  if (recording) return;
  macro = { id: "", name: t("macros.newName", { n: macros.length + 1 }), events: [], mode: "once", count: 1 };
  macros.push(macro);
  renderMacroList();
  renderMacro();
  saveMacro(macro);
  $<HTMLInputElement>("#macro-name").select();
};

$("#macro-delete").onclick = () => {
  const m = macro;
  if (!m || recording) return;
  deletedMacros.add(m);
  attempt(async () => {
    await macroSaves.get(m);
    try {
      if (m.id) await Macros.delete(m.id);
    } catch (e) {
      deletedMacros.delete(m); // still there (e.g. bound to a button): keep it, with the edits dropped while deleting
      saveMacro(m);
      throw e;
    }
    macros = macros.filter((x) => x !== m);
    macro = macros[0];
    renderMacroList();
    renderMacro();
  });
};

$("#macro-record").onclick = async () => {
  if (!macro) return;
  const m = macro;
  if (!recording) {
    if (!(await attempt(() => Macros.startRecording($<HTMLInputElement>("#record-moves").checked)))) return;
    recording = true;
  } else {
    recording = false;
    try {
      const ev = await Macros.stopRecording();
      if (ev.length && m.events.length) ev[0]!.delay = 50;
      m.events.push(...ev);
      toast(t("macros.recorded", { n: ev.length }));
      saveMacro(m);
    } catch (e) {
      toast(isCallError(e) ? e.message : String(e), true);
    }
  }
  renderMacro();
};

// The next key pressed in the window is added.
$("#add-key").onclick = () => {
  const b = $<HTMLButtonElement>("#add-key");
  b.textContent = t("macros.pressKey");
  const onKey = (e: KeyboardEvent) => {
    e.preventDefault();
    window.removeEventListener("keydown", onKey, true);
    b.textContent = t("macros.addKey");
    addPress("key", e.keyCode); // Windows virtual-key code
  };
  window.addEventListener("keydown", onKey, true);
};

const addMouse = $<HTMLSelectElement>("#add-mouse");
addMouse.innerHTML =
  option("", t("macros.addMouse"), true) +
  MOUSE_BUTTONS.map((b) => option(b, buttonName(b), false)).join("") +
  option("wheel:1", t("macros.addWheelUp"), false) +
  option("wheel:-1", t("macros.addWheelDown"), false) +
  option("hwheel:1", t("macros.addWheelRight"), false) +
  option("hwheel:-1", t("macros.addWheelLeft"), false) +
  option("move", t("macros.addMove"), false);
addMouse.onchange = () => {
  const [kind, n] = addMouse.value.split(":");
  if (kind === "move") addSteps(step("move", 0, false, 100, 0));
  else if (n) addSteps(step(kind!, +n, false));
  else if (kind) addPress("mouse", +kind);
  addMouse.value = "";
};

// ---------- scenes ----------

let scenes: ScenesConfig | undefined;

const saveScenes = debounce(() => scenes && attempt(() => Scenes.setConfig(scenes!)), 400);

let smart: SmartPoll | undefined;
let soundDevs: AudioDevices = { playback: [], recording: [], output: "", input: "" };
const saveSmart = debounce(() => smart && attempt(() => Scenes.setSmart(smart!)), 300);
const rateText = (hz: number) => (hz === 0 ? t("smart.highest") : `${hz} Hz`);
const RATE_CHOICES = [0, ...[...RATES].reverse()];

function rateSelect(sel: HTMLSelectElement, value: number, onChange: (v: number) => void) {
  sel.innerHTML = RATE_CHOICES.map((r) => option(r, rateText(r), r === value)).join("");
  sel.onchange = () => onChange(+sel.value);
  return sel;
}

async function loadScenes() {
  let presets: EQPreset[] | null;
  let kbs: KeyboardState | undefined;
  [scenes, smart, soundDevs, presets, kbs] = await Promise.all([
    Scenes.config(),
    Scenes.smart(),
    Settings.audioDevices().catch(() => soundDevs),
    Audio.presets().catch(() => userPresets),
    Keyboards.effects().catch(() => undefined),
  ]);
  userPresets = presets ?? [];
  if (kbs) [kbEffects, kbMax, kbLayout] = [kbs.effects, kbs.maxLevel, kbs.keys?.length ? { keys: kbs.keys, width: kbs.width!, height: kbs.height! } : undefined];
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
// Keyboard effects a scene can pick, from the app (no keyboard needed).
let kbEffects: KbEffect[] = [];
let kbMax = 4;
let kbLayout: KbLayout | undefined;
// The keys LEOBOG ONE's own game set lights.
const GAME_KEYS = ["Esc", "W", "A", "S", "D", "Up", "Down", "Left", "Right"];

function sceneSummary(s: Scene, kind: "custom" | "game" | "desktop") {
  const parts: string[] = [];
  if (kind !== "desktop") parts.push(s.apps?.length ? s.apps.join(", ") : kind === "game" ? t("scenes.gameHint") : t("scenes.noPrograms"));
  else parts.push(t("scenes.desktopHint"));
  const set: string[] = [];
  if (s.dpi) set.push(`${s.dpi} DPI`);
  if (s.lighting) set.push(effectName(s.lighting.effect));
  if (s.brightness != null) set.push(`${t("light.brightness")} ${s.brightness}%`);
  if (s.highlight) set.push(`${t("cat.keyboard")} ${t("scenes.kbHighlight")}`);
  else if (s.keyboard) {
    const e = kbEffects.find((x) => x.id === s.keyboard!.effect);
    set.push(`${t("cat.keyboard")} ${e ? kbEffectName(e.name) : t("kb.effect.other")}`);
  }
  if (s.eq) set.push(`${t("tab.eq")} ${presetLabel(s.eq.preset)}`);
  if (s.output) set.push(deviceName(soundDevs.playback, s.output));
  if (s.input) set.push(deviceName(soundDevs.recording, s.input));
  if (s.volume != null) set.push(`${t("scenes.volume")} ${s.volume}%`);
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
    <div class="scene-grid scene-kb">
      <label><span class="muted small">${t("scenes.kbEffect")}</span><select class="kb-effect"></select></label>
      <label class="kb-color-cell"><span class="muted small"><input class="kb-colorful" type="checkbox" /> ${t("kb.colorful")}</span><input class="color kb-color" type="color" /></label>
      <label class="kb-level"><span class="muted small">${t("light.brightness")} <b class="kb-bright-val"></b></span><input class="kb-bright" type="range" min="0" step="1" /></label>
      <label class="kb-level kb-speed-cell"><span class="muted small">${t("kb.speed")} <b class="kb-speed-val"></b></span><input class="kb-speed" type="range" min="0" step="1" /></label>
    </div>
    <div class="scene-hl" hidden>
      <div class="row">
        <span class="muted small">${t("scenes.kbHighlightHint")}</span>
        <label class="muted small">${t("scenes.kbHighlightColor")} <input class="color hl-color" type="color" /></label>
        <label class="muted small">${t("scenes.kbRestColor")} <input class="color hl-rest" type="color" /></label>
      </div>
      <div class="kb-draw hl-draw"></div>
    </div>
    <div class="scene-grid scene-audio">
      <label class="scene-eq"><span class="muted small">${t("tab.eq")}<span class="info" tabindex="0" title="${t("scenes.audioMore")}">i</span></span><select class="eq"></select></label>
      <label><span class="muted small">${t("scenes.output")}</span><select class="output"></select></label>
      <label><span class="muted small">${t("scenes.input")}</span><select class="input"></select></label>
      <label><span class="muted small"><input class="vol-on" type="checkbox" title="${t("scenes.volumeToggle")}" /> ${t("scenes.volume")} <b class="vol-val"></b></span>
        <input class="vol" type="range" min="0" max="100" step="1" /></label>
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
  sceneKeyboard(s, q, changed);
  // Audio: a copy of a preset, and devices by ID (one unplugged now stays listed).
  const keep = option("", t("scenes.audioKeep"), false);
  const eqSel = q<HTMLSelectElement>(".eq");
  const presetRefs = [...Object.keys(BUILTIN_PRESETS).map((k) => `b:${k}`), ...userPresets.map((p) => `u:${p.name}`)];
  if (s.eq && !presetRefs.includes(s.eq.preset)) presetRefs.push(s.eq.preset); // a deleted preset the scene still plays
  eqSel.innerHTML = keep + presetRefs.map((r) => option(esc(r), esc(presetLabel(r)), s.eq?.preset === r)).join("");
  // Only with a dongle around, or a scene already set: others have no EQ to switch.
  q(".scene-eq").hidden = !s.eq && !list.some((d) => d.category === "audio");
  eqSel.onchange = () => {
    const eq = presetEQ(eqSel.value) ?? (s.eq?.preset === eqSel.value ? s.eq.eq : undefined);
    s.eq = eq ? { preset: eqSel.value, eq: structuredClone(eq) } : null;
    changed();
  };
  const devSelect = (sel: HTMLSelectElement, devs: { id: string; name: string }[], get: () => string, set: (id: string) => void) => {
    const ids = devs.map((d) => d.id);
    if (get() && !ids.includes(get())) ids.push(get());
    sel.innerHTML = keep + ids.map((id) => option(esc(id), esc(deviceName(devs, id)), id === get())).join("");
    sel.onchange = () => (set(sel.value), changed());
  };
  devSelect(q(".output"), soundDevs.playback, () => s.output, (id) => (s.output = id));
  devSelect(q(".input"), soundDevs.recording, () => s.input, (id) => (s.input = id));
  const volOn = q<HTMLInputElement>(".vol-on");
  const vol = q<HTMLInputElement>(".vol");
  const syncVol = () => {
    vol.disabled = s.volume == null;
    q(".vol-val").textContent = s.volume == null ? t("scenes.audioKeep") : `${s.volume}%`;
  };
  vol.value = String(s.volume ?? 50); // unticking keeps the slider where it was
  volOn.checked = s.volume != null;
  syncVol();
  volOn.onchange = () => ((s.volume = volOn.checked ? +vol.value : null), syncVol(), changed());
  vol.oninput = () => ((s.volume = +vol.value), syncVol());
  vol.onchange = changed;
  summarize();
  return card;
}

// The scene's LEOBOG keyboard lighting; unset puts back what the keyboard
// showed before. Only with such a keyboard around, or a scene already set.
function sceneKeyboard(s: Scene, q: <T extends HTMLElement>(sel: string) => T, changed: () => void) {
  q(".scene-kb").hidden = !s.keyboard && !s.highlight && !list.some((d) => d.driver === "leobog");
  const sel = q<HTMLSelectElement>(".kb-effect");
  const colorful = q<HTMLInputElement>(".kb-colorful");
  const color = q<HTMLInputElement>(".kb-color");
  const bright = q<HTMLInputElement>(".kb-bright");
  const speed = q<HTMLInputElement>(".kb-speed");
  bright.max = speed.max = String(kbMax);
  const drawHighlight = () => {
    const h = s.highlight;
    q(".scene-hl").hidden = !h || !kbLayout;
    if (!h || !kbLayout) return;
    const hc = q<HTMLInputElement>(".hl-color");
    const rc = q<HTMLInputElement>(".hl-rest");
    hc.value = toHex(h.color);
    rc.value = toHex(h.rest);
    hc.oninput = () => ((h.color = fromHex(hc.value)), drawHighlight(), changed());
    rc.oninput = () => ((h.rest = fromHex(rc.value)), drawHighlight(), changed());
    kbDraw(
      q(".hl-draw"),
      kbLayout,
      (b, k) => {
        b.textContent = k.name;
        b.style.background = toHex(h.keys.includes(k.index) ? h.color : h.rest);
      },
      (k) => {
        h.keys = h.keys.includes(k.index) ? h.keys.filter((x) => x !== k.index) : [...h.keys, k.index];
        drawHighlight();
        changed();
      },
    );
  };
  const sync = () => {
    const k = s.keyboard;
    const fx = kbEffects.find((e) => e.id === k?.effect);
    const lit = !!k && k.effect !== 0 && !s.highlight;
    sel.innerHTML =
      option("", t("scenes.audioKeep"), !k && !s.highlight) +
      (kbLayout ? option("hl", t("scenes.kbHighlight"), !!s.highlight) : "") +
      kbEffects.filter((e) => e.id !== 21).map((e) => option(e.id, kbEffectName(e.name), !s.highlight && e.id === k?.effect)).join("");
    drawHighlight();
    // visibility, not hidden: the grid keeps its columns aligned across scenes
    q(".kb-color-cell").style.visibility = lit ? "" : "hidden";
    for (const c of q(".scene-kb").querySelectorAll<HTMLElement>(".kb-level")) c.style.visibility = lit ? "" : "hidden";
    q(".kb-speed-cell").style.visibility = lit && fx?.speed ? "" : "hidden";
    colorful.checked = !!k?.colorful;
    colorful.disabled = !fx?.color;
    color.disabled = !!k?.colorful;
    if (k) {
      color.value = toHex(k.color);
      bright.value = String(k.brightness);
      speed.value = String(k.speed);
      q(".kb-bright-val").textContent = `${k.brightness}/${kbMax}`;
      q(".kb-speed-val").textContent = `${k.speed}/${kbMax}`;
    }
  };
  sync();
  sel.onchange = () => {
    s.highlight = null;
    if (!sel.value) s.keyboard = null;
    else if (sel.value === "hl") {
      s.keyboard = null;
      const keys = kbLayout!.keys.filter((k) => GAME_KEYS.includes(k.name)).map((k) => k.index);
      s.highlight = { keys, color: { r: 0xff, g: 0x2d, b: 0x2d }, rest: { r: 0, g: 0, b: 0 } };
    } else {
      const fx = kbEffects.find((e) => e.id === +sel.value)!;
      const prev = s.keyboard;
      s.keyboard = {
        effect: fx.id,
        brightness: prev?.brightness ?? kbMax,
        speed: prev?.speed ?? Math.ceil(kbMax / 2),
        colorful: prev ? prev.colorful || !fx.color : true,
        color: prev?.color ?? fromHex(color.value),
      };
    }
    sync();
    changed();
  };
  colorful.onchange = () => s.keyboard && ((s.keyboard.colorful = colorful.checked), sync(), changed());
  color.oninput = () => s.keyboard && ((s.keyboard.color = fromHex(color.value)), changed());
  bright.oninput = () => s.keyboard && ((s.keyboard.brightness = +bright.value), sync());
  speed.oninput = () => s.keyboard && ((s.keyboard.speed = +speed.value), sync());
  bright.onchange = speed.onchange = changed;
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
  scenes.scenes.push({ name: t("scenes.newName", { n: scenes.scenes.length + 1 }), apps: [], dpi: 0, lighting: null, brightness: null, eq: null, output: "", input: "", volume: null, keyboard: null, highlight: null });
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

// Every Windows playback device and its default format.
async function loadSound() {
  const [devs, defaults] = await Promise.all([Settings.soundDevices().catch(() => []), Settings.audioDevices().catch(() => soundDevs)]);
  soundDevs = defaults;
  const card = $("#sound-card");
  card.hidden = !devs?.length && !defaults.playback.length && !defaults.recording.length;
  const head = document.createElement("div");
  head.className = "list-row";
  head.innerHTML = `<div><div class="row-title"><span>${t("sound.title")}</span><span class="info" tabindex="0" title="${t("audio.formatMore")}">i</span></div><div class="muted small">${t("sound.hint")}</div></div>`;
  // The default playback and recording devices, as the Windows volume flyout switches them.
  const defaultRow = (label: string, choices: { id: string; name: string }[], current: string) => {
    const row = document.createElement("div");
    row.className = "list-row";
    row.innerHTML = `<div class="row-title"></div><select class="lang-select wide"></select>`;
    row.querySelector(".row-title")!.textContent = label;
    const sel = row.querySelector("select")!;
    sel.innerHTML = choices.map((d) => option(esc(d.id), esc(d.name), d.id === current)).join("");
    sel.onchange = () => attempt(() => Settings.setDefaultDevice(sel.value), t("sound.defaultSet")).then((ok) => void (ok || loadSound()));
    return row;
  };
  card.replaceChildren(
    head,
    ...(defaults.playback.length ? [defaultRow(t("sound.output"), defaults.playback, defaults.output)] : []),
    ...(defaults.recording.length ? [defaultRow(t("sound.input"), defaults.recording, defaults.input)] : []),
    ...(devs ?? []).map((d) => {
      const row = document.createElement("div");
      row.className = "list-row";
      row.innerHTML = `<div class="row-title"></div><select class="lang-select"></select>`;
      row.querySelector(".row-title")!.textContent = t("sound.formatOf", { name: d.name });
      const sel = row.querySelector("select")!;
      const key = (f: { bits: number; rate: number }) => `${f.bits}:${f.rate}`;
      sel.innerHTML = d.formats.map((f) => option(key(f), t("audio.formatName", { bits: f.bits, rate: f.rate }), key(f) === key(d.format))).join("");
      sel.onchange = () => {
        const [bits, rate] = sel.value.split(":").map(Number) as [number, number];
        attempt(() => Settings.setSoundFormat(d.id, { bits, rate }), t("audio.formatSet")).then((ok) => void (ok || loadSound()));
      };
      return row;
    }),
  );
}

// ---------- about ----------

const dateTime = new Intl.DateTimeFormat(lang.effective, { dateStyle: "medium", timeStyle: "short" });

function renderAbout(a: AppInfo) {
  $("#about-version").textContent = t("about.version", { version: a.version });
  $("#about-edition").textContent = t(a.portable ? "about.portable" : "about.installed");
  $("#update-title").textContent = t("about.updates");
  $("#update-status").textContent = !a.canUpdate
    ? t("about.cantUpdate")
    : a.lastCheck
      ? t("about.lastCheck", { time: dateTime.format(a.lastCheck) })
      : t("about.neverChecked");
  $<HTMLButtonElement>("#update-check").disabled = !a.canUpdate;
  const auto = $<HTMLInputElement>("#auto-update");
  auto.checked = a.canUpdate && a.autoCheck;
  auto.disabled = !a.canUpdate;
}

const loadAbout = () => Settings.app().then(renderAbout).catch(() => {});
events.aboutChanged.on(renderAbout);
$<HTMLInputElement>("#auto-update").onchange = (e) => attempt(() => Settings.setAutoUpdate((e.target as HTMLInputElement).checked));
$("#update-check").onclick = () => attempt(() => Settings.checkForUpdates()); // MyGo's update window shows the result
// The name openrazer in the credits links to the project.
{
  const credits = $("#about-credits");
  const link = Object.assign(document.createElement("button"), { className: "inline-link", textContent: "openrazer" });
  link.dataset.link = "openrazer";
  const [before, after = ""] = credits.textContent!.split("openrazer");
  credits.replaceChildren(before!, link, after);
}
document.querySelectorAll<HTMLButtonElement>("[data-link]").forEach((b) => (b.onclick = () => attempt(() => Settings.openLink(b.dataset.link!))));

Settings.dpiDisplay().then((on) => ($<HTMLInputElement>("#dpi-display").checked = on));
$<HTMLInputElement>("#dpi-display").onchange = (e) => attempt(() => Settings.setDPIDisplay((e.target as HTMLInputElement).checked));
$("#export").onclick = () => attempt(async () => (await Settings.export()) && toast(t("backup.exported")));
// Everything on the page may change: reload it.
$("#import").onclick = () => attempt(async () => (await Settings.import()) && location.reload());

const glassSwitch = $<HTMLInputElement>("#glass");
glassSwitch.checked = glass.on;
glassSwitch.disabled = !glass.supported;
$("#glass-hint").textContent = t(glass.supported ? "settings.glassHint" : "settings.glassUnsupported");
glassSwitch.onchange = () => {
  const on = glassSwitch.checked;
  attempt(() => Settings.setGlass(on)).then((ok) => {
    if (ok) document.documentElement.classList.toggle("glass", on);
    else glassSwitch.checked = !on;
  });
};

const langSel = $<HTMLSelectElement>("#language");
langSel.innerHTML = [
  ["", t("settings.langSystem")],
  ["zh-CN", "简体中文"],
  ["en", "English"],
  ["ja", "日本語"],
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
