# Bitaxe Autotuner: Umbrel community app

A copy of Bitaxe Tuner 0.4.0 (`src/`) with a **live guard** added. It runs the same sweep, then keeps
the miner tuned live within the settings the sweep proved stable, until you press **Stop**. Bitaxe
Tuner itself is unchanged. One miner per run.

## How a run works
1. **Sweep:** identical to Bitaxe Tuner (see the main README): step frequency and core voltage,
   judge each point, recommend max hashrate / best efficiency / safe daily.
2. **Live guard:** if the sweep found a safe daily profile, the guard starts there. Otherwise the run
   ends and the original settings are restored, as in Bitaxe Tuner.
3. **Stop / Emergency stop / any error:** the original settings are restored.

## The live guard
- **Settings it can use:** one per tested-stable frequency (the stable point with the most hashrate
  at that frequency; the safe daily point keeps its own voltage). It never uses an untested setting
  or anything above the highest stable frequency, Max frequency or Max core voltage.
- **Windows:** the miner is judged in windows (Guard window, default 15 min). After each change it
  waits to settle (60 s when applied live, the warm-up time after a restart) before judging.
- **Steps down one setting at once** when the ASIC error rate over the last 2 minutes is above the
  limit, when ASIC or VR temperature is within the step-down margin (default 4 °C) of its limit for
  two readings, or when a full window is unstable (hashrate, errors or rejected shares, judged like a
  sweep point). Lowering Max frequency / Max core voltage mid-run also moves it down at once.
- **Steps up one setting** only after several good windows in a row (default 4, about an hour) that
  stayed at least the step-up headroom (default 8 °C) under the temperature limits.
- **Back-off:** each instability at a setting doubles the good windows needed to return to it (up to
  8×); heat-only step-downs don't count. One failure is forgiven each time the setting proves itself again.
- **Hard limit** (ASIC/VR temperature, power, input voltage): straight to safe daily, or one setting
  down if already at or below it. A hard limit on the lowest setting ends the run.
- **Firmware throttling:** current ESP-Miner lowers its own saved frequency when it throttles for
  heat. The guard follows it down to a tested setting at or below that frequency (never above safe
  daily) and ends the run if there is none. Firmware overheat mode ends the run.
- **App restart while guarding:** the guard resumes from safe daily. (A restart during the sweep
  restores the original settings, as in Bitaxe Tuner.)

### Live vs restart mode
Current ESP-Miner applies frequency and core-voltage changes without a restart: its power-management
loop picks up the saved values, and `/api/system/info` reports the running values as
`actualFrequency` / `coreVoltageActual` (checked in the ESP-Miner source, Oct 2026). After each
change the guard waits up to 30 s for `actualFrequency` to match. If the firmware doesn't report it
or it doesn't match, the guard switches to **restart mode** for the rest of the run: each change
restarts the miner, and stepping up needs twice as many good windows.

## Tested
`autotune-tests/test_guard.py` runs the app against `autotune-tests/mock_bitaxe.py` (a copy of the
Bitaxe Tuner simulator with live-apply, restart-only and no-live-field firmware modes, and controls
for ambient heat, a degrading chip, firmware throttling and overheat mode):

    pip install -r autotune-src/requirements.txt
    python -m unittest discover -s autotune-tests -p "test_*.py" -v

Covered: climbing to the ceiling live with no restarts; heat step-down and recovery without penalty;
fast error step-down and doubled wait to return; hard limit straight to safe daily; following a
firmware throttle; lowering Max frequency mid-run; hard limit at the lowest setting; firmware
overheat; emergency stop; restart-mode fallback; firmware without `actualFrequency`; a sweep with no
safe daily; resuming after `kill -9`. Every path checks the original settings are restored.
Each safety rule was also deliberately broken once to confirm a test fails.

**Not yet verified:** the Docker image build, install on a real Umbrel, and behavior on real
hardware, including whether the NerdQAxe and Nexus firmware (and older ESP-Miner releases) apply
changes live. Those fall back to restart mode if they don't.
