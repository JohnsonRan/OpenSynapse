"""Generate internal/razer/data/devices.json from an openrazer checkout.

    uv run --no-project python scripts/gendevices.py <path-to-openrazer>

Devices come from the openrazer daemon (daemon/openrazer_daemon/hardware/*.py):
product id, name, the methods it exposes, maximum DPI and polling rates.
Which protocol commands ("builders", razer_chroma_* in the driver) each device
uses, with the transaction id and the storage/LED arguments, comes from the
kernel driver's per-device switch statements (driver/*.c). Laptops and
headsets are left out. The files are read as data; no openrazer code is copied.
"""

import ast
import json
import re
import subprocess
import sys
from pathlib import Path

root = Path(sys.argv[1])
out = Path(__file__).resolve().parent.parent / "internal" / "razer" / "data" / "devices.json"

# ---------- daemon: device classes ----------

CATEGORY = {"mouse.py": "mouse", "keyboards.py": "keyboard", "mouse_mat.py": "mouse-mat",
            "accessory.py": "accessory", "monitor.py": "monitor"}


def classes(path):
    """Class attributes, with METHODS = Base.METHODS + [...] resolved."""
    tree = ast.parse(path.read_text(encoding="utf-8"))
    found = {}
    for node in tree.body:
        if not isinstance(node, ast.ClassDef):
            continue
        attrs = {"_bases": [b.id for b in node.bases if isinstance(b, ast.Name)]}
        for st in node.body:
            if not (isinstance(st, ast.Assign) and len(st.targets) == 1 and isinstance(st.targets[0], ast.Name)):
                continue
            name, value = st.targets[0].id, st.value
            if name not in ("USB_PID", "METHODS", "DPI_MAX", "POLL_RATES"):
                continue
            attrs[name] = value
        found[node.name] = attrs
    return found


def resolve(all_classes, name, attr, seen=()):
    cls = all_classes.get(name)
    if cls is None or name in seen:
        return None
    if attr in cls:
        return evaluate(all_classes, cls[attr], name, seen)
    for base in cls["_bases"]:
        v = resolve(all_classes, base, attr, seen + (name,))
        if v is not None:
            return v
    return None


def evaluate(all_classes, node, owner, seen):
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        return evaluate(all_classes, node.left, owner, seen) + evaluate(all_classes, node.right, owner, seen)
    if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name):
        return resolve(all_classes, node.value.id, node.attr, seen + (owner,)) or []
    return ast.literal_eval(node)


def docstring_name(path):
    """Display names from the class docstrings ("Class for the Razer X")."""
    tree = ast.parse(path.read_text(encoding="utf-8"))
    names = {}
    for node in tree.body:
        if isinstance(node, ast.ClassDef):
            doc = ast.get_docstring(node) or ""
            m = re.search(r"Class for the (.+)", doc)
            if m:
                names[node.name] = m.group(1).strip().rstrip(".")
    return names


devices = {}
hw = root / "daemon" / "openrazer_daemon" / "hardware"
for file, category in CATEGORY.items():
    path = hw / file
    cls = classes(path)
    names = docstring_name(path)
    for name in cls:
        if name.startswith("_"):
            continue
        pid = resolve(cls, name, "USB_PID")
        if pid is None:
            continue
        display = names.get(name, name)
        if category == "keyboard" and re.search(r"\b(Blade|Book)\b", display):
            continue  # laptops
        devices[pid] = {
            "productId": f"{pid:04X}",
            "displayName": display if display.startswith("Razer") else "Razer " + display,
            "category": category,
            "maximumDpi": resolve(cls, name, "DPI_MAX"),
            "pollRates": resolve(cls, name, "POLL_RATES"),
            "methods": sorted(set(resolve(cls, name, "METHODS") or [])),
        }

# ---------- driver: builders per device ----------

macros = {}
for h in (root / "driver").glob("*.h"):
    for m in re.finditer(r"#define\s+(\w+)\s+(0x[0-9A-Fa-f]+|\d+)\b", h.read_text(encoding="utf-8", errors="replace")):
        macros[m.group(1)] = int(m.group(2), 0)

ARG = {"VARSTORE": 1, "NOSTORE": 0}


def arg_byte(text):
    text = text.strip()
    if text in ARG:
        return ARG[text]
    if text in macros:
        return macros[text]
    if re.fullmatch(r"0x[0-9A-Fa-f]+|\d+", text):
        return int(text, 0)
    return None


BUILDER = re.compile(r"razer_chroma_(\w+)\s*\(([^;]*)\)\s*;")
TX = re.compile(r"transaction_id\.id\s*=\s*(0x[0-9A-Fa-f]+)")
CASE = re.compile(r"^\s*case\s+(USB_DEVICE_ID_RAZER_\w+)\s*:")
FUNC = re.compile(r"^(static\s+)?\w[\w\s\*]*\s\**(\w+)\s*\([^;]*\)\s*$")

uses = {}        # pid -> builder -> {"tx", "prefixes"}
sequences = {}   # pid -> [{"transactionId", "fixedArgument0"}] for receiver polling rate


