#!/usr/bin/env python3
"""
AxePool - read-only poller for the miners' own HTTP API (AxeOS / NerdQaxe).

Every POLL_INTERVAL seconds it reads GET /api/system/info from each known miner
and keeps a normalized snapshot: the miner's own hashrate, temperatures, power,
fan, and whether it is mining. It never writes to a miner.

Which miners:
  * auto-discovered: the LAN address of every authorized Stratum connection;
  * manual: addresses listed in Settings (for miners whose address the pool
    cannot see, e.g. behind another router).
Only private IPv4 addresses are ever contacted, the path is fixed, redirects
are refused and responses are size-capped, so the Settings field cannot be
used to make the app fetch arbitrary URLs.

Field units, verified against the firmware sources (2026-11):
  * AxeOS / ESP-Miner (bitaxeorg/ESP-Miner main/http_server/system_api_json.c):
    hashRate* and expectedHashrate in GH/s, power W, voltage mV, current mA,
    temp/temp2 = average chip temperature of chip groups 1/2, vrTemp C.
  * NerdQaxe (shufps/ESP-Miner-NerdQAxePlus main/http_server/handler_system.cpp):
    hashRate* GH/s (0 while shut down), power W, voltage mV, current mA,
    temp = hottest chip, asicTemps = per chip, shutdown = bool.
Both firmwares only answer requests from private-range addresses; with no
Origin header the source IP is what is checked, so none is sent.

Personal data the API also returns (Wi-Fi SSID, MAC address, pool passwords,
fallback pool accounts) is never copied out of the raw response.

Standard library only. HTTP runs on a small thread pool so a slow or dead miner
never blocks the event loop.
"""

import asyncio
import concurrent.futures
import ipaddress
import json
import logging
import math
import time
import urllib.error
import urllib.request

log = logging.getLogger("solobch.poller")

POLL_INTERVAL = 10.0          # seconds between reads of a healthy miner
HTTP_TIMEOUT = 3.0            # per request
MAX_BACKOFF = 120.0           # slowest retry for an unreachable miner
OFFLINE_AFTER = 3             # consecutive failures before a miner shows offline
MAX_RESPONSE = 256 * 1024     # bytes; real responses are ~4-6 KB
MAX_TARGETS = 64
FORGET_AUTO_AFTER = 3600.0    # drop an auto-discovered address unseen this long
API_PATH = "/api/system/info"
MAX_MANUAL = 32


# --------------------------------------------------------------------------- #
# addresses
# --------------------------------------------------------------------------- #
def parse_target(text):
    """'192.168.1.20' or '192.168.1.20:8080' -> ('192.168.1.20', 80|8080).
    Raises ValueError unless it is a private (RFC 1918) IPv4 address."""
    s = str(text).strip()
    host, port = s, 80
    if ":" in s:
        host, _, p = s.rpartition(":")
        if not p.isdigit() or not 1 <= int(p) <= 65535:
            raise ValueError(f"{s}: port must be 1-65535")
        port = int(p)
    try:
        ip = ipaddress.IPv4Address(host)
    except ValueError:
        raise ValueError(f"{s}: must be an IPv4 address like 192.168.1.20") from None
    if not ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_unspecified \
            or ip.is_multicast or ip.is_reserved:
        raise ValueError(f"{s}: must be a private LAN address (10.x, 172.16-31.x, 192.168.x)")
    return str(ip), port


def parse_target_list(text):
    """Settings field -> list of (ip, port). Accepts commas, spaces, newlines.
    Raises ValueError naming the first bad entry."""
    if text is None:
        return []
    parts = [p for p in str(text).replace(",", " ").split() if p]
    if len(parts) > MAX_MANUAL:
        raise ValueError(f"at most {MAX_MANUAL} miner addresses")
    out = []
    for p in parts:
        t = parse_target(p)
        if t not in out:
            out.append(t)
    return out


