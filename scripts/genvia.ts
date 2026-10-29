// Generate internal/via/data/keyboards.json.gz: the keyboards VIA supports,
// from a checkout of the-via/keyboards and the @the-via/reader package
// (both GPL-3.0), which parses their key layouts.
//
//     bun scripts/genvia.ts <the-via/keyboards checkout> <@the-via/reader package dir>
//
// Get the reader with `npm pack @the-via/reader` and untar it; it is not a
// dependency of the app. Each keyboard keeps its USB IDs, switch matrix, the
// keys at their default layout options, and its lighting controls: the
// first lighting zone of its menus, built-in or its own.

import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const [repo, readerDir] = process.argv.slice(2);
if (!repo || !readerDir) throw new Error("usage: bun scripts/genvia.ts <keyboards checkout> <reader package dir>");
// The reader's schema compiler warns about its own schemas, at length.
console.warn = console.log = () => {};
const report = (s: string) => process.stdout.write(s + "\n");
const reader = require(join(readerDir, "dist", "index.js"));
const out = join(import.meta.dir, "..", "internal", "via", "data", "keyboards.json.gz");

function* files(dir: string): Generator<string> {
  for (const f of readdirSync(dir).sort()) {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) yield* files(p);
    else if (f.endsWith(".json")) yield p;
  }
}

type Control = { label?: string; type?: string; options?: unknown[]; content?: unknown[] };

// The controls of every menu, built-in ones (named by a string) expanded.
function* controls(items: unknown[]): Generator<Control> {
  for (const it of items) {
    if (typeof it === "string") {
      if (reader.commonMenus[it]) yield* controls(reader.commonMenus[it]);
    } else if (it && typeof it === "object") {
      const c = it as Control;
      if (Array.isArray(c.content) && typeof c.content[0] === "string" && typeof c.content[1] === "number") yield c;
      else if (Array.isArray(c.content)) yield* controls(c.content);
    }
  }
}

// lighting finds one zone's brightness, effect, speed and color, from the
// VIA value names (id_qmk_rgb_matrix_brightness and the like).
function lighting(menus: unknown[]) {
  let zone = "";
  const l: Record<string, unknown> = {};
  for (const c of controls(menus)) {
    const m = /^id_(.+?)_(brightness|effect|effect_speed|color)$/.exec(c.content![0] as string);
    if (!m || c.content!.length !== 3) continue;
    zone ||= m[1]!;
    if (m[1] !== zone || l[m[2]!]) continue;
    const at = [c.content![1], c.content![2]];
    switch (m[2]) {
      case "brightness":
      case "effect_speed":
        l[m[2] === "brightness" ? "brightness" : "speed"] = [...at, (c.options?.[1] as number) ?? 255];
        break;
      case "effect":
        if (c.type !== "dropdown" || !c.options) continue;
        l.effect = at;
        l.effects = c.options.map((o, i) => (Array.isArray(o) ? [o[1], String(o[0])] : [i, String(o)]));
        break;
      case "color":
        if (c.type === "color") l.color = at;
    }
  }
  return l.effect || l.brightness ? l : undefined;
}

const unit = 40;
const gap = 4;
// Vendors publish definitions of keyboards VIA's list lacks, for VIA's Design
// tab. Several keyboards then share USB IDs; the app tells them apart by the
// product name they report, or lets the user pick.
const vendor: [string, string][] = [
  ["https://drive.wobkey.com/f/d/XNltG/Rainy-75-RGB-USB-3.0.JSON", "WOBKEY Rainy 75"],
  ["https://drive.wobkey.com/f/d/jX5hd/Rainy-75-Non-RGB-USB-3.0.JSON", "WOBKEY Rainy 75 Lite"],
  ["https://drive.wobkey.com/f/d/mEU5/Rainy75-ISO-USB.JSON", "WOBKEY Rainy 75 ISO"],
  ["https://drive.wobkey.com/f/d/npgIl/Rainy-75-JIS-New-USB.JSON", "WOBKEY Rainy 75 JIS"],
  ["https://drive.wobkey.com/f/d/o29f2/Rainy-75-RGB-2.4G-3.0.JSON", "WOBKEY Rainy 75 (2.4G)"],
  ["https://drive.wobkey.com/f/d/WANup/Rainy-75-Non-RGB-2.4G-3.0.JSON", "WOBKEY Rainy 75 Lite (2.4G)"],
  ["https://drive.wobkey.com/f/d/G8TE/Rainy75-ISO-2.4G.JSON", "WOBKEY Rainy 75 ISO (2.4G)"],
  ["https://drive.wobkey.com/f/d/8byUO/Rainy-75-JIS-NEW-2.4G.JSON", "WOBKEY Rainy 75 JIS (2.4G)"],
];

const keyboards = [];
let skipped = 0;

function add(json: string, rename?: string) {
  let def;
  try {
    def = reader.keyboardDefinitionV3ToVIADefinitionV3(JSON.parse(json));
  } catch {
    skipped++; // not a keyboard definition, or one VIA cannot read either
    return;
  }
  const vid = def.vendorProductId >>> 16;
  const pid = def.vendorProductId & 0xffff;
  const { rows, cols } = def.matrix;
  const ks = [...def.layouts.keys];
  for (const opts of Object.values(def.layouts.optionKeys) as Record<string, unknown[]>[]) ks.push(...(opts["0"] ?? []));
  const keys = [];
  let width = 0;
  let height = 0;
  for (const k of ks as { row: number; col: number; x: number; y: number; w: number; h: number; r?: number; rx?: number; ry?: number; d?: boolean }[]) {
    if (k.d || k.row < 0 || k.col < 0 || k.row >= rows || k.col >= cols) continue; // decals and keys off the matrix
    // A rotated key is drawn upright, centered where it would be.
    let cx = k.x + k.w / 2;
    let cy = k.y + k.h / 2;
    if (k.r) {
      const a = (k.r * Math.PI) / 180;
      const [dx, dy] = [cx - (k.rx ?? 0), cy - (k.ry ?? 0)];
      [cx, cy] = [(k.rx ?? 0) + dx * Math.cos(a) - dy * Math.sin(a), (k.ry ?? 0) + dx * Math.sin(a) + dy * Math.cos(a)];
    }
    const box = [k.row, k.col, Math.round((cx - k.w / 2) * unit), Math.round((cy - k.h / 2) * unit), Math.round(k.w * unit) - gap, Math.round(k.h * unit) - gap];
    keys.push(box);
  }
  if (!keys.length) return;
  const minX = Math.min(...keys.map((k) => k[2]!));
  const minY = Math.min(...keys.map((k) => k[3]!));
  for (const k of keys) {
    k[2]! -= minX;
    k[3]! -= minY;
    width = Math.max(width, k[2]! + k[4]!);
    height = Math.max(height, k[3]! + k[5]!);
  }
  const name = rename ?? (typeof def.name === "string" ? def.name : def.name.options[0]);
  keyboards.push({ name, vid, pid, rows, cols, width, height, keys, lighting: lighting(def.menus ?? []) });
}

for (const f of files(join(repo, "v3"))) add(readFileSync(f, "utf8"));
for (const [url, name] of vendor) {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`);
  add(await r.text(), name);
}
// VIA's own first among keyboards sharing USB IDs: the stable sort keeps that order.
keyboards.sort((a, b) => a.vid - b.vid || a.pid - b.pid);
writeFileSync(out, gzipSync(JSON.stringify(keyboards), { level: 9 }));
report(`${keyboards.length} keyboards (${skipped} files skipped) → ${out}`);
