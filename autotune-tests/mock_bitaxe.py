"""Simulated miner (AxeOS-style API) for testing the autotuner. Not part of the shipped app.
Copied from tests/mock_bitaxe.py (Bitaxe Tuner) and extended for the live guard.

Model: stable ceiling rises with voltage; error rate climbs near/over the ceiling;
temperature and power rise with frequency and voltage.

Environment:
  MOCK_FW      bitaxe (default) | nerd (NerdQAxe++ firmware) | nexus (Nexus S1)
  MOCK_BOARD   bitaxe: board version from src/boards.json (default "201" = Ultra)
  MOCK_OLD_FW  bitaxe: older firmware without /api/system/asic, nominalVoltage, maxPower
  MOCK_REV7    nerd: NerdQAxe++ rev 7 limits (120 W, up to 1000 MHz / 1300 mV)
  MOCK_NEXUS_ASIC  nexus: also serve /api/system/asic (with made-up test defaults)
  MOCK_VIN     input voltage in mV (default: nominal + 0.1 V)
  MOCK_NO_VR   board doesn't report VR temperature
  MOCK_LIVE    1 (default for bitaxe): PATCH takes effect at once, like current ESP-Miner, and
               actualFrequency / coreVoltageActual report it.
               0: PATCH only saves; the change takes effect at the next restart (actualFrequency stale).
               nofield (default for nerd/nexus): like 0 and actualFrequency isn't reported.
  MOCK_BOOT_S  seconds the miner is unreachable after a restart (default 2)
Test controls (not part of AxeOS): POST /mock/set {"ambient": +C, "ceiling": +MHz, "overheat": 0/1,
  "throttle_to": MHz}; GET /mock/stats -> restarts, patches, saved and applied settings.
On multi-chip boards the chips run progressively hotter (+3 C for the hottest).
"""
import json
import os
import sys
import threading
import time

from flask import Flask, jsonify, request

HERE = os.path.dirname(os.path.abspath(__file__))
FW = os.environ.get("MOCK_FW", "bitaxe")
OLD_FW = bool(os.environ.get("MOCK_OLD_FW"))

if FW == "nerd":
    rev7 = bool(os.environ.get("MOCK_REV7"))
    B = {"family": "NerdQAxe++", "asic": "BM1370", "chips": 4, "default_freq": 600,
         "default_volt": 1200 if rev7 else 1150, "nominal_v": 12,
         "freq_options": [500, 515, 525, 550, 575, 590, 600] + ([625, 650, 675, 700, 725, 750, 775, 800, 825, 850, 875, 900, 925, 950, 975, 1000] if rev7 else []),
         "volt_options": list(range(1120, (1300 if rev7 else 1200) + 1, 10)),
         "abs_max_freq": 1000 if rev7 else 800, "abs_max_volt": 1400,
         "max_pin": 120.0 if rev7 else 100.0, "min_pin": 45.0 if rev7 else 52.0}
    PSCALE, TOFF = 1.34, 4
elif FW == "nexus":
    B = {"family": "Nexus S1", "asic": "BM1373", "chips": 4, "default_freq": 450, "default_volt": 1050,
         "nominal_v": 12, "freq_options": [], "volt_options": [], "gauge_max_power": 120}
    PSCALE, TOFF = 1.84, 8
else:
    with open(os.path.join(HERE, "..", "autotune-src", "boards.json")) as f:
        BOARDS = json.load(f)["boards"]
    BV = os.environ.get("MOCK_BOARD", "201")
    B = BOARDS.get(BV, {"family": "Custom", "asic": "BM1366", "chips": 1, "default_freq": 485,
                        "default_volt": 1200, "freq_options": [], "volt_options": [],
                        "gauge_max_power": 25, "nominal_v": 5})
    PSCALE, TOFF = 1.0, 0
CHIPS = B["chips"]
F0, V0 = B["default_freq"], B["default_volt"]

LIVE = os.environ.get("MOCK_LIVE", "1" if FW == "bitaxe" else "nofield")
BOOT_S = float(os.environ.get("MOCK_BOOT_S", "2"))
app = Flask(__name__)
# freq/volt are the saved (NVS) settings that "frequency"/"coreVoltage" report; afreq/avolt are what
# the chip actually runs at ("actualFrequency"/"coreVoltageActual").
S = {"freq": F0, "volt": V0, "afreq": F0, "avolt": V0, "boot": 0.0, "acc": 0, "rej": 0, "last": time.time(),
     "ambient": 0.0, "dceil": 0.0, "overheat": 0, "restarts": 0, "patches": 0}
lock = threading.Lock()


def ceiling(v):  # max stable MHz at a core voltage (Ultra: 500 MHz at 1200 mV)
    return F0 * 1.031 + (v - V0) * 3.0 + S["dceil"]