# --------------------------------------------------------------------------- #
# normalization (pure; unit-tested against real responses in fixtures/)
# --------------------------------------------------------------------------- #
def _num(v, lo=None, hi=None):
    """A finite float within [lo, hi], else None. Firmware sometimes sends
    strings or garbage; one bad field must not poison the rest."""
    if isinstance(v, bool) or v is None:
        return None
    try:
        f = float(v)
    except (TypeError, ValueError):
        return None
    if not math.isfinite(f):
        return None
    if (lo is not None and f < lo) or (hi is not None and f > hi):
        return None
    return f


def _str(v, n=64):
    return v[:n] if isinstance(v, str) and v else None


def normalize(info, ip=None):
    """Raw /api/system/info JSON -> AxePool's common miner record, or None when
    the response does not look like a miner at all."""
    if not isinstance(info, dict) or "hashRate" not in info:
        return None
    nerd = "deviceModel" in info or "stratum" in info and isinstance(info.get("stratum"), dict)
    hr = _num(info.get("hashRate"), 0, 1e7)                  # GH/s; 10 PH/s ceiling
    temps = [t for t in (_num(info.get("temp"), 1, 150), _num(info.get("temp2"), 1, 150))
             if t is not None]
    if isinstance(info.get("asicTemps"), list):
        temps += [t for t in (_num(x, 1, 150) for x in info["asicTemps"][:64]) if t is not None]
    freq = _num(info.get("frequency"), 1, 5000)
    cores = _num(info.get("smallCoreCount"), 1, 1e6)
    asics = _num(info.get("asicCount"), 1, 1024)
    expected = _num(info.get("expectedHashrate"), 0, 1e7)
    expected_derived = False
    if expected is None and freq and cores:
        # NerdQaxe reports no expected hashrate; derive it the way AxeOS does
        # (frequency MHz x small cores x chips / 1000 -> GH/s).
        expected = freq * cores * (asics or 1) / 1000.0
        expected_derived = True
    shutdown = info.get("shutdown") is True
    overheat = _num(info.get("overheat_mode")) == 1
    paused = info.get("miningPaused") is True
    if shutdown:
        state = "stopped"
    elif overheat:
        state = "overheat"
    elif paused:
        state = "paused"
    elif not hr:
        state = "idle"
    else:
        state = "mining"
    using_fallback = bool(_num(info.get("isUsingFallbackStratum")))
    user = info.get("fallbackStratumUser" if using_fallback else "stratumUser")
    v = _num(info.get("voltage"), 0, 60000)
    cur = _num(info.get("current"), 0, 1e6)
    return {
        "firmware": "nerdqaxe" if nerd else "axeos",
        "ip": ip or _str(info.get("hostip") or info.get("ipv4"), 45),
        "hostname": _str(info.get("hostname")),
        "model": _str(info.get("deviceModel")) or (
            f"board {info['boardVersion']}" if _str(info.get("boardVersion")) else None),
        "asic_model": _str(info.get("ASICModel")),
        "version": _str(info.get("version"), 32),
        "state": state,
        "hashrate_ghs": hr,
        "hashrate_1m_ghs": _num(info.get("hashRate_1m"), 0, 1e7),
        "hashrate_10m_ghs": _num(info.get("hashRate_10m"), 0, 1e7),
        "hashrate_1h_ghs": _num(info.get("hashRate_1h"), 0, 1e7),
        "expected_ghs": round(expected, 2) if expected is not None else None,
        "expected_derived": expected_derived,
        "power_w": _num(info.get("power"), 0, 10000),
        "voltage_v": round(v / 1000.0, 3) if v is not None else None,
        "current_a": round(cur / 1000.0, 3) if cur is not None else None,
        "chip_temp_c": max(temps) if temps else None,
        "vr_temp_c": _num(info.get("vrTemp"), 1, 150),
        "fan_pct": _num(info.get("fanspeed"), 0, 100),
        "fan_rpm": _num(info.get("fanrpm"), 0, 100000),
        "frequency_mhz": freq,
        "core_mv": _num(info.get("coreVoltageActual"), 0, 5000),
        "error_pct": _num(info.get("errorPercentage"), 0, 100),
        "shares_accepted": _num(info.get("sharesAccepted"), 0),
        "shares_rejected": _num(info.get("sharesRejected"), 0),
        "best_diff": _num(info.get("bestDiff"), 0),
        "pool_difficulty": _num(info.get("poolDifficulty"), 0),
        "uptime_s": _num(info.get("uptimeSeconds"), 0),
        # The Stratum username the miner is using right now (address.worker):
        # how a miner is linked to its row in the pool. Payout address only;
        # the passwords and the other pool's account are never copied.
        "stratum_user": _str(user, 200),
        "on_fallback_pool": using_fallback,
    }


