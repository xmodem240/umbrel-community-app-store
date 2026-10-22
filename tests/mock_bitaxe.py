"""Simulated Bitaxe (AxeOS API) for testing the tuner. Not part of the shipped app.

Model: stable ceiling rises with voltage; error rate climbs near/over the ceiling;
temperature and power rise with frequency and voltage.
"""
import os
import sys
import threading
import time

from flask import Flask, jsonify, request

app = Flask(__name__)
S = {"freq": 485, "volt": 1200, "boot": 0.0, "acc": 0, "rej": 0, "last": time.time()}
CORES = 1280  # GH/s = freq * 1.2 (BM1366-ish)
lock = threading.Lock()


def ceiling(v):  # max stable MHz at a core voltage
    return 500 + (v - 1200) * 3.0


def model():
    f, v = S["freq"], S["volt"]
    over = f - ceiling(v)
    exp = f * 1.2
    eff = 1.0 if over <= 0 else max(0.0, 1 - over / 40)
    err = 0.2 if over < -20 else (0.6 if over <= 0 else 2 + over * 0.5)
    power = 8 + (f / 485) ** 1.2 * (v / 1200) ** 2 * 6
    temp = 40 + (power - 8) * 1.9
    return exp, exp * eff, err, power, temp


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
        return jsonify(hostname="bitaxe-sim", ASICModel="BM1366", deviceModel="Ultra",
                       boardVersion="201", version="v2.4.0", asicCount=1,
                       smallCoreCount=CORES // 1, frequency=S["freq"], coreVoltage=S["volt"],
                       hashRate=h, expectedHashrate=exp, errorPercentage=err, power=power,
                       temp=temp, vrTemp=temp + 8, voltage=int(os.environ.get("MOCK_VIN", "5100")), fanspeed=70,
                       sharesAccepted=S["acc"], sharesRejected=S["rej"])


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
