# AxePool

A self-hosted **Bitcoin Cash (BCH) solo-mining Stratum server** for Umbrel, built for Bitaxe,
NerdQaxe and other Stratum V1 home miners. It mines to your own node, the official Umbrel
**Bitcoin Cash Node** app, and pays a found block's whole reward to your address.

```
ASIC miner (Stratum V1)  →  AxePool  →  Bitcoin Cash Node app (JSON-RPC + ZMQ)  →  BCH network
```

AxePool is a fork of the MIT-licensed [SoloBCH Forge](https://github.com/DanCreatesStuff/SoloBCHForge)
by DanCreatesStuff, forked at upstream commit `3b4d7cf` (release 1.1.3, 2026-09-25). Upstream's
license is kept in [LICENSE](LICENSE). Internal names (the `SOLOBCH_*` settings variables, log
names and `solobch_*` Prometheus metrics) are deliberately unchanged so upstream fixes stay easy
to merge.

## Install

1. In Umbrel: **App Store → … → Community App Stores**, add
   `https://github.com/xmodem240/umbrel-community-app-store`.
2. Install the official **Bitcoin Cash Node** app (a dependency) and let it sync fully. Until it
   has, AxePool runs in a safe mock mode and never submits anything.
3. Install **AxePool**.

## Point your miners at it

- **URL:** `stratum+tcp://<your-umbrel-ip>:3344`
- **Username:** your BCH **CashAddr** address, optionally with a worker name:
  `bitcoincash:qxxxx….rig1`. That address is paid in the coinbase.
- **Password:** anything (ignored).

Only CashAddr is accepted as the username. Legacy `1…` addresses and Bitcoin `bc1…` addresses are
rejected on purpose. Give each miner its own worker name so they show as separate rows.

## Ports

| Host port | Purpose | Exposure |
|---|---|---|
| `3344` | Stratum (container port 3334) | Your LAN |
| `8793` | Dashboard + settings (container port 3335) | Umbrel app UI, behind Umbrel's login |

The node's RPC and ZMQ stay on Umbrel's internal network.

## Solo mining is a lottery

A small miner may go a very long time without finding a block. Share difficulty does not change
your odds; only hashrate does. Run this for sovereignty, learning and the ticket, not as income.

## Development

- Tests: `python3 -I server/test_pipeline.py`, `python3 server/test_metrics_store.py` and
  `python3 server/test_miner_poller.py` (standard library only).
- Miner monitoring: `server/miner_poller.py` reads each miner's own `GET /api/system/info`
  (AxeOS and NerdQaxe firmware) every 10 seconds, read-only. Miners are found from their Stratum
  connection; extra private LAN addresses can be listed in Settings. Results are at
  `GET /devices` and in the `/series` points (`reported_ghs`, `power_w`, `chip_temp_c`,
  `vr_temp_c`). Wi-Fi name, MAC address and pool passwords from the miner are never stored.
- Metrics: `server/metrics_store.py` keeps `metrics.sqlite` in the data folder: 10-second
  samples for 24 hours, 1-minute rollups for 30 days, 1-hour rollups for a year. It writes once
  a minute in a single transaction. Read it at `GET /series?range=1h|6h|24h|7d|30d|1y` with
  optional `miner=<username>` and `since=<unix time>` (for cheap incremental refreshes), and
  `GET /series/miners`.
- Real miner API responses used by tests live in `server/fixtures/`, with personal identifiers
  redacted.
- Upstream's architecture notes: [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).
- The image is built by `.github/workflows/build-axepool.yml` at the repo root.

## Disclaimer

Provided as is, without warranty. You are responsible for your node, miners and funds, and for
the laws and taxes that apply to mining where you live.

## License

[MIT](LICENSE): © 2026 DanCreatesStuff (SoloBCH Forge). AxePool changes are released under the
same license.