# --------------------------------------------------------------------------- #
# HTTP (blocking; runs on the poller's thread pool)
# --------------------------------------------------------------------------- #
class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


_OPENER = urllib.request.build_opener(_NoRedirect, urllib.request.ProxyHandler({}))


def fetch_info(ip, port, timeout=HTTP_TIMEOUT):
    """GET http://ip:port/api/system/info -> parsed JSON. Raises on any failure."""
    url = f"http://{ip}{'' if port == 80 else f':{port}'}{API_PATH}"
    req = urllib.request.Request(url, headers={"Accept": "application/json",
                                               "User-Agent": "AxePool"})
    with _OPENER.open(req, timeout=timeout) as r:
        if r.status != 200:
            raise OSError(f"HTTP {r.status}")
        body = r.read(MAX_RESPONSE + 1)
    if len(body) > MAX_RESPONSE:
        raise OSError("response too large")
    return json.loads(body)


# --------------------------------------------------------------------------- #
# poller
# --------------------------------------------------------------------------- #
class Target:
    def __init__(self, ip, port, source):
        self.ip, self.port = ip, port
        self.sources = {source}          # "auto" and/or "manual"
        self.next_due = 0.0
        self.failures = 0
        self.ever_ok = False
        self.last_ok = None              # wall time of the last good read
        self.last_error = None
        self.last_seen_auto = time.monotonic()
        self.data = None                 # last normalized record
        self.identity = None             # pool identity (address.worker) it is linked to

    @property
    def key(self):
        return f"{self.ip}:{self.port}"

    def status(self):
        if self.failures >= OFFLINE_AFTER or (self.data is None and self.failures):
            return "offline"
        return self.data["state"] if self.data else "pending"


