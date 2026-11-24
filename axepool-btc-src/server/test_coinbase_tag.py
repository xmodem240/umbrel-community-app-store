#!/usr/bin/env python3
"""
AxePool - the coinbase tag is a setting, not a constant.

Proves, through the real Stratum submit path (test_pipeline harness, real hashing
at an easy target): the default tag lands in a found block; a tag changed in
Settings is used for new work; a block found on work sent BEFORE the change is
still rebuilt byte-exact with the tag that work went out with (and submitted);
a blank tag works. Also the validation in Settings, config.json and the
scriptSig size guard.

Offline:   cd server && python3 test_coinbase_tag.py
"""
import asyncio
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_pipeline as tp                       # noqa: E402  (sets APP_DATA_DIR, harness)
import config                                    # noqa: E402
import status                                    # noqa: E402
from coinbase import build_coinbase_parts, MAX_TAG_LEN   # noqa: E402
from job_manager import JobManager               # noqa: E402
from stats import Stats                          # noqa: E402
import stratum                                   # noqa: E402

ok = fail = 0


def check(label, cond):
    global ok, fail
    if cond:
        ok += 1
        print(f"PASS  {label}")
    else:
        fail += 1
        print(f"FAIL  {label}")


def script_sig(coinbase):
    """scriptSig of a raw (non-witness) coinbase: after version, input count,
    null prevout (32+4) and the 1-byte length."""
    n = coinbase[4 + 1 + 36]
    return coinbase[4 + 1 + 36 + 1:4 + 1 + 36 + 1 + n]


