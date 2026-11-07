#!/usr/bin/env python3
"""
AxePool - firmware update check tests.

Uses the maintainer's two real miner API responses (fixtures/): a Naja Duo on
AxeOS v2.15.4 and a NerdQaxe++ on v1.1.0.1. Release lists mimic the GitHub API
(tag_name, prerelease, draft, published_at, html_url) with tags that exist in
bitaxeorg/esp-miner and shufps/ESP-Miner-NerdQAxePlus. Offline, stdlib only:
    cd server && python3 test_firmware_check.py
"""
import asyncio
import http.server
import json
import os
import sys
import tempfile
import threading

DATA_DIR = tempfile.mkdtemp(prefix="axepool-fw-test-")
os.environ["APP_DATA_DIR"] = DATA_DIR
for k in list(os.environ):
    if k.startswith(("SOLOBCH_", "BCHN_")):
        del os.environ[k]
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import logging                                            # noqa: E402
import firmware_check as fc                               # noqa: E402
from firmware_check import (FirmwareChecker, compare,     # noqa: E402
                            latest_stable, parse_version)
from miner_poller import MinerPoller, normalize           # noqa: E402
import status                                             # noqa: E402

logging.getLogger("solobch").setLevel(logging.CRITICAL)
ok = fail = 0


def check(label, cond):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}")


def fixture(n):
    with open(os.path.join(HERE, "fixtures", n)) as f:
        return json.load(f)


AXE = fixture("axeos_v2.15.4_naja_duo.json")
NERD = fixture("nerdqaxe_v1.1.0.1_nerdqaxepp_shutdown.json")


def rel(tag, pre=False, draft=False, date="2026-09-01T00:00:00Z", url=True, repo="bitaxeorg/ESP-Miner"):
    r = {"tag_name": tag, "prerelease": pre, "draft": draft, "published_at": date}
    if url:
        r["html_url"] = f"https://github.com/{repo}/releases/tag/{tag}"
    return r


# Newest first, as the API lists them: the late patch to an older line comes first.
AXE_RELEASES = [rel("v2.16.0", draft=True),
                rel("v2.14.3", date="2026-10-28T00:00:00Z"),      # late patch to an older line
                rel("v2.15.4", date="2026-10-20T00:00:00Z"),
                rel("v2.15.2rc0", pre=True), rel("v2.15.0rc1", pre=True),
                rel("v2.14.2"), rel("nightly-build")]
NERD_REPO = "shufps/ESP-Miner-NerdQAxePlus"
NERD_RELEASES = [rel("v2.3.0b7", pre=True, repo=NERD_REPO),
                 rel("v2.2.3-rc1", repo=NERD_REPO),                # rc tag not flagged pre
                 rel("v2.2.2", date="2026-08-14T00:00:00Z", repo=NERD_REPO),
                 rel("v1.1.1-rc1", pre=True, repo=NERD_REPO), rel("v1.1.0.1", repo=NERD_REPO)]


def test_versions():
    check("parse v2.15.4", parse_version("v2.15.4") == ((2, 15, 4), False))
    check("parse NerdQaxe 4-part v1.1.0.1", parse_version("v1.1.0.1") == ((1, 1, 0, 1), False))
    check("parse without v / capital V", parse_version("2.15") == ((2, 15), False)
          and parse_version("V2.1.0") == ((2, 1, 0), False))
    check("suffix marks a custom/test build", parse_version("v2.15.4-dirty") == ((2, 15, 4), True)
          and parse_version("v2.15.0rc1") == ((2, 15, 0), True))
    check("unparseable -> None", all(parse_version(x) is None
                                     for x in ("nightly", "", None, "v", ".1", 5)))
    check("compare pads with zeros", compare((2, 15, 4), (2, 15, 4, 0)) == 0)
    check("compare by number, not text (2.10 > 2.9)", compare((2, 10), (2, 9)) == 1)
    check("1.1.0.1 < 2.2.2", compare((1, 1, 0, 1), (2, 2, 2)) == -1)