class MinerPoller:
    def __init__(self, fetch=fetch_info, now=time.monotonic, wall=time.time,
                 enabled=True, manual=()):
        self._fetch = fetch
        self._now = now
        self._wall = wall
        self.enabled = enabled
        self.targets = {}
        self._pool = concurrent.futures.ThreadPoolExecutor(
            max_workers=4, thread_name_prefix="miner-api")
        self.set_manual(manual)

    # ---- target management ------------------------------------------------- #
    def set_manual(self, targets):
        """Replace the manual list (from Settings). Auto targets are kept."""
        want = {f"{ip}:{port}": (ip, port) for ip, port in targets}
        for key, t in list(self.targets.items()):
            if "manual" in t.sources and key not in want:
                t.sources.discard("manual")
                if not t.sources:
                    del self.targets[key]
        for key, (ip, port) in want.items():
            if key in self.targets:
                self.targets[key].sources.add("manual")
            elif len(self.targets) < MAX_TARGETS:
                self.targets[key] = Target(ip, port, "manual")

    def note_connections(self, clients):
        """Learn miner addresses from authorized Stratum connections and remember
        which pool identity each address is mining as."""
        now = self._now()
        seen = {}
        for c in clients:
            if getattr(c, "internal", False) or not getattr(c, "authorized", False):
                continue
            peer = getattr(c, "peer", None)
            if not peer:
                continue
            try:
                ip, _ = parse_target(peer[0])
            except ValueError:
                continue                      # loopback, public, IPv6: never polled
            seen.setdefault(ip, set()).add(c.raw_user)
        for ip, users in seen.items():
            key = f"{ip}:80"
            t = self.targets.get(key)
            if t is None:
                if len(self.targets) >= MAX_TARGETS:
                    continue
                t = self.targets[key] = Target(ip, 80, "auto")
            t.sources.add("auto")
            t.last_seen_auto = now
            # One identity per address is unambiguous; several rigs behind one
            # address (NAT) are left to stratum_user matching instead.
            t.identity = next(iter(users)) if len(users) == 1 else None
        for key, t in list(self.targets.items()):
            if t.sources == {"auto"} and now - t.last_seen_auto > FORGET_AUTO_AFTER:
                del self.targets[key]
        for t in self.targets.values():
            if "auto" in t.sources and t.ip not in seen:
                t.identity = None

    # ---- polling ----------------------------------------------------------- #
    def _record(self, t, result, err):
        now = self._now()
        if err is None:
            rec = normalize(result, t.ip)
            if rec is None:
                err = "response is not a miner API (no hashRate field)"
        if err is None:
            t.data = rec
            t.failures = 0
            t.ever_ok = True
            t.last_ok = self._wall()
            t.last_error = None
            t.next_due = now + POLL_INTERVAL
        else:
            t.failures += 1
            t.last_error = str(err)[:200]
            t.next_due = now + min(MAX_BACKOFF, POLL_INTERVAL * (2 ** min(t.failures, 6)))

    async def poll_due(self):
        """Read every miner that is due, concurrently. Returns how many were read."""
        if not self.enabled:
            return 0
        now = self._now()
        due = [t for t in self.targets.values() if t.next_due <= now]
        if not due:
            return 0
        loop = asyncio.get_running_loop()

        async def one(t):
            try:
                res = await loop.run_in_executor(self._pool, self._fetch, t.ip, t.port)
                self._record(t, res, None)
            except Exception as e:                   # timeouts, refused, bad JSON...
                self._record(t, None, e.__class__.__name__ + (f": {e}" if str(e) else ""))
        await asyncio.gather(*(one(t) for t in due))
        return len(due)

    # ---- reading ----------------------------------------------------------- #
    def link(self, pool_identities):
        """identity -> normalized record, for miners currently mining to this
        pool. Linked by connection address first, then by the Stratum username
        the miner reports."""
        out = {}
        ids = set(pool_identities)
        for t in self.targets.values():
            if not t.data or t.status() == "offline":
                continue
            ident = t.identity if t.identity in ids else None
            if ident is None and t.data.get("stratum_user") in ids:
                ident = t.data["stratum_user"]
            if ident and ident not in out:
                out[ident] = t.data
        return out

    def snapshot(self, pool_identities=()):
        """Rows for the dashboard. Auto-discovered addresses that have never
        answered are hidden (the pool may see a router or Docker gateway
        address, not the miner itself); manual ones always show."""
        ids = set(pool_identities)
        rows = []
        for t in sorted(self.targets.values(), key=lambda x: x.key):
            if "manual" not in t.sources and not t.ever_ok:
                continue
            ident = t.identity if t.identity in ids else None
            if ident is None and t.data and t.data.get("stratum_user") in ids:
                ident = t.data["stratum_user"]
            rows.append({
                "address": t.key,
                "source": "manual" if "manual" in t.sources else "auto",
                "status": t.status(),
                "linked_identity": ident,
                "last_ok_ago": (self._wall() - t.last_ok) if t.last_ok else None,
                "last_error": t.last_error,
                "miner": t.data,
            })
        return {"enabled": self.enabled, "interval_s": POLL_INTERVAL, "miners": rows}

    def close(self):
        self._pool.shutdown(wait=False, cancel_futures=True)