async def main():
    jm = JobManager(tp.FakeRPC(), 1, stats=Stats())
    jm.vardiff = {"enabled": False, "target_spm": 20, "min": 1, "max": 4000000}
    prev = "00000000000000000123456789abcdef00112233445566778899aabbccddeeff"
    jm._new_real(tp.make_gbt(950_000, prev, 312_512_345), clean=True)
    jm.mode = "real"
    server = stratum.StratumServer(jm)
    w = tp.FakeWriter("10.0.0.5")
    c = stratum.ClientConn(server, None, w)
    server.clients.add(c)
    await c.on_subscribe(1, ["bitaxe/test"])
    await c.on_authorize(2, [f"{tp.ADDR_A}.rig", "x"])
    m = tp.Miner(c, w)
    m.absorb()
    c.target = 0                                  # every find below is a block
    rpc = jm.rpc

    async def set_tag(tag):
        # apply_config is the Settings save path. It also rebuilds the node RPC
        # from config (a real host here), so put the fake node back.
        cfg = config.load()
        cfg["coinbase_tag"] = tag
        jm.apply_config(cfg)
        jm.rpc = rpc
        await asyncio.sleep(0)                    # let the difficulty push run
        m.absorb()
        c.target = 0

    async def find_block(notify=None):
        params, hdr, cbraw, _ = m.find(lambda h: h <= tp.EASY_TARGET, notify=notify)
        n = len(rpc.blocks)
        r = await m.submit(params)
        if server.submit_tasks:
            await asyncio.gather(*list(server.submit_tasks))
        sub = rpc.blocks[-1] if len(rpc.blocks) == n + 1 else None
        coinbase = tp.parse_block(sub)[2] if sub else None
        return r, cbraw, coinbase

    # ---- default -----------------------------------------------------------
    check("default tag is /AxePool/", jm.coinbase_tag == b"/AxePool/"
          and config.DEFAULTS["coinbase_tag"] == "/AxePool/")
    old_job = list(m.notify)
    r, cbraw, coinbase = await find_block()
    check("block with the default tag accepted and submitted",
          r.get("result") is True and coinbase is not None)
    check("submitted coinbase ends its scriptSig with /AxePool/",
          coinbase is not None and script_sig(coinbase).endswith(b"/AxePool/"))

    # ---- change the tag in Settings ---------------------------------------
    await set_tag("/Kyle mined this/")
    check("apply_config sets the live tag", jm.coinbase_tag == b"/Kyle mined this/")

    # A block found on the job sent BEFORE the change must still rebuild with
    # the old tag (the miner hashed a coinbase carrying /AxePool/).
    r, cbraw, coinbase = await find_block(notify=old_job)
    check("block on pre-change work accepted (not stale/rejected)",
          r.get("result") is True and coinbase is not None)
    check("...and submitted byte-exact to the miner's coinbase (old tag)",
          coinbase == cbraw and script_sig(coinbase).endswith(b"/AxePool/"))

    # New work carries the new tag.
    jm._new_real(tp.make_gbt(950_000, prev, 312_520_000, ntxs=4), clean=False)
    await jm._broadcast()
    m.absorb()
    check("new job's coinb2 starts with the new tag",
          bytes.fromhex(m.notify[3]).startswith(b"/Kyle mined this/"))
    r, cbraw, coinbase = await find_block()
    check("block on new work carries the new tag, byte-exact",
          coinbase == cbraw and script_sig(coinbase).endswith(b"/Kyle mined this/"))

    # Same job re-sent after a second change (e.g. reconnect + new tag): both
    # versions of the job stay rebuildable.
    job_new = list(m.notify)
    await set_tag("/second/")
    await c.send_current_job()
    m.absorb()
    r, cbraw, coinbase = await find_block(notify=job_new)
    check("job re-sent with another tag: the earlier copy still rebuilds",
          r.get("result") is True and coinbase == cbraw
          and script_sig(coinbase).endswith(b"/Kyle mined this/"))
    r, cbraw, coinbase = await find_block()
    check("...and so does the newer copy",
          r.get("result") is True and coinbase == cbraw
          and script_sig(coinbase).endswith(b"/second/"))

    # ---- blank tag --------------------------------------------------------
    await set_tag("")
    await c.send_current_job()
    m.absorb()
    r, cbraw, coinbase = await find_block()
    sig = script_sig(coinbase) if coinbase else b""
    check("blank tag: scriptSig is just height + extranonce (4+4 bytes)",
          r.get("result") is True and coinbase == cbraw
          and len(sig) == 4 + stratum.EXTRANONCE1_SIZE + stratum.EXTRANONCE2_SIZE)

    # ---- longest allowed tag fits the consensus limit -----------------------
    longest = "x" * MAX_TAG_LEN
    c1, c2 = build_coinbase_parts(8_388_608, 1, "51", 4, 4, tag=longest)  # 5-byte height push
    check("64-char tag + 5-byte height + 8 extranonce fits in 100 bytes", bool(c1 and c2))
    try:
        build_coinbase_parts(950_000, 1, "51", 4, 4, tag="x" * 90)
        check("scriptSig over 100 bytes refused", False)
    except ValueError:
        check("scriptSig over 100 bytes refused", True)

    # ---- Settings validation ---------------------------------------------
    cur = config.load()

    def v(tag):
        return status.validate_updates({"coinbase_tag": tag}, cur)

    check("Settings accepts /AxePool/", v("/AxePool/") is None)
    check("Settings accepts a blank tag", v("") is None)
    check("Settings accepts exactly 64 characters", v("a" * 64) is None)
    check("Settings accepts spaces and punctuation", v("Mined by Kyle! ~ #1 (MN)") is None)
    check("Settings refuses 65 characters", v("a" * 65) is not None)
    check("Settings refuses an emoji", v("/AxePool \U0001f680/") is not None)
    check("Settings refuses an accented letter", v("/café/") is not None)
    check("Settings refuses a newline", v("/a\nb/") is not None)
    check("Settings refuses a tab", v("/a\tb/") is not None)
    check("Settings refuses DEL (0x7f)", v("/a\x7fb/") is not None)
    check("Settings refuses a non-string", v(123) is not None)

    # ---- POST /config (the Settings page's save) --------------------------
    class Srv:
        clients = set()
    st = status.StatusServer(Srv(), jm, "127.0.0.1", 0)

    async def post(body):
        r = await st._route("POST", "/config", {"content-type": "application/json"},
                            json.dumps(body).encode())
        jm.rpc = rpc                              # apply_config rebuilt it
        return r[0], json.loads(r[2])

    code, body = await post({"coinbase_tag": "/via settings/"})
    check("POST /config saves the tag and applies it live",
          code.startswith("200") and config.load()["coinbase_tag"] == "/via settings/"
          and jm.coinbase_tag == b"/via settings/")
    code, body = await post({"coinbase_tag": ""})
    check("POST /config with a blank tag clears it (not 'leave unchanged')",
          code.startswith("200") and config.load()["coinbase_tag"] == ""
          and jm.coinbase_tag == b"")
    code, body = await post({"coinbase_tag": "x" * 65})
    check("POST /config refuses a 65-character tag (400) and keeps the old one",
          code.startswith("400") and config.load()["coinbase_tag"] == "")
    page = status.CONFIG_HTML
    check("Settings page has the coinbase_tag field, saved with the others",
          'id="coinbase_tag"' in page and "'coinbase_tag'];" in page)

    # ---- config.json -----------------------------------------------------
    config.save({"coinbase_tag": "/saved/"})
    check("saved tag loads back", config.load()["coinbase_tag"] == "/saved/")
    config.save({"coinbase_tag": ""})
    check("saved blank tag loads back blank (not the default)",
          config.load()["coinbase_tag"] == "")
    with open(config.config_path()) as f:
        raw = json.load(f)
    raw["coinbase_tag"] = "/badé/" + "y" * 80
    with open(config.config_path(), "w") as f:
        json.dump(raw, f)
    check("hand-edited bad tag in config.json falls back to the default",
          config.load()["coinbase_tag"] == "/AxePool/")
    config.save({"coinbase_tag": "/AxePool/"})


if __name__ == "__main__":
    asyncio.run(main())
    print(f"\n{ok} passed, {fail} failed")
    sys.exit(1 if fail else 0)