def test_latest():
    a = latest_stable(AXE_RELEASES, "axeos")
    check("AxeOS: drafts, pre-releases and non-version tags skipped -> v2.15.4",
          a and a["tag"] == "v2.15.4")
    check("AxeOS: a late patch to an older line (v2.14.3) does not win", a["tag"] != "v2.14.3")
    n = latest_stable(NERD_RELEASES, "nerdqaxe")
    check("NerdQaxe: -rc tag skipped even if not flagged (firmware's own rule) -> v2.2.2",
          n and n["tag"] == "v2.2.2")
    check("release-notes link kept", n["url"] == f"https://github.com/{NERD_REPO}/releases/tag/v2.2.2")
    odd = latest_stable([rel("v3.0.0", url=False), dict(rel("v2.0.0"), html_url="javascript:alert(1)")],
                        "axeos")
    check("missing link rebuilt from the repo",
          odd["url"] == "https://github.com/bitaxeorg/esp-miner/releases/tag/v3.0.0")
    odd = latest_stable([dict(rel("v3.0.0"), html_url="javascript:alert(1)")], "axeos")
    check("non-GitHub link replaced (no script URLs reach the page)",
          odd["url"].startswith("https://github.com/"))
    check("junk input -> None", latest_stable({"message": "rate limited"}, "axeos") is None
          and latest_stable([1, None, "x", {}], "axeos") is None)


class Clock:
    def __init__(self):
        self.t = 1_780_000_000.0

    def __call__(self):
        return self.t


def test_checker():
    clock = Clock()
    calls = []
    lists = {"axeos": AXE_RELEASES, "nerdqaxe": NERD_RELEASES}

    def fetch(fw):
        calls.append(fw)
        v = lists[fw]
        if isinstance(v, Exception):
            raise v
        return v

    path = os.path.join(DATA_DIR, "firmware.json")
    c = FirmwareChecker(fetch=fetch, now=clock, path=path)
    axe, nerd = normalize(AXE), normalize(NERD)
    check("before any check: 'unchecked'", c.status("axeos", axe["version"])["state"] == "unchecked")
    c.refresh({"axeos"})
    check("only firmwares in use are fetched", calls == ["axeos"])
    s = c.status("axeos", axe["version"])
    check("your Naja Duo (AxeOS v2.15.4) -> up to date", s["state"] == "current"
          and s["latest"] == "v2.15.4")
    c.refresh({"axeos", "nerdqaxe"})
    s = c.status("nerdqaxe", nerd["version"])
    check("your NerdQaxe (v1.1.0.1) -> update available to v2.2.2, with notes link and date",
          s["state"] == "update" and s["latest"] == "v2.2.2"
          and s["url"].endswith("/releases/tag/v2.2.2") and s["published_at"].startswith("2026-08-14"))
    check("ahead of stable (beta) recognised", c.status("nerdqaxe", "v2.3.0b7")["state"] == "ahead")
    check("custom build of the latest recognised", c.status("axeos", "v2.15.4-dirty")["state"] == "custom")
    check("unknown firmware / version handled",
          c.status("braiins", "1.0")["state"] == "unknown"
          and c.status("axeos", "weird")["state"] == "unknown")

    calls.clear()
    clock.t += fc.CHECK_INTERVAL - 60
    c.refresh({"axeos", "nerdqaxe"})
    check("no refetch inside 6 hours", calls == [])
    clock.t += 120
    c.refresh({"axeos"})
    check("refetched after 6 hours", calls == ["axeos"])

    # Failure keeps the last good answer and retries in 30 minutes.
    lists["axeos"] = OSError("HTTP Error 403: rate limit exceeded")
    clock.t += fc.CHECK_INTERVAL + 1
    c.refresh({"axeos"})
    s = c.status("axeos", "v2.15.4")
    check("GitHub failure keeps the last good result and shows the error",
          s["state"] == "current" and "rate limit" in s["error"])
    check("retry after 30 minutes, not 6 hours",
          abs(c.state["axeos"]["next_at"] - (clock.t + fc.RETRY_INTERVAL)) < 1)
    lists["axeos"] = {"message": "Not Found"}
    clock.t += fc.RETRY_INTERVAL + 1
    c.refresh({"axeos"})
    check("garbage reply is an error, not a crash", "no stable release" in c.status("axeos", "v2.15.4")["error"])

    # Restart: the cache is reused, nothing refetched.
    calls.clear()
    c2 = FirmwareChecker(fetch=fetch, now=clock, path=path)
    s = c2.status("nerdqaxe", "v1.1.0.1")
    check("after a restart the cached result is shown", s["state"] == "update" and s["latest"] == "v2.2.2")
    check("...with its schedule kept",
          c2.state["axeos"]["next_at"] == c.state["axeos"]["next_at"] > clock.t)
    c2.refresh({"axeos"})
    check("...and not refetched before its next check is due", calls == [])
    with open(path, "w") as f:
        f.write("{not json")
    check("corrupt cache ignored", FirmwareChecker(fetch=fetch, now=clock, path=path).state == {})

    # Off: nothing fetched, cards say so.
    off = FirmwareChecker(fetch=fetch, now=clock, path="", enabled=False)
    calls.clear()
    off.refresh({"axeos", "nerdqaxe"})
    check("switched off: nothing contacts GitHub", calls == []
          and off.status("axeos", "v2.15.4") == {"state": "off"})


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        self.server.seen = (self.path, self.headers.get("User-Agent"))
        big = self.server.mode == "huge"
        body = json.dumps(AXE_RELEASES + ([{"pad": "x" * (fc.MAX_RESPONSE + 10)}] if big else [])).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def test_http():
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    srv.mode = "ok"
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    real = fc.API
    fc.API = f"http://127.0.0.1:{srv.server_address[1]}/repos/{{repo}}/releases?per_page=50"
    try:
        got = fc.fetch_releases("axeos")
        check("HTTP: release list fetched for the right repo, AxePool user agent",
              latest_stable(got, "axeos")["tag"] == "v2.15.4"
              and srv.seen[0].startswith("/repos/bitaxeorg/esp-miner/releases")
              and srv.seen[1] == "AxePool")
        srv.mode = "huge"
        try:
            fc.fetch_releases("axeos")
            check("oversize reply refused", False)
        except Exception as e:                    # must be the size cap, nothing else
            check("oversize reply refused by the size cap", "too large" in str(e))
    finally:
        fc.API = real
        srv.shutdown()
    check("production URL is the GitHub API for the fixed repos",
          fc.API.startswith("https://api.github.com/repos/{repo}/releases"))