def model():
    f, v = S["afreq"], S["avolt"]
    over = f - ceiling(v)
    exp = f * 1.2 * CHIPS
    eff = 1.0 if over <= 0 else max(0.0, 1 - over / 40)
    err = 0.2 if over < -20 else (0.6 if over <= 0 else 2 + over * 0.5)
    per_chip = 8 + (f / F0) ** 1.2 * (v / V0) ** 2 * 6
    temp = 40 + (per_chip - 8) * 1.9 + TOFF + S["ambient"]
    return exp, exp * eff, err, per_chip * CHIPS * PSCALE, temp


@app.get("/api/system/info")
def info():
    with lock:
        if time.time() < S["boot"]:
            return ("rebooting", 503)
        dt = time.time() - S["last"]
        S["last"] = time.time()
        exp, h, err, power, temp = model()
        S["acc"] += int(dt * 2)
        if err > 1.5:
            S["rej"] += int(dt * 0.5 * err)
        vin = int(os.environ.get("MOCK_VIN", str(B["nominal_v"] * 1000 + 100)))
        common = dict(hostname="miner-sim", ASICModel=B["asic"], deviceModel=B["family"], asicCount=CHIPS,
                      smallCoreCount=1200, frequency=S["freq"], coreVoltage=S["volt"], hashRate=h, power=power,
                      vrTemp=None if os.environ.get("MOCK_NO_VR") else temp + 8, voltage=vin,
                      sharesAccepted=S["acc"], sharesRejected=S["rej"], version="v2.4.0")
        if LIVE != "nofield":
            common.update(actualFrequency=float(S["afreq"]), coreVoltageActual=float(S["avolt"]) - 4)
        if FW == "nerd":
            # NerdQAxe firmware: hottest chip in temp, per-chip asicTemps, rated limits,
            # no errorPercentage / expectedHashrate / boardVersion
            chips = [temp - 3 + i for i in range(CHIPS)]
            return jsonify(**common, temp=max(chips), asicTemps=chips, fanspeed=70,
                           maxPower=B["max_pin"], minPower=B["min_pin"], maxVoltage=13.0, minVoltage=11.0,
                           defaultFrequency=F0, defaultCoreVoltage=V0, overheat_temp=70)
        d = dict(**common, temp=temp, temp2=(temp + 3) if CHIPS > 1 else 0, errorPercentage=err,
                 expectedHashrate=exp, fanspeed=70, overheat_mode=S["overheat"],
                 boardVersion=os.environ.get("MOCK_BOARD", "201") if FW == "bitaxe" else "N1")
        if not OLD_FW and FW == "bitaxe":
            d.update(nominalVoltage=B["nominal_v"], maxPower=B["gauge_max_power"])
        return jsonify(d)


@app.get("/api/system/asic")
def asic():
    if (FW == "bitaxe" and OLD_FW) or (FW == "nexus" and not os.environ.get("MOCK_NEXUS_ASIC")):
        return ("not found", 404)
    d = dict(ASICModel=B["asic"], deviceModel=B["family"], asicCount=CHIPS, defaultFrequency=F0,
             frequencyOptions=B["freq_options"], defaultVoltage=V0, voltageOptions=B["volt_options"])
    if FW == "nerd":
        d.update(absMaxFrequency=B["abs_max_freq"], absMaxVoltage=B["abs_max_volt"])
    return jsonify(d)


@app.patch("/api/system")
def patch():
    d = request.json or {}
    with lock:
        S["patches"] += 1
        S["freq"] = d.get("frequency", S["freq"])
        S["volt"] = d.get("coreVoltage", S["volt"])
        if LIVE == "1":
            S["afreq"], S["avolt"] = S["freq"], S["volt"]
    return jsonify(ok=True)


@app.post("/api/system/restart")
def restart():
    with lock:
        S["restarts"] += 1
        S["boot"] = time.time() + BOOT_S
        if FW == "nerd":   # NerdQAxe firmware clamps to its absolute maximums at boot
            S["freq"] = min(S["freq"], B["abs_max_freq"])
            S["volt"] = min(S["volt"], B["abs_max_volt"])
        S["afreq"], S["avolt"] = S["freq"], S["volt"]
    return jsonify(ok=True)


@app.post("/mock/set")
def mock_set():
    d = request.json or {}
    with lock:
        if "ambient" in d:
            S["ambient"] = float(d["ambient"])
        if "ceiling" in d:
            S["dceil"] = float(d["ceiling"])
        if "overheat" in d:
            S["overheat"] = int(d["overheat"])
        if "throttle_to" in d:   # the firmware lowers its saved and running frequency itself
            S["freq"] = S["afreq"] = d["throttle_to"]
    return jsonify(ok=True)


@app.get("/mock/stats")
def mock_stats():
    with lock:
        return jsonify({k: S[k] for k in ("freq", "volt", "afreq", "avolt", "restarts", "patches", "ambient")})


if __name__ == "__main__":
    app.run(port=int(sys.argv[1]) if len(sys.argv) > 1 else 9100)
