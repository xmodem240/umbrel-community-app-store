#!/usr/bin/env python3
"""
AxePool - read-only firmware update check for the miners' own firmware.

Compares the version a miner reports on its API (miner_poller.py) with the
newest STABLE release of that firmware on GitHub - the same source each
firmware's own update page uses:
  * AxeOS / ESP-Miner:  api.github.com/repos/bitaxeorg/esp-miner/releases
    (main/http_server/axe-os/src/app/services/github-update.service.ts:
     stable = not prerelease)
  * NerdQaxe:           api.github.com/repos/shufps/ESP-Miner-NerdQAxePlus/releases
    (same file in that repo: stable = not prerelease and no "-rc" in the tag)
Versions are compared the way both firmwares do it: strip a leading "v",
split on ".", compare the leading number of each part.

It never contacts a miner and never installs anything: the dashboard only
shows the result and links to the release notes. Each firmware's releases are
fetched at most every CHECK_INTERVAL, only while a miner with that firmware is
being polled, and only while enabled in Settings. Results are cached in
firmware.json so a restart does not refetch.

Standard library only. Fetching is blocking; call refresh() from an executor.
"""

import json
import logging
import os
import re
import threading
import time
import urllib.request

log = logging.getLogger("solobch.firmware")

SOURCES = {
    "axeos": {"repo": "bitaxeorg/esp-miner", "name": "AxeOS"},
    "nerdqaxe": {"repo": "shufps/ESP-Miner-NerdQAxePlus", "name": "NerdQaxe"},
}
API = "https://api.github.com/repos/{repo}/releases?per_page=50"
CHECK_INTERVAL = 6 * 3600.0    # seconds between successful checks
RETRY_INTERVAL = 30 * 60.0     # after a failed check
HTTP_TIMEOUT = 10.0
MAX_RESPONSE = 4 * 1024 * 1024  # 50 releases with notes are ~0.5-1 MB
_PART = re.compile(r"^(\d+)(.*)$")


def parse_version(text):
    """'v2.15.4' -> ((2, 15, 4), False); 'v2.15.4-dirty' -> ((2, 15, 4), True).
    The flag marks a non-release suffix (custom or test build). None when the
    text has no leading number at all."""
    if not isinstance(text, str):
        return None
    s = text.strip()
    if s[:1] in ("v", "V"):
        s = s[1:]
    parts, custom = [], False
    for i, p in enumerate(s.split(".")):
        m = _PART.match(p)
        if not m:
            if i == 0:
                return None
            custom = True
            break
        parts.append(int(m.group(1)))
        if m.group(2):
            custom = True
            break
    if not parts:
        return None
    return tuple(parts), custom


def compare(a, b):
    """Compare two version tuples, padding with zeros: -1, 0 or 1."""
    n = max(len(a), len(b))
    a, b = a + (0,) * (n - len(a)), b + (0,) * (n - len(b))
    return (a > b) - (a < b)


def is_stable(release, firmware):
    if not isinstance(release, dict) or release.get("draft") or release.get("prerelease"):
        return False
    tag = release.get("tag_name")
    if not isinstance(tag, str) or parse_version(tag) is None:
        return False
    if firmware == "nerdqaxe" and "-rc" in tag:
        return False
    return True


def latest_stable(releases, firmware):
    """Highest-versioned stable release -> {tag, version, published_at, url}.
    Highest version, not newest date: a late patch to an older line must not
    make an up-to-date miner look outdated."""
    best = None
    for r in releases if isinstance(releases, list) else []:
        if not is_stable(r, firmware):
            continue
        ver = parse_version(r["tag_name"])[0]
        if best is None or compare(ver, best["version"]) > 0:
            url = r.get("html_url")
            if not (isinstance(url, str) and url.startswith("https://github.com/")):
                url = (f"https://github.com/{SOURCES[firmware]['repo']}"
                       f"/releases/tag/{r['tag_name']}")
            pub = r.get("published_at")
            best = {"tag": r["tag_name"][:40], "version": ver,
                    "published_at": pub if isinstance(pub, str) else None, "url": url}
    return best


