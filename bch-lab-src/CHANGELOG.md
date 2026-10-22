# Changelog

## 0.1.0 - unreleased

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
