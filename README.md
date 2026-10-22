# Kyle's Community App Store

Add this store in Umbrel: **App Store -> ... menu -> Community App Stores** -> `https://github.com/xmodem240/umbrel-community-app-store`

| App | What it does | Source |
|---|---|---|
| **Bitaxe Tuner** (`kyle-bitaxe-tuner`) | Finds safe overclock profiles for a Bitaxe | `src/` |
| **Bitcoin Cash Lab** (`kyle-bch-lab`) | Ranks which peers deliver new Bitcoin Cash blocks first; optional pool latency race. Node software selectable on the dashboard: LoneStrike Labs BCHN app, WillItMod AxeBCH (enter its RPC password), or any BCHN-compatible node | `bch-lab-src/` |

Each app's image is built by its own workflow in `.github/workflows/` and published to
`ghcr.io/xmodem240/`. A new package must be set to **public** in GitHub -> Packages once, or Umbrel
cannot pull it.

Bitcoin Cash Lab is a port of the MIT-licensed [Bitcoin Lab](https://github.com/benunskilled/bitcoin-lab)
by benunskilled. Its own docs are in `bch-lab-src/` (start with `docs/bch-differences.md`, which lists what is
still unverified).

---

# Bitaxe Tuner: Umbrel community app

Automatically sweeps frequency and core voltage on a Bitaxe (AxeOS) and reports which settings are
actually stable, with three recommended profiles: **max hashrate**, **best efficiency (J/TH)** and
**safe daily** (frequency headroom + thermal margin).

## How a run works
1. Reads the miner's current frequency/core voltage (saved so it can be restored).
2. For each point: set frequency/voltage -> restart -> verify the miner accepted the values -> warm up -> measure.
3. A point is **stable** only if hashrate >= 93% of expected, ASIC error rate <= 2%, rejected shares <= 5%
   and no hard limit was hit. Unstable points are retested (default 1x) before being believed.
4. When a voltage's ceiling is found, core voltage goes up one step and the sweep continues from the best
   stable frequency. It ends when more voltage stops helping, or at your max frequency/voltage.
5. Original settings are restored on finish, on **Stop**, on any error, and on app restart/crash.

## While it runs
- A sticky status bar shows a live heartbeat, the current point and phase (apply, warm-up, measure),
  a progress bar with countdown, and run time.
- **EMERGENCY STOP** (big red button) aborts immediately and writes the original settings back
  right away, without waiting for the current step to finish.
- Live graphs of hashrate, ASIC/VR temperature and power, split per test point (✓ stable / ✗ not),
  with your limits drawn in. Hover for exact values. A raw-samples table is under the graphs.

**Hard limits** (ASIC temp, VR temp, power, input voltage min/max) end the run for that point immediately.
All limits are editable but clamped to sane maximums in the backend.

## Install on Umbrel
1. Push this repo to GitHub. Replace `YOUR-GITHUB-USER` in `kyle-bitaxe-tuner/umbrel-app.yml` and
   `kyle-bitaxe-tuner/docker-compose.yml`.
2. Tag a release (`git tag v0.1.0 && git push --tags`). The workflow in `.github/workflows/build.yml`
   publishes a multi-arch image to `ghcr.io/<you>/bitaxe-tuner`. In GitHub -> Packages, set that package to **public**
   so Umbrel can pull it.
3. In Umbrel: App Store -> ... menu -> Community App Stores -> add your repo URL -> install **Bitaxe Tuner**.
4. Open the app, enter the Bitaxe's IP, press **Detect**, review the limits, **Start tuning**.

## Tested
`tests/mock_bitaxe.py` simulates the AxeOS API (`/api/system/info`, `PATCH /api/system`, `/api/system/restart`).
Run it, then run `src/server.py` with `TUNER_TEST_MODE=1 DATA_DIR=./data` to try the app without hardware.
Verified against the simulator: full sweep, voltage escalation, thermal abort, manual stop, emergency stop, and recovery after `kill -9`.
**Not yet verified:** the Docker image build and install on a real Umbrel, and behavior on real Bitaxe firmware.

## Notes / assumptions
- Uses `GET /api/system/info`, `PATCH /api/system` (`frequency`, `coreVoltage`) and `POST /api/system/restart`
  from stock AxeOS. Field names vary slightly between firmware versions; missing fields are skipped, not guessed.
  `errorPercentage` needs a firmware that reports it.
- Short tests don't prove long-term stability. Run your chosen profile for hours and watch temps.