def fetch_releases(firmware, timeout=HTTP_TIMEOUT):
    url = API.format(repo=SOURCES[firmware]["repo"])
    req = urllib.request.Request(url, headers={"Accept": "application/vnd.github+json",
                                               "User-Agent": "AxePool"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        if r.status != 200:
            raise OSError(f"HTTP {r.status}")
        body = r.read(MAX_RESPONSE + 1)
    if len(body) > MAX_RESPONSE:
        raise OSError("response too large")
    return json.loads(body)


def cache_path():
    data = os.environ.get("APP_DATA_DIR")
    return os.path.join(data, "firmware.json") if data else ""


class FirmwareChecker:
    def __init__(self, fetch=fetch_releases, now=time.time, path=None, enabled=True):
        self._fetch = fetch
        self._now = now
        self.enabled = enabled
        self.path = cache_path() if path is None else path
        self._lock = threading.Lock()
        # firmware -> {latest: {...}|None, checked_at, error, next_at}
        self.state = {}
        self._load()

    # ---- cache -------------------------------------------------------------- #
    def _load(self):
        if not self.path:
            return
        try:
            with open(self.path) as f:
                raw = json.load(f)
        except (OSError, ValueError):
            return
        for fw, st in (raw.items() if isinstance(raw, dict) else []):
            if fw not in SOURCES or not isinstance(st, dict):
                continue
            latest = st.get("latest")
            if isinstance(latest, dict) and parse_version(latest.get("tag")):
                latest = dict(latest, version=parse_version(latest["tag"])[0])
            else:
                latest = None
            self.state[fw] = {"latest": latest,
                              "checked_at": st.get("checked_at"),
                              "error": st.get("error"),
                              "next_at": float(st.get("next_at") or 0)}

    def _save(self):
        if not self.path:
            return
        out = {}
        for fw, st in self.state.items():
            latest = st["latest"]
            out[fw] = {"latest": ({k: v for k, v in latest.items() if k != "version"}
                                  if latest else None),
                       "checked_at": st["checked_at"], "error": st["error"],
                       "next_at": st["next_at"]}
        tmp = self.path + ".tmp"
        try:
            with open(tmp, "w") as f:
                json.dump(out, f)
            os.replace(tmp, self.path)
        except OSError as e:
            log.debug("firmware cache not saved: %s", e)

    # ---- checking ------------------------------------------------------------ #
    def due(self, firmwares):
        """Firmwares in use whose check is due."""
        if not self.enabled:
            return []
        now = self._now()
        return [fw for fw in sorted(set(firmwares)) if fw in SOURCES
                and now >= self.state.get(fw, {}).get("next_at", 0)]

    def refresh(self, firmwares):
        """Blocking: check every due firmware. Returns the ones checked."""
        done = []
        for fw in self.due(firmwares):
            now = self._now()
            st = self.state.setdefault(fw, {"latest": None, "checked_at": None,
                                            "error": None, "next_at": 0})
            try:
                latest = latest_stable(self._fetch(fw), fw)
                if latest is None:
                    raise ValueError("no stable release found")
                with self._lock:
                    st.update(latest=latest, checked_at=now, error=None,
                              next_at=now + CHECK_INTERVAL)
                log.info("firmware check: latest stable %s is %s",
                         SOURCES[fw]["name"], latest["tag"])
            except Exception as e:                    # network, JSON, rate limit...
                # Keep the last good answer; try again sooner than usual.
                with self._lock:
                    st.update(error=(e.__class__.__name__ + (f": {e}" if str(e) else ""))[:160],
                              next_at=now + RETRY_INTERVAL)
                log.info("firmware check for %s failed: %s", SOURCES[fw]["name"], st["error"])
            done.append(fw)
        if done:
            self._save()
        return done

    # ---- reading ------------------------------------------------------------- #
    def status(self, firmware, version):
        """What the dashboard shows for one miner."""
        if not self.enabled:
            return {"state": "off"}
        if firmware not in SOURCES:
            return {"state": "unknown", "reason": "unrecognised firmware"}
        cur = parse_version(version)
        if cur is None:
            return {"state": "unknown", "reason": "unrecognised version"}
        st = self.state.get(firmware)
        latest = st and st["latest"]
        base = {"current": str(version)[:40], "source": SOURCES[firmware]["name"],
                "checked_at": st["checked_at"] if st else None,
                "error": st["error"] if st else None}
        if not latest:
            return dict(base, state="unchecked")
        base.update(latest=latest["tag"], published_at=latest["published_at"],
                    url=latest["url"])
        c = compare(cur[0], latest["version"])
        if c < 0:
            return dict(base, state="update")
        if c > 0:
            return dict(base, state="ahead")           # beta / pre-release build
        return dict(base, state="custom" if cur[1] else "current")


if __name__ == "__main__":
    # Live check, for CI and debugging: python3 firmware_check.py
    import sys
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    ok = True
    for fw in SOURCES:
        try:
            latest = latest_stable(fetch_releases(fw), fw)
            print(f"{SOURCES[fw]['name']:9s} latest stable: "
                  f"{latest['tag'] if latest else None}  ({latest and latest['published_at']})  "
                  f"{latest and latest['url']}")
            ok = ok and latest is not None
        except Exception as e:
            print(f"{SOURCES[fw]['name']:9s} check failed: {e}")
            ok = False
    sys.exit(0 if ok else 1)
