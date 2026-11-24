# AxePool BTC

A self-hosted **Bitcoin (BTC) solo-mining Stratum server** for Umbrel, built for Bitaxe, NerdQaxe
and other Stratum V1 home miners. It mines to your own node (the Umbrel **Bitcoin Node** app or
**Bitcoin Knots**) and pays a found block's whole reward to your address.

```
ASIC miner (Stratum V1)  →  AxePool BTC  →  Bitcoin node app (JSON-RPC + ZMQ)  →  Bitcoin network
```

AxePool BTC is a copy of [AxePool](../axepool-src) (Bitcoin Cash), itself a fork of the
MIT-licensed [SoloBCH Forge](https://github.com/DanCreatesStuff/SoloBCHForge) by DanCreatesStuff.
Upstream's license is kept in [LICENSE](LICENSE). Internal names (the `SOLOBCH_*` and `BCHN_RPC_*`
settings variables, the `bch_rpc` module, log names and `solobch_*` Prometheus metrics) are
deliberately unchanged from AxePool so fixes can be copied across with small diffs.

What differs from AxePool:

- **SegWit blocks.** Templates are requested with the `segwit` rule (Bitcoin Core refuses
  without it). Every coinbase carries the template's `default_witness_commitment` as a second,
  zero-value output; the block sent to `submitblock` carries the coinbase in witness form
  (marker, flag and the 32-byte zero witness reserved value). The merkle tree uses `txid`, never
  the `hash` (wtxid) field.
- **Bitcoin addresses** (`server/btcaddr.py`): `bc1q…` (P2WPKH/P2WSH), `bc1p…` (Taproot), `1…`
  and `3…`. Witness versions 2-16 and non-32-byte v1 programs are refused: their outputs can be
  spent by anyone today. Addresses must match the node's network: the chain the node reports,
  or `SOLOBCH_NETWORK` (Umbrel's `APP_BITCOIN_NETWORK`) until it answers. Tested against Bitcoin
  Core's own vectors (`server/fixtures/bitcoin_core_key_io_*.json`, from Bitcoin Core v32.1,
  MIT license).
- Recognizes **Bitcoin Lab**'s Stratum Race (`bitcoin-lab/…`) as a monitor, not a miner.
- Alerts and the dashboard say **AxePool BTC**, so both apps can share one Discord channel.

## Install

1. In Umbrel: **App Store → … → Community App Stores**, add
   `https://github.com/xmodem240/umbrel-community-app-store`.
2. Install the **Bitcoin Node** app or **Bitcoin Knots** and let it sync fully. Until it has,
   AxePool BTC runs in a safe mock mode and never submits anything.
3. Install **AxePool BTC**.

## Point your miners at it

- **URL:** `stratum+tcp://<your-umbrel-ip>:3345`
- **Username:** your Bitcoin address, optionally with a worker name: `bc1qxxxx….rig1`
  (`bc1p…`, `1…` and `3…` work too).
- **Password:** anything (ignored).

Bitcoin Cash (`bitcoincash:…`) and testnet addresses are rejected on mainnet. Give each miner its
own worker name so they show as separate rows.

## Ports

| Host port | Purpose | Exposure |
|---|---|---|
| `3345` | Stratum (container port 3334) | Your LAN |
| `8794` | Dashboard + settings (container port 3335) | Umbrel app UI, behind Umbrel's login |

3345 keeps clear of AxePool (3344), Public Pool (3333) and Bassin (3456), so all can run at once.
The node's RPC and ZMQ stay on Umbrel's internal network.

## Solo mining is a lottery

A small miner may go a very long time without finding a block. Share difficulty does not change
your odds; only hashrate does. Run this for sovereignty, learning and the ticket, not as income.

## Development

- Tests: `python3 -I server/btcaddr.py`, `python3 server/test_network.py`,
  `python3 -I server/test_pipeline.py`, `python3 server/test_metrics_store.py`,
  `python3 server/test_miner_poller.py`, `python3 server/test_vardiff_seed.py`,
  `python3 server/test_configure.py` and `python3 server/test_asic_floor.py` (standard library
  only). CI runs the full list in `.github/workflows/build-axepool-btc.yml`.
- Firmware check: `server/firmware_check.py` compares each polled miner's firmware version with
  the newest stable release on GitHub (AxeOS: bitaxeorg/esp-miner, NerdQaxe:
  shufps/ESP-Miner-NerdQAxePlus), at most every 6 hours, cached in `firmware.json`. Read-only:
  it never contacts or updates a miner. Switch: Settings, Miner monitoring.
- Difficulty seeding: with vardiff on, a newly connected miner whose own API reports its
  hashrate gets the difficulty for the target share rate within seconds
  (difficulty = hashrate x 60 / (target shares/min x 2^32), from the 10-minute average), instead
  of vardiff working it out over several minutes. One shot per connection, clamped to the vardiff
  limits, never for a paused/stopped/overheating miner. The share-rate loop stays in charge
  afterwards, and now only counts shares mined at the current difficulty.
- ASIC difficulty floor: when a miner's API reports `asicDifficulty` (newer AxeOS), vardiff,
  seeding and `mining.suggest_difficulty` never set that miner below it, since AxeOS warns when
  the pool difficulty is lower. The Settings maximum still wins; miners without the field are
  unaffected.
- Coinbase tag: the text written into every block found (default `/AxePool/`) is a setting
  (Settings, Coinbase tag). Up to 64 printable ASCII characters; blank means none. It applies to
  new work within about 30 seconds; each connection remembers the tag every job went out with,
  so a block found on earlier work is rebuilt byte-exact. Test: `server/test_coinbase_tag.py`.
- Version rolling (BIP310): `mining.configure` grants the pool mask `1fffe000` ANDed with the
  mask the miner asks for, and shares are rebuilt with the granted mask. A miner that sends no
  mask gets `1fffe000`; with no common bit, version rolling is refused.
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
- Release check against a real node: `BITCOIND=/path/to/bitcoind python3 tools/regtest_proof.py`
  (throwaway regtest node; see [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)).
- The image is built by `.github/workflows/build-axepool-btc.yml` at the repo root.

## Disclaimer

Provided as is, without warranty. You are responsible for your node, miners and funds, and for
the laws and taxes that apply to mining where you live.

## License

[MIT](LICENSE): © 2026 DanCreatesStuff (SoloBCH Forge). AxePool changes are released under the
same license.
