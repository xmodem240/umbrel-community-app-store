"""Generate boards.json from ESP-Miner's main/device_config.h (factory defaults per board)."""
import json, re, sys
h = open(sys.argv[1]).read()
commit = sys.argv[2]
arrays = {m[0]: [int(x) for x in m[1].split(",") if x.strip() and int(x) != 0]
          for m in re.findall(r"static const uint16_t (\w+)\[\]\s*=\s*\{([^}]*)\}", h)}
def fields(body):
    return dict(re.findall(r"\.(\w+)\s*=\s*([^,]+?)\s*(?:,|$)", body))
asics = {}
for name, body in re.findall(r"static const AsicConfig (ASIC_\w+)\s*=\s*\{(.*?)\};", h, re.S):
    f = fields(body)
    asics[name] = {"asic": f["name"].strip('"'), "default_freq": int(f["default_frequency_mhz"]),
                   "freq_options": arrays[f["frequency_options"]], "default_volt": int(f["default_voltage_mv"]),
                   "volt_options": arrays[f["voltage_options"]], "small_cores": int(f["small_core_count"])}
families = {}
for name, body in re.findall(r"static const FamilyConfig (FAMILY_\w+)\s*=\s*\{(.*?)\};", h, re.S):
    f = fields(body)
    families[name] = {"family": f["name"].strip('"'), **asics[f["asic"]], "chips": int(f["asic_count"]),
                      "gauge_max_power": int(f["max_power"]), "nominal_v": int(f["nominal_voltage"])}
boards = {}
block = re.search(r"default_configs\[\]\s*=\s*\{(.*?)\n\};", h, re.S).group(1)
for bv, fam in re.findall(r'\.board_version\s*=\s*"([^"]+)"\s*,\s*\.family\s*=\s*(FAMILY_\w+)', block):
    boards[bv] = families[fam]
out = {"source": f"bitaxeorg/ESP-Miner main/device_config.h @ {commit}", "boards": boards}
json.dump(out, open(sys.argv[3], "w"), indent=1)
print(len(boards), "boards:", ", ".join(f"{k}={v['family']}" for k, v in boards.items()))
