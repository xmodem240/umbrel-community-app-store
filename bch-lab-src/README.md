# Bitcoin Cash Lab

Find your fastest Bitcoin Cash peers, keep them, and see whether your solo-mining
pool got faster because of it.

This is a port of [Bitcoin Lab](https://github.com/benunskilled/bitcoin-lab) (MIT) to
Bitcoin Cash. It talks to a **Bitcoin Cash Node (BCHN)** over RPC and ZMQ and does
nothing else to it.

> **Status: untested against a live BCHN node.** The code and its 340+ unit tests run, but
> several assumptions about BCHN's RPC output come from reading documentation, not from a
> running node. Run `node scripts/check-node.js` against your node first - see
> [docs/bch-differences.md](docs/bch-differences.md).

## What it does

- **Peer relay ranking** - for every new block, records which peer delivered it first
  (which peers delivered each new block: `last_block` where the node reports it, otherwise growth of each peer's block-message bytes, as on BCHN) and ranks
  peers by First / Eligible.
- **Manual peers** - keeps the best peers via `addnode`, re-asserting them after a node
  restart. Optional automatic rotation (off by default).
- **Stratum Race** - times when each pool's `mining.notify` for a new block arrives. Off
  by default.

## Node software

Pick it on the dashboard (**Node** card), no file editing: the LoneStrike Labs
**Bitcoin Cash Node** Umbrel app, WillItMod's **AxeBCH** (you enter its RPC password; see
[docs/bch-differences.md](docs/bch-differences.md)), or any other BCHN-compatible node.

## Install on Umbrel

Add the community store `https://github.com/xmodem240/umbrel-community-app-store`, install
**Bitcoin Cash Lab**, and open it. It starts out pointed at the LoneStrike Labs Bitcoin Cash
Node app; use the Node card to change that.

Dashboard: `<your-umbrel>:8792`.

## Running locally

```
docker compose -f docker-compose.dev.yml up --build
```

Dashboard: <http://localhost:8788>

## Configuration

Everything is an environment variable; see `src/lib/config.js` and
[docs/configuration.md](docs/configuration.md). The node connection uses
`BITCOIN_RPC_HOST`, `BITCOIN_RPC_PORT`, `BITCOIN_RPC_USER`, `BITCOIN_RPC_PASS`,
`BITCOIN_ZMQ_HOST` and `BITCOIN_ZMQ_HASHBLOCK_PORT`.

## Tests

```
npm install
npm test
```

## Licence

MIT. Original work (c) benunskilled; see [LICENSE](LICENSE).

## Logs

The dashboard's **Logs** card shows what each background service is doing. The same lines are in `/data/logs/*.log` and, as before, in `docker logs`.
