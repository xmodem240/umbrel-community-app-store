"""Simulated Bitaxe (AxeOS API) for testing the tuner. Not part of the shipped app.

Model: stable ceiling rises with voltage; error rate climbs near/over the ceiling;
temperature and power rise with frequency and voltage.

Environment:
  MOCK_BOARD   board version to emulate, from src/boards.json (default "201" = Ultra)
  MOCK_OLD_FW  1 = older firmware: no /api/system/asic, no nominalVoltage/maxPower in info
  MOCK_VIN     input voltage in mV (default: board nominal + 0.1 V)
  MOCK_NO_VR   1 = board doesn't report VR temperature
On multi-chip boards the second chip (temp2) runs 3 °C hotter than the first.
"""
import json
import os
import sys
import threading
import time

from flask import Flask, jsonify, request

HERE = os.path.dirname(os.path.abspath(__file__))
with open(os.path.join(HERE, "..", "src", "boards.json")) as f:
    BOARDS = json.load(f)["boards"]
BV = os.environ.get("MOCK_BOARD", "201")
B = BOARDS.get(BV, {"family": "Custom", "asic": "BM1366", "chips": 1, "default_freq": 485,
                    "default_volt": 1200, "freq_options": [], "volt_options": [],
                    "gauge_max_power": 25, "nominal_v": 5})
OLD_FW = bool(os.environ.get("MOCK_OLD_FW"))
CHIPS = B["chips"]
F0, V0 = B["default_freq"], B["default_volt"]

app = Flask(__name__)
S = {"freq": F0, "volt": V0, "boot": 0.0, "acc": 0, "rej": 0, "last": time.time()}
lock = threading.Lock()


def ceiling(v):  # max stable MHz at a core voltage (Ultra: 500 MHz at 1200 mV)
    return F0 * 1.031 + (v - V0) * 3.0


def model():
    f, v = S["freq"], S["volt"]
    over = f - ceiling(v)
    exp = f * 1.2 * CHIPS
    eff = 1.0 if over <= 0 else max(0.0, 1 - over / 40)
    err = 0.2 if over < -20 else (0.6 if over <= 0 else 2 + over * 0.5)
    per_chip = 8 + (f / F0) ** 1.2 * (v / V0) ** 2 * 6
    temp = 40 + (per_chip - 8) * 1.9
    return exp, exp * eff, err, per_chip * CHIPS, temp


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
        d = dict(hostname="bitaxe-sim", ASICModel=B["asic"], deviceModel=B["family"],
                 boardVersion=BV, version="v2.4.0", asicCount=CHIPS,
                 smallCoreCount=1280, frequency=S["freq"], coreVoltage=S["volt"],
                 hashRate=h, expectedHashrate=exp, errorPercentage=err, power=power,
                 temp=temp, temp2=(temp + 3) if CHIPS > 1 else 0,
                 vrTemp=None if os.environ.get("MOCK_NO_VR") else temp + 8,
                 voltage=int(os.environ.get("MOCK_VIN", str(B["nominal_v"] * 1000 + 100))), fanspeed=70,
                 sharesAccepted=S["acc"], sharesRejected=S["rej"], overheat_mode=0)
        if not OLD_FW:
            d.update(nominalVoltage=B["nominal_v"], maxPower=B["gauge_max_power"])
        return jsonify(d)


@app.get("/api/system/asic")
def asic():
    if OLD_FW:
        return ("not found", 404)
    return jsonify(ASICModel=B["asic"], deviceModel=B["family"], asicCount=CHIPS,
                   defaultFrequency=F0, frequencyOptions=B["freq_options"],
                   defaultVoltage=V0, voltageOptions=B["volt_options"])


@app.patch("/api/system")
def patch():
    d = request.json or {}
    with lock:
        S["freq"] = d.get("frequency", S["freq"])
        S["volt"] = d.get("coreVoltage", S["volt"])
    return jsonify(ok=True)


@app.post("/api/system/restart")
def restart():
    with lock:
        S["boot"] = time.time() + 2
    return jsonify(ok=True)


if __name__ == "__main__":
    app.run(port=int(sys.argv[1]) if len(sys.argv) > 1 else 9100)
