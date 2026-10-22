# Bitcoin Cash differences, and what is still unverified

This app was written for Bitcoin Core. The rest of the documentation in `docs/`
still describes Core's behaviour, with "Core" replaced by "BCHN" mechanically.
Treat those passages as the design intent, not as verified BCHN facts.

## What was changed

| Area | Bitcoin Lab | Bitcoin Cash Lab |
|---|---|---|
| Node | Bitcoin Core, ZMQ 28334 | BCHN-compatible, ZMQ hashblock 28332, chosen on the dashboard (Node card) |
| Probed listening ports for inbound peers | 8333, 9333 | 8333 |
| Network port table | main/test/testnet4/signet/regtest | mainnet 8333, testnet3 18333, testnet4 28333, scalenet 38333, chipnet 48333, regtest 18444 |
| `getblocktemplate` | `{rules: ['segwit']}` | `{}` (BCH has no segwit) |
| Default Stratum Race pools | 8 Bitcoin solo pools | 2Miners Solo BCH (EU, US), Kryptex BCH (EU, US) |
| Local pool detection | GoBrrr, Bassin, Public Pool, DATUM, Pogolo | LoneStrike Cash (`sslabs-solostrike-cash`), AxeBCH (`willitmod-dev-bch_ckpool_1:3333`) |
| Pool attribution list | bitcoin-data/mining-pools | empty (see MINING-POOLS.md) |
| Database file | `bitcoinlab.db` | `bchlab.db` |

## Unverified assumptions

Run `node scripts/check-node.js` against your node to settle the first three.

1. **`getpeerinfo` fields.** The app needs `last_block` (the whole ranking rests on it),
   `connection_type`, `bip152_hb_to`, `servicesnames`, `network`. Bitcoin ABC's
   documentation lists all of them; BCHN's was not checked field by field.
2. **`addnode` cap of 8.** `MAX_MANUAL_PEERS=8` mirrors Bitcoin Core's
   `MAX_ADDNODE_CONNECTIONS`. Whether BCHN enforces the same cap is unchecked. The value
   is configurable.
3. **`last_block` semantics.** The ranking assumes the field is only set for a block that
   was new to the node, so that exactly one peer is credited. If BCHN sets it differently,
   First % will be wrong.
4. **`getblocktemplate` with `{}`.** Should work on BCHN; the template-timing number is
   non-critical and logs a warning on failure.
5. **Stratum.** Pools are timed on `mining.notify`. The authorize username is a legacy
   Base58 address (`1BitcoinEater...`), which is valid on BCH too; some pools may want a
   CashAddr. Override with `STRATUM_AUTHORIZE_ADDRESS`.
6. **No Peer Map.** The Bitcoin sibling app has no BCH version; the link to it is inert.

## Node selection

Neither Bitcoin Cash node app on Umbrel exports `APP_*` variables for the apps that
use them, so the connection cannot be wired in the compose file. The Node card on the
dashboard saves the choice in `node-profile.json` (see `docs/configuration.md`). With
nothing saved, the compose file's environment is used, which points at the LoneStrike
Labs app (`sslabs-bitcoin-cash-node`, RPC user/password and container name read from its
own docker-compose.yml, Oct 2026). If that app changes them, pick "Other" and enter them.

The app no longer declares a hard Umbrel dependency on any one node app, since either
can serve. It will install without a node and show "not connected" until one is chosen.

## AxeBCH (WillItMod)

AxeBCH is WillItMod's Bitcoin Cash node + solo ckpool for Umbrel (app id
`willitmod-dev-bch`, same in their main and dev stores). Its connection details were read
from the compose file and config templates in WillItMod/umbrel-dev-community-store and
WillItMod/AxeSuite (app v0.9.27, Oct 2026):

| | Value |
|---|---|
| Node container | `willitmod-dev-bch_bchn_1` (their `bchn` service, image `ghcr.io/willitmod/bitcoin-cash-node:v29.0.0-wm3`) |
| RPC | port **28332**, user **`bch`**, password = the install's `APP_PASSWORD` |
| ZMQ `hashblock` | port **28334**, published on all interfaces |
| Pool | container `willitmod-dev-bch_ckpool_1`, stratum 3333 inside (4567 on the host) |
| RPC access | `rpcbind=0.0.0.0` with `rpcallowip` for the Umbrel apps subnet, so other apps may connect |

The **password is generated per install**, so the app cannot know it. Read it on the Umbrel:

```
grep rpcpassword ~/umbrel/app-data/willitmod-dev-bch/data/node/bitcoin.conf
```

Choose "AxeBCH (WillItMod)" in the Node card, paste the password, press Test, then Save.

Not checked: that their patched BCHN (`-wm3`) returns the `getpeerinfo` fields listed above,
and that this app's RPC and ZMQ connections coexist with theirs on a running node. Test
and `scripts/check-node.js` answer the first. Note that AxeBCH2 (`willitmod-dev-axebch2`)
is a different chain (Bitcoin Cash II); do not point this app at it.
