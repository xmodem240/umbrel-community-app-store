import json, re, sys
src = sys.argv[1]; tbl = json.load(open(sys.argv[2]))["nerd"]
files = {"NerdAxe": "nerdaxe", "NerdAxeGamma": "nerdaxegamma", "NerdAxeGaia": "nerdaxegaia",
         "NerdQAxe+": "nerdqaxeplus", "NerdQAxe++": "nerdqaxeplus2", "Q1373": "q1373"}
def grab(txt, name):
    m = re.search(rf"\b{name}\s*=\s*(?:m_\w+\s*=\s*)?([^;]+);", txt)
    return m.group(1).strip() if m else None
bad = 0
for model, f in files.items():
    txt = open(f"{src}/{f}.cpp").read()
    txt = txt.split("applyRev7Profile")[0]           # base profile only
    t = tbl[model]
    checks = {
      "freq_options": [int(x) for x in re.findall(r"\d+", grab(txt, "m_asicFrequencies") or "")],
      "volt_options": [int(x) for x in re.findall(r"\d+", grab(txt, "m_asicVoltages") or "")],
      "default_freq": grab(txt, "m_defaultAsicFrequency"), "default_volt": grab(txt, "m_defaultAsicVoltageMillis"),
      "gauge_max_power": grab(txt, "m_maxPin"), "asic": grab(txt, "m_asicModel"), "chips": grab(txt, "m_asicCount"),
      "vin": [grab(txt, "m_minVin"), grab(txt, "m_maxVin")],
    }
    for k, v in checks.items():
        if v in (None, [], [None, None]): continue
        if k in ("default_freq", "default_volt", "chips"): v = int(re.findall(r"\d+", v)[0])
        if k == "gauge_max_power": v = float(re.findall(r"[\d.]+", v)[0])
        if k == "asic": v = v.strip('"')
        if k == "vin": v = [float(re.findall(r"[\d.]+", x)[0]) if x else None for x in v]
        exp = t[k]
        ok = (v == exp) if k != "vin" else all(a is None or a == b for a, b in zip(v, exp))
        if not ok: bad += 1; print(f"MISMATCH {model}.{k}: source={v} table={exp}")
print("checked", len(files), "boards;", "all match" if not bad else f"{bad} mismatches")
