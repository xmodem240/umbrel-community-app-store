# Changelog

## 0.2.0

- **Fix: works on BCHN.** BCHN's `getpeerinfo` has no `last_block`, so no peer was ever credited
  First (FIRST % stayed 0, red "no peer credited" banner). First is now credited from per-peer
  block-message byte counters when `last_block` is absent.
- `connection_type`, `network` and `servicesnames` are derived when BCHN omits them, so peers are no
  longer typed "unknown" and the rotation can tell manual/inbound/outbound apart.
- Fix: every BCHN peer was labelled "a client of another chain" because of its `(EB32.0)` user agent.
- `scripts/check-node.js` reports which attribution method the node supports.
- After upgrading, use Storage -> Reset peer data: earlier races have no First credits.

## 0.1.1

- AxeBCH preset corrected from WillItMod's own compose files: node `willitmod-dev-bch_bchn_1`, RPC 28332, user `bch`,
  ZMQ 28334 (was a guess at 8332/28332). Only the per-install RPC password has to be entered.
- AxeBCH local pool is `willitmod-dev-bch_ckpool_1:3333` (was a wrong guess).

## 0.1.0

First Bitcoin Cash port, derived from the MIT-licensed Bitcoin Lab by benunskilled
(https://github.com/benunskilled/bitcoin-lab). Not yet run against a live BCHN node.

- Node defaults for BCHN (RPC 8332, ZMQ hashblock 28332); network names for mainnet,
  testnet3, testnet4, scalenet, chipnet and regtest.
- getblocktemplate timing no longer asks for the `segwit` rule, which BCH does not have.
- Stratum Race defaults replaced with BCH pools whose endpoints are published on the
  pools' own setup pages (2Miners Solo BCH, Kryptex BCH).
- Local pool discovery looks for LoneStrike Cash instead of the Bitcoin solo-pool apps.
- Pool attribution list is empty by default (the upstream list is Bitcoin-only); supply
  your own with MINING_POOLS_FILE.
- Added scripts/check-node.js to verify a BCHN node returns the RPC fields the app reads.
- Node selection on the dashboard (Node card): LoneStrike Labs BCHN app, AxeBCH (WillItMod,
  unverified) or any BCHN-compatible node. Test, auto-detect, and apply-by-restart. Saved in
  `node-profile.json` (0600); the password is never sent to the browser.
- AxeBCH added to local Stratum Race pool detection (unverified container name, stratum 4567).
- Dashboard accent colour changed to Bitcoin Cash green.