def record(pid, builder, args, tx):
    entry = uses.setdefault(pid, {}).setdefault(builder, {"tx": None, "prefixes": []})
    if tx is not None and entry["tx"] is None:
        entry["tx"] = tx
    # Storage and LED arguments form the prefix the protocol code addresses.
    parts = [a for a in args.split(",")]
    prefix = []
    for a in parts[:2]:
        b = arg_byte(a)
        if b is None:
            break
        prefix.append(b)
    if prefix and not builder.startswith("misc_set_polling_rate"):
        p = "".join(f"{b:02X}" for b in prefix)
        if p not in entry["prefixes"]:
            entry["prefixes"].append(p)


def builders_in(lines):
    """Builder calls in order, each with the transaction id set after it."""
    calls = []
    text = "\n".join(lines)
    for m in BUILDER.finditer(text):
        tx_match = TX.search(text, m.end())
        nxt = BUILDER.search(text, m.end())
        tx = None
        if tx_match and (nxt is None or tx_match.start() < nxt.start()):
            tx = int(tx_match.group(1), 16)
        calls.append((m.group(1), m.group(2), tx))
    return calls


def strip_code(line):
    """The line without comments and string literals, for brace counting."""
    line = re.sub(r'"(?:[^"\\]|\\.)*"', '""', line)
    line = re.sub(r"'(?:[^'\\]|\\.)*'", "''", line)
    return re.sub(r"//.*", "", line)


def case_groups(body):
    """Yield (labels, statement lines, builder calls just before the switch) for
    each group of USB product-id case labels. A group ends at the next label
    after statements, at default:, or where its switch closes."""
    depth = 0
    switches = []          # brace depth inside each open switch
    outside = []           # lines since the last switch closed, outside any switch
    before_stack = []      # builder calls preceding each open switch
    labels, stmts, owner = [], [], None
    for line in body:
        code = strip_code(line)
        m = CASE.match(line)
        at_switch_level = switches and depth == switches[-1]
        if m and at_switch_level:
            if stmts:
                yield labels, stmts, before_stack[-1]
                labels, stmts = [], []
            labels.append(m.group(1))
            owner = len(switches)
        elif at_switch_level and re.match(r"^\s*(default\s*:|case\s)", line):
            if labels:
                yield labels, stmts, before_stack[-1]
            labels, stmts, owner = [], [], None
        elif labels:
            stmts.append(line)
        elif not switches:
            outside.append(line)
        if re.search(r"\bswitch\s*\(", code) and not labels:
            before_stack.append(builders_in(outside))
            outside = []
            switches.append(depth + code.count("{") - code.count("}"))
            depth = switches[-1]
            continue
        depth += code.count("{") - code.count("}")
        while switches and depth < switches[-1]:
            if labels and owner == len(switches):
                yield labels, stmts[:-1], before_stack[-1]  # drop the closing brace line
                labels, stmts, owner = [], [], None
            switches.pop()
            before_stack.pop()


for c in sorted((root / "driver").glob("razer*_driver.c")):
    if "kraken" in c.name:
        continue
    lines = c.read_text(encoding="utf-8", errors="replace").splitlines()
    i = 0
    while i < len(lines):
        if not (FUNC.match(lines[i]) and i + 1 < len(lines) and lines[i + 1].strip() == "{"):
            i += 1
            continue
        fname = FUNC.match(lines[i]).group(2)
        j = i + 2
        while j < len(lines) and lines[j] != "}":
            j += 1
        body = lines[i + 2:j]
        i = j + 1
        second_poll = "write_poll_rate" in fname
        for labels, stmts, before in case_groups(body):
            calls = builders_in(stmts)
            tx_only = TX.search("\n".join(stmts))
            for label in labels:
                pid = macros.get(label)
                if pid is None:
                    continue
                if calls:
                    for builder, args, tx in calls:
                        record(pid, builder, args, tx)
                elif tx_only and before:
                    for builder, args, _ in before:
                        record(pid, builder, args, int(tx_only.group(1), 16))
                # the receiver half of a polling-rate write: set_polling_rate2(rate, 0x01)
                if second_poll:
                    for builder, args, tx in calls:
                        if builder == "misc_set_polling_rate2" and args.split(",")[-1].strip() in ("0x01", "1"):
                            first = uses.get(pid, {}).get(builder, {}).get("tx") or 0x1F
                            sequences[pid] = [{"transactionId": f"{first:02X}", "fixedArgument0": "00"},
                                              {"transactionId": f"{tx if tx is not None else first:02X}", "fixedArgument0": "01"}]

result = []
for pid, d in sorted(devices.items()):
    b = uses.get(pid, {})
    d["transactions"] = {"razer_chroma_" + k: f"{v['tx']:02X}" if v["tx"] is not None else "1F" for k, v in sorted(b.items())}
    d["builderArgumentPrefixes"] = {"razer_chroma_" + k: v["prefixes"] for k, v in sorted(b.items()) if v["prefixes"]}
    d["commandSequences"] = {"razer_chroma_misc_set_polling_rate2": sequences[pid]} if pid in sequences else {}
    result.append(d)

commit = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
out.write_text(json.dumps({"source": {"repository": "openrazer/openrazer", "commit": commit}, "devices": result},
                          ensure_ascii=False, separators=(",", ":")), encoding="utf-8")
cats = {}
for d in result:
    cats[d["category"]] = cats.get(d["category"], 0) + 1
print(f"{len(result)} devices {cats} -> {out}")
