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
| Local pool detection | GoBrrr, Bassin, Public Pool, DATUM, Pogolo | LoneStrike Cash (`sslabs-solostrike-cash`), AxeBCH (unverified) |
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

## AxeBCH (WillItMod): what is and is not known

AxeBCH is WillItMod's Bitcoin Cash node + solo pool for Umbrel (app id `willitmod-dev-bch`
in the dev store). From public pages: it runs Bitcoin Cash Node from the
`mainnet/bitcoin-cash-node` image, miners connect to `stratum+tcp://<umbrel>:4567`, and
its web UI container is `app` (container port 3000, host 21212).

**Not known**, because the compose file could not be read: the node container's name, its
RPC user/password, whether it publishes ZMQ `hashblock` and on which port. So:

- The "AxeBCH" choice has BCHN defaults (RPC 8332, ZMQ 28332) and no login. "Find installed
  nodes" tries a few likely container names; a hit only proves a container of that name
  exists. Press **Test**; it says whether the RPC login or the ZMQ port is the problem.
- If ZMQ is not published by AxeBCH's node, the app cannot time blocks (it deliberately uses
  nothing else). Test reports "nothing is listening on host:28332".
- Find the real values with `docker ps` and by reading the app's `docker-compose.yml` under
  `~/umbrel/app-data/willitmod-dev-bch/` on the Umbrel.
- Local pool detection assumes the stratum port 4567 is served by `willitmod-dev-bch_app_1`.
  If a Stratum Race against it never connects, add the pool by hand with the container
  that really serves stratum.
