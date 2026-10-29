// Generate internal/leobog/data/models.json: the keyboards on Sinowealth's
// BY-520 firmware (the LEOBOG Hi75's), from the tables of Let Me Drive
// (https://letmedrive.cn), a web driver that drives them over WebHID.
//
//     bun scripts/genkeyboards.ts
//
// Let Me Drive took the tables from the vendors' own drivers. Each row is one
// firmware: USB IDs, the model number the keyboard answers the 0x82 request
// with, its effects, the key at every matrix index, and a drawing of its keys
// (one row of key names and widths per keyboard row). Only the data is read;
// none of its code is run or copied.

import { writeFileSync } from "node:fs";
import { join } from "node:path";

const site = "https://letmedrive.cn/";
const out = join(import.meta.dir, "..", "internal", "leobog", "data", "models.json");

async function get(path: string) {
  const r = await fetch(new URL(path, site));
  if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
  return r.text();
}

// The array literal starting at `start`, as a string: brackets are matched
// outside string literals.
function literal(src: string, start: number) {
  let depth = 0;
  let quote = "";
  for (let i = start; i < src.length; i++) {
    const c = src[i]!;
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = "";
    } else if (c === '"' || c === "'" || c === "`") quote = c;
    else if (c === "[" || c === "{") depth++;
    else if ((c === "]" || c === "}") && --depth === 0) return src.slice(start, i + 1);
  }
  throw new Error("unterminated literal");
}

// The table is minified JS: quote the object keys and it parses as JSON.
const parse = (js: string) => JSON.parse(js.replace(/([{,])([A-Za-z_$][\w$]*):/g, '$1"$2":'));

async function table(): Promise<unknown[][]> {
  const index = await get("/");
  const bundle = await get(index.match(/src="(app\/bundle-[^"]+\.js)"/)![1]!);
  const driver = bundle.match(/"\.\.\/drivers\/sinowealth\.js":\(\)=>import\("\.\/([^"]+)"\)/)![1]!;
  const chunks = [...(await get(`app/${driver}`)).matchAll(/from"\.\/([^"]+)"/g)].map((m) => m[1]!);
  for (const c of chunks) {
    const src = await get(`app/${c}`);
    const at = src.indexOf(",k4=[");
    if (at >= 0) return parse(literal(src, at + 4));
  }
  throw new Error("BY-520 table not found");
}

type Row = [number, number, string, string, string, string, string, { bri?: number[]; spd?: number[] }?];
type Layout = { rows: (string | { k?: string; w?: number; gap?: number; label?: string })[][] };

// Effect IDs to the names the app translates (kb.effect.*); 18 is the
// per-key custom effect and 0 is off, which the app adds itself.
const effectNames: Record<number, string> = {
  1: "static", 2: "breathing", 3: "rainbow", 4: "trigger", 5: "raindrops", 6: "rainbowWheel", 7: "ripples", 8: "stars",
  9: "snow", 10: "stream", 11: "drift", 12: "reactive", 13: "sineWave", 14: "scan", 15: "windmill", 16: "waterfall", 17: "blossom",
};

// Key names as the Hi75's drawing writes them.
const labels: Record<string, string> = {
  esc: "Esc", grave: "`", minus: "-", equal: "=", backspace: "Backspace", tab: "Tab", leftbrace: "[", rightbrace: "]",
  backslash: "\\", capslock: "CapsLock", semicolon: ";", apostrophe: "'", enter: "Enter", leftshift: "LShift",
  rightshift: "RShift", comma: ",", dot: ".", slash: "/", leftctrl: "LCtrl", leftmeta: "LWin", leftalt: "LAlt",
  space: "Space", rightalt: "RAlt", fn: "FN", rightctrl: "RCtrl", menu: "Menu", left: "Left", right: "Right", up: "Up",
  down: "Down", insert: "Insert", delete: "Delete", home: "Home", end: "End", pageup: "PgUp", pagedown: "PgDn",
  prtsc: "PrtSc", scrlk: "ScrLk", pause: "Pause", numlock: "NumLock", kpslash: "Num/", kpasterisk: "Num*",
  kpminus: "Num-", kpplus: "Num+", kpenter: "NumEnter", kpdot: "Num.", calculator: "Calc", "102nd": "\\|", ro: "Ro",
};
const label = (k: string) => labels[k] ?? (/^kp\d$/.test(k) ? `Num${k[2]}` : /^f\d+$/.test(k) ? k.toUpperCase() : k.length === 1 ? k.toUpperCase() : k);

const unit = 40; // px per key width
const gap = 4; // px between keys

const layouts = new Map<string, Layout>();
const models = [];
for (const [vid, pid, ident, name, layoutID, fx, order, levels] of (await table()) as Row[]) {
  if (!layouts.has(layoutID)) layouts.set(layoutID, JSON.parse(await get(`layouts/${layoutID}.json`)));
  const matrix = order.split(" ");
  const keys = [];
  let width = 0;
  for (const [r, row] of layouts.get(layoutID)!.rows.entries()) {
    let x = 0;
    for (const it of row) {
      const k = typeof it === "string" ? { k: it } : it;
      if (k.gap) {
        x += k.gap;
        continue;
      }
      const w = k.w ?? 1;
      const index = matrix.indexOf(k.k!);
      // A key not in the matrix cannot be lit or remapped; nothing can show it.
      if (index >= 0 && index < 126) {
        keys.push({ index, name: k.label && !labels[k.k!] ? k.label : label(k.k!), x: Math.round(x * unit), y: r * unit, w: Math.round(w * unit) - gap, h: unit - gap });
      }
      x += w;
    }
    width = Math.max(width, Math.round(x * unit) - gap);
  }
  const effects = fx
    .split(" ")
    .map((s) => ({ id: parseInt(s), speed: !s.includes("x"), color: !s.includes("n") }))
    .filter((e) => e.id !== 18)
    .map((e) => ({ id: e.id, name: effectNames[e.id]!, speed: e.speed, color: e.color }));
  if (effects.some((e) => !e.name)) throw new Error(`${name}: unknown effect in "${fx}"`);
  const hex = ident.split(",").map((b) => b.padStart(2, "0")).join("");
  if (!/^[0-9a-f]{12}$/.test(hex)) throw new Error(`${name}: model number ${ident}`);
  models.push({
    name,
    vid,
    pid,
    ident: hex,
    effects,
    ...(levels?.bri ? { brightness: levels.bri } : {}),
    ...(levels?.spd ? { speed: levels.spd } : {}),
    fnIndex: matrix.indexOf("fn"),
    width,
    height: layouts.get(layoutID)!.rows.length * unit - gap,
    keys,
  });
}
models.sort((a, b) => a.name.localeCompare(b.name) || a.ident.localeCompare(b.ident));
// One keyboard per line keeps a regenerated table's diff readable.
writeFileSync(out, "[\n" + models.map((m) => JSON.stringify(m)).join(",\n") + "\n]\n");
console.log(`${models.length} keyboards → ${out}`);