def test_devices_and_settings():
    # The miner is added the way a user does it (Settings), so later saves keep it.
    poller = MinerPoller(fetch=lambda ip, port: NERD)
    checker = FirmwareChecker(fetch=lambda fw: NERD_RELEASES, path="")
    checker.refresh({"nerdqaxe"})

    class Jobs:
        def apply_config(self, cfg):
            pass

    class Srv:
        clients = set()
    st = status.StatusServer(Srv(), Jobs(), "127.0.0.1", 0, poller=poller, firmware=checker)

    async def go():
        await st._route("POST", "/config", {"content-type": "application/json"},
                        json.dumps({"miner_api_hosts": "192.168.50.16"}).encode())
        await poller.poll_due()
        r = await st._route("GET", "/devices", {}, b"")
        dev = json.loads(r[2])
        body = json.dumps({"firmware_check_enabled": False}).encode()
        r2 = await st._route("POST", "/config", {"content-type": "application/json"}, body)
        r3 = await st._route("GET", "/devices", {}, b"")
        bad = await st._route("POST", "/config", {"content-type": "application/json"},
                              json.dumps({"firmware_check_enabled": "maybe"}).encode())
        return dev, r2[0], json.loads(r3[2]), bad[0]
    dev, saved, dev_off, bad = asyncio.run(go())
    fs = dev["miners"][0]["firmware_status"]
    check("/devices carries each miner's firmware status", fs["state"] == "update"
          and fs["current"] == "v1.1.0.1" and fs["latest"] == "v2.2.2")
    check("Settings switch applies live", saved.startswith("200") and checker.enabled is False
          and dev_off["miners"][0]["firmware_status"] == {"state": "off"})
    check("switch must be true/false (on/off words allowed, 'maybe' is not)", bad.startswith("400"))
    poller.close()


if __name__ == "__main__":
    test_versions()
    test_latest()
    test_checker()
    test_http()
    test_devices_and_settings()
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(0 if fail == 0 else 1)
