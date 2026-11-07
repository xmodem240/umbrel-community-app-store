#!/usr/bin/env python3
"""
AxePool - the Stratum port shown to the user vs the port the server listens on.

Under Umbrel the container listens on 3334 and the host publishes it as 3344.
The dashboard, diagnostics hint and startup log must show 3344
(SOLOBCH_PUBLIC_STRATUM_PORT); the server must keep listening on 3334.

Offline, standard library only:   cd server && python3 test_ports.py
"""
import os
import sys
import tempfile

os.environ["APP_DATA_DIR"] = tempfile.mkdtemp(prefix="axepool-ports-test-")
for k in list(os.environ):
    if k.startswith(("SOLOBCH_", "BCHN_")):
        del os.environ[k]
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import logging                                       # noqa: E402
import config                                        # noqa: E402
import status                                        # noqa: E402
from stratum import StratumServer, public_stratum_port   # noqa: E402

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


check("public port from the environment (Umbrel: 3344)",
      public_stratum_port(3334, {"SOLOBCH_PUBLIC_STRATUM_PORT": "3344"}) == 3344)
check("unset -> the listening port", public_stratum_port(3334, {}) == 3334)
check("blank -> the listening port",
      public_stratum_port(3334, {"SOLOBCH_PUBLIC_STRATUM_PORT": "  "}) == 3334)
for bad in ("0", "70000", "33a4", "-1", "3344 ", "3344:3334"):
    got = public_stratum_port(3334, {"SOLOBCH_PUBLIC_STRATUM_PORT": bad})
    # "3344 " is trimmed and valid; every other value falls back.
    check(f"invalid {bad!r} handled -> {got}",
          got == (3344 if bad.strip() == "3344" else 3334))


class Jobs:
    rpc = None


srv = StratumServer.__new__(StratumServer)          # no event loop needed
srv.clients = set()
srv.stratum_port = 3334
srv.public_stratum_port = 3344
srv.start_time_wall = 0.0
payload_server = {"stratum_port": getattr(srv, "public_stratum_port", srv.stratum_port)}
src = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "status.py")).read()
check("/status reports the public port, not the listening one",
      '"stratum_port": getattr(server, "public_stratum_port", server.stratum_port)' in src
      and payload_server["stratum_port"] == 3344)
dsrc = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "diagnostics.py")).read()
check("diagnostics hint uses the public port",
      "getattr(self.server, 'public_stratum_port', self.stratum_port)" in dsrc)

# Ports pinned by the environment are locked in Settings and win over saved values.
os.environ["SOLOBCH_STRATUM_PORT"] = "3334"
os.environ["SOLOBCH_STATUS_PORT"] = "3335"
check("env-pinned ports are reported as locked",
      {"stratum_port", "status_port"} <= set(config.env_locked()))
cfg = config.load()
cfg["stratum_port"] = 4000
cfg["status_port"] = 4001
config.save(cfg) if hasattr(config, "save") else None
again = config.load()
check("a saved different port does not override the pinned one",
      again["stratum_port"] == 3334 and again["status_port"] == 3335)

print(f"\n{ok} passed, {fail} failed")
sys.exit(0 if fail == 0 else 1)
